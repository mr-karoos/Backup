package repository

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"backup-platform/internal/resource/domain"
	"backup-platform/pkg/uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type mockQuerier struct {
	execFunc     func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	queryRowFunc func(ctx context.Context, sql string, args ...any) pgx.Row
	queryFunc    func(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

func (m *mockQuerier) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if m.execFunc != nil {
		return m.execFunc(ctx, sql, args...)
	}
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}

func (m *mockQuerier) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if m.queryRowFunc != nil {
		return m.queryRowFunc(ctx, sql, args...)
	}
	return &mockRow{err: pgx.ErrNoRows}
}

func (m *mockQuerier) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if m.queryFunc != nil {
		return m.queryFunc(ctx, sql, args...)
	}
	return &mockRows{}, nil
}

type mockRow struct {
	scanFunc func(dest ...any) error
	err      error
}

func (r *mockRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if r.scanFunc != nil {
		return r.scanFunc(dest...)
	}
	return nil
}

type mockRows struct {
	closed bool
}

func (r *mockRows) Close()                                       { r.closed = true }
func (r *mockRows) Err() error                                   { return nil }
func (r *mockRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (r *mockRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (r *mockRows) Next() bool                                   { return false }
func (r *mockRows) Scan(dest ...any) error                       { return nil }
func (r *mockRows) Values() ([]any, error)                       { return nil, nil }
func (r *mockRows) RawValues() [][]byte                          { return nil }
func (r *mockRows) Conn() *pgx.Conn                              { return nil }

func TestPostgresResourceRepository_CreateResource(t *testing.T) {
	repo := NewPostgresResourceRepository()
	ctx := context.Background()
	orgID := uuid.New()
	resID := uuid.New()
	now := time.Now().UTC()

	res := &domain.Resource{
		ID:             resID,
		OrganizationID: orgID,
		Name:           "Ubuntu Web Server",
		Type:           domain.TypeUbuntuSSH,
		Status:         domain.StatusActive,
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	t.Run("successfully inserts resource", func(t *testing.T) {
		var capturedSQL string
		var capturedArgs []any
		mock := &mockQuerier{
			execFunc: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				capturedSQL = sql
				capturedArgs = args
				return pgconn.NewCommandTag("INSERT 0 1"), nil
			},
		}

		err := repo.CreateResource(ctx, mock, res)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(capturedSQL, "INSERT INTO resources") {
			t.Errorf("unexpected query: %s", capturedSQL)
		}
		if len(capturedArgs) != 11 {
			t.Errorf("expected 11 arguments, got %d", len(capturedArgs))
		}
	})

	t.Run("does NOT convert generic 23505 to ErrResourceConflict", func(t *testing.T) {
		mock := &mockQuerier{
			execFunc: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, &pgconn.PgError{Code: "23505", ConstraintName: "uq_resources_org_id_id"}
			},
		}

		err := repo.CreateResource(ctx, mock, res)
		if errors.Is(err, domain.ErrResourceConflict) {
			t.Errorf("SECURITY/DESIGN FLAW: CreateResource must not convert generic 23505 to ErrResourceConflict")
		}
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
			t.Errorf("expected raw pgconn.PgError, got: %v", err)
		}
	})
}

func TestPostgresResourceRepository_CreateConnector(t *testing.T) {
	repo := NewPostgresResourceRepository()
	ctx := context.Background()
	orgID := uuid.New()
	resID := uuid.New()
	connID := uuid.New()
	credID := uuid.New()
	now := time.Now().UTC()

	conn := &domain.ResourceConnector{
		ID:             connID,
		OrganizationID: orgID,
		ResourceID:     resID,
		ConnectorType:  domain.ConnectorTypeUbuntuSSH,
		CredentialID:   credID,
		Host:           "192.168.1.50",
		Port:           22,
		AuthType:       domain.AuthTypeSSHKey,
		Config: domain.ConnectorConfig{
			Username: "root",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}

	t.Run("successfully inserts connector", func(t *testing.T) {
		var capturedSQL string
		var capturedArgs []any
		mock := &mockQuerier{
			execFunc: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				capturedSQL = sql
				capturedArgs = args
				return pgconn.NewCommandTag("INSERT 0 1"), nil
			},
		}

		err := repo.CreateConnector(ctx, mock, conn)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(capturedSQL, "INSERT INTO resource_connectors") {
			t.Errorf("unexpected query: %s", capturedSQL)
		}
		if len(capturedArgs) != 12 {
			t.Errorf("expected 12 arguments, got %d", len(capturedArgs))
		}
	})

	t.Run("classifies foreign key violation on credential as ErrInvalidCredentialReference", func(t *testing.T) {
		mock := &mockQuerier{
			execFunc: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, &pgconn.PgError{Code: "23503", ConstraintName: "fk_resource_connectors_org_credential"}
			},
		}

		err := repo.CreateConnector(ctx, mock, conn)
		if !errors.Is(err, domain.ErrInvalidCredentialReference) {
			t.Errorf("expected ErrInvalidCredentialReference, got: %v", err)
		}
	})

	t.Run("does NOT classify other foreign key violations as ErrInvalidCredentialReference", func(t *testing.T) {
		mock := &mockQuerier{
			execFunc: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, &pgconn.PgError{Code: "23503", ConstraintName: "fk_resource_connectors_org_resource"}
			},
		}

		err := repo.CreateConnector(ctx, mock, conn)
		if errors.Is(err, domain.ErrInvalidCredentialReference) {
			t.Errorf("should not classify fk_resource_connectors_org_resource as ErrInvalidCredentialReference")
		}
	})

	t.Run("classifies 1:1 unique constraint violation on resource_id as ErrResourceConflict", func(t *testing.T) {
		mock := &mockQuerier{
			execFunc: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, &pgconn.PgError{Code: "23505", ConstraintName: "uq_resource_connectors_resource_id"}
			},
		}

		err := repo.CreateConnector(ctx, mock, conn)
		if !errors.Is(err, domain.ErrResourceConflict) {
			t.Errorf("expected ErrResourceConflict, got: %v", err)
		}
	})

	t.Run("does NOT classify other 23505 constraint violations as ErrResourceConflict", func(t *testing.T) {
		mock := &mockQuerier{
			execFunc: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, &pgconn.PgError{Code: "23505", ConstraintName: "some_other_unique_idx"}
			},
		}

		err := repo.CreateConnector(ctx, mock, conn)
		if errors.Is(err, domain.ErrResourceConflict) {
			t.Errorf("should not classify unexpected 23505 as ErrResourceConflict")
		}
	})
}

func TestPostgresResourceRepository_UpdateQueries_TenantScope(t *testing.T) {
	repo := NewPostgresResourceRepository()
	ctx := context.Background()
	orgID := uuid.New()
	resID := uuid.New()
	now := time.Now().UTC()

	t.Run("UpdateResource SQL includes id, organization_id, and status <> 'archived'", func(t *testing.T) {
		var capturedSQL string
		mock := &mockQuerier{
			execFunc: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				capturedSQL = sql
				return pgconn.NewCommandTag("UPDATE 1"), nil
			},
		}

		res := &domain.Resource{
			ID:             resID,
			OrganizationID: orgID,
			Name:           "Updated Name",
			UpdatedAt:      now,
		}

		err := repo.UpdateResource(ctx, mock, res)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !strings.Contains(capturedSQL, "id = $3") ||
			!strings.Contains(capturedSQL, "organization_id = $4") ||
			!strings.Contains(capturedSQL, "status <> 'archived'") {
			t.Errorf("SECURITY FLAW: UpdateResource missing tenant or archive scoping: %s", capturedSQL)
		}
	})

	t.Run("UpdateConnector SQL includes resource_id and organization_id", func(t *testing.T) {
		var capturedSQL string
		mock := &mockQuerier{
			execFunc: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				capturedSQL = sql
				return pgconn.NewCommandTag("UPDATE 1"), nil
			},
		}

		conn := &domain.ResourceConnector{
			OrganizationID: orgID,
			ResourceID:     resID,
			CredentialID:   uuid.New(),
			Host:           "10.0.0.1",
			Port:           22,
			AuthType:       domain.AuthTypeSSHKey,
			Config:         domain.ConnectorConfig{Username: "root"},
			UpdatedAt:      now,
		}

		err := repo.UpdateConnector(ctx, mock, conn)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !strings.Contains(capturedSQL, "resource_id = $8") ||
			!strings.Contains(capturedSQL, "organization_id = $9") {
			t.Errorf("SECURITY FLAW: UpdateConnector missing resource_id or organization_id in WHERE: %s", capturedSQL)
		}
	})
}

func TestPostgresResourceRepository_FindByIDForOrganization_NoSecretQuery(t *testing.T) {
	repo := NewPostgresResourceRepository()
	ctx := context.Background()
	orgID := uuid.New()
	resID := uuid.New()

	var capturedQuery string
	mock := &mockQuerier{
		queryRowFunc: func(ctx context.Context, sql string, args ...any) pgx.Row {
			capturedQuery = sql
			return &mockRow{
				scanFunc: func(dest ...any) error {
					*dest[0].(*uuid.UUID) = resID
					*dest[1].(*uuid.UUID) = orgID
					*dest[2].(*string) = "Test Resource"
					*dest[3].(*string) = "ubuntu_ssh"
					*dest[4].(*string) = "active"
					*dest[5].(**time.Time) = nil
					*dest[6].(**string) = nil
					*dest[7].(**string) = nil
					*dest[8].(*json.RawMessage) = []byte("{}")
					*dest[9].(*time.Time) = time.Now().UTC()
					*dest[10].(*time.Time) = time.Now().UTC()
					*dest[11].(*uuid.UUID) = uuid.New()
					*dest[12].(*uuid.UUID) = orgID
					*dest[13].(*uuid.UUID) = resID
					*dest[14].(*string) = "ubuntu_ssh"
					*dest[15].(*uuid.UUID) = uuid.New()
					*dest[16].(*string) = "192.168.1.10"
					*dest[17].(*int) = 22
					*dest[18].(*string) = "ssh_key"
					*dest[19].(**string) = nil
					*dest[20].(*[]byte) = []byte(`{"username":"root"}`)
					*dest[21].(*time.Time) = time.Now().UTC()
					*dest[22].(*time.Time) = time.Now().UTC()
					*dest[23].(*string) = "Test Credential"
					*dest[24].(**string) = nil
					return nil
				},
			}
		},
	}

	resWithConn, err := repo.FindByIDForOrganization(ctx, mock, orgID, resID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resWithConn.Resource.Name != "Test Resource" || resWithConn.Connector.Config.Username != "root" {
		t.Errorf("unexpected scanned resource: %+v", resWithConn)
	}

	// REGRESSION CHECK: query must NEVER select secret columns
	queryLower := strings.ToLower(capturedQuery)
	forbiddenColumns := []string{"encrypted_secret", "nonce", "auth_tag"}
	for _, col := range forbiddenColumns {
		if strings.Contains(queryLower, col) {
			t.Errorf("SECURITY FLAW: query selects forbidden secret column %q: %s", col, capturedQuery)
		}
	}

	// Tenant isolation check
	if !strings.Contains(capturedQuery, "organization_id = $2") {
		t.Errorf("SECURITY FLAW: query missing organization_id filter: %s", capturedQuery)
	}
}

func TestPostgresResourceRepository_ListForOrganization_NoSecretQuery(t *testing.T) {
	repo := NewPostgresResourceRepository()
	ctx := context.Background()
	orgID := uuid.New()

	var capturedQuery string
	mock := &mockQuerier{
		queryFunc: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			capturedQuery = sql
			return &mockRows{}, nil
		},
	}

	_, err := repo.ListForOrganization(ctx, mock, orgID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	queryLower := strings.ToLower(capturedQuery)
	forbiddenColumns := []string{"encrypted_secret", "nonce", "auth_tag"}
	for _, col := range forbiddenColumns {
		if strings.Contains(queryLower, col) {
			t.Errorf("SECURITY FLAW: List query selects forbidden secret column %q: %s", col, capturedQuery)
		}
	}

	if !strings.Contains(capturedQuery, "r.organization_id = $1") {
		t.Errorf("SECURITY FLAW: List query missing organization_id filter: %s", capturedQuery)
	}

	if !strings.Contains(capturedQuery, "ORDER BY r.created_at DESC, r.id DESC") {
		t.Errorf("List query missing deterministic ordering (created_at DESC, id DESC): %s", capturedQuery)
	}
}

func TestPostgresResourceRepository_ArchiveForOrganization(t *testing.T) {
	repo := NewPostgresResourceRepository()
	ctx := context.Background()
	orgID := uuid.New()
	resID := uuid.New()

	t.Run("successfully archives active resource with rows affected = 1", func(t *testing.T) {
		var capturedSQL string
		mock := &mockQuerier{
			execFunc: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				capturedSQL = sql
				return pgconn.NewCommandTag("UPDATE 1"), nil
			},
		}

		err := repo.ArchiveForOrganization(ctx, mock, orgID, resID)
		if err != nil {
			t.Errorf("expected success, got: %v", err)
		}
		if !strings.Contains(capturedSQL, "id = $1") || !strings.Contains(capturedSQL, "organization_id = $2") {
			t.Errorf("SECURITY FLAW: archive query missing tenant scope: %s", capturedSQL)
		}
	})

	t.Run("idempotent archive returns success if already archived", func(t *testing.T) {
		mock := &mockQuerier{
			execFunc: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				return pgconn.NewCommandTag("UPDATE 0"), nil
			},
			queryRowFunc: func(ctx context.Context, sql string, args ...any) pgx.Row {
				return &mockRow{
					scanFunc: func(dest ...any) error {
						*dest[0].(*int) = 1
						return nil
					},
				}
			},
		}

		err := repo.ArchiveForOrganization(ctx, mock, orgID, resID)
		if err != nil {
			t.Errorf("expected idempotent success on already archived resource, got: %v", err)
		}
	})

	t.Run("returns ErrResourceNotFound when resource does not exist in organization", func(t *testing.T) {
		mock := &mockQuerier{
			execFunc: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				return pgconn.NewCommandTag("UPDATE 0"), nil
			},
			queryRowFunc: func(ctx context.Context, sql string, args ...any) pgx.Row {
				return &mockRow{err: pgx.ErrNoRows}
			},
		}

		err := repo.ArchiveForOrganization(ctx, mock, orgID, resID)
		if !errors.Is(err, domain.ErrResourceNotFound) {
			t.Errorf("expected ErrResourceNotFound, got: %v", err)
		}
	})
}

func TestDecodeConnectorConfig(t *testing.T) {
	t.Run("Valid Ubuntu SSH config succeeds", func(t *testing.T) {
		raw := []byte(`{"username":"root","connection_timeout_seconds":15}`)
		cfg, err := decodeConnectorConfig(raw, domain.TypeUbuntuSSH)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.Username != "root" || *cfg.ConnectionTimeoutSeconds != 15 || cfg.UseHTTPS != nil {
			t.Errorf("unexpected decoded config: %+v", cfg)
		}
	})

	t.Run("Unicode username with 200 Persian runes (>255 UTF-8 bytes) succeeds", func(t *testing.T) {
		// 200 repetitions of Persian character 'ک' (2 bytes each in UTF-8 -> 400 bytes, but 200 runes)
		persianUsername := strings.Repeat("ک", 200)
		raw, _ := json.Marshal(map[string]any{
			"username":                   persianUsername,
			"connection_timeout_seconds": 20,
		})

		cfg, err := decodeConnectorConfig(raw, domain.TypeUbuntuSSH)
		if err != nil {
			t.Fatalf("REGRESSION: valid 200-rune Unicode username rejected: %v", err)
		}
		if cfg.Username != persianUsername {
			t.Errorf("expected username %s, got %s", persianUsername, cfg.Username)
		}
	})

	t.Run("Unicode username exceeding 255 runes returns ErrCorruptResourceData", func(t *testing.T) {
		// 256 runes
		longPersianUsername := strings.Repeat("ک", 256)
		raw, _ := json.Marshal(map[string]any{
			"username": longPersianUsername,
		})

		_, err := decodeConnectorConfig(raw, domain.TypeUbuntuSSH)
		if !errors.Is(err, domain.ErrCorruptResourceData) {
			t.Errorf("expected ErrCorruptResourceData for 256-rune username, got: %v", err)
		}
	})

	t.Run("Write and Read validation consistency roundtrip", func(t *testing.T) {
		validUnicodeUser := "کاربر-سرور-۰۱"
		timeout := 30
		validatedWrite, err := domain.ValidateConnectorConfig(domain.TypeUbuntuSSH, validUnicodeUser, &timeout, nil)
		if err != nil {
			t.Fatalf("write validation failed: %v", err)
		}

		persistedBytes, err := json.Marshal(validatedWrite)
		if err != nil {
			t.Fatalf("marshal failed: %v", err)
		}

		decodedRead, err := decodeConnectorConfig(persistedBytes, domain.TypeUbuntuSSH)
		if err != nil {
			t.Fatalf("read decoding failed: %v", err)
		}

		if decodedRead.Username != validatedWrite.Username ||
			*decodedRead.ConnectionTimeoutSeconds != *validatedWrite.ConnectionTimeoutSeconds ||
			decodedRead.UseHTTPS != validatedWrite.UseHTTPS {
			t.Errorf("write/read contract mismatch: write=%+v, read=%+v", validatedWrite, decodedRead)
		}
	})

	t.Run("Ubuntu SSH with use_https returns ErrCorruptResourceData", func(t *testing.T) {
		raw := []byte(`{"username":"root","use_https":true}`)
		_, err := decodeConnectorConfig(raw, domain.TypeUbuntuSSH)
		if !errors.Is(err, domain.ErrCorruptResourceData) {
			t.Errorf("expected ErrCorruptResourceData for Ubuntu SSH with use_https in DB, got: %v", err)
		}
	})

	t.Run("Valid cPanel config with use_https succeeds", func(t *testing.T) {
		raw := []byte(`{"username":"cpuser","use_https":true,"connection_timeout_seconds":20}`)
		cfg, err := decodeConnectorConfig(raw, domain.TypeCPanel)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.Username != "cpuser" || cfg.UseHTTPS == nil || !*cfg.UseHTTPS || *cfg.ConnectionTimeoutSeconds != 20 {
			t.Errorf("unexpected decoded config: %+v", cfg)
		}
	})

	t.Run("Valid cPanel config without use_https succeeds", func(t *testing.T) {
		raw := []byte(`{"username":"cpuser","connection_timeout_seconds":20}`)
		cfg, err := decodeConnectorConfig(raw, domain.TypeCPanel)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.Username != "cpuser" || cfg.UseHTTPS != nil {
			t.Errorf("unexpected decoded config: %+v", cfg)
		}
	})

	t.Run("Unknown field returns ErrCorruptResourceData (strict disallow unknown fields)", func(t *testing.T) {
		raw := []byte(`{"username":"root","password":"secret-leak"}`)
		_, err := decodeConnectorConfig(raw, domain.TypeUbuntuSSH)
		if !errors.Is(err, domain.ErrCorruptResourceData) {
			t.Errorf("expected ErrCorruptResourceData for DB config with unknown field, got: %v", err)
		}
	})

	t.Run("Missing or empty username returns ErrCorruptResourceData", func(t *testing.T) {
		raw1 := []byte(`{}`)
		_, err1 := decodeConnectorConfig(raw1, domain.TypeUbuntuSSH)
		if !errors.Is(err1, domain.ErrCorruptResourceData) {
			t.Errorf("expected ErrCorruptResourceData for missing username, got: %v", err1)
		}

		raw2 := []byte(`{"username":""}`)
		_, err2 := decodeConnectorConfig(raw2, domain.TypeUbuntuSSH)
		if !errors.Is(err2, domain.ErrCorruptResourceData) {
			t.Errorf("expected ErrCorruptResourceData for empty username, got: %v", err2)
		}
	})

	t.Run("Invalid timeout range returns ErrCorruptResourceData", func(t *testing.T) {
		raw := []byte(`{"username":"root","connection_timeout_seconds":5000}`)
		_, err := decodeConnectorConfig(raw, domain.TypeUbuntuSSH)
		if !errors.Is(err, domain.ErrCorruptResourceData) {
			t.Errorf("expected ErrCorruptResourceData for timeout > 300, got: %v", err)
		}
	})

	t.Run("Malformed JSON returns ErrCorruptResourceData", func(t *testing.T) {
		raw := []byte(`{not-json}`)
		_, err := decodeConnectorConfig(raw, domain.TypeUbuntuSSH)
		if !errors.Is(err, domain.ErrCorruptResourceData) {
			t.Errorf("expected ErrCorruptResourceData for malformed JSON, got: %v", err)
		}
	})

	t.Run("Strict trailing JSON tokens rejected with ErrCorruptResourceData", func(t *testing.T) {
		// Second JSON object
		raw1 := []byte(`{"username":"root"} {"username":"other"}`)
		_, err1 := decodeConnectorConfig(raw1, domain.TypeUbuntuSSH)
		if !errors.Is(err1, domain.ErrCorruptResourceData) {
			t.Errorf("expected ErrCorruptResourceData for second JSON object, got: %v", err1)
		}

		// Trailing scalar value
		raw2 := []byte(`{"username":"root"} true`)
		_, err2 := decodeConnectorConfig(raw2, domain.TypeUbuntuSSH)
		if !errors.Is(err2, domain.ErrCorruptResourceData) {
			t.Errorf("expected ErrCorruptResourceData for trailing scalar, got: %v", err2)
		}

		// Trailing garbage
		raw3 := []byte(`{"username":"root"} garbage`)
		_, err3 := decodeConnectorConfig(raw3, domain.TypeUbuntuSSH)
		if !errors.Is(err3, domain.ErrCorruptResourceData) {
			t.Errorf("expected ErrCorruptResourceData for trailing garbage, got: %v", err3)
		}

		// Trailing whitespace only is accepted
		raw4 := []byte("{\"username\":\"root\"}   \n\t  ")
		cfg4, err4 := decodeConnectorConfig(raw4, domain.TypeUbuntuSSH)
		if err4 != nil {
			t.Errorf("trailing whitespace should be accepted, got: %v", err4)
		}
		if cfg4.Username != "root" {
			t.Errorf("unexpected username: %s", cfg4.Username)
		}
	})
}

func TestValidateAndNormalizeLoadedResource_Integrity(t *testing.T) {
	orgID := uuid.New()
	resID := uuid.New()
	now := time.Now().UTC()

	baseResource := func() domain.Resource {
		return domain.Resource{
			ID:             resID,
			OrganizationID: orgID,
			Name:           "Valid Server",
			Type:           domain.TypeUbuntuSSH,
			Status:         domain.StatusActive,
			CreatedAt:      now,
			UpdatedAt:      now,
		}
	}

	baseConnector := func() domain.ResourceConnector {
		return domain.ResourceConnector{
			ID:             uuid.New(),
			OrganizationID: orgID,
			ResourceID:     resID,
			ConnectorType:  domain.ConnectorTypeUbuntuSSH,
			CredentialID:   uuid.New(),
			Host:           "192.168.1.10",
			Port:           22,
			AuthType:       domain.AuthTypeSSHKey,
			CreatedAt:      now,
			UpdatedAt:      now,
		}
	}

	validConfigRaw := []byte(`{"username":"root","connection_timeout_seconds":15}`)

	t.Run("Valid entities normalize host and return canonical model", func(t *testing.T) {
		res := baseResource()
		conn := baseConnector()
		conn.Host = "[2001:db8::1]" // Bracketed IPv6 in storage

		item, err := validateAndNormalizeLoadedResource(&res, &conn, validConfigRaw, "Cred Name", nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if item.Connector.Host != "2001:db8::1" {
			t.Errorf("expected unbracketed canonical IPv6, got %s", item.Connector.Host)
		}
	})

	t.Run("Connector type mismatch with resource type returns ErrCorruptResourceData", func(t *testing.T) {
		res := baseResource()
		res.Type = domain.TypeUbuntuSSH
		conn := baseConnector()
		conn.ConnectorType = domain.ConnectorTypeCPanel // Mismatched connector type

		_, err := validateAndNormalizeLoadedResource(&res, &conn, validConfigRaw, "Cred Name", nil)
		if !errors.Is(err, domain.ErrCorruptResourceData) {
			t.Errorf("expected ErrCorruptResourceData for mismatched connector type, got: %v", err)
		}
	})

	t.Run("Auth type mismatch with resource type returns ErrCorruptResourceData", func(t *testing.T) {
		res := baseResource()
		res.Type = domain.TypeUbuntuSSH
		conn := baseConnector()
		conn.AuthType = domain.AuthTypeCPanelAPIToken // Incompatible auth type

		_, err := validateAndNormalizeLoadedResource(&res, &conn, validConfigRaw, "Cred Name", nil)
		if !errors.Is(err, domain.ErrCorruptResourceData) {
			t.Errorf("expected ErrCorruptResourceData for incompatible auth type, got: %v", err)
		}
	})

	t.Run("Invalid stored port returns ErrCorruptResourceData", func(t *testing.T) {
		res := baseResource()
		conn := baseConnector()
		conn.Port = 70000 // Out of TCP range

		_, err := validateAndNormalizeLoadedResource(&res, &conn, validConfigRaw, "Cred Name", nil)
		if !errors.Is(err, domain.ErrCorruptResourceData) {
			t.Errorf("expected ErrCorruptResourceData for invalid port, got: %v", err)
		}
	})

	t.Run("cPanel with non-empty fingerprint returns ErrCorruptResourceData", func(t *testing.T) {
		res := baseResource()
		res.Type = domain.TypeCPanel
		conn := baseConnector()
		conn.ConnectorType = domain.ConnectorTypeCPanel
		conn.AuthType = domain.AuthTypeCPanelAPIToken
		fp := "SHA256:abc12345"
		conn.HostKeyFingerprint = &fp
		cpanelConfigRaw := []byte(`{"username":"cpuser"}`)

		_, err := validateAndNormalizeLoadedResource(&res, &conn, cpanelConfigRaw, "Cred Name", nil)
		if !errors.Is(err, domain.ErrCorruptResourceData) {
			t.Errorf("expected ErrCorruptResourceData for cPanel with fingerprint, got: %v", err)
		}
	})

	t.Run("Organization mismatch between resource and connector returns ErrCorruptResourceData", func(t *testing.T) {
		res := baseResource()
		conn := baseConnector()
		conn.OrganizationID = uuid.New() // Mismatched organization

		_, err := validateAndNormalizeLoadedResource(&res, &conn, validConfigRaw, "Cred Name", nil)
		if !errors.Is(err, domain.ErrCorruptResourceData) {
			t.Errorf("expected ErrCorruptResourceData for org mismatch, got: %v", err)
		}
	})

	t.Run("Resource ID mismatch between resource and connector returns ErrCorruptResourceData", func(t *testing.T) {
		res := baseResource()
		conn := baseConnector()
		conn.ResourceID = uuid.New() // Mismatched resource ID

		_, err := validateAndNormalizeLoadedResource(&res, &conn, validConfigRaw, "Cred Name", nil)
		if !errors.Is(err, domain.ErrCorruptResourceData) {
			t.Errorf("expected ErrCorruptResourceData for resource ID mismatch, got: %v", err)
		}
	})
}

func TestPostgresResourceRepository_UpdateConnectionTestStateForOrganization(t *testing.T) {
	ctx := context.Background()
	orgID := uuid.New()
	resID := uuid.New()
	now := time.Now().UTC()
	repo := NewPostgresResourceRepository()

	t.Run("Successfully updates connection test state and resource status", func(t *testing.T) {
		var capturedSQL string
		var capturedArgs []any

		mock := &mockQuerier{
			execFunc: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				capturedSQL = sql
				capturedArgs = args
				return pgconn.NewCommandTag("UPDATE 1"), nil
			},
		}

		err := repo.UpdateConnectionTestStateForOrganization(
			ctx,
			mock,
			orgID,
			resID,
			now,
			domain.ConnectionStatusSuccess,
			nil,
			domain.StatusActive,
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !strings.Contains(capturedSQL, "UPDATE resources") || !strings.Contains(capturedSQL, "status <> 'archived'") {
			t.Errorf("unexpected query: %s", capturedSQL)
		}
		if len(capturedArgs) != 6 {
			t.Fatalf("expected 6 args, got %d", len(capturedArgs))
		}
		if capturedArgs[0] != now {
			t.Errorf("expected lastTestAt %v, got %v", now, capturedArgs[0])
		}
		if capturedArgs[1] != string(domain.ConnectionStatusSuccess) {
			t.Errorf("expected status 'success', got %v", capturedArgs[1])
		}
		if capturedArgs[2] != nil && capturedArgs[2].(*string) != nil {
			t.Errorf("expected nil lastError, got %v", capturedArgs[2])
		}
		if capturedArgs[3] != string(domain.StatusActive) {
			t.Errorf("expected new status 'active', got %v", capturedArgs[3])
		}
		if capturedArgs[4] != resID {
			t.Errorf("expected resID %v, got %v", resID, capturedArgs[4])
		}
		if capturedArgs[5] != orgID {
			t.Errorf("expected orgID %v, got %v", orgID, capturedArgs[5])
		}
	})

	t.Run("Returns ErrResourceNotFound when 0 rows affected", func(t *testing.T) {
		mock := &mockQuerier{
			execFunc: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				return pgconn.NewCommandTag("UPDATE 0"), nil
			},
		}

		err := repo.UpdateConnectionTestStateForOrganization(
			ctx,
			mock,
			orgID,
			resID,
			now,
			domain.ConnectionStatusFailed,
			nil,
			domain.StatusUnreachable,
		)
		if !errors.Is(err, domain.ErrResourceNotFound) {
			t.Errorf("expected ErrResourceNotFound, got: %v", err)
		}
	})
}
