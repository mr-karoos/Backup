package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"backup-platform/internal/audit/domain"
	"backup-platform/pkg/uuid"
)

type mockAuditRepo struct {
	insertFunc func(ctx context.Context, entry *domain.AuditLog) error
}

func (m *mockAuditRepo) Insert(ctx context.Context, entry *domain.AuditLog) error {
	if m.insertFunc != nil {
		return m.insertFunc(ctx, entry)
	}
	return nil
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
