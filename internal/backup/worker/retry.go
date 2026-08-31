package worker

import (
	"crypto/sha256"
	"encoding/binary"
	"math"
	"time"

	"backup-platform/pkg/uuid"
)

const (
	maxBackoffSeconds = 600.0 // 10 minutes
	baseDelaySeconds  = 30.0  // 30 seconds
)

// CalculateRetryDelay computes exponential backoff with deterministic jitter based on job ID and attempt number:
// min(2^attempt_number * 30 seconds + jitter, 10 minutes).
func CalculateRetryDelay(jobID uuid.UUID, attemptNumber int) time.Duration {
	if attemptNumber <= 0 {
		attemptNumber = 1
	}

	// 2^attempt * 30s
	exp := math.Pow(2, float64(attemptNumber))
	baseSec := exp * baseDelaySeconds

	// Deterministic jitter (0-14 seconds) derived from (jobID, attemptNumber)
	h := sha256.New()
	h.Write(jobID[:])
	_ = binary.Write(h, binary.BigEndian, int64(attemptNumber))
	sum := h.Sum(nil)
	jitterSec := float64(binary.BigEndian.Uint32(sum[:4]) % 15)

	totalSec := baseSec + jitterSec
	if totalSec > maxBackoffSeconds {
		totalSec = maxBackoffSeconds
	}

	return time.Duration(totalSec) * time.Second
}

// IsRetryable determines whether a failed backup execution attempt is eligible for automated retry.
func IsRetryable(err error) bool {
	if err == nil {
		return false
	}
	ef := ClassifyError(err)
	if ef == nil {
		return false
	}
	return ef.Kind.IsRetryable()
}

// SafeErrorMessage produces a sanitized error string safe for database persistence and API visibility,
// guaranteeing zero sensitive passwords, keys, raw stderr, or internal server paths are exposed.
func SafeErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	ef := ClassifyError(err)
	if ef == nil {
		return ""
	}
	return ef.Kind.SafeMessage()
}
