package connector

import (
	"context"
	"io"
	"sync"

	"backup-platform/internal/credential/payload"
	resDomain "backup-platform/internal/resource/domain"
)

// DatabaseBackupCapability defines the capability interface for operational MySQL database backup extraction.
type DatabaseBackupCapability interface {
	BackupDatabase(
		ctx context.Context,
		target Target,
		credPayload *payload.PayloadV1,
		databaseName string,
		dest io.Writer,
	) error
}

// BackupCapabilityRegistry manages registered database backup capabilities by resource type.
type BackupCapabilityRegistry struct {
	mu           sync.RWMutex
	capabilities map[resDomain.Type]DatabaseBackupCapability
}

// NewBackupCapabilityRegistry initializes an empty backup capability registry.
func NewBackupCapabilityRegistry() *BackupCapabilityRegistry {
	return &BackupCapabilityRegistry{
		capabilities: make(map[resDomain.Type]DatabaseBackupCapability),
	}
}

// Register binds a resource type to its database backup capability implementation.
func (r *BackupCapabilityRegistry) Register(resType resDomain.Type, cap DatabaseBackupCapability) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.capabilities[resType] = cap
}

// Get resolves the database backup capability for the given resource type.
func (r *BackupCapabilityRegistry) Get(resType resDomain.Type) (DatabaseBackupCapability, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	cap, ok := r.capabilities[resType]
	return cap, ok
}
