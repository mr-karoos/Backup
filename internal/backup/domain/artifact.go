package domain

import (
	"fmt"
	"path"
	"strings"
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
	ArtifactFormatSQLGzip        ArtifactFormat = "sql_gzip"
	ArtifactFormatTarGzip        ArtifactFormat = "tar_gzip"
	ArtifactFormatResticSnapshot ArtifactFormat = "restic_snapshot"
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

// BackupArtifact represents a physical backup file or Restic repository snapshot metadata record.
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
	RepositoryID        *uuid.UUID
	SnapshotID          string
	LogicalSizeBytes    *int64
	IsDeleted           bool
	DeletedAt           *time.Time
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

// SafeArtifactFilenameWithType generates a deterministic, safe, logical filename for a backup artifact with type awareness.
func SafeArtifactFilenameWithType(targetName string, format ArtifactFormat, artType ArtifactType, artifactID uuid.UUID) string {
	ext := ".bin"
	switch format {
	case ArtifactFormatSQLGzip:
		ext = ".sql.gz"
	case ArtifactFormatTarGzip:
		ext = ".tar.gz"
	case ArtifactFormatResticSnapshot:
		if artType == ArtifactTypeDatabaseDump || strings.HasSuffix(targetName, ".sql") {
			ext = ".sql.gz"
		} else {
			ext = ".tar.gz"
		}
	}

	clean := strings.TrimSpace(targetName)
	clean = strings.ReplaceAll(clean, "\\", "/")
	clean = path.Base(clean)
	clean = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.' {
			return r
		}
		return '_'
	}, clean)

	clean = strings.Trim(clean, "._-")
	if clean == "" {
		return fmt.Sprintf("backup_%s%s", artifactID.String(), ext)
	}

	if strings.HasSuffix(clean, ext) {
		return clean
	}
	return fmt.Sprintf("%s%s", clean, ext)
}

// SafeArtifactFilename generates a deterministic, safe, logical filename for a backup artifact.
func SafeArtifactFilename(targetName string, format ArtifactFormat, artifactID uuid.UUID) string {
	artType := ArtifactTypeFilesArchive
	if format == ArtifactFormatSQLGzip || strings.HasSuffix(targetName, ".sql") {
		artType = ArtifactTypeDatabaseDump
	}
	return SafeArtifactFilenameWithType(targetName, format, artType, artifactID)
}
