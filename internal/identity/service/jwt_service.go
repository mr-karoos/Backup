package service

import (
	"errors"
	"strings"
	"time"

	"backup-platform/pkg/uuid"

	"github.com/golang-jwt/jwt/v5"
)

const (
	AccessTokenIssuer   = "backup-platform"
	DefaultTokenExpiry  = 15 * time.Minute
	MinSigningKeyLength = 32
)

// Safe sentinel errors for token generation and validation.
var (
	ErrInvalidToken       = errors.New("invalid or malformed access token")
	ErrExpiredToken       = errors.New("access token has expired")
	ErrInvalidTokenClaims = errors.New("access token claims are invalid or incomplete")
	ErrInvalidSigningKey  = errors.New("signing key must be at least 32 bytes")
	ErrTokenGeneration    = errors.New("failed to generate access token")
)

// AccessClaims defines the payload claims embedded in the short-lived access JWT.
type AccessClaims struct {
	UserID        string `json:"user_id"`
	SessionID     string `json:"session_id"`
	IsSystemAdmin bool   `json:"is_system_admin"`
	jwt.RegisteredClaims
}

// TokenPayload represents the validated, typed payload extracted from an access token.
type TokenPayload struct {
	UserID        uuid.UUID
	SessionID     uuid.UUID
	IsSystemAdmin bool
	ExpiresAt     time.Time
}

// TokenService abstracts generation and validation of short-lived access JWTs.
type TokenService interface {
	GenerateAccessToken(userID, sessionID uuid.UUID, isSystemAdmin bool) (tokenStr string, expiresAt time.Time, err error)
	ValidateAccessToken(tokenStr string) (*TokenPayload, error)
}

// JWTService implements TokenService using HMAC-SHA256 (HS256).
type JWTService struct {
	signingKey []byte
	expiry     time.Duration
	issuer     string
}

// NewJWTService constructs a new JWTService with a secret signing key of at least 32 bytes.
func NewJWTService(signingKey []byte) (*JWTService, error) {
	if len(signingKey) < MinSigningKeyLength {
		return nil, ErrInvalidSigningKey
	}
	// Defensive copy to ensure immutability independent of caller's slice
	keyCopy := make([]byte, len(signingKey))
	copy(keyCopy, signingKey)

	return &JWTService{
		signingKey: keyCopy,
		expiry:     DefaultTokenExpiry,
		issuer:     AccessTokenIssuer,
	}, nil
}

// GenerateAccessToken signs a new short-lived HS256 JWT access token.
func (s *JWTService) GenerateAccessToken(userID, sessionID uuid.UUID, isSystemAdmin bool) (string, time.Time, error) {
	if userID == uuid.Nil || sessionID == uuid.Nil {
		return "", time.Time{}, ErrTokenGeneration
	}

	now := time.Now().UTC()
	expiresAt := now.Add(s.expiry)

	claims := AccessClaims{
		UserID:        userID.String(),
		SessionID:     sessionID.String(),
		IsSystemAdmin: isSystemAdmin,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    s.issuer,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenStr, err := token.SignedString(s.signingKey)
	if err != nil {
		return "", time.Time{}, ErrTokenGeneration
	}

	return tokenStr, expiresAt, nil
}

// ValidateAccessToken parses and strictly validates an HS256 access JWT.
func (s *JWTService) ValidateAccessToken(tokenStr string) (*TokenPayload, error) {
	tokenStr = strings.TrimSpace(tokenStr)
	if tokenStr == "" {
		return nil, ErrInvalidToken
	}

	token, err := jwt.ParseWithClaims(
		tokenStr,
		&AccessClaims{},
		func(t *jwt.Token) (any, error) {
			// Strictly verify HMAC signing method and HS256
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok || t.Method != jwt.SigningMethodHS256 {
				return nil, ErrInvalidToken
			}
			return s.signingKey, nil
		},
		jwt.WithValidMethods([]string{"HS256"}),
		jwt.WithIssuer(s.issuer),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
	)

	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, ErrExpiredToken
		}
		return nil, ErrInvalidToken
	}

	claims, ok := token.Claims.(*AccessClaims)
	if !ok || !token.Valid {
		return nil, ErrInvalidToken
	}

	// Validate required registered timestamps
	if claims.IssuedAt == nil || claims.ExpiresAt == nil {
		return nil, ErrInvalidTokenClaims
	}

	// Validate required subject claims
	userUUID, err := uuid.Parse(claims.UserID)
	if err != nil || userUUID == uuid.Nil {
		return nil, ErrInvalidTokenClaims
	}

	sessionUUID, err := uuid.Parse(claims.SessionID)
	if err != nil || sessionUUID == uuid.Nil {
		return nil, ErrInvalidTokenClaims
	}

	return &TokenPayload{
		UserID:        userUUID,
		SessionID:     sessionUUID,
		IsSystemAdmin: claims.IsSystemAdmin,
		ExpiresAt:     claims.ExpiresAt.Time,
	}, nil
}
