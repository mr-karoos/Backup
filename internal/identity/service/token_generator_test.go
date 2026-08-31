package service

import (
	"encoding/hex"
	"testing"
)

func TestSecureTokenGenerator_GenerateAndHash(t *testing.T) {
	gen := NewSecureTokenGenerator()

	raw1, hash1, err := gen.GenerateRefreshToken()
	if err != nil {
		t.Fatalf("failed to generate refresh token: %v", err)
	}

	raw2, hash2, err := gen.GenerateRefreshToken()
	if err != nil {
		t.Fatalf("failed to generate second refresh token: %v", err)
	}

	// 1. Uniqueness check
	if raw1 == raw2 {
		t.Errorf("expected distinct raw tokens, got identical: %s", raw1)
	}
	if hash1 == hash2 {
		t.Errorf("expected distinct token hashes, got identical: %s", hash1)
	}

	// 2. Format & Length check
	if len(raw1) < 40 {
		t.Errorf("expected base64 raw token length >= 40 for 32 bytes, got len: %d", len(raw1))
	}
	if len(hash1) != 64 {
		t.Errorf("expected 64 hex characters for SHA-256 hash, got len: %d", len(hash1))
	}

	// 3. Hex validity check
	decodedHash, err := hex.DecodeString(hash1)
	if err != nil || len(decodedHash) != 32 {
		t.Errorf("expected valid 32-byte hex decoded hash, err: %v, len: %d", err, len(decodedHash))
	}

	// 4. Raw token does not equal hash
	if raw1 == hash1 {
		t.Errorf("raw token must not equal hash")
	}

	// 5. Deterministic hashing check
	recomputedHash := gen.HashRefreshToken(raw1)
	if recomputedHash != hash1 {
		t.Errorf("expected deterministic hash '%s', got '%s'", hash1, recomputedHash)
	}
}
