package domain

import (
	"time"

	"backup-platform/pkg/uuid"
)

// StorageTargetType represents the physical storage backend type.
type StorageTargetType string

const (
	StorageTargetTypeLocal        StorageTargetType = "local"
	StorageTargetTypeS3           StorageTargetType = "s3"
	StorageTargetTypeS3Compatible StorageTargetType = "s3_compatible"
	StorageTargetTypeRemoteSSH    StorageTargetType = "remote_ssh"
)

// StorageTargetStatus represents the availability state of a storage target.
type StorageTargetStatus string

const (
	StorageTargetStatusActive   StorageTargetStatus = "active"
	StorageTargetStatusDisabled StorageTargetStatus = "disabled"
	StorageTargetStatusError    StorageTargetStatus = "error"
)

// StorageTarget represents a physical or cloud destination for backup artifacts.
type StorageTarget struct {
	ID             uuid.UUID
	OrganizationID uuid.UUID
	Name           string
	Type           StorageTargetType
	Status         StorageTargetStatus
	IsDefault      bool
	CredentialID   *uuid.UUID
	Config         []byte
	CreatedAt      time.Time
	UpdatedAt      time.Time
}
