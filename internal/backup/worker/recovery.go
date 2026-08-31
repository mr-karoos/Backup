package worker

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"backup-platform/internal/backup/repository"
)

const (
	defaultReaperInterval = 30 * time.Second
)

// RunStartupRecovery recovers any orphaned running runs from previous process crashes before worker pool startup.
func RunStartupRecovery(ctx context.Context, repo repository.BackupRepository, log *slog.Logger) error {
	if log == nil {
		log = slog.Default()
	}

	recovered, err := repo.RecoverInterruptedRuns(ctx)
	if err != nil {
		log.Error("backup startup recovery failed")
		return err
	}

	if recovered > 0 {
		log.Info("startup recovery completed", slog.Int("recovered_interrupted_runs", recovered))
	}

	return nil
}

// StaleRunReaper periodically queries the database for active runs whose lease has expired and marks them failed.
type StaleRunReaper struct {
	repo     repository.BackupRepository
	interval time.Duration
	logger   *slog.Logger
	cancel   context.CancelFunc
	wg       sync.WaitGroup
}

// NewStaleRunReaper constructs a new StaleRunReaper.
func NewStaleRunReaper(repo repository.BackupRepository, interval time.Duration, log *slog.Logger) *StaleRunReaper {
	if interval <= 0 {
		interval = defaultReaperInterval
	}
	if log == nil {
		log = slog.Default()
	}
	return &StaleRunReaper{
		repo:     repo,
		interval: interval,
		logger:   log,
	}
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
				reaped, err := r.repo.ReapStaleRuns(reaperCtx)
				if err != nil {
					r.logger.Warn("stale run reaper encountered error")
				} else if reaped > 0 {
					r.logger.Info("reaped expired backup runs", slog.Int("reaped_runs", reaped))
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
