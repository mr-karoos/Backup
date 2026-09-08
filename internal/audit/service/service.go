package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"backup-platform/internal/audit/domain"
	"backup-platform/internal/audit/repository"
	"backup-platform/internal/organization/authz"
	orgDomain "backup-platform/internal/organization/domain"
	"backup-platform/pkg/uuid"
)

// AuditRecorder provides high-level recording of system and operational audit logs.
type AuditRecorder interface {
	Record(ctx context.Context, entry *domain.AuditLog) error
}

// AuditLogReader specifies read operations for tenant audit logs.
type AuditLogReader interface {
	ListAuditLogs(ctx context.Context, role orgDomain.Role, orgID uuid.UUID, filter domain.AuditLogFilter) (*domain.AuditLogListResult, error)
}

// AuditService coordinates audit log persistence.
type AuditService struct {
	repo   repository.AuditRepository
	logger *slog.Logger
}

// NewAuditService constructs a new AuditService.
func NewAuditService(repo repository.AuditRepository, logger *slog.Logger) *AuditService {
	if logger == nil {
		logger = slog.Default()
	}
	return &AuditService{
		repo:   repo,
		logger: logger,
	}
}

// Record persists an audit event.
func (s *AuditService) Record(ctx context.Context, entry *domain.AuditLog) error {
	if entry == nil {
		return fmt.Errorf("audit entry cannot be nil")
	}
	if err := s.repo.Insert(ctx, entry); err != nil {
		s.logger.Error("failed recording audit log", slog.String("action", entry.Action), slog.String("entity_type", entry.EntityType))
		return err
	}
	return nil
}

// ListAuditLogs retrieves a paginated and filtered list of audit logs for the specified organization.
// It strictly enforces tenant boundaries and service-level RBAC (only roles with PermissionAuditLogRead are permitted).
func (s *AuditService) ListAuditLogs(
	ctx context.Context,
	role orgDomain.Role,
	orgID uuid.UUID,
	filter domain.AuditLogFilter,
) (*domain.AuditLogListResult, error) {
	// 1. Service-level RBAC enforcement (defense-in-depth)
	if !authz.HasPermission(role, authz.PermissionAuditLogRead) {
		return nil, domain.ErrUnauthorizedRole
	}

	// 2. Tenant validation
	if orgID == uuid.Nil {
		return nil, domain.ErrInvalidAuditFilter
	}

	// 3. Domain validation for filters
	if filter.Action != nil {
		trimmed := strings.TrimSpace(*filter.Action)
		if trimmed == "" || len(*filter.Action) > 100 {
			return nil, domain.ErrInvalidAuditFilter
		}
	}

	if filter.EntityType != nil {
		trimmed := strings.TrimSpace(*filter.EntityType)
		if trimmed == "" || len(*filter.EntityType) > 50 {
			return nil, domain.ErrInvalidAuditFilter
		}
	}

	if filter.EntityID != nil && *filter.EntityID == uuid.Nil {
		return nil, domain.ErrInvalidAuditFilter
	}

	if filter.UserID != nil && *filter.UserID == uuid.Nil {
		return nil, domain.ErrInvalidAuditFilter
	}

	if (filter.CursorCreatedAt != nil && filter.CursorID == nil) ||
		(filter.CursorCreatedAt == nil && filter.CursorID != nil) {
		return nil, domain.ErrInvalidAuditFilter
	}

	if filter.CursorID != nil && *filter.CursorID == uuid.Nil {
		return nil, domain.ErrInvalidAuditFilter
	}

	if filter.From != nil && filter.To != nil && filter.From.After(*filter.To) {
		return nil, domain.ErrInvalidAuditFilter
	}

	// 4. Default limit bounding
	if filter.Limit <= 0 || filter.Limit > 100 {
		filter.Limit = 50
	}

	// 5. Query repository
	logs, hasMore, err := s.repo.ListPaginated(ctx, orgID, filter)
	if err != nil {
		s.logger.Error("failed listing audit logs from repository", slog.String("error", err.Error()), slog.String("org_id", orgID.String()))
		return nil, domain.ErrAuditServiceUnavailable
	}

	// 6. Keyset cursor calculation
	var nextCursor *string
	if hasMore && len(logs) > 0 {
		lastLog := logs[len(logs)-1]
		encoded := domain.EncodeAuditCursor(lastLog.CreatedAt, lastLog.ID)
		nextCursor = &encoded
	}

	return &domain.AuditLogListResult{
		Logs:       logs,
		NextCursor: nextCursor,
		HasMore:    hasMore,
	}, nil
}
