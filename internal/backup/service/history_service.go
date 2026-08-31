package service

import (
	"context"
	"fmt"
	"log/slog"

	"backup-platform/internal/backup/domain"
	"backup-platform/internal/backup/repository"
	orgDomain "backup-platform/internal/organization/domain"
	"backup-platform/pkg/uuid"
)

// HistoryService manages retrieval of historical backup runs and their derived execution metrics.
type HistoryService struct {
	repo   repository.BackupRepository
	logger *slog.Logger
}

// NewHistoryService constructs a new HistoryService.
func NewHistoryService(repo repository.BackupRepository, logger *slog.Logger) *HistoryService {
	if logger == nil {
		logger = slog.Default()
	}
	return &HistoryService{
		repo:   repo,
		logger: logger,
	}
}

// ListRuns retrieves filtered backup runs for an organization.
func (s *HistoryService) ListRuns(
	ctx context.Context,
	role orgDomain.Role,
	orgID uuid.UUID,
	filter domain.RunFilter,
) ([]*domain.BackupRunWithStats, error) {
	if orgID == uuid.Nil {
		return nil, domain.ErrInvalidRunFilter
	}

	runs, err := s.repo.ListRuns(ctx, orgID, filter)
	if err != nil {
		s.logger.Error("failed listing backup runs", slog.String("org_id", orgID.String()))
		return nil, domain.ErrBackupServiceUnavailable
	}

	return runs, nil
}

// GetRun retrieves a single backup run with its derived statistics for an organization.
func (s *HistoryService) GetRun(
	ctx context.Context,
	role orgDomain.Role,
	orgID, runID uuid.UUID,
) (*domain.BackupRunWithStats, error) {
	if orgID == uuid.Nil || runID == uuid.Nil {
		return nil, domain.ErrRunNotFound
	}

	run, err := s.repo.GetRunDetail(ctx, orgID, runID)
	if err != nil {
		if err == domain.ErrRunNotFound {
			return nil, domain.ErrRunNotFound
		}
		s.logger.Error("failed getting backup run detail", slog.String("org_id", orgID.String()), slog.String("run_id", runID.String()))
		return nil, fmt.Errorf("failed retrieving backup run: %w", err)
	}

	return run, nil
}
