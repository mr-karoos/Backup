package artifactcrypto

import (
	"crypto/subtle"
	"errors"
)

var (
	// ErrInvalidKeyLength indicates a key that is not exactly 32 bytes (AES-256).
	ErrInvalidKeyLength = errors.New("artifact encryption key must be exactly 32 bytes")

	// ErrInvalidKeyVersion indicates a key version that is less than 1.
	ErrInvalidKeyVersion = errors.New("artifact encryption key version must be a positive integer")

	// ErrUnknownKeyVersion indicates a requested key version is not registered or unavailable.
	ErrUnknownKeyVersion = errors.New("unknown or unavailable artifact encryption key version")
)

// KeyProvider supplies artifact encryption master keys and their version identifiers.
type KeyProvider interface {
	// Current returns the active artifact encryption master key and its version number.
	// Returned byte slices are defensive copies.
	Current() (key []byte, version int, err error)

	// ByVersion returns the artifact encryption master key for the specified version number.
	// Returned byte slices are defensive copies.
	ByVersion(version int) (key []byte, err error)
}

// StaticKeyProvider implements KeyProvider with a single immutable 32-byte AES-256 master key.
type StaticKeyProvider struct {
	key     []byte
	version int
}

// NewStaticKeyProvider constructs a StaticKeyProvider after validating key length (32 bytes) and version (>= 1).
// A defensive copy of the masterKey slice is made internally.
func NewStaticKeyProvider(masterKey []byte, version int) (*StaticKeyProvider, error) {
	if len(masterKey) != 32 {
		return nil, ErrInvalidKeyLength
	}
	if version < 1 {
		return nil, ErrInvalidKeyVersion
	}

	keyCopy := make([]byte, len(masterKey))
	copy(keyCopy, masterKey)

	return &StaticKeyProvider{
		key:     keyCopy,
		version: version,
	}, nil
}

// Current returns a defensive copy of the active encryption key and its version.
func (p *StaticKeyProvider) Current() ([]byte, int, error) {
	out := make([]byte, len(p.key))
	copy(out, p.key)
	return out, p.version, nil
}

// ByVersion returns a defensive copy of the key if version matches, or ErrUnknownKeyVersion otherwise.
func (p *StaticKeyProvider) ByVersion(version int) ([]byte, error) {
	if version < 1 {
		return nil, ErrInvalidKeyVersion
	}
	if version != p.version {
		return nil, ErrUnknownKeyVersion
	}

	out := make([]byte, len(p.key))
	copy(out, p.key)
	return out, nil
}

// ZeroBytes best-effort overwrites sensitive key slices in memory with zeros.
func ZeroBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
	subtle.ConstantTimeByteEq(0, 0)
}
