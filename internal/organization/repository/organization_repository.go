package repository

import (
	"context"
	"errors"
	"strings"

	"backup-platform/internal/organization/domain"
	"backup-platform/internal/platform/database"
	"backup-platform/pkg/uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// OrganizationRepository defines data access operations for organizations.
type OrganizationRepository interface {
	Create(ctx context.Context, q database.Querier, org *domain.Organization) error
	FindByID(ctx context.Context, q database.Querier, id uuid.UUID) (*domain.Organization, error)
	FindActiveByID(ctx context.Context, q database.Querier, id uuid.UUID) (*domain.Organization, error)
	FindBySlug(ctx context.Context, q database.Querier, slug string) (*domain.Organization, error)
	FindDefaultInternal(ctx context.Context, q database.Querier) (*domain.Organization, error)
	UpdateActive(ctx context.Context, q database.Querier, id uuid.UUID, name string, metadata []byte) (*domain.Organization, error)
}

// PostgresOrganizationRepository implements OrganizationRepository using PostgreSQL.
type PostgresOrganizationRepository struct{}

// NewPostgresOrganizationRepository constructs a new PostgresOrganizationRepository.
func NewPostgresOrganizationRepository() *PostgresOrganizationRepository {
	return &PostgresOrganizationRepository{}
}

// Create inserts a new organization record.
func (r *PostgresOrganizationRepository) Create(ctx context.Context, q database.Querier, org *domain.Organization) error {
	const query = `
		INSERT INTO organizations (
			id, name, slug, is_default_internal, status, metadata, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8
		)
	`

	metadata := org.Metadata
	if len(metadata) == 0 {
		metadata = []byte("{}")
	}

	_, err := q.Exec(
		ctx,
		query,
		org.ID,
		org.Name,
		strings.ToLower(org.Slug),
		org.IsDefaultInternal,
		string(org.Status),
		metadata,
		org.CreatedAt,
		org.UpdatedAt,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "organizations_slug_key" {
			return domain.ErrDuplicateOrgSlug
		}
		return err
	}

	return nil
}

// FindByID retrieves an organization by its UUID primary key.
func (r *PostgresOrganizationRepository) FindByID(ctx context.Context, q database.Querier, id uuid.UUID) (*domain.Organization, error) {
	const query = `
		SELECT id, name, slug, is_default_internal, status, metadata, created_at, updated_at
		FROM organizations
		WHERE id = $1
	`

	var o domain.Organization
	var statusStr string

	err := q.QueryRow(ctx, query, id).Scan(
		&o.ID,
		&o.Name,
		&o.Slug,
		&o.IsDefaultInternal,
		&statusStr,
		&o.Metadata,
		&o.CreatedAt,
		&o.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrOrgNotFound
		}
		return nil, err
	}

	o.Status = domain.OrganizationStatus(statusStr)
	return &o, nil
}

// FindActiveByID retrieves an active organization by its UUID primary key.
func (r *PostgresOrganizationRepository) FindActiveByID(ctx context.Context, q database.Querier, id uuid.UUID) (*domain.Organization, error) {
	const query = `
		SELECT id, name, slug, is_default_internal, status, metadata, created_at, updated_at
		FROM organizations
		WHERE id = $1 AND status = 'active'
	`

	var o domain.Organization
	var statusStr string

	err := q.QueryRow(ctx, query, id).Scan(
		&o.ID,
		&o.Name,
		&o.Slug,
		&o.IsDefaultInternal,
		&statusStr,
		&o.Metadata,
		&o.CreatedAt,
		&o.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrOrgNotFound
		}
		return nil, err
	}

	o.Status = domain.OrganizationStatus(statusStr)
	return &o, nil
}

// FindBySlug retrieves an organization by its slug.
func (r *PostgresOrganizationRepository) FindBySlug(ctx context.Context, q database.Querier, slug string) (*domain.Organization, error) {
	const query = `
		SELECT id, name, slug, is_default_internal, status, metadata, created_at, updated_at
		FROM organizations
		WHERE slug = $1
	`

	var o domain.Organization
	var statusStr string

	err := q.QueryRow(ctx, query, strings.ToLower(strings.TrimSpace(slug))).Scan(
		&o.ID,
		&o.Name,
		&o.Slug,
		&o.IsDefaultInternal,
		&statusStr,
		&o.Metadata,
		&o.CreatedAt,
		&o.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrOrgNotFound
		}
		return nil, err
	}

	o.Status = domain.OrganizationStatus(statusStr)
	return &o, nil
}

// FindDefaultInternal retrieves the system's default internal organization.
func (r *PostgresOrganizationRepository) FindDefaultInternal(ctx context.Context, q database.Querier) (*domain.Organization, error) {
	const query = `
		SELECT id, name, slug, is_default_internal, status, metadata, created_at, updated_at
		FROM organizations
		WHERE is_default_internal = true
		LIMIT 1
	`

	var o domain.Organization
	var statusStr string

	err := q.QueryRow(ctx, query).Scan(
		&o.ID,
		&o.Name,
		&o.Slug,
		&o.IsDefaultInternal,
		&statusStr,
		&o.Metadata,
		&o.CreatedAt,
		&o.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrOrgNotFound
		}
		return nil, err
	}

	o.Status = domain.OrganizationStatus(statusStr)
	return &o, nil
}

// UpdateActive updates the name and metadata of an active organization.
func (r *PostgresOrganizationRepository) UpdateActive(ctx context.Context, q database.Querier, id uuid.UUID, name string, metadata []byte) (*domain.Organization, error) {
	const query = `
		UPDATE organizations
		SET
			name = $2,
			metadata = $3,
			updated_at = NOW()
		WHERE id = $1
		  AND status = 'active'
		RETURNING
			id,
			name,
			slug,
			is_default_internal,
			status,
			metadata,
			created_at,
			updated_at
	`

	if len(metadata) == 0 {
		metadata = []byte("{}")
	}

	var o domain.Organization
	var statusStr string

	err := q.QueryRow(ctx, query, id, name, metadata).Scan(
		&o.ID,
		&o.Name,
		&o.Slug,
		&o.IsDefaultInternal,
		&statusStr,
		&o.Metadata,
		&o.CreatedAt,
		&o.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrOrgNotFound
		}
		return nil, err
	}

	o.Status = domain.OrganizationStatus(statusStr)
	return &o, nil
}
