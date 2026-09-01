package worker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"backup-platform/internal/backup/domain"
	"backup-platform/internal/backup/repository"
	"backup-platform/internal/storage"
	"backup-platform/internal/storage/local"
	"backup-platform/pkg/uuid"
)

type mockRecoveryRepo struct {
	mu           sync.Mutex
	runs         map[uuid.UUID]*domain.BackupRun
	jobs         map[uuid.UUID]*domain.BackupJob
	artifacts    map[uuid.UUID][]*domain.BackupArtifact
	tombstoned   map[uuid.UUID]bool
	tombstoneErr error
	recoverErr   error
	reapErr      error
}

func newMockRecoveryRepo() *mockRecoveryRepo {
	return &mockRecoveryRepo{
		runs:       make(map[uuid.UUID]*domain.BackupRun),
		jobs:       make(map[uuid.UUID]*domain.BackupJob),
		artifacts:  make(map[uuid.UUID][]*domain.BackupArtifact),
		tombstoned: make(map[uuid.UUID]bool),
	}
}

func (m *mockRecoveryRepo) EnsureDefaultLocalStorageTarget(ctx context.Context, orgID uuid.UUID) (*domain.StorageTarget, error) {
	return nil, nil
}
func (m *mockRecoveryRepo) GetStorageTargetByID(ctx context.Context, orgID, targetID uuid.UUID) (*domain.StorageTarget, error) {
	return nil, nil
}
func (m *mockRecoveryRepo) GetPlanByID(ctx context.Context, orgID, planID uuid.UUID) (*domain.BackupPlan, error) {
	return nil, nil
}
func (m *mockRecoveryRepo) CreateJob(ctx context.Context, job *domain.BackupJob) (*domain.BackupJob, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.jobs[job.ID] = job
	return job, nil
}
func (m *mockRecoveryRepo) GetJobByID(ctx context.Context, orgID, jobID uuid.UUID) (*domain.BackupJob, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[jobID]
	if !ok || j.OrganizationID != orgID {
		return nil, domain.ErrJobNotFound
	}
	return j, nil
}
func (m *mockRecoveryRepo) GetActiveManualJobForResource(ctx context.Context, orgID, resourceID uuid.UUID) (*domain.BackupJob, error) {
	return nil, nil
}
func (m *mockRecoveryRepo) GetActiveJobConflictForResource(ctx context.Context, orgID, resourceID uuid.UUID) (*domain.BackupJob, error) {
	return nil, nil
}
func (m *mockRecoveryRepo) FindPendingJobs(ctx context.Context, limit int, afterCreatedAt *time.Time, afterID *uuid.UUID) ([]*domain.BackupJob, error) {
	return nil, nil
}
func (m *mockRecoveryRepo) TransactionalClaimJob(ctx context.Context, orgID, jobID uuid.UUID) (*domain.BackupRun, *domain.BackupJob, error) {
	return nil, nil, nil
}
func (m *mockRecoveryRepo) GetRunByID(ctx context.Context, orgID, runID uuid.UUID) (*domain.BackupRun, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.runs[runID]
	if !ok || r.OrganizationID != orgID {
		return nil, domain.ErrRunNotFound
	}
	return r, nil
}
func (m *mockRecoveryRepo) GetRunDetail(ctx context.Context, orgID, runID uuid.UUID) (*domain.BackupRunWithStats, error) {
	return nil, nil
}
func (m *mockRecoveryRepo) ListRuns(ctx context.Context, orgID uuid.UUID, filter domain.RunFilter) ([]*domain.BackupRunWithStats, error) {
	return nil, nil
}
func (m *mockRecoveryRepo) ListSuccessfulRunsForPlan(ctx context.Context, orgID, planID uuid.UUID) ([]*domain.BackupRun, error) {
	return nil, nil
}
func (m *mockRecoveryRepo) GetLatestRunForJob(ctx context.Context, orgID, jobID uuid.UUID) (*domain.BackupRun, error) {
	return nil, domain.ErrRunNotFound
}
func (m *mockRecoveryRepo) UpdateHeartbeat(ctx context.Context, orgID, runID uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.runs[runID]
	if !ok || r.OrganizationID != orgID || r.Status != domain.RunStatusRunning {
		return domain.ErrRunNoLongerActive
	}
	r.HeartbeatAt = time.Now().UTC()
	r.LeaseUntil = time.Now().UTC().Add(2 * time.Minute)
	return nil
}
func (m *mockRecoveryRepo) FinalizeRunAndJob(ctx context.Context, orgID, runID, jobID uuid.UUID, runStatus domain.RunStatus, jobStatus domain.JobStatus, errMsg *string, logsSummary []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.runs[runID]
	if !ok || r.OrganizationID != orgID || r.Status != domain.RunStatusRunning {
		return domain.ErrRunNoLongerActive
	}
	r.Status = runStatus
	r.ErrorMessage = errMsg
	r.LogsSummary = logsSummary
	now := time.Now().UTC()
	r.EndedAt = &now

	if j, ok := m.jobs[jobID]; ok {
		j.Status = jobStatus
	}
	return nil
}
func (m *mockRecoveryRepo) CreateArtifact(ctx context.Context, artifact *domain.BackupArtifact) (*domain.BackupArtifact, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.artifacts[artifact.RunID] = append(m.artifacts[artifact.RunID], artifact)
	return artifact, nil
}
func (m *mockRecoveryRepo) GetArtifactByID(ctx context.Context, orgID, artifactID uuid.UUID) (*domain.BackupArtifact, error) {
	return nil, nil
}
func (m *mockRecoveryRepo) ListArtifacts(ctx context.Context, orgID uuid.UUID) ([]*domain.BackupArtifact, error) {
	return nil, nil
}
func (m *mockRecoveryRepo) UpdateArtifactVerification(ctx context.Context, orgID, artifactID uuid.UUID, status domain.VerificationStatus, details *string) error {
	return nil
}
func (m *mockRecoveryRepo) TombstoneArtifact(ctx context.Context, orgID, artifactID uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.tombstoneErr != nil {
		return m.tombstoneErr
	}
	m.tombstoned[artifactID] = true
	for _, artList := range m.artifacts {
		for _, art := range artList {
			if art.ID == artifactID && art.OrganizationID == orgID {
				art.IsDeleted = true
				now := time.Now().UTC()
				art.DeletedAt = &now
			}
		}
	}
	return nil
}
func (m *mockRecoveryRepo) GetRunArtifacts(ctx context.Context, orgID, runID uuid.UUID) ([]*domain.BackupArtifact, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var res []*domain.BackupArtifact
	for _, art := range m.artifacts[runID] {
		if art.OrganizationID == orgID {
			res = append(res, art)
		}
	}
	return res, nil
}
func (m *mockRecoveryRepo) RecoverInterruptedRuns(ctx context.Context) ([]domain.RecoveredRunInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.recoverErr != nil {
		return nil, m.recoverErr
	}
	var recovered []domain.RecoveredRunInfo
	for _, r := range m.runs {
		if r.Status == domain.RunStatusRunning {
			r.Status = domain.RunStatusFailed
			now := time.Now().UTC()
			r.EndedAt = &now
			msg := "worker process restarted before completion"
			r.ErrorMessage = &msg

			if j, ok := m.jobs[r.JobID]; ok {
				if r.AttemptNumber < 3 {
					j.Status = domain.JobStatusPending
				} else {
					j.Status = domain.JobStatusFailed
				}
			}

			recovered = append(recovered, domain.RecoveredRunInfo{
				ID:             r.ID,
				OrganizationID: r.OrganizationID,
				JobID:          r.JobID,
				AttemptNumber:  r.AttemptNumber,
			})
		}
	}
	return recovered, nil
}
func (m *mockRecoveryRepo) ReapStaleRuns(ctx context.Context) ([]domain.RecoveredRunInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.reapErr != nil {
		return nil, m.reapErr
	}
	now := time.Now().UTC()
	var reaped []domain.RecoveredRunInfo
	for _, r := range m.runs {
		if r.Status == domain.RunStatusRunning && r.LeaseUntil.Before(now) {
			r.Status = domain.RunStatusFailed
			r.EndedAt = &now
			msg := "worker lease expired"
			r.ErrorMessage = &msg

			if j, ok := m.jobs[r.JobID]; ok {
				if r.AttemptNumber < 3 {
					j.Status = domain.JobStatusPending
				} else {
					j.Status = domain.JobStatusFailed
				}
			}

			reaped = append(reaped, domain.RecoveredRunInfo{
				ID:             r.ID,
				OrganizationID: r.OrganizationID,
				JobID:          r.JobID,
				AttemptNumber:  r.AttemptNumber,
			})
		}
	}
	return reaped, nil
}

var _ repository.BackupRepository = (*mockRecoveryRepo)(nil)

type mockStorageWithControl struct {
	storage.StorageProvider
	deletedRefs []string
	deleteErr   error
}

func (m *mockStorageWithControl) DeleteArtifact(ctx context.Context, storageRef string) error {
	m.deletedRefs = append(m.deletedRefs, storageRef)
	if m.deleteErr != nil {
		return m.deleteErr
	}
	if m.StorageProvider != nil {
		return m.StorageProvider.DeleteArtifact(ctx, storageRef)
	}
	return nil
}

func TestRecoveryAndReaper_Comprehensive(t *testing.T) {
	nullLogger := slog.New(slog.NewTextHandler(io.Discard, nil))

	t.Run("Scenario 6: Worker crash + application restart resets attempt < 3 to pending", func(t *testing.T) {
		repo := newMockRecoveryRepo()
		orgID := uuid.New()
		jobID := uuid.New()
		runID := uuid.New()

		repo.jobs[jobID] = &domain.BackupJob{
			ID:             jobID,
			OrganizationID: orgID,
			Status:         domain.JobStatusRunning,
		}
		repo.runs[runID] = &domain.BackupRun{
			ID:             runID,
			OrganizationID: orgID,
			JobID:          jobID,
			AttemptNumber:  1,
			Status:         domain.RunStatusRunning,
		}

		store, _ := local.NewLocalStorageProvider(t.TempDir())
		err := RunStartupRecovery(context.Background(), repo, store, nullLogger)
		if err != nil {
			t.Fatalf("unexpected recovery error: %v", err)
		}

		if repo.runs[runID].Status != domain.RunStatusFailed {
			t.Errorf("expected run failed, got: %s", repo.runs[runID].Status)
		}
		if repo.jobs[jobID].Status != domain.JobStatusPending {
			t.Errorf("expected job reset to pending, got: %s", repo.jobs[jobID].Status)
		}
	})

	t.Run("Scenario 7: Crash on max attempt (attempt >= 3) marks job failed", func(t *testing.T) {
		repo := newMockRecoveryRepo()
		orgID := uuid.New()
		jobID := uuid.New()
		runID := uuid.New()

		repo.jobs[jobID] = &domain.BackupJob{
			ID:             jobID,
			OrganizationID: orgID,
			Status:         domain.JobStatusRunning,
		}
		repo.runs[runID] = &domain.BackupRun{
			ID:             runID,
			OrganizationID: orgID,
			JobID:          jobID,
			AttemptNumber:  3,
			Status:         domain.RunStatusRunning,
		}

		store, _ := local.NewLocalStorageProvider(t.TempDir())
		err := RunStartupRecovery(context.Background(), repo, store, nullLogger)
		if err != nil {
			t.Fatalf("unexpected recovery error: %v", err)
		}

		if repo.runs[runID].Status != domain.RunStatusFailed {
			t.Errorf("expected run failed, got: %s", repo.runs[runID].Status)
		}
		if repo.jobs[jobID].Status != domain.JobStatusFailed {
			t.Errorf("expected job failed, got: %s", repo.jobs[jobID].Status)
		}
	})

	t.Run("Scenario 8: Stale lease reaps expired run and resets job", func(t *testing.T) {
		repo := newMockRecoveryRepo()
		orgID := uuid.New()
		jobID := uuid.New()
		runID := uuid.New()

		repo.jobs[jobID] = &domain.BackupJob{
			ID:             jobID,
			OrganizationID: orgID,
			Status:         domain.JobStatusRunning,
		}
		repo.runs[runID] = &domain.BackupRun{
			ID:             runID,
			OrganizationID: orgID,
			JobID:          jobID,
			AttemptNumber:  2,
			Status:         domain.RunStatusRunning,
			LeaseUntil:     time.Now().UTC().Add(-1 * time.Minute), // expired
		}

		store, _ := local.NewLocalStorageProvider(t.TempDir())
		reaper := NewStaleRunReaper(repo, store, 10*time.Millisecond, nullLogger)

		ctx, cancel := context.WithCancel(context.Background())
		reaper.Start(ctx)
		time.Sleep(30 * time.Millisecond)
		_ = reaper.Stop(context.Background())
		cancel()

		if repo.runs[runID].Status != domain.RunStatusFailed {
			t.Errorf("expected run failed, got: %s", repo.runs[runID].Status)
		}
		if repo.jobs[jobID].Status != domain.JobStatusPending {
			t.Errorf("expected job reset to pending, got: %s", repo.jobs[jobID].Status)
		}
	})

	t.Run("Scenario 9: Healthy lease remains untouched by reaper", func(t *testing.T) {
		repo := newMockRecoveryRepo()
		orgID := uuid.New()
		jobID := uuid.New()
		runID := uuid.New()

		repo.jobs[jobID] = &domain.BackupJob{
			ID:             jobID,
			OrganizationID: orgID,
			Status:         domain.JobStatusRunning,
		}
		repo.runs[runID] = &domain.BackupRun{
			ID:             runID,
			OrganizationID: orgID,
			JobID:          jobID,
			AttemptNumber:  1,
			Status:         domain.RunStatusRunning,
			LeaseUntil:     time.Now().UTC().Add(2 * time.Minute), // healthy
		}

		store, _ := local.NewLocalStorageProvider(t.TempDir())
		reaper := NewStaleRunReaper(repo, store, 10*time.Millisecond, nullLogger)

		ctx, cancel := context.WithCancel(context.Background())
		reaper.Start(ctx)
		time.Sleep(30 * time.Millisecond)
		_ = reaper.Stop(context.Background())
		cancel()

		if repo.runs[runID].Status != domain.RunStatusRunning {
			t.Errorf("healthy run must remain running, got: %s", repo.runs[runID].Status)
		}
		if repo.jobs[jobID].Status != domain.JobStatusRunning {
			t.Errorf("job must remain running, got: %s", repo.jobs[jobID].Status)
		}
	})

	t.Run("Scenario 10 & 11: Heartbeat race and ownership loss", func(t *testing.T) {
		repo := newMockRecoveryRepo()
		orgID := uuid.New()
		jobID := uuid.New()
		runID := uuid.New()

		repo.jobs[jobID] = &domain.BackupJob{
			ID:             jobID,
			OrganizationID: orgID,
			Status:         domain.JobStatusRunning,
		}
		repo.runs[runID] = &domain.BackupRun{
			ID:             runID,
			OrganizationID: orgID,
			JobID:          jobID,
			AttemptNumber:  1,
			Status:         domain.RunStatusRunning,
			LeaseUntil:     time.Now().UTC().Add(-10 * time.Second), // expired
		}

		// Reaping marks it failed
		reaped, err := repo.ReapStaleRuns(context.Background())
		if err != nil || len(reaped) != 1 {
			t.Fatalf("expected 1 reaped run, got %d, err: %v", len(reaped), err)
		}

		// Worker's subsequent heartbeat must fail with ErrRunNoLongerActive
		hbErr := repo.UpdateHeartbeat(context.Background(), orgID, runID)
		if !errors.Is(hbErr, domain.ErrRunNoLongerActive) {
			t.Errorf("expected ErrRunNoLongerActive on heartbeat, got: %v", hbErr)
		}

		// Worker's subsequent finalize must fail with ErrRunNoLongerActive
		finErr := repo.FinalizeRunAndJob(context.Background(), orgID, runID, jobID, domain.RunStatusSuccess, domain.JobStatusCompleted, nil, nil)
		if !errors.Is(finErr, domain.ErrRunNoLongerActive) {
			t.Errorf("expected ErrRunNoLongerActive on finalize, got: %v", finErr)
		}
	})

	t.Run("Scenario 12: Crash with persisted active artifact deletes physical file and tombstones metadata", func(t *testing.T) {
		tempDir := t.TempDir()
		store, err := local.NewLocalStorageProvider(tempDir)
		if err != nil {
			t.Fatalf("failed creating storage: %v", err)
		}
		_ = store.EnsureStorageRoot(context.Background())

		repo := newMockRecoveryRepo()
		orgID := uuid.New()
		resID := uuid.New()
		jobID := uuid.New()
		runID := uuid.New()
		artID := uuid.New()

		repo.jobs[jobID] = &domain.BackupJob{ID: jobID, OrganizationID: orgID, Status: domain.JobStatusRunning}
		repo.runs[runID] = &domain.BackupRun{ID: runID, OrganizationID: orgID, JobID: jobID, AttemptNumber: 1, Status: domain.RunStatusRunning}

		// Save a physical artifact
		saveRes, err := store.SaveArtifact(context.Background(), orgID, resID, runID, artID, ".sql.gz", strings.NewReader("sample data"))
		if err != nil {
			t.Fatalf("failed saving artifact: %v", err)
		}

		repo.artifacts[runID] = []*domain.BackupArtifact{
			{
				ID:               artID,
				OrganizationID:   orgID,
				RunID:            runID,
				ResourceID:       resID,
				StorageReference: saveRes.StorageReference,
				IsDeleted:        false,
			},
		}

		err = RunStartupRecovery(context.Background(), repo, store, nullLogger)
		if err != nil {
			t.Fatalf("unexpected recovery error: %v", err)
		}

		// Verify metadata tombstoned
		if !repo.tombstoned[artID] {
			t.Errorf("expected artifact %s to be tombstoned in repo", artID)
		}

		// Verify physical file deleted
		_, openErr := store.OpenArtifact(context.Background(), saveRes.StorageReference)
		if !errors.Is(openErr, storage.ErrArtifactNotFound) {
			t.Errorf("expected physical artifact to be deleted, got: %v", openErr)
		}
	})

	t.Run("Scenario 13: Physical cleanup failure does NOT rollback run recovery and does NOT tombstone", func(t *testing.T) {
		repo := newMockRecoveryRepo()
		orgID := uuid.New()
		jobID := uuid.New()
		runID := uuid.New()
		artID := uuid.New()

		repo.jobs[jobID] = &domain.BackupJob{ID: jobID, OrganizationID: orgID, Status: domain.JobStatusRunning}
		repo.runs[runID] = &domain.BackupRun{ID: runID, OrganizationID: orgID, JobID: jobID, AttemptNumber: 1, Status: domain.RunStatusRunning}
		repo.artifacts[runID] = []*domain.BackupArtifact{
			{
				ID:               artID,
				OrganizationID:   orgID,
				RunID:            runID,
				StorageReference: "local://test.sql.gz",
				IsDeleted:        false,
			},
		}

		mockStore := &mockStorageWithControl{deleteErr: storage.ErrStorageIO}
		err := RunStartupRecovery(context.Background(), repo, mockStore, nullLogger)
		if err != nil {
			t.Fatalf("run recovery itself must not fail: %v", err)
		}

		// Run must remain failed
		if repo.runs[runID].Status != domain.RunStatusFailed {
			t.Errorf("run status must be failed, got: %s", repo.runs[runID].Status)
		}

		// Artifact must NOT be tombstoned since physical delete failed
		if repo.tombstoned[artID] {
			t.Errorf("artifact must NOT be tombstoned on physical delete failure")
		}
	})

	t.Run("Scenario 14: Tombstone failure does NOT rollback run recovery", func(t *testing.T) {
		repo := newMockRecoveryRepo()
		repo.tombstoneErr = errors.New("db deadlock on tombstone")

		orgID := uuid.New()
		jobID := uuid.New()
		runID := uuid.New()
		artID := uuid.New()

		repo.jobs[jobID] = &domain.BackupJob{ID: jobID, OrganizationID: orgID, Status: domain.JobStatusRunning}
		repo.runs[runID] = &domain.BackupRun{ID: runID, OrganizationID: orgID, JobID: jobID, AttemptNumber: 1, Status: domain.RunStatusRunning}
		repo.artifacts[runID] = []*domain.BackupArtifact{
			{
				ID:               artID,
				OrganizationID:   orgID,
				RunID:            runID,
				StorageReference: "local://test.sql.gz",
				IsDeleted:        false,
			},
		}

		mockStore := &mockStorageWithControl{}
		err := RunStartupRecovery(context.Background(), repo, mockStore, nullLogger)
		if err != nil {
			t.Fatalf("run recovery must succeed even if tombstone fails: %v", err)
		}

		if repo.runs[runID].Status != domain.RunStatusFailed {
			t.Errorf("run status must be failed, got: %s", repo.runs[runID].Status)
		}
	})

	t.Run("Scenario 15: Orphan temporary file startup cleanup removes recognized partials and empty dir", func(t *testing.T) {
		tempDir := t.TempDir()
		store, err := local.NewLocalStorageProvider(tempDir)
		if err != nil {
			t.Fatalf("failed creating storage: %v", err)
		}
		_ = store.EnsureStorageRoot(context.Background())

		runID := uuid.New()
		artID1 := uuid.New()
		artID2 := uuid.New()

		runDir := filepath.Join(tempDir, "tmp", fmt.Sprintf("run-%s", runID.String()))
		if err := os.MkdirAll(runDir, 0700); err != nil {
			t.Fatalf("failed creating temp run dir: %v", err)
		}

		file1 := filepath.Join(runDir, fmt.Sprintf("artifact-%s.sql.gz.partial", artID1.String()))
		file2 := filepath.Join(runDir, fmt.Sprintf("artifact-%s.tar.gz.partial", artID2.String()))

		_ = os.WriteFile(file1, []byte("partial data 1"), 0600)
		_ = os.WriteFile(file2, []byte("partial data 2"), 0600)

		cleaned, err := store.CleanOrphanTemporaryArtifacts(context.Background())
		if err != nil {
			t.Fatalf("clean failed: %v", err)
		}
		if cleaned != 2 {
			t.Errorf("expected 2 partial files cleaned, got %d", cleaned)
		}

		// Verify run directory was removed because it was empty
		if _, err := os.Stat(runDir); !os.IsNotExist(err) {
			t.Errorf("expected empty run directory to be removed")
		}
	})

	t.Run("Scenario 16: Temp cleanup path safety preserves unknown files and directories", func(t *testing.T) {
		tempDir := t.TempDir()
		store, err := local.NewLocalStorageProvider(tempDir)
		if err != nil {
			t.Fatalf("failed creating storage: %v", err)
		}
		_ = store.EnsureStorageRoot(context.Background())

		runID := uuid.New()
		runDir := filepath.Join(tempDir, "tmp", fmt.Sprintf("run-%s", runID.String()))
		_ = os.MkdirAll(runDir, 0700)

		validPartial := filepath.Join(runDir, fmt.Sprintf("artifact-%s.sql.gz.partial", uuid.New().String()))
		_ = os.WriteFile(validPartial, []byte("valid partial"), 0600)

		unknownFileInRunDir := filepath.Join(runDir, "unknown.txt")
		_ = os.WriteFile(unknownFileInRunDir, []byte("keep me"), 0600)

		unknownDirInTmp := filepath.Join(tempDir, "tmp", "arbitrary-folder")
		_ = os.MkdirAll(unknownDirInTmp, 0700)
		fileInUnknownDir := filepath.Join(unknownDirInTmp, "keep-me-too.txt")
		_ = os.WriteFile(fileInUnknownDir, []byte("data"), 0600)

		cleaned, err := store.CleanOrphanTemporaryArtifacts(context.Background())
		if err != nil {
			t.Fatalf("clean error: %v", err)
		}
		if cleaned != 1 {
			t.Errorf("expected exactly 1 valid partial cleaned, got %d", cleaned)
		}

		// Unknown file in run dir must still exist
		if _, err := os.Stat(unknownFileInRunDir); err != nil {
			t.Errorf("unknown file in run dir must be preserved")
		}
		// Run dir must still exist because unknown file remains
		if _, err := os.Stat(runDir); err != nil {
			t.Errorf("run dir with unknown file must be preserved")
		}
		// Arbitrary folder must still exist
		if _, err := os.Stat(fileInUnknownDir); err != nil {
			t.Errorf("arbitrary folder file must be preserved")
		}
	})

	t.Run("Scenario 17: Multi-organization recovery isolation", func(t *testing.T) {
		repo := newMockRecoveryRepo()
		org1 := uuid.New()
		org2 := uuid.New()

		job1 := uuid.New()
		run1 := uuid.New()
		art1 := uuid.New()

		job2 := uuid.New()
		run2 := uuid.New()
		art2 := uuid.New()

		repo.jobs[job1] = &domain.BackupJob{ID: job1, OrganizationID: org1, Status: domain.JobStatusRunning}
		repo.runs[run1] = &domain.BackupRun{ID: run1, OrganizationID: org1, JobID: job1, AttemptNumber: 1, Status: domain.RunStatusRunning}
		repo.artifacts[run1] = []*domain.BackupArtifact{
			{ID: art1, OrganizationID: org1, RunID: run1, StorageReference: "local://org1.sql.gz", IsDeleted: false},
		}

		repo.jobs[job2] = &domain.BackupJob{ID: job2, OrganizationID: org2, Status: domain.JobStatusRunning}
		repo.runs[run2] = &domain.BackupRun{ID: run2, OrganizationID: org2, JobID: job2, AttemptNumber: 1, Status: domain.RunStatusRunning}
		repo.artifacts[run2] = []*domain.BackupArtifact{
			{ID: art2, OrganizationID: org2, RunID: run2, StorageReference: "local://org2.sql.gz", IsDeleted: false},
		}

		mockStore := &mockStorageWithControl{}
		err := RunStartupRecovery(context.Background(), repo, mockStore, nullLogger)
		if err != nil {
			t.Fatalf("recovery error: %v", err)
		}

		if !repo.tombstoned[art1] || !repo.tombstoned[art2] {
			t.Errorf("both org artifacts must be tombstoned under their own org ID")
		}
	})

	t.Run("Scenario 18: Repeated recovery/reaper invocation is idempotent", func(t *testing.T) {
		repo := newMockRecoveryRepo()
		orgID := uuid.New()
		jobID := uuid.New()
		runID := uuid.New()

		repo.jobs[jobID] = &domain.BackupJob{ID: jobID, OrganizationID: orgID, Status: domain.JobStatusRunning}
		repo.runs[runID] = &domain.BackupRun{ID: runID, OrganizationID: orgID, JobID: jobID, AttemptNumber: 1, Status: domain.RunStatusRunning}

		mockStore := &mockStorageWithControl{}
		// First pass
		_ = RunStartupRecovery(context.Background(), repo, mockStore, nullLogger)
		if repo.runs[runID].Status != domain.RunStatusFailed {
			t.Fatalf("first recovery failed")
		}

		// Second pass
		_ = RunStartupRecovery(context.Background(), repo, mockStore, nullLogger)
		if repo.jobs[jobID].Status != domain.JobStatusPending {
			t.Errorf("job must remain pending without corruption")
		}
	})
}
