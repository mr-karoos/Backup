package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"backup-platform/internal/artifactcrypto"
	"backup-platform/internal/backup/domain"
	"backup-platform/internal/backup/engine"
	"backup-platform/internal/backup/repository"
	"backup-platform/internal/backup/retention"
	"backup-platform/internal/backup/verification"
	"backup-platform/internal/connector"
	"backup-platform/internal/connector/sshconn"
	credDomain "backup-platform/internal/credential/domain"
	"backup-platform/internal/credential/payload"
	"backup-platform/internal/credential/secretcrypto"
	resDomain "backup-platform/internal/resource/domain"
	"backup-platform/internal/storage"
	"backup-platform/pkg/uuid"
)

// RetentionManager applies retention policy to expired runs after successful backup completion.
type RetentionManager interface {
	ApplyAfterSuccessfulRun(ctx context.Context, orgID uuid.UUID, planID *uuid.UUID, currentRunID uuid.UUID) (*retention.CleanupSummary, error)
}

// ResourceConnectorFinder fetches resource and connector configurations within an organization.
type ResourceConnectorFinder interface {
	FindByIDForOrganization(ctx context.Context, orgID, resID uuid.UUID) (*resDomain.ResourceWithConnector, error)
}

// CredentialVault loads and decrypts credential payloads for internal operational use.
type CredentialVault interface {
	LoadCredentialForUse(ctx context.Context, orgID, credID uuid.UUID) (credDomain.Type, []byte, error)
}

// WorkerPoolConfig defines runtime configuration for the backup worker pool.
type WorkerPoolConfig struct {
	NumWorkers        int
	PollInterval      time.Duration
	HeartbeatInterval time.Duration
}

// DefaultWorkerPoolConfig returns the default worker pool configuration.
func DefaultWorkerPoolConfig() WorkerPoolConfig {
	return WorkerPoolConfig{
		NumWorkers:        2,
		PollInterval:      1 * time.Second,
		HeartbeatInterval: 30 * time.Second,
	}
}

// databaseDiscoverer discovers non-system databases dynamically on a target resource.
type databaseDiscoverer interface {
	DiscoverDatabases(ctx context.Context, target connector.Target, credPayload *payload.PayloadV1) ([]connector.DatabaseInfo, error)
}

// WorkerPool manages concurrent worker goroutines consuming jobs from the durable PostgreSQL queue.
type WorkerPool struct {
	cfg                    WorkerPoolConfig
	repo                   repository.BackupRepository
	resFinder              ResourceConnectorFinder
	vault                  CredentialVault
	capabilityRegistry     *connector.BackupCapabilityRegistry
	fileCapabilityRegistry *connector.FileBackupCapabilityRegistry
	engine                 engine.BackupEngine
	storageProvider        storage.StorageProvider
	storageResolver        storage.StorageProviderResolver
	verifier               verification.Verifier
	mutexManager           *PerResourceMutexManager
	logger                 *slog.Logger
	nowFunc                func() time.Time
	cancel                 context.CancelFunc
	wg                     sync.WaitGroup
	databaseDiscoverer     databaseDiscoverer
	retentionManager       RetentionManager
	keyProvider            artifactcrypto.KeyProvider
}

// NewWorkerPool constructs a new WorkerPool instance.
func NewWorkerPool(
	cfg WorkerPoolConfig,
	repo repository.BackupRepository,
	resFinder ResourceConnectorFinder,
	vault CredentialVault,
	capabilityRegistry *connector.BackupCapabilityRegistry,
	fileCapabilityRegistry *connector.FileBackupCapabilityRegistry,
	engine engine.BackupEngine,
	storageProvider storage.StorageProvider,
	verifier verification.Verifier,
	mutexManager *PerResourceMutexManager,
	log *slog.Logger,
) *WorkerPool {
	if cfg.NumWorkers <= 0 {
		cfg.NumWorkers = 2
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 1 * time.Second
	}
	if cfg.HeartbeatInterval <= 0 {
		cfg.HeartbeatInterval = 30 * time.Second
	}
	if log == nil {
		log = slog.Default()
	}
	if mutexManager == nil {
		mutexManager = NewPerResourceMutexManager()
	}

	wp := &WorkerPool{
		cfg:                    cfg,
		repo:                   repo,
		resFinder:              resFinder,
		vault:                  vault,
		capabilityRegistry:     capabilityRegistry,
		fileCapabilityRegistry: fileCapabilityRegistry,
		engine:                 engine,
		storageProvider:        storageProvider,
		verifier:               verifier,
		mutexManager:           mutexManager,
		logger:                 log,
		nowFunc:                time.Now,
		databaseDiscoverer:     sshconn.NewSSHDatabaseDiscoverer(nil),
	}

	return wp
}

// NewWorkerPoolWithKeyProvider constructs a new WorkerPool instance with an explicit KeyProvider.
func NewWorkerPoolWithKeyProvider(
	cfg WorkerPoolConfig,
	repo repository.BackupRepository,
	resFinder ResourceConnectorFinder,
	vault CredentialVault,
	capabilityRegistry *connector.BackupCapabilityRegistry,
	fileCapabilityRegistry *connector.FileBackupCapabilityRegistry,
	engine engine.BackupEngine,
	storageProvider storage.StorageProvider,
	verifier verification.Verifier,
	mutexManager *PerResourceMutexManager,
	log *slog.Logger,
	keyProvider artifactcrypto.KeyProvider,
) *WorkerPool {
	wp := NewWorkerPool(
		cfg,
		repo,
		resFinder,
		vault,
		capabilityRegistry,
		fileCapabilityRegistry,
		engine,
		storageProvider,
		verifier,
		mutexManager,
		log,
	)
	if keyProvider != nil {
		wp.SetKeyProvider(keyProvider)
	}
	return wp
}

// SetStorageResolver configures a dynamic storage provider resolver for S3/Local multi-target resolution.
func (p *WorkerPool) SetStorageResolver(resolver storage.StorageProviderResolver) {
	p.storageResolver = resolver
}

func (p *WorkerPool) resolveStorageProvider(ctx context.Context, orgID, targetID uuid.UUID) (storage.StorageProvider, error) {
	if p.storageResolver != nil {
		if targetID == uuid.Nil {
			return nil, domain.ErrStorageTargetNotFound
		}
		return p.storageResolver.Resolve(ctx, orgID, targetID)
	}
	if p.storageProvider != nil {
		return p.storageProvider, nil
	}
	return nil, errors.New("no storage provider configured in worker pool")
}

// SetRetentionManager injects a custom retention manager into the worker pool.
func (p *WorkerPool) SetRetentionManager(rm RetentionManager) {
	p.retentionManager = rm
}

// SetKeyProvider injects an artifact crypto key provider into the worker pool.
func (p *WorkerPool) SetKeyProvider(kp artifactcrypto.KeyProvider) {
	p.keyProvider = kp
	if eng, ok := p.engine.(interface {
		SetKeyProvider(artifactcrypto.KeyProvider)
	}); ok {
		eng.SetKeyProvider(kp)
	}
	if ver, ok := p.verifier.(interface {
		SetKeyProvider(artifactcrypto.KeyProvider)
	}); ok {
		ver.SetKeyProvider(kp)
	}
}

// SetNowFunc sets the custom time supplier for testing.
func (p *WorkerPool) SetNowFunc(f func() time.Time) {
	if f != nil {
		p.nowFunc = f
	}
}

// Start launches worker goroutines.
func (p *WorkerPool) Start(ctx context.Context) {
	workerCtx, cancel := context.WithCancel(ctx)
	p.cancel = cancel

	for i := 1; i <= p.cfg.NumWorkers; i++ {
		p.wg.Add(1)
		go p.workerLoop(workerCtx, i)
	}

	p.logger.Info("backup worker pool started",
		slog.Int("num_workers", p.cfg.NumWorkers),
		slog.Duration("poll_interval", p.cfg.PollInterval),
		slog.Duration("heartbeat_interval", p.cfg.HeartbeatInterval),
	)
}

// Stop initiates graceful shutdown of the worker pool.
func (p *WorkerPool) Stop(ctx context.Context) error {
	if p.cancel != nil {
		p.cancel()
	}

	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		p.logger.Info("backup worker pool stopped cleanly")
		return nil
	case <-ctx.Done():
		p.logger.Warn("backup worker pool shutdown timed out")
		return ctx.Err()
	}
}

func (p *WorkerPool) workerLoop(ctx context.Context, workerID int) {
	defer p.wg.Done()

	ticker := time.NewTicker(p.cfg.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.processNextAvailableJob(ctx, workerID)
		}
	}
}

func (p *WorkerPool) isRetryEligible(ctx context.Context, job *domain.BackupJob) bool {
	latestRun, err := p.repo.GetLatestRunForJob(ctx, job.OrganizationID, job.ID)
	if err != nil {
		if errors.Is(err, domain.ErrRunNotFound) {
			return true // First attempt is always eligible
		}
		p.logger.Warn("worker failed querying latest run for retry eligibility",
			slog.String("job_id", job.ID.String()),
		)
		return false // Database/infrastructure error -> NOT eligible this cycle
	}

	if latestRun == nil {
		return false
	}

	if latestRun.Status != domain.RunStatusFailed {
		return false // Inconsistent / active run -> NOT eligible
	}

	if latestRun.EndedAt == nil {
		return false
	}

	retryDelay := CalculateRetryDelay(job.ID, latestRun.AttemptNumber)
	return !p.nowFunc().Before(latestRun.EndedAt.Add(retryDelay))
}

func (p *WorkerPool) processNextAvailableJob(ctx context.Context, workerID int) {
	var cursorCreatedAt *time.Time
	var cursorID *uuid.UUID

	for {
		if ctx.Err() != nil {
			return
		}

		pollCtx, pollCancel := context.WithTimeout(ctx, 5*time.Second)
		pendingJobs, err := p.repo.FindPendingJobs(pollCtx, 50, cursorCreatedAt, cursorID)
		pollCancel()

		if err != nil {
			if !errors.Is(err, context.Canceled) {
				p.logger.Error("worker failed querying pending jobs",
					slog.Int("worker_id", workerID),
				)
			}
			return
		}

		if len(pendingJobs) == 0 {
			return
		}

		for _, job := range pendingJobs {
			if ctx.Err() != nil {
				return
			}

			// Check retry backoff eligibility
			checkCtx, checkCancel := context.WithTimeout(ctx, 5*time.Second)
			eligible := p.isRetryEligible(checkCtx, job)
			checkCancel()

			if !eligible {
				continue
			}

			// Try to acquire the in-process per-resource mutex
			unlockResource, ok := p.mutexManager.TryAcquire(job.ResourceID)
			if !ok {
				continue
			}

			// Acquired mutex: attempt transactional claim
			claimCtx, claimCancel := context.WithTimeout(ctx, 5*time.Second)
			run, claimedJob, claimErr := p.repo.TransactionalClaimJob(claimCtx, job.OrganizationID, job.ID)
			claimCancel()

			if claimErr != nil {
				unlockResource()
				if !errors.Is(claimErr, domain.ErrJobNotFound) && !errors.Is(claimErr, context.Canceled) {
					p.logger.Warn("failed claiming pending backup job",
						slog.Int("worker_id", workerID),
						slog.String("job_id", job.ID.String()),
					)
				}
				continue
			}

			// Execute claimed job safely within dedicated execution boundary with guaranteed mutex release
			p.executeJobSafely(ctx, workerID, run, claimedJob, unlockResource)
			return
		}

		lastJob := pendingJobs[len(pendingJobs)-1]
		cursorCreatedAt = &lastJob.CreatedAt
		cursorID = &lastJob.ID
	}
}

func (p *WorkerPool) executeJobSafely(
	ctx context.Context,
	workerID int,
	run *domain.BackupRun,
	job *domain.BackupJob,
	unlockResource func(),
) {
	defer unlockResource()

	p.logger.Info("starting backup job execution",
		slog.Int("worker_id", workerID),
		slog.String("run_id", run.ID.String()),
		slog.String("job_id", job.ID.String()),
		slog.String("resource_id", job.ResourceID.String()),
		slog.String("backup_type", string(job.BackupType)),
	)

	execCtx, execCancel := context.WithCancel(ctx)
	defer execCancel()

	// Start concurrent heartbeat routine using the canonical StartHeartbeat helper
	stopHeartbeat := StartHeartbeat(execCtx, p.repo, run.OrganizationID, run.ID, p.cfg.HeartbeatInterval, execCancel, p.logger)

	var pipelineSucceeded bool

	defer func() {
		if r := recover(); r != nil {
			stopHeartbeat()
			p.logger.Error("backup worker panic recovered",
				slog.Int("worker_id", workerID),
				slog.String("job_id", job.ID.String()),
				slog.String("run_id", run.ID.String()),
			)

			if pipelineSucceeded {
				return
			}

			p.cleanupRunArtifacts(run.OrganizationID, run.ID)

			errMsg := "internal worker panic"
			finalizeCtx, finalizeCancel := context.WithTimeout(context.Background(), 5*time.Second)
			if finErr := p.repo.FinalizeRunAndJob(finalizeCtx, run.OrganizationID, run.ID, job.ID, domain.RunStatusFailed, domain.JobStatusFailed, &errMsg, nil); finErr != nil {
				p.logger.Warn("failed finalizing panicked backup run in database",
					slog.String("run_id", run.ID.String()),
				)
			}
			finalizeCancel()
		}
	}()

	p.logger.Info("starting backup run heartbeat",
		slog.String("run_id", run.ID.String()),
		slog.String("job_id", job.ID.String()),
		slog.Int("attempt", run.AttemptNumber),
	)

	execErr := p.executeBackupPipeline(execCtx, run, job)

	// Invariant: Stop heartbeat before final DB transition
	stopHeartbeat()

	if execErr == nil {
		finalizeCtx, finalizeCancel := context.WithTimeout(context.Background(), 5*time.Second)
		finErr := p.repo.FinalizeRunAndJob(finalizeCtx, run.OrganizationID, run.ID, job.ID, domain.RunStatusSuccess, domain.JobStatusCompleted, nil, nil)
		finalizeCancel()

		if finErr != nil {
			if errors.Is(finErr, domain.ErrRunNoLongerActive) {
				p.logger.Warn("backup run ownership lost to reaper or recovery",
					slog.String("run_id", run.ID.String()),
				)
			} else {
				p.logger.Error("failed finalizing successful backup run in database",
					slog.String("run_id", run.ID.String()),
				)
			}
			p.cleanupRunArtifacts(run.OrganizationID, run.ID)
		} else {
			pipelineSucceeded = true
			p.logger.Info("backup run completed successfully",
				slog.Int("worker_id", workerID),
				slog.String("run_id", run.ID.String()),
				slog.String("job_id", job.ID.String()),
			)

			// Post-Success Lifecycle: Apply Retention Policy if job is associated with a plan
			if p.retentionManager != nil && job.BackupPlanID != nil {
				retentionCtx, retentionCancel := context.WithTimeout(context.Background(), 30*time.Second)
				if _, retErr := p.retentionManager.ApplyAfterSuccessfulRun(retentionCtx, run.OrganizationID, job.BackupPlanID, run.ID); retErr != nil {
					p.logger.Warn("retention policy execution completed with errors",
						slog.String("org_id", run.OrganizationID.String()),
						slog.String("plan_id", job.BackupPlanID.String()),
						slog.String("run_id", run.ID.String()),
					)
				}
				retentionCancel()
			}
		}
		return
	}

	// Error path
	p.logger.Warn("backup run execution failed",
		slog.Int("worker_id", workerID),
		slog.String("run_id", run.ID.String()),
		slog.String("job_id", job.ID.String()),
	)
	safeMsg := SafeErrorMessage(execErr)

	nextJobStatus := domain.JobStatusFailed
	if IsRetryable(execErr) && run.AttemptNumber < 3 {
		nextJobStatus = domain.JobStatusPending
		p.logger.Info("backup job scheduled for retry",
			slog.String("job_id", job.ID.String()),
			slog.Int("attempt", run.AttemptNumber),
		)
	}

	finalizeCtx, finalizeCancel := context.WithTimeout(context.Background(), 5*time.Second)
	finErr := p.repo.FinalizeRunAndJob(finalizeCtx, run.OrganizationID, run.ID, job.ID, domain.RunStatusFailed, nextJobStatus, &safeMsg, nil)
	finalizeCancel()
	if finErr != nil {
		p.logger.Warn("failed finalizing failed backup run in database",
			slog.String("run_id", run.ID.String()),
		)
	}

	// Clean up artifacts if failure occurred (do NOT delete/tombstone if failure is key infrastructure unavailability)
	if !errors.Is(execErr, artifactcrypto.ErrUnknownKeyVersion) && !errors.Is(execErr, artifactcrypto.ErrInvalidKeyVersion) {
		p.cleanupRunArtifacts(run.OrganizationID, run.ID)
	}
}

func (p *WorkerPool) cleanupArtifact(ctx context.Context, orgID, artID, targetID uuid.UUID, storageRef string) error {
	store, err := p.resolveStorageProvider(ctx, orgID, targetID)
	if err != nil {
		p.logger.Warn("failed resolving storage provider during cleanup", slog.String("target_id", targetID.String()))
		return err
	}
	delErr := store.DeleteArtifact(ctx, storageRef)
	if delErr != nil && !errors.Is(delErr, storage.ErrArtifactNotFound) {
		p.logger.Warn("failed to delete artifact physical file during cleanup",
			slog.String("artifact_id", artID.String()),
		)
		return delErr
	}

	if tbErr := p.repo.TombstoneArtifact(ctx, orgID, artID); tbErr != nil {
		p.logger.Warn("failed tombstoning cleaned up artifact in database",
			slog.String("artifact_id", artID.String()),
		)
		return tbErr
	}
	return nil
}

func (p *WorkerPool) cleanupUnpersistedArtifact(ctx context.Context, store storage.StorageProvider, storageRef string) {
	if store == nil {
		store = p.storageProvider
	}
	if store == nil {
		return
	}
	if delErr := store.DeleteArtifact(ctx, storageRef); delErr != nil && !errors.Is(delErr, storage.ErrArtifactNotFound) {
		p.logger.Warn("failed to delete unpersisted artifact physical file")
	}
}

func (p *WorkerPool) cleanupRunArtifacts(orgID, runID uuid.UUID) {
	cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cleanupCancel()

	artifacts, err := p.repo.GetRunArtifacts(cleanupCtx, orgID, runID)
	if err != nil {
		p.logger.Warn("failed querying artifacts for cleanup",
			slog.String("run_id", runID.String()),
		)
		return
	}

	for _, art := range artifacts {
		if art == nil || art.IsDeleted {
			continue
		}
		if err := p.cleanupArtifact(cleanupCtx, orgID, art.ID, art.StorageTargetID, art.StorageReference); err != nil {
			p.logger.Warn("failed cleaning up artifact for run",
				slog.String("run_id", runID.String()),
				slog.String("artifact_id", art.ID.String()),
			)
		}
	}
}

func (p *WorkerPool) executeBackupPipeline(
	ctx context.Context,
	run *domain.BackupRun,
	job *domain.BackupJob,
) error {
	// 1. Validate EngineType (ADR-033: only direct_stream supported in Step A.1; fail-closed on blank or unsupported)
	if job.EngineType == "" {
		return domain.ErrInvalidEngineType
	}
	if job.EngineType != domain.EngineTypeDirectStream {
		return domain.ErrUnsupportedEngineType
	}

	// 2. Validate and Normalize Job Target Specification
	if job.BackupType != domain.BackupTypeMySQLDatabase && job.BackupType != domain.BackupTypeWebsiteFiles {
		return domain.ErrUnsupportedBackupType
	}
	normalizedSpec, err := domain.NormalizeTargetSpec(job.BackupType, &job.TargetSpec)
	if err != nil {
		return domain.ErrInvalidTargetSpec
	}
	job.TargetSpec = *normalizedSpec

	// 3. Fetch Resource and Connector Configuration
	resWithConn, err := p.resFinder.FindByIDForOrganization(ctx, job.OrganizationID, job.ResourceID)
	if err != nil {
		return err
	}
	if resWithConn.Resource.Status == resDomain.StatusDisabled {
		return domain.ErrResourceDisabled
	}
	if resWithConn.Resource.Status == resDomain.StatusArchived {
		return domain.ErrResourceArchived
	}
	if resWithConn.Resource.Type != resDomain.TypeUbuntuSSH {
		return domain.ErrUnsupportedResourceType
	}
	if resWithConn.Connector == nil {
		return domain.ErrInvalidTargetSpec
	}

	// 4. Fetch and Validate Storage Target (Immutable Job Snapshot; fail-closed on missing target)
	if job.StorageTargetID == uuid.Nil {
		return domain.ErrStorageTargetNotFound
	}
	storageTarget, err := p.repo.GetStorageTargetByID(ctx, job.OrganizationID, job.StorageTargetID)
	if err != nil {
		return err
	}
	if storageTarget.Status != domain.StorageTargetStatusActive {
		return domain.ErrStorageTargetNotActive
	}
	if !domain.IsEngineCompatibleWithStorage(job.EngineType, storageTarget.Type) {
		return domain.ErrIncompatibleEngineStorage
	}

	// 5. Resolve StorageProvider for target
	targetStorageProvider, err := p.resolveStorageProvider(ctx, job.OrganizationID, storageTarget.ID)
	if err != nil {
		return err
	}

	// 6. Branch by BackupType
	switch job.BackupType {
	case domain.BackupTypeMySQLDatabase:
		cap, ok := p.capabilityRegistry.Get(resWithConn.Resource.Type)
		if !ok || cap == nil {
			return domain.ErrUnsupportedResourceType
		}

		dbNames, err := p.resolveMySQLDatabases(ctx, job, resWithConn)
		if err != nil {
			return err
		}

		for _, dbName := range dbNames {
			if ctx.Err() != nil {
				return ctx.Err()
			}

			if err := p.executeDatabaseTarget(ctx, run, job, resWithConn, storageTarget, targetStorageProvider, cap, dbName); err != nil {
				return err
			}
		}
		return nil

	case domain.BackupTypeWebsiteFiles:
		fileCap, ok := p.fileCapabilityRegistry.Get(resWithConn.Resource.Type)
		if !ok || fileCap == nil {
			return domain.ErrUnsupportedResourceType
		}

		excludes := job.TargetSpec.GetExcludePatterns()
		for _, sourcePath := range job.TargetSpec.Paths {
			if ctx.Err() != nil {
				return ctx.Err()
			}

			if err := p.executeFileTarget(ctx, run, job, resWithConn, storageTarget, targetStorageProvider, fileCap, sourcePath, excludes); err != nil {
				return err
			}
		}
		return nil

	default:
		return domain.ErrUnsupportedBackupType
	}
}

func (p *WorkerPool) executeDatabaseTarget(
	ctx context.Context,
	run *domain.BackupRun,
	job *domain.BackupJob,
	resWithConn *resDomain.ResourceWithConnector,
	storageTarget *domain.StorageTarget,
	targetStorageProvider storage.StorageProvider,
	cap connector.DatabaseBackupCapability,
	dbName string,
) error {
	// Decrypt credential payload strictly scoped to this database dump
	credType, plaintextBytes, credErr := p.vault.LoadCredentialForUse(ctx, job.OrganizationID, resWithConn.Connector.CredentialID)
	if credErr != nil {
		return credErr
	}

	// Validate AuthType vs Stored Credential Type compatibility
	if (resWithConn.Connector.AuthType == resDomain.AuthTypeSSHKey && credType != credDomain.TypeSSHPrivateKey) ||
		(resWithConn.Connector.AuthType == resDomain.AuthTypeSSHPassword && credType != credDomain.TypeSSHPassword) {
		secretcrypto.ZeroBytes(plaintextBytes)
		return connector.ErrInvalidCredentialFormat
	}

	credPayload, decodeErr := payload.Decode(plaintextBytes)
	secretcrypto.ZeroBytes(plaintextBytes) // Zero plaintext bytes immediately
	if decodeErr != nil {
		return connector.ErrInvalidCredentialFormat
	}
	defer payload.Clear(credPayload)

	target := connector.Target{
		Host:               resWithConn.Connector.Host,
		Port:               resWithConn.Connector.Port,
		Username:           resWithConn.Connector.Config.Username,
		AuthType:           resWithConn.Connector.AuthType,
		HostKeyFingerprint: resWithConn.Connector.HostKeyFingerprint,
		ConnectionTimeout:  resWithConn.Connector.Config.ConnectionTimeoutSeconds,
	}

	artifactID := uuid.New()

	// Stream dump + gzip directly into storage
	saveRes, streamErr := p.engine.ExecuteDatabaseBackup(
		ctx,
		cap,
		target,
		credPayload,
		dbName,
		targetStorageProvider,
		job.OrganizationID,
		job.ResourceID,
		run.ID,
		artifactID,
	)
	if streamErr != nil {
		return streamErr
	}

	// Insert initial unverified artifact record
	storedSize := saveRes.StoredSizeBytes
	engineMeta, metaErr := json.Marshal(map[string]string{
		"ciphertext_sha256": saveRes.CiphertextSHA256,
	})
	if metaErr != nil {
		return fmt.Errorf("failed marshaling engine metadata: %w", metaErr)
	}

	artRecord := &domain.BackupArtifact{
		ID:                 artifactID,
		OrganizationID:     job.OrganizationID,
		RunID:              run.ID,
		ResourceID:         job.ResourceID,
		StorageTargetID:    storageTarget.ID,
		ArtifactType:       domain.ArtifactTypeDatabaseDump,
		Format:             domain.ArtifactFormatSQLGzip,
		TargetName:         dbName,
		StorageReference:   saveRes.StorageReference,
		SizeBytes:          saveRes.PlaintextSizeBytes,
		ChecksumAlgorithm:  domain.ChecksumAlgorithmSHA256,
		ChecksumHash:       saveRes.PlaintextChecksumSHA256,
		StoredSizeBytes:    &storedSize,
		EngineMetadata:     engineMeta,
		VerificationStatus: domain.VerificationStatusUnverified,
	}

	_, createArtErr := p.repo.CreateArtifact(ctx, artRecord)
	if createArtErr != nil {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		p.cleanupUnpersistedArtifact(cleanupCtx, targetStorageProvider, saveRes.StorageReference)
		cleanupCancel()
		return createArtErr
	}

	// Verification Phase: verify physical object + BPAE decryption + plaintext gzip
	verDetails, verErr := p.verifier.VerifyEncryptedDatabaseArtifact(
		ctx,
		targetStorageProvider,
		saveRes.StorageReference,
		saveRes.PlaintextSizeBytes,
		saveRes.PlaintextChecksumSHA256,
		saveRes.StoredSizeBytes,
		saveRes.CiphertextSHA256,
		job.OrganizationID,
		artifactID,
	)
	if verErr != nil {
		if errors.Is(verErr, context.Canceled) || errors.Is(verErr, context.DeadlineExceeded) {
			return verErr
		}
		// Artifact key availability is infrastructure error, NOT evidence of corruption.
		if errors.Is(verErr, artifactcrypto.ErrUnknownKeyVersion) || errors.Is(verErr, artifactcrypto.ErrInvalidKeyVersion) {
			p.logger.Error("artifact key infrastructure error during post-backup verification",
				slog.String("run_id", run.ID.String()),
				slog.String("artifact_id", artifactID.String()),
				slog.String("error", verErr.Error()),
			)
			// Do NOT persist verification_status=failed. Fail the execution safely.
			return fmt.Errorf("artifact encryption key infrastructure error: %w", verErr)
		}
		failMsg := "backup verification failed"
		if updateErr := p.repo.UpdateArtifactVerification(ctx, job.OrganizationID, artifactID, domain.VerificationStatusFailed, &failMsg); updateErr != nil {
			return updateErr
		}
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		if clErr := p.cleanupArtifact(cleanupCtx, job.OrganizationID, artifactID, storageTarget.ID, saveRes.StorageReference); clErr != nil {
			p.logger.Warn("failed cleaning up artifact after verification failure",
				slog.String("run_id", run.ID.String()),
				slog.String("artifact_id", artifactID.String()),
			)
		}
		cleanupCancel()
		return domain.ErrVerificationFailed
	}

	// Mark verified
	if updateErr := p.repo.UpdateArtifactVerification(ctx, job.OrganizationID, artifactID, domain.VerificationStatusVerified, &verDetails); updateErr != nil {
		return updateErr
	}

	return nil
}

func (p *WorkerPool) executeFileTarget(
	ctx context.Context,
	run *domain.BackupRun,
	job *domain.BackupJob,
	resWithConn *resDomain.ResourceWithConnector,
	storageTarget *domain.StorageTarget,
	targetStorageProvider storage.StorageProvider,
	fileCap connector.FileBackupCapability,
	sourcePath string,
	excludePatterns []string,
) error {
	// Decrypt credential payload strictly scoped to this file path extraction
	credType, plaintextBytes, credErr := p.vault.LoadCredentialForUse(ctx, job.OrganizationID, resWithConn.Connector.CredentialID)
	if credErr != nil {
		return credErr
	}

	// Validate AuthType vs Stored Credential Type compatibility
	if (resWithConn.Connector.AuthType == resDomain.AuthTypeSSHKey && credType != credDomain.TypeSSHPrivateKey) ||
		(resWithConn.Connector.AuthType == resDomain.AuthTypeSSHPassword && credType != credDomain.TypeSSHPassword) {
		secretcrypto.ZeroBytes(plaintextBytes)
		return connector.ErrInvalidCredentialFormat
	}

	credPayload, decodeErr := payload.Decode(plaintextBytes)
	secretcrypto.ZeroBytes(plaintextBytes) // Zero plaintext bytes immediately
	if decodeErr != nil {
		return connector.ErrInvalidCredentialFormat
	}
	defer payload.Clear(credPayload)

	target := connector.Target{
		Host:               resWithConn.Connector.Host,
		Port:               resWithConn.Connector.Port,
		Username:           resWithConn.Connector.Config.Username,
		AuthType:           resWithConn.Connector.AuthType,
		HostKeyFingerprint: resWithConn.Connector.HostKeyFingerprint,
		ConnectionTimeout:  resWithConn.Connector.Config.ConnectionTimeoutSeconds,
	}

	config := connector.FileBackupConfig{
		SourcePath:      sourcePath,
		ExcludePatterns: excludePatterns,
	}

	artifactID := uuid.New()

	// Stream raw tar from remote + gzip compress directly into storage
	saveRes, streamErr := p.engine.ExecuteFilesBackup(
		ctx,
		fileCap,
		target,
		credPayload,
		config,
		targetStorageProvider,
		job.OrganizationID,
		job.ResourceID,
		run.ID,
		artifactID,
	)
	if streamErr != nil {
		return streamErr
	}

	// Insert initial unverified artifact record
	storedSize := saveRes.StoredSizeBytes
	engineMeta, metaErr := json.Marshal(map[string]string{
		"ciphertext_sha256": saveRes.CiphertextSHA256,
	})
	if metaErr != nil {
		return fmt.Errorf("failed marshaling engine metadata: %w", metaErr)
	}

	artRecord := &domain.BackupArtifact{
		ID:                 artifactID,
		OrganizationID:     job.OrganizationID,
		RunID:              run.ID,
		ResourceID:         job.ResourceID,
		StorageTargetID:    storageTarget.ID,
		ArtifactType:       domain.ArtifactTypeFilesArchive,
		Format:             domain.ArtifactFormatTarGzip,
		TargetName:         sourcePath,
		StorageReference:   saveRes.StorageReference,
		SizeBytes:          saveRes.PlaintextSizeBytes,
		ChecksumAlgorithm:  domain.ChecksumAlgorithmSHA256,
		ChecksumHash:       saveRes.PlaintextChecksumSHA256,
		StoredSizeBytes:    &storedSize,
		EngineMetadata:     engineMeta,
		VerificationStatus: domain.VerificationStatusUnverified,
	}

	_, createArtErr := p.repo.CreateArtifact(ctx, artRecord)
	if createArtErr != nil {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		p.cleanupUnpersistedArtifact(cleanupCtx, targetStorageProvider, saveRes.StorageReference)
		cleanupCancel()
		return createArtErr
	}

	// Verification Phase: verify physical object + BPAE decryption + plaintext gzip
	verDetails, verErr := p.verifier.VerifyEncryptedFilesArtifact(
		ctx,
		targetStorageProvider,
		saveRes.StorageReference,
		saveRes.PlaintextSizeBytes,
		saveRes.PlaintextChecksumSHA256,
		saveRes.StoredSizeBytes,
		saveRes.CiphertextSHA256,
		job.OrganizationID,
		artifactID,
	)
	if verErr != nil {
		if errors.Is(verErr, context.Canceled) || errors.Is(verErr, context.DeadlineExceeded) {
			return verErr
		}
		// Artifact key availability is infrastructure error, NOT evidence of corruption.
		if errors.Is(verErr, artifactcrypto.ErrUnknownKeyVersion) || errors.Is(verErr, artifactcrypto.ErrInvalidKeyVersion) {
			p.logger.Error("artifact key infrastructure error during post-backup verification",
				slog.String("run_id", run.ID.String()),
				slog.String("artifact_id", artifactID.String()),
				slog.String("error", verErr.Error()),
			)
			// Do NOT persist verification_status=failed. Fail the execution safely.
			return fmt.Errorf("artifact encryption key infrastructure error: %w", verErr)
		}
		failMsg := "backup verification failed"
		if updateErr := p.repo.UpdateArtifactVerification(ctx, job.OrganizationID, artifactID, domain.VerificationStatusFailed, &failMsg); updateErr != nil {
			return updateErr
		}
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		if clErr := p.cleanupArtifact(cleanupCtx, job.OrganizationID, artifactID, storageTarget.ID, saveRes.StorageReference); clErr != nil {
			p.logger.Warn("failed cleaning up artifact after verification failure",
				slog.String("run_id", run.ID.String()),
				slog.String("artifact_id", artifactID.String()),
			)
		}
		cleanupCancel()
		return domain.ErrVerificationFailed
	}

	// Mark verified
	if updateErr := p.repo.UpdateArtifactVerification(ctx, job.OrganizationID, artifactID, domain.VerificationStatusVerified, &verDetails); updateErr != nil {
		return updateErr
	}

	return nil
}

func (p *WorkerPool) resolveMySQLDatabases(
	ctx context.Context,
	job *domain.BackupJob,
	resWithConn *resDomain.ResourceWithConnector,
) ([]string, error) {
	if len(job.TargetSpec.Databases) > 0 {
		return job.TargetSpec.Databases, nil
	}

	// Mode "all": discover non-system databases dynamically via SSH
	if resWithConn.Resource.Type != resDomain.TypeUbuntuSSH {
		return nil, domain.ErrUnsupportedResourceType
	}

	credType, plaintextBytes, credErr := p.vault.LoadCredentialForUse(ctx, job.OrganizationID, resWithConn.Connector.CredentialID)
	if credErr != nil {
		return nil, credErr
	}

	if (resWithConn.Connector.AuthType == resDomain.AuthTypeSSHKey && credType != credDomain.TypeSSHPrivateKey) ||
		(resWithConn.Connector.AuthType == resDomain.AuthTypeSSHPassword && credType != credDomain.TypeSSHPassword) {
		secretcrypto.ZeroBytes(plaintextBytes)
		return nil, connector.ErrInvalidCredentialFormat
	}

	credPayload, decodeErr := payload.Decode(plaintextBytes)
	secretcrypto.ZeroBytes(plaintextBytes)
	if decodeErr != nil {
		return nil, connector.ErrInvalidCredentialFormat
	}
	defer payload.Clear(credPayload)

	target := connector.Target{
		Host:               resWithConn.Connector.Host,
		Port:               resWithConn.Connector.Port,
		Username:           resWithConn.Connector.Config.Username,
		AuthType:           resWithConn.Connector.AuthType,
		HostKeyFingerprint: resWithConn.Connector.HostKeyFingerprint,
		ConnectionTimeout:  resWithConn.Connector.Config.ConnectionTimeoutSeconds,
	}

	discoverer := p.databaseDiscoverer
	if discoverer == nil {
		discoverer = sshconn.NewSSHDatabaseDiscoverer(nil)
	}
	discovered, discErr := discoverer.DiscoverDatabases(ctx, target, credPayload)
	if discErr != nil {
		return nil, discErr
	}

	dbNames := make([]string, 0, len(discovered))
	for _, db := range discovered {
		dbNames = append(dbNames, db.Name)
	}

	return dbNames, nil
}
