package domain

import (
	"time"

	"backup-platform/pkg/uuid"
)

// ArtifactType represents the logical category of a backup artifact.
type ArtifactType string

const (
	ArtifactTypeDatabaseDump ArtifactType = "database_dump"
	ArtifactTypeFilesArchive ArtifactType = "files_archive"
)

// ArtifactFormat represents the packaging format of a backup artifact.
type ArtifactFormat string

const (
	ArtifactFormatSQLGzip ArtifactFormat = "sql_gzip"
	ArtifactFormatTarGzip ArtifactFormat = "tar_gzip"
)

// VerificationStatus represents the integrity verification state of an artifact.
type VerificationStatus string

const (
	VerificationStatusUnverified VerificationStatus = "unverified"
	VerificationStatusVerified   VerificationStatus = "verified"
	VerificationStatusFailed     VerificationStatus = "failed"
)

// ChecksumAlgorithm represents the cryptographic hashing algorithm used.
type ChecksumAlgorithm string

const (
	ChecksumAlgorithmSHA256 ChecksumAlgorithm = "sha256"
)

// BackupArtifact represents a physical backup file metadata record.
type BackupArtifact struct {
	ID                  uuid.UUID
	OrganizationID      uuid.UUID
	RunID               uuid.UUID
	ResourceID          uuid.UUID
	StorageTargetID     uuid.UUID
	ArtifactType        ArtifactType
	Format              ArtifactFormat
	TargetName          string
	StorageReference    string
	SizeBytes           int64
	ChecksumAlgorithm   ChecksumAlgorithm
	ChecksumHash        string
	VerificationStatus  VerificationStatus
	VerifiedAt          *time.Time
	VerificationDetails *string
	StoredSizeBytes     *int64
	EngineMetadata      []byte
	IsDeleted           bool
	DeletedAt           *time.Time
	CreatedAt           time.Time
	UpdatedAt           time.Time
}
