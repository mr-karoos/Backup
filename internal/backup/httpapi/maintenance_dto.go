package httpapi

import (
	"strings"
	"time"
	"unicode/utf8"

	"backup-platform/internal/backup/domain"
	"backup-platform/pkg/uuid"
)

// MaintenanceJobDTO represents the safe, public representation of a repository maintenance job.
type MaintenanceJobDTO struct {
	ID            uuid.UUID  `json:"id"`
	RepositoryID  uuid.UUID  `json:"repository_id"`
	OperationType string     `json:"operation_type"`
	Status        string     `json:"status"`
	ArtifactID    *uuid.UUID `json:"artifact_id,omitempty"`
	SnapshotID    *string    `json:"snapshot_id,omitempty"`
	SubsetIndex   *int       `json:"subset_index,omitempty"`
	SubsetTotal   *int       `json:"subset_total,omitempty"`
	AttemptCount  int        `json:"attempt_count"`
	MaxAttempts   int        `json:"max_attempts"`
	NextAttemptAt *time.Time `json:"next_attempt_at,omitempty"`
	Phase         *string    `json:"phase,omitempty"`
	CompletedAt   *time.Time `json:"completed_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

// MaintenanceRunSummaryDTO represents the safe, redacted summary of an execution attempt for a maintenance job.
type MaintenanceRunSummaryDTO struct {
	ID            uuid.UUID  `json:"id"`
	AttemptNumber int        `json:"attempt_number"`
	Status        string     `json:"status"`
	StartedAt     time.Time  `json:"started_at"`
	EndedAt       *time.Time `json:"ended_at,omitempty"`
	DurationMS    *int64     `json:"duration_ms,omitempty"`
	ErrorSummary  *string    `json:"error_summary,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
}

// MaintenanceJobDetailDTO provides a detailed view of a maintenance job along with its execution runs.
type MaintenanceJobDetailDTO struct {
	MaintenanceJobDTO
	Runs []MaintenanceRunSummaryDTO `json:"runs"`
}

// PaginationMeta encapsulates keyset cursor pagination metadata.
type PaginationMeta struct {
	NextCursor *string `json:"next_cursor"`
	HasMore    bool    `json:"has_more"`
}

// MaintenanceJobListResponse represents the top-level response envelope for paginated maintenance jobs.
type MaintenanceJobListResponse struct {
	Data []MaintenanceJobDTO `json:"data"`
	Page PaginationMeta      `json:"page"`
}

// ToMaintenanceJobDTO transforms a domain MaintenanceJob into a safe public DTO, hiding internal and sensitive fields.
func ToMaintenanceJobDTO(job *domain.MaintenanceJob) MaintenanceJobDTO {
	if job == nil {
		return MaintenanceJobDTO{}
	}

	dto := MaintenanceJobDTO{
		ID:            job.ID,
		RepositoryID:  job.RepositoryID,
		OperationType: string(job.OperationType),
		Status:        string(job.Status),
		ArtifactID:    job.ArtifactID,
		SubsetIndex:   job.SubsetIndex,
		SubsetTotal:   job.SubsetTotal,
		AttemptCount:  job.AttemptCount,
		MaxAttempts:   job.MaxAttempts,
		Phase:         job.Phase,
		CompletedAt:   job.CompletedAt,
		CreatedAt:     job.CreatedAt,
		UpdatedAt:     job.UpdatedAt,
	}

	if job.SnapshotID != "" {
		snapshotID := job.SnapshotID
		dto.SnapshotID = &snapshotID
	}

	if !job.NextAttemptAt.IsZero() {
		nextAt := job.NextAttemptAt
		dto.NextAttemptAt = &nextAt
	}

	return dto
}

// ToMaintenanceRunSummaryDTO transforms a domain MaintenanceRun into a safe public summary DTO, redacting secrets and logs.
func ToMaintenanceRunSummaryDTO(run *domain.MaintenanceRun) MaintenanceRunSummaryDTO {
	if run == nil {
		return MaintenanceRunSummaryDTO{}
	}

	dto := MaintenanceRunSummaryDTO{
		ID:            run.ID,
		AttemptNumber: run.AttemptNumber,
		Status:        string(run.Status),
		StartedAt:     run.StartedAt,
		EndedAt:       run.EndedAt,
		CreatedAt:     run.CreatedAt,
	}

	if run.EndedAt != nil {
		dur := run.EndedAt.Sub(run.StartedAt).Milliseconds()
		if dur < 0 {
			dur = 0
		}
		dto.DurationMS = &dur
	}

	if run.ErrorMessage != nil && strings.TrimSpace(*run.ErrorMessage) != "" {
		sanitized := sanitizeErrorSummary(*run.ErrorMessage)
		dto.ErrorSummary = &sanitized
	}

	return dto
}

// sanitizeErrorSummary ensures error messages do not leak internal stack traces, passwords or paths and are bounded to 1024 bytes UTF-8.
func sanitizeErrorSummary(msg string) string {
	clean := strings.TrimSpace(msg)
	const maxSummaryLen = 1024
	const truncateSuffix = "... [truncated]"
	if len(clean) <= maxSummaryLen {
		return clean
	}

	budget := maxSummaryLen - len(truncateSuffix)
	cut := budget
	for cut > 0 && !utf8.RuneStart(clean[cut]) {
		cut--
	}

	return clean[:cut] + truncateSuffix
}
