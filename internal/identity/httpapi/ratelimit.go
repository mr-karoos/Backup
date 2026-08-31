package httpapi

import (
	"crypto/sha256"
	"strings"
	"sync"
	"time"
)

// MaxStoreCapacity bounds the maximum number of unique keys tracked per dimension to prevent memory exhaustion attacks.
const MaxStoreCapacity = 10000

// RateLimitRule configures the threshold for sliding window rate limiting.
type RateLimitRule struct {
	MaxAttempts int
	Window      time.Duration
}

type attemptRecord struct {
	timestamps []time.Time
}

type rateLimitPairKey struct {
	IP        string
	EmailHash [32]byte
}

// RateLimiter provides multi-dimensional, thread-safe, strictly bounded rate limiting using fixed-size hashed keys.
type RateLimiter struct {
	mu         sync.Mutex
	ipRule     RateLimitRule
	pairRule   RateLimitRule
	emailRule  RateLimitRule
	ipStore    map[string]*attemptRecord
	pairStore  map[rateLimitPairKey]*attemptRecord
	emailStore map[[32]byte]*attemptRecord
	nowFunc    func() time.Time
}

// NewRateLimiter constructs a new RateLimiter with canonical V1 thresholds.
func NewRateLimiter(nowFunc func() time.Time) *RateLimiter {
	if nowFunc == nil {
		nowFunc = func() time.Time { return time.Now().UTC() }
	}
	return &RateLimiter{
		ipRule:     RateLimitRule{MaxAttempts: 5, Window: 1 * time.Minute},
		pairRule:   RateLimitRule{MaxAttempts: 5, Window: 1 * time.Minute},
		emailRule:  RateLimitRule{MaxAttempts: 10, Window: 1 * time.Minute},
		ipStore:    make(map[string]*attemptRecord),
		pairStore:  make(map[rateLimitPairKey]*attemptRecord),
		emailStore: make(map[[32]byte]*attemptRecord),
		nowFunc:    nowFunc,
	}
}

// AllowAndRecord checks multi-dimensional rate limits (IP, IP+EmailHash, EmailHash) and records the attempt if allowed.
func (l *RateLimiter) AllowAndRecord(ip, email string) (allowed bool, retryAfter time.Duration) {
	ip = strings.TrimSpace(ip)
	email = strings.ToLower(strings.TrimSpace(email))

	var emailHash [32]byte
	hasEmail := false
	if email != "" {
		emailHash = sha256.Sum256([]byte(email))
		hasEmail = true
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.nowFunc()

	// 1. Evaluate IP dimension
	if ip != "" {
		if rec, exists := l.ipStore[ip]; exists {
			rec.timestamps = pruneOldTimestamps(rec.timestamps, now.Add(-l.ipRule.Window))
			if len(rec.timestamps) >= l.ipRule.MaxAttempts {
				retryAfter := rec.timestamps[0].Add(l.ipRule.Window).Sub(now)
				if retryAfter < time.Second {
					retryAfter = time.Second
				}
				return false, retryAfter
			}
		}
	}

	// 2. Evaluate (IP, EmailHash) pair dimension using typed struct key
	pairKey := rateLimitPairKey{IP: ip, EmailHash: emailHash}
	if ip != "" && hasEmail {
		if rec, exists := l.pairStore[pairKey]; exists {
			rec.timestamps = pruneOldTimestamps(rec.timestamps, now.Add(-l.pairRule.Window))
			if len(rec.timestamps) >= l.pairRule.MaxAttempts {
				retryAfter := rec.timestamps[0].Add(l.pairRule.Window).Sub(now)
				if retryAfter < time.Second {
					retryAfter = time.Second
				}
				return false, retryAfter
			}
		}
	}

	// 3. Evaluate Email account dimension (anti-credential stuffing across IPs)
	if hasEmail {
		if rec, exists := l.emailStore[emailHash]; exists {
			rec.timestamps = pruneOldTimestamps(rec.timestamps, now.Add(-l.emailRule.Window))
			if len(rec.timestamps) >= l.emailRule.MaxAttempts {
				retryAfter := rec.timestamps[0].Add(l.emailRule.Window).Sub(now)
				if retryAfter < time.Second {
					retryAfter = time.Second
				}
				return false, retryAfter
			}
		}
	}

	// 4. Check Hard Store Capacity before inserting any new untracked keys
	_, trackedIP := l.ipStore[ip]
	_, trackedPair := l.pairStore[pairKey]
	_, trackedEmail := l.emailStore[emailHash]

	isNewKey := (ip != "" && !trackedIP) || (ip != "" && hasEmail && !trackedPair) || (hasEmail && !trackedEmail)
	if isNewKey && (len(l.ipStore) >= MaxStoreCapacity || len(l.pairStore) >= MaxStoreCapacity || len(l.emailStore) >= MaxStoreCapacity) {
		l.cleanupExpiredLocked(now)
		// Recheck if still at capacity after full cleanup
		if (ip != "" && !trackedIP && len(l.ipStore) >= MaxStoreCapacity) ||
			(ip != "" && hasEmail && !trackedPair && len(l.pairStore) >= MaxStoreCapacity) ||
			(hasEmail && !trackedEmail && len(l.emailStore) >= MaxStoreCapacity) {
			// Fail-closed when stores hit hard maximum capacity under high-cardinality attacks
			return false, l.ipRule.Window
		}
	}

	// 5. Record attempt across all dimensions
	if ip != "" {
		if _, exists := l.ipStore[ip]; !exists {
			l.ipStore[ip] = &attemptRecord{}
		}
		l.ipStore[ip].timestamps = append(l.ipStore[ip].timestamps, now)
	}

	if ip != "" && hasEmail {
		if _, exists := l.pairStore[pairKey]; !exists {
			l.pairStore[pairKey] = &attemptRecord{}
		}
		l.pairStore[pairKey].timestamps = append(l.pairStore[pairKey].timestamps, now)
	}

	if hasEmail {
		if _, exists := l.emailStore[emailHash]; !exists {
			l.emailStore[emailHash] = &attemptRecord{}
		}
		l.emailStore[emailHash].timestamps = append(l.emailStore[emailHash].timestamps, now)
	}

	// Periodic cleanup trigger
	if len(l.ipStore) > 5000 || len(l.pairStore) > 5000 || len(l.emailStore) > 5000 {
		l.cleanupExpiredLocked(now)
	}

	return true, 0
}

// CleanupExpired purges stale records from memory.
func (l *RateLimiter) CleanupExpired(now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.cleanupExpiredLocked(now)
}

func (l *RateLimiter) cleanupExpiredLocked(now time.Time) {
	for k, v := range l.ipStore {
		v.timestamps = pruneOldTimestamps(v.timestamps, now.Add(-l.ipRule.Window))
		if len(v.timestamps) == 0 {
			delete(l.ipStore, k)
		}
	}
	for k, v := range l.pairStore {
		v.timestamps = pruneOldTimestamps(v.timestamps, now.Add(-l.pairRule.Window))
		if len(v.timestamps) == 0 {
			delete(l.pairStore, k)
		}
	}
	for k, v := range l.emailStore {
		v.timestamps = pruneOldTimestamps(v.timestamps, now.Add(-l.emailRule.Window))
		if len(v.timestamps) == 0 {
			delete(l.emailStore, k)
		}
	}
}

func pruneOldTimestamps(ts []time.Time, cutoff time.Time) []time.Time {
	idx := 0
	for i, t := range ts {
		if t.After(cutoff) {
			idx = i
			break
		}
		if i == len(ts)-1 {
			return nil
		}
	}
	return ts[idx:]
}
