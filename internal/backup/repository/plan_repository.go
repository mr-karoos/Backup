package repository

import (
	"context"

	"backup-platform/internal/backup/domain"
	"backup-platform/pkg/uuid"
)

// BackupPlanRepository defines the storage interface for managing backup plans within tenant organizations.
type BackupPlanRepository interface {
	CreatePlan(ctx context.Context, plan *domain.BackupPlan) (*domain.BackupPlan, error)
	GetPlanByID(ctx context.Context, orgID, planID uuid.UUID) (*domain.BackupPlan, error)
	GetPlanWithResourceByID(ctx context.Context, orgID, planID uuid.UUID) (*domain.BackupPlanWithResource, error)
	ListPlans(ctx context.Context, orgID uuid.UUID, filter domain.PlanFilter) ([]*domain.BackupPlanWithResource, error)
	UpdatePlan(ctx context.Context, plan *domain.BackupPlan) (*domain.BackupPlan, error)
	ArchivePlan(ctx context.Context, orgID, planID uuid.UUID) error
}
