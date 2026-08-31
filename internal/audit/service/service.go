package service

import (
	"context"
	"fmt"
	"log/slog"

	"backup-platform/internal/audit/domain"
	"backup-platform/internal/audit/repository"
)

// AuditRecorder provides high-level recording of system and operational audit logs.
type AuditRecorder interface {
	Record(ctx context.Context, entry *domain.AuditLog) error
}

// AuditService coordinates audit log persistence.
type AuditService struct {
	repo   repository.AuditRepository
	logger *slog.Logger
}

// NewAuditService constructs a new AuditService.
func NewAuditService(repo repository.AuditRepository, logger *slog.Logger) *AuditService {
	if logger == nil {
		logger = slog.Default()
	}
	return &AuditService{
		repo:   repo,
		logger: logger,
	}
}

// Record persists an audit event.
func (s *AuditService) Record(ctx context.Context, entry *domain.AuditLog) error {
	if entry == nil {
		return fmt.Errorf("audit entry cannot be nil")
	}
	if err := s.repo.Insert(ctx, entry); err != nil {
		s.logger.Error("failed recording audit log", slog.String("action", entry.Action), slog.String("entity_type", entry.EntityType))
		return err
	}
	return nil
}
