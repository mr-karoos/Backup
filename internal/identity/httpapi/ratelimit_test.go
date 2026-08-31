package httpapi

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRateLimiter_IPLimit(t *testing.T) {
	currentTime := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return currentTime }

	limiter := NewRateLimiter(clock)
	ip := "192.168.1.50"

	// 5 attempts should be allowed
	for i := 1; i <= 5; i++ {
		allowed, retryAfter := limiter.AllowAndRecord(ip, "user@example.com")
		if !allowed {
			t.Fatalf("attempt %d should be allowed, got blocked with retryAfter: %v", i, retryAfter)
		}
	}

	// 6th attempt within same minute should be blocked
	allowed, retryAfter := limiter.AllowAndRecord(ip, "user@example.com")
	if allowed {
		t.Fatal("6th attempt should be rate limited")
	}
	if retryAfter <= 0 {
		t.Errorf("expected positive retryAfter duration, got: %v", retryAfter)
	}

	// Advance time by 61 seconds (past 1-minute window)
	currentTime = currentTime.Add(61 * time.Second)

	// Next attempt should now be allowed
	allowed, _ = limiter.AllowAndRecord(ip, "user@example.com")
	if !allowed {
		t.Fatal("attempt after window expiry should be allowed")
	}
}

func TestRateLimiter_PairDimension(t *testing.T) {
	currentTime := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return currentTime }

	limiter := NewRateLimiter(clock)
	ip := "192.168.1.100"
	email := "target-pair@example.com"

	for i := 1; i <= 5; i++ {
		allowed, _ := limiter.AllowAndRecord(ip, email)
		if !allowed {
			t.Fatalf("attempt %d should be allowed", i)
		}
	}

	// 6th attempt for the exact (IP, email) pair should be rate limited
	allowed, retryAfter := limiter.AllowAndRecord(ip, email)
	if allowed {
		t.Fatal("6th attempt for (IP, email) pair should be rate limited")
	}
	if retryAfter <= 0 {
		t.Errorf("expected positive retryAfter, got: %v", retryAfter)
	}
}

func TestRateLimiter_EmailDimension(t *testing.T) {
	currentTime := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return currentTime }

	limiter := NewRateLimiter(clock)
	targetEmail := "target@example.com"

	// 10 attempts distributed across 10 distinct IPs targeting the same email
	for i := 1; i <= 10; i++ {
		ip := fmt.Sprintf("10.0.0.%d", i)
		allowed, _ := limiter.AllowAndRecord(ip, targetEmail)
		if !allowed {
			t.Fatalf("attempt %d should be allowed under email limit 10", i)
		}
	}

	// 11th attempt from an 11th IP against the same email should be blocked
	allowed, retryAfter := limiter.AllowAndRecord("10.0.0.11", targetEmail)
	if allowed {
		t.Fatal("11th attempt against same target email should be blocked by email rate limit")
	}
	if retryAfter <= 0 {
		t.Errorf("expected positive retryAfter, got: %v", retryAfter)
	}
}

func TestRateLimiter_HardStoreCapacity(t *testing.T) {
	currentTime := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return currentTime }

	limiter := NewRateLimiter(clock)

	// Pre-fill ipStore up to MaxStoreCapacity
	for i := 0; i < MaxStoreCapacity; i++ {
		fakeIP := fmt.Sprintf("ip-%d", i)
		limiter.ipStore[fakeIP] = &attemptRecord{timestamps: []time.Time{currentTime}}
	}

	// Any new key when store is at MaxStoreCapacity and active should fail-closed
	allowed, retryAfter := limiter.AllowAndRecord("new-ip-beyond-cap", "someone@example.com")
	if allowed {
		t.Fatal("expected new key to fail-closed when store capacity is saturated")
	}
	if retryAfter <= 0 {
		t.Errorf("expected positive retryAfter, got: %v", retryAfter)
	}

	// Existing keys should still be allowed if within their quota
	allowed, _ = limiter.AllowAndRecord("ip-0", "")
	if !allowed {
		t.Errorf("existing tracked key should still be allowed within quota")
	}

	// Advance time past window so entries expire
	currentTime = currentTime.Add(2 * time.Minute)

	// Now new key should trigger cleanup and succeed
	allowed, _ = limiter.AllowAndRecord("brand-new-ip-after-expiry", "")
	if !allowed {
		t.Fatalf("new key after window expiry should trigger cleanup and succeed")
	}
}

func TestRateLimiter_CleanupExpired(t *testing.T) {
	currentTime := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return currentTime }

	limiter := NewRateLimiter(clock)
	limiter.AllowAndRecord("192.168.1.1", "a@example.com")

	if len(limiter.ipStore) != 1 || len(limiter.emailStore) != 1 || len(limiter.pairStore) != 1 {
		t.Fatalf("expected stores to contain 1 entry")
	}

	// Advance time past window
	currentTime = currentTime.Add(2 * time.Minute)
	limiter.CleanupExpired(currentTime)

	if len(limiter.ipStore) != 0 || len(limiter.emailStore) != 0 || len(limiter.pairStore) != 0 {
		t.Errorf("expected stores to be empty after cleanup of expired entries")
	}
}

func TestRateLimiter_ConcurrentSafety(t *testing.T) {
	limiter := NewRateLimiter(nil)
	var wg sync.WaitGroup
	workers := 20
	attemptsPerWorker := 50

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			ip := fmt.Sprintf("192.168.100.%d", workerID)
			email := fmt.Sprintf("user%d@example.com", workerID)
			for j := 0; j < attemptsPerWorker; j++ {
				_, _ = limiter.AllowAndRecord(ip, email)
			}
		}(i)
	}

	wg.Wait()
}

func TestRateLimiter_FixedSizeHashedKeys(t *testing.T) {
	limiter := NewRateLimiter(nil)
	longEmail := strings.Repeat("testuser", 20) + "@example.com"

	limiter.AllowAndRecord("10.0.0.1", longEmail)

	if len(limiter.emailStore) != 1 {
		t.Fatalf("expected exactly 1 entry in emailStore")
	}

	// Verify key type is [32]byte
	for key := range limiter.emailStore {
		if len(key) != 32 {
			t.Errorf("expected 32-byte fixed-size hash key, got: %d", len(key))
		}
	}

	for pairKey := range limiter.pairStore {
		if pairKey.IP != "10.0.0.1" {
			t.Errorf("expected IP match in pair key")
		}
		if len(pairKey.EmailHash) != 32 {
			t.Errorf("expected 32-byte fixed-size hash in pair key, got: %d", len(pairKey.EmailHash))
		}
	}
}
