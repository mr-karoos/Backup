package domain

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
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

// Operational defaults and bounds for deep checks
const (
	DefaultMaintenanceDeepCheckSubsets = 4
	MaxMaintenanceDeepCheckSubsets     = 100
)

// MaintenancePhase markers
const (
	MaintenancePhaseForgetExecuted = "forget_executed"
)

// MaintenanceRunStatus defines the status of an execution attempt for a maintenance job.
type MaintenanceRunStatus string

const (
	MaintenanceRunPending   MaintenanceRunStatus = "pending"
	MaintenanceRunRunning   MaintenanceRunStatus = "running"
	MaintenanceRunCompleted MaintenanceRunStatus = "completed"
	MaintenanceRunSuccess   MaintenanceRunStatus = "completed" // Alias for backwards compatibility
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
	AttemptCount   int
	MaxAttempts    int
	NextAttemptAt  time.Time
	Phase          *string
	CompletedAt    *time.Time
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

// RecoveredMaintenanceRunInfo contains metadata for an interrupted or stale maintenance run recovered by the system.
type RecoveredMaintenanceRunInfo struct {
	ID             uuid.UUID
	OrganizationID uuid.UUID
	JobID          uuid.UUID
	AttemptNumber  int
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
		if *p.SubsetTotal > MaxMaintenanceDeepCheckSubsets {
			return fmt.Errorf("subset_total cannot exceed %d", MaxMaintenanceDeepCheckSubsets)
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

// MaintenanceJobFilter defines filter criteria and cursor parameters for listing maintenance jobs.
type MaintenanceJobFilter struct {
	RepositoryID    *uuid.UUID
	Status          *MaintenanceJobStatus
	OperationType   *MaintenanceOperationType
	Limit           int
	CursorCreatedAt *time.Time
	CursorID        *uuid.UUID
}

// MaintenanceJobListResult wraps a paginated slice of maintenance jobs with pagination metadata.
type MaintenanceJobListResult struct {
	Jobs       []*MaintenanceJob
	NextCursor *string
	HasMore    bool
}

// MaintenanceJobDetail represents a maintenance job along with its execution run history.
type MaintenanceJobDetail struct {
	Job  *MaintenanceJob
	Runs []*MaintenanceRun
}

// EncodeMaintenanceCursor serializes created_at and id into a URL-safe base64 opaque cursor string.
func EncodeMaintenanceCursor(t time.Time, id uuid.UUID) string {
	raw := fmt.Sprintf("%s,%s", t.UTC().Format(time.RFC3339Nano), id.String())
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

// DecodeMaintenanceCursor parses a URL-safe base64 opaque cursor string into created_at and id.
func DecodeMaintenanceCursor(cursor string) (time.Time, uuid.UUID, error) {
	if cursor == "" {
		return time.Time{}, uuid.Nil, fmt.Errorf("cursor is empty")
	}

	decoded, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		decoded, err = base64.URLEncoding.DecodeString(cursor)
		if err != nil {
			decoded, err = base64.StdEncoding.DecodeString(cursor)
			if err != nil {
				return time.Time{}, uuid.Nil, fmt.Errorf("malformed base64 cursor: %w", err)
			}
		}
	}

	parts := strings.Split(string(decoded), ",")
	if len(parts) != 2 {
		return time.Time{}, uuid.Nil, fmt.Errorf("invalid cursor components")
	}

	t, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return time.Time{}, uuid.Nil, fmt.Errorf("invalid cursor timestamp: %w", err)
	}

	id, err := uuid.Parse(parts[1])
	if err != nil || id == uuid.Nil {
		return time.Time{}, uuid.Nil, fmt.Errorf("invalid cursor UUID: %w", err)
	}

	return t, id, nil
}
