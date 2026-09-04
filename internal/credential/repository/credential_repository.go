package repository

import (
	"context"
	"errors"

	"backup-platform/internal/credential/domain"
	"backup-platform/internal/platform/database"
	"backup-platform/pkg/uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// CredentialRepository defines PostgreSQL data access operations for credentials.
type CredentialRepository interface {
	Create(ctx context.Context, q database.Querier, cred *domain.Credential) error
	FindEncryptedByIDForOrganization(ctx context.Context, q database.Querier, orgID uuid.UUID, credID uuid.UUID) (*domain.Credential, error)
	FindMetadataForOrganization(ctx context.Context, q database.Querier, orgID uuid.UUID, credID uuid.UUID) (*domain.CredentialMetadata, error)
	ListMetadataForOrganization(ctx context.Context, q database.Querier, orgID uuid.UUID) ([]*domain.CredentialMetadata, error)
	UpdateNameForOrganization(ctx context.Context, q database.Querier, orgID, credID uuid.UUID, name string) (*domain.CredentialMetadata, error)
	UpdateEncryptedForOrganization(ctx context.Context, q database.Querier, cred *domain.Credential) (*domain.CredentialMetadata, error)
	DeleteForOrganization(ctx context.Context, q database.Querier, orgID, credID uuid.UUID) error
	DeleteSystemResticKeyForOrganization(ctx context.Context, q database.Querier, orgID, credID uuid.UUID) error
}

// PostgresCredentialRepository implements CredentialRepository using PostgreSQL.
type PostgresCredentialRepository struct{}

// NewPostgresCredentialRepository constructs a new PostgresCredentialRepository.
func NewPostgresCredentialRepository() *PostgresCredentialRepository {
	return &PostgresCredentialRepository{}
}

// Create persists a new encrypted credential entity.
func (r *PostgresCredentialRepository) Create(ctx context.Context, q database.Querier, cred *domain.Credential) error {
	managedBy := cred.ManagedBy
	if managedBy == "" {
		managedBy = domain.ManagedByUser
	}

	const query = `
		INSERT INTO credentials (
			id, organization_id, name, type, managed_by, encrypted_secret, nonce, auth_tag, key_version, fingerprint, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12
		)
	`

	_, err := q.Exec(
		ctx,
		query,
		cred.ID,
		cred.OrganizationID,
		cred.Name,
		string(cred.Type),
		string(managedBy),
		cred.EncryptedSecret,
		cred.Nonce,
		cred.AuthTag,
		cred.KeyVersion,
		cred.Fingerprint,
		cred.CreatedAt,
		cred.UpdatedAt,
	)
	if err != nil {
		return err
	}

	return nil
}

// FindEncryptedByIDForOrganization retrieves an encrypted credential guaranteeing tenant isolation.
// If the credential does not exist or belongs to another organization, returns domain.ErrCredentialNotFound.
func (r *PostgresCredentialRepository) FindEncryptedByIDForOrganization(ctx context.Context, q database.Querier, orgID uuid.UUID, credID uuid.UUID) (*domain.Credential, error) {
	const query = `
		SELECT id, organization_id, name, type, managed_by, encrypted_secret, nonce, auth_tag, key_version, fingerprint, created_at, updated_at
		FROM credentials
		WHERE id = $1 AND organization_id = $2
	`

	var c domain.Credential
	var typeStr string
	var managedByStr string

	err := q.QueryRow(ctx, query, credID, orgID).Scan(
		&c.ID,
		&c.OrganizationID,
		&c.Name,
		&typeStr,
		&managedByStr,
		&c.EncryptedSecret,
		&c.Nonce,
		&c.AuthTag,
		&c.KeyVersion,
		&c.Fingerprint,
		&c.CreatedAt,
		&c.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrCredentialNotFound
		}
		return nil, err
	}

	c.Type = domain.Type(typeStr)
	c.ManagedBy = domain.ManagedBy(managedByStr)
	return &c, nil
}

// FindMetadataForOrganization retrieves safe metadata for a specific credential in the organization.
func (r *PostgresCredentialRepository) FindMetadataForOrganization(ctx context.Context, q database.Querier, orgID uuid.UUID, credID uuid.UUID) (*domain.CredentialMetadata, error) {
	const query = `
		SELECT id, organization_id, name, type, managed_by, fingerprint, key_version, created_at, updated_at
		FROM credentials
		WHERE id = $1 AND organization_id = $2
	`

	var m domain.CredentialMetadata
	var typeStr string
	var managedByStr string

	err := q.QueryRow(ctx, query, credID, orgID).Scan(
		&m.ID,
		&m.OrganizationID,
		&m.Name,
		&typeStr,
		&managedByStr,
		&m.Fingerprint,
		&m.KeyVersion,
		&m.CreatedAt,
		&m.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrCredentialNotFound
		}
		return nil, err
	}

	m.Type = domain.Type(typeStr)
	m.ManagedBy = domain.ManagedBy(managedByStr)
	return &m, nil
}

// ListMetadataForOrganization retrieves all safe credential metadata for an organization.
// System credentials (managed_by = 'system') are strictly excluded from the query.
func (r *PostgresCredentialRepository) ListMetadataForOrganization(ctx context.Context, q database.Querier, orgID uuid.UUID) ([]*domain.CredentialMetadata, error) {
	const query = `
		SELECT id, organization_id, name, type, managed_by, fingerprint, key_version, created_at, updated_at
		FROM credentials
		WHERE organization_id = $1 AND managed_by = 'user'
		ORDER BY created_at DESC, id DESC
	`

	rows, err := q.Query(ctx, query, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []*domain.CredentialMetadata
	for rows.Next() {
		var m domain.CredentialMetadata
		var typeStr string
		var managedByStr string
		if err := rows.Scan(
			&m.ID,
			&m.OrganizationID,
			&m.Name,
			&typeStr,
			&managedByStr,
			&m.Fingerprint,
			&m.KeyVersion,
			&m.CreatedAt,
			&m.UpdatedAt,
		); err != nil {
			return nil, err
		}
		m.Type = domain.Type(typeStr)
		m.ManagedBy = domain.ManagedBy(managedByStr)
		result = append(result, &m)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	if result == nil {
		result = []*domain.CredentialMetadata{}
	}

	return result, nil
}

// UpdateNameForOrganization updates only the name of an existing credential in the organization.
// No cryptographic secrets, nonces, tags, or keys are modified or accessed.
func (r *PostgresCredentialRepository) UpdateNameForOrganization(ctx context.Context, q database.Querier, orgID, credID uuid.UUID, name string) (*domain.CredentialMetadata, error) {
	const query = `
		UPDATE credentials
		SET name = $3, updated_at = NOW()
		WHERE id = $1 AND organization_id = $2
		RETURNING id, organization_id, name, type, managed_by, fingerprint, key_version, created_at, updated_at
	`

	var m domain.CredentialMetadata
	var typeStr string
	var managedByStr string

	err := q.QueryRow(ctx, query, credID, orgID, name).Scan(
		&m.ID,
		&m.OrganizationID,
		&m.Name,
		&typeStr,
		&managedByStr,
		&m.Fingerprint,
		&m.KeyVersion,
		&m.CreatedAt,
		&m.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrCredentialNotFound
		}
		return nil, err
	}

	m.Type = domain.Type(typeStr)
	m.ManagedBy = domain.ManagedBy(managedByStr)
	return &m, nil
}

// UpdateEncryptedForOrganization updates encrypted secret material, name, and fingerprint atomically.
func (r *PostgresCredentialRepository) UpdateEncryptedForOrganization(ctx context.Context, q database.Querier, cred *domain.Credential) (*domain.CredentialMetadata, error) {
	const query = `
		UPDATE credentials
		SET name = $3,
		    encrypted_secret = $4,
		    nonce = $5,
		    auth_tag = $6,
		    key_version = $7,
		    fingerprint = $8,
		    updated_at = $9
		WHERE id = $1 AND organization_id = $2
		RETURNING id, organization_id, name, type, managed_by, fingerprint, key_version, created_at, updated_at
	`

	var m domain.CredentialMetadata
	var typeStr string
	var managedByStr string

	err := q.QueryRow(
		ctx,
		query,
		cred.ID,
		cred.OrganizationID,
		cred.Name,
		cred.EncryptedSecret,
		cred.Nonce,
		cred.AuthTag,
		cred.KeyVersion,
		cred.Fingerprint,
		cred.UpdatedAt,
	).Scan(
		&m.ID,
		&m.OrganizationID,
		&m.Name,
		&typeStr,
		&managedByStr,
		&m.Fingerprint,
		&m.KeyVersion,
		&m.CreatedAt,
		&m.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrCredentialNotFound
		}
		return nil, err
	}

	m.Type = domain.Type(typeStr)
	m.ManagedBy = domain.ManagedBy(managedByStr)
	return &m, nil
}

// DeleteForOrganization permanently deletes a credential belonging to the organization.
// If the credential is in use by a resource connector or restic repository, returns domain.ErrCredentialInUse.
// If the credential is system-managed, returns domain.ErrSystemCredentialRestricted.
func (r *PostgresCredentialRepository) DeleteForOrganization(ctx context.Context, q database.Querier, orgID, credID uuid.UUID) error {
	const query = `
		DELETE FROM credentials
		WHERE id = $1 AND organization_id = $2 AND managed_by = 'user'
	`

	cmdTag, err := q.Exec(ctx, query, credID, orgID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			if pgErr.ConstraintName == "fk_resource_connectors_org_credential" ||
				pgErr.ConstraintName == "fk_backup_repositories_credential" ||
				pgErr.ConstraintName == "fk_storage_targets_org_credential" {
				return domain.ErrCredentialInUse
			}
		}
		return err
	}

	if cmdTag.RowsAffected() == 0 {
		var managedBy string
		row := q.QueryRow(ctx, "SELECT managed_by FROM credentials WHERE id = $1 AND organization_id = $2", credID, orgID)
		if row != nil {
			chkErr := row.Scan(&managedBy)
			if chkErr == nil && managedBy == string(domain.ManagedBySystem) {
				return domain.ErrSystemCredentialRestricted
			}
		}
		return domain.ErrCredentialNotFound
	}

	return nil
}

// DeleteSystemResticKeyForOrganization strictly deletes an internal system restic key credential.
// Only deletes where type = 'restic_repository_key' AND managed_by = 'system' AND organization_id = orgID AND id = credID.
// If referenced by a backup_repository, FK constraint violation returns domain.ErrCredentialInUse.
func (r *PostgresCredentialRepository) DeleteSystemResticKeyForOrganization(ctx context.Context, q database.Querier, orgID, credID uuid.UUID) error {
	const query = `
		DELETE FROM credentials
		WHERE id = $1 AND organization_id = $2
		  AND type = 'restic_repository_key'
		  AND managed_by = 'system'
	`

	cmdTag, err := q.Exec(ctx, query, credID, orgID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			if pgErr.ConstraintName == "fk_backup_repositories_credential" {
				return domain.ErrCredentialInUse
			}
		}
		return err
	}

	if cmdTag.RowsAffected() == 0 {
		return domain.ErrCredentialNotFound
	}

	return nil
}
