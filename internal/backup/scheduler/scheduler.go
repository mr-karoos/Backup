package scheduler

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"backup-platform/internal/backup/domain"
	"backup-platform/internal/backup/scheduling"
	"backup-platform/pkg/uuid"
)

// SchedulerRepository defines the database interface required by the Scheduler component.
type SchedulerRepository interface {
	FindDuePlans(ctx context.Context, now time.Time, limit int, afterNextRunAt *time.Time, afterID *uuid.UUID) ([]*domain.BackupPlan, error)
	EnqueueScheduledJobAndAdvanceNextRun(ctx context.Context, planID uuid.UUID, expectedUpdatedAt time.Time, jobToInsert *domain.BackupJob, newNextRunAt *time.Time) (*domain.BackupJob, error)
}

const (
	// DefaultPollInterval is the default period between schedule evaluation ticks.
	DefaultPollInterval = 10 * time.Second

	// DefaultBatchSize is the number of due plans evaluated per pagination query.
	DefaultBatchSize = 50
)

// Scheduler scans for due backup plans, persists pending scheduled jobs, and advances next_run_at.
type Scheduler struct {
	repo         SchedulerRepository
	logger       *slog.Logger
	pollInterval time.Duration
	batchSize    int
	nowFunc      func() time.Time
}

// NewScheduler constructs a new Scheduler instance.
func NewScheduler(repo SchedulerRepository, log *slog.Logger, pollInterval time.Duration) *Scheduler {
	if log == nil {
		log = slog.Default()
	}
	if pollInterval <= 0 {
		pollInterval = DefaultPollInterval
	}
	return &Scheduler{
		repo:         repo,
		logger:       log,
		pollInterval: pollInterval,
		batchSize:    DefaultBatchSize,
		nowFunc:      time.Now,
	}
}

// SetNowFunc sets the custom time supplier for testing.
func (s *Scheduler) SetNowFunc(f func() time.Time) {
	if f != nil {
		s.nowFunc = f
	}
}

// SetBatchSize sets the pagination batch size for evaluation queries.
func (s *Scheduler) SetBatchSize(size int) {
	if size > 0 {
		s.batchSize = size
	}
}

// Start runs the scheduler loop until the context is canceled.
func (s *Scheduler) Start(ctx context.Context) error {
	s.logger.Info("starting backup scheduler", "poll_interval", s.pollInterval.String())

	// Run an immediate tick on startup to evaluate plans that became due during downtime
	if _, err := s.Tick(ctx, s.nowFunc()); err != nil && !errors.Is(err, context.Canceled) {
		s.logger.Error("initial scheduler tick failed", "error", err)
	}

	ticker := time.NewTicker(s.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			s.logger.Info("stopping backup scheduler")
			return ctx.Err()
		case t := <-ticker.C:
			if _, err := s.Tick(ctx, t); err != nil && !errors.Is(err, context.Canceled) {
				s.logger.Error("scheduler evaluation tick failed", "error", err)
			}
		}
	}
}

// Tick executes a single scheduled plan evaluation cycle.
// Returns the count of jobs newly enqueued.
func (s *Scheduler) Tick(ctx context.Context, now time.Time) (int, error) {
	var totalEnqueued int
	var afterNextRunAt *time.Time
	var afterID *uuid.UUID

	for {
		if ctx.Err() != nil {
			return totalEnqueued, ctx.Err()
		}

		duePlans, err := s.repo.FindDuePlans(ctx, now, s.batchSize, afterNextRunAt, afterID)
		if err != nil {
			return totalEnqueued, err
		}

		if len(duePlans) == 0 {
			break
		}

		for _, plan := range duePlans {
			if ctx.Err() != nil {
				return totalEnqueued, ctx.Err()
			}

			// Validate plan has a valid cron schedule
			if plan.ScheduleCron == nil || *plan.ScheduleCron == "" {
				s.logger.Warn("due plan missing cron expression, skipping", "plan_id", plan.ID)
				continue
			}

			// Calculate next run occurrence strictly after current evaluation time 'now'
			// This automatically coalesces multiple missed intervals into the next single future occurrence.
			newNextRunAt, err := scheduling.CalculateNextRun(*plan.ScheduleCron, plan.ScheduleTimezone, now)
			if err != nil {
				s.logger.Error("failed calculating next run for due plan", "plan_id", plan.ID, "cron", *plan.ScheduleCron, "tz", plan.ScheduleTimezone, "error", err)
				continue
			}

			jobToInsert := &domain.BackupJob{
				ID:             uuid.New(),
				OrganizationID: plan.OrganizationID,
				ResourceID:     plan.ResourceID,
				BackupPlanID:   &plan.ID,
				TriggerType:    domain.TriggerTypeScheduled,
				BackupType:     plan.BackupType,
				TargetSpec:     plan.TargetSpec,
				Status:         domain.JobStatusPending,
			}

			enqueued, err := s.repo.EnqueueScheduledJobAndAdvanceNextRun(ctx, plan.ID, plan.UpdatedAt, jobToInsert, newNextRunAt)
			if err != nil {
				s.logger.Error("failed enqueuing scheduled job and advancing plan", "plan_id", plan.ID, "error", err)
				continue
			}

			if enqueued != nil {
				totalEnqueued++
				s.logger.Info("enqueued scheduled backup job", "plan_id", plan.ID, "job_id", enqueued.ID, "resource_id", plan.ResourceID, "next_run_at", newNextRunAt)
			} else {
				s.logger.Debug("advanced due plan without enqueuing duplicate pending job", "plan_id", plan.ID, "next_run_at", newNextRunAt)
			}
		}

		if len(duePlans) < s.batchSize {
			break
		}

		lastPlan := duePlans[len(duePlans)-1]
		afterNextRunAt = lastPlan.NextRunAt
		afterID = &lastPlan.ID
	}

	return totalEnqueued, nil
}
