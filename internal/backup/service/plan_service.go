package service

import (
	"context"
	"errors"
	"time"

	"backup-platform/internal/backup/domain"
	"backup-platform/internal/backup/repository"
	"backup-platform/internal/backup/scheduling"
	orgDomain "backup-platform/internal/organization/domain"
	resDomain "backup-platform/internal/resource/domain"
	"backup-platform/pkg/uuid"
)

// DatabaseSelectionInput defines database target configuration for MySQL plans.
type DatabaseSelectionInput struct {
	Mode      string   `json:"mode"`
	Databases []string `json:"databases,omitempty"`
}

// FileSelectionInput defines file and directory target configuration for website plans.
type FileSelectionInput struct {
	Paths           []string  `json:"paths"`
	ExcludePatterns *[]string `json:"exclude_patterns"`
}

// ScheduleInput defines scheduling configuration for a backup plan.
type ScheduleInput struct {
	IsEnabled      bool    `json:"is_enabled"`
	CronExpression *string `json:"cron_expression,omitempty"`
	Timezone       string  `json:"timezone"`
}

// RetentionPolicyInput defines retention thresholds for a backup plan.
type RetentionPolicyInput struct {
	KeepLastN *int `json:"keep_last_n,omitempty"`
	KeepDays  *int `json:"keep_days,omitempty"`
}

// CreatePlanInput encapsulates parameters for creating a new BackupPlan.
type CreatePlanInput struct {
	Name              string                  `json:"name"`
	ResourceID        uuid.UUID               `json:"resource_id"`
	BackupType        domain.BackupType       `json:"backup_type"`
	EngineType        *domain.EngineType      `json:"engine_type,omitempty"`
	StorageTargetID   *uuid.UUID              `json:"storage_target_id,omitempty"`
	DatabaseSelection *DatabaseSelectionInput `json:"database_selection,omitempty"`
	FileSelection     *FileSelectionInput     `json:"file_selection,omitempty"`
	Schedule          ScheduleInput           `json:"schedule"`
	RetentionPolicy   *RetentionPolicyInput   `json:"retention_policy,omitempty"`
}

// UpdatePlanInput encapsulates parameters for updating an existing BackupPlan.
type UpdatePlanInput struct {
	Name              string                  `json:"name"`
	EngineType        *domain.EngineType      `json:"engine_type,omitempty"`
	StorageTargetID   *uuid.UUID              `json:"storage_target_id,omitempty"`
	DatabaseSelection *DatabaseSelectionInput `json:"database_selection,omitempty"`
	FileSelection     *FileSelectionInput     `json:"file_selection,omitempty"`
	Schedule          ScheduleInput           `json:"schedule"`
	RetentionPolicy   *RetentionPolicyInput   `json:"retention_policy,omitempty"`
	Status            domain.PlanStatus       `json:"status"`
}

// BackupPlanService coordinates business logic, authorization, validation, and persistence for backup plans.
type BackupPlanService struct {
	repo              repository.BackupPlanRepository
	storageTargetRepo repository.StorageTargetRepository
	resourceFinder    ResourceFinder
	nowFunc           func() time.Time
}

// NewBackupPlanService constructs a new BackupPlanService.
func NewBackupPlanService(repo repository.BackupPlanRepository, resourceFinder ResourceFinder) *BackupPlanService {
	svc := &BackupPlanService{
		repo:           repo,
		resourceFinder: resourceFinder,
		nowFunc:        time.Now,
	}
	if stRepo, ok := repo.(repository.StorageTargetRepository); ok {
		svc.storageTargetRepo = stRepo
	}
	return svc
}

// SetStorageTargetRepository configures a dedicated storage target repository if not already provided.
func (s *BackupPlanService) SetStorageTargetRepository(stRepo repository.StorageTargetRepository) {
	s.storageTargetRepo = stRepo
}

// SetNowFunc sets the custom time supplier for testing.
func (s *BackupPlanService) SetNowFunc(f func() time.Time) {
	if f != nil {
		s.nowFunc = f
	}
}

// CreatePlan validates tenant authorization, resource existence, target specification, and schedule before persisting a new plan.
func (s *BackupPlanService) CreatePlan(
	ctx context.Context,
	userRole orgDomain.Role,
	orgID uuid.UUID,
	input CreatePlanInput,
) (*domain.BackupPlan, error) {
	// Service-level RBAC: Only Admin can create backup plans
	if userRole != orgDomain.RoleAdmin {
		return nil, domain.ErrUnauthorizedRole
	}

	// 1. Validate Plan Name
	trimmedName, err := domain.ValidatePlanName(input.Name)
	if err != nil {
		return nil, domain.ErrInvalidPlanName
	}

	// 2. Validate Resource existence and eligibility
	if input.ResourceID == uuid.Nil {
		return nil, domain.ErrResourceNotFound
	}
	resource, err := s.resourceFinder.GetByID(ctx, orgID, input.ResourceID)
	if err != nil {
		if errors.Is(err, resDomain.ErrResourceNotFound) {
			return nil, domain.ErrResourceNotFound
		}
		return nil, domain.ErrBackupServiceUnavailable
	}
	if resource.Status == resDomain.StatusArchived {
		return nil, domain.ErrResourceNotFound
	}
	if resource.Status == resDomain.StatusDisabled {
		return nil, domain.ErrResourceDisabled
	}
	if resource.Type != resDomain.TypeUbuntuSSH {
		return nil, domain.ErrUnsupportedResourceType
	}

	// 3. Validate BackupType
	if input.BackupType != domain.BackupTypeMySQLDatabase && input.BackupType != domain.BackupTypeWebsiteFiles {
		return nil, domain.ErrUnsupportedBackupType
	}

	// 4. Validate and Normalize Target Specification from selection DTOs
	targetSpec, err := s.resolveTargetSpec(input.BackupType, input.DatabaseSelection, input.FileSelection)
	if err != nil {
		return nil, err
	}

	// 5. Validate Schedule & Timezone, compute initial next_run_at
	scheduleCron, scheduleTimezone, isEnabled, nextRunAt, err := s.resolveSchedule(input.Schedule, domain.PlanStatusActive)
	if err != nil {
		return nil, err
	}

	// 6. Validate Retention Policy
	var retentionCount, retentionDays *int
	if input.RetentionPolicy != nil {
		if err := domain.ValidateRetentionPolicy(input.RetentionPolicy.KeepLastN, input.RetentionPolicy.KeepDays); err != nil {
			return nil, domain.ErrInvalidRetentionPolicy
		}
		retentionCount = input.RetentionPolicy.KeepLastN
		retentionDays = input.RetentionPolicy.KeepDays
	}

	// 7. Validate and Resolve EngineType
	engineType := domain.EngineTypeDirectStream
	if input.EngineType != nil {
		if err := domain.ValidateEngineType(*input.EngineType); err != nil {
			return nil, err
		}
		engineType = *input.EngineType
	}

	// 8. Validate and Resolve StorageTarget
	var storageTargetID uuid.UUID
	if input.StorageTargetID != nil && *input.StorageTargetID != uuid.Nil {
		storageTargetID = *input.StorageTargetID
		if s.storageTargetRepo != nil {
			target, err := s.storageTargetRepo.GetStorageTargetByID(ctx, orgID, storageTargetID)
			if err != nil {
				if errors.Is(err, domain.ErrStorageTargetNotFound) || errors.Is(err, domain.ErrStorageTargetNotSupported) {
					return nil, domain.ErrStorageTargetNotFound
				}
				return nil, domain.ErrBackupServiceUnavailable
			}
			if target.Status != domain.StorageTargetStatusActive {
				return nil, domain.ErrStorageTargetNotActive
			}
			if !domain.IsEngineCompatibleWithStorage(engineType, target.Type) {
				return nil, domain.ErrIncompatibleEngineStorage
			}
		}
	} else if s.storageTargetRepo != nil {
		defaultTarget, err := s.storageTargetRepo.EnsureDefaultLocalStorageTarget(ctx, orgID)
		if err != nil {
			return nil, domain.ErrBackupServiceUnavailable
		}
		storageTargetID = defaultTarget.ID
	}

	// 9. Construct and persist plan
	plan := &domain.BackupPlan{
		ID:                uuid.New(),
		OrganizationID:    orgID,
		ResourceID:        input.ResourceID,
		Name:              trimmedName,
		BackupType:        input.BackupType,
		EngineType:        engineType,
		StorageTargetID:   storageTargetID,
		TargetSpec:        *targetSpec,
		ScheduleCron:      scheduleCron,
		ScheduleTimezone:  scheduleTimezone,
		IsScheduleEnabled: isEnabled,
		RetentionCount:    retentionCount,
		RetentionDays:     retentionDays,
		Status:            domain.PlanStatusActive,
		NextRunAt:         nextRunAt,
	}

	return s.repo.CreatePlan(ctx, plan)
}

// GetPlan retrieves a single plan by ID joined with its resource name for authorized roles.
func (s *BackupPlanService) GetPlan(
	ctx context.Context,
	userRole orgDomain.Role,
	orgID, planID uuid.UUID,
) (*domain.BackupPlanWithResource, error) {
	if userRole != orgDomain.RoleAdmin && userRole != orgDomain.RoleMember && userRole != orgDomain.RoleViewer {
		return nil, domain.ErrUnauthorizedRole
	}
	return s.repo.GetPlanWithResourceByID(ctx, orgID, planID)
}

// ListPlans lists plans matching the given filter for authorized roles.
func (s *BackupPlanService) ListPlans(
	ctx context.Context,
	userRole orgDomain.Role,
	orgID uuid.UUID,
	filter domain.PlanFilter,
) ([]*domain.BackupPlanWithResource, error) {
	if userRole != orgDomain.RoleAdmin && userRole != orgDomain.RoleMember && userRole != orgDomain.RoleViewer {
		return nil, domain.ErrUnauthorizedRole
	}
	return s.repo.ListPlans(ctx, orgID, filter)
}

// UpdatePlan applies a full update to an editable backup plan.
func (s *BackupPlanService) UpdatePlan(
	ctx context.Context,
	userRole orgDomain.Role,
	orgID, planID uuid.UUID,
	input UpdatePlanInput,
) (*domain.BackupPlan, error) {
	// Service-level RBAC: Only Admin can update backup plans
	if userRole != orgDomain.RoleAdmin {
		return nil, domain.ErrUnauthorizedRole
	}

	// 1. Fetch existing plan
	existing, err := s.repo.GetPlanByID(ctx, orgID, planID)
	if err != nil {
		return nil, err
	}
	if existing.Status == domain.PlanStatusArchived {
		return nil, domain.ErrPlanAlreadyArchived
	}

	// 2. Validate input status (only active or paused allowed in PUT)
	if input.Status != domain.PlanStatusActive && input.Status != domain.PlanStatusPaused {
		return nil, domain.ErrInvalidTargetSpec
	}

	// 3. Validate Plan Name
	trimmedName, err := domain.ValidatePlanName(input.Name)
	if err != nil {
		return nil, domain.ErrInvalidPlanName
	}

	// 4. Validate and Normalize Target Specification using existing.BackupType
	targetSpec, err := s.resolveTargetSpec(existing.BackupType, input.DatabaseSelection, input.FileSelection)
	if err != nil {
		return nil, err
	}

	// 5. Validate Schedule & Timezone, compute new next_run_at
	scheduleCron, scheduleTimezone, isEnabled, nextRunAt, err := s.resolveSchedule(input.Schedule, input.Status)
	if err != nil {
		return nil, err
	}

	// 6. Validate Retention Policy
	var retentionCount, retentionDays *int
	if input.RetentionPolicy != nil {
		if err := domain.ValidateRetentionPolicy(input.RetentionPolicy.KeepLastN, input.RetentionPolicy.KeepDays); err != nil {
			return nil, domain.ErrInvalidRetentionPolicy
		}
		retentionCount = input.RetentionPolicy.KeepLastN
		retentionDays = input.RetentionPolicy.KeepDays
	}

	// 7. Validate and Update EngineType
	engineType := existing.EngineType
	if input.EngineType != nil {
		if err := domain.ValidateEngineType(*input.EngineType); err != nil {
			return nil, err
		}
		engineType = *input.EngineType
	}

	// 8. Validate and Update StorageTarget
	storageTargetID := existing.StorageTargetID
	if input.StorageTargetID != nil && *input.StorageTargetID != uuid.Nil {
		storageTargetID = *input.StorageTargetID
		if s.storageTargetRepo != nil {
			target, err := s.storageTargetRepo.GetStorageTargetByID(ctx, orgID, storageTargetID)
			if err != nil {
				if errors.Is(err, domain.ErrStorageTargetNotFound) || errors.Is(err, domain.ErrStorageTargetNotSupported) {
					return nil, domain.ErrStorageTargetNotFound
				}
				return nil, domain.ErrBackupServiceUnavailable
			}
			if target.Status != domain.StorageTargetStatusActive {
				return nil, domain.ErrStorageTargetNotActive
			}
			if !domain.IsEngineCompatibleWithStorage(engineType, target.Type) {
				return nil, domain.ErrIncompatibleEngineStorage
			}
		}
	} else if s.storageTargetRepo != nil && storageTargetID != uuid.Nil {
		target, err := s.storageTargetRepo.GetStorageTargetByID(ctx, orgID, storageTargetID)
		if err == nil && target != nil {
			if !domain.IsEngineCompatibleWithStorage(engineType, target.Type) {
				return nil, domain.ErrIncompatibleEngineStorage
			}
		}
	}

	updatedPlan := &domain.BackupPlan{
		ID:                existing.ID,
		OrganizationID:    orgID,
		ResourceID:        existing.ResourceID,
		Name:              trimmedName,
		BackupType:        existing.BackupType,
		EngineType:        engineType,
		StorageTargetID:   storageTargetID,
		TargetSpec:        *targetSpec,
		ScheduleCron:      scheduleCron,
		ScheduleTimezone:  scheduleTimezone,
		IsScheduleEnabled: isEnabled,
		RetentionCount:    retentionCount,
		RetentionDays:     retentionDays,
		Status:            input.Status,
		NextRunAt:         nextRunAt,
	}

	return s.repo.UpdatePlan(ctx, updatedPlan)
}

// ArchivePlan soft-deletes a backup plan, clearing next_run_at.
func (s *BackupPlanService) ArchivePlan(
	ctx context.Context,
	userRole orgDomain.Role,
	orgID, planID uuid.UUID,
) error {
	// Service-level RBAC: Only Admin can archive backup plans
	if userRole != orgDomain.RoleAdmin {
		return domain.ErrUnauthorizedRole
	}

	existing, err := s.repo.GetPlanByID(ctx, orgID, planID)
	if err != nil {
		return err
	}
	if existing.Status == domain.PlanStatusArchived {
		return nil // Idempotent success
	}

	return s.repo.ArchivePlan(ctx, orgID, planID)
}

func (s *BackupPlanService) resolveTargetSpec(
	backupType domain.BackupType,
	dbSel *DatabaseSelectionInput,
	fileSel *FileSelectionInput,
) (*domain.TargetSpec, error) {
	switch backupType {
	case domain.BackupTypeMySQLDatabase:
		if fileSel != nil {
			return nil, domain.ErrInvalidTargetSpec
		}
		if dbSel == nil {
			return nil, domain.ErrInvalidTargetSpec
		}
		if dbSel.Mode == "all" {
			if len(dbSel.Databases) > 0 {
				return nil, domain.ErrInvalidTargetSpec
			}
			return &domain.TargetSpec{
				Databases: []string{},
			}, nil
		}
		if dbSel.Mode != "selected" {
			return nil, domain.ErrInvalidTargetSpec
		}
		if len(dbSel.Databases) == 0 {
			return nil, domain.ErrInvalidTargetSpec
		}

		rawSpec := &domain.TargetSpec{
			Databases: dbSel.Databases,
		}
		normalized, err := domain.NormalizeTargetSpec(domain.BackupTypeMySQLDatabase, rawSpec)
		if err != nil {
			return nil, domain.ErrInvalidTargetSpec
		}
		return normalized, nil

	case domain.BackupTypeWebsiteFiles:
		if dbSel != nil {
			return nil, domain.ErrInvalidTargetSpec
		}
		if fileSel == nil {
			return nil, domain.ErrInvalidTargetSpec
		}
		if len(fileSel.Paths) == 0 || fileSel.ExcludePatterns == nil {
			return nil, domain.ErrInvalidTargetSpec
		}

		rawSpec := &domain.TargetSpec{
			Paths:           fileSel.Paths,
			ExcludePatterns: fileSel.ExcludePatterns,
		}
		normalized, err := domain.NormalizeTargetSpec(domain.BackupTypeWebsiteFiles, rawSpec)
		if err != nil {
			return nil, domain.ErrInvalidTargetSpec
		}
		return normalized, nil

	default:
		return nil, domain.ErrUnsupportedBackupType
	}
}

func (s *BackupPlanService) resolveSchedule(
	schedule ScheduleInput,
	status domain.PlanStatus,
) (cronExpr *string, timezone string, isEnabled bool, nextRunAt *time.Time, err error) {
	// Validate Timezone (always required)
	if _, err := scheduling.ValidateTimezone(schedule.Timezone); err != nil {
		return nil, "", false, nil, domain.ErrInvalidTimezone
	}
	timezone = schedule.Timezone
	isEnabled = schedule.IsEnabled

	if schedule.IsEnabled {
		if schedule.CronExpression == nil || *schedule.CronExpression == "" {
			return nil, "", false, nil, domain.ErrInvalidCronExpression
		}
		if _, _, err := scheduling.ParseSchedule(*schedule.CronExpression, schedule.Timezone); err != nil {
			return nil, "", false, nil, domain.ErrInvalidCronExpression
		}
		cronExpr = schedule.CronExpression

		if status == domain.PlanStatusActive {
			nextRun, err := scheduling.CalculateNextRun(*cronExpr, timezone, s.nowFunc())
			if err != nil {
				return nil, "", false, nil, domain.ErrInvalidCronExpression
			}
			nextRunAt = nextRun
		}
	} else {
		// Schedule disabled -> preserve valid cron if supplied, next_run_at is nil
		if schedule.CronExpression != nil && *schedule.CronExpression != "" {
			if _, _, err := scheduling.ParseSchedule(*schedule.CronExpression, schedule.Timezone); err != nil {
				return nil, "", false, nil, domain.ErrInvalidCronExpression
			}
			cronExpr = schedule.CronExpression
		}
		nextRunAt = nil
	}

	return cronExpr, timezone, isEnabled, nextRunAt, nil
}
