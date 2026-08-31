package service

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

var (
	ErrInvalidHashFormat   = errors.New("invalid or corrupt password hash format")
	ErrIncompatibleHash    = errors.New("incompatible password hash algorithm")
	ErrInvalidHasherParams = errors.New("invalid password hasher configuration parameters")
	ErrHashParamsBounds    = errors.New("password hash parameters are out of acceptable bounds")
)

// Conservative defensive bounds for Argon2id parameters (V1 security policy).
const (
	MinMemory      uint32 = 8 * 1024   // 8 MiB (8192 KiB)
	MaxMemory      uint32 = 256 * 1024 // 256 MiB (262144 KiB)
	MinIterations  uint32 = 1
	MaxIterations  uint32 = 10
	MinParallelism uint8  = 1
	MaxParallelism uint8  = 16
	MinSaltLength  uint32 = 8
	MaxSaltLength  uint32 = 64
	MinKeyLength   uint32 = 16
	MaxKeyLength   uint32 = 64
)

// Argon2idParams holds configurable tuning parameters for Argon2id.
type Argon2idParams struct {
	Memory      uint32
	Iterations  uint32
	Parallelism uint8
	SaltLength  uint32
	KeyLength   uint32
}

// DefaultParams provides application default Argon2id profile (m=64MiB, t=3, p=4, salt=16B, key=32B).
var DefaultParams = Argon2idParams{
	Memory:      64 * 1024, // 64 MB (65536 KiB)
	Iterations:  3,
	Parallelism: 4,
	SaltLength:  16,
	KeyLength:   32,
}

// Argon2idHasher implements domain.PasswordHasher using the Argon2id key derivation function.
type Argon2idHasher struct {
	params Argon2idParams
}

// NewArgon2idHasher constructs a new hasher with the given tuning parameters.
func NewArgon2idHasher(params Argon2idParams) *Argon2idHasher {
	return &Argon2idHasher{params: params}
}

// NewDefaultArgon2idHasher constructs a new hasher with secure default parameters.
func NewDefaultArgon2idHasher() *Argon2idHasher {
	return &Argon2idHasher{params: DefaultParams}
}

// Hash securely hashes a plaintext password using Argon2id and returns a standard PHC formatted string.
func (h *Argon2idHasher) Hash(password string) (string, error) {
	if h.params.Memory < MinMemory || h.params.Memory > MaxMemory ||
		h.params.Iterations < MinIterations || h.params.Iterations > MaxIterations ||
		h.params.Parallelism < MinParallelism || h.params.Parallelism > MaxParallelism ||
		h.params.SaltLength < MinSaltLength || h.params.SaltLength > MaxSaltLength ||
		h.params.KeyLength < MinKeyLength || h.params.KeyLength > MaxKeyLength {
		return "", ErrInvalidHasherParams
	}

	salt := make([]byte, h.params.SaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("failed to generate random salt: %w", err)
	}

	hash := argon2.IDKey([]byte(password), salt, h.params.Iterations, h.params.Memory, h.params.Parallelism, h.params.KeyLength)

	b64Salt := base64.RawStdEncoding.EncodeToString(salt)
	b64Hash := base64.RawStdEncoding.EncodeToString(hash)

	// Format: $argon2id$v=19$m=65536,t=3,p=4$<salt>$<hash>
	encoded := fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, h.params.Memory, h.params.Iterations, h.params.Parallelism, b64Salt, b64Hash)

	return encoded, nil
}

// Verify checks whether a plaintext candidate password matches the Argon2id PHC encoded hash.
func (h *Argon2idHasher) Verify(password, encodedHash string) (bool, error) {
	parts := strings.Split(encodedHash, "$")
	// Expected parts: ["", "argon2id", "v=19", "m=65536,t=3,p=4", "<salt>", "<hash>"]
	if len(parts) != 6 {
		return false, ErrInvalidHashFormat
	}

	if parts[1] != "argon2id" {
		return false, ErrIncompatibleHash
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return false, ErrInvalidHashFormat
	}
	if version != argon2.Version {
		return false, ErrIncompatibleHash
	}

	paramList := strings.Split(parts[3], ",")
	if len(paramList) != 3 {
		return false, ErrInvalidHashFormat
	}

	var memory, iterations uint32
	var parallelism uint32
	seenM, seenT, seenP := false, false, false

	for _, p := range paramList {
		kv := strings.Split(p, "=")
		if len(kv) != 2 {
			return false, ErrInvalidHashFormat
		}
		val, err := strconv.ParseUint(kv[1], 10, 32)
		if err != nil {
			return false, ErrInvalidHashFormat
		}
		switch kv[0] {
		case "m":
			if seenM {
				return false, ErrInvalidHashFormat
			}
			seenM = true
			memory = uint32(val)
		case "t":
			if seenT {
				return false, ErrInvalidHashFormat
			}
			seenT = true
			iterations = uint32(val)
		case "p":
			if seenP {
				return false, ErrInvalidHashFormat
			}
			seenP = true
			parallelism = uint32(val)
		default:
			return false, ErrInvalidHashFormat
		}
	}

	if !seenM || !seenT || !seenP {
		return false, ErrInvalidHashFormat
	}

	// Defensive parameter bounds checking
	if memory < MinMemory || memory > MaxMemory ||
		iterations < MinIterations || iterations > MaxIterations ||
		parallelism < uint32(MinParallelism) || parallelism > uint32(MaxParallelism) {
		return false, ErrHashParamsBounds
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, ErrInvalidHashFormat
	}
	if len(salt) < int(MinSaltLength) || len(salt) > int(MaxSaltLength) {
		return false, ErrHashParamsBounds
	}

	expectedHash, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false, ErrInvalidHashFormat
	}
	if len(expectedHash) < int(MinKeyLength) || len(expectedHash) > int(MaxKeyLength) {
		return false, ErrHashParamsBounds
	}

	computedHash := argon2.IDKey([]byte(password), salt, iterations, memory, uint8(parallelism), uint32(len(expectedHash)))

	if subtle.ConstantTimeCompare(computedHash, expectedHash) == 1 {
		return true, nil
	}

	return false, nil
}
