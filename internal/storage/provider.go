package storage

import (
	"context"
	"errors"
	"io"

	"backup-platform/pkg/uuid"
)

var (
	// ErrStorageFull indicates that the storage medium is out of disk space (ENOSPC).
	ErrStorageFull = errors.New("storage device has no space left")

	// ErrStorageIO indicates a generic temporary filesystem or storage I/O failure.
	ErrStorageIO = errors.New("storage I/O error")

	// ErrArtifactCollision indicates that an artifact already exists at the destination path.
	ErrArtifactCollision = errors.New("artifact destination already exists")

	// ErrInvalidStorageReference indicates a malformed or unsafe storage reference.
	ErrInvalidStorageReference = errors.New("invalid storage reference")

	// ErrArtifactNotFound indicates that the referenced artifact does not exist.
	ErrArtifactNotFound = errors.New("artifact not found")
)

// SaveResult contains metadata describing a newly stored artifact.
type SaveResult struct {
	StorageReference string
	SizeBytes        int64
	ChecksumSHA256   string
}

// StorageProvider abstracts physical artifact storage and retrieval operations.
type StorageProvider interface {
	SaveArtifact(ctx context.Context, orgID, resID, runID, artifactID uuid.UUID, extension string, src io.Reader) (*SaveResult, error)
	OpenArtifact(ctx context.Context, storageReference string) (io.ReadCloser, error)
	DeleteArtifact(ctx context.Context, storageReference string) error
	EnsureStorageRoot(ctx context.Context) error
}

// TemporaryArtifactCleaner is an optional interface implemented by storage providers
// that support cleaning orphan platform-generated temporary files upon startup.
type TemporaryArtifactCleaner interface {
	CleanOrphanTemporaryArtifacts(ctx context.Context) (int, error)
}
