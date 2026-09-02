package domain

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"backup-platform/pkg/uuid"
)

// PlanStatus represents the operational status of a backup plan.
type PlanStatus string

const (
	PlanStatusActive   PlanStatus = "active"
	PlanStatusPaused   PlanStatus = "paused"
	PlanStatusArchived PlanStatus = "archived"
)

// BackupType represents the target workload type of a backup.
type BackupType string

const (
	BackupTypeMySQLDatabase BackupType = "mysql_database"
	BackupTypeWebsiteFiles  BackupType = "website_files"
	BackupTypeBoth          BackupType = "both"
)

// EngineType represents the concrete backup engine used for artifact generation.
type EngineType string

const (
	EngineTypeDirectStream EngineType = "direct_stream"
)

// IsValid checks whether the engine type is supported in this release.
func (e EngineType) IsValid() bool {
	return e == EngineTypeDirectStream
}

// ValidateEngineType verifies that the engine type is supported.
func ValidateEngineType(e EngineType) error {
	if !e.IsValid() {
		return ErrUnsupportedEngineType
	}
	return nil
}

// BackupPlan represents a scheduled or manual policy for resource backups.
type BackupPlan struct {
	ID                uuid.UUID
	OrganizationID    uuid.UUID
	ResourceID        uuid.UUID
	Name              string
	BackupType        BackupType
	EngineType        EngineType
	StorageTargetID   uuid.UUID
	TargetSpec        TargetSpec
	ScheduleCron      *string
	ScheduleTimezone  string
	IsScheduleEnabled bool
	RetentionCount    *int
	RetentionDays     *int
	Status            PlanStatus
	NextRunAt         *time.Time
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// BackupPlanWithResource wraps a BackupPlan with its joined Resource name.
type BackupPlanWithResource struct {
	Plan         BackupPlan
	ResourceName string
}

// PlanFilter defines filtering criteria when listing backup plans for an organization.
type PlanFilter struct {
	ResourceID *uuid.UUID
	Status     *PlanStatus
}

const (
	// MaxPlanNameRunes defines the maximum length of a plan name in Unicode runes.
	MaxPlanNameRunes = 100
)

// ValidatePlanName validates and returns the trimmed plan name.
func ValidatePlanName(name string) (string, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return "", errors.New("backup plan name cannot be empty")
	}

	if !utf8.ValidString(trimmed) {
		return "", errors.New("backup plan name contains invalid UTF-8 characters")
	}

	for i := 0; i < len(trimmed); i++ {
		c := trimmed[i]
		if c < 32 || c == 127 {
			return "", errors.New("backup plan name contains control characters or NUL byte")
		}
	}

	if utf8.RuneCountInString(trimmed) > MaxPlanNameRunes {
		return "", errors.New("backup plan name exceeds maximum length of 100 characters")
	}

	return trimmed, nil
}

// ValidateRetentionPolicy validates that retention settings, if provided, are strictly positive integers (>= 1).
func ValidateRetentionPolicy(retentionCount, retentionDays *int) error {
	if retentionCount != nil && *retentionCount <= 0 {
		return errors.New("retention count must be greater than 0")
	}
	if retentionDays != nil && *retentionDays <= 0 {
		return errors.New("retention days must be greater than 0")
	}
	return nil
}
