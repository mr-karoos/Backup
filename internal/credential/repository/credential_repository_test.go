package repository

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"backup-platform/internal/credential/domain"
	"backup-platform/pkg/uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type spyExecQuerier struct {
	capturedSQL  string
	capturedArgs []any
	rowsAffected int64
	execErr      error
}

func (s *spyExecQuerier) Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
	s.capturedSQL = sql
	s.capturedArgs = arguments
	if s.execErr != nil {
		return pgconn.CommandTag{}, s.execErr
	}
	tag := pgconn.NewCommandTag(fmt.Sprintf("DELETE %d", s.rowsAffected))
	return tag, nil
}

func (s *spyExecQuerier) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return nil, nil
}

func (s *spyExecQuerier) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return nil
}

type fakeRow struct {
	scanErr error
	values  []any
}

func (r *fakeRow) Scan(dest ...any) error {
	if r.scanErr != nil {
		return r.scanErr
	}
	for i, v := range r.values {
		if i >= len(dest) {
			break
		}
		switch d := dest[i].(type) {
		case *uuid.UUID:
			*d = v.(uuid.UUID)
		case *string:
			*d = v.(string)
		case **string:
			if v != nil {
				s := v.(string)
				*d = &s
			} else {
				*d = nil
			}
		case *int:
			*d = v.(int)
		case *[]byte:
			*d = v.([]byte)
		case *time.Time:
			*d = v.(time.Time)
		}
	}
	return nil
}

type spyQueryRowQuerier struct {
	capturedSQL  string
	capturedArgs []any
	row          pgx.Row
}

func (s *spyQueryRowQuerier) Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}

func (s *spyQueryRowQuerier) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return nil, nil
}

func (s *spyQueryRowQuerier) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	s.capturedSQL = sql
	s.capturedArgs = args
	return s.row
}

type fakeRows struct {
	data    [][]any
	cursor  int
	err     error
	scanErr error
}

func (f *fakeRows) Close() {}
func (f *fakeRows) Err() error {
	return f.err
}
func (f *fakeRows) CommandTag() pgconn.CommandTag {
	return pgconn.CommandTag{}
}
func (f *fakeRows) FieldDescriptions() []pgconn.FieldDescription {
	return nil
}
func (f *fakeRows) Next() bool {
	if f.cursor < len(f.data) {
		f.cursor++
		return true
	}
	return false
}
func (f *fakeRows) Scan(dest ...any) error {
	if f.scanErr != nil {
		return f.scanErr
	}
	row := f.data[f.cursor-1]
	for i, v := range row {
		if i >= len(dest) {
			break
		}
		switch d := dest[i].(type) {
		case *uuid.UUID:
			*d = v.(uuid.UUID)
		case *string:
			*d = v.(string)
		case **string:
			if v != nil {
				s := v.(string)
				*d = &s
			} else {
				*d = nil
			}
		case *int:
			*d = v.(int)
		case *time.Time:
			*d = v.(time.Time)
		}
	}
	return nil
}
func (f *fakeRows) Values() ([]any, error) {
	return nil, nil
}
func (f *fakeRows) RawValues() [][]byte {
	return nil
}
func (f *fakeRows) Conn() *pgx.Conn {
	return nil
}

type spyQueryQuerier struct {
	capturedSQL  string
	capturedArgs []any
	rows         pgx.Rows
	queryErr     error
}

func (s *spyQueryQuerier) Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}

func (s *spyQueryQuerier) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	s.capturedSQL = sql
	s.capturedArgs = args
	if s.queryErr != nil {
		return nil, s.queryErr
	}
	return s.rows, nil
}

func (s *spyQueryQuerier) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return nil
}

func TestPostgresCredentialRepository_Create(t *testing.T) {
	ctx := context.Background()
	repo := NewPostgresCredentialRepository()

	orgID := uuid.New()
	credID := uuid.New()
	now := time.Now().UTC()
	fp := "SHA256:abc123xyz"

	cred := &domain.Credential{
		ID:              credID,
		OrganizationID:  orgID,
		Name:            "Prod SSH Key",
		Type:            domain.TypeSSHPrivateKey,
		EncryptedSecret: []byte("encrypted-ciphertext-bytes"),
		Nonce:           []byte("nonce-12-byte"),
		AuthTag:         []byte("tag-16-bytes-len"),
		KeyVersion:      1,
		Fingerprint:     &fp,
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	t.Run("successfully executes insert with encrypted arguments", func(t *testing.T) {
		spy := &spyExecQuerier{}
		err := repo.Create(ctx, spy, cred)
		if err != nil {
			t.Fatalf("expected success, got: %v", err)
		}

		if !strings.Contains(spy.capturedSQL, "INSERT INTO credentials") {
			t.Errorf("unexpected query SQL: %s", spy.capturedSQL)
		}

		if len(spy.capturedArgs) != 12 {
			t.Fatalf("expected 12 query arguments, got %d", len(spy.capturedArgs))
		}

		// Validate argument mapping
		if spy.capturedArgs[0] != credID {
			t.Errorf("expected arg 0 (ID) to be %v, got %v", credID, spy.capturedArgs[0])
		}
		if spy.capturedArgs[1] != orgID {
			t.Errorf("expected arg 1 (OrgID) to be %v, got %v", orgID, spy.capturedArgs[1])
		}
		if spy.capturedArgs[2] != "Prod SSH Key" {
			t.Errorf("expected arg 2 (Name) to be 'Prod SSH Key', got %v", spy.capturedArgs[2])
		}
		if spy.capturedArgs[3] != string(domain.TypeSSHPrivateKey) {
			t.Errorf("expected arg 3 (Type) to be %s, got %v", domain.TypeSSHPrivateKey, spy.capturedArgs[3])
		}
		if spy.capturedArgs[4] != string(domain.ManagedByUser) {
			t.Errorf("expected arg 4 (ManagedBy) to be %s, got %v", domain.ManagedByUser, spy.capturedArgs[4])
		}
		if !bytes.Equal(spy.capturedArgs[5].([]byte), []byte("encrypted-ciphertext-bytes")) {
			t.Errorf("expected arg 5 (EncryptedSecret) to match")
		}
		if !bytes.Equal(spy.capturedArgs[6].([]byte), []byte("nonce-12-byte")) {
			t.Errorf("expected arg 6 (Nonce) to match")
		}
		if !bytes.Equal(spy.capturedArgs[7].([]byte), []byte("tag-16-bytes-len")) {
			t.Errorf("expected arg 7 (AuthTag) to match")
		}
		if spy.capturedArgs[8] != 1 {
			t.Errorf("expected arg 8 (KeyVersion) to be 1, got %v", spy.capturedArgs[8])
		}
		if *spy.capturedArgs[9].(*string) != fp {
			t.Errorf("expected arg 9 (Fingerprint) to be %s, got %v", fp, spy.capturedArgs[9])
		}
	})

	t.Run("returns raw error on execution failure", func(t *testing.T) {
		expectedErr := errors.New("db connection lost")
		spy := &spyExecQuerier{execErr: expectedErr}

		err := repo.Create(ctx, spy, cred)
		if !errors.Is(err, expectedErr) {
			t.Errorf("expected %v, got: %v", expectedErr, err)
		}
	})
}

func TestPostgresCredentialRepository_FindEncryptedByIDForOrganization(t *testing.T) {
	ctx := context.Background()
	repo := NewPostgresCredentialRepository()

	orgID := uuid.New()
	credID := uuid.New()
	now := time.Now().UTC()
	fp := "SHA256:test"

	t.Run("successfully queries with tenant isolation condition", func(t *testing.T) {
		fake := &spyQueryRowQuerier{
			row: &fakeRow{
				values: []any{
					credID,
					orgID,
					"Database Key",
					string(domain.TypeSSHPassword),
					string(domain.ManagedByUser),
					[]byte("encrypted-secret"),
					[]byte("nonce-bytes"),
					[]byte("auth-tag-bytes"),
					1,
					fp,
					now,
					now,
				},
			},
		}

		res, err := repo.FindEncryptedByIDForOrganization(ctx, fake, orgID, credID)
		if err != nil {
			t.Fatalf("expected success, got: %v", err)
		}

		// Ensure SQL explicitly queries both ID and organization_id
		if !strings.Contains(fake.capturedSQL, "WHERE id = $1 AND organization_id = $2") {
			t.Errorf("SQL query missing required tenant isolation clause: %s", fake.capturedSQL)
		}

		if len(fake.capturedArgs) != 2 || fake.capturedArgs[0] != credID || fake.capturedArgs[1] != orgID {
			t.Errorf("expected query arguments [credID, orgID], got %v", fake.capturedArgs)
		}

		if res.ID != credID || res.OrganizationID != orgID || res.Name != "Database Key" || res.Type != domain.TypeSSHPassword {
			t.Errorf("unexpected scanned entity: %+v", res)
		}
	})

	t.Run("returns ErrCredentialNotFound when pgx.ErrNoRows", func(t *testing.T) {
		fake := &spyQueryRowQuerier{
			row: &fakeRow{scanErr: pgx.ErrNoRows},
		}

		_, err := repo.FindEncryptedByIDForOrganization(ctx, fake, orgID, credID)
		if !errors.Is(err, domain.ErrCredentialNotFound) {
			t.Errorf("expected domain.ErrCredentialNotFound, got: %v", err)
		}
	})

	t.Run("returns raw db error on other scan errors", func(t *testing.T) {
		rawErr := errors.New("query timeout")
		fake := &spyQueryRowQuerier{
			row: &fakeRow{scanErr: rawErr},
		}

		_, err := repo.FindEncryptedByIDForOrganization(ctx, fake, orgID, credID)
		if !errors.Is(err, rawErr) {
			t.Errorf("expected rawErr, got: %v", err)
		}
	})
}

func TestPostgresCredentialRepository_ListMetadataForOrganization(t *testing.T) {
	ctx := context.Background()
	repo := NewPostgresCredentialRepository()
	orgID := uuid.New()
	now := time.Now().UTC()
	fp := "SHA256:fingerprint-list"

	t.Run("successfully queries safe metadata without encrypted columns", func(t *testing.T) {
		credID1 := uuid.New()
		credID2 := uuid.New()

		spy := &spyQueryQuerier{
			rows: &fakeRows{
				data: [][]any{
					{credID1, orgID, "Key 1", string(domain.TypeSSHPrivateKey), string(domain.ManagedByUser), fp, 1, now, now},
					{credID2, orgID, "Password 1", string(domain.TypeSSHPassword), string(domain.ManagedByUser), nil, 1, now.Add(-time.Hour), now.Add(-time.Hour)},
				},
			},
		}

		res, err := repo.ListMetadataForOrganization(ctx, spy, orgID)
		if err != nil {
			t.Fatalf("expected success, got: %v", err)
		}

		if len(res) != 2 {
			t.Fatalf("expected 2 items, got %d", len(res))
		}

		// Verify SQL has tenant filter and ordering
		if !strings.Contains(spy.capturedSQL, "WHERE organization_id = $1") {
			t.Errorf("SQL missing tenant where clause: %s", spy.capturedSQL)
		}
		if !strings.Contains(spy.capturedSQL, "ORDER BY created_at DESC, id DESC") {
			t.Errorf("SQL missing deterministic ordering: %s", spy.capturedSQL)
		}

		// Verify sensitive columns are NOT selected
		forbiddenCols := []string{"encrypted_secret", "nonce", "auth_tag"}
		for _, col := range forbiddenCols {
			if strings.Contains(spy.capturedSQL, col) {
				t.Errorf("SECURITY FLAW: List query selected sensitive column %s: %s", col, spy.capturedSQL)
			}
		}

		if res[0].ID != credID1 || res[0].Name != "Key 1" || *res[0].Fingerprint != fp {
			t.Errorf("unexpected item 0: %+v", res[0])
		}
		if res[1].ID != credID2 || res[1].Name != "Password 1" || res[1].Fingerprint != nil {
			t.Errorf("unexpected item 1: %+v", res[1])
		}
	})

	t.Run("returns empty non-nil slice when no rows found", func(t *testing.T) {
		spy := &spyQueryQuerier{
			rows: &fakeRows{data: [][]any{}},
		}

		res, err := repo.ListMetadataForOrganization(ctx, spy, orgID)
		if err != nil {
			t.Fatalf("expected success, got: %v", err)
		}
		if res == nil || len(res) != 0 {
			t.Errorf("expected empty non-nil slice, got: %v", res)
		}
	})

	t.Run("returns raw error when query execution fails", func(t *testing.T) {
		dbErr := errors.New("database connection refused")
		spy := &spyQueryQuerier{queryErr: dbErr}

		_, err := repo.ListMetadataForOrganization(ctx, spy, orgID)
		if !errors.Is(err, dbErr) {
			t.Errorf("expected raw dbErr, got: %v", err)
		}
	})
}

func TestPostgresCredentialRepository_FindMetadataForOrganization(t *testing.T) {
	repo := NewPostgresCredentialRepository()
	ctx := context.Background()
	orgID := uuid.New()
	credID := uuid.New()
	now := time.Now().UTC()
	fp := "SHA256:abcd"

	t.Run("successfully finds metadata with tenant scoping", func(t *testing.T) {
		spy := &spyQueryRowQuerier{
			row: &fakeRow{
				values: []any{
					credID,
					orgID,
					"My Key",
					"ssh_private_key",
					string(domain.ManagedByUser),
					fp,
					1,
					now,
					now,
				},
			},
		}

		m, err := repo.FindMetadataForOrganization(ctx, spy, orgID, credID)
		if err != nil {
			t.Fatalf("expected success, got: %v", err)
		}
		if m.ID != credID || m.OrganizationID != orgID || m.Name != "My Key" || m.Type != domain.TypeSSHPrivateKey || *m.Fingerprint != fp {
			t.Errorf("unexpected metadata: %+v", m)
		}
		if !strings.Contains(spy.capturedSQL, "WHERE id = $1 AND organization_id = $2") {
			t.Errorf("SQL missing tenant scoping: %s", spy.capturedSQL)
		}
	})

	t.Run("returns ErrCredentialNotFound when no row found", func(t *testing.T) {
		spy := &spyQueryRowQuerier{
			row: &fakeRow{scanErr: pgx.ErrNoRows},
		}

		_, err := repo.FindMetadataForOrganization(ctx, spy, orgID, credID)
		if !errors.Is(err, domain.ErrCredentialNotFound) {
			t.Errorf("expected ErrCredentialNotFound, got: %v", err)
		}
	})
}

func TestPostgresCredentialRepository_UpdateNameForOrganization(t *testing.T) {
	repo := NewPostgresCredentialRepository()
	ctx := context.Background()
	orgID := uuid.New()
	credID := uuid.New()
	now := time.Now().UTC()
	fp := "SHA256:xyz"

	t.Run("successfully updates name with tenant scoping and returns safe metadata", func(t *testing.T) {
		spy := &spyQueryRowQuerier{
			row: &fakeRow{
				values: []any{
					credID,
					orgID,
					"Updated Name",
					"ssh_private_key",
					string(domain.ManagedByUser),
					fp,
					1,
					now,
					now,
				},
			},
		}

		m, err := repo.UpdateNameForOrganization(ctx, spy, orgID, credID, "Updated Name")
		if err != nil {
			t.Fatalf("expected success, got: %v", err)
		}
		if m.Name != "Updated Name" || m.ID != credID || m.OrganizationID != orgID {
			t.Errorf("unexpected metadata: %+v", m)
		}
		if !strings.Contains(spy.capturedSQL, "UPDATE credentials") || !strings.Contains(spy.capturedSQL, "WHERE id = $1 AND organization_id = $2") {
			t.Errorf("SQL missing expected update statement: %s", spy.capturedSQL)
		}
		forbiddenCols := []string{"encrypted_secret", "nonce", "auth_tag"}
		for _, col := range forbiddenCols {
			if strings.Contains(spy.capturedSQL, col) {
				t.Errorf("SECURITY FLAW: UpdateName query touched sensitive column %s: %s", col, spy.capturedSQL)
			}
		}
	})

	t.Run("returns ErrCredentialNotFound when no rows affected", func(t *testing.T) {
		spy := &spyQueryRowQuerier{
			row: &fakeRow{scanErr: pgx.ErrNoRows},
		}

		_, err := repo.UpdateNameForOrganization(ctx, spy, orgID, credID, "New Name")
		if !errors.Is(err, domain.ErrCredentialNotFound) {
			t.Errorf("expected ErrCredentialNotFound, got: %v", err)
		}
	})
}

func TestPostgresCredentialRepository_UpdateEncryptedForOrganization(t *testing.T) {
	repo := NewPostgresCredentialRepository()
	ctx := context.Background()
	orgID := uuid.New()
	credID := uuid.New()
	now := time.Now().UTC()
	fp := "SHA256:newfp"

	cred := &domain.Credential{
		ID:              credID,
		OrganizationID:  orgID,
		Name:            "Rotated Secret Credential",
		Type:            domain.TypeSSHPrivateKey,
		EncryptedSecret: []byte("new-ciphertext"),
		Nonce:           []byte("new-nonce"),
		AuthTag:         []byte("new-tag"),
		KeyVersion:      2,
		Fingerprint:     &fp,
		UpdatedAt:       now,
	}

	t.Run("successfully updates encrypted secret and returns metadata", func(t *testing.T) {
		spy := &spyQueryRowQuerier{
			row: &fakeRow{
				values: []any{
					credID,
					orgID,
					"Rotated Secret Credential",
					"ssh_private_key",
					string(domain.ManagedByUser),
					fp,
					2,
					now,
					now,
				},
			},
		}

		m, err := repo.UpdateEncryptedForOrganization(ctx, spy, cred)
		if err != nil {
			t.Fatalf("expected success, got: %v", err)
		}
		if m.KeyVersion != 2 || m.Name != "Rotated Secret Credential" {
			t.Errorf("unexpected metadata: %+v", m)
		}
		if !strings.Contains(spy.capturedSQL, "WHERE id = $1 AND organization_id = $2") {
			t.Errorf("SQL missing tenant scoping: %s", spy.capturedSQL)
		}
	})

	t.Run("returns ErrCredentialNotFound when credential not found", func(t *testing.T) {
		spy := &spyQueryRowQuerier{
			row: &fakeRow{scanErr: pgx.ErrNoRows},
		}

		_, err := repo.UpdateEncryptedForOrganization(ctx, spy, cred)
		if !errors.Is(err, domain.ErrCredentialNotFound) {
			t.Errorf("expected ErrCredentialNotFound, got: %v", err)
		}
	})
}

func TestPostgresCredentialRepository_DeleteForOrganization(t *testing.T) {
	repo := NewPostgresCredentialRepository()
	ctx := context.Background()
	orgID := uuid.New()
	credID := uuid.New()

	t.Run("successfully deletes unreferenced credential in organization", func(t *testing.T) {
		spy := &spyExecQuerier{
			rowsAffected: 1,
		}

		err := repo.DeleteForOrganization(ctx, spy, orgID, credID)
		if err != nil {
			t.Fatalf("expected success, got: %v", err)
		}
		if !strings.Contains(spy.capturedSQL, "DELETE FROM credentials") || !strings.Contains(spy.capturedSQL, "WHERE id = $1 AND organization_id = $2") {
			t.Errorf("SQL missing tenant-scoped delete: %s", spy.capturedSQL)
		}
	})

	t.Run("returns ErrCredentialNotFound when no rows affected", func(t *testing.T) {
		spy := &spyExecQuerier{
			rowsAffected: 0,
		}

		err := repo.DeleteForOrganization(ctx, spy, orgID, credID)
		if !errors.Is(err, domain.ErrCredentialNotFound) {
			t.Errorf("expected ErrCredentialNotFound, got: %v", err)
		}
	})

	t.Run("returns ErrCredentialInUse on exact foreign key restrict violation", func(t *testing.T) {
		fkErr := &pgconn.PgError{
			Code:           "23503",
			ConstraintName: "fk_resource_connectors_org_credential",
		}
		spy := &spyExecQuerier{
			execErr: fkErr,
		}

		err := repo.DeleteForOrganization(ctx, spy, orgID, credID)
		if !errors.Is(err, domain.ErrCredentialInUse) {
			t.Errorf("expected ErrCredentialInUse, got: %v", err)
		}
	})

	t.Run("does NOT return ErrCredentialInUse on unrelated 23503 foreign key violation", func(t *testing.T) {
		unrelatedFkErr := &pgconn.PgError{
			Code:           "23503",
			ConstraintName: "some_other_unrelated_constraint",
		}
		spy := &spyExecQuerier{
			execErr: unrelatedFkErr,
		}

		err := repo.DeleteForOrganization(ctx, spy, orgID, credID)
		if errors.Is(err, domain.ErrCredentialInUse) {
			t.Errorf("should NOT convert unrelated FK violation to ErrCredentialInUse")
		}
		if !errors.Is(err, unrelatedFkErr) {
			t.Errorf("expected raw unrelated error, got: %v", err)
		}
	})

	t.Run("returns raw error on generic database failure", func(t *testing.T) {
		dbErr := errors.New("db disk failure")
		spy := &spyExecQuerier{
			execErr: dbErr,
		}

		err := repo.DeleteForOrganization(ctx, spy, orgID, credID)
		if !errors.Is(err, dbErr) {
			t.Errorf("expected raw dbErr, got: %v", err)
		}
	})
}

func TestPostgresCredentialRepository_DeleteSystemResticKeyForOrganization(t *testing.T) {
	ctx := context.Background()
	orgID := uuid.New()
	credID := uuid.New()
	repo := NewPostgresCredentialRepository()

	t.Run("successfully executes delete for system restic key", func(t *testing.T) {
		spy := &spyExecQuerier{
			rowsAffected: 1,
		}

		err := repo.DeleteSystemResticKeyForOrganization(ctx, spy, orgID, credID)
		if err != nil {
			t.Fatalf("expected nil error, got: %v", err)
		}

		if !strings.Contains(spy.capturedSQL, "type = 'restic_repository_key'") ||
			!strings.Contains(spy.capturedSQL, "managed_by = 'system'") {
			t.Errorf("query missing type/managed_by check: %s", spy.capturedSQL)
		}
	})

	t.Run("returns ErrCredentialNotFound when 0 rows affected", func(t *testing.T) {
		spy := &spyExecQuerier{
			rowsAffected: 0,
		}

		err := repo.DeleteSystemResticKeyForOrganization(ctx, spy, orgID, credID)
		if !errors.Is(err, domain.ErrCredentialNotFound) {
			t.Fatalf("expected ErrCredentialNotFound, got: %v", err)
		}
	})

	t.Run("converts fk_backup_repositories_credential to ErrCredentialInUse", func(t *testing.T) {
		fkErr := &pgconn.PgError{
			Code:           "23503",
			ConstraintName: "fk_backup_repositories_credential",
		}
		spy := &spyExecQuerier{
			execErr: fkErr,
		}

		err := repo.DeleteSystemResticKeyForOrganization(ctx, spy, orgID, credID)
		if !errors.Is(err, domain.ErrCredentialInUse) {
			t.Fatalf("expected ErrCredentialInUse, got: %v", err)
		}
	})
}
