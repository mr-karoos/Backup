package connector

import (
	"context"
	"io"
	"sync"

	"backup-platform/internal/credential/payload"
	resDomain "backup-platform/internal/resource/domain"
)

// FileBackupConfig defines the workload configuration for archiving a single file source path.
type FileBackupConfig struct {
	SourcePath      string
	ExcludePatterns []string
}

// FileBackupCapability defines the capability interface for extracting file archives from target resources.
type FileBackupCapability interface {
	BackupFiles(
		ctx context.Context,
		target Target,
		credPayload *payload.PayloadV1,
		config FileBackupConfig,
		dest io.Writer,
	) error
}

// FileBackupCapabilityRegistry manages registered file backup capabilities by resource type.
type FileBackupCapabilityRegistry struct {
	mu           sync.RWMutex
	capabilities map[resDomain.Type]FileBackupCapability
}

// NewFileBackupCapabilityRegistry initializes an empty file backup capability registry.
func NewFileBackupCapabilityRegistry() *FileBackupCapabilityRegistry {
	return &FileBackupCapabilityRegistry{
		capabilities: make(map[resDomain.Type]FileBackupCapability),
	}
}

// Register binds a resource type to its file backup capability implementation.
func (r *FileBackupCapabilityRegistry) Register(resType resDomain.Type, cap FileBackupCapability) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.capabilities[resType] = cap
}

// Get resolves the file backup capability for the given resource type.
func (r *FileBackupCapabilityRegistry) Get(resType resDomain.Type) (FileBackupCapability, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	cap, ok := r.capabilities[resType]
	return cap, ok
}
