package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"backup-platform/internal/backup/domain"
	orgDomain "backup-platform/internal/organization/domain"
	resDomain "backup-platform/internal/resource/domain"
	"backup-platform/pkg/uuid"
)

type mockBackupRepo struct {
	plans             map[uuid.UUID]*domain.BackupPlan
	jobs              map[uuid.UUID]*domain.BackupJob
	activeJobConflict *domain.BackupJob
	storageTarget     *domain.StorageTarget
	storageTargetErr  error
	createJobErr      error
}

func (m *mockBackupRepo) EnsureDefaultLocalStorageTarget(ctx context.Context, orgID uuid.UUID) (*domain.StorageTarget, error) {
	if m.storageTargetErr != nil {
		return nil, m.storageTargetErr
	}
	if m.storageTarget != nil {
		return m.storageTarget, nil
	}
	return &domain.StorageTarget{ID: uuid.New(), OrganizationID: orgID, Type: domain.StorageTargetTypeLocal, Status: domain.StorageTargetStatusActive, IsDefault: true}, nil
}
func (m *mockBackupRepo) GetStorageTargetByID(ctx context.Context, orgID, targetID uuid.UUID) (*domain.StorageTarget, error) {
	return nil, nil
}
func (m *mockBackupRepo) GetPlanByID(ctx context.Context, orgID, planID uuid.UUID) (*domain.BackupPlan, error) {
	if p, ok := m.plans[planID]; ok && p.OrganizationID == orgID {
		return p, nil
	}
	return nil, domain.ErrPlanNotFound
}
func (m *mockBackupRepo) CreateJob(ctx context.Context, job *domain.BackupJob) (*domain.BackupJob, error) {
	if m.createJobErr != nil {
		return nil, m.createJobErr
	}
	m.jobs[job.ID] = job
	return job, nil
}
func (m *mockBackupRepo) GetJobByID(ctx context.Context, orgID, jobID uuid.UUID) (*domain.BackupJob, error) {
	if j, ok := m.jobs[jobID]; ok && j.OrganizationID == orgID {
		return j, nil
	}
	return nil, domain.ErrJobNotFound
}
func (m *mockBackupRepo) GetActiveManualJobForResource(ctx context.Context, orgID, resourceID uuid.UUID) (*domain.BackupJob, error) {
	return m.activeJobConflict, nil
}
func (m *mockBackupRepo) GetActiveJobConflictForResource(ctx context.Context, orgID, resourceID uuid.UUID) (*domain.BackupJob, error) {
	return m.activeJobConflict, nil
}
func (m *mockBackupRepo) FindPendingJobs(ctx context.Context, limit int, afterCreatedAt *time.Time, afterID *uuid.UUID) ([]*domain.BackupJob, error) {
	return nil, nil
}
func (m *mockBackupRepo) TransactionalClaimJob(ctx context.Context, orgID, jobID uuid.UUID) (*domain.BackupRun, *domain.BackupJob, error) {
	return nil, nil, nil
}
func (m *mockBackupRepo) GetRunByID(ctx context.Context, orgID, runID uuid.UUID) (*domain.BackupRun, error) {
	return nil, nil
}
func (m *mockBackupRepo) GetLatestRunForJob(ctx context.Context, orgID, jobID uuid.UUID) (*domain.BackupRun, error) {
	return nil, nil
}
func (m *mockBackupRepo) UpdateHeartbeat(ctx context.Context, orgID, runID uuid.UUID) error {
	return nil
}
func (m *mockBackupRepo) FinalizeRunAndJob(ctx context.Context, orgID, runID, jobID uuid.UUID, runStatus domain.RunStatus, jobStatus domain.JobStatus, errMsg *string, logsSummary []byte) error {
	return nil
}
func (m *mockBackupRepo) CreateArtifact(ctx context.Context, artifact *domain.BackupArtifact) (*domain.BackupArtifact, error) {
	return artifact, nil
}
func (m *mockBackupRepo) UpdateArtifactVerification(ctx context.Context, orgID, artifactID uuid.UUID, status domain.VerificationStatus, details *string) error {
	return nil
}
func (m *mockBackupRepo) TombstoneArtifact(ctx context.Context, orgID, artifactID uuid.UUID) error {
	return nil
}
func (m *mockBackupRepo) GetRunArtifacts(ctx context.Context, orgID, runID uuid.UUID) ([]*domain.BackupArtifact, error) {
	return nil, nil
}
func (m *mockBackupRepo) GetRunDetail(ctx context.Context, orgID, runID uuid.UUID) (*domain.BackupRunWithStats, error) {
	return nil, domain.ErrRunNotFound
}
func (m *mockBackupRepo) ListRuns(ctx context.Context, orgID uuid.UUID, filter domain.RunFilter) ([]*domain.BackupRunWithStats, error) {
	return nil, nil
}
func (m *mockBackupRepo) GetArtifactByID(ctx context.Context, orgID, artifactID uuid.UUID) (*domain.BackupArtifact, error) {
	return nil, domain.ErrArtifactNotFound
}
func (m *mockBackupRepo) ListArtifacts(ctx context.Context, orgID uuid.UUID) ([]*domain.BackupArtifact, error) {
	return nil, nil
}
func (m *mockBackupRepo) RecoverInterruptedRuns(ctx context.Context) (int, error) {
	return 0, nil
}
func (m *mockBackupRepo) ReapStaleRuns(ctx context.Context) (int, error) {
	return 0, nil
}

type mockResourceFinder struct {
	resources map[uuid.UUID]*resDomain.Resource
}

func (m *mockResourceFinder) GetByID(ctx context.Context, orgID, resourceID uuid.UUID) (*resDomain.Resource, error) {
	if r, ok := m.resources[resourceID]; ok && r.OrganizationID == orgID {
		return r, nil
	}
	return nil, resDomain.ErrResourceNotFound
}

func TestBackupJobService_CreateManualJob(t *testing.T) {
	ctx := context.Background()
	orgID := uuid.New()
	userID := uuid.New()
	resID := uuid.New()

	activeResource := &resDomain.Resource{
		ID:             resID,
		OrganizationID: orgID,
		Name:           "Production DB Server",
		Type:           resDomain.TypeUbuntuSSH,
		Status:         resDomain.StatusActive,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	t.Run("Admin creates valid ad-hoc manual backup", func(t *testing.T) {
		repo := &mockBackupRepo{jobs: make(map[uuid.UUID]*domain.BackupJob), plans: make(map[uuid.UUID]*domain.BackupPlan)}
		rf := &mockResourceFinder{resources: map[uuid.UUID]*resDomain.Resource{resID: activeResource}}
		svc := NewBackupJobService(repo, rf)

		input := CreateManualJobInput{
			ResourceID: &resID,
			BackupType: domain.BackupTypeMySQLDatabase,
			TargetSpec: &domain.TargetSpec{
				Databases: []string{"ecommerce_prod"},
			},
		}

		job, err := svc.CreateManualJob(ctx, orgDomain.RoleAdmin, orgID, userID, input)
		if err != nil {
			t.Fatalf("unexpected error creating job: %v", err)
		}

		if job.Status != domain.JobStatusPending {
			t.Errorf("expected pending job status, got %s", job.Status)
		}
		if job.TriggerType != domain.TriggerTypeManual {
			t.Errorf("expected manual trigger type, got %s", job.TriggerType)
		}
		if job.ResourceID != resID {
			t.Errorf("expected resource id %s, got %s", resID, job.ResourceID)
		}
	})

	t.Run("Member forbidden from creating ad-hoc manual backup", func(t *testing.T) {
		repo := &mockBackupRepo{jobs: make(map[uuid.UUID]*domain.BackupJob)}
		rf := &mockResourceFinder{resources: map[uuid.UUID]*resDomain.Resource{resID: activeResource}}
		svc := NewBackupJobService(repo, rf)

		input := CreateManualJobInput{
			ResourceID: &resID,
			BackupType: domain.BackupTypeMySQLDatabase,
			TargetSpec: &domain.TargetSpec{Databases: []string{"ecommerce_prod"}},
		}

		_, err := svc.CreateManualJob(ctx, orgDomain.RoleMember, orgID, userID, input)
		if !errors.Is(err, domain.ErrUnauthorizedRole) {
			t.Fatalf("expected ErrUnauthorizedRole for member ad-hoc, got: %v", err)
		}
	})

	t.Run("Viewer forbidden from creating any backup", func(t *testing.T) {
		repo := &mockBackupRepo{jobs: make(map[uuid.UUID]*domain.BackupJob)}
		rf := &mockResourceFinder{resources: map[uuid.UUID]*resDomain.Resource{resID: activeResource}}
		svc := NewBackupJobService(repo, rf)

		input := CreateManualJobInput{
			ResourceID: &resID,
			BackupType: domain.BackupTypeMySQLDatabase,
			TargetSpec: &domain.TargetSpec{Databases: []string{"ecommerce_prod"}},
		}

		_, err := svc.CreateManualJob(ctx, orgDomain.RoleViewer, orgID, userID, input)
		if !errors.Is(err, domain.ErrUnauthorizedRole) {
			t.Fatalf("expected ErrUnauthorizedRole for viewer, got: %v", err)
		}
	})

	t.Run("Member can trigger active plan", func(t *testing.T) {
		planID := uuid.New()
		plan := &domain.BackupPlan{
			ID:             planID,
			OrganizationID: orgID,
			ResourceID:     resID,
			Name:           "Daily DB Plan",
			BackupType:     domain.BackupTypeMySQLDatabase,
			TargetSpec:     domain.TargetSpec{Databases: []string{"ecommerce_prod"}},
			Status:         domain.PlanStatusActive,
		}

		repo := &mockBackupRepo{
			jobs:  make(map[uuid.UUID]*domain.BackupJob),
			plans: map[uuid.UUID]*domain.BackupPlan{planID: plan},
		}
		rf := &mockResourceFinder{resources: map[uuid.UUID]*resDomain.Resource{resID: activeResource}}
		svc := NewBackupJobService(repo, rf)

		input := CreateManualJobInput{
			BackupPlanID: &planID,
		}

		job, err := svc.CreateManualJob(ctx, orgDomain.RoleMember, orgID, userID, input)
		if err != nil {
			t.Fatalf("unexpected error creating plan-triggered job: %v", err)
		}

		if job.BackupPlanID == nil || *job.BackupPlanID != planID {
			t.Errorf("expected job plan id %s, got %v", planID, job.BackupPlanID)
		}
	})

	t.Run("Resource disabled returns ErrResourceDisabled", func(t *testing.T) {
		disabledResID := uuid.New()
		disabledRes := &resDomain.Resource{
			ID:             disabledResID,
			OrganizationID: orgID,
			Name:           "Disabled DB",
			Type:           resDomain.TypeUbuntuSSH,
			Status:         resDomain.StatusDisabled,
		}

		repo := &mockBackupRepo{jobs: make(map[uuid.UUID]*domain.BackupJob)}
		rf := &mockResourceFinder{resources: map[uuid.UUID]*resDomain.Resource{disabledResID: disabledRes}}
		svc := NewBackupJobService(repo, rf)

		input := CreateManualJobInput{
			ResourceID: &disabledResID,
			BackupType: domain.BackupTypeMySQLDatabase,
			TargetSpec: &domain.TargetSpec{Databases: []string{"ecommerce_prod"}},
		}

		_, err := svc.CreateManualJob(ctx, orgDomain.RoleAdmin, orgID, userID, input)
		if !errors.Is(err, domain.ErrResourceDisabled) {
			t.Fatalf("expected ErrResourceDisabled, got: %v", err)
		}
	})

	t.Run("Conflict returns ErrManualBackupConflict", func(t *testing.T) {
		repo := &mockBackupRepo{
			jobs:              make(map[uuid.UUID]*domain.BackupJob),
			activeJobConflict: &domain.BackupJob{ID: uuid.New(), Status: domain.JobStatusRunning},
		}
		rf := &mockResourceFinder{resources: map[uuid.UUID]*resDomain.Resource{resID: activeResource}}
		svc := NewBackupJobService(repo, rf)

		input := CreateManualJobInput{
			ResourceID: &resID,
			BackupType: domain.BackupTypeMySQLDatabase,
			TargetSpec: &domain.TargetSpec{Databases: []string{"ecommerce_prod"}},
		}

		_, err := svc.CreateManualJob(ctx, orgDomain.RoleAdmin, orgID, userID, input)
		if !errors.Is(err, domain.ErrManualBackupConflict) {
			t.Fatalf("expected ErrManualBackupConflict, got: %v", err)
		}
	})

	t.Run("Admin creates valid ad-hoc website_files job on ubuntu_ssh with normalized paths", func(t *testing.T) {
		repo := &mockBackupRepo{jobs: make(map[uuid.UUID]*domain.BackupJob)}
		rf := &mockResourceFinder{resources: map[uuid.UUID]*resDomain.Resource{resID: activeResource}}
		svc := NewBackupJobService(repo, rf)

		emptyExcludes := []string{}
		input := CreateManualJobInput{
			ResourceID: &resID,
			BackupType: domain.BackupTypeWebsiteFiles,
			TargetSpec: &domain.TargetSpec{
				Paths:           []string{"/var/www/site/", "/var//www/./site2"},
				ExcludePatterns: &emptyExcludes,
			},
		}

		job, err := svc.CreateManualJob(ctx, orgDomain.RoleAdmin, orgID, userID, input)
		if err != nil {
			t.Fatalf("unexpected error creating website_files job: %v", err)
		}

		if job.BackupType != domain.BackupTypeWebsiteFiles {
			t.Errorf("expected backup type website_files, got %s", job.BackupType)
		}
		if len(job.TargetSpec.Paths) != 2 || job.TargetSpec.Paths[0] != "/var/www/site" || job.TargetSpec.Paths[1] != "/var/www/site2" {
			t.Errorf("expected normalized paths, got %v", job.TargetSpec.Paths)
		}
	})

	t.Run("Member can trigger active website_files plan", func(t *testing.T) {
		planID := uuid.New()
		emptyExcludes := []string{}
		plan := &domain.BackupPlan{
			ID:             planID,
			OrganizationID: orgID,
			ResourceID:     resID,
			Name:           "Daily Website Backup",
			BackupType:     domain.BackupTypeWebsiteFiles,
			TargetSpec: domain.TargetSpec{
				Paths:           []string{"/var/www/site"},
				ExcludePatterns: &emptyExcludes,
			},
			Status: domain.PlanStatusActive,
		}

		repo := &mockBackupRepo{
			jobs:  make(map[uuid.UUID]*domain.BackupJob),
			plans: map[uuid.UUID]*domain.BackupPlan{planID: plan},
		}
		rf := &mockResourceFinder{resources: map[uuid.UUID]*resDomain.Resource{resID: activeResource}}
		svc := NewBackupJobService(repo, rf)

		input := CreateManualJobInput{
			BackupPlanID: &planID,
		}

		job, err := svc.CreateManualJob(ctx, orgDomain.RoleMember, orgID, userID, input)
		if err != nil {
			t.Fatalf("unexpected error creating plan-triggered website job: %v", err)
		}

		if job.BackupPlanID == nil || *job.BackupPlanID != planID {
			t.Errorf("expected job plan id %s, got %v", planID, job.BackupPlanID)
		}
	})

	t.Run("Rejects website_files job on cPanel resource", func(t *testing.T) {
		cpanelResID := uuid.New()
		cpanelRes := &resDomain.Resource{
			ID:             cpanelResID,
			OrganizationID: orgID,
			Name:           "cPanel Shared",
			Type:           resDomain.TypeCPanel,
			Status:         resDomain.StatusActive,
		}

		repo := &mockBackupRepo{jobs: make(map[uuid.UUID]*domain.BackupJob)}
		rf := &mockResourceFinder{resources: map[uuid.UUID]*resDomain.Resource{cpanelResID: cpanelRes}}
		svc := NewBackupJobService(repo, rf)

		emptyExcludes := []string{}
		input := CreateManualJobInput{
			ResourceID: &cpanelResID,
			BackupType: domain.BackupTypeWebsiteFiles,
			TargetSpec: &domain.TargetSpec{
				Paths:           []string{"/home/user/public_html"},
				ExcludePatterns: &emptyExcludes,
			},
		}

		_, err := svc.CreateManualJob(ctx, orgDomain.RoleAdmin, orgID, userID, input)
		if !errors.Is(err, domain.ErrUnsupportedResourceType) {
			t.Fatalf("expected ErrUnsupportedResourceType for cPanel website backup, got: %v", err)
		}
	})

	t.Run("Rejects both backup type", func(t *testing.T) {
		repo := &mockBackupRepo{jobs: make(map[uuid.UUID]*domain.BackupJob)}
		rf := &mockResourceFinder{resources: map[uuid.UUID]*resDomain.Resource{resID: activeResource}}
		svc := NewBackupJobService(repo, rf)

		input := CreateManualJobInput{
			ResourceID: &resID,
			BackupType: domain.BackupTypeBoth,
			TargetSpec: &domain.TargetSpec{Databases: []string{"test_db"}},
		}

		_, err := svc.CreateManualJob(ctx, orgDomain.RoleAdmin, orgID, userID, input)
		if !errors.Is(err, domain.ErrUnsupportedBackupType) {
			t.Fatalf("expected ErrUnsupportedBackupType for both, got: %v", err)
		}
	})

	t.Run("Ad-hoc manual MySQL job with empty databases list is rejected with ErrInvalidTargetSpec", func(t *testing.T) {
		repo := &mockBackupRepo{jobs: make(map[uuid.UUID]*domain.BackupJob)}
		rf := &mockResourceFinder{resources: map[uuid.UUID]*resDomain.Resource{resID: activeResource}}
		svc := NewBackupJobService(repo, rf)

		input := CreateManualJobInput{
			ResourceID: &resID,
			BackupType: domain.BackupTypeMySQLDatabase,
			TargetSpec: &domain.TargetSpec{Databases: []string{}},
		}

		_, err := svc.CreateManualJob(ctx, orgDomain.RoleAdmin, orgID, userID, input)
		if !errors.Is(err, domain.ErrInvalidTargetSpec) {
			t.Fatalf("expected ErrInvalidTargetSpec for ad-hoc empty databases, got: %v", err)
		}
	})

	t.Run("Ad-hoc manual MySQL job with empty target spec is rejected with ErrInvalidTargetSpec", func(t *testing.T) {
		repo := &mockBackupRepo{jobs: make(map[uuid.UUID]*domain.BackupJob)}
		rf := &mockResourceFinder{resources: map[uuid.UUID]*resDomain.Resource{resID: activeResource}}
		svc := NewBackupJobService(repo, rf)

		input := CreateManualJobInput{
			ResourceID: &resID,
			BackupType: domain.BackupTypeMySQLDatabase,
			TargetSpec: &domain.TargetSpec{},
		}

		_, err := svc.CreateManualJob(ctx, orgDomain.RoleAdmin, orgID, userID, input)
		if !errors.Is(err, domain.ErrInvalidTargetSpec) {
			t.Fatalf("expected ErrInvalidTargetSpec for ad-hoc empty target spec, got: %v", err)
		}
	})

	t.Run("Plan-triggered job with mode all plan succeeds", func(t *testing.T) {
		planID := uuid.New()
		plan := &domain.BackupPlan{
			ID:             planID,
			OrganizationID: orgID,
			ResourceID:     resID,
			Name:           "All DB Plan",
			BackupType:     domain.BackupTypeMySQLDatabase,
			TargetSpec:     domain.TargetSpec{Databases: []string{}}, // mode = "all"
			Status:         domain.PlanStatusActive,
		}

		repo := &mockBackupRepo{
			jobs:  make(map[uuid.UUID]*domain.BackupJob),
			plans: map[uuid.UUID]*domain.BackupPlan{planID: plan},
		}
		rf := &mockResourceFinder{resources: map[uuid.UUID]*resDomain.Resource{resID: activeResource}}
		svc := NewBackupJobService(repo, rf)

		input := CreateManualJobInput{
			BackupPlanID: &planID,
		}

		job, err := svc.CreateManualJob(ctx, orgDomain.RoleMember, orgID, userID, input)
		if err != nil {
			t.Fatalf("unexpected error creating plan-triggered mode all job: %v", err)
		}

		if job.BackupPlanID == nil || *job.BackupPlanID != planID {
			t.Errorf("expected job plan id %s, got %v", planID, job.BackupPlanID)
		}
		if len(job.TargetSpec.Databases) != 0 {
			t.Errorf("expected empty databases (mode all) for job, got %v", job.TargetSpec.Databases)
		}
	})
}
