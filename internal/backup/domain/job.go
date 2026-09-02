package domain

import (
	"time"

	"backup-platform/pkg/uuid"
)

// JobStatus represents the lifecycle state of a logical backup job.
type JobStatus string

const (
	JobStatusPending   JobStatus = "pending"
	JobStatusRunning   JobStatus = "running"
	JobStatusCompleted JobStatus = "completed"
	JobStatusFailed    JobStatus = "failed"
	JobStatusCancelled JobStatus = "cancelled"
)

// TriggerType indicates how the backup job was created.
type TriggerType string

const (
	TriggerTypeManual    TriggerType = "manual"
	TriggerTypeScheduled TriggerType = "scheduled"
)

// BackupJob represents a logical backup execution request.
type BackupJob struct {
	ID              uuid.UUID
	OrganizationID  uuid.UUID
	ResourceID      uuid.UUID
	BackupPlanID    *uuid.UUID
	TriggerType     TriggerType
	CreatedByUserID *uuid.UUID
	BackupType      BackupType
	EngineType      EngineType
	StorageTargetID uuid.UUID
	TargetSpec      TargetSpec
	Status          JobStatus
	CreatedAt       time.Time
	UpdatedAt       time.Time
}
