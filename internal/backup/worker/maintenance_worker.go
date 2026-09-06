package worker

import (
	"context"
	"encoding/json"
	"errors"
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
	reapTicker := time.NewTicker(time.Minute)
	defer reapTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-reapTicker.C:
			reaped, err := w.maintRepo.ReapStaleMaintenanceRuns(ctx)
			if err != nil {
				w.logger.Warn("failed reaping stale maintenance runs", slog.String("error", err.Error()))
			} else if len(reaped) > 0 {
				w.logger.Info("reaped stale maintenance runs", slog.Int("reaped_count", len(reaped)))
			}
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
		_ = w.maintRepo.FailMaintenanceJob(ctx, job.OrganizationID, job.ID, run.ID, errMsg, false)
		return
	}

	if repo.Status != domain.BackupRepositoryStatusActive {
		errMsg := fmt.Sprintf("repository status is %s, not active", repo.Status)
		w.logger.Error("maintenance failed: repository not active", slog.String("job_id", job.ID.String()), slog.String("error", errMsg))
		_ = w.maintRepo.FailMaintenanceJob(ctx, job.OrganizationID, job.ID, run.ID, errMsg, false)
		return
	}

	// 2. TryAcquireExclusive (Yield immediately if busy to avoid starving customer operations)
	unlock, acquired, err := w.coordinator.TryAcquireExclusive(job.RepositoryID)
	if err != nil {
		errMsg := fmt.Sprintf("coordinator error: %v", err)
		w.logger.Error("maintenance failed: coordinator error", slog.String("job_id", job.ID.String()), slog.String("error", errMsg))
		_ = w.maintRepo.FailMaintenanceJob(ctx, job.OrganizationID, job.ID, run.ID, errMsg, false)
		return
	}
	if !acquired {
		w.logger.Info("repository is busy with active operations, yielding maintenance job",
			slog.String("job_id", job.ID.String()),
			slog.String("repo_id", job.RepositoryID.String()),
		)
		_ = w.maintRepo.FailMaintenanceJob(ctx, job.OrganizationID, job.ID, run.ID, "repository busy with concurrent operations, yielding for retry", true)
		return
	}
	defer unlock()

	// 3. Resolve Concrete Target
	storageTarget, err := w.storageRepo.GetStorageTargetByID(ctx, job.OrganizationID, repo.StorageTargetID)
	if err != nil {
		errMsg := fmt.Sprintf("storage target not found: %v", err)
		w.logger.Error("maintenance failed: storage target not found", slog.String("job_id", job.ID.String()), slog.String("error", errMsg))
		_ = w.maintRepo.FailMaintenanceJob(ctx, job.OrganizationID, job.ID, run.ID, errMsg, false)
		return
	}

	repoTarget, err := w.targetResolver.ResolveTarget(ctx, job.OrganizationID, repo.ResourceID, storageTarget)
	if err != nil {
		errMsg := fmt.Sprintf("failed resolving repository target: %v", err)
		w.logger.Error("maintenance failed: target resolution failed", slog.String("job_id", job.ID.String()), slog.String("error", errMsg))
		_ = w.maintRepo.FailMaintenanceJob(ctx, job.OrganizationID, job.ID, run.ID, errMsg, false)
		return
	}
	defer repoTarget.Cleanup()

	// 4. Load Repository Key
	credType, repoKey, err := w.vault.LoadCredentialForUse(ctx, job.OrganizationID, repo.CredentialID)
	if err != nil {
		errMsg := fmt.Sprintf("failed loading repository key: %v", err)
		w.logger.Error("maintenance failed: key load failed", slog.String("job_id", job.ID.String()), slog.String("error", errMsg))
		_ = w.maintRepo.FailMaintenanceJob(ctx, job.OrganizationID, job.ID, run.ID, errMsg, false)
		return
	}
	defer secretcrypto.ZeroBytes(repoKey)

	if credType != credDomain.TypeResticRepositoryKey || len(repoKey) == 0 {
		errMsg := "invalid restic repository key format"
		w.logger.Error("maintenance failed: invalid repository key", slog.String("job_id", job.ID.String()))
		_ = w.maintRepo.FailMaintenanceJob(ctx, job.OrganizationID, job.ID, run.ID, errMsg, false)
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
				_ = w.maintRepo.HeartbeatMaintenanceRun(ctx, job.OrganizationID, run.ID, w.cfg.LeaseDuration)
			}
		}
	}()

	// 6. Execute Subprocess by Operation Type
	switch job.OperationType {
	case domain.MaintenanceOpResticForget:
		w.handleForget(ctx, job, run, repo, repoTarget, repoKey)

	case domain.MaintenanceOpResticPrune:
		w.handlePrune(ctx, job, run, repoTarget, repoKey)

	case domain.MaintenanceOpResticDeepCheck:
		w.handleDeepCheck(ctx, job, run, repoTarget, repoKey)

	default:
		errMsg := fmt.Sprintf("unsupported operation type: %s", job.OperationType)
		w.logger.Error("maintenance failed", slog.String("error", errMsg))
		_ = w.maintRepo.FailMaintenanceJob(ctx, job.OrganizationID, job.ID, run.ID, errMsg, false)
	}
}

func (w *RepositoryMaintenanceWorker) handleForget(
	ctx context.Context,
	job *domain.MaintenanceJob,
	run *domain.MaintenanceRun,
	repo *domain.BackupRepository,
	repoTarget restic.RepositoryTarget,
	repoKey []byte,
) {
	if job.ArtifactID == nil || *job.ArtifactID == uuid.Nil || job.SnapshotID == "" {
		errMsg := "restic_forget missing artifact_id or snapshot_id"
		_ = w.maintRepo.FailMaintenanceJob(ctx, job.OrganizationID, job.ID, run.ID, errMsg, false)
		return
	}

	if !domain.IsValidCanonicalResticSnapshotID(job.SnapshotID) {
		errMsg := fmt.Sprintf("invalid canonical snapshot id format: %s", job.SnapshotID)
		_ = w.maintRepo.FailMaintenanceJob(ctx, job.OrganizationID, job.ID, run.ID, errMsg, false)
		return
	}

	// Invariant validations against artifact entity (Fail closed terminally)
	artifact, err := w.backupRepo.GetArtifactByID(ctx, job.OrganizationID, *job.ArtifactID)
	if err != nil {
		errMsg := fmt.Sprintf("failed fetching artifact: %v", err)
		_ = w.maintRepo.FailMaintenanceJob(ctx, job.OrganizationID, job.ID, run.ID, errMsg, false)
		return
	}
	if artifact == nil {
		_ = w.maintRepo.FailMaintenanceJob(ctx, job.OrganizationID, job.ID, run.ID, "artifact not found", false)
		return
	}
	if artifact.OrganizationID != job.OrganizationID {
		_ = w.maintRepo.FailMaintenanceJob(ctx, job.OrganizationID, job.ID, run.ID, "artifact organization mismatch", false)
		return
	}
	if artifact.ID != *job.ArtifactID {
		_ = w.maintRepo.FailMaintenanceJob(ctx, job.OrganizationID, job.ID, run.ID, "artifact id mismatch", false)
		return
	}
	if artifact.Format != domain.ArtifactFormatResticSnapshot {
		_ = w.maintRepo.FailMaintenanceJob(ctx, job.OrganizationID, job.ID, run.ID, "artifact format is not restic_snapshot", false)
		return
	}
	if artifact.IsDeleted {
		_ = w.maintRepo.FailMaintenanceJob(ctx, job.OrganizationID, job.ID, run.ID, "artifact is already marked deleted", false)
		return
	}
	if artifact.RepositoryID == nil || *artifact.RepositoryID != job.RepositoryID {
		_ = w.maintRepo.FailMaintenanceJob(ctx, job.OrganizationID, job.ID, run.ID, "artifact repository mismatch", false)
		return
	}
	if artifact.SnapshotID != job.SnapshotID {
		_ = w.maintRepo.FailMaintenanceJob(ctx, job.OrganizationID, job.ID, run.ID, "artifact snapshot id mismatch", false)
		return
	}
	if artifact.ResourceID != repo.ResourceID {
		_ = w.maintRepo.FailMaintenanceJob(ctx, job.OrganizationID, job.ID, run.ID, "artifact resource mismatch with repository", false)
		return
	}
	if artifact.StorageTargetID != repo.StorageTargetID {
		_ = w.maintRepo.FailMaintenanceJob(ctx, job.OrganizationID, job.ID, run.ID, "artifact storage target mismatch with repository", false)
		return
	}

	alreadyForgotten := job.Phase != nil && *job.Phase == domain.MaintenancePhaseForgetExecuted

	if !alreadyForgotten {
		// Execute Restic Forget (NO prune inline!)
		err := w.resticRunner.ForgetSnapshot(ctx, repoTarget, repoKey, job.SnapshotID)
		if err != nil {
			w.logger.Error("restic forget failed",
				slog.String("job_id", job.ID.String()),
				slog.String("snapshot_id", job.SnapshotID),
				slog.String("error", err.Error()),
			)
			_ = w.maintRepo.FailMaintenanceJob(ctx, job.OrganizationID, job.ID, run.ID, err.Error(), isRetryable(err))
			return
		}

		// Update phase to forget_executed
		if pErr := w.maintRepo.UpdateJobPhase(ctx, job.OrganizationID, job.ID, domain.MaintenancePhaseForgetExecuted); pErr != nil {
			w.logger.Warn("failed updating job phase to forget_executed", slog.String("error", pErr.Error()))
		}
	}

	// Verify snapshot is truly absent from repository
	if vErr := w.resticRunner.VerifySnapshotAbsent(ctx, repoTarget, repoKey, job.SnapshotID); vErr != nil {
		w.logger.Error("snapshot absence verification failed",
			slog.String("job_id", job.ID.String()),
			slog.String("snapshot_id", job.SnapshotID),
			slog.String("error", vErr.Error()),
		)
		_ = w.maintRepo.FailMaintenanceJob(ctx, job.OrganizationID, job.ID, run.ID, vErr.Error(), isRetryable(vErr))
		return
	}

	// Atomic Finalize: Tombstone artifact, debounce enqueue prune, complete run, complete job
	logsSummary, _ := json.Marshal(map[string]string{
		"operation":   "restic_forget",
		"snapshot_id": job.SnapshotID,
		"status":      "success",
	})
	if fErr := w.maintRepo.FinalizeSuccessfulResticForget(ctx, job.OrganizationID, job.ID, run.ID, *job.ArtifactID, job.RepositoryID, job.SnapshotID, logsSummary); fErr != nil {
		w.logger.Error("failed finalizing restic forget in database",
			slog.String("job_id", job.ID.String()),
			slog.String("error", fErr.Error()),
		)
		_ = w.maintRepo.FailMaintenanceJob(ctx, job.OrganizationID, job.ID, run.ID, fErr.Error(), isRetryable(fErr))
		return
	}

	// Audit Log Emission (user_id = nil)
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
			UserID:         nil,
			Metadata:       metaBytes,
			CreatedAt:      w.nowFunc(),
		}
		if auditErr := w.auditRecorder.Record(ctx, auditLog); auditErr != nil {
			w.logger.Warn("failed recording retention cleanup audit log", slog.String("error", auditErr.Error()))
		}
	}

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
		if w.auditRecorder != nil {
			metaObj := map[string]any{
				"repo_id": job.RepositoryID.String(),
				"job_id":  job.ID.String(),
				"run_id":  run.ID.String(),
				"error":   err.Error(),
			}
			metaBytes, _ := json.Marshal(metaObj)
			_ = w.auditRecorder.Record(ctx, &auditDomain.AuditLog{
				ID:             uuid.New(),
				OrganizationID: &job.OrganizationID,
				Action:         auditDomain.ActionMaintenancePruneFail,
				EntityType:     auditDomain.EntityTypeBackupRepository,
				EntityID:       &job.RepositoryID,
				UserID:         nil,
				Metadata:       metaBytes,
				CreatedAt:      w.nowFunc(),
			})
		}
		_ = w.maintRepo.FailMaintenanceJob(ctx, job.OrganizationID, job.ID, run.ID, err.Error(), isRetryable(err))
		return
	}

	logsSummary, _ := json.Marshal(map[string]string{
		"operation": "restic_prune",
		"status":    "success",
	})
	_ = w.maintRepo.CompleteMaintenanceJob(ctx, job.OrganizationID, job.ID, run.ID, logsSummary)

	if w.auditRecorder != nil {
		metaObj := map[string]any{
			"repo_id": job.RepositoryID.String(),
			"job_id":  job.ID.String(),
			"run_id":  run.ID.String(),
			"status":  "success",
		}
		metaBytes, _ := json.Marshal(metaObj)
		_ = w.auditRecorder.Record(ctx, &auditDomain.AuditLog{
			ID:             uuid.New(),
			OrganizationID: &job.OrganizationID,
			Action:         auditDomain.ActionMaintenancePrune,
			EntityType:     auditDomain.EntityTypeBackupRepository,
			EntityID:       &job.RepositoryID,
			UserID:         nil,
			Metadata:       metaBytes,
			CreatedAt:      w.nowFunc(),
		})
	}

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
		_ = w.maintRepo.FailMaintenanceJob(ctx, job.OrganizationID, job.ID, run.ID, errMsg, false)
		return
	}

	if *job.SubsetTotal > domain.MaxMaintenanceDeepCheckSubsets || *job.SubsetIndex < 1 || *job.SubsetIndex > *job.SubsetTotal {
		errMsg := fmt.Sprintf("invalid subset parameters: index %d, total %d", *job.SubsetIndex, *job.SubsetTotal)
		_ = w.maintRepo.FailMaintenanceJob(ctx, job.OrganizationID, job.ID, run.ID, errMsg, false)
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
		if w.auditRecorder != nil {
			metaObj := map[string]any{
				"repo_id":      job.RepositoryID.String(),
				"job_id":       job.ID.String(),
				"run_id":       run.ID.String(),
				"subset_index": *job.SubsetIndex,
				"subset_total": *job.SubsetTotal,
				"error":        err.Error(),
			}
			metaBytes, _ := json.Marshal(metaObj)
			_ = w.auditRecorder.Record(ctx, &auditDomain.AuditLog{
				ID:             uuid.New(),
				OrganizationID: &job.OrganizationID,
				Action:         auditDomain.ActionMaintenanceCheckFail,
				EntityType:     auditDomain.EntityTypeBackupRepository,
				EntityID:       &job.RepositoryID,
				UserID:         nil,
				Metadata:       metaBytes,
				CreatedAt:      w.nowFunc(),
			})
		}
		_ = w.maintRepo.FailMaintenanceJob(ctx, job.OrganizationID, job.ID, run.ID, err.Error(), isRetryable(err))
		return
	}

	logsSummary, _ := json.Marshal(map[string]interface{}{
		"operation":    "restic_deep_check",
		"subset_index": *job.SubsetIndex,
		"subset_total": *job.SubsetTotal,
		"status":       "success",
	})
	_ = w.maintRepo.CompleteMaintenanceJob(ctx, job.OrganizationID, job.ID, run.ID, logsSummary)

	if w.auditRecorder != nil {
		metaObj := map[string]any{
			"repo_id":      job.RepositoryID.String(),
			"job_id":       job.ID.String(),
			"run_id":       run.ID.String(),
			"subset_index": *job.SubsetIndex,
			"subset_total": *job.SubsetTotal,
			"status":       "success",
		}
		metaBytes, _ := json.Marshal(metaObj)
		_ = w.auditRecorder.Record(ctx, &auditDomain.AuditLog{
			ID:             uuid.New(),
			OrganizationID: &job.OrganizationID,
			Action:         auditDomain.ActionMaintenanceCheck,
			EntityType:     auditDomain.EntityTypeBackupRepository,
			EntityID:       &job.RepositoryID,
			UserID:         nil,
			Metadata:       metaBytes,
			CreatedAt:      w.nowFunc(),
		})
	}

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
	if errors.Is(err, restic.ErrRepositoryBusy) {
		return true
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "deadline exceeded") ||
		strings.Contains(msg, "timeout") ||
		strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "temporary") ||
		strings.Contains(msg, "reset by peer") ||
		strings.Contains(msg, "busy") ||
		strings.Contains(msg, "i/o timeout") ||
		strings.Contains(msg, "network is unreachable") {
		return true
	}
	return false
}
