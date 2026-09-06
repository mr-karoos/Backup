package domain

import (
	"encoding/json"
	"fmt"
	"time"

	"backup-platform/pkg/uuid"
)

// MaintenanceOperationType defines the durable repository maintenance operation.
type MaintenanceOperationType string

const (
	MaintenanceOpResticForget    MaintenanceOperationType = "restic_forget"
	MaintenanceOpResticPrune     MaintenanceOperationType = "restic_prune"
	MaintenanceOpResticDeepCheck MaintenanceOperationType = "restic_deep_check"
)

// MaintenanceJobStatus defines the lifecycle status of a maintenance job.
type MaintenanceJobStatus string

const (
	MaintenanceJobPending   MaintenanceJobStatus = "pending"
	MaintenanceJobRunning   MaintenanceJobStatus = "running"
	MaintenanceJobCompleted MaintenanceJobStatus = "completed"
	MaintenanceJobFailed    MaintenanceJobStatus = "failed"
	MaintenanceJobCancelled MaintenanceJobStatus = "cancelled"
)

// MaintenanceRunStatus defines the status of an execution attempt for a maintenance job.
type MaintenanceRunStatus string

const (
	MaintenanceRunPending   MaintenanceRunStatus = "pending"
	MaintenanceRunRunning   MaintenanceRunStatus = "running"
	MaintenanceRunSuccess   MaintenanceRunStatus = "success"
	MaintenanceRunFailed    MaintenanceRunStatus = "failed"
	MaintenanceRunCancelled MaintenanceRunStatus = "cancelled"
)

// MaintenanceJob represents a queued or executed repository maintenance job.
type MaintenanceJob struct {
	ID             uuid.UUID
	OrganizationID uuid.UUID
	RepositoryID   uuid.UUID
	OperationType  MaintenanceOperationType
	Status         MaintenanceJobStatus
	ArtifactID     *uuid.UUID
	SnapshotID     string
	SubsetIndex    *int
	SubsetTotal    *int
	Metadata       []byte
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// MaintenanceRun represents a single execution attempt of a maintenance job.
type MaintenanceRun struct {
	ID             uuid.UUID
	OrganizationID uuid.UUID
	JobID          uuid.UUID
	AttemptNumber  int
	Status         MaintenanceRunStatus
	StartedAt      time.Time
	EndedAt        *time.Time
	HeartbeatAt    time.Time
	LeaseUntil     time.Time
	ErrorMessage   *string
	LogsSummary    []byte
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// EnqueueMaintenanceJobParams contains parameters for queuing a maintenance job.
type EnqueueMaintenanceJobParams struct {
	OrganizationID uuid.UUID
	RepositoryID   uuid.UUID
	OperationType  MaintenanceOperationType
	ArtifactID     *uuid.UUID
	SnapshotID     string
	SubsetIndex    *int
	SubsetTotal    *int
	Metadata       map[string]interface{}
}

// Validate checks that the enqueue parameters satisfy all domain and polymorphic constraints.
func (p EnqueueMaintenanceJobParams) Validate() error {
	if p.OrganizationID == uuid.Nil {
		return fmt.Errorf("organization_id is required")
	}
	if p.RepositoryID == uuid.Nil {
		return fmt.Errorf("repository_id is required")
	}

	switch p.OperationType {
	case MaintenanceOpResticForget:
		if p.ArtifactID == nil || *p.ArtifactID == uuid.Nil {
			return fmt.Errorf("artifact_id is required for restic_forget")
		}
		if !IsValidCanonicalResticSnapshotID(p.SnapshotID) {
			return fmt.Errorf("snapshot_id must be canonical 64-hex for restic_forget")
		}
		if p.SubsetIndex != nil || p.SubsetTotal != nil {
			return fmt.Errorf("subset parameters must be nil for restic_forget")
		}

	case MaintenanceOpResticPrune:
		if p.ArtifactID != nil {
			return fmt.Errorf("artifact_id must be nil for restic_prune")
		}
		if p.SnapshotID != "" {
			return fmt.Errorf("snapshot_id must be empty for restic_prune")
		}
		if p.SubsetIndex != nil || p.SubsetTotal != nil {
			return fmt.Errorf("subset parameters must be nil for restic_prune")
		}

	case MaintenanceOpResticDeepCheck:
		if p.ArtifactID != nil {
			return fmt.Errorf("artifact_id must be nil for restic_deep_check")
		}
		if p.SnapshotID != "" {
			return fmt.Errorf("snapshot_id must be empty for restic_deep_check")
		}
		if p.SubsetIndex == nil || p.SubsetTotal == nil {
			return fmt.Errorf("subset_index and subset_total are required for restic_deep_check")
		}
		if *p.SubsetIndex < 1 {
			return fmt.Errorf("subset_index must be >= 1")
		}
		if *p.SubsetTotal < *p.SubsetIndex {
			return fmt.Errorf("subset_total must be >= subset_index")
		}

	default:
		return fmt.Errorf("unsupported maintenance operation_type: %s", p.OperationType)
	}

	return nil
}

// MetadataJSON marshals metadata or returns an empty JSON object.
func (p EnqueueMaintenanceJobParams) MetadataJSON() []byte {
	if p.Metadata == nil {
		return []byte("{}")
	}
	b, err := json.Marshal(p.Metadata)
	if err != nil {
		return []byte("{}")
	}
	return b
}
