package worker

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"backup-platform/internal/backup/repository"
	"backup-platform/internal/storage"
	"backup-platform/pkg/uuid"
)

const (
	defaultReaperInterval = 30 * time.Second
)

// RunStartupRecovery recovers any orphaned running runs from previous process crashes,
// cleans orphan platform-generated temporary files, and removes active artifacts of recovered runs.
func RunStartupRecovery(
	ctx context.Context,
	repo repository.BackupRepository,
	storageProvider storage.StorageProvider,
	log *slog.Logger,
) error {
	return RunStartupRecoveryWithResolver(ctx, repo, storageProvider, nil, log)
}

// RunStartupRecoveryWithResolver recovers orphaned running runs with dynamic storage resolver support.
func RunStartupRecoveryWithResolver(
	ctx context.Context,
	repo repository.BackupRepository,
	storageProvider storage.StorageProvider,
	storageResolver storage.StorageProviderResolver,
	log *slog.Logger,
) error {
	if log == nil {
		log = slog.Default()
	}

	// 1. Clean orphan platform-generated temporary partial files left from process crashes
	if cleaner, ok := storageProvider.(storage.TemporaryArtifactCleaner); ok && cleaner != nil {
		cleaned, err := cleaner.CleanOrphanTemporaryArtifacts(ctx)
		if err != nil {
			log.Warn("failed cleaning orphan temporary backup files")
		} else if cleaned > 0 {
			log.Info("cleaned orphan temporary backup files", slog.Int("cleaned_partials", cleaned))
		}
	}

	// 2. Recover interrupted runs in database (fail-fast on startup)
	recoveredRuns, recErr := repo.RecoverInterruptedRuns(ctx)
	if len(recoveredRuns) > 0 {
		log.Info("startup recovery completed", slog.Int("recovered_interrupted_runs", len(recoveredRuns)))

		// 3. Clean active artifacts for each successfully recovered run using a bounded independent context
		for _, run := range recoveredRuns {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
			cleanupCrashArtifacts(cleanupCtx, repo, storageProvider, storageResolver, run.OrganizationID, run.ID, log)
			cleanupCancel()
		}
	}

	if recErr != nil {
		log.Error("backup startup recovery failed")
		return recErr
	}

	return nil
}

// StaleRunReaper periodically queries the database for active runs whose lease has expired,
// marks them failed, resets parent jobs, and cleans any active artifacts.
type StaleRunReaper struct {
	repo            repository.BackupRepository
	storageProvider storage.StorageProvider
	storageResolver storage.StorageProviderResolver
	interval        time.Duration
	logger          *slog.Logger
	cancel          context.CancelFunc
	wg              sync.WaitGroup
}

// NewStaleRunReaper constructs a new StaleRunReaper.
func NewStaleRunReaper(
	repo repository.BackupRepository,
	storageProvider storage.StorageProvider,
	interval time.Duration,
	log *slog.Logger,
) *StaleRunReaper {
	if interval <= 0 {
		interval = defaultReaperInterval
	}
	if log == nil {
		log = slog.Default()
	}
	return &StaleRunReaper{
		repo:            repo,
		storageProvider: storageProvider,
		interval:        interval,
		logger:          log,
	}
}

// SetStorageResolver configures a dynamic storage provider resolver for StaleRunReaper.
func (r *StaleRunReaper) SetStorageResolver(resolver storage.StorageProviderResolver) {
	r.storageResolver = resolver
}

// Start begins periodic background execution of stale run reaping.
func (r *StaleRunReaper) Start(ctx context.Context) {
	reaperCtx, cancel := context.WithCancel(ctx)
	r.cancel = cancel

	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		ticker := time.NewTicker(r.interval)
		defer ticker.Stop()

		for {
			select {
			case <-reaperCtx.Done():
				return
			case <-ticker.C:
				reapedRuns, reapErr := r.repo.ReapStaleRuns(reaperCtx)
				if len(reapedRuns) > 0 {
					r.logger.Info("reaped expired backup runs", slog.Int("reaped_runs", len(reapedRuns)))
					for _, run := range reapedRuns {
						cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
						cleanupCrashArtifacts(cleanupCtx, r.repo, r.storageProvider, r.storageResolver, run.OrganizationID, run.ID, r.logger)
						cleanupCancel()
					}
				}
				if reapErr != nil {
					r.logger.Warn("stale run reaper encountered error")
				}
			}
		}
	}()
}

// Stop gracefully halts the reaper background loop with a bounded context.
func (r *StaleRunReaper) Stop(ctx context.Context) error {
	if r.cancel != nil {
		r.cancel()
	}
	done := make(chan struct{})
	go func() {
		r.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		r.logger.Info("stale run reaper stopped")
		return nil
	case <-ctx.Done():
		r.logger.Warn("stale run reaper shutdown timed out")
		return ctx.Err()
	}
}

// cleanupCrashArtifacts purges physical storage files and tombstones metadata for active artifacts of a failed run.
func cleanupCrashArtifacts(
	ctx context.Context,
	repo repository.BackupRepository,
	storageProvider storage.StorageProvider,
	storageResolver storage.StorageProviderResolver,
	orgID, runID uuid.UUID,
	log *slog.Logger,
) {
	if repo == nil || (storageProvider == nil && storageResolver == nil) || orgID == uuid.Nil || runID == uuid.Nil {
		return
	}

	artifacts, err := repo.GetRunArtifacts(ctx, orgID, runID)
	if err != nil {
		log.Warn("failed querying artifacts for crashed run cleanup",
			slog.String("run_id", runID.String()),
		)
		return
	}

	for _, art := range artifacts {
		if art == nil || art.IsDeleted {
			continue // Already tombstoned: skip
		}

		var store storage.StorageProvider
		if storageResolver != nil {
			if art.StorageTargetID == uuid.Nil {
				log.Warn("skipping crash artifact cleanup: missing storage_target_id",
					slog.String("run_id", runID.String()),
					slog.String("artifact_id", art.ID.String()),
				)
				continue
			}
			resolved, err := storageResolver.Resolve(ctx, orgID, art.StorageTargetID)
			if err != nil || resolved == nil {
				log.Warn("skipping crash artifact cleanup: storage provider resolution failed",
					slog.String("run_id", runID.String()),
					slog.String("artifact_id", art.ID.String()),
					slog.String("target_id", art.StorageTargetID.String()),
				)
				continue
			}
			store = resolved
		} else {
			store = storageProvider
		}

		if store == nil {
			continue
		}

		// 1. Delete physical file from storage provider
		delErr := store.DeleteArtifact(ctx, art.StorageReference)
		if delErr != nil && !errors.Is(delErr, storage.ErrArtifactNotFound) {
			log.Warn("failed deleting artifact physical file during crash recovery",
				slog.String("artifact_id", art.ID.String()),
			)
			// If physical delete fails: do NOT tombstone, do NOT rollback run/job recovery, continue to next artifact
			continue
		}

		// 2. Only after successful/idempotent physical deletion: TombstoneArtifact
		if tbErr := repo.TombstoneArtifact(ctx, orgID, art.ID); tbErr != nil {
			log.Warn("failed tombstoning artifact metadata after physical deletion",
				slog.String("artifact_id", art.ID.String()),
			)
			// If tombstone fails: do NOT fake-restore physical file, run/job recovery remains valid, continue
		}
	}
}
