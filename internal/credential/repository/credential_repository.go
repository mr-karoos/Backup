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
}

// PostgresCredentialRepository implements CredentialRepository using PostgreSQL.
type PostgresCredentialRepository struct{}

// NewPostgresCredentialRepository constructs a new PostgresCredentialRepository.
func NewPostgresCredentialRepository() *PostgresCredentialRepository {
	return &PostgresCredentialRepository{}
}

// Create persists a new encrypted credential entity.
func (r *PostgresCredentialRepository) Create(ctx context.Context, q database.Querier, cred *domain.Credential) error {
	const query = `
		INSERT INTO credentials (
			id, organization_id, name, type, encrypted_secret, nonce, auth_tag, key_version, fingerprint, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11
		)
	`

	_, err := q.Exec(
		ctx,
		query,
		cred.ID,
		cred.OrganizationID,
		cred.Name,
		string(cred.Type),
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
		SELECT id, organization_id, name, type, encrypted_secret, nonce, auth_tag, key_version, fingerprint, created_at, updated_at
		FROM credentials
		WHERE id = $1 AND organization_id = $2
	`

	var c domain.Credential
	var typeStr string

	err := q.QueryRow(ctx, query, credID, orgID).Scan(
		&c.ID,
		&c.OrganizationID,
		&c.Name,
		&typeStr,
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
	return &c, nil
}

// FindMetadataForOrganization retrieves safe metadata for a specific credential in the organization.
func (r *PostgresCredentialRepository) FindMetadataForOrganization(ctx context.Context, q database.Querier, orgID uuid.UUID, credID uuid.UUID) (*domain.CredentialMetadata, error) {
	const query = `
		SELECT id, organization_id, name, type, fingerprint, key_version, created_at, updated_at
		FROM credentials
		WHERE id = $1 AND organization_id = $2
	`

	var m domain.CredentialMetadata
	var typeStr string

	err := q.QueryRow(ctx, query, credID, orgID).Scan(
		&m.ID,
		&m.OrganizationID,
		&m.Name,
		&typeStr,
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
	return &m, nil
}

// ListMetadataForOrganization retrieves all safe credential metadata for an organization.
// Sensitive ciphertext and auth tags (encrypted_secret, nonce, auth_tag) are strictly excluded from the query.
func (r *PostgresCredentialRepository) ListMetadataForOrganization(ctx context.Context, q database.Querier, orgID uuid.UUID) ([]*domain.CredentialMetadata, error) {
	const query = `
		SELECT id, organization_id, name, type, fingerprint, key_version, created_at, updated_at
		FROM credentials
		WHERE organization_id = $1
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
		if err := rows.Scan(
			&m.ID,
			&m.OrganizationID,
			&m.Name,
			&typeStr,
			&m.Fingerprint,
			&m.KeyVersion,
			&m.CreatedAt,
			&m.UpdatedAt,
		); err != nil {
			return nil, err
		}
		m.Type = domain.Type(typeStr)
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
		RETURNING id, organization_id, name, type, fingerprint, key_version, created_at, updated_at
	`

	var m domain.CredentialMetadata
	var typeStr string

	err := q.QueryRow(ctx, query, credID, orgID, name).Scan(
		&m.ID,
		&m.OrganizationID,
		&m.Name,
		&typeStr,
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
		RETURNING id, organization_id, name, type, fingerprint, key_version, created_at, updated_at
	`

	var m domain.CredentialMetadata
	var typeStr string

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
	return &m, nil
}

// DeleteForOrganization permanently deletes a credential belonging to the organization.
// If the credential is in use by a resource connector (foreign key restrict violation), returns domain.ErrCredentialInUse.
func (r *PostgresCredentialRepository) DeleteForOrganization(ctx context.Context, q database.Querier, orgID, credID uuid.UUID) error {
	const query = `
		DELETE FROM credentials
		WHERE id = $1 AND organization_id = $2
	`

	cmdTag, err := q.Exec(ctx, query, credID, orgID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" && pgErr.ConstraintName == "fk_resource_connectors_org_credential" {
			return domain.ErrCredentialInUse
		}
		return err
	}

	if cmdTag.RowsAffected() == 0 {
		return domain.ErrCredentialNotFound
	}

	return nil
}
