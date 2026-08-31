package repository

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"time"

	"backup-platform/internal/platform/database"
	"backup-platform/internal/resource/domain"
	"backup-platform/pkg/uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// ResourceRepository defines data access operations for resources and their connectors.
type ResourceRepository interface {
	CreateResource(ctx context.Context, q database.Querier, res *domain.Resource) error
	CreateConnector(ctx context.Context, q database.Querier, conn *domain.ResourceConnector) error
	UpdateResource(ctx context.Context, q database.Querier, res *domain.Resource) error
	UpdateConnector(ctx context.Context, q database.Querier, conn *domain.ResourceConnector) error
	FindByIDForOrganization(ctx context.Context, q database.Querier, orgID, resID uuid.UUID) (*domain.ResourceWithConnector, error)
	ListForOrganization(ctx context.Context, q database.Querier, orgID uuid.UUID) ([]*domain.ResourceWithConnector, error)
	ArchiveForOrganization(ctx context.Context, q database.Querier, orgID, resID uuid.UUID) error
	UpdateConnectionTestStateForOrganization(
		ctx context.Context,
		q database.Querier,
		orgID, resID uuid.UUID,
		lastTestAt time.Time,
		lastStatus domain.ConnectionStatus,
		lastError *string,
		newResourceStatus domain.Status,
	) error
}

// PostgresResourceRepository implements ResourceRepository using PostgreSQL.
type PostgresResourceRepository struct{}

// NewPostgresResourceRepository constructs a new PostgresResourceRepository.
func NewPostgresResourceRepository() *PostgresResourceRepository {
	return &PostgresResourceRepository{}
}

// decodeConnectorConfig strictly decodes and verifies stored connector configuration.
// It disallows unknown fields, trailing tokens, and delegates to domain.ValidateConnectorConfig
// to ensure identical write/read validation contracts (including Unicode rune count safety).
func decodeConnectorConfig(configRaw []byte, resType domain.Type) (domain.ConnectorConfig, error) {
	if len(configRaw) == 0 {
		return domain.ConnectorConfig{}, domain.ErrCorruptResourceData
	}

	dec := json.NewDecoder(bytes.NewReader(configRaw))
	dec.DisallowUnknownFields()

	var cfg domain.ConnectorConfig
	if err := dec.Decode(&cfg); err != nil {
		return domain.ConnectorConfig{}, domain.ErrCorruptResourceData
	}

	// Strict EOF check on second decode to reject trailing JSON objects, values, or garbage
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return domain.ConnectorConfig{}, domain.ErrCorruptResourceData
	}

	// Re-use domain validation for unified write/read contract and Unicode rune count integrity
	validatedConfig, err := domain.ValidateConnectorConfig(resType, cfg.Username, cfg.ConnectionTimeoutSeconds, cfg.UseHTTPS)
	if err != nil {
		return domain.ConnectorConfig{}, domain.ErrCorruptResourceData
	}

	return *validatedConfig, nil
}

// validateAndNormalizeLoadedResource validates and normalizes loaded database entities into a canonical domain model.
// Any stored integrity violation returns domain.ErrCorruptResourceData.
func validateAndNormalizeLoadedResource(
	res *domain.Resource,
	conn *domain.ResourceConnector,
	configRaw []byte,
	credName string,
	credFingerprint *string,
) (*domain.ResourceWithConnector, error) {
	// 1. Validate Resource Type & Status
	if err := domain.ValidateResourceType(res.Type); err != nil {
		return nil, domain.ErrCorruptResourceData
	}
	if err := domain.ValidateResourceStatus(res.Status); err != nil {
		return nil, domain.ErrCorruptResourceData
	}

	// 2. Validate Connector Type vs Resource Type
	if err := domain.ValidateConnectorType(res.Type, conn.ConnectorType); err != nil {
		return nil, domain.ErrCorruptResourceData
	}

	// 3. Validate Auth Type compatibility
	if err := domain.ValidateAuthType(conn.AuthType, res.Type); err != nil {
		return nil, domain.ErrCorruptResourceData
	}

	// 4. Validate & Normalize Host (canonical unbracketed IPv6 and lowercase DNS hostname)
	canonHost, err := domain.ValidateConnectorHost(conn.Host)
	if err != nil {
		return nil, domain.ErrCorruptResourceData
	}
	conn.Host = canonHost

	// 5. Validate Port
	if err := domain.ValidateConnectorPort(conn.Port); err != nil {
		return nil, domain.ErrCorruptResourceData
	}

	// 6. Validate & Normalize Host Key Fingerprint
	canonFP, err := domain.ValidateHostKeyFingerprint(conn.HostKeyFingerprint, res.Type)
	if err != nil {
		return nil, domain.ErrCorruptResourceData
	}
	conn.HostKeyFingerprint = canonFP

	// 7. Strictly Decode & Validate Connector Config
	cfg, err := decodeConnectorConfig(configRaw, res.Type)
	if err != nil {
		return nil, domain.ErrCorruptResourceData
	}
	conn.Config = cfg

	// 8. Identity & Tenant Consistency Check
	if conn.OrganizationID != res.OrganizationID || conn.ResourceID != res.ID {
		return nil, domain.ErrCorruptResourceData
	}

	return &domain.ResourceWithConnector{
		Resource:              res,
		Connector:             conn,
		CredentialName:        credName,
		CredentialFingerprint: credFingerprint,
	}, nil
}

// CreateResource inserts a new resource row within the provided querier/transaction.
func (r *PostgresResourceRepository) CreateResource(ctx context.Context, q database.Querier, res *domain.Resource) error {
	const query = `
		INSERT INTO resources (
			id, organization_id, name, type, status,
			last_connection_test_at, last_connection_status, last_connection_error,
			metadata, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11
		)
	`

	metadataJSON := res.Metadata
	if len(metadataJSON) == 0 {
		metadataJSON = []byte("{}")
	}

	var connStatusStr *string
	if res.LastConnectionStatus != nil {
		s := string(*res.LastConnectionStatus)
		connStatusStr = &s
	}

	_, err := q.Exec(
		ctx,
		query,
		res.ID,
		res.OrganizationID,
		res.Name,
		string(res.Type),
		string(res.Status),
		res.LastConnectionTestAt,
		connStatusStr,
		res.LastConnectionError,
		metadataJSON,
		res.CreatedAt,
		res.UpdatedAt,
	)
	if err != nil {
		return err
	}

	return nil
}

// CreateConnector inserts a new resource_connector row within the provided querier/transaction.
func (r *PostgresResourceRepository) CreateConnector(ctx context.Context, q database.Querier, conn *domain.ResourceConnector) error {
	const query = `
		INSERT INTO resource_connectors (
			id, organization_id, resource_id, connector_type, credential_id,
			host, port, auth_type, host_key_fingerprint, config,
			created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12
		)
	`

	configJSON, err := json.Marshal(conn.Config)
	if err != nil {
		return err
	}

	_, err = q.Exec(
		ctx,
		query,
		conn.ID,
		conn.OrganizationID,
		conn.ResourceID,
		string(conn.ConnectorType),
		conn.CredentialID,
		conn.Host,
		conn.Port,
		string(conn.AuthType),
		conn.HostKeyFingerprint,
		configJSON,
		conn.CreatedAt,
		conn.UpdatedAt,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			if pgErr.Code == "23503" && pgErr.ConstraintName == "fk_resource_connectors_org_credential" {
				return domain.ErrInvalidCredentialReference
			}
			if pgErr.Code == "23505" && pgErr.ConstraintName == "uq_resource_connectors_resource_id" {
				return domain.ErrResourceConflict
			}
		}
		return err
	}

	return nil
}

// UpdateResource updates editable resource fields (name, updated_at).
func (r *PostgresResourceRepository) UpdateResource(ctx context.Context, q database.Querier, res *domain.Resource) error {
	const query = `
		UPDATE resources
		SET name = $1, updated_at = $2
		WHERE id = $3 AND organization_id = $4 AND status <> 'archived'
	`

	tag, err := q.Exec(ctx, query, res.Name, res.UpdatedAt, res.ID, res.OrganizationID)
	if err != nil {
		return err
	}

	if tag.RowsAffected() == 0 {
		return domain.ErrResourceNotFound
	}

	return nil
}

// UpdateConnector updates connector network configuration and credential assignment.
func (r *PostgresResourceRepository) UpdateConnector(ctx context.Context, q database.Querier, conn *domain.ResourceConnector) error {
	const query = `
		UPDATE resource_connectors
		SET credential_id = $1, host = $2, port = $3, auth_type = $4,
		    host_key_fingerprint = $5, config = $6, updated_at = $7
		WHERE resource_id = $8 AND organization_id = $9
	`

	configJSON, err := json.Marshal(conn.Config)
	if err != nil {
		return err
	}

	tag, err := q.Exec(
		ctx,
		query,
		conn.CredentialID,
		conn.Host,
		conn.Port,
		string(conn.AuthType),
		conn.HostKeyFingerprint,
		configJSON,
		conn.UpdatedAt,
		conn.ResourceID,
		conn.OrganizationID,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" && pgErr.ConstraintName == "fk_resource_connectors_org_credential" {
			return domain.ErrInvalidCredentialReference
		}
		return err
	}

	if tag.RowsAffected() == 0 {
		return domain.ErrResourceNotFound
	}

	return nil
}

// FindByIDForOrganization retrieves a resource and its connector, strictly scoped to the tenant organization.
// Non-archived resources only.
func (r *PostgresResourceRepository) FindByIDForOrganization(ctx context.Context, q database.Querier, orgID, resID uuid.UUID) (*domain.ResourceWithConnector, error) {
	const query = `
		SELECT
			r.id, r.organization_id, r.name, r.type, r.status,
			r.last_connection_test_at, r.last_connection_status, r.last_connection_error,
			r.metadata, r.created_at, r.updated_at,
			rc.id, rc.organization_id, rc.resource_id, rc.connector_type, rc.credential_id,
			rc.host, rc.port, rc.auth_type, rc.host_key_fingerprint, rc.config,
			rc.created_at, rc.updated_at,
			c.name AS credential_name,
			c.fingerprint AS credential_fingerprint
		FROM resources r
		JOIN resource_connectors rc ON rc.resource_id = r.id AND rc.organization_id = r.organization_id
		JOIN credentials c ON c.id = rc.credential_id AND c.organization_id = r.organization_id
		WHERE r.id = $1 AND r.organization_id = $2 AND r.status <> 'archived'
	`

	row := q.QueryRow(ctx, query, resID, orgID)

	var (
		res             domain.Resource
		conn            domain.ResourceConnector
		resTypeStr      string
		resStatusStr    string
		connStatusStr   *string
		connTypeStr     string
		authTypeStr     string
		configRaw       []byte
		credName        string
		credFingerprint *string
	)

	err := row.Scan(
		&res.ID,
		&res.OrganizationID,
		&res.Name,
		&resTypeStr,
		&resStatusStr,
		&res.LastConnectionTestAt,
		&connStatusStr,
		&res.LastConnectionError,
		&res.Metadata,
		&res.CreatedAt,
		&res.UpdatedAt,
		&conn.ID,
		&conn.OrganizationID,
		&conn.ResourceID,
		&connTypeStr,
		&conn.CredentialID,
		&conn.Host,
		&conn.Port,
		&authTypeStr,
		&conn.HostKeyFingerprint,
		&configRaw,
		&conn.CreatedAt,
		&conn.UpdatedAt,
		&credName,
		&credFingerprint,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrResourceNotFound
		}
		return nil, err
	}

	res.Type = domain.Type(resTypeStr)
	res.Status = domain.Status(resStatusStr)
	if connStatusStr != nil {
		cs := domain.ConnectionStatus(*connStatusStr)
		res.LastConnectionStatus = &cs
	}

	conn.ConnectorType = domain.ConnectorType(connTypeStr)
	conn.AuthType = domain.AuthType(authTypeStr)

	return validateAndNormalizeLoadedResource(&res, &conn, configRaw, credName, credFingerprint)
}

// ListForOrganization retrieves all active/non-archived resources and their connectors for an organization.
func (r *PostgresResourceRepository) ListForOrganization(ctx context.Context, q database.Querier, orgID uuid.UUID) ([]*domain.ResourceWithConnector, error) {
	const query = `
		SELECT
			r.id, r.organization_id, r.name, r.type, r.status,
			r.last_connection_test_at, r.last_connection_status, r.last_connection_error,
			r.metadata, r.created_at, r.updated_at,
			rc.id, rc.organization_id, rc.resource_id, rc.connector_type, rc.credential_id,
			rc.host, rc.port, rc.auth_type, rc.host_key_fingerprint, rc.config,
			rc.created_at, rc.updated_at,
			c.name AS credential_name,
			c.fingerprint AS credential_fingerprint
		FROM resources r
		JOIN resource_connectors rc ON rc.resource_id = r.id AND rc.organization_id = r.organization_id
		JOIN credentials c ON c.id = rc.credential_id AND c.organization_id = r.organization_id
		WHERE r.organization_id = $1 AND r.status <> 'archived'
		ORDER BY r.created_at DESC, r.id DESC
	`

	rows, err := q.Query(ctx, query, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]*domain.ResourceWithConnector, 0)
	for rows.Next() {
		var (
			res             domain.Resource
			conn            domain.ResourceConnector
			resTypeStr      string
			resStatusStr    string
			connStatusStr   *string
			connTypeStr     string
			authTypeStr     string
			configRaw       []byte
			credName        string
			credFingerprint *string
		)

		err := rows.Scan(
			&res.ID,
			&res.OrganizationID,
			&res.Name,
			&resTypeStr,
			&resStatusStr,
			&res.LastConnectionTestAt,
			&connStatusStr,
			&res.LastConnectionError,
			&res.Metadata,
			&res.CreatedAt,
			&res.UpdatedAt,
			&conn.ID,
			&conn.OrganizationID,
			&conn.ResourceID,
			&connTypeStr,
			&conn.CredentialID,
			&conn.Host,
			&conn.Port,
			&authTypeStr,
			&conn.HostKeyFingerprint,
			&configRaw,
			&conn.CreatedAt,
			&conn.UpdatedAt,
			&credName,
			&credFingerprint,
		)
		if err != nil {
			return nil, err
		}

		res.Type = domain.Type(resTypeStr)
		res.Status = domain.Status(resStatusStr)
		if connStatusStr != nil {
			cs := domain.ConnectionStatus(*connStatusStr)
			res.LastConnectionStatus = &cs
		}

		conn.ConnectorType = domain.ConnectorType(connTypeStr)
		conn.AuthType = domain.AuthType(authTypeStr)

		item, err := validateAndNormalizeLoadedResource(&res, &conn, configRaw, credName, credFingerprint)
		if err != nil {
			return nil, err
		}

		items = append(items, item)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return items, nil
}

// ArchiveForOrganization soft-deletes a resource by setting status = 'archived'.
// If the resource was already archived in this organization, it returns nil (idempotent archive).
// If the resource does not exist in this organization, returns domain.ErrResourceNotFound.
func (r *PostgresResourceRepository) ArchiveForOrganization(ctx context.Context, q database.Querier, orgID, resID uuid.UUID) error {
	const archiveQuery = `
		UPDATE resources
		SET status = 'archived', updated_at = NOW()
		WHERE id = $1 AND organization_id = $2 AND status <> 'archived'
	`

	tag, err := q.Exec(ctx, archiveQuery, resID, orgID)
	if err != nil {
		return err
	}

	if tag.RowsAffected() > 0 {
		return nil
	}

	// Check if already archived in this organization (idempotent soft delete)
	const checkArchivedQuery = `
		SELECT 1 FROM resources
		WHERE id = $1 AND organization_id = $2 AND status = 'archived'
	`
	var exists int
	err = q.QueryRow(ctx, checkArchivedQuery, resID, orgID).Scan(&exists)
	if err == nil {
		return nil // Already archived
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrResourceNotFound
	}

	return err
}

// UpdateConnectionTestStateForOrganization updates the operational connection testing columns
// (last_connection_test_at, last_connection_status, last_connection_error, status) for an active resource.
func (r *PostgresResourceRepository) UpdateConnectionTestStateForOrganization(
	ctx context.Context,
	q database.Querier,
	orgID, resID uuid.UUID,
	lastTestAt time.Time,
	lastStatus domain.ConnectionStatus,
	lastError *string,
	newResourceStatus domain.Status,
) error {
	const query = `
		UPDATE resources
		SET last_connection_test_at = $1,
		    last_connection_status = $2,
		    last_connection_error = $3,
		    status = $4,
		    updated_at = NOW()
		WHERE id = $5 AND organization_id = $6 AND status <> 'archived'
	`

	tag, err := q.Exec(
		ctx,
		query,
		lastTestAt,
		string(lastStatus),
		lastError,
		string(newResourceStatus),
		resID,
		orgID,
	)
	if err != nil {
		return err
	}

	if tag.RowsAffected() == 0 {
		return domain.ErrResourceNotFound
	}

	return nil
}
