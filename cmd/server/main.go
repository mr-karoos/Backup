package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"backup-platform/internal/artifactcrypto"
	auditRepo "backup-platform/internal/audit/repository"
	auditService "backup-platform/internal/audit/service"
	backupEngine "backup-platform/internal/backup/engine"
	backupHttpapi "backup-platform/internal/backup/httpapi"
	backupRepo "backup-platform/internal/backup/repository"
	"backup-platform/internal/backup/restic"
	backupRetention "backup-platform/internal/backup/retention"
	backupScheduler "backup-platform/internal/backup/scheduler"
	backupService "backup-platform/internal/backup/service"
	backupVerification "backup-platform/internal/backup/verification"
	backupWorker "backup-platform/internal/backup/worker"
	"backup-platform/internal/bootstrap"
	"backup-platform/internal/config"
	"backup-platform/internal/connector"
	"backup-platform/internal/connector/cpanel"
	"backup-platform/internal/connector/sshconn"
	credentialHttpapi "backup-platform/internal/credential/httpapi"
	credentialRepo "backup-platform/internal/credential/repository"
	"backup-platform/internal/credential/secretcrypto"
	credentialService "backup-platform/internal/credential/service"
	"backup-platform/internal/health"
	"backup-platform/internal/identity/httpapi"
	identityRepo "backup-platform/internal/identity/repository"
	identityService "backup-platform/internal/identity/service"
	"backup-platform/internal/organization/authz"
	orgHttpapi "backup-platform/internal/organization/httpapi"
	orgRepo "backup-platform/internal/organization/repository"
	orgService "backup-platform/internal/organization/service"
	"backup-platform/internal/platform/database"
	"backup-platform/internal/platform/httpserver"
	"backup-platform/internal/platform/logger"
	"backup-platform/internal/platform/migrations"
	resDomain "backup-platform/internal/resource/domain"
	resourceHttpapi "backup-platform/internal/resource/httpapi"
	resourceRepo "backup-platform/internal/resource/repository"
	resourceService "backup-platform/internal/resource/service"
	"backup-platform/internal/storage/local"
	storageResolver "backup-platform/internal/storage/resolver"
	s3Storage "backup-platform/internal/storage/s3"
	"backup-platform/pkg/uuid"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "fatal error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	// 1. Load and validate startup configuration
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// 2. Initialize structured JSON logger with automated sensitive key redaction
	log := logger.New(cfg.LogLevel, os.Stdout)

	// 3. Log startup metadata with non-sensitive parameters only
	log.Info("starting backup platform",
		slog.String("app_env", cfg.AppEnv),
		slog.String("http_addr", cfg.HTTPAddr),
		slog.String("log_level", cfg.LogLevel),
		slog.Bool("auth_cookie_secure", cfg.AuthCookieSecure),
	)

	// 4. Run database migrations using embedded SQL scripts
	if err := migrations.Run(cfg.DatabaseURL); err != nil {
		log.Error("database migration failed")
		return fmt.Errorf("database migration failed")
	}
	log.Info("database migrations applied successfully")

	// 5. Initialize PostgreSQL connection pool with timeout
	dbCtx, dbCancel := context.WithTimeout(context.Background(), 10*time.Second)
	db, err := database.New(dbCtx, cfg.DatabaseURL)
	dbCancel()
	if err != nil {
		log.Error("database connection initialization failed")
		return fmt.Errorf("database initialization failed")
	}
	// Guaranteed pool cleanup across all normal and error exit paths
	defer db.Close()
	log.Info("database connection pool initialized successfully")

	// 6. Initialize and validate credential encryption subsystem and operational Vault service
	keyProvider, err := secretcrypto.NewStaticKeyProvider(cfg.EncryptionMasterKey, cfg.EncryptionMasterKeyVersion)
	if err != nil {
		log.Error("credential encryption subsystem initialization failed")
		return fmt.Errorf("credential encryption subsystem initialization failed")
	}

	cryptoEngine, err := secretcrypto.NewAESGCMEngine(keyProvider)
	if err != nil {
		log.Error("credential encryption subsystem initialization failed")
		return fmt.Errorf("credential encryption subsystem initialization failed")
	}

	credRepo := credentialRepo.NewPostgresCredentialRepository()
	vaultService := credentialService.NewVaultService(cryptoEngine, credRepo, db, log)
	credHandler := credentialHttpapi.NewHandler(vaultService, log)

	// Best-effort reduction of encryption master key lifetime in config memory and environment.
	// The keyProvider retains its internal defensive key copy for runtime encryption and decryption.
	secretcrypto.ZeroBytes(cfg.EncryptionMasterKey)
	cfg.EncryptionMasterKey = nil
	_ = os.Unsetenv("ENCRYPTION_MASTER_KEY")
	_ = os.Unsetenv("ENCRYPTION_MASTER_KEY_VERSION")
	log.Info("credential encryption subsystem initialized")

	// 7. Initialize Identity and Auth core components
	userRepo := identityRepo.NewPostgresUserRepository()
	sessionRepo := identityRepo.NewPostgresSessionRepository()
	orgRepository := orgRepo.NewPostgresOrganizationRepository()
	memberRepo := orgRepo.NewPostgresMemberRepository()
	hasher := identityService.NewDefaultArgon2idHasher()
	tokenGen := identityService.NewSecureTokenGenerator()

	jwtService, err := identityService.NewJWTService([]byte(cfg.JWTSigningKey))
	if err != nil {
		log.Error("failed to initialize JWT service")
		return fmt.Errorf("jwt service initialization failed")
	}

	// Best-effort reduction of JWT secret lifetime in config struct and environment
	cfg.JWTSigningKey = ""
	_ = os.Unsetenv("JWT_SIGNING_KEY")

	// 7. Run Initial System Admin & Internal Organization Bootstrap (Idempotent)
	bootstrapSvc := bootstrap.NewService(
		bootstrap.Config{
			AdminEmail:    cfg.BootstrapAdminEmail,
			AdminPassword: cfg.BootstrapAdminPassword,
		},
		db,
		userRepo,
		orgRepository,
		memberRepo,
		hasher,
		log,
	)

	bootstrapCtx, bootstrapCancel := context.WithTimeout(context.Background(), 15*time.Second)
	err = bootstrapSvc.Run(bootstrapCtx)
	bootstrapCancel()
	if err != nil {
		log.Error("initial bootstrap failed")
		return fmt.Errorf("bootstrap initialization failed")
	}

	// Best-effort reduction of bootstrap secret lifetime in process memory and environment
	cfg.BootstrapAdminPassword = ""
	_ = os.Unsetenv("BOOTSTRAP_ADMIN_PASSWORD")

	// 8. Initialize Auth Services and HTTP Handlers
	authService := identityService.NewAuthService(
		userRepo,
		sessionRepo,
		memberRepo,
		hasher,
		tokenGen,
		jwtService,
		db,
		log,
	)

	rateLimiter := httpapi.NewRateLimiter(nil)
	authHandler := httpapi.NewHandler(
		authService,
		memberRepo,
		db,
		rateLimiter,
		cfg.AuthCookieSecure,
		log,
	)
	authMiddleware := httpapi.NewAuthMiddleware(jwtService, authService, log)

	// 9. Initialize Organization Services and HTTP Handlers
	organizationService := orgService.NewService(orgRepository, memberRepo, db, log)
	orgHandler := orgHttpapi.NewHandler(organizationService, log)

	// 10. Initialize Connector Capabilities & Registry (Phase 3D & Phase 4)
	sshTester := sshconn.NewSSHConnectionTester(nil)
	cpanelTester := cpanel.NewCPanelConnectionTester(nil)
	connectorRegistry := connector.NewRegistry()
	connectorRegistry.Register(resDomain.TypeUbuntuSSH, sshTester)
	connectorRegistry.Register(resDomain.TypeCPanel, cpanelTester)

	sshDiscoverer := sshconn.NewSSHDatabaseDiscoverer(nil)
	cpanelDiscoverer := cpanel.NewCPanelDatabaseDiscoverer(nil)
	discoveryRegistry := connector.NewDiscoveryRegistry()
	discoveryRegistry.Register(resDomain.TypeUbuntuSSH, sshDiscoverer)
	discoveryRegistry.Register(resDomain.TypeCPanel, cpanelDiscoverer)

	// 11. Initialize Resource Services and HTTP Handlers (Phase 3C, 3D & Phase 4)
	resourceRepository := resourceRepo.NewPostgresResourceRepository()
	resourceAppService := resourceService.NewService(resourceRepository, credRepo, db, log)
	connectionTestService := resourceService.NewConnectionTestService(resourceRepository, vaultService, connectorRegistry, db, log)
	databaseDiscoveryService := resourceService.NewDatabaseDiscoveryService(resourceRepository, vaultService, discoveryRegistry, db, log)
	resourceHandler := resourceHttpapi.NewHandler(resourceAppService, connectionTestService, databaseDiscoveryService, log)

	// 12. Initialize Storage Subsystem (Phase 5 & Future Phase A Step A.1)
	localStorageProvider, err := local.NewLocalStorageProvider(cfg.StorageRoot)
	if err != nil {
		log.Error("failed to initialize local storage provider")
		return fmt.Errorf("local storage initialization failed")
	}
	if err := localStorageProvider.EnsureStorageRoot(context.Background()); err != nil {
		log.Error("failed to ensure storage root directory")
		return fmt.Errorf("storage root setup failed")
	}

	endpointPolicy := &s3Storage.EndpointSecurityPolicy{
		AllowInsecureHTTP: cfg.S3AllowInsecureEndpoints,
		PrivateAllowlist:  cfg.S3PrivateEndpointsAllowlist,
	}

	// 13. Initialize Backup Engine, Capability Registries & Verification Engine (Phase 5 & Phase 6A)
	sshBackupCap := sshconn.NewSSHDatabaseBackupCapability(nil)
	backupCapRegistry := connector.NewBackupCapabilityRegistry()
	backupCapRegistry.Register(resDomain.TypeUbuntuSSH, sshBackupCap)

	sshFileCap := sshconn.NewSSHFileBackupCapability(nil)
	fileCapRegistry := connector.NewFileBackupCapabilityRegistry()
	fileCapRegistry.Register(resDomain.TypeUbuntuSSH, sshFileCap)

	artifactKeyProvider, err := artifactcrypto.NewStaticKeyProvider(cfg.ArtifactEncryptionMasterKey, cfg.ArtifactEncryptionMasterKeyVersion)
	if err != nil {
		log.Error("failed initializing artifact encryption key provider", slog.String("error", err.Error()))
		return fmt.Errorf("artifact key provider initialization failed")
	}
	secretcrypto.ZeroBytes(cfg.ArtifactEncryptionMasterKey)

	directStreamEngine := backupEngine.NewDirectStreamBackupEngineWithKeyProvider(artifactKeyProvider)
	verificationEngine := backupVerification.NewVerificationEngineWithKeyProvider(artifactKeyProvider)

	// 14. Initialize Audit and Backup Repositories, Services, and HTTP Handlers (Phase 5, 6A, 7A & Phase 8)
	auditRepository := auditRepo.NewPostgresAuditRepository(db)
	auditRecorder := auditService.NewAuditService(auditRepository, log)

	backupRepository := backupRepo.NewPostgresBackupRepository(db)
	storageResolverService := storageResolver.NewStorageResolver(
		localStorageProvider,
		backupRepository,
		vaultService,
		cfg.S3AllowInsecureEndpoints,
		cfg.S3PrivateEndpointsAllowlist,
	)
	resFinder := &resourceFinderAdapter{repo: resourceRepository, db: db}
	backupJobService := backupService.NewBackupJobService(backupRepository, resFinder)
	backupPlanService := backupService.NewBackupPlanService(backupRepository, resFinder)
	historyService := backupService.NewHistoryService(backupRepository, log)
	artifactService := backupService.NewArtifactService(backupRepository, localStorageProvider, auditRecorder, log)
	artifactService.SetStorageResolver(storageResolverService)
	artifactService.SetKeyProvider(artifactKeyProvider)
	verificationService := backupService.NewVerificationService(backupRepository, localStorageProvider, verificationEngine, log)
	verificationService.SetStorageResolver(storageResolverService)

	storageTargetService := backupService.NewStorageTargetService(backupRepository, vaultService, endpointPolicy, log)

	// 14b. Initialize Restic Repository Subsystem (Future Phase A Step A.3)
	resticBinPath := "/usr/local/bin/restic"
	if p := os.Getenv("RESTIC_BINARY_PATH"); p != "" {
		resticBinPath = p
	}
	resticRunner := restic.NewResticRunner(resticBinPath, log)

	// Validate exact Restic version 0.19.1 at startup (fail-fast)
	resticVerCtx, resticVerCancel := context.WithTimeout(context.Background(), 5*time.Second)
	if err := resticRunner.ValidateVersion(resticVerCtx); err != nil {
		resticVerCancel()
		log.Error("restic binary validation failed at startup", "error", err)
		os.Exit(1)
	}
	resticVerCancel()
	resticTargetResolver := restic.NewTargetResolver(
		backupRepository,
		vaultService,
		cfg.StorageRoot,
		cfg.S3AllowInsecureEndpoints,
		cfg.S3PrivateEndpointsAllowlist,
	)
	_ = backupService.NewRepositoryService(
		backupRepository,
		backupRepository,
		resFinder,
		vaultService,
		resticTargetResolver,
		resticRunner,
		log,
	)

	backupHandler := backupHttpapi.NewHandler(backupJobService, backupPlanService, historyService, artifactService, verificationService, log)
	backupHandler.SetStorageTargetService(storageTargetService)

	// 15. Run Startup Recovery for Interrupted Backup Runs (Fail-fast)
	recoveryCtx, recoveryCancel := context.WithTimeout(context.Background(), 10*time.Second)
	if err := backupWorker.RunStartupRecoveryWithResolver(recoveryCtx, backupRepository, localStorageProvider, storageResolverService, log); err != nil {
		recoveryCancel()
		log.Error("startup backup recovery failed")
		return fmt.Errorf("startup backup recovery failed")
	}
	recoveryCancel()

	// 16. Initialize and Start In-Process Mutex, Worker Pool, Stale Reaper & Scheduler
	backgroundCtx, backgroundCancel := context.WithCancel(context.Background())
	defer backgroundCancel()

	resourceMutexManager := backupWorker.NewPerResourceMutexManager()
	resConnFinder := &resourceConnectorFinderAdapter{repo: resourceRepository, db: db}
	retentionProcessor := backupRetention.NewProcessor(
		backupRepository,
		localStorageProvider,
		auditRecorder,
		log,
	)
	retentionProcessor.SetStorageResolver(storageResolverService)

	workerPool := backupWorker.NewWorkerPool(
		backupWorker.DefaultWorkerPoolConfig(),
		backupRepository,
		resConnFinder,
		vaultService,
		backupCapRegistry,
		fileCapRegistry,
		directStreamEngine,
		localStorageProvider,
		verificationEngine,
		resourceMutexManager,
		log,
	)
	workerPool.SetRetentionManager(retentionProcessor)
	workerPool.SetStorageResolver(storageResolverService)
	workerPool.SetKeyProvider(artifactKeyProvider)
	workerPool.Start(backgroundCtx)

	staleReaper := backupWorker.NewStaleRunReaper(backupRepository, localStorageProvider, 30*time.Second, log)
	staleReaper.SetStorageResolver(storageResolverService)
	staleReaper.Start(backgroundCtx)

	backupSched := backupScheduler.NewScheduler(backupRepository, log, 10*time.Second)
	go func() {
		_ = backupSched.Start(backgroundCtx)
	}()

	stopBackgroundServices := func(ctx context.Context) {
		backgroundCancel()
		_ = staleReaper.Stop(ctx)
		_ = workerPool.Stop(ctx)
	}

	// 17. Setup HTTP routes and middleware chain
	mux := http.NewServeMux()

	// Health endpoint (Public)
	healthHandler := health.NewHandler(db)
	mux.Handle("/api/v1/health", healthHandler)

	// Public Auth endpoints
	mux.HandleFunc("/api/v1/auth/login", authHandler.Login)
	mux.HandleFunc("/api/v1/auth/refresh", authHandler.Refresh)

	// Protected Auth endpoints
	mux.Handle("/api/v1/auth/logout", authMiddleware(http.HandlerFunc(authHandler.Logout)))
	mux.Handle("/api/v1/auth/me", authMiddleware(http.HandlerFunc(authHandler.Me)))

	// Organization middlewares & endpoints
	orgContextMiddleware := orgHttpapi.NewOrganizationContextMiddleware(memberRepo, db, log)

	// Global/Platform organization routes (Phase 2D-1)
	mux.Handle("GET /api/v1/organizations", authMiddleware(http.HandlerFunc(orgHandler.List)))
	mux.Handle("POST /api/v1/organizations", authMiddleware(orgHttpapi.RequireSystemAdmin(log)(http.HandlerFunc(orgHandler.Create))))

	// Tenant-scoped organization routes (Phase 2D-2A & Phase 2D-2B)
	mux.Handle("GET /api/v1/organizations/{id}", authMiddleware(orgContextMiddleware(orgHttpapi.RequirePathOrganizationMatch("id", log)(http.HandlerFunc(orgHandler.GetByID)))))
	mux.Handle("PUT /api/v1/organizations/{id}", authMiddleware(orgHttpapi.AuthorizeOrganizationUpdate(memberRepo, db, log)(http.HandlerFunc(orgHandler.Update))))

	// Tenant-scoped credential routes (Phase 3B-3A & Phase 3B-3B)
	mux.Handle("GET /api/v1/credentials", authMiddleware(orgContextMiddleware(orgHttpapi.RequireOrganizationAdmin(log)(http.HandlerFunc(credHandler.List)))))
	mux.Handle("GET /api/v1/credentials/{id}", authMiddleware(orgContextMiddleware(orgHttpapi.RequireOrganizationAdmin(log)(http.HandlerFunc(credHandler.GetByID)))))
	mux.Handle("POST /api/v1/credentials", authMiddleware(orgContextMiddleware(orgHttpapi.RequireOrganizationAdmin(log)(http.HandlerFunc(credHandler.Create)))))
	mux.Handle("PUT /api/v1/credentials/{id}", authMiddleware(orgContextMiddleware(orgHttpapi.RequireOrganizationAdmin(log)(http.HandlerFunc(credHandler.Update)))))
	mux.Handle("DELETE /api/v1/credentials/{id}", authMiddleware(orgContextMiddleware(orgHttpapi.RequireOrganizationAdmin(log)(http.HandlerFunc(credHandler.Delete)))))

	// Tenant-scoped resource routes (Phase 3C, Phase 3D & Phase 4)
	mux.Handle("GET /api/v1/resources", authMiddleware(orgContextMiddleware(orgHttpapi.RequirePermission(authz.PermissionResourceRead, log)(http.HandlerFunc(resourceHandler.List)))))
	mux.Handle("POST /api/v1/resources", authMiddleware(orgContextMiddleware(orgHttpapi.RequirePermission(authz.PermissionResourceWrite, log)(http.HandlerFunc(resourceHandler.Create)))))
	mux.Handle("GET /api/v1/resources/{id}", authMiddleware(orgContextMiddleware(orgHttpapi.RequirePermission(authz.PermissionResourceRead, log)(http.HandlerFunc(resourceHandler.GetByID)))))
	mux.Handle("PUT /api/v1/resources/{id}", authMiddleware(orgContextMiddleware(orgHttpapi.RequirePermission(authz.PermissionResourceWrite, log)(http.HandlerFunc(resourceHandler.Update)))))
	mux.Handle("DELETE /api/v1/resources/{id}", authMiddleware(orgContextMiddleware(orgHttpapi.RequirePermission(authz.PermissionResourceWrite, log)(http.HandlerFunc(resourceHandler.Delete)))))
	mux.Handle("POST /api/v1/resources/{id}/test-connection", authMiddleware(orgContextMiddleware(orgHttpapi.RequirePermission(authz.PermissionResourceWrite, log)(http.HandlerFunc(resourceHandler.TestConnection)))))
	mux.Handle("GET /api/v1/resources/{id}/databases", authMiddleware(orgContextMiddleware(orgHttpapi.RequirePermission(authz.PermissionResourceWrite, log)(http.HandlerFunc(resourceHandler.DiscoverDatabases)))))

	// Tenant-scoped storage target routes (Future Phase A Step A.1)
	mux.Handle("GET /api/v1/storage-targets", authMiddleware(orgContextMiddleware(orgHttpapi.RequirePermission(authz.PermissionStorageTargetRead, log)(http.HandlerFunc(backupHandler.ListStorageTargets)))))
	mux.Handle("POST /api/v1/storage-targets", authMiddleware(orgContextMiddleware(orgHttpapi.RequirePermission(authz.PermissionStorageTargetWrite, log)(http.HandlerFunc(backupHandler.CreateStorageTarget)))))
	mux.Handle("GET /api/v1/storage-targets/{id}", authMiddleware(orgContextMiddleware(orgHttpapi.RequirePermission(authz.PermissionStorageTargetRead, log)(http.HandlerFunc(backupHandler.GetStorageTarget)))))
	mux.Handle("PUT /api/v1/storage-targets/{id}", authMiddleware(orgContextMiddleware(orgHttpapi.RequirePermission(authz.PermissionStorageTargetWrite, log)(http.HandlerFunc(backupHandler.UpdateStorageTarget)))))
	mux.Handle("DELETE /api/v1/storage-targets/{id}", authMiddleware(orgContextMiddleware(orgHttpapi.RequirePermission(authz.PermissionStorageTargetWrite, log)(http.HandlerFunc(backupHandler.DeleteStorageTarget)))))

	// Tenant-scoped backup plan routes (Phase 7A)
	mux.Handle("GET /api/v1/backup-plans", authMiddleware(orgContextMiddleware(orgHttpapi.RequirePermission(authz.PermissionBackupPlanRead, log)(http.HandlerFunc(backupHandler.ListBackupPlans)))))
	mux.Handle("POST /api/v1/backup-plans", authMiddleware(orgContextMiddleware(orgHttpapi.RequirePermission(authz.PermissionBackupPlanWrite, log)(http.HandlerFunc(backupHandler.CreateBackupPlan)))))
	mux.Handle("GET /api/v1/backup-plans/{id}", authMiddleware(orgContextMiddleware(orgHttpapi.RequirePermission(authz.PermissionBackupPlanRead, log)(http.HandlerFunc(backupHandler.GetBackupPlan)))))
	mux.Handle("PUT /api/v1/backup-plans/{id}", authMiddleware(orgContextMiddleware(orgHttpapi.RequirePermission(authz.PermissionBackupPlanWrite, log)(http.HandlerFunc(backupHandler.UpdateBackupPlan)))))
	mux.Handle("DELETE /api/v1/backup-plans/{id}", authMiddleware(orgContextMiddleware(orgHttpapi.RequirePermission(authz.PermissionBackupPlanWrite, log)(http.HandlerFunc(backupHandler.ArchiveBackupPlan)))))

	// Tenant-scoped backup execution routes (Phase 5 & Phase 6A)
	mux.Handle("POST /api/v1/backup-jobs", authMiddleware(orgContextMiddleware(orgHttpapi.RequirePermission(authz.PermissionBackupJobExecute, log)(http.HandlerFunc(backupHandler.CreateBackupJob)))))

	// Tenant-scoped backup runs and history routes (Phase 8 Step 1 & Phase 9 Step 1)
	mux.Handle("GET /api/v1/backup-runs", authMiddleware(orgContextMiddleware(http.HandlerFunc(backupHandler.ListBackupRuns))))
	mux.Handle("GET /api/v1/backup-runs/{id}", authMiddleware(orgContextMiddleware(http.HandlerFunc(backupHandler.GetBackupRun))))
	mux.Handle("POST /api/v1/backup-runs/{id}/verify", authMiddleware(orgContextMiddleware(orgHttpapi.RequirePermission(authz.PermissionBackupRunVerify, log)(http.HandlerFunc(backupHandler.VerifyBackupRun)))))

	// Tenant-scoped backup artifacts, secure download and delete routes (Phase 8 Step 1)
	mux.Handle("GET /api/v1/backup-artifacts", authMiddleware(orgContextMiddleware(http.HandlerFunc(backupHandler.ListBackupArtifacts))))
	mux.Handle("GET /api/v1/backup-artifacts/{id}", authMiddleware(orgContextMiddleware(http.HandlerFunc(backupHandler.GetBackupArtifact))))
	mux.Handle("GET /api/v1/backup-artifacts/{id}/download", authMiddleware(orgContextMiddleware(orgHttpapi.RequirePermission(authz.PermissionBackupArtifactDownload, log)(http.HandlerFunc(backupHandler.DownloadBackupArtifact)))))
	mux.Handle("DELETE /api/v1/backup-artifacts/{id}", authMiddleware(orgContextMiddleware(orgHttpapi.RequirePermission(authz.PermissionBackupArtifactDelete, log)(http.HandlerFunc(backupHandler.DeleteBackupArtifact)))))

	// Chain middlewares: RequestID -> Logging -> Route Handler
	handler := httpserver.RequestIDMiddleware(httpserver.LoggingMiddleware(log)(mux))
	server := httpserver.New(cfg.HTTPAddr, handler)

	// 18. Start HTTP server in a background goroutine
	serverErrors := make(chan error, 1)
	go func() {
		log.Info("http server listening", slog.String("addr", cfg.HTTPAddr))
		serverErrors <- server.Start()
	}()

	// 19. Listen for OS termination signals (SIGINT, SIGTERM)
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(shutdown)

	select {
	case err := <-serverErrors:
		log.Error("server crashed")
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		stopBackgroundServices(shutdownCtx)
		shutdownCancel()
		return fmt.Errorf("server crashed: %w", err)

	case sig := <-shutdown:
		log.Info("shutdown signal received, initiating graceful shutdown", slog.String("signal", sig.String()))

		// 20. Graceful shutdown sequence with bounded timeout
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer shutdownCancel()

		// Step A: Stop accepting new HTTP requests and wait for active requests to finish
		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Error("http server graceful shutdown failed")
		} else {
			log.Info("http server stopped successfully")
		}

		// Step B: Stop background workers and reaper with bounded timeout
		workerShutdownCtx, workerShutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		stopBackgroundServices(workerShutdownCtx)
		workerShutdownCancel()

		log.Info("backup platform stopped gracefully")
		return nil
	}
}

type resourceFinderAdapter struct {
	repo resourceRepo.ResourceRepository
	db   database.TxManager
}

func (a *resourceFinderAdapter) GetByID(ctx context.Context, orgID, resourceID uuid.UUID) (*resDomain.Resource, error) {
	resWithConn, err := a.repo.FindByIDForOrganization(ctx, a.db.Querier(), orgID, resourceID)
	if err != nil {
		return nil, err
	}
	return resWithConn.Resource, nil
}

type resourceConnectorFinderAdapter struct {
	repo resourceRepo.ResourceRepository
	db   database.TxManager
}

func (a *resourceConnectorFinderAdapter) FindByIDForOrganization(ctx context.Context, orgID, resID uuid.UUID) (*resDomain.ResourceWithConnector, error) {
	return a.repo.FindByIDForOrganization(ctx, a.db.Querier(), orgID, resID)
}
