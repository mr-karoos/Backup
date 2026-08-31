package service

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

func TestArgon2idHasher_HashAndVerifySuccess(t *testing.T) {
	hasher := NewDefaultArgon2idHasher()
	password := "Correct-Horse-Battery-Staple-2026!"

	encoded, err := hasher.Hash(password)
	if err != nil {
		t.Fatalf("failed to hash password: %v", err)
	}

	if encoded == "" {
		t.Fatal("expected non-empty hash string")
	}

	// Verify PHC format prefix with default profile (t=3)
	if !strings.HasPrefix(encoded, "$argon2id$v=19$m=65536,t=3,p=4$") {
		t.Errorf("expected $argon2id$v=19$m=65536,t=3,p=4$ prefix, got: %s", encoded)
	}

	// Ensure raw password is NOT in the encoded hash
	if strings.Contains(encoded, password) {
		t.Errorf("raw password leaked into hash string!")
	}

	// Verify correct password
	matched, err := hasher.Verify(password, encoded)
	if err != nil {
		t.Fatalf("unexpected error during verification: %v", err)
	}
	if !matched {
		t.Errorf("expected password verification to succeed, got false")
	}
}

func TestArgon2idHasher_VerifyWrongPassword(t *testing.T) {
	hasher := NewDefaultArgon2idHasher()
	password := "SecretPassword123"
	wrongPassword := "WrongPassword123"

	encoded, err := hasher.Hash(password)
	if err != nil {
		t.Fatalf("failed to hash password: %v", err)
	}

	matched, err := hasher.Verify(wrongPassword, encoded)
	if err != nil {
		t.Fatalf("unexpected error during wrong password verification: %v", err)
	}
	if matched {
		t.Errorf("expected wrong password to fail verification, got true")
	}
}

func TestArgon2idHasher_InvalidHasherConfiguration(t *testing.T) {
	invalidConfigs := []Argon2idParams{
		{Memory: 0, Iterations: 3, Parallelism: 4, SaltLength: 16, KeyLength: 32},
		{Memory: 4 * 1024, Iterations: 3, Parallelism: 4, SaltLength: 16, KeyLength: 32},   // < 8MiB
		{Memory: 512 * 1024, Iterations: 3, Parallelism: 4, SaltLength: 16, KeyLength: 32}, // > 256MiB
		{Memory: 64 * 1024, Iterations: 0, Parallelism: 4, SaltLength: 16, KeyLength: 32},  // t=0
		{Memory: 64 * 1024, Iterations: 20, Parallelism: 4, SaltLength: 16, KeyLength: 32}, // t > 10
		{Memory: 64 * 1024, Iterations: 3, Parallelism: 0, SaltLength: 16, KeyLength: 32},  // p=0
		{Memory: 64 * 1024, Iterations: 3, Parallelism: 32, SaltLength: 16, KeyLength: 32}, // p > 16
		{Memory: 64 * 1024, Iterations: 3, Parallelism: 4, SaltLength: 4, KeyLength: 32},   // salt < 8
		{Memory: 64 * 1024, Iterations: 3, Parallelism: 4, SaltLength: 128, KeyLength: 32}, // salt > 64
		{Memory: 64 * 1024, Iterations: 3, Parallelism: 4, SaltLength: 16, KeyLength: 8},   // key < 16
		{Memory: 64 * 1024, Iterations: 3, Parallelism: 4, SaltLength: 16, KeyLength: 128}, // key > 64
	}

	for i, cfg := range invalidConfigs {
		h := NewArgon2idHasher(cfg)
		_, err := h.Hash("test-password")
		if !errors.Is(err, ErrInvalidHasherParams) {
			t.Errorf("config %d: expected ErrInvalidHasherParams, got: %v", i, err)
		}
	}
}

func TestArgon2idHasher_VerifyMalformedAndOutOfBoundsHashes(t *testing.T) {
	hasher := NewDefaultArgon2idHasher()
	validSalt := base64.RawStdEncoding.EncodeToString([]byte("1234567890123456"))
	validKey := base64.RawStdEncoding.EncodeToString(make([]byte, 32))
	shortSalt := base64.RawStdEncoding.EncodeToString([]byte("1234"))
	shortKey := base64.RawStdEncoding.EncodeToString([]byte("1234"))

	malformedCases := []struct {
		name string
		hash string
	}{
		{"empty", ""},
		{"plain string", "plain-string"},
		{"not argon2id", "$bcrypt$v=19$m=65536,t=3,p=4$" + validSalt + "$" + validKey},
		{"wrong version", "$argon2id$v=99$m=65536,t=3,p=4$" + validSalt + "$" + validKey},
		{"non-numeric version", "$argon2id$v=abc$m=65536,t=3,p=4$" + validSalt + "$" + validKey},
		{"missing m", "$argon2id$v=19$t=3,p=4$" + validSalt + "$" + validKey},
		{"missing t", "$argon2id$v=19$m=65536,p=4$" + validSalt + "$" + validKey},
		{"missing p", "$argon2id$v=19$m=65536,t=3$" + validSalt + "$" + validKey},
		{"duplicate m", "$argon2id$v=19$m=65536,m=65536,t=3,p=4$" + validSalt + "$" + validKey},
		{"duplicate t", "$argon2id$v=19$m=65536,t=3,t=3,p=4$" + validSalt + "$" + validKey},
		{"duplicate p", "$argon2id$v=19$m=65536,t=3,p=4,p=4$" + validSalt + "$" + validKey},
		{"memory zero", "$argon2id$v=19$m=0,t=3,p=4$" + validSalt + "$" + validKey},
		{"memory too small", "$argon2id$v=19$m=4096,t=3,p=4$" + validSalt + "$" + validKey},
		{"memory too large", "$argon2id$v=19$m=1048576,t=3,p=4$" + validSalt + "$" + validKey},
		{"iterations zero", "$argon2id$v=19$m=65536,t=0,p=4$" + validSalt + "$" + validKey},
		{"iterations too large", "$argon2id$v=19$m=65536,t=20,p=4$" + validSalt + "$" + validKey},
		{"parallelism zero", "$argon2id$v=19$m=65536,t=3,p=0$" + validSalt + "$" + validKey},
		{"parallelism too large", "$argon2id$v=19$m=65536,t=3,p=32$" + validSalt + "$" + validKey},
		{"parallelism 256", "$argon2id$v=19$m=65536,t=3,p=256$" + validSalt + "$" + validKey},
		{"invalid base64 salt", "$argon2id$v=19$m=65536,t=3,p=4$invalid_base64!$" + validKey},
		{"invalid base64 key", "$argon2id$v=19$m=65536,t=3,p=4$" + validSalt + "$invalid_base64!"},
		{"salt too short", "$argon2id$v=19$m=65536,t=3,p=4$" + shortSalt + "$" + validKey},
		{"hash key too short", "$argon2id$v=19$m=65536,t=3,p=4$" + validSalt + "$" + shortKey},
	}

	for _, tc := range malformedCases {
		t.Run(tc.name, func(t *testing.T) {
			matched, err := hasher.Verify("password", tc.hash)
			if err == nil {
				t.Fatalf("expected error for case '%s', got nil", tc.name)
			}
			if matched {
				t.Fatalf("expected matched=false for case '%s'", tc.name)
			}
		})
	}
}
