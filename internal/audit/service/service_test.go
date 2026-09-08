package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"backup-platform/internal/audit/domain"
	orgDomain "backup-platform/internal/organization/domain"
	"backup-platform/pkg/uuid"
)

type mockAuditRepo struct {
	insertFunc        func(ctx context.Context, entry *domain.AuditLog) error
	listPaginatedFunc func(ctx context.Context, orgID uuid.UUID, filter domain.AuditLogFilter) ([]*domain.AuditLog, bool, error)
}

func (m *mockAuditRepo) Insert(ctx context.Context, entry *domain.AuditLog) error {
	if m.insertFunc != nil {
		return m.insertFunc(ctx, entry)
	}
	return nil
}

func (m *mockAuditRepo) ListPaginated(ctx context.Context, orgID uuid.UUID, filter domain.AuditLogFilter) ([]*domain.AuditLog, bool, error) {
	if m.listPaginatedFunc != nil {
		return m.listPaginatedFunc(ctx, orgID, filter)
	}
	return nil, false, nil
}

func TestAuditService_Record(t *testing.T) {
	orgID := uuid.New()
	userID := uuid.New()
	entID := uuid.New()

	t.Run("records valid audit log successfully", func(t *testing.T) {
		var recorded *domain.AuditLog
		repo := &mockAuditRepo{
			insertFunc: func(ctx context.Context, entry *domain.AuditLog) error {
				recorded = entry
				return nil
			},
		}

		svc := NewAuditService(repo, nil)

		ip := "127.0.0.1"
		ua := "test-agent"
		entry := &domain.AuditLog{
			ID:             uuid.New(),
			OrganizationID: &orgID,
			UserID:         &userID,
			Action:         domain.ActionBackupDownload,
			EntityType:     domain.EntityTypeBackupArtifact,
			EntityID:       &entID,
			IPAddress:      &ip,
			UserAgent:      &ua,
			Metadata:       []byte(`{"size_bytes":1024}`),
			CreatedAt:      time.Now(),
		}

		err := svc.Record(context.Background(), entry)
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if recorded == nil || recorded.Action != domain.ActionBackupDownload {
			t.Fatalf("expected audit log with action %s, got: %+v", domain.ActionBackupDownload, recorded)
		}
	})

	t.Run("returns error on nil entry", func(t *testing.T) {
		svc := NewAuditService(&mockAuditRepo{}, nil)
		err := svc.Record(context.Background(), nil)
		if err == nil {
			t.Fatalf("expected error on nil entry")
		}
	})

	t.Run("propagates repository error", func(t *testing.T) {
		repo := &mockAuditRepo{
			insertFunc: func(ctx context.Context, entry *domain.AuditLog) error {
				return errors.New("db error")
			},
		}
		svc := NewAuditService(repo, nil)
		err := svc.Record(context.Background(), &domain.AuditLog{Action: "test"})
		if err == nil {
			t.Fatalf("expected error when repo fails")
		}
	})
}

func TestAuditService_ListAuditLogs(t *testing.T) {
	ctx := context.Background()
	orgID := uuid.New()

	t.Run("admin role with audit_log:read is permitted", func(t *testing.T) {
		logID := uuid.New()
		repo := &mockAuditRepo{
			listPaginatedFunc: func(ctx context.Context, oID uuid.UUID, filter domain.AuditLogFilter) ([]*domain.AuditLog, bool, error) {
				return []*domain.AuditLog{
					{
						ID:             logID,
						OrganizationID: &oID,
						Action:         domain.ActionBackupDownload,
						EntityType:     domain.EntityTypeBackupArtifact,
						CreatedAt:      time.Now(),
					},
				}, false, nil
			},
		}

		svc := NewAuditService(repo, nil)
		res, err := svc.ListAuditLogs(ctx, orgDomain.RoleAdmin, orgID, domain.AuditLogFilter{})
		if err != nil {
			t.Fatalf("unexpected error for admin: %v", err)
		}
		if len(res.Logs) != 1 || res.Logs[0].ID != logID {
			t.Fatalf("expected 1 log with ID %s, got %+v", logID, res.Logs)
		}
		if res.HasMore || res.NextCursor != nil {
			t.Fatalf("expected has_more=false, next_cursor=nil")
		}
	})

	t.Run("member and viewer roles are rejected with ErrUnauthorizedRole", func(t *testing.T) {
		svc := NewAuditService(&mockAuditRepo{}, nil)
		unauthorizedRoles := []orgDomain.Role{
			orgDomain.RoleMember,
			orgDomain.RoleViewer,
			orgDomain.Role("unauthorized_guest"),
			orgDomain.Role(""),
		}

		for _, role := range unauthorizedRoles {
			res, err := svc.ListAuditLogs(ctx, role, orgID, domain.AuditLogFilter{})
			if !errors.Is(err, domain.ErrUnauthorizedRole) {
				t.Fatalf("expected ErrUnauthorizedRole for role %q, got err=%v, res=%v", role, err, res)
			}
		}
	})

	t.Run("nil orgID rejected with ErrInvalidAuditFilter", func(t *testing.T) {
		svc := NewAuditService(&mockAuditRepo{}, nil)
		_, err := svc.ListAuditLogs(ctx, orgDomain.RoleAdmin, uuid.Nil, domain.AuditLogFilter{})
		if !errors.Is(err, domain.ErrInvalidAuditFilter) {
			t.Fatalf("expected ErrInvalidAuditFilter for nil orgID, got %v", err)
		}
	})

	t.Run("invalid filter validation rejects bad action, entity_type, and from > to", func(t *testing.T) {
		svc := NewAuditService(&mockAuditRepo{}, nil)

		// Empty action
		emptyAction := "   "
		_, err := svc.ListAuditLogs(ctx, orgDomain.RoleAdmin, orgID, domain.AuditLogFilter{Action: &emptyAction})
		if !errors.Is(err, domain.ErrInvalidAuditFilter) {
			t.Fatalf("expected ErrInvalidAuditFilter for empty action, got %v", err)
		}

		// Action > 100
		longAction := strings.Repeat("a", 101)
		_, err = svc.ListAuditLogs(ctx, orgDomain.RoleAdmin, orgID, domain.AuditLogFilter{Action: &longAction})
		if !errors.Is(err, domain.ErrInvalidAuditFilter) {
			t.Fatalf("expected ErrInvalidAuditFilter for action > 100, got %v", err)
		}

		// Empty entity_type
		emptyEntity := ""
		_, err = svc.ListAuditLogs(ctx, orgDomain.RoleAdmin, orgID, domain.AuditLogFilter{EntityType: &emptyEntity})
		if !errors.Is(err, domain.ErrInvalidAuditFilter) {
			t.Fatalf("expected ErrInvalidAuditFilter for empty entity_type, got %v", err)
		}

		// EntityType > 50
		longEntity := strings.Repeat("e", 51)
		_, err = svc.ListAuditLogs(ctx, orgDomain.RoleAdmin, orgID, domain.AuditLogFilter{EntityType: &longEntity})
		if !errors.Is(err, domain.ErrInvalidAuditFilter) {
			t.Fatalf("expected ErrInvalidAuditFilter for entity_type > 50, got %v", err)
		}

		// from > to
		now := time.Now()
		from := now.Add(time.Hour)
		to := now
		_, err = svc.ListAuditLogs(ctx, orgDomain.RoleAdmin, orgID, domain.AuditLogFilter{From: &from, To: &to})
		if !errors.Is(err, domain.ErrInvalidAuditFilter) {
			t.Fatalf("expected ErrInvalidAuditFilter for from > to, got %v", err)
		}

		// EntityID is uuid.Nil
		nilUUID := uuid.Nil
		_, err = svc.ListAuditLogs(ctx, orgDomain.RoleAdmin, orgID, domain.AuditLogFilter{EntityID: &nilUUID})
		if !errors.Is(err, domain.ErrInvalidAuditFilter) {
			t.Fatalf("expected ErrInvalidAuditFilter for entity_id == uuid.Nil, got %v", err)
		}

		// UserID is uuid.Nil
		_, err = svc.ListAuditLogs(ctx, orgDomain.RoleAdmin, orgID, domain.AuditLogFilter{UserID: &nilUUID})
		if !errors.Is(err, domain.ErrInvalidAuditFilter) {
			t.Fatalf("expected ErrInvalidAuditFilter for user_id == uuid.Nil, got %v", err)
		}

		// CursorCreatedAt set but CursorID nil
		curTime := time.Now()
		_, err = svc.ListAuditLogs(ctx, orgDomain.RoleAdmin, orgID, domain.AuditLogFilter{CursorCreatedAt: &curTime, CursorID: nil})
		if !errors.Is(err, domain.ErrInvalidAuditFilter) {
			t.Fatalf("expected ErrInvalidAuditFilter for CursorCreatedAt set without CursorID, got %v", err)
		}

		// CursorID set but CursorCreatedAt nil
		curID := uuid.New()
		_, err = svc.ListAuditLogs(ctx, orgDomain.RoleAdmin, orgID, domain.AuditLogFilter{CursorCreatedAt: nil, CursorID: &curID})
		if !errors.Is(err, domain.ErrInvalidAuditFilter) {
			t.Fatalf("expected ErrInvalidAuditFilter for CursorID set without CursorCreatedAt, got %v", err)
		}

		// CursorID set to uuid.Nil
		_, err = svc.ListAuditLogs(ctx, orgDomain.RoleAdmin, orgID, domain.AuditLogFilter{CursorCreatedAt: &curTime, CursorID: &nilUUID})
		if !errors.Is(err, domain.ErrInvalidAuditFilter) {
			t.Fatalf("expected ErrInvalidAuditFilter for CursorID == uuid.Nil, got %v", err)
		}
	})

	t.Run("defense-in-depth accepts non-nil zero time for From, To, and CursorCreatedAt", func(t *testing.T) {
		called := false
		zeroTime := time.Time{}
		curID := uuid.New()
		repo := &mockAuditRepo{
			listPaginatedFunc: func(ctx context.Context, oID uuid.UUID, filter domain.AuditLogFilter) ([]*domain.AuditLog, bool, error) {
				called = true
				if filter.From == nil || !filter.From.IsZero() {
					t.Errorf("expected zero From time to be passed to repo")
				}
				if filter.To == nil || !filter.To.IsZero() {
					t.Errorf("expected zero To time to be passed to repo")
				}
				if filter.CursorCreatedAt == nil || !filter.CursorCreatedAt.IsZero() {
					t.Errorf("expected zero CursorCreatedAt to be passed to repo")
				}
				return []*domain.AuditLog{}, false, nil
			},
		}

		svc := NewAuditService(repo, nil)
		_, err := svc.ListAuditLogs(ctx, orgDomain.RoleAdmin, orgID, domain.AuditLogFilter{
			From:            &zeroTime,
			To:              &zeroTime,
			CursorCreatedAt: &zeroTime,
			CursorID:        &curID,
		})
		if err != nil {
			t.Fatalf("unexpected error for zero-time filters: %v", err)
		}
		if !called {
			t.Fatalf("expected repo to be called")
		}
	})

	t.Run("calculates next_cursor when has_more is true", func(t *testing.T) {
		now := time.Now().UTC()
		logID := uuid.New()
		repo := &mockAuditRepo{
			listPaginatedFunc: func(ctx context.Context, oID uuid.UUID, filter domain.AuditLogFilter) ([]*domain.AuditLog, bool, error) {
				return []*domain.AuditLog{
					{
						ID:             logID,
						OrganizationID: &oID,
						Action:         domain.ActionBackupDownload,
						EntityType:     domain.EntityTypeBackupArtifact,
						CreatedAt:      now,
					},
				}, true, nil
			},
		}

		svc := NewAuditService(repo, nil)
		res, err := svc.ListAuditLogs(ctx, orgDomain.RoleAdmin, orgID, domain.AuditLogFilter{Limit: 1})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !res.HasMore {
			t.Fatalf("expected HasMore = true")
		}
		if res.NextCursor == nil || *res.NextCursor == "" {
			t.Fatalf("expected non-empty next_cursor")
		}

		decTime, decID, err := domain.DecodeAuditCursor(*res.NextCursor)
		if err != nil {
			t.Fatalf("failed decoding cursor: %v", err)
		}
		if decID != logID || decTime.Unix() != now.Unix() {
			t.Fatalf("cursor decoded values mismatch")
		}
	})

	t.Run("repository error is wrapped as ErrAuditServiceUnavailable", func(t *testing.T) {
		repo := &mockAuditRepo{
			listPaginatedFunc: func(ctx context.Context, oID uuid.UUID, filter domain.AuditLogFilter) ([]*domain.AuditLog, bool, error) {
				return nil, false, errors.New("db connection lost")
			},
		}

		svc := NewAuditService(repo, nil)
		_, err := svc.ListAuditLogs(ctx, orgDomain.RoleAdmin, orgID, domain.AuditLogFilter{})
		if !errors.Is(err, domain.ErrAuditServiceUnavailable) {
			t.Fatalf("expected ErrAuditServiceUnavailable, got %v", err)
		}
	})
}
