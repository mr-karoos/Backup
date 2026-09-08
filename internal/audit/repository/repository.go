package repository

import (
	"context"

	"backup-platform/internal/audit/domain"
	"backup-platform/pkg/uuid"
)

// AuditRepository abstracts persistence for append-oriented audit logs.
type AuditRepository interface {
	Insert(ctx context.Context, entry *domain.AuditLog) error
	ListPaginated(ctx context.Context, orgID uuid.UUID, filter domain.AuditLogFilter) ([]*domain.AuditLog, bool, error)
}
