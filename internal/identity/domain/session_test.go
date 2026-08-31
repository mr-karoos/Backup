package domain

import (
	"errors"
	"strings"
	"testing"
	"time"

	"backup-platform/pkg/uuid"
)

func TestNewSession_Validation(t *testing.T) {
	validUserID := uuid.New()
	validHash := strings.Repeat("a", 64)
	ip := "192.168.1.100"
	ua := "Mozilla/5.0"

	t.Run("valid session creation", func(t *testing.T) {
		s, err := NewSession(validUserID, validHash, &ip, &ua, 7*24*time.Hour)
		if err != nil {
			t.Fatalf("expected success, got: %v", err)
		}
		if s.UserID != validUserID {
			t.Errorf("expected matching user ID")
		}
		if s.RefreshTokenHash != validHash {
			t.Errorf("expected matching hash")
		}
		if *s.IPAddress != ip {
			t.Errorf("expected matching IP")
		}
		if *s.UserAgent != ua {
			t.Errorf("expected matching UserAgent")
		}
		if s.RevokedAt != nil {
			t.Errorf("expected nil RevokedAt")
		}
		if !s.IsActive(time.Now().UTC()) {
			t.Errorf("expected session to be active")
		}
	})

	t.Run("nil user ID", func(t *testing.T) {
		_, err := NewSession(uuid.Nil, validHash, nil, nil, 7*24*time.Hour)
		if !errors.Is(err, ErrInvalidSessionUser) {
			t.Errorf("expected ErrInvalidSessionUser, got: %v", err)
		}
	})

	t.Run("invalid hash length", func(t *testing.T) {
		_, err := NewSession(validUserID, "short_hash", nil, nil, 7*24*time.Hour)
		if !errors.Is(err, ErrInvalidRefreshToken) {
			t.Errorf("expected ErrInvalidRefreshToken, got: %v", err)
		}
	})
}

func TestSession_StatusMethods(t *testing.T) {
	validUserID := uuid.New()
	validHash := strings.Repeat("b", 64)
	now := time.Now().UTC()

	s, err := NewSession(validUserID, validHash, nil, nil, 1*time.Hour)
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	if !s.IsActive(now) {
		t.Errorf("expected session to be active now")
	}
	if s.IsRevoked() {
		t.Errorf("expected IsRevoked = false")
	}
	if s.IsExpired(now) {
		t.Errorf("expected IsExpired = false")
	}

	// Test after expiry
	future := now.Add(2 * time.Hour)
	if s.IsActive(future) {
		t.Errorf("expected session to be inactive in future")
	}
	if !s.IsExpired(future) {
		t.Errorf("expected IsExpired = true in future")
	}

	// Test revocation
	s.Revoke(now)
	if !s.IsRevoked() {
		t.Errorf("expected IsRevoked = true after Revoke()")
	}
	if s.IsActive(now) {
		t.Errorf("expected IsActive = false after Revoke()")
	}
}
