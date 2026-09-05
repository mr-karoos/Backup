package restic

import (
	"context"
	"errors"
	"sync"
	"time"

	"backup-platform/pkg/uuid"
)

// RepositoryOperationCoordinator coordinates concurrent access to dedicated Restic repositories (ADR-035).
// Shared access is required for backup, download/dump, and Level-1 verification.
// Exclusive access is required for maintenance operations (forget, prune, check, key rotation).
type RepositoryOperationCoordinator interface {
	AcquireShared(ctx context.Context, repoID uuid.UUID) (func(), error)
	AcquireExclusive(ctx context.Context, repoID uuid.UUID) (func(), error)
}

// InProcessRepositoryOperationCoordinator implements RepositoryOperationCoordinator using in-process RWMutexes.
type InProcessRepositoryOperationCoordinator struct {
	mu    sync.Mutex
	locks map[uuid.UUID]*sync.RWMutex
}

// NewRepositoryOperationCoordinator constructs a new InProcessRepositoryOperationCoordinator.
func NewRepositoryOperationCoordinator() *InProcessRepositoryOperationCoordinator {
	return &InProcessRepositoryOperationCoordinator{
		locks: make(map[uuid.UUID]*sync.RWMutex),
	}
}

func (c *InProcessRepositoryOperationCoordinator) getLock(repoID uuid.UUID) *sync.RWMutex {
	c.mu.Lock()
	defer c.mu.Unlock()
	l, exists := c.locks[repoID]
	if !exists {
		l = &sync.RWMutex{}
		c.locks[repoID] = l
	}
	return l
}

// AcquireShared acquires a shared (read) lock on the repository.
// Multiple shared operations (backups, downloads, Level-1 checks) can execute concurrently.
func (c *InProcessRepositoryOperationCoordinator) AcquireShared(ctx context.Context, repoID uuid.UUID) (func(), error) {
	if repoID == uuid.Nil {
		return nil, errors.New("repository ID cannot be nil")
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	l := c.getLock(repoID)

	for {
		if l.TryRLock() {
			var once sync.Once
			return func() {
				once.Do(func() {
					l.RUnlock()
				})
			}, nil
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Millisecond):
		}
	}
}

// AcquireExclusive acquires an exclusive (write) lock on the repository.
// Blocks until all shared operations have completed.
func (c *InProcessRepositoryOperationCoordinator) AcquireExclusive(ctx context.Context, repoID uuid.UUID) (func(), error) {
	if repoID == uuid.Nil {
		return nil, errors.New("repository ID cannot be nil")
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	l := c.getLock(repoID)

	for {
		if l.TryLock() {
			var once sync.Once
			return func() {
				once.Do(func() {
					l.Unlock()
				})
			}, nil
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Millisecond):
		}
	}
}
