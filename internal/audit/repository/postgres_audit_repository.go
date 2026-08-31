package repository

import (
	"context"
	"fmt"

	"backup-platform/internal/audit/domain"
	"backup-platform/internal/platform/database"
	"backup-platform/pkg/uuid"
)

// PostgresAuditRepository implements AuditRepository using database.TxManager.
type PostgresAuditRepository struct {
	txManager database.TxManager
}

// NewPostgresAuditRepository constructs a new PostgresAuditRepository.
func NewPostgresAuditRepository(txManager database.TxManager) *PostgresAuditRepository {
	return &PostgresAuditRepository{txManager: txManager}
}

// Insert persists an audit log record into PostgreSQL.
func (r *PostgresAuditRepository) Insert(ctx context.Context, entry *domain.AuditLog) error {
	if entry == nil {
		return fmt.Errorf("audit log entry cannot be nil")
	}

	if entry.ID == uuid.Nil {
		entry.ID = uuid.New()
	}

	meta := entry.Metadata
	if len(meta) == 0 {
		meta = []byte("{}")
	}

	q := r.txManager.Querier()
	query := `
		INSERT INTO audit_logs (
			id, organization_id, user_id, action, entity_type, entity_id, ip_address, user_agent, metadata, created_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, NOW()
		);
	`

	_, err := q.Exec(
		ctx,
		query,
		entry.ID,
		entry.OrganizationID,
		entry.UserID,
		entry.Action,
		entry.EntityType,
		entry.EntityID,
		entry.IPAddress,
		entry.UserAgent,
		meta,
	)
	if err != nil {
		return fmt.Errorf("failed inserting audit log: %w", err)
	}

	return nil
}
