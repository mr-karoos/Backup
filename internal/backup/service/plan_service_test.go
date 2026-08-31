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

type fakePlanRepo struct {
	plans     map[uuid.UUID]*domain.BackupPlan
	resources map[uuid.UUID]string
}

func newFakePlanRepo() *fakePlanRepo {
	return &fakePlanRepo{
		plans:     make(map[uuid.UUID]*domain.BackupPlan),
		resources: make(map[uuid.UUID]string),
	}
}

func (f *fakePlanRepo) CreatePlan(ctx context.Context, plan *domain.BackupPlan) (*domain.BackupPlan, error) {
	plan.CreatedAt = time.Now()
	plan.UpdatedAt = time.Now()
	f.plans[plan.ID] = plan
	return plan, nil
}

func (f *fakePlanRepo) GetPlanByID(ctx context.Context, orgID, planID uuid.UUID) (*domain.BackupPlan, error) {
	p, ok := f.plans[planID]
	if !ok || p.OrganizationID != orgID {
		return nil, domain.ErrPlanNotFound
	}
	return p, nil
}

func (f *fakePlanRepo) GetPlanWithResourceByID(ctx context.Context, orgID, planID uuid.UUID) (*domain.BackupPlanWithResource, error) {
	p, err := f.GetPlanByID(ctx, orgID, planID)
	if err != nil {
		return nil, err
	}
	resName := f.resources[p.ResourceID]
	if resName == "" {
		resName = "Default Resource"
	}
	return &domain.BackupPlanWithResource{
		Plan:         *p,
		ResourceName: resName,
	}, nil
}

func (f *fakePlanRepo) ListPlans(ctx context.Context, orgID uuid.UUID, filter domain.PlanFilter) ([]*domain.BackupPlanWithResource, error) {
	var list []*domain.BackupPlanWithResource
	for _, p := range f.plans {
		if p.OrganizationID != orgID {
			continue
		}
		if filter.ResourceID != nil && p.ResourceID != *filter.ResourceID {
			continue
		}
		if filter.Status != nil {
			if p.Status != *filter.Status {
				continue
			}
		} else {
			if p.Status == domain.PlanStatusArchived {
				continue
			}
		}
		resName := f.resources[p.ResourceID]
		list = append(list, &domain.BackupPlanWithResource{
			Plan:         *p,
			ResourceName: resName,
		})
	}
	return list, nil
}

func (f *fakePlanRepo) UpdatePlan(ctx context.Context, plan *domain.BackupPlan) (*domain.BackupPlan, error) {
	existing, ok := f.plans[plan.ID]
	if !ok || existing.OrganizationID != plan.OrganizationID {
		return nil, domain.ErrPlanNotFound
	}
	if existing.Status == domain.PlanStatusArchived {
		return nil, domain.ErrPlanAlreadyArchived
	}
	plan.UpdatedAt = time.Now()
	f.plans[plan.ID] = plan
	return plan, nil
}

func (f *fakePlanRepo) ArchivePlan(ctx context.Context, orgID, planID uuid.UUID) error {
	p, ok := f.plans[planID]
	if !ok || p.OrganizationID != orgID {
		return domain.ErrPlanNotFound
	}
	p.Status = domain.PlanStatusArchived
	p.NextRunAt = nil
	p.UpdatedAt = time.Now()
	return nil
}

type fakeResFinder struct {
	resources map[uuid.UUID]*resDomain.Resource
}

func (f *fakeResFinder) GetByID(ctx context.Context, orgID, resourceID uuid.UUID) (*resDomain.Resource, error) {
	r, ok := f.resources[resourceID]
	if !ok || r.OrganizationID != orgID {
		return nil, resDomain.ErrResourceNotFound
	}
	return r, nil
}

func TestBackupPlanService_CreatePlan(t *testing.T) {
	orgID := uuid.New()
	resID := uuid.New()
	cron := "0 2 * * *"
	count := 7
	days := 30

	validResource := &resDomain.Resource{
		ID:             resID,
		OrganizationID: orgID,
		Name:           "Production DB",
		Type:           resDomain.TypeUbuntuSSH,
		Status:         resDomain.StatusActive,
	}

	resFinder := &fakeResFinder{
		resources: map[uuid.UUID]*resDomain.Resource{
			resID: validResource,
		},
	}

	t.Run("Admin successfully creates MySQL plan with selected databases", func(t *testing.T) {
		repo := newFakePlanRepo()
		svc := NewBackupPlanService(repo, resFinder)
		fixedNow := time.Date(2026, 8, 20, 1, 0, 0, 0, time.UTC)
		svc.SetNowFunc(func() time.Time { return fixedNow })

		input := CreatePlanInput{
			Name:       "Daily MySQL Plan",
			ResourceID: resID,
			BackupType: domain.BackupTypeMySQLDatabase,
			DatabaseSelection: &DatabaseSelectionInput{
				Mode:      "selected",
				Databases: []string{"ecommerce_prod", "analytics_dw"},
			},
			Schedule: ScheduleInput{
				IsEnabled:      true,
				CronExpression: &cron,
				Timezone:       "Asia/Tehran",
			},
			RetentionPolicy: &RetentionPolicyInput{
				KeepLastN: &count,
				KeepDays:  &days,
			},
		}

		plan, err := svc.CreatePlan(context.Background(), orgDomain.RoleAdmin, orgID, input)
		if err != nil {
			t.Fatalf("CreatePlan failed: %v", err)
		}
		if plan == nil || plan.ID == uuid.Nil {
			t.Fatalf("expected non-nil plan with valid UUID")
		}
		if plan.NextRunAt == nil {
			t.Fatalf("expected non-nil NextRunAt for enabled plan")
		}
		if len(plan.TargetSpec.Databases) != 2 {
			t.Fatalf("expected 2 databases in TargetSpec, got %d", len(plan.TargetSpec.Databases))
		}
	})

	t.Run("Admin successfully creates MySQL plan with mode all", func(t *testing.T) {
		repo := newFakePlanRepo()
		svc := NewBackupPlanService(repo, resFinder)
		fixedNow := time.Date(2026, 8, 20, 1, 0, 0, 0, time.UTC)
		svc.SetNowFunc(func() time.Time { return fixedNow })

		input := CreatePlanInput{
			Name:       "All MySQL Plan",
			ResourceID: resID,
			BackupType: domain.BackupTypeMySQLDatabase,
			DatabaseSelection: &DatabaseSelectionInput{
				Mode: "all",
			},
			Schedule: ScheduleInput{
				IsEnabled:      true,
				CronExpression: &cron,
				Timezone:       "UTC",
			},
		}

		plan, err := svc.CreatePlan(context.Background(), orgDomain.RoleAdmin, orgID, input)
		if err != nil {
			t.Fatalf("expected success for mode all, got: %v", err)
		}
		if plan == nil || plan.ID == uuid.Nil {
			t.Fatalf("expected non-nil plan with valid UUID")
		}
		if len(plan.TargetSpec.Databases) != 0 {
			t.Fatalf("expected empty databases for mode all, got: %+v", plan.TargetSpec.Databases)
		}
	})

	t.Run("Admin creating MySQL plan with invalid mode returns ErrInvalidTargetSpec", func(t *testing.T) {
		repo := newFakePlanRepo()
		svc := NewBackupPlanService(repo, resFinder)

		input := CreatePlanInput{
			Name:       "Invalid Mode Plan",
			ResourceID: resID,
			BackupType: domain.BackupTypeMySQLDatabase,
			DatabaseSelection: &DatabaseSelectionInput{
				Mode: "invalid_mode",
			},
			Schedule: ScheduleInput{
				IsEnabled:      true,
				CronExpression: &cron,
				Timezone:       "UTC",
			},
		}

		_, err := svc.CreatePlan(context.Background(), orgDomain.RoleAdmin, orgID, input)
		if !errors.Is(err, domain.ErrInvalidTargetSpec) {
			t.Fatalf("expected ErrInvalidTargetSpec, got: %v", err)
		}
	})

	t.Run("Admin creating MySQL plan with selected mode but empty databases returns ErrInvalidTargetSpec", func(t *testing.T) {
		repo := newFakePlanRepo()
		svc := NewBackupPlanService(repo, resFinder)

		input := CreatePlanInput{
			Name:       "Selected Mode Empty Plan",
			ResourceID: resID,
			BackupType: domain.BackupTypeMySQLDatabase,
			DatabaseSelection: &DatabaseSelectionInput{
				Mode:      "selected",
				Databases: []string{},
			},
			Schedule: ScheduleInput{
				IsEnabled:      true,
				CronExpression: &cron,
				Timezone:       "UTC",
			},
		}

		_, err := svc.CreatePlan(context.Background(), orgDomain.RoleAdmin, orgID, input)
		if !errors.Is(err, domain.ErrInvalidTargetSpec) {
			t.Fatalf("expected ErrInvalidTargetSpec, got: %v", err)
		}
	})

	t.Run("Admin creating MySQL plan with mode all but non-empty databases returns ErrInvalidTargetSpec", func(t *testing.T) {
		repo := newFakePlanRepo()
		svc := NewBackupPlanService(repo, resFinder)

		input := CreatePlanInput{
			Name:       "Conflicting Mode All Plan",
			ResourceID: resID,
			BackupType: domain.BackupTypeMySQLDatabase,
			DatabaseSelection: &DatabaseSelectionInput{
				Mode:      "all",
				Databases: []string{"db1"},
			},
			Schedule: ScheduleInput{
				IsEnabled:      true,
				CronExpression: &cron,
				Timezone:       "UTC",
			},
		}

		_, err := svc.CreatePlan(context.Background(), orgDomain.RoleAdmin, orgID, input)
		if !errors.Is(err, domain.ErrInvalidTargetSpec) {
			t.Fatalf("expected ErrInvalidTargetSpec for mode all with non-empty databases, got: %v", err)
		}
	})

	t.Run("Admin successfully creates Website files plan", func(t *testing.T) {
		repo := newFakePlanRepo()
		svc := NewBackupPlanService(repo, resFinder)

		emptyExcludes := []string{}
		input := CreatePlanInput{
			Name:       "Website Files Plan",
			ResourceID: resID,
			BackupType: domain.BackupTypeWebsiteFiles,
			FileSelection: &FileSelectionInput{
				Paths:           []string{"/var/www/site"},
				ExcludePatterns: &emptyExcludes,
			},
			Schedule: ScheduleInput{
				IsEnabled:      true,
				CronExpression: &cron,
				Timezone:       "UTC",
			},
		}

		plan, err := svc.CreatePlan(context.Background(), orgDomain.RoleAdmin, orgID, input)
		if err != nil {
			t.Fatalf("CreatePlan failed: %v", err)
		}
		if len(plan.TargetSpec.Paths) != 1 || plan.TargetSpec.Paths[0] != "/var/www/site" {
			t.Fatalf("unexpected paths in target spec: %+v", plan.TargetSpec)
		}
	})

	t.Run("Creating plan with backup_type both returns ErrUnsupportedBackupType", func(t *testing.T) {
		repo := newFakePlanRepo()
		svc := NewBackupPlanService(repo, resFinder)

		input := CreatePlanInput{
			Name:       "Both Plan",
			ResourceID: resID,
			BackupType: domain.BackupTypeBoth,
			Schedule: ScheduleInput{
				IsEnabled:      true,
				CronExpression: &cron,
				Timezone:       "UTC",
			},
		}

		_, err := svc.CreatePlan(context.Background(), orgDomain.RoleAdmin, orgID, input)
		if !errors.Is(err, domain.ErrUnsupportedBackupType) {
			t.Fatalf("expected ErrUnsupportedBackupType, got: %v", err)
		}
	})

	t.Run("Creating plan on cPanel resource returns ErrUnsupportedResourceType", func(t *testing.T) {
		cpanelResID := uuid.New()
		cpanelResource := &resDomain.Resource{
			ID:             cpanelResID,
			OrganizationID: orgID,
			Name:           "cPanel Shared Host",
			Type:           resDomain.TypeCPanel,
			Status:         resDomain.StatusActive,
		}
		localResFinder := &fakeResFinder{
			resources: map[uuid.UUID]*resDomain.Resource{
				cpanelResID: cpanelResource,
			},
		}
		repo := newFakePlanRepo()
		svc := NewBackupPlanService(repo, localResFinder)

		input := CreatePlanInput{
			Name:       "cPanel Plan",
			ResourceID: cpanelResID,
			BackupType: domain.BackupTypeMySQLDatabase,
			DatabaseSelection: &DatabaseSelectionInput{
				Mode:      "selected",
				Databases: []string{"db1"},
			},
			Schedule: ScheduleInput{
				IsEnabled:      true,
				CronExpression: &cron,
				Timezone:       "UTC",
			},
		}

		_, err := svc.CreatePlan(context.Background(), orgDomain.RoleAdmin, orgID, input)
		if !errors.Is(err, domain.ErrUnsupportedResourceType) {
			t.Fatalf("expected ErrUnsupportedResourceType, got: %v", err)
		}
	})

	t.Run("Creating plan on disabled resource returns ErrResourceDisabled", func(t *testing.T) {
		disabledResID := uuid.New()
		disabledRes := &resDomain.Resource{
			ID:             disabledResID,
			OrganizationID: orgID,
			Name:           "Disabled Server",
			Type:           resDomain.TypeUbuntuSSH,
			Status:         resDomain.StatusDisabled,
		}
		localResFinder := &fakeResFinder{
			resources: map[uuid.UUID]*resDomain.Resource{
				disabledResID: disabledRes,
			},
		}
		repo := newFakePlanRepo()
		svc := NewBackupPlanService(repo, localResFinder)

		input := CreatePlanInput{
			Name:       "Disabled Plan",
			ResourceID: disabledResID,
			BackupType: domain.BackupTypeMySQLDatabase,
			DatabaseSelection: &DatabaseSelectionInput{
				Mode:      "selected",
				Databases: []string{"db1"},
			},
			Schedule: ScheduleInput{
				IsEnabled:      true,
				CronExpression: &cron,
				Timezone:       "UTC",
			},
		}

		_, err := svc.CreatePlan(context.Background(), orgDomain.RoleAdmin, orgID, input)
		if !errors.Is(err, domain.ErrResourceDisabled) {
			t.Fatalf("expected ErrResourceDisabled, got: %v", err)
		}
	})

	t.Run("Member or Viewer creating plan returns ErrUnauthorizedRole", func(t *testing.T) {
		repo := newFakePlanRepo()
		svc := NewBackupPlanService(repo, resFinder)

		input := CreatePlanInput{
			Name:       "Member Plan",
			ResourceID: resID,
			BackupType: domain.BackupTypeMySQLDatabase,
			DatabaseSelection: &DatabaseSelectionInput{
				Mode:      "selected",
				Databases: []string{"db1"},
			},
			Schedule: ScheduleInput{
				IsEnabled:      true,
				CronExpression: &cron,
				Timezone:       "UTC",
			},
		}

		_, err := svc.CreatePlan(context.Background(), orgDomain.RoleMember, orgID, input)
		if !errors.Is(err, domain.ErrUnauthorizedRole) {
			t.Fatalf("expected ErrUnauthorizedRole for member, got: %v", err)
		}

		_, err = svc.CreatePlan(context.Background(), orgDomain.RoleViewer, orgID, input)
		if !errors.Is(err, domain.ErrUnauthorizedRole) {
			t.Fatalf("expected ErrUnauthorizedRole for viewer, got: %v", err)
		}
	})
}

func TestBackupPlanService_UpdateAndArchive(t *testing.T) {
	orgID := uuid.New()
	resID := uuid.New()
	cron := "0 2 * * *"

	validResource := &resDomain.Resource{
		ID:             resID,
		OrganizationID: orgID,
		Name:           "Production DB",
		Type:           resDomain.TypeUbuntuSSH,
		Status:         resDomain.StatusActive,
	}

	resFinder := &fakeResFinder{
		resources: map[uuid.UUID]*resDomain.Resource{
			resID: validResource,
		},
	}

	t.Run("Admin pauses and resumes plan", func(t *testing.T) {
		repo := newFakePlanRepo()
		svc := NewBackupPlanService(repo, resFinder)
		fixedNow := time.Date(2026, 8, 20, 1, 0, 0, 0, time.UTC)
		svc.SetNowFunc(func() time.Time { return fixedNow })

		// 1. Create active plan
		created, err := svc.CreatePlan(context.Background(), orgDomain.RoleAdmin, orgID, CreatePlanInput{
			Name:       "Plan 1",
			ResourceID: resID,
			BackupType: domain.BackupTypeMySQLDatabase,
			DatabaseSelection: &DatabaseSelectionInput{
				Mode:      "selected",
				Databases: []string{"db1"},
			},
			Schedule: ScheduleInput{
				IsEnabled:      true,
				CronExpression: &cron,
				Timezone:       "UTC",
			},
		})
		if err != nil {
			t.Fatalf("CreatePlan failed: %v", err)
		}
		if created.NextRunAt == nil {
			t.Fatalf("expected next_run_at calculated for active plan")
		}

		// 2. Pause plan
		paused, err := svc.UpdatePlan(context.Background(), orgDomain.RoleAdmin, orgID, created.ID, UpdatePlanInput{
			Name: "Plan 1 Paused",
			DatabaseSelection: &DatabaseSelectionInput{
				Mode:      "selected",
				Databases: []string{"db1"},
			},
			Schedule: ScheduleInput{
				IsEnabled:      true,
				CronExpression: &cron,
				Timezone:       "UTC",
			},
			Status: domain.PlanStatusPaused,
		})
		if err != nil {
			t.Fatalf("UpdatePlan pause failed: %v", err)
		}
		if paused.Status != domain.PlanStatusPaused || paused.NextRunAt != nil {
			t.Fatalf("expected status=paused and next_run_at=nil, got status=%s next_run=%v", paused.Status, paused.NextRunAt)
		}

		// 3. Resume plan
		resumed, err := svc.UpdatePlan(context.Background(), orgDomain.RoleAdmin, orgID, created.ID, UpdatePlanInput{
			Name: "Plan 1 Resumed",
			DatabaseSelection: &DatabaseSelectionInput{
				Mode:      "selected",
				Databases: []string{"db1"},
			},
			Schedule: ScheduleInput{
				IsEnabled:      true,
				CronExpression: &cron,
				Timezone:       "UTC",
			},
			Status: domain.PlanStatusActive,
		})
		if err != nil {
			t.Fatalf("UpdatePlan resume failed: %v", err)
		}
		if resumed.Status != domain.PlanStatusActive || resumed.NextRunAt == nil {
			t.Fatalf("expected status=active and next_run_at recomputed, got status=%s next_run=%v", resumed.Status, resumed.NextRunAt)
		}
	})

	t.Run("Updating archived plan returns ErrPlanAlreadyArchived", func(t *testing.T) {
		repo := newFakePlanRepo()
		svc := NewBackupPlanService(repo, resFinder)

		created, _ := svc.CreatePlan(context.Background(), orgDomain.RoleAdmin, orgID, CreatePlanInput{
			Name:       "Plan to Archive",
			ResourceID: resID,
			BackupType: domain.BackupTypeMySQLDatabase,
			DatabaseSelection: &DatabaseSelectionInput{
				Mode:      "selected",
				Databases: []string{"db1"},
			},
			Schedule: ScheduleInput{
				IsEnabled:      true,
				CronExpression: &cron,
				Timezone:       "UTC",
			},
		})

		// Archive plan
		if err := svc.ArchivePlan(context.Background(), orgDomain.RoleAdmin, orgID, created.ID); err != nil {
			t.Fatalf("ArchivePlan failed: %v", err)
		}

		// Try updating archived plan
		_, err := svc.UpdatePlan(context.Background(), orgDomain.RoleAdmin, orgID, created.ID, UpdatePlanInput{
			Name: "Try Updating",
			DatabaseSelection: &DatabaseSelectionInput{
				Mode:      "selected",
				Databases: []string{"db1"},
			},
			Schedule: ScheduleInput{
				IsEnabled:      true,
				CronExpression: &cron,
				Timezone:       "UTC",
			},
			Status: domain.PlanStatusActive,
		})
		if !errors.Is(err, domain.ErrPlanAlreadyArchived) {
			t.Fatalf("expected ErrPlanAlreadyArchived, got: %v", err)
		}
	})

	t.Run("ArchivePlan is idempotent on already archived plan", func(t *testing.T) {
		repo := newFakePlanRepo()
		svc := NewBackupPlanService(repo, resFinder)

		created, _ := svc.CreatePlan(context.Background(), orgDomain.RoleAdmin, orgID, CreatePlanInput{
			Name:       "Plan Archive Idempotency",
			ResourceID: resID,
			BackupType: domain.BackupTypeMySQLDatabase,
			DatabaseSelection: &DatabaseSelectionInput{
				Mode:      "selected",
				Databases: []string{"db1"},
			},
			Schedule: ScheduleInput{
				IsEnabled:      true,
				CronExpression: &cron,
				Timezone:       "UTC",
			},
		})

		if err := svc.ArchivePlan(context.Background(), orgDomain.RoleAdmin, orgID, created.ID); err != nil {
			t.Fatalf("first archive failed: %v", err)
		}
		if err := svc.ArchivePlan(context.Background(), orgDomain.RoleAdmin, orgID, created.ID); err != nil {
			t.Fatalf("second archive failed (should be idempotent): %v", err)
		}
	})
}
