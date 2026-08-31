package worker

import (
	"context"
	"sync"
	"time"

	"backup-platform/pkg/uuid"
)

// PerResourceMutexManager manages in-process exclusive execution locks per resource.
type PerResourceMutexManager struct {
	mu     sync.Mutex
	locked map[uuid.UUID]bool
}

// NewPerResourceMutexManager constructs a new PerResourceMutexManager.
func NewPerResourceMutexManager() *PerResourceMutexManager {
	return &PerResourceMutexManager{
		locked: make(map[uuid.UUID]bool),
	}
}

// TryAcquire attempts to acquire an exclusive lock for the given resourceID.
// If acquired, it returns a non-nil unlock function and true. If the resource is already locked,
// it returns nil and false immediately without blocking.
func (m *PerResourceMutexManager) TryAcquire(resourceID uuid.UUID) (func(), bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.locked[resourceID] {
		return nil, false
	}

	m.locked[resourceID] = true

	var once sync.Once
	unlock := func() {
		once.Do(func() {
			m.mu.Lock()
			defer m.mu.Unlock()
			delete(m.locked, resourceID)
		})
	}

	return unlock, true
}

// Acquire blocks until the lock for resourceID is acquired or ctx is cancelled.
func (m *PerResourceMutexManager) Acquire(ctx context.Context, resourceID uuid.UUID) (func(), error) {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		if unlock, ok := m.TryAcquire(resourceID); ok {
			return unlock, nil
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

// IsLocked returns whether the given resourceID is currently locked.
func (m *PerResourceMutexManager) IsLocked(resourceID uuid.UUID) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.locked[resourceID]
}
