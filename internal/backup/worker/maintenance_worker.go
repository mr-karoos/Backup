package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	auditDomain "backup-platform/internal/audit/domain"
	"backup-platform/internal/backup/domain"
	"backup-platform/internal/backup/repository"
	"backup-platform/internal/backup/restic"
	"backup-platform/internal/backup/service"
	credDomain "backup-platform/internal/credential/domain"
	"backup-platform/internal/credential/secretcrypto"
	"backup-platform/pkg/uuid"
)

// AuditRecorder defines the audit log emission interface.
type AuditRecorder interface {
	Record(ctx context.Context, entry *auditDomain.AuditLog) error
}

// MaintenanceWorkerConfig holds configuration parameters for RepositoryMaintenanceWorker.
type MaintenanceWorkerConfig struct {
	PollInterval       time.Duration
	LeaseDuration      time.Duration
	HeartbeatInterval  time.Duration
	CoordinatorTimeout time.Duration
}

// DefaultMaintenanceWorkerConfig returns safe production defaults.
func DefaultMaintenanceWorkerConfig() MaintenanceWorkerConfig {
	return MaintenanceWorkerConfig{
		PollInterval:       3 * time.Second,
		LeaseDuration:      2 * time.Minute,
		HeartbeatInterval:  20 * time.Second,
		CoordinatorTimeout: 3 * time.Second,
	}
}

// RepositoryMaintenanceWorker orchestrates durable repository maintenance jobs (forget, prune, deep check).
type RepositoryMaintenanceWorker struct {
	maintRepo      repository.MaintenanceRepository
	backupRepo     repository.BackupRepository
	storageRepo    repository.StorageTargetRepository
	vault          CredentialVault
	targetResolver service.RepositoryTargetResolver
	resticRunner   restic.CommandRunner
	coordinator    restic.RepositoryOperationCoordinator
	auditRecorder  AuditRecorder
	logger         *slog.Logger
	cfg            MaintenanceWorkerConfig
	cancel         context.CancelFunc
	wg             sync.WaitGroup
	nowFunc        func() time.Time
}

// NewRepositoryMaintenanceWorker constructs a new RepositoryMaintenanceWorker.
func NewRepositoryMaintenanceWorker(
	maintRepo repository.MaintenanceRepository,
	backupRepo repository.BackupRepository,
	storageRepo repository.StorageTargetRepository,
	vault CredentialVault,
	targetResolver service.RepositoryTargetResolver,
	resticRunner restic.CommandRunner,
	coordinator restic.RepositoryOperationCoordinator,
	auditRecorder AuditRecorder,
	cfg MaintenanceWorkerConfig,
	logger *slog.Logger,
) *RepositoryMaintenanceWorker {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 3 * time.Second
	}
	if cfg.LeaseDuration <= 0 {
		cfg.LeaseDuration = 2 * time.Minute
	}
	if cfg.HeartbeatInterval <= 0 {
		cfg.HeartbeatInterval = 20 * time.Second
	}
	if cfg.CoordinatorTimeout <= 0 {
		cfg.CoordinatorTimeout = 3 * time.Second
	}

	return &RepositoryMaintenanceWorker{
		maintRepo:      maintRepo,
		backupRepo:     backupRepo,
		storageRepo:    storageRepo,
		vault:          vault,
		targetResolver: targetResolver,
		resticRunner:   resticRunner,
		coordinator:    coordinator,
		auditRecorder:  auditRecorder,
		cfg:            cfg,
		logger:         logger,
		nowFunc:        time.Now,
	}
}

// SetNowFunc injects a custom clock for testing.
func (w *RepositoryMaintenanceWorker) SetNowFunc(f func() time.Time) {
	if f != nil {
		w.nowFunc = f
	}
}

// Start launches the background worker loop.
func (w *RepositoryMaintenanceWorker) Start(parentCtx context.Context) {
	ctx, cancel := context.WithCancel(parentCtx)
	w.cancel = cancel

	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		w.runLoop(ctx)
	}()
	w.logger.Info("repository maintenance worker started")
}

// Stop gracefully stops the worker and waits for active jobs to complete.
func (w *RepositoryMaintenanceWorker) Stop() {
	if w.cancel != nil {
		w.cancel()
	}
	w.wg.Wait()
	w.logger.Info("repository maintenance worker stopped")
}

func (w *RepositoryMaintenanceWorker) runLoop(ctx context.Context) {
	ticker := time.NewTicker(w.cfg.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.ProcessNextJob(ctx)
		}
	}
}

// ProcessNextJob attempts to claim and execute one maintenance job.
// Returns true if a job was claimed and executed, false otherwise.
func (w *RepositoryMaintenanceWorker) ProcessNextJob(ctx context.Context) bool {
	job, run, err := w.maintRepo.ClaimNextMaintenanceJob(ctx, w.cfg.LeaseDuration)
	if err != nil {
		w.logger.Warn("failed claiming maintenance job", slog.String("error", err.Error()))
		return false
	}
	if job == nil || run == nil {
		return false
	}

	w.logger.Info("claimed maintenance job",
		slog.String("job_id", job.ID.String()),
		slog.String("repo_id", job.RepositoryID.String()),
		slog.String("op", string(job.OperationType)),
		slog.Int("attempt", run.AttemptNumber),
	)

	w.executeJob(ctx, job, run)
	return true
}

func (w *RepositoryMaintenanceWorker) executeJob(ctx context.Context, job *domain.MaintenanceJob, run *domain.MaintenanceRun) {
	// 1. Fetch Repository Entity
	repo, err := w.backupRepo.GetRepositoryByID(ctx, job.OrganizationID, job.RepositoryID)
	if err != nil {
		errMsg := fmt.Sprintf("repository not found: %v", err)
		w.logger.Error("maintenance failed: repository not found", slog.String("job_id", job.ID.String()), slog.String("error", errMsg))
		_ = w.maintRepo.FailMaintenanceJob(ctx, job.ID, run.ID, errMsg, false)
		return
	}

	// 2. Acquire Exclusive Lock with bounded timeout (Yield if busy to avoid starving customer operations)
	coordCtx, coordCancel := context.WithTimeout(ctx, w.cfg.CoordinatorTimeout)
	unlock, err := w.coordinator.AcquireExclusive(coordCtx, job.RepositoryID)
	coordCancel()
	if err != nil {
		w.logger.Info("repository is busy with active operations, yielding maintenance job",
			slog.String("job_id", job.ID.String()),
			slog.String("repo_id", job.RepositoryID.String()),
		)
		_ = w.maintRepo.FailMaintenanceJob(ctx, job.ID, run.ID, "repository busy with concurrent operations, yielding for retry", true)
		return
	}
	defer unlock()

	// 3. Resolve Concrete Target
	storageTarget, err := w.storageRepo.GetStorageTargetByID(ctx, job.OrganizationID, repo.StorageTargetID)
	if err != nil {
		errMsg := fmt.Sprintf("storage target not found: %v", err)
		w.logger.Error("maintenance failed: storage target not found", slog.String("job_id", job.ID.String()), slog.String("error", errMsg))
		_ = w.maintRepo.FailMaintenanceJob(ctx, job.ID, run.ID, errMsg, false)
		return
	}

	repoTarget, err := w.targetResolver.ResolveTarget(ctx, job.OrganizationID, repo.ResourceID, storageTarget)
	if err != nil {
		errMsg := fmt.Sprintf("failed resolving repository target: %v", err)
		w.logger.Error("maintenance failed: target resolution failed", slog.String("job_id", job.ID.String()), slog.String("error", errMsg))
		_ = w.maintRepo.FailMaintenanceJob(ctx, job.ID, run.ID, errMsg, false)
		return
	}
	defer repoTarget.Cleanup()

	// 4. Load Repository Key
	credType, repoKey, err := w.vault.LoadCredentialForUse(ctx, job.OrganizationID, repo.CredentialID)
	if err != nil {
		errMsg := fmt.Sprintf("failed loading repository key: %v", err)
		w.logger.Error("maintenance failed: key load failed", slog.String("job_id", job.ID.String()), slog.String("error", errMsg))
		_ = w.maintRepo.FailMaintenanceJob(ctx, job.ID, run.ID, errMsg, false)
		return
	}
	defer secretcrypto.ZeroBytes(repoKey)

	if credType != credDomain.TypeResticRepositoryKey || len(repoKey) == 0 {
		errMsg := "invalid restic repository key format"
		w.logger.Error("maintenance failed: invalid repository key", slog.String("job_id", job.ID.String()))
		_ = w.maintRepo.FailMaintenanceJob(ctx, job.ID, run.ID, errMsg, false)
		return
	}

	// 5. Start Lease Heartbeat Goroutine
	hbCtx, hbCancel := context.WithCancel(ctx)
	defer hbCancel()
	go func() {
		ticker := time.NewTicker(w.cfg.HeartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-hbCtx.Done():
				return
			case <-ticker.C:
				_ = w.maintRepo.HeartbeatMaintenanceRun(ctx, run.ID, w.cfg.LeaseDuration)
			}
		}
	}()

	// 6. Execute Subprocess by Operation Type
	switch job.OperationType {
	case domain.MaintenanceOpResticForget:
		w.handleForget(ctx, job, run, repoTarget, repoKey)

	case domain.MaintenanceOpResticPrune:
		w.handlePrune(ctx, job, run, repoTarget, repoKey)

	case domain.MaintenanceOpResticDeepCheck:
		w.handleDeepCheck(ctx, job, run, repoTarget, repoKey)

	default:
		errMsg := fmt.Sprintf("unsupported operation type: %s", job.OperationType)
		w.logger.Error("maintenance failed", slog.String("error", errMsg))
		_ = w.maintRepo.FailMaintenanceJob(ctx, job.ID, run.ID, errMsg, false)
	}
}

func (w *RepositoryMaintenanceWorker) handleForget(
	ctx context.Context,
	job *domain.MaintenanceJob,
	run *domain.MaintenanceRun,
	repoTarget restic.RepositoryTarget,
	repoKey []byte,
) {
	if job.ArtifactID == nil || *job.ArtifactID == uuid.Nil || job.SnapshotID == "" {
		errMsg := "restic_forget missing artifact_id or snapshot_id"
		_ = w.maintRepo.FailMaintenanceJob(ctx, job.ID, run.ID, errMsg, false)
		return
	}

	// Execute Restic Forget (NO prune inline!)
	err := w.resticRunner.ForgetSnapshot(ctx, repoTarget, repoKey, job.SnapshotID)
	if err != nil {
		w.logger.Error("restic forget failed",
			slog.String("job_id", job.ID.String()),
			slog.String("snapshot_id", job.SnapshotID),
			slog.String("error", err.Error()),
		)
		_ = w.maintRepo.FailMaintenanceJob(ctx, job.ID, run.ID, err.Error(), isRetryable(err))
		return
	}

	// Database Tombstone: is_deleted = true, deleted_at = NOW()
	tbErr := w.backupRepo.TombstoneArtifact(ctx, job.OrganizationID, *job.ArtifactID)
	if tbErr != nil {
		w.logger.Warn("failed tombstoning artifact after restic forget",
			slog.String("artifact_id", job.ArtifactID.String()),
			slog.String("error", tbErr.Error()),
		)
	}

	// Audit Log Emission
	if w.auditRecorder != nil {
		metaObj := map[string]any{
			"artifact_id": job.ArtifactID.String(),
			"snapshot_id": job.SnapshotID,
			"repo_id":     job.RepositoryID.String(),
			"action":      "retention.restic_forget",
		}
		metaBytes, _ := json.Marshal(metaObj)
		auditLog := &auditDomain.AuditLog{
			ID:             uuid.New(),
			OrganizationID: &job.OrganizationID,
			Action:         auditDomain.ActionRetentionCleanup,
			EntityType:     auditDomain.EntityTypeBackupArtifact,
			EntityID:       job.ArtifactID,
			Metadata:       metaBytes,
			CreatedAt:      w.nowFunc(),
		}
		if auditErr := w.auditRecorder.Record(ctx, auditLog); auditErr != nil {
			w.logger.Warn("failed recording retention cleanup audit log", slog.String("error", auditErr.Error()))
		}
	}

	// Debounced Prune Enqueue
	_, _ = w.maintRepo.EnqueueMaintenanceJob(ctx, domain.EnqueueMaintenanceJobParams{
		OrganizationID: job.OrganizationID,
		RepositoryID:   job.RepositoryID,
		OperationType:  domain.MaintenanceOpResticPrune,
		Metadata: map[string]interface{}{
			"source":                "post_forget_cleanup",
			"forgotten_snapshot_id": job.SnapshotID,
		},
	})

	logsSummary, _ := json.Marshal(map[string]string{
		"operation":   "restic_forget",
		"snapshot_id": job.SnapshotID,
		"status":      "success",
	})
	_ = w.maintRepo.CompleteMaintenanceJob(ctx, job.ID, run.ID, logsSummary)
	w.logger.Info("restic forget completed successfully",
		slog.String("job_id", job.ID.String()),
		slog.String("snapshot_id", job.SnapshotID),
	)
}

func (w *RepositoryMaintenanceWorker) handlePrune(
	ctx context.Context,
	job *domain.MaintenanceJob,
	run *domain.MaintenanceRun,
	repoTarget restic.RepositoryTarget,
	repoKey []byte,
) {
	err := w.resticRunner.Prune(ctx, repoTarget, repoKey)
	if err != nil {
		w.logger.Error("restic prune failed",
			slog.String("job_id", job.ID.String()),
			slog.String("error", err.Error()),
		)
		_ = w.maintRepo.FailMaintenanceJob(ctx, job.ID, run.ID, err.Error(), isRetryable(err))
		return
	}

	logsSummary, _ := json.Marshal(map[string]string{
		"operation": "restic_prune",
		"status":    "success",
	})
	_ = w.maintRepo.CompleteMaintenanceJob(ctx, job.ID, run.ID, logsSummary)
	w.logger.Info("restic prune completed successfully", slog.String("job_id", job.ID.String()))
}

func (w *RepositoryMaintenanceWorker) handleDeepCheck(
	ctx context.Context,
	job *domain.MaintenanceJob,
	run *domain.MaintenanceRun,
	repoTarget restic.RepositoryTarget,
	repoKey []byte,
) {
	if job.SubsetIndex == nil || job.SubsetTotal == nil {
		errMsg := "restic_deep_check missing subset_index or subset_total"
		_ = w.maintRepo.FailMaintenanceJob(ctx, job.ID, run.ID, errMsg, false)
		return
	}

	err := w.resticRunner.CheckSubset(ctx, repoTarget, repoKey, *job.SubsetIndex, *job.SubsetTotal)
	if err != nil {
		w.logger.Error("restic deep check failed",
			slog.String("job_id", job.ID.String()),
			slog.Int("subset_index", *job.SubsetIndex),
			slog.Int("subset_total", *job.SubsetTotal),
			slog.String("error", err.Error()),
		)
		_ = w.maintRepo.FailMaintenanceJob(ctx, job.ID, run.ID, err.Error(), isRetryable(err))
		return
	}

	logsSummary, _ := json.Marshal(map[string]interface{}{
		"operation":    "restic_deep_check",
		"subset_index": *job.SubsetIndex,
		"subset_total": *job.SubsetTotal,
		"status":       "success",
	})
	_ = w.maintRepo.CompleteMaintenanceJob(ctx, job.ID, run.ID, logsSummary)
	w.logger.Info("restic deep check completed successfully",
		slog.String("job_id", job.ID.String()),
		slog.Int("subset_index", *job.SubsetIndex),
		slog.Int("subset_total", *job.SubsetTotal),
	)
}

func isRetryable(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "deadline exceeded") ||
		strings.Contains(msg, "timeout") ||
		strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "temporary") ||
		strings.Contains(msg, "reset by peer") ||
		strings.Contains(msg, "busy") {
		return true
	}
	return false
}
