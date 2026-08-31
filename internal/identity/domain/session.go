package domain

import (
	"errors"
	"strings"
	"time"

	"backup-platform/pkg/uuid"
)

var (
	ErrInvalidCredentials     = errors.New("invalid email or password")
	ErrSessionNotFound        = errors.New("session not found")
	ErrInvalidSession         = errors.New("invalid, expired or revoked session")
	ErrInvalidRefreshToken    = errors.New("invalid refresh token format")
	ErrInvalidSessionUser     = errors.New("session user ID cannot be empty")
	ErrAuthServiceUnavailable = errors.New("authentication service temporarily unavailable")
)

// Session represents a persisted user session for refresh token management (Global/User-scoped).
type Session struct {
	ID               uuid.UUID
	UserID           uuid.UUID
	RefreshTokenHash string
	IPAddress        *string
	UserAgent        *string
	CreatedAt        time.Time
	LastUsedAt       time.Time
	ExpiresAt        time.Time
	RevokedAt        *time.Time
}

// NewSession constructs a new validated Session.
func NewSession(userID uuid.UUID, refreshTokenHash string, ipAddress, userAgent *string, duration time.Duration) (*Session, error) {
	if userID == uuid.Nil {
		return nil, ErrInvalidSessionUser
	}
	refreshTokenHash = strings.TrimSpace(refreshTokenHash)
	if len(refreshTokenHash) != 64 {
		return nil, ErrInvalidRefreshToken
	}

	now := time.Now().UTC()
	return &Session{
		ID:               uuid.New(),
		UserID:           userID,
		RefreshTokenHash: refreshTokenHash,
		IPAddress:        ipAddress,
		UserAgent:        userAgent,
		CreatedAt:        now,
		LastUsedAt:       now,
		ExpiresAt:        now.Add(duration),
		RevokedAt:        nil,
	}, nil
}

// IsActive returns true if the session is not revoked and has not expired.
func (s *Session) IsActive(now time.Time) bool {
	if s.RevokedAt != nil {
		return false
	}
	return now.Before(s.ExpiresAt)
}

// IsRevoked returns true if the session has been explicitly revoked.
func (s *Session) IsRevoked() bool {
	return s.RevokedAt != nil
}

// IsExpired returns true if current time is equal to or past expires_at.
func (s *Session) IsExpired(now time.Time) bool {
	return !now.Before(s.ExpiresAt)
}

// Revoke marks the session as revoked at the specified timestamp.
func (s *Session) Revoke(now time.Time) {
	t := now.UTC()
	s.RevokedAt = &t
}
