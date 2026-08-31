package scheduler

import (
	"context"
	"sync"
	"testing"
	"time"

	"backup-platform/internal/backup/domain"
	"backup-platform/pkg/uuid"
)

type fakeSchedulerRepo struct {
	mu           sync.Mutex
	plans        map[uuid.UUID]*domain.BackupPlan
	jobs         []*domain.BackupJob
	pendingPlans map[uuid.UUID]bool // tracks if plan has active pending job
}

func newFakeSchedulerRepo() *fakeSchedulerRepo {
	return &fakeSchedulerRepo{
		plans:        make(map[uuid.UUID]*domain.BackupPlan),
		pendingPlans: make(map[uuid.UUID]bool),
	}
}

func (f *fakeSchedulerRepo) FindDuePlans(ctx context.Context, now time.Time, limit int, afterNextRunAt *time.Time, afterID *uuid.UUID) ([]*domain.BackupPlan, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	var due []*domain.BackupPlan
	for _, p := range f.plans {
		if p.Status != domain.PlanStatusActive || !p.IsScheduleEnabled || p.NextRunAt == nil {
			continue
		}
		if p.NextRunAt.After(now) {
			continue
		}
		if afterNextRunAt != nil {
			if p.NextRunAt.Before(*afterNextRunAt) {
				continue
			}
			if p.NextRunAt.Equal(*afterNextRunAt) && afterID != nil && p.ID.String() <= afterID.String() {
				continue
			}
		}
		// Deep copy
		planCopy := *p
		due = append(due, &planCopy)
		if len(due) >= limit {
			break
		}
	}
	return due, nil
}

func (f *fakeSchedulerRepo) EnqueueScheduledJobAndAdvanceNextRun(
	ctx context.Context,
	planID uuid.UUID,
	expectedUpdatedAt time.Time,
	jobToInsert *domain.BackupJob,
	newNextRunAt *time.Time,
) (*domain.BackupJob, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	p, ok := f.plans[planID]
	if !ok || p.Status != domain.PlanStatusActive || !p.IsScheduleEnabled {
		return nil, nil
	}

	p.NextRunAt = newNextRunAt
	p.UpdatedAt = time.Now()

	hasPending := f.pendingPlans[planID]
	if !hasPending && jobToInsert != nil {
		jobToInsert.CreatedAt = time.Now()
		jobToInsert.UpdatedAt = time.Now()
		f.jobs = append(f.jobs, jobToInsert)
		f.pendingPlans[planID] = true
		return jobToInsert, nil
	}

	return nil, nil
}

func TestScheduler_Tick_EnqueuesDuePlan(t *testing.T) {
	repo := newFakeSchedulerRepo()
	scheduler := NewScheduler(repo, nil, time.Second)

	planID := uuid.New()
	orgID := uuid.New()
	resID := uuid.New()
	cron := "0 2 * * *"
	dueTime := time.Date(2026, 8, 20, 2, 0, 0, 0, time.UTC)
	now := time.Date(2026, 8, 20, 2, 5, 0, 0, time.UTC)

	repo.plans[planID] = &domain.BackupPlan{
		ID:                planID,
		OrganizationID:    orgID,
		ResourceID:        resID,
		Name:              "Daily MySQL",
		BackupType:        domain.BackupTypeMySQLDatabase,
		TargetSpec:        domain.TargetSpec{Databases: []string{"db1"}},
		ScheduleCron:      &cron,
		ScheduleTimezone:  "UTC",
		IsScheduleEnabled: true,
		Status:            domain.PlanStatusActive,
		NextRunAt:         &dueTime,
		UpdatedAt:         time.Now(),
	}

	enqueued, err := scheduler.Tick(context.Background(), now)
	if err != nil {
		t.Fatalf("Tick failed: %v", err)
	}
	if enqueued != 1 {
		t.Fatalf("expected 1 job enqueued, got %d", enqueued)
	}

	if len(repo.jobs) != 1 {
		t.Fatalf("expected 1 job in repo, got %d", len(repo.jobs))
	}
	job := repo.jobs[0]
	if job.TriggerType != domain.TriggerTypeScheduled {
		t.Errorf("expected trigger_type=scheduled, got %s", job.TriggerType)
	}
	if job.Status != domain.JobStatusPending {
		t.Errorf("expected status=pending, got %s", job.Status)
	}
	if job.CreatedByUserID != nil {
		t.Errorf("expected nil created_by_user_id for scheduled job")
	}

	// Verify next_run_at advanced to next day at 02:00 UTC
	expectedNext := time.Date(2026, 8, 21, 2, 0, 0, 0, time.UTC)
	if repo.plans[planID].NextRunAt == nil || !repo.plans[planID].NextRunAt.Equal(expectedNext) {
		t.Errorf("expected next_run_at %v, got %v", expectedNext, repo.plans[planID].NextRunAt)
	}
}

func TestScheduler_Tick_CoalescesMissedIntervals(t *testing.T) {
	repo := newFakeSchedulerRepo()
	scheduler := NewScheduler(repo, nil, time.Second)

	planID := uuid.New()
	orgID := uuid.New()
	resID := uuid.New()
	cron := "0 2 * * *"
	// Missed 3 days ago
	missedTime := time.Date(2026, 8, 17, 2, 0, 0, 0, time.UTC)
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)

	repo.plans[planID] = &domain.BackupPlan{
		ID:                planID,
		OrganizationID:    orgID,
		ResourceID:        resID,
		Name:              "Daily MySQL",
		BackupType:        domain.BackupTypeMySQLDatabase,
		TargetSpec:        domain.TargetSpec{Databases: []string{"db1"}},
		ScheduleCron:      &cron,
		ScheduleTimezone:  "UTC",
		IsScheduleEnabled: true,
		Status:            domain.PlanStatusActive,
		NextRunAt:         &missedTime,
		UpdatedAt:         time.Now(),
	}

	enqueued, err := scheduler.Tick(context.Background(), now)
	if err != nil {
		t.Fatalf("Tick failed: %v", err)
	}
	if enqueued != 1 {
		t.Fatalf("expected exactly 1 job enqueued (coalesced), got %d", enqueued)
	}

	// Next run should be tomorrow (2026-08-21 02:00 UTC), skipping missed dates without burst
	expectedNext := time.Date(2026, 8, 21, 2, 0, 0, 0, time.UTC)
	if repo.plans[planID].NextRunAt == nil || !repo.plans[planID].NextRunAt.Equal(expectedNext) {
		t.Errorf("expected next_run_at %v, got %v", expectedNext, repo.plans[planID].NextRunAt)
	}
}

func TestScheduler_Tick_SkipsWhenPendingJobExists(t *testing.T) {
	repo := newFakeSchedulerRepo()
	scheduler := NewScheduler(repo, nil, time.Second)

	planID := uuid.New()
	orgID := uuid.New()
	resID := uuid.New()
	cron := "0 2 * * *"
	dueTime := time.Date(2026, 8, 20, 2, 0, 0, 0, time.UTC)
	now := time.Date(2026, 8, 20, 2, 5, 0, 0, time.UTC)

	repo.plans[planID] = &domain.BackupPlan{
		ID:                planID,
		OrganizationID:    orgID,
		ResourceID:        resID,
		Name:              "Daily MySQL",
		BackupType:        domain.BackupTypeMySQLDatabase,
		TargetSpec:        domain.TargetSpec{Databases: []string{"db1"}},
		ScheduleCron:      &cron,
		ScheduleTimezone:  "UTC",
		IsScheduleEnabled: true,
		Status:            domain.PlanStatusActive,
		NextRunAt:         &dueTime,
		UpdatedAt:         time.Now(),
	}
	// Mark pending job already active for this plan
	repo.pendingPlans[planID] = true

	enqueued, err := scheduler.Tick(context.Background(), now)
	if err != nil {
		t.Fatalf("Tick failed: %v", err)
	}
	if enqueued != 0 {
		t.Fatalf("expected 0 jobs enqueued when pending job exists, got %d", enqueued)
	}

	// Next run should still advance to prevent tight spin
	expectedNext := time.Date(2026, 8, 21, 2, 0, 0, 0, time.UTC)
	if repo.plans[planID].NextRunAt == nil || !repo.plans[planID].NextRunAt.Equal(expectedNext) {
		t.Errorf("expected next_run_at %v, got %v", expectedNext, repo.plans[planID].NextRunAt)
	}
}

func TestScheduler_Tick_EnqueuesDuePlan_ModeAll(t *testing.T) {
	repo := newFakeSchedulerRepo()
	scheduler := NewScheduler(repo, nil, time.Second)

	planID := uuid.New()
	orgID := uuid.New()
	resID := uuid.New()
	cron := "0 2 * * *"
	dueTime := time.Date(2026, 8, 20, 2, 0, 0, 0, time.UTC)
	now := time.Date(2026, 8, 20, 2, 5, 0, 0, time.UTC)

	repo.plans[planID] = &domain.BackupPlan{
		ID:                planID,
		OrganizationID:    orgID,
		ResourceID:        resID,
		Name:              "All Databases Plan",
		BackupType:        domain.BackupTypeMySQLDatabase,
		TargetSpec:        domain.TargetSpec{Databases: []string{}}, // mode = "all"
		ScheduleCron:      &cron,
		ScheduleTimezone:  "UTC",
		IsScheduleEnabled: true,
		Status:            domain.PlanStatusActive,
		NextRunAt:         &dueTime,
		UpdatedAt:         time.Now(),
	}

	enqueued, err := scheduler.Tick(context.Background(), now)
	if err != nil {
		t.Fatalf("Tick failed: %v", err)
	}
	if enqueued != 1 {
		t.Fatalf("expected 1 job enqueued, got %d", enqueued)
	}

	if len(repo.jobs) != 1 {
		t.Fatalf("expected 1 job in repo, got %d", len(repo.jobs))
	}
	job := repo.jobs[0]
	if job.TriggerType != domain.TriggerTypeScheduled {
		t.Errorf("expected trigger_type=scheduled, got %s", job.TriggerType)
	}
	if len(job.TargetSpec.Databases) != 0 {
		t.Errorf("expected empty databases (mode all) in scheduled job target spec, got %v", job.TargetSpec.Databases)
	}
}
