package domain

import (
	"errors"
	"testing"
)

func TestNewUser_Validation(t *testing.T) {
	validEmail := "admin@example.com"
	validHash := "$argon2id$v=19$m=65536,t=3,p=4$fake_salt$fake_hash"
	validName := "Administrator"

	t.Run("valid user creation", func(t *testing.T) {
		u, err := NewUser(validEmail, validHash, validName, true)
		if err != nil {
			t.Fatalf("expected success, got: %v", err)
		}
		if u.Email != "admin@example.com" {
			t.Errorf("expected lowercase email, got: %s", u.Email)
		}
		if u.PasswordHash != validHash {
			t.Errorf("expected password hash match")
		}
		if u.FullName != validName {
			t.Errorf("expected full name match")
		}
		if !u.IsSystemAdmin {
			t.Errorf("expected is_system_admin = true")
		}
		if u.Status != UserStatusActive {
			t.Errorf("expected active status, got: %s", u.Status)
		}
	})

	t.Run("empty email", func(t *testing.T) {
		_, err := NewUser("", validHash, validName, false)
		if !errors.Is(err, ErrInvalidUserEmail) {
			t.Errorf("expected ErrInvalidUserEmail, got: %v", err)
		}
	})

	t.Run("empty password hash", func(t *testing.T) {
		_, err := NewUser(validEmail, "", validName, false)
		if !errors.Is(err, ErrInvalidPasswordHash) {
			t.Errorf("expected ErrInvalidPasswordHash, got: %v", err)
		}

		_, err = NewUser(validEmail, "   ", validName, false)
		if !errors.Is(err, ErrInvalidPasswordHash) {
			t.Errorf("expected ErrInvalidPasswordHash for whitespace hash, got: %v", err)
		}
	})

	t.Run("empty full name", func(t *testing.T) {
		_, err := NewUser(validEmail, validHash, "", false)
		if !errors.Is(err, ErrInvalidFullName) {
			t.Errorf("expected ErrInvalidFullName, got: %v", err)
		}
	})
}
