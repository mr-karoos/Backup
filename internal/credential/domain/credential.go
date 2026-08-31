package domain

import (
	"strings"
	"time"
	"unicode/utf8"

	"backup-platform/pkg/uuid"
)

// Type defines the supported canonical credential types for external resources in V1.
type Type string

const (
	TypeSSHPrivateKey  Type = "ssh_private_key"
	TypeSSHPassword    Type = "ssh_password"
	TypeCPanelAPIToken Type = "cpanel_api_token"
	TypeCPanelPassword Type = "cpanel_password"
)

// IsValid checks whether the credential type is one of the supported V1 canonical types.
func (t Type) IsValid() bool {
	switch t {
	case TypeSSHPrivateKey, TypeSSHPassword, TypeCPanelAPIToken, TypeCPanelPassword:
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

// Credential represents the internal stored entity for an external resource credential.
// Note: Plaintext secrets are strictly excluded from this struct by design.
type Credential struct {
	ID              uuid.UUID
	OrganizationID  uuid.UUID
	Name            string
	Type            Type
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
	Fingerprint    *string   `json:"fingerprint"`
	KeyVersion     int       `json:"key_version"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}
