package domain

import (
	"errors"
	"strings"
	"time"

	"backup-platform/pkg/uuid"
)

var (
	ErrUserNotFound        = errors.New("user not found")
	ErrDuplicateEmail      = errors.New("user with this email already exists")
	ErrInvalidUserEmail    = errors.New("user email cannot be empty")
	ErrInvalidFullName     = errors.New("user full name cannot be empty")
	ErrInvalidPasswordHash = errors.New("user password hash cannot be empty")
	ErrInvalidStatus       = errors.New("invalid user status")
)

type UserStatus string

const (
	UserStatusActive   UserStatus = "active"
	UserStatusInactive UserStatus = "inactive"
	UserStatusBlocked  UserStatus = "blocked"
)

// User represents the global user entity (independent of organizations).
type User struct {
	ID            uuid.UUID
	Email         string
	PasswordHash  string
	FullName      string
	IsSystemAdmin bool
	Status        UserStatus
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// NewUser constructs a new User instance with validation.
func NewUser(email, passwordHash, fullName string, isSystemAdmin bool) (*User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return nil, ErrInvalidUserEmail
	}
	passwordHash = strings.TrimSpace(passwordHash)
	if passwordHash == "" {
		return nil, ErrInvalidPasswordHash
	}
	fullName = strings.TrimSpace(fullName)
	if fullName == "" {
		return nil, ErrInvalidFullName
	}

	now := time.Now().UTC()
	return &User{
		ID:            uuid.New(),
		Email:         email,
		PasswordHash:  passwordHash,
		FullName:      fullName,
		IsSystemAdmin: isSystemAdmin,
		Status:        UserStatusActive,
		CreatedAt:     now,
		UpdatedAt:     now,
	}, nil
}

// PasswordHasher abstracts secure cryptographic password hashing and verification.
type PasswordHasher interface {
	Hash(password string) (string, error)
	Verify(password, encodedHash string) (bool, error)
}
