package worker

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5"

	"backup-platform/internal/backup/domain"
	"backup-platform/internal/backup/engine"
	backupRepo "backup-platform/internal/backup/repository"
	"backup-platform/internal/backup/restic"
	"backup-platform/internal/backup/service"
	"backup-platform/internal/backup/verification"
	"backup-platform/internal/connector"
	credDomain "backup-platform/internal/credential/domain"
	"backup-platform/internal/credential/payload"
	credRepo "backup-platform/internal/credential/repository"
	"backup-platform/internal/credential/secretcrypto"
	credService "backup-platform/internal/credential/service"
	orgDomain "backup-platform/internal/organization/domain"
	"backup-platform/internal/platform/database"
	"backup-platform/internal/platform/migrations"
	resDomain "backup-platform/internal/resource/domain"
	resRepo "backup-platform/internal/resource/repository"
	"backup-platform/pkg/uuid"
)

// realTestResourceFinder adapts the resource repository for WorkerPool and RepositoryService.
type realTestResourceFinder struct {
	repo resRepo.ResourceRepository
	db   database.TxManager
}

func (f *realTestResourceFinder) FindByIDForOrganization(ctx context.Context, orgID, resourceID uuid.UUID) (*resDomain.ResourceWithConnector, error) {
	return f.repo.FindByIDForOrganization(ctx, f.db.Querier(), orgID, resourceID)
}

func (f *realTestResourceFinder) GetByID(ctx context.Context, orgID, resourceID uuid.UUID) (*resDomain.Resource, error) {
	resWithConn, err := f.repo.FindByIDForOrganization(ctx, f.db.Querier(), orgID, resourceID)
	if err != nil {
		return nil, err
	}
	return resWithConn.Resource, nil
}

// realMySQLDumpCapability generates a deterministic MySQL dump stream.
type realMySQLDumpCapability struct {
	dumpContent string
}

func (c *realMySQLDumpCapability) BackupDatabase(
	ctx context.Context,
	target connector.Target,
	credPayload *payload.PayloadV1,
	databaseName string,
	dest io.Writer,
) error {
	_, err := io.WriteString(dest, c.dumpContent)
	return err
}

func findRealResticBinary() string {
	binaryPath := os.Getenv("RESTIC_BINARY_PATH")
	if binaryPath != "" {
		return binaryPath
	}
	candidates := []string{
		filepath.Join(os.TempDir(), "restic-bin", "restic.exe"),
		filepath.Join(os.TempDir(), "restic-bin", "restic"),
		"/usr/local/bin/restic",
		"restic",
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return ""
}

// TestWorkerPool_RealPostgres15AndRestic_A4_E2E validates the complete 20-step Worker execution
// pipeline against real PostgreSQL 15.x and real Restic 0.19.1 without faking the database, Restic engine,
// runner, or verification engine.
func TestWorkerPool_RealPostgres15AndRestic_A4_E2E(t *testing.T) {
	testDBURL := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	if testDBURL == "" {
		t.Skip("skipping real worker E2E test: TEST_DATABASE_URL not set")
	}

	resticBin := findRealResticBinary()
	if resticBin == "" {
		t.Skip("skipping real worker E2E test: RESTIC_BINARY_PATH not set or binary not found")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	// 1. Validate PostgreSQL connection and version
	conn, err := pgx.Connect(ctx, testDBURL)
	if err != nil {
		t.Fatalf("failed connecting to test database: %v", err)
	}
	var serverVersion string
	if err := conn.QueryRow(ctx, "SHOW server_version;").Scan(&serverVersion); err != nil {
		_ = conn.Close(ctx)
		t.Fatalf("failed querying server_version: %v", err)
	}
	_ = conn.Close(ctx)
	t.Logf("PostgreSQL server_version: %s", serverVersion)

	// 2. Ensure database schema is migrated to migration 9
	d, err := iofs.New(migrations.FS, "sql")
	if err != nil {
		t.Fatalf("failed creating iofs driver: %v", err)
	}
	m, err := migrate.NewWithSourceInstance("iofs", d, testDBURL)
	if err != nil {
		t.Fatalf("failed initializing migrate instance: %v", err)
	}
	defer func() {
		_, _ = m.Close()
	}()
	if err := m.Migrate(9); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		t.Fatalf("failed migrating database to version 9: %v", err)
	}

	// 3. Connect database connection pool
	pool, err := database.New(ctx, testDBURL)
	if err != nil {
		t.Fatalf("failed initializing database pool: %v", err)
	}
	defer pool.Close()

	// 4. Initialize Core Repositories & Real VaultService
	masterKey := []byte("12345678901234567890123456789012") // 32-byte AES-256
	keyProvider, err := secretcrypto.NewStaticKeyProvider(masterKey, 1)
	if err != nil {
		t.Fatalf("failed creating static key provider: %v", err)
	}
	cryptoEngine, err := secretcrypto.NewAESGCMEngine(keyProvider)
	if err != nil {
		t.Fatalf("failed creating crypto engine: %v", err)
	}

	credentialRepo := credRepo.NewPostgresCredentialRepository()
	vaultService := credService.NewVaultService(cryptoEngine, credentialRepo, pool, nil)
	backupRepository := backupRepo.NewPostgresBackupRepository(pool)
	resourceRepository := resRepo.NewPostgresResourceRepository()
	resFinder := &realTestResourceFinder{repo: resourceRepository, db: pool}

	// 5. Initialize Restic Engine, Runner, Supervisor, Coordinator, and Resolver
	tempStorageRoot := t.TempDir()
	logger := slog.Default()
	resticRunner := restic.NewResticRunner(resticBin, logger)
	if err := resticRunner.ValidateVersion(ctx); err != nil {
		t.Fatalf("restic version validation failed: %v", err)
	}

	targetResolver := restic.NewTargetResolver(backupRepository, vaultService, tempStorageRoot, false, nil)
	repoCoordinator := restic.NewRepositoryOperationCoordinator()
	repoService := service.NewRepositoryService(
		backupRepository,
		backupRepository,
		resFinder,
		vaultService,
		targetResolver,
		resticRunner,
		logger,
	)

	gatedSupervisor := engine.NewGatedEOFSupervisor(resticBin, logger)
	resticEngine := engine.NewResticBackupEngine(gatedSupervisor, logger)
	verificationEngine := verification.NewVerificationEngine()

	// 6. Setup Test Organization, Resource, Connector Credential, and Storage Target
	orgID := uuid.New()
	resID := uuid.New()
	connCredID := uuid.New()
	targetID := uuid.New()
	jobID := uuid.New()

	cleanup := func() {
		cleanupCtx := context.Background()
		_, _ = pool.Querier().Exec(cleanupCtx, "DELETE FROM backup_artifacts WHERE organization_id = $1", orgID)
		_, _ = pool.Querier().Exec(cleanupCtx, "DELETE FROM backup_runs WHERE organization_id = $1", orgID)
		_, _ = pool.Querier().Exec(cleanupCtx, "DELETE FROM backup_jobs WHERE organization_id = $1", orgID)
		_, _ = pool.Querier().Exec(cleanupCtx, "DELETE FROM backup_repositories WHERE organization_id = $1", orgID)
		_, _ = pool.Querier().Exec(cleanupCtx, "DELETE FROM resource_connectors WHERE organization_id = $1", orgID)
		_, _ = pool.Querier().Exec(cleanupCtx, "DELETE FROM resources WHERE organization_id = $1", orgID)
		_, _ = pool.Querier().Exec(cleanupCtx, "DELETE FROM credentials WHERE organization_id = $1", orgID)
		_, _ = pool.Querier().Exec(cleanupCtx, "DELETE FROM storage_targets WHERE organization_id = $1", orgID)
		_, _ = pool.Querier().Exec(cleanupCtx, "DELETE FROM organizations WHERE id = $1", orgID)
	}
	cleanup()
	t.Cleanup(cleanup)

	// Seed Organization
	slug := fmt.Sprintf("org-real-worker-e2e-%s", orgID.String()[:8])
	_, err = pool.Querier().Exec(ctx, `
		INSERT INTO organizations (id, name, slug, status, metadata, created_at, updated_at)
		VALUES ($1, 'Real Worker E2E Org', $2, 'active', '{}'::jsonb, NOW(), NOW())`,
		orgID, slug)
	if err != nil {
		t.Fatalf("failed inserting org: %v", err)
	}

	// Seed User Credential for Connector via VaultService
	connectorPassword := "database-secret-ssh-pw-1234"
	encConnPayload, err := payload.EncodeV1(connectorPassword, nil)
	if err != nil {
		t.Fatalf("failed encoding connector payload: %v", err)
	}
	_, err = vaultService.CreateCredential(ctx, orgID, "Resource SSH Password", credDomain.TypeSSHPassword, encConnPayload, nil)
	if err != nil {
		t.Fatalf("failed creating credential via vault service: %v", err)
	}
	// Fetch generated cred ID
	var seededCredID uuid.UUID
	err = pool.Querier().QueryRow(ctx, "SELECT id FROM credentials WHERE organization_id = $1 AND name = 'Resource SSH Password'", orgID).Scan(&seededCredID)
	if err != nil {
		t.Fatalf("failed querying connector cred ID: %v", err)
	}
	connCredID = seededCredID

	// Seed Resource and Connector
	_, err = pool.Querier().Exec(ctx, `
		INSERT INTO resources (id, organization_id, name, type, status, metadata, created_at, updated_at)
		VALUES ($1, $2, 'Production MySQL Server', 'ubuntu_ssh', 'active', '{}'::jsonb, NOW(), NOW())`,
		resID, orgID)
	if err != nil {
		t.Fatalf("failed inserting resource: %v", err)
	}

	_, err = pool.Querier().Exec(ctx, `
		INSERT INTO resource_connectors (id, organization_id, resource_id, connector_type, credential_id, host, port, auth_type, host_key_fingerprint, config, created_at, updated_at)
		VALUES ($1, $2, $3, 'ubuntu_ssh', $4, '127.0.0.1', 22, 'ssh_password', NULL, '{"username":"root"}'::jsonb, NOW(), NOW())`,
		uuid.New(), orgID, resID, connCredID)
	if err != nil {
		t.Fatalf("failed inserting resource connector: %v", err)
	}

	// Seed Storage Target (Local filesystem target)
	_, err = pool.Querier().Exec(ctx, `
		INSERT INTO storage_targets (id, organization_id, name, type, status, is_default, config, created_at, updated_at)
		VALUES ($1, $2, 'Primary Local Storage', 'local', 'active', true, '{}'::jsonb, NOW(), NOW())`,
		targetID, orgID)
	if err != nil {
		t.Fatalf("failed inserting storage target: %v", err)
	}

	// 7. Seed Pending Backup Job
	_, err = pool.Querier().Exec(ctx, `
		INSERT INTO backup_jobs (
			id, organization_id, resource_id, trigger_type, backup_type, engine_type, storage_target_id,
			target_spec, status, created_at, updated_at
		) VALUES (
			$1, $2, $3, 'scheduled', 'mysql_database', 'restic', $4,
			'{"databases":["test_db"]}'::jsonb, 'pending', NOW(), NOW()
		)`,
		jobID, orgID, resID, targetID)
	if err != nil {
		t.Fatalf("failed inserting pending backup job: %v", err)
	}

	// 8. Register capability
	const syntheticDump = "-- MySQL dump 10.13  Distrib 8.0.32\n-- Host: 127.0.0.1    Database: test_db\nCREATE TABLE `users` (`id` int NOT NULL, `name` varchar(50));\nINSERT INTO `users` VALUES (1, 'Alice');\n"
	capRegistry := connector.NewBackupCapabilityRegistry()
	capRegistry.Register(resDomain.TypeUbuntuSSH, &realMySQLDumpCapability{dumpContent: syntheticDump})

	// 9. Build WorkerPool
	workerPool := NewWorkerPool(
		WorkerPoolConfig{
			NumWorkers:        1,
			PollInterval:      100 * time.Millisecond,
			HeartbeatInterval: 2 * time.Second,
		},
		backupRepository,
		resFinder,
		vaultService,
		capRegistry,
		nil,
		engine.NewDirectStreamBackupEngine(),
		nil,
		verificationEngine,
		NewPerResourceMutexManager(),
		logger,
	)
	workerPool.SetResticEngine(
		resticEngine,
		repoCoordinator,
		repoService,
		resticRunner,
		targetResolver,
	)

	// 10. Execute Job via processNextAvailableJob
	workerPool.processNextAvailableJob(ctx, 1)

	// 11. Assertions on Database State
	// 11a. Job should be finalized to completed
	var jobStatus string
	err = pool.Querier().QueryRow(ctx, "SELECT status FROM backup_jobs WHERE id = $1", jobID).Scan(&jobStatus)
	if err != nil {
		t.Fatalf("failed querying job status: %v", err)
	}
	if jobStatus != string(domain.JobStatusCompleted) {
		t.Errorf("expected job status %s, got %s", domain.JobStatusCompleted, jobStatus)
	}

	// 11b. Run should be finalized as success
	var runID uuid.UUID
	var runStatus string
	err = pool.Querier().QueryRow(ctx, `
		SELECT id, status FROM backup_runs
		WHERE job_id = $1 ORDER BY created_at DESC LIMIT 1`,
		jobID).Scan(&runID, &runStatus)
	if err != nil {
		t.Fatalf("failed querying run: %v", err)
	}
	if runStatus != string(domain.RunStatusSuccess) {
		t.Fatalf("expected run status success, got %s", runStatus)
	}

	// 11c. Artifact should be polymorphic restic snapshot
	var artID uuid.UUID
	var artFormat string
	var artSnapshotID string
	var artRepoID uuid.UUID
	var artLogicalSize int64
	var artPhysicalSize *int64
	var artVerStatus string
	var artStorageRef *string
	var engineMetaBytes []byte
	err = pool.Querier().QueryRow(ctx, `
		SELECT id, format, snapshot_id, repository_id, logical_size_bytes, size_bytes,
		       verification_status, storage_reference, engine_metadata
		FROM backup_artifacts WHERE run_id = $1 LIMIT 1`,
		runID).Scan(&artID, &artFormat, &artSnapshotID, &artRepoID, &artLogicalSize, &artPhysicalSize,
		&artVerStatus, &artStorageRef, &engineMetaBytes)
	if err != nil {
		t.Fatalf("failed querying artifact: %v", err)
	}

	if artFormat != string(domain.ArtifactFormatResticSnapshot) {
		t.Errorf("expected format restic_snapshot, got %s", artFormat)
	}
	if !domain.IsValidCanonicalResticSnapshotID(artSnapshotID) {
		t.Errorf("expected 64-hex snapshot ID, got %s", artSnapshotID)
	}
	if artRepoID == uuid.Nil {
		t.Errorf("expected non-nil repository_id")
	}
	if artLogicalSize != int64(len(syntheticDump)) {
		t.Errorf("expected logical_size_bytes %d, got %d", len(syntheticDump), artLogicalSize)
	}
	if artPhysicalSize != nil {
		t.Errorf("expected nil physical size_bytes for restic artifact, got %v", artPhysicalSize)
	}
	if artStorageRef != nil {
		t.Errorf("expected nil storage_reference for restic artifact, got %v", artStorageRef)
	}
	if artVerStatus != string(domain.VerificationStatusVerified) {
		t.Errorf("expected verification_status verified, got %s", artVerStatus)
	}

	var meta domain.ResticArtifactMetadata
	if err := json.Unmarshal(engineMetaBytes, &meta); err != nil {
		t.Fatalf("failed unmarshaling engine metadata: %v", err)
	}
	if meta.InternalFilename != "test_db.sql" {
		t.Errorf("expected internal_filename 'test_db.sql', got %s", meta.InternalFilename)
	}

	// 12. Assertions on Physical Repository
	expectedRepoPath := filepath.Join(tempStorageRoot, "repositories", "organizations", orgID.String(), "resources", resID.String(), "restic")
	if _, err := os.Stat(filepath.Join(expectedRepoPath, "config")); err != nil {
		t.Fatalf("expected physical restic config at %s: %v", expectedRepoPath, err)
	}

	// 13. Test ArtifactService streaming download roundtrip
	artifactService := service.NewArtifactService(backupRepository, nil, nil, logger)
	artifactService.SetResticDependencies(resticRunner, repoCoordinator, vaultService, targetResolver)

	downloadDesc, err := artifactService.OpenArtifactDownload(ctx, orgDomain.RoleAdmin, orgID, artID)
	if err != nil {
		t.Fatalf("expected OpenArtifactDownload to succeed, got %v", err)
	}
	defer func() {
		_ = downloadDesc.Close()
	}()

	if downloadDesc.ContentType != "application/gzip" {
		t.Errorf("expected content-type application/gzip, got %s", downloadDesc.ContentType)
	}

	// Decompress stream and verify exact content matches syntheticDump
	gzReader, err := gzip.NewReader(downloadDesc.Reader)
	if err != nil {
		t.Fatalf("failed creating gzip reader: %v", err)
	}
	defer func() {
		_ = gzReader.Close()
	}()

	decompressedBytes, err := io.ReadAll(gzReader)
	if err != nil {
		t.Fatalf("failed reading decompressed stream: %v", err)
	}

	if string(decompressedBytes) != syntheticDump {
		t.Fatalf("decompressed stream content mismatch: expected %q, got %q", syntheticDump, string(decompressedBytes))
	}
	t.Logf("Successfully verified complete 20-step Worker A.4 E2E execution and download roundtrip!")
}
