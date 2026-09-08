package repository

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

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

// ListPaginated retrieves a paginated list of audit logs for an organization using keyset pagination.
func (r *PostgresAuditRepository) ListPaginated(
	ctx context.Context,
	orgID uuid.UUID,
	filter domain.AuditLogFilter,
) ([]*domain.AuditLog, bool, error) {
	if orgID == uuid.Nil {
		return nil, false, fmt.Errorf("orgID is required")
	}

	limit := filter.Limit
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	q := r.txManager.Querier()

	var (
		queryBuilder strings.Builder
		args         []any
		argIdx       = 1
	)

	queryBuilder.WriteString(`
		SELECT id, organization_id, user_id, action, entity_type, entity_id, ip_address, user_agent, created_at
		FROM audit_logs
		WHERE organization_id = $` + strconv.Itoa(argIdx) + `
	`)
	args = append(args, orgID)
	argIdx++

	if filter.Action != nil && *filter.Action != "" {
		queryBuilder.WriteString(` AND action = $` + strconv.Itoa(argIdx))
		args = append(args, *filter.Action)
		argIdx++
	}

	if filter.EntityType != nil && *filter.EntityType != "" {
		queryBuilder.WriteString(` AND entity_type = $` + strconv.Itoa(argIdx))
		args = append(args, *filter.EntityType)
		argIdx++
	}

	if filter.EntityID != nil && *filter.EntityID != uuid.Nil {
		queryBuilder.WriteString(` AND entity_id = $` + strconv.Itoa(argIdx))
		args = append(args, *filter.EntityID)
		argIdx++
	}

	if filter.UserID != nil && *filter.UserID != uuid.Nil {
		queryBuilder.WriteString(` AND user_id = $` + strconv.Itoa(argIdx))
		args = append(args, *filter.UserID)
		argIdx++
	}

	if filter.From != nil {
		queryBuilder.WriteString(` AND created_at >= $` + strconv.Itoa(argIdx))
		args = append(args, *filter.From)
		argIdx++
	}

	if filter.To != nil {
		queryBuilder.WriteString(` AND created_at <= $` + strconv.Itoa(argIdx))
		args = append(args, *filter.To)
		argIdx++
	}

	if filter.CursorCreatedAt != nil && filter.CursorID != nil && *filter.CursorID != uuid.Nil {
		queryBuilder.WriteString(fmt.Sprintf(` AND (created_at < $%d OR (created_at = $%d AND id < $%d))`, argIdx, argIdx, argIdx+1))
		args = append(args, *filter.CursorCreatedAt, *filter.CursorID)
		argIdx += 2
	}

	queryBuilder.WriteString(fmt.Sprintf(` ORDER BY created_at DESC, id DESC LIMIT $%d;`, argIdx))
	args = append(args, limit+1)

	rows, err := q.Query(ctx, queryBuilder.String(), args...)
	if err != nil {
		return nil, false, fmt.Errorf("failed querying paginated audit logs: %w", err)
	}
	defer rows.Close()

	logs := make([]*domain.AuditLog, 0)
	for rows.Next() {
		log, err := scanAuditLogRow(rows)
		if err != nil {
			return nil, false, fmt.Errorf("failed scanning paginated audit log: %w", err)
		}
		logs = append(logs, log)
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("failed iterating audit logs: %w", err)
	}

	hasMore := false
	if len(logs) > limit {
		hasMore = true
		logs = logs[:limit]
	}

	return logs, hasMore, nil
}

func scanAuditLogRow(s interface{ Scan(dest ...any) error }) (*domain.AuditLog, error) {
	var (
		id             uuid.UUID
		organizationID *uuid.UUID
		userID         *uuid.UUID
		action         string
		entityType     string
		entityID       *uuid.UUID
		ipAddress      *string
		userAgent      *string
		createdAt      time.Time
	)

	err := s.Scan(
		&id,
		&organizationID,
		&userID,
		&action,
		&entityType,
		&entityID,
		&ipAddress,
		&userAgent,
		&createdAt,
	)
	if err != nil {
		return nil, err
	}

	return &domain.AuditLog{
		ID:             id,
		OrganizationID: organizationID,
		UserID:         userID,
		Action:         action,
		EntityType:     entityType,
		EntityID:       entityID,
		IPAddress:      ipAddress,
		UserAgent:      userAgent,
		Metadata:       nil,
		CreatedAt:      createdAt,
	}, nil
}
