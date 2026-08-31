package repository

import (
	"context"
	"time"

	"backup-platform/internal/backup/domain"
	"backup-platform/pkg/uuid"
)

// BackupRepository abstracts all database persistence operations for plans, jobs, runs, artifacts, and storage targets.
type BackupRepository interface {
	// Storage Targets
	EnsureDefaultLocalStorageTarget(ctx context.Context, orgID uuid.UUID) (*domain.StorageTarget, error)
	GetStorageTargetByID(ctx context.Context, orgID, targetID uuid.UUID) (*domain.StorageTarget, error)

	// Backup Plans (Read-only query in Phase 5)
	GetPlanByID(ctx context.Context, orgID, planID uuid.UUID) (*domain.BackupPlan, error)

	// Backup Jobs
	CreateJob(ctx context.Context, job *domain.BackupJob) (*domain.BackupJob, error)
	GetJobByID(ctx context.Context, orgID, jobID uuid.UUID) (*domain.BackupJob, error)
	GetActiveManualJobForResource(ctx context.Context, orgID, resourceID uuid.UUID) (*domain.BackupJob, error)
	GetActiveJobConflictForResource(ctx context.Context, orgID, resourceID uuid.UUID) (*domain.BackupJob, error)
	FindPendingJobs(ctx context.Context, limit int, afterCreatedAt *time.Time, afterID *uuid.UUID) ([]*domain.BackupJob, error)

	// Transactional Claim & Runs
	TransactionalClaimJob(ctx context.Context, orgID, jobID uuid.UUID) (*domain.BackupRun, *domain.BackupJob, error)
	GetRunByID(ctx context.Context, orgID, runID uuid.UUID) (*domain.BackupRun, error)
	GetRunDetail(ctx context.Context, orgID, runID uuid.UUID) (*domain.BackupRunWithStats, error)
	ListRuns(ctx context.Context, orgID uuid.UUID, filter domain.RunFilter) ([]*domain.BackupRunWithStats, error)
	GetLatestRunForJob(ctx context.Context, orgID, jobID uuid.UUID) (*domain.BackupRun, error)
	UpdateHeartbeat(ctx context.Context, orgID, runID uuid.UUID) error
	FinalizeRunAndJob(
		ctx context.Context,
		orgID, runID, jobID uuid.UUID,
		runStatus domain.RunStatus,
		jobStatus domain.JobStatus,
		errMsg *string,
		logsSummary []byte,
	) error

	// Backup Artifacts
	CreateArtifact(ctx context.Context, artifact *domain.BackupArtifact) (*domain.BackupArtifact, error)
	GetArtifactByID(ctx context.Context, orgID, artifactID uuid.UUID) (*domain.BackupArtifact, error)
	ListArtifacts(ctx context.Context, orgID uuid.UUID) ([]*domain.BackupArtifact, error)
	UpdateArtifactVerification(ctx context.Context, orgID, artifactID uuid.UUID, status domain.VerificationStatus, details *string) error
	TombstoneArtifact(ctx context.Context, orgID, artifactID uuid.UUID) error
	GetRunArtifacts(ctx context.Context, orgID, runID uuid.UUID) ([]*domain.BackupArtifact, error)

	// Lifecycle Recovery & Reaper
	RecoverInterruptedRuns(ctx context.Context) (int, error)
	ReapStaleRuns(ctx context.Context) (int, error)
}
