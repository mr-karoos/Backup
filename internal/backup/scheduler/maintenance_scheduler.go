package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"backup-platform/internal/backup/domain"
	"backup-platform/internal/backup/repository"
	"backup-platform/pkg/uuid"
)

// MaintenanceSchedulerConfig defines schedule intervals and parameters for maintenance orchestration.
type MaintenanceSchedulerConfig struct {
	PollInterval     time.Duration
	TotalSubsets     int
	DeepCheckEnabled bool
}

// DefaultMaintenanceSchedulerConfig returns default production settings.
func DefaultMaintenanceSchedulerConfig() MaintenanceSchedulerConfig {
	return MaintenanceSchedulerConfig{
		PollInterval:     15 * time.Minute,
		TotalSubsets:     4,
		DeepCheckEnabled: true,
	}
}

// MaintenanceScheduler periodically enqueues maintenance operations (like restic_deep_check with deterministic rotation).
type MaintenanceScheduler struct {
	maintRepo repository.MaintenanceRepository
	logger    *slog.Logger
	cfg       MaintenanceSchedulerConfig
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	nowFunc   func() time.Time
}

// NewMaintenanceScheduler constructs a new MaintenanceScheduler.
func NewMaintenanceScheduler(
	maintRepo repository.MaintenanceRepository,
	cfg MaintenanceSchedulerConfig,
	logger *slog.Logger,
) *MaintenanceScheduler {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 15 * time.Minute
	}
	if cfg.TotalSubsets < 1 {
		cfg.TotalSubsets = 4
	}

	return &MaintenanceScheduler{
		maintRepo: maintRepo,
		cfg:       cfg,
		logger:    logger,
		nowFunc:   time.Now,
	}
}

// SetNowFunc sets the custom time supplier for testing.
func (s *MaintenanceScheduler) SetNowFunc(f func() time.Time) {
	if f != nil {
		s.nowFunc = f
	}
}

// Start runs the periodic maintenance scheduler loop in the background.
func (s *MaintenanceScheduler) Start(parentCtx context.Context) {
	ctx, cancel := context.WithCancel(parentCtx)
	s.cancel = cancel

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.runLoop(ctx)
	}()
	s.logger.Info("repository maintenance scheduler started")
}

// Stop gracefully stops the scheduler.
func (s *MaintenanceScheduler) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
	s.wg.Wait()
	s.logger.Info("repository maintenance scheduler stopped")
}

func (s *MaintenanceScheduler) runLoop(ctx context.Context) {
	ticker := time.NewTicker(s.cfg.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.Tick(ctx)
		}
	}
}

// Tick evaluates active repositories and enqueues due maintenance operations.
func (s *MaintenanceScheduler) Tick(ctx context.Context) {
	if !s.cfg.DeepCheckEnabled {
		return
	}

	repos, err := s.maintRepo.ListActiveRepositories(ctx, 100)
	if err != nil {
		s.logger.Warn("failed listing active repositories for maintenance schedule", slog.String("error", err.Error()))
		return
	}

	for _, r := range repos {
		if r == nil {
			continue
		}
		job, err := s.EnqueueNextDeepCheck(ctx, r.OrganizationID, r.ID)
		if err != nil {
			s.logger.Warn("failed enqueuing deep check for repository",
				slog.String("org_id", r.OrganizationID.String()),
				slog.String("repo_id", r.ID.String()),
				slog.String("error", err.Error()),
			)
			continue
		}
		s.logger.Debug("ensured deep check maintenance job for repository",
			slog.String("repo_id", r.ID.String()),
			slog.String("job_id", job.ID.String()),
			slog.Int("subset", *job.SubsetIndex),
		)
	}
}

// EnqueueNextDeepCheck deterministically rotates and enqueues a restic_deep_check job.
// Next subset is calculated via modulo arithmetic: (last_successful % N) + 1.
func (s *MaintenanceScheduler) EnqueueNextDeepCheck(ctx context.Context, orgID, repoID uuid.UUID) (*domain.MaintenanceJob, error) {
	lastIndex, err := s.maintRepo.GetLastSuccessfulDeepCheckSubset(ctx, orgID, repoID)
	if err != nil {
		return nil, fmt.Errorf("failed querying last successful deep check subset: %w", err)
	}

	total := s.cfg.TotalSubsets
	nextIndex := (lastIndex % total) + 1

	job, err := s.maintRepo.EnqueueMaintenanceJob(ctx, domain.EnqueueMaintenanceJobParams{
		OrganizationID: orgID,
		RepositoryID:   repoID,
		OperationType:  domain.MaintenanceOpResticDeepCheck,
		SubsetIndex:    &nextIndex,
		SubsetTotal:    &total,
		Metadata: map[string]interface{}{
			"source": "maintenance_scheduler",
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed enqueuing deep check job: %w", err)
	}
	return job, nil
}
