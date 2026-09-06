package worker

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	auditDomain "backup-platform/internal/audit/domain"
	"backup-platform/internal/backup/domain"
	"backup-platform/internal/backup/restic"
	credDomain "backup-platform/internal/credential/domain"
	"backup-platform/pkg/uuid"
)

type mockMaintenanceWorkerRepo struct {
	mu           sync.Mutex
	jobs         map[uuid.UUID]*domain.MaintenanceJob
	runs         map[uuid.UUID]*domain.MaintenanceRun
	pendingQueue []*domain.MaintenanceJob
	tombstoned   map[uuid.UUID]bool
	repos        map[uuid.UUID]*domain.BackupRepository
	targets      map[uuid.UUID]*domain.StorageTarget
	artifacts    map[uuid.UUID]*domain.BackupArtifact
	completed    []uuid.UUID
	failed       []uuid.UUID
	retryable    map[uuid.UUID]bool
	phases       map[uuid.UUID]string
}

func newMockMaintenanceWorkerRepo() *mockMaintenanceWorkerRepo {
	return &mockMaintenanceWorkerRepo{
		jobs:       make(map[uuid.UUID]*domain.MaintenanceJob),
		runs:       make(map[uuid.UUID]*domain.MaintenanceRun),
		tombstoned: make(map[uuid.UUID]bool),
		repos:      make(map[uuid.UUID]*domain.BackupRepository),
		targets:    make(map[uuid.UUID]*domain.StorageTarget),
		artifacts:  make(map[uuid.UUID]*domain.BackupArtifact),
		retryable:  make(map[uuid.UUID]bool),
		phases:     make(map[uuid.UUID]string),
	}
}

func (m *mockMaintenanceWorkerRepo) EnqueueMaintenanceJob(ctx context.Context, params domain.EnqueueMaintenanceJobParams) (*domain.MaintenanceJob, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	job := &domain.MaintenanceJob{
		ID:             uuid.New(),
		OrganizationID: params.OrganizationID,
		RepositoryID:   params.RepositoryID,
		OperationType:  params.OperationType,
		Status:         domain.MaintenanceJobPending,
		ArtifactID:     params.ArtifactID,
		SnapshotID:     params.SnapshotID,
		SubsetIndex:    params.SubsetIndex,
		SubsetTotal:    params.SubsetTotal,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}
	m.jobs[job.ID] = job
	m.pendingQueue = append(m.pendingQueue, job)
	return job, nil
}

func (m *mockMaintenanceWorkerRepo) ClaimNextMaintenanceJob(ctx context.Context, leaseDuration time.Duration) (*domain.MaintenanceJob, *domain.MaintenanceRun, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.pendingQueue) == 0 {
		return nil, nil, nil
	}
	job := m.pendingQueue[0]
	m.pendingQueue = m.pendingQueue[1:]

	job.Status = domain.MaintenanceJobRunning
	run := &domain.MaintenanceRun{
		ID:             uuid.New(),
		OrganizationID: job.OrganizationID,
		JobID:          job.ID,
		AttemptNumber:  1,
		Status:         domain.MaintenanceRunRunning,
		StartedAt:      time.Now(),
		LeaseUntil:     time.Now().Add(leaseDuration),
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}
	m.runs[run.ID] = run
	return job, run, nil
}

func (m *mockMaintenanceWorkerRepo) HeartbeatMaintenanceRun(ctx context.Context, orgID, runID uuid.UUID, leaseDuration time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r, ok := m.runs[runID]; ok {
		r.HeartbeatAt = time.Now()
		r.LeaseUntil = time.Now().Add(leaseDuration)
	}
	return nil
}

func (m *mockMaintenanceWorkerRepo) CompleteMaintenanceJob(ctx context.Context, orgID, jobID, runID uuid.UUID, logsSummary []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if j, ok := m.jobs[jobID]; ok {
		j.Status = domain.MaintenanceJobCompleted
	}
	if r, ok := m.runs[runID]; ok {
		r.Status = domain.MaintenanceRunSuccess
		now := time.Now()
		r.EndedAt = &now
	}
	m.completed = append(m.completed, jobID)
	return nil
}

func (m *mockMaintenanceWorkerRepo) FailMaintenanceJob(ctx context.Context, orgID, jobID, runID uuid.UUID, errMsg string, retryable bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if j, ok := m.jobs[jobID]; ok {
		if retryable {
			j.Status = domain.MaintenanceJobPending
		} else {
			j.Status = domain.MaintenanceJobFailed
		}
	}
	if r, ok := m.runs[runID]; ok {
		r.Status = domain.MaintenanceRunFailed
		r.ErrorMessage = &errMsg
		now := time.Now()
		r.EndedAt = &now
	}
	m.failed = append(m.failed, jobID)
	m.retryable[jobID] = retryable
	return nil
}

func (m *mockMaintenanceWorkerRepo) UpdateJobPhase(ctx context.Context, orgID, jobID uuid.UUID, phase string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.phases[jobID] = phase
	if j, ok := m.jobs[jobID]; ok {
		j.Phase = &phase
	}
	return nil
}

func (m *mockMaintenanceWorkerRepo) FinalizeSuccessfulResticForget(ctx context.Context, orgID, jobID, runID, artifactID, repoID uuid.UUID, snapshotID string, logsSummary []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tombstoned[artifactID] = true
	if a, ok := m.artifacts[artifactID]; ok {
		a.IsDeleted = true
	}
	if j, ok := m.jobs[jobID]; ok {
		j.Status = domain.MaintenanceJobCompleted
	}
	if r, ok := m.runs[runID]; ok {
		r.Status = domain.MaintenanceRunSuccess
		now := time.Now()
		r.EndedAt = &now
	}
	m.completed = append(m.completed, jobID)

	// Enqueue debounced prune
	pruneJob := &domain.MaintenanceJob{
		ID:             uuid.New(),
		OrganizationID: orgID,
		RepositoryID:   repoID,
		OperationType:  domain.MaintenanceOpResticPrune,
		Status:         domain.MaintenanceJobPending,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}
	m.jobs[pruneJob.ID] = pruneJob
	m.pendingQueue = append(m.pendingQueue, pruneJob)
	return nil
}

func (m *mockMaintenanceWorkerRepo) GetLastSuccessfulDeepCheckSubset(ctx context.Context, orgID, repoID uuid.UUID) (int, error) {
	return 0, nil
}

func (m *mockMaintenanceWorkerRepo) GetDeepCheckDueStatus(ctx context.Context, orgID, repoID uuid.UUID, repoCreatedAt time.Time, dueInterval time.Duration, totalSubsets int) (due bool, nextSubset int, err error) {
	return true, 1, nil
}

func (m *mockMaintenanceWorkerRepo) RecoverInterruptedMaintenanceRuns(ctx context.Context) ([]domain.RecoveredMaintenanceRunInfo, error) {
	return nil, nil
}

func (m *mockMaintenanceWorkerRepo) ReapStaleMaintenanceRuns(ctx context.Context) ([]domain.RecoveredMaintenanceRunInfo, error) {
	return nil, nil
}

func (m *mockMaintenanceWorkerRepo) GetMaintenanceJobByID(ctx context.Context, orgID, jobID uuid.UUID) (*domain.MaintenanceJob, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.jobs[jobID], nil
}

func (m *mockMaintenanceWorkerRepo) GetMaintenanceRunByID(ctx context.Context, orgID, runID uuid.UUID) (*domain.MaintenanceRun, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.runs[runID], nil
}

func (m *mockMaintenanceWorkerRepo) ListMaintenanceJobs(ctx context.Context, orgID, repoID uuid.UUID, limit int) ([]*domain.MaintenanceJob, error) {
	return nil, nil
}

func (m *mockMaintenanceWorkerRepo) ListMaintenanceRuns(ctx context.Context, orgID, jobID uuid.UUID) ([]*domain.MaintenanceRun, error) {
	return nil, nil
}

func (m *mockMaintenanceWorkerRepo) ListActiveRepositories(ctx context.Context, limit int, afterCreatedAt *time.Time, afterID *uuid.UUID) ([]*domain.BackupRepository, error) {
	return nil, nil
}

func (m *mockMaintenanceWorkerRepo) TombstoneArtifact(ctx context.Context, orgID, artifactID uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tombstoned[artifactID] = true
	return nil
}

func (m *mockMaintenanceWorkerRepo) GetRepositoryByID(ctx context.Context, orgID, repoID uuid.UUID) (*domain.BackupRepository, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r, ok := m.repos[repoID]; ok {
		return r, nil
	}
	return nil, domain.ErrRepositoryNotFound
}

func (m *mockMaintenanceWorkerRepo) GetStorageTargetByID(ctx context.Context, orgID, targetID uuid.UUID) (*domain.StorageTarget, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t, ok := m.targets[targetID]; ok {
		return t, nil
	}
	return nil, domain.ErrStorageTargetNotFound
}

// Implement mock backup repo minimal methods
func (m *mockMaintenanceWorkerRepo) EnsureDefaultLocalStorageTarget(ctx context.Context, orgID uuid.UUID) (*domain.StorageTarget, error) {
	return nil, nil
}
func (m *mockMaintenanceWorkerRepo) CreateStorageTarget(ctx context.Context, target *domain.StorageTarget) (*domain.StorageTarget, error) {
	return target, nil
}
func (m *mockMaintenanceWorkerRepo) ListStorageTargets(ctx context.Context, orgID uuid.UUID) ([]*domain.StorageTarget, error) {
	return nil, nil
}
func (m *mockMaintenanceWorkerRepo) UpdateStorageTarget(ctx context.Context, target *domain.StorageTarget) (*domain.StorageTarget, error) {
	return target, nil
}
func (m *mockMaintenanceWorkerRepo) DeleteStorageTarget(ctx context.Context, orgID, targetID uuid.UUID) error {
	return nil
}
func (m *mockMaintenanceWorkerRepo) CountArtifactsByStorageTarget(ctx context.Context, orgID, targetID uuid.UUID) (int64, error) {
	return 0, nil
}
func (m *mockMaintenanceWorkerRepo) CountPlansByStorageTarget(ctx context.Context, orgID, targetID uuid.UUID) (int64, error) {
	return 0, nil
}
func (m *mockMaintenanceWorkerRepo) CountActiveJobsByStorageTarget(ctx context.Context, orgID, targetID uuid.UUID) (int64, error) {
	return 0, nil
}
func (m *mockMaintenanceWorkerRepo) CountRepositoriesByStorageTarget(ctx context.Context, orgID, targetID uuid.UUID) (int64, error) {
	return 0, nil
}
func (m *mockMaintenanceWorkerRepo) GetPlanByID(ctx context.Context, orgID, planID uuid.UUID) (*domain.BackupPlan, error) {
	return nil, nil
}
func (m *mockMaintenanceWorkerRepo) CreateJob(ctx context.Context, job *domain.BackupJob) (*domain.BackupJob, error) {
	return job, nil
}
func (m *mockMaintenanceWorkerRepo) GetJobByID(ctx context.Context, orgID, jobID uuid.UUID) (*domain.BackupJob, error) {
	return nil, nil
}
func (m *mockMaintenanceWorkerRepo) GetActiveManualJobForResource(ctx context.Context, orgID, resourceID uuid.UUID) (*domain.BackupJob, error) {
	return nil, nil
}
func (m *mockMaintenanceWorkerRepo) GetActiveJobConflictForResource(ctx context.Context, orgID, resourceID uuid.UUID) (*domain.BackupJob, error) {
	return nil, nil
}
func (m *mockMaintenanceWorkerRepo) FindPendingJobs(ctx context.Context, limit int, afterCreatedAt *time.Time, afterID *uuid.UUID) ([]*domain.BackupJob, error) {
	return nil, nil
}
func (m *mockMaintenanceWorkerRepo) TransactionalClaimJob(ctx context.Context, orgID, jobID uuid.UUID) (*domain.BackupRun, *domain.BackupJob, error) {
	return nil, nil, nil
}
func (m *mockMaintenanceWorkerRepo) GetRunByID(ctx context.Context, orgID, runID uuid.UUID) (*domain.BackupRun, error) {
	return nil, nil
}
func (m *mockMaintenanceWorkerRepo) GetRunDetail(ctx context.Context, orgID, runID uuid.UUID) (*domain.BackupRunWithStats, error) {
	return nil, nil
}
func (m *mockMaintenanceWorkerRepo) ListRuns(ctx context.Context, orgID uuid.UUID, filter domain.RunFilter) ([]*domain.BackupRunWithStats, error) {
	return nil, nil
}
func (m *mockMaintenanceWorkerRepo) ListSuccessfulRunsForPlan(ctx context.Context, orgID, planID uuid.UUID) ([]*domain.BackupRun, error) {
	return nil, nil
}
func (m *mockMaintenanceWorkerRepo) GetLatestRunForJob(ctx context.Context, orgID, jobID uuid.UUID) (*domain.BackupRun, error) {
	return nil, nil
}
func (m *mockMaintenanceWorkerRepo) UpdateHeartbeat(ctx context.Context, orgID, runID uuid.UUID) error {
	return nil
}
func (m *mockMaintenanceWorkerRepo) FinalizeRunAndJob(ctx context.Context, orgID, runID, jobID uuid.UUID, runStatus domain.RunStatus, jobStatus domain.JobStatus, errMsg *string, logsSummary []byte) error {
	return nil
}
func (m *mockMaintenanceWorkerRepo) CreateArtifact(ctx context.Context, artifact *domain.BackupArtifact) (*domain.BackupArtifact, error) {
	return artifact, nil
}
func (m *mockMaintenanceWorkerRepo) GetArtifactByID(ctx context.Context, orgID, artifactID uuid.UUID) (*domain.BackupArtifact, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if a, ok := m.artifacts[artifactID]; ok {
		return a, nil
	}
	return nil, domain.ErrArtifactNotFound
}
func (m *mockMaintenanceWorkerRepo) ListArtifacts(ctx context.Context, orgID uuid.UUID) ([]*domain.BackupArtifact, error) {
	return nil, nil
}
func (m *mockMaintenanceWorkerRepo) UpdateArtifactVerification(ctx context.Context, orgID, artifactID uuid.UUID, status domain.VerificationStatus, details *string) error {
	return nil
}
func (m *mockMaintenanceWorkerRepo) GetRunArtifacts(ctx context.Context, orgID, runID uuid.UUID) ([]*domain.BackupArtifact, error) {
	return nil, nil
}
func (m *mockMaintenanceWorkerRepo) RecoverInterruptedRuns(ctx context.Context) ([]domain.RecoveredRunInfo, error) {
	return nil, nil
}
func (m *mockMaintenanceWorkerRepo) ReapStaleRuns(ctx context.Context) ([]domain.RecoveredRunInfo, error) {
	return nil, nil
}
func (m *mockMaintenanceWorkerRepo) CreateRepository(ctx context.Context, repo *domain.BackupRepository) (*domain.BackupRepository, error) {
	return repo, nil
}
func (m *mockMaintenanceWorkerRepo) GetRepositoryByResourceID(ctx context.Context, orgID, resourceID uuid.UUID) (*domain.BackupRepository, error) {
	return nil, nil
}

// Mock test runner
type mockMaintenanceResticRunner struct {
	forgottenSnapshots []string
	pruned             bool
	deepCheckSubsets   []string
	forgetErr          error
	pruneErr           error
	checkErr           error
	verifyAbsentErr    error
}

func (r *mockMaintenanceResticRunner) Init(ctx context.Context, target restic.RepositoryTarget, password []byte) error {
	return nil
}
func (r *mockMaintenanceResticRunner) Probe(ctx context.Context, target restic.RepositoryTarget, password []byte) error {
	return nil
}
func (r *mockMaintenanceResticRunner) Version(ctx context.Context) (string, error) {
	return "restic 0.19.1", nil
}
func (r *mockMaintenanceResticRunner) ValidateVersion(ctx context.Context) error {
	return nil
}
func (r *mockMaintenanceResticRunner) GetSnapshot(ctx context.Context, target restic.RepositoryTarget, password []byte, snapshotID string) (*restic.SnapshotItem, error) {
	return nil, nil
}
func (r *mockMaintenanceResticRunner) ListSnapshotNodes(ctx context.Context, target restic.RepositoryTarget, password []byte, snapshotID string) ([]restic.SnapshotNode, error) {
	return nil, nil
}
func (r *mockMaintenanceResticRunner) DumpSample(ctx context.Context, target restic.RepositoryTarget, password []byte, snapshotID, internalFilename string, maxBytes int) ([]byte, error) {
	return nil, nil
}
func (r *mockMaintenanceResticRunner) DumpStream(ctx context.Context, target restic.RepositoryTarget, password []byte, snapshotID, internalFilename string) (io.ReadCloser, error) {
	return nil, nil
}
func (r *mockMaintenanceResticRunner) ForgetSnapshot(ctx context.Context, target restic.RepositoryTarget, password []byte, snapshotID string) error {
	if r.forgetErr != nil {
		return r.forgetErr
	}
	r.forgottenSnapshots = append(r.forgottenSnapshots, snapshotID)
	return nil
}
func (r *mockMaintenanceResticRunner) Prune(ctx context.Context, target restic.RepositoryTarget, password []byte) error {
	if r.pruneErr != nil {
		return r.pruneErr
	}
	r.pruned = true
	return nil
}
func (r *mockMaintenanceResticRunner) CheckSubset(ctx context.Context, target restic.RepositoryTarget, password []byte, subsetIndex, subsetTotal int) error {
	if r.checkErr != nil {
		return r.checkErr
	}
	r.deepCheckSubsets = append(r.deepCheckSubsets, "")
	return nil
}
func (r *mockMaintenanceResticRunner) VerifySnapshotAbsent(ctx context.Context, target restic.RepositoryTarget, password []byte, snapshotID string) error {
	if r.verifyAbsentErr != nil {
		return r.verifyAbsentErr
	}
	return nil
}

type mockVault struct {
	key []byte
}

func (v *mockVault) LoadCredentialForUse(ctx context.Context, orgID, credID uuid.UUID) (credDomain.Type, []byte, error) {
	copyKey := make([]byte, len(v.key))
	copy(copyKey, v.key)
	return credDomain.TypeResticRepositoryKey, copyKey, nil
}

type mockTargetResolver struct{}

func (tr *mockTargetResolver) ResolveTarget(ctx context.Context, orgID, resourceID uuid.UUID, target *domain.StorageTarget) (restic.RepositoryTarget, error) {
	return &fakeResticRepoTarget{}, nil
}

type mockAuditRec struct {
	mu   sync.Mutex
	logs []*auditDomain.AuditLog
}

func (a *mockAuditRec) Record(ctx context.Context, entry *auditDomain.AuditLog) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.logs = append(a.logs, entry)
	return nil
}

func TestRepositoryMaintenanceWorker_ForgetPruneDeepCheck(t *testing.T) {
	orgID := uuid.New()
	repoID := uuid.New()
	targetID := uuid.New()
	credID := uuid.New()
	resID := uuid.New()
	artID := uuid.New()
	snapID := "1111222233334444555566667777888899990000aaaabbbbccccddddeeeeffff"

	repo := newMockMaintenanceWorkerRepo()
	repo.repos[repoID] = &domain.BackupRepository{
		ID:                repoID,
		OrganizationID:    orgID,
		ResourceID:        resID,
		StorageTargetID:   targetID,
		CredentialID:      credID,
		RepositoryLocator: "/tmp/fake-repo",
		Status:            domain.BackupRepositoryStatusActive,
	}
	repo.targets[targetID] = &domain.StorageTarget{
		ID:             targetID,
		OrganizationID: orgID,
		Status:         domain.StorageTargetStatusActive,
	}
	repo.artifacts[artID] = &domain.BackupArtifact{
		ID:              artID,
		OrganizationID:  orgID,
		ResourceID:      resID,
		StorageTargetID: targetID,
		RepositoryID:    &repoID,
		SnapshotID:      snapID,
		Format:          domain.ArtifactFormatResticSnapshot,
		IsDeleted:       false,
	}

	runner := &mockMaintenanceResticRunner{}
	vault := &mockVault{key: []byte("test-repo-password-123")}
	coord := restic.NewRepositoryOperationCoordinator()
	audit := &mockAuditRec{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	worker := NewRepositoryMaintenanceWorker(
		repo,
		repo,
		repo,
		vault,
		&mockTargetResolver{},
		runner,
		coord,
		audit,
		DefaultMaintenanceWorkerConfig(),
		logger,
	)

	// 1. Enqueue Forget Job
	_, err := repo.EnqueueMaintenanceJob(context.Background(), domain.EnqueueMaintenanceJobParams{
		OrganizationID: orgID,
		RepositoryID:   repoID,
		OperationType:  domain.MaintenanceOpResticForget,
		ArtifactID:     &artID,
		SnapshotID:     snapID,
	})
	if err != nil {
		t.Fatalf("failed enqueuing forget job: %v", err)
	}

	// 2. Process Forget Job
	processed := worker.ProcessNextJob(context.Background())
	if !processed {
		t.Fatalf("expected job to be processed")
	}

	// Verify forget was called on runner
	if len(runner.forgottenSnapshots) != 1 || runner.forgottenSnapshots[0] != snapID {
		t.Fatalf("expected runner to forget snapshot %s, got %v", snapID, runner.forgottenSnapshots)
	}

	// Verify artifact was tombstoned
	if !repo.tombstoned[artID] {
		t.Fatalf("expected artifact to be tombstoned after forget")
	}

	// Verify audit log emitted
	if len(audit.logs) != 1 {
		t.Fatalf("expected 1 audit log, got %d", len(audit.logs))
	}

	// Verify debounced prune was automatically enqueued!
	if len(repo.pendingQueue) != 1 {
		t.Fatalf("expected post-forget prune to be queued, queue size: %d", len(repo.pendingQueue))
	}
	pruneJob := repo.pendingQueue[0]
	if pruneJob.OperationType != domain.MaintenanceOpResticPrune {
		t.Fatalf("expected queued job to be restic_prune, got %s", pruneJob.OperationType)
	}

	// 3. Process Prune Job
	processed = worker.ProcessNextJob(context.Background())
	if !processed {
		t.Fatalf("expected prune job to be processed")
	}
	if !runner.pruned {
		t.Fatalf("expected runner.Prune to be called")
	}

	// 4. Enqueue & Process Deep Check Job
	subIdx := 2
	subTot := 4
	_, err = repo.EnqueueMaintenanceJob(context.Background(), domain.EnqueueMaintenanceJobParams{
		OrganizationID: orgID,
		RepositoryID:   repoID,
		OperationType:  domain.MaintenanceOpResticDeepCheck,
		SubsetIndex:    &subIdx,
		SubsetTotal:    &subTot,
	})
	if err != nil {
		t.Fatalf("failed enqueuing deep check: %v", err)
	}

	processed = worker.ProcessNextJob(context.Background())
	if !processed {
		t.Fatalf("expected deep check to be processed")
	}
	if len(runner.deepCheckSubsets) != 1 {
		t.Fatalf("expected runner.CheckSubset to be called")
	}
}

func TestRepositoryMaintenanceWorker_YieldsWhenBusy(t *testing.T) {
	orgID := uuid.New()
	repoID := uuid.New()
	targetID := uuid.New()
	credID := uuid.New()
	resID := uuid.New()
	artID := uuid.New()
	snapID := "1111222233334444555566667777888899990000aaaabbbbccccddddeeeeffff"

	repo := newMockMaintenanceWorkerRepo()
	repo.repos[repoID] = &domain.BackupRepository{
		ID:                repoID,
		OrganizationID:    orgID,
		ResourceID:        resID,
		StorageTargetID:   targetID,
		CredentialID:      credID,
		RepositoryLocator: "/tmp/fake-repo",
		Status:            domain.BackupRepositoryStatusActive,
	}
	repo.targets[targetID] = &domain.StorageTarget{
		ID:             targetID,
		OrganizationID: orgID,
		Status:         domain.StorageTargetStatusActive,
	}
	repo.artifacts[artID] = &domain.BackupArtifact{
		ID:              artID,
		OrganizationID:  orgID,
		ResourceID:      resID,
		StorageTargetID: targetID,
		RepositoryID:    &repoID,
		SnapshotID:      snapID,
		Format:          domain.ArtifactFormatResticSnapshot,
		IsDeleted:       false,
	}

	runner := &mockMaintenanceResticRunner{}
	vault := &mockVault{key: []byte("test-repo-password-123")}
	coord := restic.NewRepositoryOperationCoordinator()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// Simulate an ongoing shared operation (e.g. backup or restore) holding the repo lock
	sharedRelease, err := coord.AcquireShared(context.Background(), repoID)
	if err != nil {
		t.Fatalf("failed acquiring shared lock: %v", err)
	}
	defer sharedRelease()

	cfg := DefaultMaintenanceWorkerConfig()
	cfg.CoordinatorTimeout = 20 * time.Millisecond // Short timeout for test

	worker := NewRepositoryMaintenanceWorker(
		repo,
		repo,
		repo,
		vault,
		&mockTargetResolver{},
		runner,
		coord,
		nil,
		cfg,
		logger,
	)

	// Enqueue maintenance job
	job, _ := repo.EnqueueMaintenanceJob(context.Background(), domain.EnqueueMaintenanceJobParams{
		OrganizationID: orgID,
		RepositoryID:   repoID,
		OperationType:  domain.MaintenanceOpResticForget,
		ArtifactID:     &artID,
		SnapshotID:     snapID,
	})

	// Process job -> should yield because repository is busy
	processed := worker.ProcessNextJob(context.Background())
	if !processed {
		t.Fatalf("expected job to be processed (claimed and evaluated)")
	}

	// Verify runner was NOT called
	if len(runner.forgottenSnapshots) != 0 {
		t.Fatalf("runner should not be called while repository is locked by shared operations")
	}

	// Verify job was marked retryable so customer backups are not starved!
	if !repo.retryable[job.ID] {
		t.Fatalf("expected job to be marked retryable on busy yield")
	}
}

func TestRepositoryMaintenanceWorker_InvariantFailures(t *testing.T) {
	orgID := uuid.New()
	repoID := uuid.New()
	targetID := uuid.New()
	credID := uuid.New()
	resID := uuid.New()
	validSnapID := "1111222233334444555566667777888899990000aaaabbbbccccddddeeeeffff"

	baseSetup := func() (*mockMaintenanceWorkerRepo, *mockMaintenanceResticRunner, *RepositoryMaintenanceWorker) {
		repo := newMockMaintenanceWorkerRepo()
		repo.repos[repoID] = &domain.BackupRepository{
			ID:                repoID,
			OrganizationID:    orgID,
			ResourceID:        resID,
			StorageTargetID:   targetID,
			CredentialID:      credID,
			RepositoryLocator: "/tmp/fake-repo",
			Status:            domain.BackupRepositoryStatusActive,
		}
		repo.targets[targetID] = &domain.StorageTarget{
			ID:             targetID,
			OrganizationID: orgID,
			Status:         domain.StorageTargetStatusActive,
		}
		runner := &mockMaintenanceResticRunner{}
		vault := &mockVault{key: []byte("test-repo-password-123")}
		coord := restic.NewRepositoryOperationCoordinator()
		logger := slog.New(slog.NewTextHandler(io.Discard, nil))
		w := NewRepositoryMaintenanceWorker(repo, repo, repo, vault, &mockTargetResolver{}, runner, coord, nil, DefaultMaintenanceWorkerConfig(), logger)
		return repo, runner, w
	}

	testCases := []struct {
		name   string
		mutate func(repo *mockMaintenanceWorkerRepo, artID uuid.UUID) (jobArtID *uuid.UUID, snapID string)
	}{
		{
			name: "Invariant A: Nil artifact_id",
			mutate: func(repo *mockMaintenanceWorkerRepo, artID uuid.UUID) (*uuid.UUID, string) {
				return nil, validSnapID
			},
		},
		{
			name: "Invariant B: Unknown artifact not in DB",
			mutate: func(repo *mockMaintenanceWorkerRepo, artID uuid.UUID) (*uuid.UUID, string) {
				// Don't add to repo.artifacts
				return &artID, validSnapID
			},
		},
		{
			name: "Invariant C: Organization mismatch",
			mutate: func(repo *mockMaintenanceWorkerRepo, artID uuid.UUID) (*uuid.UUID, string) {
				otherOrg := uuid.New()
				repo.artifacts[artID] = &domain.BackupArtifact{
					ID:              artID,
					OrganizationID:  otherOrg,
					ResourceID:      resID,
					StorageTargetID: targetID,
					RepositoryID:    &repoID,
					SnapshotID:      validSnapID,
					Format:          domain.ArtifactFormatResticSnapshot,
				}
				return &artID, validSnapID
			},
		},
		{
			name: "Invariant D: ID mismatch",
			mutate: func(repo *mockMaintenanceWorkerRepo, artID uuid.UUID) (*uuid.UUID, string) {
				otherID := uuid.New()
				repo.artifacts[artID] = &domain.BackupArtifact{
					ID:              otherID,
					OrganizationID:  orgID,
					ResourceID:      resID,
					StorageTargetID: targetID,
					RepositoryID:    &repoID,
					SnapshotID:      validSnapID,
					Format:          domain.ArtifactFormatResticSnapshot,
				}
				return &artID, validSnapID
			},
		},
		{
			name: "Invariant E: Format mismatch (not restic_snapshot)",
			mutate: func(repo *mockMaintenanceWorkerRepo, artID uuid.UUID) (*uuid.UUID, string) {
				repo.artifacts[artID] = &domain.BackupArtifact{
					ID:              artID,
					OrganizationID:  orgID,
					ResourceID:      resID,
					StorageTargetID: targetID,
					RepositoryID:    &repoID,
					SnapshotID:      validSnapID,
					Format:          domain.ArtifactFormatSQLGzip,
				}
				return &artID, validSnapID
			},
		},
		{
			name: "Invariant F: Already deleted / tombstoned",
			mutate: func(repo *mockMaintenanceWorkerRepo, artID uuid.UUID) (*uuid.UUID, string) {
				repo.artifacts[artID] = &domain.BackupArtifact{
					ID:              artID,
					OrganizationID:  orgID,
					ResourceID:      resID,
					StorageTargetID: targetID,
					RepositoryID:    &repoID,
					SnapshotID:      validSnapID,
					Format:          domain.ArtifactFormatResticSnapshot,
					IsDeleted:       true,
				}
				return &artID, validSnapID
			},
		},
		{
			name: "Invariant G: Repository ID mismatch",
			mutate: func(repo *mockMaintenanceWorkerRepo, artID uuid.UUID) (*uuid.UUID, string) {
				otherRepo := uuid.New()
				repo.artifacts[artID] = &domain.BackupArtifact{
					ID:              artID,
					OrganizationID:  orgID,
					ResourceID:      resID,
					StorageTargetID: targetID,
					RepositoryID:    &otherRepo,
					SnapshotID:      validSnapID,
					Format:          domain.ArtifactFormatResticSnapshot,
				}
				return &artID, validSnapID
			},
		},
		{
			name: "Invariant H: Snapshot ID mismatch",
			mutate: func(repo *mockMaintenanceWorkerRepo, artID uuid.UUID) (*uuid.UUID, string) {
				repo.artifacts[artID] = &domain.BackupArtifact{
					ID:              artID,
					OrganizationID:  orgID,
					ResourceID:      resID,
					StorageTargetID: targetID,
					RepositoryID:    &repoID,
					SnapshotID:      "2222222233334444555566667777888899990000aaaabbbbccccddddeeeeffff",
					Format:          domain.ArtifactFormatResticSnapshot,
				}
				return &artID, validSnapID
			},
		},
		{
			name: "Invariant I: Resource ID mismatch with repository",
			mutate: func(repo *mockMaintenanceWorkerRepo, artID uuid.UUID) (*uuid.UUID, string) {
				otherRes := uuid.New()
				repo.artifacts[artID] = &domain.BackupArtifact{
					ID:              artID,
					OrganizationID:  orgID,
					ResourceID:      otherRes,
					StorageTargetID: targetID,
					RepositoryID:    &repoID,
					SnapshotID:      validSnapID,
					Format:          domain.ArtifactFormatResticSnapshot,
				}
				return &artID, validSnapID
			},
		},
		{
			name: "Invariant J: Storage target mismatch with repository",
			mutate: func(repo *mockMaintenanceWorkerRepo, artID uuid.UUID) (*uuid.UUID, string) {
				otherTarget := uuid.New()
				repo.artifacts[artID] = &domain.BackupArtifact{
					ID:              artID,
					OrganizationID:  orgID,
					ResourceID:      resID,
					StorageTargetID: otherTarget,
					RepositoryID:    &repoID,
					SnapshotID:      validSnapID,
					Format:          domain.ArtifactFormatResticSnapshot,
				}
				return &artID, validSnapID
			},
		},
		{
			name: "Invariant K: Non-canonical hex snapshot ID",
			mutate: func(repo *mockMaintenanceWorkerRepo, artID uuid.UUID) (*uuid.UUID, string) {
				nonHex := "not-a-valid-canonical-64-hex-id-too-short"
				repo.artifacts[artID] = &domain.BackupArtifact{
					ID:              artID,
					OrganizationID:  orgID,
					ResourceID:      resID,
					StorageTargetID: targetID,
					RepositoryID:    &repoID,
					SnapshotID:      nonHex,
					Format:          domain.ArtifactFormatResticSnapshot,
				}
				return &artID, nonHex
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			repo, runner, w := baseSetup()
			artID := uuid.New()
			jobArtID, snapID := tc.mutate(repo, artID)

			job := &domain.MaintenanceJob{
				ID:             uuid.New(),
				OrganizationID: orgID,
				RepositoryID:   repoID,
				OperationType:  domain.MaintenanceOpResticForget,
				Status:         domain.MaintenanceJobPending,
				ArtifactID:     jobArtID,
				SnapshotID:     snapID,
				CreatedAt:      time.Now(),
				UpdatedAt:      time.Now(),
			}
			repo.jobs[job.ID] = job
			repo.pendingQueue = append(repo.pendingQueue, job)

			processed := w.ProcessNextJob(context.Background())
			if !processed {
				t.Fatalf("expected job to be claimed and evaluated")
			}

			// Invariant failure MUST fail closed:
			// 1. runner.ForgetSnapshot must NEVER be called
			if len(runner.forgottenSnapshots) != 0 {
				t.Fatalf("runner.ForgetSnapshot must not be called when invariant fails")
			}
			// 2. Job must be marked failed permanently (NOT retryable)
			if repo.retryable[job.ID] {
				t.Fatalf("expected invariant failure to be terminal (not retryable)")
			}
			if repo.jobs[job.ID].Status != domain.MaintenanceJobFailed {
				t.Fatalf("expected job status failed, got %s", repo.jobs[job.ID].Status)
			}
		})
	}
}

func TestRepositoryMaintenanceWorker_PhaseForgetExecutedRecovery(t *testing.T) {
	orgID := uuid.New()
	repoID := uuid.New()
	targetID := uuid.New()
	credID := uuid.New()
	resID := uuid.New()
	artID := uuid.New()
	snapID := "1111222233334444555566667777888899990000aaaabbbbccccddddeeeeffff"

	repo := newMockMaintenanceWorkerRepo()
	repo.repos[repoID] = &domain.BackupRepository{
		ID:                repoID,
		OrganizationID:    orgID,
		ResourceID:        resID,
		StorageTargetID:   targetID,
		CredentialID:      credID,
		RepositoryLocator: "/tmp/fake-repo",
		Status:            domain.BackupRepositoryStatusActive,
	}
	repo.targets[targetID] = &domain.StorageTarget{
		ID:             targetID,
		OrganizationID: orgID,
		Status:         domain.StorageTargetStatusActive,
	}
	repo.artifacts[artID] = &domain.BackupArtifact{
		ID:              artID,
		OrganizationID:  orgID,
		ResourceID:      resID,
		StorageTargetID: targetID,
		RepositoryID:    &repoID,
		SnapshotID:      snapID,
		Format:          domain.ArtifactFormatResticSnapshot,
		IsDeleted:       false,
	}

	runner := &mockMaintenanceResticRunner{}
	vault := &mockVault{key: []byte("test-repo-password-123")}
	coord := restic.NewRepositoryOperationCoordinator()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	audit := &mockAuditRec{}

	worker := NewRepositoryMaintenanceWorker(repo, repo, repo, vault, &mockTargetResolver{}, runner, coord, audit, DefaultMaintenanceWorkerConfig(), logger)

	// Enqueue a job that already reached Phase = forget_executed before crashing/interrupting
	phase := domain.MaintenancePhaseForgetExecuted
	job := &domain.MaintenanceJob{
		ID:             uuid.New(),
		OrganizationID: orgID,
		RepositoryID:   repoID,
		OperationType:  domain.MaintenanceOpResticForget,
		Status:         domain.MaintenanceJobPending,
		ArtifactID:     &artID,
		SnapshotID:     snapID,
		Phase:          &phase,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}
	repo.jobs[job.ID] = job
	repo.pendingQueue = append(repo.pendingQueue, job)

	processed := worker.ProcessNextJob(context.Background())
	if !processed {
		t.Fatalf("expected job to be processed")
	}

	// Worker should NOT call ForgetSnapshot again (idempotent skip)
	if len(runner.forgottenSnapshots) != 0 {
		t.Fatalf("expected ForgetSnapshot to be skipped when phase is forget_executed, got called %d times", len(runner.forgottenSnapshots))
	}

	// But it SHOULD complete the atomic DB finalization
	if !repo.tombstoned[artID] {
		t.Fatalf("expected artifact to be tombstoned on resume")
	}
	if repo.jobs[job.ID].Status != domain.MaintenanceJobCompleted {
		t.Fatalf("expected job status completed, got %s", repo.jobs[job.ID].Status)
	}

	// Debounced prune should be queued
	if len(repo.pendingQueue) != 1 || repo.pendingQueue[0].OperationType != domain.MaintenanceOpResticPrune {
		t.Fatalf("expected prune to be queued after forget finalization")
	}
}

func TestRepositoryMaintenanceWorker_AuditLogging(t *testing.T) {
	orgID := uuid.New()
	repoID := uuid.New()
	targetID := uuid.New()
	credID := uuid.New()
	resID := uuid.New()
	artID := uuid.New()
	snapID := "1111222233334444555566667777888899990000aaaabbbbccccddddeeeeffff"

	repo := newMockMaintenanceWorkerRepo()
	repo.repos[repoID] = &domain.BackupRepository{
		ID:                repoID,
		OrganizationID:    orgID,
		ResourceID:        resID,
		StorageTargetID:   targetID,
		CredentialID:      credID,
		RepositoryLocator: "/tmp/fake-repo",
		Status:            domain.BackupRepositoryStatusActive,
	}
	repo.targets[targetID] = &domain.StorageTarget{
		ID:             targetID,
		OrganizationID: orgID,
		Status:         domain.StorageTargetStatusActive,
	}
	repo.artifacts[artID] = &domain.BackupArtifact{
		ID:              artID,
		OrganizationID:  orgID,
		ResourceID:      resID,
		StorageTargetID: targetID,
		RepositoryID:    &repoID,
		SnapshotID:      snapID,
		Format:          domain.ArtifactFormatResticSnapshot,
		IsDeleted:       false,
	}

	runner := &mockMaintenanceResticRunner{}
	vault := &mockVault{key: []byte("test-repo-password-123")}
	coord := restic.NewRepositoryOperationCoordinator()
	audit := &mockAuditRec{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	worker := NewRepositoryMaintenanceWorker(repo, repo, repo, vault, &mockTargetResolver{}, runner, coord, audit, DefaultMaintenanceWorkerConfig(), logger)

	// 1. Successful Forget
	_, _ = repo.EnqueueMaintenanceJob(context.Background(), domain.EnqueueMaintenanceJobParams{
		OrganizationID: orgID,
		RepositoryID:   repoID,
		OperationType:  domain.MaintenanceOpResticForget,
		ArtifactID:     &artID,
		SnapshotID:     snapID,
	})
	worker.ProcessNextJob(context.Background())

	if len(audit.logs) != 1 {
		t.Fatalf("expected 1 audit log, got %d", len(audit.logs))
	}
	forgetAudit := audit.logs[0]
	if forgetAudit.Action != auditDomain.ActionRetentionCleanup {
		t.Fatalf("expected ActionRetentionCleanup, got %s", forgetAudit.Action)
	}
	if forgetAudit.UserID != nil {
		t.Fatalf("expected system maintenance audit log to have nil UserID")
	}

	// 2. Prune Failure Audit
	runner.pruneErr = errors.New("simulated prune failure")
	worker.ProcessNextJob(context.Background()) // pops the queued prune job

	if len(audit.logs) != 2 {
		t.Fatalf("expected 2 audit logs, got %d", len(audit.logs))
	}
	pruneFailAudit := audit.logs[1]
	if pruneFailAudit.Action != auditDomain.ActionMaintenancePruneFail {
		t.Fatalf("expected ActionMaintenancePruneFail, got %s", pruneFailAudit.Action)
	}
	if pruneFailAudit.UserID != nil {
		t.Fatalf("expected system prune failure audit log to have nil UserID")
	}
}
