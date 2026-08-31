package connector

import (
	"errors"
	"sync"

	resDomain "backup-platform/internal/resource/domain"
)

var (
	// ErrNoTesterRegistered indicates that no connection tester is registered for the specified resource type.
	ErrNoTesterRegistered = errors.New("no connection tester registered for resource type")
)

// Registry maps resource types to their respective connection tester implementations.
type Registry struct {
	mu      sync.RWMutex
	testers map[resDomain.Type]ConnectionTester
}

// NewRegistry instantiates an empty connector capability registry.
func NewRegistry() *Registry {
	return &Registry{
		testers: make(map[resDomain.Type]ConnectionTester),
	}
}

// Register assigns a connection tester to a specific resource type.
func (r *Registry) Register(resType resDomain.Type, tester ConnectionTester) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.testers[resType] = tester
}

// Get retrieves the connection tester for the given resource type.
func (r *Registry) Get(resType resDomain.Type) (ConnectionTester, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	tester, ok := r.testers[resType]
	if !ok || tester == nil {
		return nil, ErrNoTesterRegistered
	}

	return tester, nil
}
