package httpapi

import (
	"time"
)

// CreateCredentialRequest defines the strict input JSON schema for creating a credential.
type CreateCredentialRequest struct {
	Name            string  `json:"name"`
	Type            string  `json:"type"`
	Secret          string  `json:"secret,omitempty"`
	Passphrase      *string `json:"passphrase,omitempty"`
	AccessKeyID     *string `json:"access_key_id,omitempty"`
	SecretAccessKey *string `json:"secret_access_key,omitempty"`
	SessionToken    *string `json:"session_token,omitempty"`
}

// CredentialCreateResponse defines the safe HTTP response schema for a newly created credential.
type CredentialCreateResponse struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Type        string    `json:"type"`
	Fingerprint *string   `json:"fingerprint"`
	CreatedAt   time.Time `json:"created_at"`
}

// UpdateCredentialRequest defines the strict input JSON schema for updating an existing credential.
// Only name, secret, passphrase, access_key_id, secret_access_key, session_token may be supplied.
type UpdateCredentialRequest struct {
	Name            *string `json:"name,omitempty"`
	Secret          *string `json:"secret,omitempty"`
	Passphrase      *string `json:"passphrase,omitempty"`
	AccessKeyID     *string `json:"access_key_id,omitempty"`
	SecretAccessKey *string `json:"secret_access_key,omitempty"`
	SessionToken    *string `json:"session_token,omitempty"`
}

// CredentialUpdateResponse defines the safe HTTP response schema for an updated credential.
type CredentialUpdateResponse struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Type        string    `json:"type"`
	Fingerprint *string   `json:"fingerprint"`
	KeyVersion  int       `json:"key_version"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// CredentialListItemResponse defines the safe HTTP response schema for listing credentials.
type CredentialListItemResponse struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Type        string    `json:"type"`
	Fingerprint *string   `json:"fingerprint"`
	KeyVersion  int       `json:"key_version"`
	CreatedAt   time.Time `json:"created_at"`
}
