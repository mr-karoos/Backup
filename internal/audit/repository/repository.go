package repository

import (
	"context"

	"backup-platform/internal/audit/domain"
)

// AuditRepository abstracts persistence for append-oriented audit logs.
type AuditRepository interface {
	Insert(ctx context.Context, entry *domain.AuditLog) error
}
