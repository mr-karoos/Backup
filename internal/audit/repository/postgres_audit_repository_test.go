package repository

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"backup-platform/internal/audit/domain"
	"backup-platform/internal/platform/database"
	"backup-platform/pkg/uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type mockQuerier struct {
	execFunc  func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	queryFunc func(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

func (m *mockQuerier) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if m.execFunc != nil {
		return m.execFunc(ctx, sql, args...)
	}
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}

func (m *mockQuerier) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if m.queryFunc != nil {
		return m.queryFunc(ctx, sql, args...)
	}
	return nil, nil
}

func (m *mockQuerier) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return nil
}

type mockTxManager struct {
	querier database.Querier
}

func (m *mockTxManager) Querier() database.Querier {
	return m.querier
}

func (m *mockTxManager) WithinTx(ctx context.Context, fn func(q database.Querier) error) error {
	return fn(m.querier)
}

func TestPostgresAuditRepository_Insert(t *testing.T) {
	orgID := uuid.New()
	userID := uuid.New()
	entityID := uuid.New()
	ip := "192.168.1.100"
	ua := "Mozilla/5.0"

	t.Run("successfully inserts parameterized audit log entry", func(t *testing.T) {
		var capturedSQL string
		var capturedArgs []any

		q := &mockQuerier{
			execFunc: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				capturedSQL = sql
				capturedArgs = args
				return pgconn.NewCommandTag("INSERT 0 1"), nil
			},
		}

		repo := NewPostgresAuditRepository(&mockTxManager{querier: q})

		entry := &domain.AuditLog{
			ID:             uuid.New(),
			OrganizationID: &orgID,
			UserID:         &userID,
			Action:         domain.ActionBackupDownload,
			EntityType:     domain.EntityTypeBackupArtifact,
			EntityID:       &entityID,
			IPAddress:      &ip,
			UserAgent:      &ua,
			Metadata:       []byte(`{"size_bytes":2048}`),
			CreatedAt:      time.Now(),
		}

		err := repo.Insert(context.Background(), entry)
		if err != nil {
			t.Fatalf("expected nil error, got: %v", err)
		}

		if !strings.Contains(capturedSQL, "INSERT INTO audit_logs") {
			t.Errorf("expected SQL to contain INSERT INTO audit_logs, got: %s", capturedSQL)
		}

		if len(capturedArgs) != 9 {
			t.Fatalf("expected 9 arguments, got %d", len(capturedArgs))
		}

		if capturedArgs[0] != entry.ID {
			t.Errorf("arg 0 (ID) mismatch: expected %v, got %v", entry.ID, capturedArgs[0])
		}
		if capturedArgs[1] != entry.OrganizationID {
			t.Errorf("arg 1 (OrganizationID) mismatch: expected %v, got %v", entry.OrganizationID, capturedArgs[1])
		}
		if capturedArgs[2] != entry.UserID {
			t.Errorf("arg 2 (UserID) mismatch: expected %v, got %v", entry.UserID, capturedArgs[2])
		}
		if capturedArgs[3] != domain.ActionBackupDownload {
			t.Errorf("arg 3 (Action) mismatch: expected %v, got %v", domain.ActionBackupDownload, capturedArgs[3])
		}
		if capturedArgs[4] != domain.EntityTypeBackupArtifact {
			t.Errorf("arg 4 (EntityType) mismatch: expected %v, got %v", domain.EntityTypeBackupArtifact, capturedArgs[4])
		}
		if capturedArgs[5] != entry.EntityID {
			t.Errorf("arg 5 (EntityID) mismatch: expected %v, got %v", entry.EntityID, capturedArgs[5])
		}
		if capturedArgs[6] != entry.IPAddress {
			t.Errorf("arg 6 (IPAddress) mismatch: expected %v, got %v", entry.IPAddress, capturedArgs[6])
		}
		if capturedArgs[7] != entry.UserAgent {
			t.Errorf("arg 7 (UserAgent) mismatch: expected %v, got %v", entry.UserAgent, capturedArgs[7])
		}
		if string(capturedArgs[8].(json.RawMessage)) != `{"size_bytes":2048}` {
			t.Errorf("arg 8 (Metadata) mismatch: expected %s, got %s", `{"size_bytes":2048}`, string(capturedArgs[8].(json.RawMessage)))
		}
	})

	t.Run("assigns UUID if ID is Nil and defaults empty metadata to {}", func(t *testing.T) {
		var capturedArgs []any

		q := &mockQuerier{
			execFunc: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				capturedArgs = args
				return pgconn.NewCommandTag("INSERT 0 1"), nil
			},
		}

		repo := NewPostgresAuditRepository(&mockTxManager{querier: q})

		entry := &domain.AuditLog{
			ID:             uuid.Nil,
			OrganizationID: &orgID,
			Action:         domain.ActionBackupDelete,
			EntityType:     domain.EntityTypeBackupArtifact,
		}

		err := repo.Insert(context.Background(), entry)
		if err != nil {
			t.Fatalf("expected nil error, got: %v", err)
		}

		if entry.ID == uuid.Nil {
			t.Errorf("expected entry.ID to be populated with new UUID")
		}

		if string(capturedArgs[8].(json.RawMessage)) != "{}" {
			t.Errorf("expected default metadata {}, got: %s", string(capturedArgs[8].(json.RawMessage)))
		}
	})

	t.Run("returns error when entry is nil", func(t *testing.T) {
		repo := NewPostgresAuditRepository(&mockTxManager{querier: &mockQuerier{}})
		err := repo.Insert(context.Background(), nil)
		if err == nil {
			t.Fatalf("expected error on nil entry")
		}
	})

	t.Run("propagates database error to caller", func(t *testing.T) {
		dbErr := errors.New("connection reset by peer")
		q := &mockQuerier{
			execFunc: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, dbErr
			},
		}

		repo := NewPostgresAuditRepository(&mockTxManager{querier: q})
		entry := &domain.AuditLog{
			Action:     domain.ActionBackupDelete,
			EntityType: domain.EntityTypeBackupArtifact,
		}

		err := repo.Insert(context.Background(), entry)
		if err == nil || !strings.Contains(err.Error(), "connection reset by peer") {
			t.Fatalf("expected database error propagation, got: %v", err)
		}
	})
}

func TestPostgresAuditRepository_ListPaginated(t *testing.T) {
	orgID := uuid.New()
	action := "backup.download"
	entityType := "backup_artifact"
	entityID := uuid.New()
	userID := uuid.New()
	now := time.Now().UTC()
	from := now.Add(-1 * time.Hour)
	to := now
	cursorID := uuid.New()

	t.Run("nil orgID returns error", func(t *testing.T) {
		repo := NewPostgresAuditRepository(&mockTxManager{querier: &mockQuerier{}})
		_, _, err := repo.ListPaginated(context.Background(), uuid.Nil, domain.AuditLogFilter{})
		if err == nil {
			t.Fatalf("expected error when orgID is uuid.Nil")
		}
	})

	t.Run("constructs correct parameterized SQL query with all filters and cursor", func(t *testing.T) {
		var capturedSQL string
		var capturedArgs []any

		q := &mockQuerier{
			queryFunc: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
				capturedSQL = sql
				capturedArgs = args
				return &mockRows{}, nil
			},
		}

		repo := NewPostgresAuditRepository(&mockTxManager{querier: q})

		filter := domain.AuditLogFilter{
			Limit:           25,
			CursorCreatedAt: &from,
			CursorID:        &cursorID,
			Action:          &action,
			EntityType:      &entityType,
			EntityID:        &entityID,
			UserID:          &userID,
			From:            &from,
			To:              &to,
		}

		_, _, err := repo.ListPaginated(context.Background(), orgID, filter)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Verify organization_id is always the first predicate
		if !strings.Contains(capturedSQL, "WHERE organization_id = $1") {
			t.Errorf("expected SQL to contain 'WHERE organization_id = $1', got: %s", capturedSQL)
		}
		if capturedArgs[0] != orgID {
			t.Errorf("arg 0 (orgID) mismatch: expected %v, got %v", orgID, capturedArgs[0])
		}

		// Verify other filter clauses are present
		if !strings.Contains(capturedSQL, "AND action = $") {
			t.Errorf("expected SQL to contain 'AND action = $', got: %s", capturedSQL)
		}
		if !strings.Contains(capturedSQL, "AND entity_type = $") {
			t.Errorf("expected SQL to contain 'AND entity_type = $', got: %s", capturedSQL)
		}
		if !strings.Contains(capturedSQL, "AND entity_id = $") {
			t.Errorf("expected SQL to contain 'AND entity_id = $', got: %s", capturedSQL)
		}
		if !strings.Contains(capturedSQL, "AND user_id = $") {
			t.Errorf("expected SQL to contain 'AND user_id = $', got: %s", capturedSQL)
		}
		if !strings.Contains(capturedSQL, "AND created_at >= $") {
			t.Errorf("expected SQL to contain 'AND created_at >= $', got: %s", capturedSQL)
		}
		if !strings.Contains(capturedSQL, "AND created_at <= $") {
			t.Errorf("expected SQL to contain 'AND created_at <= $', got: %s", capturedSQL)
		}
		if !strings.Contains(capturedSQL, "AND (created_at < $") || !strings.Contains(capturedSQL, "ORDER BY created_at DESC, id DESC LIMIT $") {
			t.Errorf("expected SQL to contain keyset predicate and order/limit, got: %s", capturedSQL)
		}

		// Check limit + 1 = 26
		lastArg := capturedArgs[len(capturedArgs)-1]
		if lastArg != 26 {
			t.Errorf("expected last argument (limit+1) to be 26, got: %v", lastArg)
		}
	})

	t.Run("non-nil zero From and To include created_at predicates in SQL", func(t *testing.T) {
		var capturedSQL string
		var capturedArgs []any

		q := &mockQuerier{
			queryFunc: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
				capturedSQL = sql
				capturedArgs = args
				return &mockRows{}, nil
			},
		}

		repo := NewPostgresAuditRepository(&mockTxManager{querier: q})

		zeroFrom := time.Time{}
		zeroTo := time.Time{}
		filter := domain.AuditLogFilter{
			From: &zeroFrom,
			To:   &zeroTo,
		}

		_, _, err := repo.ListPaginated(context.Background(), orgID, filter)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !strings.Contains(capturedSQL, "AND created_at >= $") {
			t.Errorf("expected SQL to contain 'AND created_at >= $' for zero From, got: %s", capturedSQL)
		}
		if !strings.Contains(capturedSQL, "AND created_at <= $") {
			t.Errorf("expected SQL to contain 'AND created_at <= $' for zero To, got: %s", capturedSQL)
		}

		// Ensure zero times are passed in args
		foundFrom := false
		foundTo := false
		for _, arg := range capturedArgs {
			if tm, ok := arg.(time.Time); ok && tm.IsZero() {
				if !foundFrom {
					foundFrom = true
				} else {
					foundTo = true
				}
			}
		}
		if !foundFrom || !foundTo {
			t.Errorf("expected both zero From and To to be passed in query args")
		}
	})

	t.Run("defaults limit to 50 when <= 0 or > 100", func(t *testing.T) {
		for _, invalidLimit := range []int{0, -10, 101, 500} {
			var capturedArgs []any
			q := &mockQuerier{
				queryFunc: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
					capturedArgs = args
					return &mockRows{}, nil
				},
			}
			repo := NewPostgresAuditRepository(&mockTxManager{querier: q})
			_, _, err := repo.ListPaginated(context.Background(), orgID, domain.AuditLogFilter{Limit: invalidLimit})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			lastArg := capturedArgs[len(capturedArgs)-1]
			if lastArg != 51 { // 50 + 1
				t.Errorf("expected default limit+1 to be 51 for limit %d, got: %v", invalidLimit, lastArg)
			}
		}
	})

	t.Run("propagates query error", func(t *testing.T) {
		dbErr := errors.New("db connection down")
		q := &mockQuerier{
			queryFunc: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
				return nil, dbErr
			},
		}
		repo := NewPostgresAuditRepository(&mockTxManager{querier: q})
		_, _, err := repo.ListPaginated(context.Background(), orgID, domain.AuditLogFilter{})
		if err == nil || !strings.Contains(err.Error(), "db connection down") {
			t.Fatalf("expected db connection error, got: %v", err)
		}
	})
}

type mockRows struct{}

func (m *mockRows) Close()                                       {}
func (m *mockRows) Err() error                                   { return nil }
func (m *mockRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (m *mockRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (m *mockRows) Next() bool                                   { return false }
func (m *mockRows) Scan(dest ...any) error                       { return nil }
func (m *mockRows) Values() ([]any, error)                       { return nil, nil }
func (m *mockRows) RawValues() [][]byte                          { return nil }
func (m *mockRows) Conn() *pgx.Conn                              { return nil }
