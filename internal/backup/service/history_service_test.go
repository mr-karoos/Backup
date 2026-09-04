package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"backup-platform/internal/backup/domain"
	orgDomain "backup-platform/internal/organization/domain"
	"backup-platform/pkg/uuid"
)

type mockHistoryRepo struct {
	listRunsFunc     func(ctx context.Context, orgID uuid.UUID, filter domain.RunFilter) ([]*domain.BackupRunWithStats, error)
	getRunDetailFunc func(ctx context.Context, orgID, runID uuid.UUID) (*domain.BackupRunWithStats, error)
}

func (m *mockHistoryRepo) ListRuns(ctx context.Context, orgID uuid.UUID, filter domain.RunFilter) ([]*domain.BackupRunWithStats, error) {
	if m.listRunsFunc != nil {
		return m.listRunsFunc(ctx, orgID, filter)
	}
	return nil, nil
}

func (m *mockHistoryRepo) ListSuccessfulRunsForPlan(ctx context.Context, orgID, planID uuid.UUID) ([]*domain.BackupRun, error) {
	return nil, nil
}

func (m *mockHistoryRepo) GetRunDetail(ctx context.Context, orgID, runID uuid.UUID) (*domain.BackupRunWithStats, error) {
	if m.getRunDetailFunc != nil {
		return m.getRunDetailFunc(ctx, orgID, runID)
	}
	return nil, domain.ErrRunNotFound
}

func (m *mockHistoryRepo) EnsureDefaultLocalStorageTarget(ctx context.Context, orgID uuid.UUID) (*domain.StorageTarget, error) {
	return nil, nil
}
func (m *mockHistoryRepo) GetStorageTargetByID(ctx context.Context, orgID, targetID uuid.UUID) (*domain.StorageTarget, error) {
	return nil, nil
}
func (m *mockHistoryRepo) GetPlanByID(ctx context.Context, orgID, planID uuid.UUID) (*domain.BackupPlan, error) {
	return nil, nil
}
func (m *mockHistoryRepo) CreateJob(ctx context.Context, job *domain.BackupJob) (*domain.BackupJob, error) {
	return nil, nil
}
func (m *mockHistoryRepo) GetJobByID(ctx context.Context, orgID, jobID uuid.UUID) (*domain.BackupJob, error) {
	return nil, nil
}
func (m *mockHistoryRepo) GetActiveManualJobForResource(ctx context.Context, orgID, resourceID uuid.UUID) (*domain.BackupJob, error) {
	return nil, nil
}
func (m *mockHistoryRepo) GetActiveJobConflictForResource(ctx context.Context, orgID, resourceID uuid.UUID) (*domain.BackupJob, error) {
	return nil, nil
}
func (m *mockHistoryRepo) FindPendingJobs(ctx context.Context, limit int, afterCreatedAt *time.Time, afterID *uuid.UUID) ([]*domain.BackupJob, error) {
	return nil, nil
}
func (m *mockHistoryRepo) TransactionalClaimJob(ctx context.Context, orgID, jobID uuid.UUID) (*domain.BackupRun, *domain.BackupJob, error) {
	return nil, nil, nil
}
func (m *mockHistoryRepo) GetRunByID(ctx context.Context, orgID, runID uuid.UUID) (*domain.BackupRun, error) {
	return nil, nil
}
func (m *mockHistoryRepo) GetLatestRunForJob(ctx context.Context, orgID, jobID uuid.UUID) (*domain.BackupRun, error) {
	return nil, nil
}
func (m *mockHistoryRepo) UpdateHeartbeat(ctx context.Context, orgID, runID uuid.UUID) error {
	return nil
}
func (m *mockHistoryRepo) FinalizeRunAndJob(ctx context.Context, orgID, runID, jobID uuid.UUID, runStatus domain.RunStatus, jobStatus domain.JobStatus, errMsg *string, logsSummary []byte) error {
	return nil
}
func (m *mockHistoryRepo) CreateArtifact(ctx context.Context, artifact *domain.BackupArtifact) (*domain.BackupArtifact, error) {
	return nil, nil
}
func (m *mockHistoryRepo) GetArtifactByID(ctx context.Context, orgID, artifactID uuid.UUID) (*domain.BackupArtifact, error) {
	return nil, nil
}
func (m *mockHistoryRepo) ListArtifacts(ctx context.Context, orgID uuid.UUID) ([]*domain.BackupArtifact, error) {
	return nil, nil
}
func (m *mockHistoryRepo) UpdateArtifactVerification(ctx context.Context, orgID, artifactID uuid.UUID, status domain.VerificationStatus, details *string) error {
	return nil
}
func (m *mockHistoryRepo) TombstoneArtifact(ctx context.Context, orgID, artifactID uuid.UUID) error {
	return nil
}
func (m *mockHistoryRepo) GetRunArtifacts(ctx context.Context, orgID, runID uuid.UUID) ([]*domain.BackupArtifact, error) {
	return nil, nil
}
func (m *mockHistoryRepo) RecoverInterruptedRuns(ctx context.Context) ([]domain.RecoveredRunInfo, error) {
	return nil, nil
}
func (m *mockHistoryRepo) ReapStaleRuns(ctx context.Context) ([]domain.RecoveredRunInfo, error) {
	return nil, nil
}
func (m *mockHistoryRepo) CreateStorageTarget(ctx context.Context, target *domain.StorageTarget) (*domain.StorageTarget, error) {
	return nil, nil
}
func (m *mockHistoryRepo) ListStorageTargets(ctx context.Context, orgID uuid.UUID) ([]*domain.StorageTarget, error) {
	return nil, nil
}
func (m *mockHistoryRepo) UpdateStorageTarget(ctx context.Context, target *domain.StorageTarget) (*domain.StorageTarget, error) {
	return nil, nil
}
func (m *mockHistoryRepo) DeleteStorageTarget(ctx context.Context, orgID, targetID uuid.UUID) error {
	return nil
}
func (m *mockHistoryRepo) CountArtifactsByStorageTarget(ctx context.Context, orgID, targetID uuid.UUID) (int64, error) {
	return 0, nil
}
func (m *mockHistoryRepo) CountPlansByStorageTarget(ctx context.Context, orgID, targetID uuid.UUID) (int64, error) {
	return 0, nil
}
func (m *mockHistoryRepo) CountActiveJobsByStorageTarget(ctx context.Context, orgID, targetID uuid.UUID) (int64, error) {
	return 0, nil
}
func (m *mockHistoryRepo) CountRepositoriesByStorageTarget(ctx context.Context, orgID, targetID uuid.UUID) (int64, error) {
	return 0, nil
}
func (m *mockHistoryRepo) CreateRepository(ctx context.Context, repo *domain.BackupRepository) (*domain.BackupRepository, error) {
	return nil, nil
}
func (m *mockHistoryRepo) GetRepositoryByResourceID(ctx context.Context, orgID, resourceID uuid.UUID) (*domain.BackupRepository, error) {
	return nil, nil
}
func (m *mockHistoryRepo) GetRepositoryByID(ctx context.Context, orgID, repoID uuid.UUID) (*domain.BackupRepository, error) {
	return nil, nil
}

func TestHistoryService_ListRuns(t *testing.T) {
	orgID := uuid.New()
	resID := uuid.New()
	runID := uuid.New()

	t.Run("lists runs with filters successfully", func(t *testing.T) {
		repo := &mockHistoryRepo{
			listRunsFunc: func(ctx context.Context, oID uuid.UUID, filter domain.RunFilter) ([]*domain.BackupRunWithStats, error) {
				return []*domain.BackupRunWithStats{
					{
						Run: domain.BackupRun{
							ID:             runID,
							OrganizationID: oID,
							Status:         domain.RunStatusSuccess,
						},
						ResourceID:             resID,
						TotalArtifactSizeBytes: 1024,
						ArtifactsCount:         1,
					},
				}, nil
			},
		}

		svc := NewHistoryService(repo, nil)
		runs, err := svc.ListRuns(context.Background(), orgDomain.RoleMember, orgID, domain.RunFilter{ResourceID: &resID})
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if len(runs) != 1 || runs[0].Run.ID != runID {
			t.Fatalf("expected 1 run with ID %s", runID)
		}
	})

	t.Run("returns ErrInvalidRunFilter when orgID is nil", func(t *testing.T) {
		svc := NewHistoryService(&mockHistoryRepo{}, nil)
		_, err := svc.ListRuns(context.Background(), orgDomain.RoleMember, uuid.Nil, domain.RunFilter{})
		if !errors.Is(err, domain.ErrInvalidRunFilter) {
			t.Fatalf("expected ErrInvalidRunFilter, got: %v", err)
		}
	})
}

func TestHistoryService_GetRun(t *testing.T) {
	orgID := uuid.New()
	runID := uuid.New()

	t.Run("gets run detail successfully", func(t *testing.T) {
		repo := &mockHistoryRepo{
			getRunDetailFunc: func(ctx context.Context, oID, rID uuid.UUID) (*domain.BackupRunWithStats, error) {
				return &domain.BackupRunWithStats{
					Run: domain.BackupRun{
						ID:             rID,
						OrganizationID: oID,
						Status:         domain.RunStatusSuccess,
					},
					TotalArtifactSizeBytes: 2048,
					ArtifactsCount:         2,
				}, nil
			},
		}

		svc := NewHistoryService(repo, nil)
		run, err := svc.GetRun(context.Background(), orgDomain.RoleViewer, orgID, runID)
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if run.Run.ID != runID || run.TotalArtifactSizeBytes != 2048 {
			t.Fatalf("unexpected run detail: %+v", run)
		}
	})

	t.Run("returns ErrRunNotFound when not found", func(t *testing.T) {
		repo := &mockHistoryRepo{
			getRunDetailFunc: func(ctx context.Context, oID, rID uuid.UUID) (*domain.BackupRunWithStats, error) {
				return nil, domain.ErrRunNotFound
			},
		}

		svc := NewHistoryService(repo, nil)
		_, err := svc.GetRun(context.Background(), orgDomain.RoleAdmin, orgID, runID)
		if !errors.Is(err, domain.ErrRunNotFound) {
			t.Fatalf("expected ErrRunNotFound, got: %v", err)
		}
	})
}
