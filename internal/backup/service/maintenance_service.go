package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"backup-platform/internal/backup/domain"
	"backup-platform/internal/backup/repository"
	"backup-platform/internal/organization/authz"
	orgDomain "backup-platform/internal/organization/domain"
	"backup-platform/pkg/uuid"
)

// MaintenanceJobReader specifies the service interface for querying maintenance jobs and runs.
type MaintenanceJobReader interface {
	ListMaintenanceJobs(
		ctx context.Context,
		role orgDomain.Role,
		orgID uuid.UUID,
		filter domain.MaintenanceJobFilter,
	) (*domain.MaintenanceJobListResult, error)

	GetMaintenanceJobDetail(
		ctx context.Context,
		role orgDomain.Role,
		orgID, jobID uuid.UUID,
	) (*domain.MaintenanceJobDetail, error)
}

// RepositoryFinder defines the repository lookup needed to validate repository tenant ownership.
type RepositoryFinder interface {
	GetRepositoryByID(ctx context.Context, orgID, repoID uuid.UUID) (*domain.BackupRepository, error)
}

// MaintenanceService handles business logic and security policies for repository maintenance read-only operations.
type MaintenanceService struct {
	maintRepo repository.MaintenanceRepository
	repoRepo  RepositoryFinder
	logger    *slog.Logger
}

// NewMaintenanceService constructs a new MaintenanceService.
func NewMaintenanceService(
	maintRepo repository.MaintenanceRepository,
	repoRepo RepositoryFinder,
	logger *slog.Logger,
) *MaintenanceService {
	if logger == nil {
		logger = slog.Default()
	}
	return &MaintenanceService{
		maintRepo: maintRepo,
		repoRepo:  repoRepo,
		logger:    logger,
	}
}

// ListMaintenanceJobs retrieves a paginated and filtered list of maintenance jobs for the given organization.
func (s *MaintenanceService) ListMaintenanceJobs(
	ctx context.Context,
	role orgDomain.Role,
	orgID uuid.UUID,
	filter domain.MaintenanceJobFilter,
) (*domain.MaintenanceJobListResult, error) {
	// Defense-in-depth: enforce service-level RBAC
	if !authz.HasPermission(role, authz.PermissionMaintenanceRead) {
		return nil, domain.ErrUnauthorizedRole
	}

	if orgID == uuid.Nil {
		return nil, domain.ErrInvalidRunFilter
	}

	// If filtering by a specific repository, verify it belongs to this tenant
	if filter.RepositoryID != nil && *filter.RepositoryID != uuid.Nil {
		if s.repoRepo != nil {
			repo, err := s.repoRepo.GetRepositoryByID(ctx, orgID, *filter.RepositoryID)
			if err != nil {
				if errors.Is(err, domain.ErrRepositoryNotFound) {
					// Anti-enumeration: return empty list if repository not found or belongs to another tenant
					return &domain.MaintenanceJobListResult{
						Jobs:       []*domain.MaintenanceJob{},
						NextCursor: nil,
						HasMore:    false,
					}, nil
				}
				s.logger.Error("repository lookup failed for maintenance filter",
					slog.String("org_id", orgID.String()),
					slog.String("repository_id", filter.RepositoryID.String()),
					slog.String("error", err.Error()),
				)
				return nil, fmt.Errorf("%w: repository lookup failed", domain.ErrBackupServiceUnavailable)
			}
			if repo == nil {
				// Anti-enumeration: return empty list if repository not found or belongs to another tenant
				return &domain.MaintenanceJobListResult{
					Jobs:       []*domain.MaintenanceJob{},
					NextCursor: nil,
					HasMore:    false,
				}, nil
			}
		}
	}

	jobs, hasMore, err := s.maintRepo.ListMaintenanceJobsPaginated(ctx, orgID, filter)
	if err != nil {
		s.logger.Error("failed listing paginated maintenance jobs",
			slog.String("org_id", orgID.String()),
			slog.String("error", err.Error()),
		)
		return nil, fmt.Errorf("%w: failed listing maintenance jobs", domain.ErrBackupServiceUnavailable)
	}

	var nextCursor *string
	if hasMore && len(jobs) > 0 {
		lastJob := jobs[len(jobs)-1]
		c := domain.EncodeMaintenanceCursor(lastJob.CreatedAt, lastJob.ID)
		nextCursor = &c
	}

	return &domain.MaintenanceJobListResult{
		Jobs:       jobs,
		NextCursor: nextCursor,
		HasMore:    hasMore,
	}, nil
}

// GetMaintenanceJobDetail retrieves a maintenance job along with its execution run history for the given organization.
func (s *MaintenanceService) GetMaintenanceJobDetail(
	ctx context.Context,
	role orgDomain.Role,
	orgID, jobID uuid.UUID,
) (*domain.MaintenanceJobDetail, error) {
	// Defense-in-depth: enforce service-level RBAC
	if !authz.HasPermission(role, authz.PermissionMaintenanceRead) {
		return nil, domain.ErrUnauthorizedRole
	}

	if orgID == uuid.Nil || jobID == uuid.Nil {
		return nil, domain.ErrJobNotFound
	}

	job, err := s.maintRepo.GetMaintenanceJobByID(ctx, orgID, jobID)
	if err != nil {
		if errors.Is(err, domain.ErrJobNotFound) {
			return nil, domain.ErrJobNotFound
		}
		s.logger.Error("failed getting maintenance job by id",
			slog.String("org_id", orgID.String()),
			slog.String("job_id", jobID.String()),
			slog.String("error", err.Error()),
		)
		return nil, fmt.Errorf("%w: failed retrieving maintenance job", domain.ErrBackupServiceUnavailable)
	}

	runs, err := s.maintRepo.ListMaintenanceRuns(ctx, orgID, jobID)
	if err != nil {
		s.logger.Error("failed listing maintenance runs for job",
			slog.String("org_id", orgID.String()),
			slog.String("job_id", jobID.String()),
			slog.String("error", err.Error()),
		)
		return nil, fmt.Errorf("%w: failed retrieving maintenance runs", domain.ErrBackupServiceUnavailable)
	}

	return &domain.MaintenanceJobDetail{
		Job:  job,
		Runs: runs,
	}, nil
}
