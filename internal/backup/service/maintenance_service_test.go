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

type mockMaintenanceRepoForService struct {
	listPaginatedFunc func(ctx context.Context, orgID uuid.UUID, filter domain.MaintenanceJobFilter) ([]*domain.MaintenanceJob, bool, error)
	getJobByIDFunc    func(ctx context.Context, orgID, jobID uuid.UUID) (*domain.MaintenanceJob, error)
	listRunsFunc      func(ctx context.Context, orgID, jobID uuid.UUID) ([]*domain.MaintenanceRun, error)
}

func (m *mockMaintenanceRepoForService) EnqueueMaintenanceJob(ctx context.Context, params domain.EnqueueMaintenanceJobParams) (*domain.MaintenanceJob, error) {
	return nil, nil
}
func (m *mockMaintenanceRepoForService) ClaimNextMaintenanceJob(ctx context.Context, leaseDuration time.Duration) (*domain.MaintenanceJob, *domain.MaintenanceRun, error) {
	return nil, nil, nil
}
func (m *mockMaintenanceRepoForService) HeartbeatMaintenanceRun(ctx context.Context, orgID, runID uuid.UUID, leaseDuration time.Duration) error {
	return nil
}
func (m *mockMaintenanceRepoForService) CompleteMaintenanceJob(ctx context.Context, orgID, jobID, runID uuid.UUID, logsSummary []byte) error {
	return nil
}
func (m *mockMaintenanceRepoForService) FailMaintenanceJob(ctx context.Context, orgID, jobID, runID uuid.UUID, errMsg string, retryable bool) error {
	return nil
}
func (m *mockMaintenanceRepoForService) UpdateJobPhase(ctx context.Context, orgID, jobID uuid.UUID, phase string) error {
	return nil
}
func (m *mockMaintenanceRepoForService) FinalizeSuccessfulResticForget(ctx context.Context, orgID, jobID, runID, artifactID, repoID uuid.UUID, snapshotID string, logsSummary []byte) error {
	return nil
}
func (m *mockMaintenanceRepoForService) GetLastSuccessfulDeepCheckSubset(ctx context.Context, orgID, repoID uuid.UUID) (int, error) {
	return 0, nil
}
func (m *mockMaintenanceRepoForService) GetDeepCheckDueStatus(ctx context.Context, orgID, repoID uuid.UUID, repoCreatedAt time.Time, dueInterval time.Duration, totalSubsets int) (bool, int, error) {
	return false, 0, nil
}
func (m *mockMaintenanceRepoForService) RecoverInterruptedMaintenanceRuns(ctx context.Context) ([]domain.RecoveredMaintenanceRunInfo, error) {
	return nil, nil
}
func (m *mockMaintenanceRepoForService) ReapStaleMaintenanceRuns(ctx context.Context) ([]domain.RecoveredMaintenanceRunInfo, error) {
	return nil, nil
}
func (m *mockMaintenanceRepoForService) GetMaintenanceJobByID(ctx context.Context, orgID, jobID uuid.UUID) (*domain.MaintenanceJob, error) {
	if m.getJobByIDFunc != nil {
		return m.getJobByIDFunc(ctx, orgID, jobID)
	}
	return nil, domain.ErrJobNotFound
}
func (m *mockMaintenanceRepoForService) GetMaintenanceRunByID(ctx context.Context, orgID, runID uuid.UUID) (*domain.MaintenanceRun, error) {
	return nil, nil
}
func (m *mockMaintenanceRepoForService) ListMaintenanceJobs(ctx context.Context, orgID, repoID uuid.UUID, limit int) ([]*domain.MaintenanceJob, error) {
	return nil, nil
}
func (m *mockMaintenanceRepoForService) ListMaintenanceJobsPaginated(ctx context.Context, orgID uuid.UUID, filter domain.MaintenanceJobFilter) ([]*domain.MaintenanceJob, bool, error) {
	if m.listPaginatedFunc != nil {
		return m.listPaginatedFunc(ctx, orgID, filter)
	}
	return nil, false, nil
}
func (m *mockMaintenanceRepoForService) ListMaintenanceRuns(ctx context.Context, orgID, jobID uuid.UUID) ([]*domain.MaintenanceRun, error) {
	if m.listRunsFunc != nil {
		return m.listRunsFunc(ctx, orgID, jobID)
	}
	return nil, nil
}
func (m *mockMaintenanceRepoForService) ListActiveRepositories(ctx context.Context, limit int, afterCreatedAt *time.Time, afterID *uuid.UUID) ([]*domain.BackupRepository, error) {
	return nil, nil
}

type mockRepositoryFinder struct {
	getRepoByIDFunc func(ctx context.Context, orgID, repoID uuid.UUID) (*domain.BackupRepository, error)
}

func (m *mockRepositoryFinder) GetRepositoryByID(ctx context.Context, orgID, repoID uuid.UUID) (*domain.BackupRepository, error) {
	if m.getRepoByIDFunc != nil {
		return m.getRepoByIDFunc(ctx, orgID, repoID)
	}
	return nil, nil
}

func TestMaintenanceService_ListMaintenanceJobs(t *testing.T) {
	ctx := context.Background()
	orgID := uuid.New()
	repoID := uuid.New()

	t.Run("successful listing with next_cursor when has_more is true", func(t *testing.T) {
		job1ID := uuid.New()
		job2ID := uuid.New()
		now := time.Now().UTC()

		maintMock := &mockMaintenanceRepoForService{
			listPaginatedFunc: func(ctx context.Context, oID uuid.UUID, filter domain.MaintenanceJobFilter) ([]*domain.MaintenanceJob, bool, error) {
				return []*domain.MaintenanceJob{
					{ID: job1ID, OrganizationID: oID, RepositoryID: repoID, OperationType: domain.MaintenanceOpResticPrune, CreatedAt: now},
					{ID: job2ID, OrganizationID: oID, RepositoryID: repoID, OperationType: domain.MaintenanceOpResticPrune, CreatedAt: now.Add(-time.Minute)},
				}, true, nil
			},
		}

		backupMock := &mockRepositoryFinder{
			getRepoByIDFunc: func(ctx context.Context, oID, rID uuid.UUID) (*domain.BackupRepository, error) {
				return &domain.BackupRepository{ID: rID, OrganizationID: oID}, nil
			},
		}

		svc := NewMaintenanceService(maintMock, backupMock, nil)
		res, err := svc.ListMaintenanceJobs(ctx, orgDomain.RoleMember, orgID, domain.MaintenanceJobFilter{
			RepositoryID: &repoID,
			Limit:        2,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(res.Jobs) != 2 {
			t.Fatalf("expected 2 jobs, got %d", len(res.Jobs))
		}
		if !res.HasMore {
			t.Fatalf("expected HasMore = true")
		}
		if res.NextCursor == nil || *res.NextCursor == "" {
			t.Fatalf("expected non-empty next_cursor")
		}

		// Verify cursor decodes to job2
		curTime, curID, err := domain.DecodeMaintenanceCursor(*res.NextCursor)
		if err != nil {
			t.Fatalf("failed decoding cursor: %v", err)
		}
		if curID != job2ID {
			t.Fatalf("expected cursor ID %s, got %s", job2ID, curID)
		}
		if curTime.Unix() != now.Add(-time.Minute).Unix() {
			t.Fatalf("cursor time mismatch")
		}
	})

	t.Run("empty next_cursor when has_more is false", func(t *testing.T) {
		jobID := uuid.New()
		maintMock := &mockMaintenanceRepoForService{
			listPaginatedFunc: func(ctx context.Context, oID uuid.UUID, filter domain.MaintenanceJobFilter) ([]*domain.MaintenanceJob, bool, error) {
				return []*domain.MaintenanceJob{
					{ID: jobID, OrganizationID: oID, RepositoryID: repoID, OperationType: domain.MaintenanceOpResticPrune, CreatedAt: time.Now()},
				}, false, nil
			},
		}

		svc := NewMaintenanceService(maintMock, nil, nil)
		res, err := svc.ListMaintenanceJobs(ctx, orgDomain.RoleViewer, orgID, domain.MaintenanceJobFilter{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.HasMore {
			t.Fatalf("expected HasMore = false")
		}
		if res.NextCursor != nil {
			t.Fatalf("expected NextCursor = nil, got %v", res.NextCursor)
		}
	})

	t.Run("anti-enumeration: non-existent repository returns empty result instead of error", func(t *testing.T) {
		backupMock := &mockRepositoryFinder{
			getRepoByIDFunc: func(ctx context.Context, oID, rID uuid.UUID) (*domain.BackupRepository, error) {
				return nil, domain.ErrRepositoryNotFound
			},
		}
		maintMock := &mockMaintenanceRepoForService{}

		svc := NewMaintenanceService(maintMock, backupMock, nil)
		nonExistentRepoID := uuid.New()
		res, err := svc.ListMaintenanceJobs(ctx, orgDomain.RoleAdmin, orgID, domain.MaintenanceJobFilter{
			RepositoryID: &nonExistentRepoID,
		})
		if err != nil {
			t.Fatalf("expected nil error for anti-enumeration, got %v", err)
		}
		if len(res.Jobs) != 0 || res.HasMore || res.NextCursor != nil {
			t.Fatalf("expected empty result, got %v", res)
		}
	})

	t.Run("anti-enumeration: nil repository pointer returns empty result", func(t *testing.T) {
		backupMock := &mockRepositoryFinder{
			getRepoByIDFunc: func(ctx context.Context, oID, rID uuid.UUID) (*domain.BackupRepository, error) {
				return nil, nil
			},
		}
		maintMock := &mockMaintenanceRepoForService{}

		svc := NewMaintenanceService(maintMock, backupMock, nil)
		nonExistentRepoID := uuid.New()
		res, err := svc.ListMaintenanceJobs(ctx, orgDomain.RoleMember, orgID, domain.MaintenanceJobFilter{
			RepositoryID: &nonExistentRepoID,
		})
		if err != nil {
			t.Fatalf("expected nil error for anti-enumeration, got %v", err)
		}
		if len(res.Jobs) != 0 || res.HasMore || res.NextCursor != nil {
			t.Fatalf("expected empty result, got %v", res)
		}
	})

	t.Run("repository lookup database failure returns ErrBackupServiceUnavailable", func(t *testing.T) {
		backupMock := &mockRepositoryFinder{
			getRepoByIDFunc: func(ctx context.Context, oID, rID uuid.UUID) (*domain.BackupRepository, error) {
				return nil, errors.New("db connection lost")
			},
		}
		maintMock := &mockMaintenanceRepoForService{}

		svc := NewMaintenanceService(maintMock, backupMock, nil)
		targetRepoID := uuid.New()
		res, err := svc.ListMaintenanceJobs(ctx, orgDomain.RoleAdmin, orgID, domain.MaintenanceJobFilter{
			RepositoryID: &targetRepoID,
		})
		if err == nil {
			t.Fatalf("expected error on repository lookup DB failure, got nil")
		}
		if !errors.Is(err, domain.ErrBackupServiceUnavailable) {
			t.Fatalf("expected ErrBackupServiceUnavailable, got %v", err)
		}
		if res != nil {
			t.Fatalf("expected nil result on error, got %v", res)
		}
	})

	t.Run("paginated list database failure returns ErrBackupServiceUnavailable", func(t *testing.T) {
		maintMock := &mockMaintenanceRepoForService{
			listPaginatedFunc: func(ctx context.Context, oID uuid.UUID, filter domain.MaintenanceJobFilter) ([]*domain.MaintenanceJob, bool, error) {
				return nil, false, errors.New("query execution failed")
			},
		}

		svc := NewMaintenanceService(maintMock, nil, nil)
		res, err := svc.ListMaintenanceJobs(ctx, orgDomain.RoleMember, orgID, domain.MaintenanceJobFilter{})
		if err == nil {
			t.Fatalf("expected error on DB failure, got nil")
		}
		if !errors.Is(err, domain.ErrBackupServiceUnavailable) {
			t.Fatalf("expected ErrBackupServiceUnavailable, got %v", err)
		}
		if res != nil {
			t.Fatalf("expected nil result on error, got %v", res)
		}
	})

	t.Run("service-level RBAC: unpermitted role rejected with ErrUnauthorizedRole", func(t *testing.T) {
		svc := NewMaintenanceService(&mockMaintenanceRepoForService{}, nil, nil)
		unauthorizedRoles := []orgDomain.Role{
			orgDomain.Role("guest"),
			orgDomain.Role("anonymous"),
			orgDomain.Role(""),
		}
		for _, role := range unauthorizedRoles {
			res, err := svc.ListMaintenanceJobs(ctx, role, orgID, domain.MaintenanceJobFilter{})
			if !errors.Is(err, domain.ErrUnauthorizedRole) {
				t.Fatalf("expected ErrUnauthorizedRole for role %q, got err=%v, res=%v", role, err, res)
			}
		}
	})

	t.Run("nil orgID returns error", func(t *testing.T) {
		svc := NewMaintenanceService(&mockMaintenanceRepoForService{}, nil, nil)
		_, err := svc.ListMaintenanceJobs(ctx, orgDomain.RoleAdmin, uuid.Nil, domain.MaintenanceJobFilter{})
		if err == nil {
			t.Fatalf("expected error for nil orgID")
		}
	})
}

func TestMaintenanceService_GetMaintenanceJobDetail(t *testing.T) {
	ctx := context.Background()
	orgID := uuid.New()
	jobID := uuid.New()

	t.Run("successfully retrieves job and runs", func(t *testing.T) {
		maintMock := &mockMaintenanceRepoForService{
			getJobByIDFunc: func(ctx context.Context, oID, jID uuid.UUID) (*domain.MaintenanceJob, error) {
				return &domain.MaintenanceJob{
					ID:             jID,
					OrganizationID: oID,
					OperationType:  domain.MaintenanceOpResticPrune,
					Status:         domain.MaintenanceJobCompleted,
				}, nil
			},
			listRunsFunc: func(ctx context.Context, oID, jID uuid.UUID) ([]*domain.MaintenanceRun, error) {
				errMsg := "some sanitized error"
				return []*domain.MaintenanceRun{
					{
						ID:            uuid.New(),
						AttemptNumber: 1,
						Status:        domain.MaintenanceRunFailed,
						ErrorMessage:  &errMsg,
					},
					{
						ID:            uuid.New(),
						AttemptNumber: 2,
						Status:        domain.MaintenanceRunCompleted,
					},
				}, nil
			},
		}

		svc := NewMaintenanceService(maintMock, nil, nil)
		detail, err := svc.GetMaintenanceJobDetail(ctx, orgDomain.RoleAdmin, orgID, jobID)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if detail.Job == nil || detail.Job.ID != jobID {
			t.Fatalf("expected job ID %s", jobID)
		}
		if len(detail.Runs) != 2 {
			t.Fatalf("expected 2 runs, got %d", len(detail.Runs))
		}
	})

	t.Run("returns ErrJobNotFound if job does not exist", func(t *testing.T) {
		maintMock := &mockMaintenanceRepoForService{
			getJobByIDFunc: func(ctx context.Context, oID, jID uuid.UUID) (*domain.MaintenanceJob, error) {
				return nil, domain.ErrJobNotFound
			},
		}

		svc := NewMaintenanceService(maintMock, nil, nil)
		_, err := svc.GetMaintenanceJobDetail(ctx, orgDomain.RoleMember, orgID, jobID)
		if !errors.Is(err, domain.ErrJobNotFound) {
			t.Fatalf("expected ErrJobNotFound, got %v", err)
		}
	})

	t.Run("unexpected job lookup database error returns ErrBackupServiceUnavailable", func(t *testing.T) {
		maintMock := &mockMaintenanceRepoForService{
			getJobByIDFunc: func(ctx context.Context, oID, jID uuid.UUID) (*domain.MaintenanceJob, error) {
				return nil, errors.New("connection reset by peer")
			},
		}

		svc := NewMaintenanceService(maintMock, nil, nil)
		detail, err := svc.GetMaintenanceJobDetail(ctx, orgDomain.RoleViewer, orgID, jobID)
		if err == nil {
			t.Fatalf("expected error on DB failure, got nil")
		}
		if !errors.Is(err, domain.ErrBackupServiceUnavailable) {
			t.Fatalf("expected ErrBackupServiceUnavailable, got %v", err)
		}
		if detail != nil {
			t.Fatalf("expected nil detail on error, got %v", detail)
		}
	})

	t.Run("runs lookup database error returns ErrBackupServiceUnavailable", func(t *testing.T) {
		maintMock := &mockMaintenanceRepoForService{
			getJobByIDFunc: func(ctx context.Context, oID, jID uuid.UUID) (*domain.MaintenanceJob, error) {
				return &domain.MaintenanceJob{
					ID:             jID,
					OrganizationID: oID,
					OperationType:  domain.MaintenanceOpResticPrune,
					Status:         domain.MaintenanceJobCompleted,
				}, nil
			},
			listRunsFunc: func(ctx context.Context, oID, jID uuid.UUID) ([]*domain.MaintenanceRun, error) {
				return nil, errors.New("read runs failed")
			},
		}

		svc := NewMaintenanceService(maintMock, nil, nil)
		detail, err := svc.GetMaintenanceJobDetail(ctx, orgDomain.RoleAdmin, orgID, jobID)
		if err == nil {
			t.Fatalf("expected error on runs DB failure, got nil")
		}
		if !errors.Is(err, domain.ErrBackupServiceUnavailable) {
			t.Fatalf("expected ErrBackupServiceUnavailable, got %v", err)
		}
		if detail != nil {
			t.Fatalf("expected nil detail on error, got %v", detail)
		}
	})

	t.Run("service-level RBAC: unpermitted role rejected with ErrUnauthorizedRole", func(t *testing.T) {
		svc := NewMaintenanceService(&mockMaintenanceRepoForService{}, nil, nil)
		unauthorizedRoles := []orgDomain.Role{
			orgDomain.Role("guest"),
			orgDomain.Role("anonymous"),
			orgDomain.Role(""),
		}
		for _, role := range unauthorizedRoles {
			detail, err := svc.GetMaintenanceJobDetail(ctx, role, orgID, jobID)
			if !errors.Is(err, domain.ErrUnauthorizedRole) {
				t.Fatalf("expected ErrUnauthorizedRole for role %q, got err=%v, detail=%v", role, err, detail)
			}
		}
	})
}

func TestMaintenanceCursor_Roundtrip(t *testing.T) {
	id := uuid.New()
	ts := time.Date(2026, 9, 7, 18, 30, 0, 123456789, time.UTC)

	cursor := domain.EncodeMaintenanceCursor(ts, id)
	if cursor == "" {
		t.Fatalf("expected non-empty cursor")
	}

	decodedTime, decodedID, err := domain.DecodeMaintenanceCursor(cursor)
	if err != nil {
		t.Fatalf("failed decoding cursor: %v", err)
	}

	if decodedID != id {
		t.Fatalf("expected ID %s, got %s", id, decodedID)
	}
	if !decodedTime.Equal(ts) {
		t.Fatalf("expected time %v, got %v", ts, decodedTime)
	}

	// Invalid cursors
	invalidCursors := []string{
		"",
		"not-base64-!@#$",
		domain.EncodeMaintenanceCursor(ts, id) + "extra",
		"invalid,components,three",
	}
	for _, ic := range invalidCursors {
		_, _, err := domain.DecodeMaintenanceCursor(ic)
		if err == nil {
			t.Errorf("expected error for invalid cursor %q", ic)
		}
	}
}
