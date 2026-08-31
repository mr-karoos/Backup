package worker

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"backup-platform/internal/backup/domain"
	"backup-platform/internal/backup/repository"
	"backup-platform/pkg/uuid"
)

const (
	defaultHeartbeatInterval = 30 * time.Second
	maxHeartbeatRetries      = 3
)

// StartHeartbeat initiates a background goroutine updating the lease of an active BackupRun every heartbeatInterval.
// If the heartbeat fails 3 consecutive times or the run lease is lost, it invokes cancelExec to safely terminate the running backup.
func StartHeartbeat(
	ctx context.Context,
	repo repository.BackupRepository,
	orgID, runID uuid.UUID,
	interval time.Duration,
	cancelExec context.CancelFunc,
	log *slog.Logger,
) (stop func()) {
	if log == nil {
		log = slog.Default()
	}
	if interval <= 0 {
		interval = defaultHeartbeatInterval
	}

	hbCtx, hbCancel := context.WithCancel(ctx)
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		consecutiveFailures := 0

		for {
			select {
			case <-hbCtx.Done():
				return
			case <-ticker.C:
				updateCtx, updateCancel := context.WithTimeout(context.Background(), 5*time.Second)
				err := repo.UpdateHeartbeat(updateCtx, orgID, runID)
				updateCancel()

				if err != nil {
					if errors.Is(err, domain.ErrRunNoLongerActive) {
						log.Warn("backup run lease lost, cancelling execution",
							slog.String("run_id", runID.String()),
						)
						if cancelExec != nil {
							cancelExec()
						}
						return
					}

					consecutiveFailures++
					log.Warn("backup heartbeat update failed",
						slog.String("run_id", runID.String()),
						slog.Int("failures", consecutiveFailures),
					)
					if consecutiveFailures >= maxHeartbeatRetries {
						log.Error("backup heartbeat threshold reached, cancelling execution",
							slog.String("run_id", runID.String()),
						)
						if cancelExec != nil {
							cancelExec()
						}
						return
					}
				} else {
					consecutiveFailures = 0
				}
			}
		}
	}()

	var stopOnce sync.Once
	return func() {
		stopOnce.Do(func() {
			hbCancel()
			wg.Wait()
		})
	}
}
