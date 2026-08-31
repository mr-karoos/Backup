package service

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
)

const RefreshTokenEntropyBytes = 32

// TokenGenerator defines operations for generating and hashing secure opaque refresh tokens.
type TokenGenerator interface {
	GenerateRefreshToken() (rawToken string, tokenHash string, err error)
	HashRefreshToken(rawToken string) string
}

// SecureTokenGenerator implements TokenGenerator using crypto/rand and SHA-256.
type SecureTokenGenerator struct{}

// NewSecureTokenGenerator constructs a new SecureTokenGenerator.
func NewSecureTokenGenerator() *SecureTokenGenerator {
	return &SecureTokenGenerator{}
}

// GenerateRefreshToken generates a cryptographically secure, URL-safe opaque token and its SHA-256 hash.
func (g *SecureTokenGenerator) GenerateRefreshToken() (string, string, error) {
	b := make([]byte, RefreshTokenEntropyBytes)
	if _, err := rand.Read(b); err != nil {
		return "", "", fmt.Errorf("failed to generate random token entropy: %w", err)
	}

	rawToken := base64.RawURLEncoding.EncodeToString(b)
	tokenHash := g.HashRefreshToken(rawToken)

	return rawToken, tokenHash, nil
}

// HashRefreshToken computes the deterministic 64-character lowercase SHA-256 hex string of a raw token.
func (g *SecureTokenGenerator) HashRefreshToken(rawToken string) string {
	rawToken = strings.TrimSpace(rawToken)
	sum := sha256.Sum256([]byte(rawToken))
	return hex.EncodeToString(sum[:])
}
