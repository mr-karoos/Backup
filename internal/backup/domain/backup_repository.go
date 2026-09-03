package domain

import (
	"time"

	"backup-platform/pkg/uuid"
)

// BackupRepositoryStatus represents the operational status of a Restic repository.
type BackupRepositoryStatus string

const (
	BackupRepositoryStatusActive   BackupRepositoryStatus = "active"
	BackupRepositoryStatusDisabled BackupRepositoryStatus = "disabled"
	BackupRepositoryStatusError    BackupRepositoryStatus = "error"
)

// IsValid checks whether the status is valid.
func (s BackupRepositoryStatus) IsValid() bool {
	switch s {
	case BackupRepositoryStatusActive, BackupRepositoryStatusDisabled, BackupRepositoryStatusError:
		return true
	default:
		return false
	}
}

// BackupRepository represents a dedicated per-resource Restic repository entity (ADR-033).
// Hard invariant: exactly one canonical repository per Resource.
// Note: Secret passwords and S3 access keys are strictly excluded by design.
type BackupRepository struct {
	ID                uuid.UUID              `json:"id"`
	OrganizationID    uuid.UUID              `json:"organization_id"`
	ResourceID        uuid.UUID              `json:"resource_id"`
	StorageTargetID   uuid.UUID              `json:"storage_target_id"`
	CredentialID      uuid.UUID              `json:"credential_id"`
	RepositoryLocator string                 `json:"repository_locator"`
	Status            BackupRepositoryStatus `json:"status"`
	Metadata          []byte                 `json:"metadata"`
	CreatedAt         time.Time              `json:"created_at"`
	UpdatedAt         time.Time              `json:"updated_at"`
}
