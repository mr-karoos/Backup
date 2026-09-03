package domain

import (
	"strings"
	"time"
	"unicode/utf8"

	"backup-platform/pkg/uuid"
)

// Type defines the supported canonical credential types for external resources and internal engines.
type Type string

const (
	TypeSSHPrivateKey       Type = "ssh_private_key"
	TypeSSHPassword         Type = "ssh_password"
	TypeCPanelAPIToken      Type = "cpanel_api_token"
	TypeCPanelPassword      Type = "cpanel_password"
	TypeS3Credentials       Type = "s3_credentials"
	TypeResticRepositoryKey Type = "restic_repository_key"
)

// ManagedBy defines whether a credential is user-managed or platform system-managed.
type ManagedBy string

const (
	ManagedByUser   ManagedBy = "user"
	ManagedBySystem ManagedBy = "system"
)

// IsValid checks whether the managed_by discriminator is valid.
func (m ManagedBy) IsValid() bool {
	return m == ManagedByUser || m == ManagedBySystem
}

// IsValid checks whether the credential type is one of the supported canonical types.
func (t Type) IsValid() bool {
	switch t {
	case TypeSSHPrivateKey, TypeSSHPassword, TypeCPanelAPIToken, TypeCPanelPassword, TypeS3Credentials, TypeResticRepositoryKey:
		return true
	default:
		return false
	}
}

// IsUserManaged checks whether the credential type is allowed to be created/managed directly by users.
func (t Type) IsUserManaged() bool {
	switch t {
	case TypeSSHPrivateKey, TypeSSHPassword, TypeCPanelAPIToken, TypeCPanelPassword, TypeS3Credentials:
		return true
	default:
		return false
	}
}

// ValidateType checks if the type is supported and returns ErrInvalidCredentialType if not.
func ValidateType(t Type) error {
	if !t.IsValid() {
		return ErrInvalidCredentialType
	}
	return nil
}

// ValidateName trims leading and trailing whitespace and verifies that the name is non-empty
// and does not exceed 100 Unicode characters.
func ValidateName(name string) (string, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return "", ErrInvalidCredentialName
	}
	if utf8.RuneCountInString(trimmed) > 100 {
		return "", ErrInvalidCredentialName
	}
	return trimmed, nil
}

// Credential represents the internal stored entity for an external resource credential or system engine secret.
// Note: Plaintext secrets are strictly excluded from this struct by design.
type Credential struct {
	ID              uuid.UUID
	OrganizationID  uuid.UUID
	Name            string
	Type            Type
	ManagedBy       ManagedBy
	EncryptedSecret []byte
	Nonce           []byte
	AuthTag         []byte
	KeyVersion      int
	Fingerprint     *string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// CredentialMetadata represents safe, non-sensitive metadata for a credential.
// Contains no ciphertext, nonce, authentication tags, or plaintext secret data.
type CredentialMetadata struct {
	ID             uuid.UUID `json:"id"`
	OrganizationID uuid.UUID `json:"organization_id"`
	Name           string    `json:"name"`
	Type           Type      `json:"type"`
	ManagedBy      ManagedBy `json:"managed_by"`
	Fingerprint    *string   `json:"fingerprint"`
	KeyVersion     int       `json:"key_version"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}
