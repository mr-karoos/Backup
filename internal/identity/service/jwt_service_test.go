package service

import (
	"errors"
	"testing"
	"time"

	"backup-platform/pkg/uuid"

	"github.com/golang-jwt/jwt/v5"
)

func TestJWTService_GenerateAndValidate(t *testing.T) {
	key := []byte("super-secret-signing-key-for-test-2026-32-bytes!")
	svc, err := NewJWTService(key)
	if err != nil {
		t.Fatalf("failed to create JWT service: %v", err)
	}

	userID := uuid.New()
	sessionID := uuid.New()
	isSystemAdmin := true

	tokenStr, expiresAt, err := svc.GenerateAccessToken(userID, sessionID, isSystemAdmin)
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}

	if tokenStr == "" {
		t.Fatal("expected non-empty token string")
	}

	// Expiry check (~15 minutes from now)
	expectedExpiry := time.Now().UTC().Add(15 * time.Minute)
	if expiresAt.Sub(expectedExpiry).Abs() > 5*time.Second {
		t.Errorf("expected expiry close to 15m, got: %v", expiresAt)
	}

	// Validate token
	payload, err := svc.ValidateAccessToken(tokenStr)
	if err != nil {
		t.Fatalf("failed to validate token: %v", err)
	}

	if payload.UserID != userID {
		t.Errorf("expected UserID %v, got %v", userID, payload.UserID)
	}
	if payload.SessionID != sessionID {
		t.Errorf("expected SessionID %v, got %v", sessionID, payload.SessionID)
	}
	if payload.IsSystemAdmin != isSystemAdmin {
		t.Errorf("expected IsSystemAdmin %v, got %v", isSystemAdmin, payload.IsSystemAdmin)
	}
}

func TestJWTService_SigningKeyLengthValidation(t *testing.T) {
	t.Run("nil key", func(t *testing.T) {
		_, err := NewJWTService(nil)
		if !errors.Is(err, ErrInvalidSigningKey) {
			t.Errorf("expected ErrInvalidSigningKey, got: %v", err)
		}
	})

	t.Run("key shorter than 32 bytes", func(t *testing.T) {
		shortKey := []byte("short-key-under-32-bytes")
		_, err := NewJWTService(shortKey)
		if !errors.Is(err, ErrInvalidSigningKey) {
			t.Errorf("expected ErrInvalidSigningKey, got: %v", err)
		}
	})

	t.Run("exactly 32 bytes key", func(t *testing.T) {
		exactKey := []byte("12345678901234567890123456789012")
		svc, err := NewJWTService(exactKey)
		if err != nil || svc == nil {
			t.Fatalf("expected success with 32-byte key, got: %v", err)
		}
	})

	t.Run("greater than 32 bytes key", func(t *testing.T) {
		longKey := []byte("1234567890123456789012345678901234567890")
		svc, err := NewJWTService(longKey)
		if err != nil || svc == nil {
			t.Fatalf("expected success with >32-byte key, got: %v", err)
		}
	})
}

func TestJWTService_ValidationFailures(t *testing.T) {
	key := []byte("correct-key-32-bytes-long-1234567890!")
	wrongKey := []byte("wrong-key-32-bytes-long-1234567890-diff!")

	svc, err := NewJWTService(key)
	if err != nil {
		t.Fatalf("failed to create JWT service: %v", err)
	}

	userID := uuid.New()
	sessionID := uuid.New()

	t.Run("wrong signing key", func(t *testing.T) {
		wrongSvc, _ := NewJWTService(wrongKey)
		tokenStr, _, _ := wrongSvc.GenerateAccessToken(userID, sessionID, false)

		_, err := svc.ValidateAccessToken(tokenStr)
		if err == nil {
			t.Fatal("expected validation failure with wrong key, got nil")
		}
	})

	t.Run("expired token", func(t *testing.T) {
		claims := AccessClaims{
			UserID:        userID.String(),
			SessionID:     sessionID.String(),
			IsSystemAdmin: false,
			RegisteredClaims: jwt.RegisteredClaims{
				Issuer:    AccessTokenIssuer,
				IssuedAt:  jwt.NewNumericDate(time.Now().UTC().Add(-30 * time.Minute)),
				ExpiresAt: jwt.NewNumericDate(time.Now().UTC().Add(-15 * time.Minute)),
			},
		}
		tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		expiredTokenStr, _ := tok.SignedString(key)

		_, err := svc.ValidateAccessToken(expiredTokenStr)
		if !errors.Is(err, ErrExpiredToken) {
			t.Errorf("expected ErrExpiredToken, got: %v", err)
		}
	})

	t.Run("missing exp claim", func(t *testing.T) {
		claims := AccessClaims{
			UserID:        userID.String(),
			SessionID:     sessionID.String(),
			IsSystemAdmin: false,
			RegisteredClaims: jwt.RegisteredClaims{
				Issuer:   AccessTokenIssuer,
				IssuedAt: jwt.NewNumericDate(time.Now().UTC()),
			},
		}
		tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		noExpTokenStr, _ := tok.SignedString(key)

		_, err := svc.ValidateAccessToken(noExpTokenStr)
		if err == nil {
			t.Fatal("expected rejection of token without exp claim, got nil")
		}
	})

	t.Run("missing iat claim", func(t *testing.T) {
		claims := AccessClaims{
			UserID:        userID.String(),
			SessionID:     sessionID.String(),
			IsSystemAdmin: false,
			RegisteredClaims: jwt.RegisteredClaims{
				Issuer:    AccessTokenIssuer,
				ExpiresAt: jwt.NewNumericDate(time.Now().UTC().Add(15 * time.Minute)),
			},
		}
		tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		noIatTokenStr, _ := tok.SignedString(key)

		_, err := svc.ValidateAccessToken(noIatTokenStr)
		if !errors.Is(err, ErrInvalidTokenClaims) {
			t.Errorf("expected ErrInvalidTokenClaims for missing iat, got: %v", err)
		}
	})

	t.Run("future iat claim rejected", func(t *testing.T) {
		claims := AccessClaims{
			UserID:        userID.String(),
			SessionID:     sessionID.String(),
			IsSystemAdmin: false,
			RegisteredClaims: jwt.RegisteredClaims{
				Issuer:    AccessTokenIssuer,
				IssuedAt:  jwt.NewNumericDate(time.Now().UTC().Add(10 * time.Minute)),
				ExpiresAt: jwt.NewNumericDate(time.Now().UTC().Add(25 * time.Minute)),
			},
		}
		tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		futureIatTokenStr, _ := tok.SignedString(key)

		_, err := svc.ValidateAccessToken(futureIatTokenStr)
		if err == nil {
			t.Fatal("expected rejection of token with future iat, got nil")
		}
	})

	t.Run("wrong algorithm none", func(t *testing.T) {
		claims := AccessClaims{
			UserID:        userID.String(),
			SessionID:     sessionID.String(),
			IsSystemAdmin: false,
			RegisteredClaims: jwt.RegisteredClaims{
				Issuer:    AccessTokenIssuer,
				IssuedAt:  jwt.NewNumericDate(time.Now().UTC()),
				ExpiresAt: jwt.NewNumericDate(time.Now().UTC().Add(15 * time.Minute)),
			},
		}
		tok := jwt.NewWithClaims(jwt.SigningMethodNone, claims)
		unsignedTokenStr, _ := tok.SignedString(jwt.UnsafeAllowNoneSignatureType)

		_, err := svc.ValidateAccessToken(unsignedTokenStr)
		if err == nil {
			t.Fatal("expected rejection of alg=none, got nil")
		}
	})

	t.Run("wrong algorithm HS384", func(t *testing.T) {
		claims := AccessClaims{
			UserID:        userID.String(),
			SessionID:     sessionID.String(),
			IsSystemAdmin: false,
			RegisteredClaims: jwt.RegisteredClaims{
				Issuer:    AccessTokenIssuer,
				IssuedAt:  jwt.NewNumericDate(time.Now().UTC()),
				ExpiresAt: jwt.NewNumericDate(time.Now().UTC().Add(15 * time.Minute)),
			},
		}
		tok := jwt.NewWithClaims(jwt.SigningMethodHS384, claims)
		hs384TokenStr, _ := tok.SignedString(key)

		_, err := svc.ValidateAccessToken(hs384TokenStr)
		if err == nil {
			t.Fatal("expected rejection of HS384 algorithm, got nil")
		}
	})

	t.Run("wrong issuer", func(t *testing.T) {
		claims := AccessClaims{
			UserID:        userID.String(),
			SessionID:     sessionID.String(),
			IsSystemAdmin: false,
			RegisteredClaims: jwt.RegisteredClaims{
				Issuer:    "unknown-issuer",
				IssuedAt:  jwt.NewNumericDate(time.Now().UTC()),
				ExpiresAt: jwt.NewNumericDate(time.Now().UTC().Add(15 * time.Minute)),
			},
		}
		tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		wrongIssuerTokenStr, _ := tok.SignedString(key)

		_, err := svc.ValidateAccessToken(wrongIssuerTokenStr)
		if err == nil {
			t.Fatal("expected rejection of wrong issuer, got nil")
		}
	})

	t.Run("malformed user_id UUID", func(t *testing.T) {
		claims := AccessClaims{
			UserID:        "not-a-valid-uuid",
			SessionID:     sessionID.String(),
			IsSystemAdmin: false,
			RegisteredClaims: jwt.RegisteredClaims{
				Issuer:    AccessTokenIssuer,
				IssuedAt:  jwt.NewNumericDate(time.Now().UTC()),
				ExpiresAt: jwt.NewNumericDate(time.Now().UTC().Add(15 * time.Minute)),
			},
		}
		tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		badUUIDTokenStr, _ := tok.SignedString(key)

		_, err := svc.ValidateAccessToken(badUUIDTokenStr)
		if !errors.Is(err, ErrInvalidTokenClaims) {
			t.Errorf("expected ErrInvalidTokenClaims, got: %v", err)
		}
	})

	t.Run("malformed session_id UUID", func(t *testing.T) {
		claims := AccessClaims{
			UserID:        userID.String(),
			SessionID:     "not-a-valid-uuid",
			IsSystemAdmin: false,
			RegisteredClaims: jwt.RegisteredClaims{
				Issuer:    AccessTokenIssuer,
				IssuedAt:  jwt.NewNumericDate(time.Now().UTC()),
				ExpiresAt: jwt.NewNumericDate(time.Now().UTC().Add(15 * time.Minute)),
			},
		}
		tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		badUUIDTokenStr, _ := tok.SignedString(key)

		_, err := svc.ValidateAccessToken(badUUIDTokenStr)
		if !errors.Is(err, ErrInvalidTokenClaims) {
			t.Errorf("expected ErrInvalidTokenClaims, got: %v", err)
		}
	})

	t.Run("malformed token string", func(t *testing.T) {
		_, err := svc.ValidateAccessToken("garbage.token.string")
		if !errors.Is(err, ErrInvalidToken) {
			t.Errorf("expected ErrInvalidToken, got: %v", err)
		}
	})
}
