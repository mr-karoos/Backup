package service

import (
	"context"
	"errors"

	"backup-platform/internal/backup/domain"
	"backup-platform/internal/backup/repository"
	orgDomain "backup-platform/internal/organization/domain"
	resDomain "backup-platform/internal/resource/domain"
	"backup-platform/pkg/uuid"
)

// ResourceFinder allows looking up a resource by organization ID and resource ID.
type ResourceFinder interface {
	GetByID(ctx context.Context, orgID, resourceID uuid.UUID) (*resDomain.Resource, error)
}

// CreateManualJobInput encapsulates the parameters for creating a manual backup job.
type CreateManualJobInput struct {
	BackupPlanID *uuid.UUID         `json:"backup_plan_id,omitempty"`
	ResourceID   *uuid.UUID         `json:"resource_id,omitempty"`
	BackupType   domain.BackupType  `json:"backup_type,omitempty"`
	TargetSpec   *domain.TargetSpec `json:"target_spec,omitempty"`
}

// BackupJobService coordinates the business logic for creating and managing backup jobs.
type BackupJobService struct {
	repo           repository.BackupRepository
	resourceFinder ResourceFinder
}

// NewBackupJobService constructs a new BackupJobService.
func NewBackupJobService(repo repository.BackupRepository, resourceFinder ResourceFinder) *BackupJobService {
	return &BackupJobService{
		repo:           repo,
		resourceFinder: resourceFinder,
	}
}

// CreateManualJob validates authorization, tenant isolation, target resource/plan constraints,
// checks for active manual conflicts, and enqueues a new pending BackupJob in PostgreSQL.
func (s *BackupJobService) CreateManualJob(
	ctx context.Context,
	userRole orgDomain.Role,
	orgID, userID uuid.UUID,
	input CreateManualJobInput,
) (*domain.BackupJob, error) {
	// Service-level role defense
	if userRole != orgDomain.RoleAdmin && userRole != orgDomain.RoleMember {
		return nil, domain.ErrUnauthorizedRole
	}

	var targetResourceID uuid.UUID
	var targetBackupType domain.BackupType
	var targetSpec domain.TargetSpec

	if input.BackupPlanID != nil {
		// --- Case 1: Plan-Triggered Backup Job ---
		// Disallow mixing plan ID with ad-hoc fields
		if input.ResourceID != nil || input.BackupType != "" || input.TargetSpec != nil {
			return nil, domain.ErrInvalidTargetSpec
		}

		plan, err := s.repo.GetPlanByID(ctx, orgID, *input.BackupPlanID)
		if err != nil {
			if errors.Is(err, domain.ErrPlanNotFound) {
				return nil, domain.ErrPlanNotFound
			}
			return nil, domain.ErrBackupServiceUnavailable
		}
		if plan.Status != domain.PlanStatusActive {
			return nil, domain.ErrPlanNotActive
		}
		if plan.BackupType != domain.BackupTypeMySQLDatabase && plan.BackupType != domain.BackupTypeWebsiteFiles {
			return nil, domain.ErrUnsupportedBackupType
		}

		// Validate and normalize stored plan TargetSpec
		normalizedSpec, err := domain.NormalizeTargetSpec(plan.BackupType, &plan.TargetSpec)
		if err != nil {
			return nil, domain.ErrBackupServiceUnavailable
		}

		targetResourceID = plan.ResourceID
		targetBackupType = plan.BackupType
		targetSpec = *normalizedSpec
	} else {
		// --- Case 2: Ad-Hoc Manual Backup Job ---
		// RBAC Rule: Ad-hoc manual backups require Admin role (Members can only trigger Plans)
		if userRole != orgDomain.RoleAdmin {
			return nil, domain.ErrUnauthorizedRole
		}

		if input.ResourceID == nil || *input.ResourceID == uuid.Nil {
			return nil, domain.ErrInvalidTargetSpec
		}
		if input.BackupType != domain.BackupTypeMySQLDatabase && input.BackupType != domain.BackupTypeWebsiteFiles {
			return nil, domain.ErrUnsupportedBackupType
		}
		normalizedSpec, err := domain.NormalizeTargetSpec(input.BackupType, input.TargetSpec)
		if err != nil {
			return nil, domain.ErrInvalidTargetSpec
		}
		if input.BackupType == domain.BackupTypeMySQLDatabase && len(normalizedSpec.Databases) == 0 {
			return nil, domain.ErrInvalidTargetSpec
		}

		targetResourceID = *input.ResourceID
		targetBackupType = input.BackupType
		targetSpec = *normalizedSpec
	}

	// 1. Verify target resource exists in organization and validate status
	resource, err := s.resourceFinder.GetByID(ctx, orgID, targetResourceID)
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
		// cPanel or other unsupported connector types in V1
		return nil, domain.ErrUnsupportedResourceType
	}

	// 2. Preflight Active Conflict Check (UX Defense: conflict if active manual OR any running job)
	activeConflict, err := s.repo.GetActiveJobConflictForResource(ctx, orgID, targetResourceID)
	if err != nil {
		return nil, domain.ErrBackupServiceUnavailable
	}
	if activeConflict != nil {
		return nil, domain.ErrManualBackupConflict
	}

	// 3. Ensure and validate Default Storage Target
	storageTarget, err := s.repo.EnsureDefaultLocalStorageTarget(ctx, orgID)
	if err != nil {
		return nil, domain.ErrBackupServiceUnavailable
	}
	if storageTarget.Type != domain.StorageTargetTypeLocal ||
		storageTarget.Status != domain.StorageTargetStatusActive ||
		!storageTarget.IsDefault {
		return nil, domain.ErrBackupServiceUnavailable
	}

	// 4. Construct and insert pending BackupJob
	job := &domain.BackupJob{
		ID:              uuid.New(),
		OrganizationID:  orgID,
		ResourceID:      targetResourceID,
		BackupPlanID:    input.BackupPlanID,
		TriggerType:     domain.TriggerTypeManual,
		CreatedByUserID: &userID,
		BackupType:      targetBackupType,
		TargetSpec:      targetSpec,
		Status:          domain.JobStatusPending,
	}

	createdJob, err := s.repo.CreateJob(ctx, job)
	if err != nil {
		if errors.Is(err, domain.ErrManualBackupConflict) {
			return nil, domain.ErrManualBackupConflict
		}
		return nil, domain.ErrBackupServiceUnavailable
	}

	return createdJob, nil
}
