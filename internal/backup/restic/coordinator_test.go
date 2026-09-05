package restic

import (
	"context"
	"sync"
	"testing"
	"time"

	"backup-platform/pkg/uuid"
)

func TestRepositoryOperationCoordinator_SharedConcurrency(t *testing.T) {
	coord := NewRepositoryOperationCoordinator()
	repoID := uuid.New()
	ctx := context.Background()

	release1, err := coord.AcquireShared(ctx, repoID)
	if err != nil {
		t.Fatalf("unexpected error acquiring shared lock 1: %v", err)
	}
	defer release1()

	release2, err := coord.AcquireShared(ctx, repoID)
	if err != nil {
		t.Fatalf("unexpected error acquiring shared lock 2: %v", err)
	}
	defer release2()

	// Both shared locks held simultaneously
}

func TestRepositoryOperationCoordinator_ExclusiveExclusion(t *testing.T) {
	coord := NewRepositoryOperationCoordinator()
	repoID := uuid.New()
	ctx := context.Background()

	releaseShared, err := coord.AcquireShared(ctx, repoID)
	if err != nil {
		t.Fatalf("unexpected error acquiring shared: %v", err)
	}

	// Try acquiring exclusive with short timeout - must fail
	timeoutCtx, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
	defer cancel()

	_, err = coord.AcquireExclusive(timeoutCtx, repoID)
	if err == nil {
		t.Fatalf("expected exclusive acquire to fail while shared is held")
	}

	// Release shared
	releaseShared()

	// Now exclusive should succeed
	releaseExclusive, err := coord.AcquireExclusive(ctx, repoID)
	if err != nil {
		t.Fatalf("unexpected error acquiring exclusive after shared release: %v", err)
	}
	defer releaseExclusive()
}

func TestRepositoryOperationCoordinator_DistinctRepositoriesDoNotBlock(t *testing.T) {
	coord := NewRepositoryOperationCoordinator()
	repoA := uuid.New()
	repoB := uuid.New()
	ctx := context.Background()

	releaseExA, err := coord.AcquireExclusive(ctx, repoA)
	if err != nil {
		t.Fatalf("unexpected error acquiring exclusive on repoA: %v", err)
	}
	defer releaseExA()

	// repoB shared and exclusive should both succeed without blocking
	releaseExB, err := coord.AcquireExclusive(ctx, repoB)
	if err != nil {
		t.Fatalf("unexpected error acquiring exclusive on repoB: %v", err)
	}
	defer releaseExB()
}

func TestRepositoryOperationCoordinator_IdempotentRelease(t *testing.T) {
	coord := NewRepositoryOperationCoordinator()
	repoID := uuid.New()
	ctx := context.Background()

	release, err := coord.AcquireShared(ctx, repoID)
	if err != nil {
		t.Fatalf("unexpected error acquiring shared: %v", err)
	}

	// Calling release multiple times must not panic
	release()
	release()
	release()
}

func TestRepositoryOperationCoordinator_NilUUID(t *testing.T) {
	coord := NewRepositoryOperationCoordinator()
	ctx := context.Background()

	_, err := coord.AcquireShared(ctx, uuid.Nil)
	if err == nil {
		t.Fatal("expected error for nil uuid in AcquireShared")
	}

	_, err = coord.AcquireExclusive(ctx, uuid.Nil)
	if err == nil {
		t.Fatal("expected error for nil uuid in AcquireExclusive")
	}
}

func TestRepositoryOperationCoordinator_ContextCancellation(t *testing.T) {
	coord := NewRepositoryOperationCoordinator()
	repoID := uuid.New()

	// Hold exclusive lock
	releaseEx, err := coord.AcquireExclusive(context.Background(), repoID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer releaseEx()

	// Try acquiring shared with cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err = coord.AcquireShared(ctx, repoID)
	if err != context.Canceled {
		t.Fatalf("expected context.Canceled, got: %v", err)
	}
}

func TestRepositoryOperationCoordinator_ConcurrentGoroutines(t *testing.T) {
	coord := NewRepositoryOperationCoordinator()
	repoID := uuid.New()

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rel, err := coord.AcquireShared(context.Background(), repoID)
			if err != nil {
				t.Errorf("concurrent shared acquire failed: %v", err)
				return
			}
			time.Sleep(5 * time.Millisecond)
			rel()
		}()
	}
	wg.Wait()
}
