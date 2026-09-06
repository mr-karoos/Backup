package scheduler

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"backup-platform/internal/backup/domain"
	"backup-platform/pkg/uuid"
)

type mockMaintenanceRepo struct {
	lastSubset   map[uuid.UUID]int
	enqueuedJobs []*domain.MaintenanceJob
	activeRepos  []*domain.BackupRepository
}

func newMockMaintenanceRepo() *mockMaintenanceRepo {
	return &mockMaintenanceRepo{
		lastSubset: make(map[uuid.UUID]int),
	}
}

func (m *mockMaintenanceRepo) EnqueueMaintenanceJob(ctx context.Context, params domain.EnqueueMaintenanceJobParams) (*domain.MaintenanceJob, error) {
	if err := params.Validate(); err != nil {
		return nil, err
	}
	job := &domain.MaintenanceJob{
		ID:             uuid.New(),
		OrganizationID: params.OrganizationID,
		RepositoryID:   params.RepositoryID,
		OperationType:  params.OperationType,
		Status:         domain.MaintenanceJobPending,
		SnapshotID:     params.SnapshotID,
		SubsetIndex:    params.SubsetIndex,
		SubsetTotal:    params.SubsetTotal,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}
	m.enqueuedJobs = append(m.enqueuedJobs, job)
	return job, nil
}

func (m *mockMaintenanceRepo) ClaimNextMaintenanceJob(ctx context.Context, leaseDuration time.Duration) (*domain.MaintenanceJob, *domain.MaintenanceRun, error) {
	return nil, nil, nil
}

func (m *mockMaintenanceRepo) HeartbeatMaintenanceRun(ctx context.Context, runID uuid.UUID, leaseDuration time.Duration) error {
	return nil
}

func (m *mockMaintenanceRepo) CompleteMaintenanceJob(ctx context.Context, jobID, runID uuid.UUID, logsSummary []byte) error {
	return nil
}

func (m *mockMaintenanceRepo) FailMaintenanceJob(ctx context.Context, jobID, runID uuid.UUID, errMsg string, retryable bool) error {
	return nil
}

func (m *mockMaintenanceRepo) GetLastSuccessfulDeepCheckSubset(ctx context.Context, orgID, repoID uuid.UUID) (int, error) {
	return m.lastSubset[repoID], nil
}

func (m *mockMaintenanceRepo) GetMaintenanceJobByID(ctx context.Context, orgID, jobID uuid.UUID) (*domain.MaintenanceJob, error) {
	return nil, nil
}

func (m *mockMaintenanceRepo) GetMaintenanceRunByID(ctx context.Context, orgID, runID uuid.UUID) (*domain.MaintenanceRun, error) {
	return nil, nil
}

func (m *mockMaintenanceRepo) ListMaintenanceJobs(ctx context.Context, orgID, repoID uuid.UUID, limit int) ([]*domain.MaintenanceJob, error) {
	return nil, nil
}

func (m *mockMaintenanceRepo) ListMaintenanceRuns(ctx context.Context, orgID, jobID uuid.UUID) ([]*domain.MaintenanceRun, error) {
	return nil, nil
}

func (m *mockMaintenanceRepo) ListActiveRepositories(ctx context.Context, limit int) ([]*domain.BackupRepository, error) {
	return m.activeRepos, nil
}

func TestMaintenanceScheduler_DeterministicSubsetRotation(t *testing.T) {
	repo := newMockMaintenanceRepo()
	cfg := MaintenanceSchedulerConfig{
		PollInterval:     10 * time.Minute,
		TotalSubsets:     4,
		DeepCheckEnabled: true,
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	sched := NewMaintenanceScheduler(repo, cfg, logger)

	orgID := uuid.New()
	repoID := uuid.New()

	// 1. Initial run: no previous check (lastSubset=0) -> expect subset 1/4
	job1, err := sched.EnqueueNextDeepCheck(context.Background(), orgID, repoID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if *job1.SubsetIndex != 1 || *job1.SubsetTotal != 4 {
		t.Fatalf("expected 1/4, got %d/%d", *job1.SubsetIndex, *job1.SubsetTotal)
	}

	// 2. Simulate job1 success: record lastSubset = 1 -> expect subset 2/4
	repo.lastSubset[repoID] = 1
	job2, err := sched.EnqueueNextDeepCheck(context.Background(), orgID, repoID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if *job2.SubsetIndex != 2 || *job2.SubsetTotal != 4 {
		t.Fatalf("expected 2/4, got %d/%d", *job2.SubsetIndex, *job2.SubsetTotal)
	}

	// 3. Simulate job2 failure: lastSubset remains 1 -> expect subset 2/4 AGAIN
	job3, err := sched.EnqueueNextDeepCheck(context.Background(), orgID, repoID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if *job3.SubsetIndex != 2 || *job3.SubsetTotal != 4 {
		t.Fatalf("expected 2/4 on retry, got %d/%d", *job3.SubsetIndex, *job3.SubsetTotal)
	}

	// 4. Advance through 3 and 4
	repo.lastSubset[repoID] = 2
	job4, _ := sched.EnqueueNextDeepCheck(context.Background(), orgID, repoID)
	if *job4.SubsetIndex != 3 {
		t.Fatalf("expected 3/4, got %d", *job4.SubsetIndex)
	}

	repo.lastSubset[repoID] = 3
	job5, _ := sched.EnqueueNextDeepCheck(context.Background(), orgID, repoID)
	if *job5.SubsetIndex != 4 {
		t.Fatalf("expected 4/4, got %d", *job5.SubsetIndex)
	}

	// 5. Wrap around: lastSubset = 4 -> expect subset 1/4
	repo.lastSubset[repoID] = 4
	job6, _ := sched.EnqueueNextDeepCheck(context.Background(), orgID, repoID)
	if *job6.SubsetIndex != 1 {
		t.Fatalf("expected wrap around to 1/4, got %d", *job6.SubsetIndex)
	}
}

func TestMaintenanceScheduler_TickActiveRepositories(t *testing.T) {
	repo := newMockMaintenanceRepo()
	cfg := MaintenanceSchedulerConfig{
		PollInterval:     10 * time.Minute,
		TotalSubsets:     4,
		DeepCheckEnabled: true,
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	sched := NewMaintenanceScheduler(repo, cfg, logger)

	orgID := uuid.New()
	repoA := &domain.BackupRepository{
		ID:             uuid.New(),
		OrganizationID: orgID,
		Status:         domain.BackupRepositoryStatusActive,
	}
	repoB := &domain.BackupRepository{
		ID:             uuid.New(),
		OrganizationID: orgID,
		Status:         domain.BackupRepositoryStatusActive,
	}
	repo.activeRepos = []*domain.BackupRepository{repoA, repoB}

	sched.Tick(context.Background())

	if len(repo.enqueuedJobs) != 2 {
		t.Fatalf("expected 2 enqueued jobs for active repos, got %d", len(repo.enqueuedJobs))
	}
}
