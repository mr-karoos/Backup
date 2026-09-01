package domain

import (
	"time"

	"backup-platform/pkg/uuid"
)

// RunStatus represents the state of a physical execution attempt.
type RunStatus string

const (
	RunStatusPending   RunStatus = "pending"
	RunStatusRunning   RunStatus = "running"
	RunStatusSuccess   RunStatus = "success"
	RunStatusFailed    RunStatus = "failed"
	RunStatusCancelled RunStatus = "cancelled"
)

// BackupRun represents an individual physical execution attempt for a BackupJob.
type BackupRun struct {
	ID             uuid.UUID
	OrganizationID uuid.UUID
	JobID          uuid.UUID
	AttemptNumber  int
	Status         RunStatus
	StartedAt      time.Time
	EndedAt        *time.Time
	HeartbeatAt    time.Time
	LeaseUntil     time.Time
	ErrorMessage   *string
	LogsSummary    []byte
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// RunFilter contains filtering criteria for querying backup runs within an organization.
type RunFilter struct {
	ResourceID *uuid.UUID
	JobID      *uuid.UUID
	Status     *RunStatus
	FromDate   *time.Time
	ToDate     *time.Time
}

// BackupRunWithStats represents a backup run combined with its aggregate artifact metrics and target resource ID.
type BackupRunWithStats struct {
	Run                    BackupRun
	ResourceID             uuid.UUID
	TotalArtifactSizeBytes int64
	ArtifactsCount         int
}

// RecoveredRunInfo contains metadata for a backup run transitioned by recovery or reaper.
type RecoveredRunInfo struct {
	ID             uuid.UUID
	OrganizationID uuid.UUID
	JobID          uuid.UUID
	AttemptNumber  int
}
