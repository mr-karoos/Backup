package connector

import (
	"errors"
	"sync"

	resDomain "backup-platform/internal/resource/domain"
)

var (
	// ErrNoDiscovererRegistered indicates that no database discoverer is registered for the specified resource type.
	ErrNoDiscovererRegistered = errors.New("no database discoverer registered for resource type")
)

// DiscoveryRegistry maps resource types to their respective DatabaseDiscoverer implementations.
type DiscoveryRegistry struct {
	mu          sync.RWMutex
	discoverers map[resDomain.Type]DatabaseDiscoverer
}

// NewDiscoveryRegistry instantiates an empty database discovery registry.
func NewDiscoveryRegistry() *DiscoveryRegistry {
	return &DiscoveryRegistry{
		discoverers: make(map[resDomain.Type]DatabaseDiscoverer),
	}
}

// Register assigns a DatabaseDiscoverer to a specific resource type.
func (r *DiscoveryRegistry) Register(resType resDomain.Type, discoverer DatabaseDiscoverer) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.discoverers[resType] = discoverer
}

// Get retrieves the DatabaseDiscoverer for the given resource type.
func (r *DiscoveryRegistry) Get(resType resDomain.Type) (DatabaseDiscoverer, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	discoverer, ok := r.discoverers[resType]
	if !ok || discoverer == nil {
		return nil, ErrNoDiscovererRegistered
	}

	return discoverer, nil
}
