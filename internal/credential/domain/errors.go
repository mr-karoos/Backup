package domain

import "errors"

var (
	// ErrInvalidCredentialName indicates the credential name is empty or exceeds 100 runes.
	ErrInvalidCredentialName = errors.New("credential: name must be between 1 and 100 characters")

	// ErrInvalidCredentialType indicates an unsupported or unknown credential type.
	ErrInvalidCredentialType = errors.New("credential: unsupported or invalid credential type")

	// ErrInvalidOrganizationID indicates a nil or invalid organization UUID was provided.
	ErrInvalidOrganizationID = errors.New("credential: valid organization id is required")

	// ErrInvalidCredentialID indicates a nil or invalid credential UUID was provided.
	ErrInvalidCredentialID = errors.New("credential: valid credential id is required")

	// ErrEmptyPlaintextSecret indicates an encryption attempt with empty plaintext secret bytes.
	ErrEmptyPlaintextSecret = errors.New("credential: secret payload cannot be empty")

	// ErrCredentialNotFound indicates the credential does not exist or does not belong to the organization.
	ErrCredentialNotFound = errors.New("credential: not found")

	// ErrCredentialServiceUnavailable indicates an underlying database or infrastructure failure.
	ErrCredentialServiceUnavailable = errors.New("credential: service temporarily unavailable")

	// ErrCredentialEncryptionFailed indicates cryptographic encryption failed before storage.
	ErrCredentialEncryptionFailed = errors.New("credential: encryption operation failed")

	// ErrCredentialSecretUnavailable indicates authentication failure, tampering, or missing key during decryption.
	ErrCredentialSecretUnavailable = errors.New("credential: secret cannot be decrypted or authenticated")

	// ErrInvalidSSHKey indicates the SSH private key format or passphrase is invalid.
	ErrInvalidSSHKey = errors.New("credential: invalid SSH private key or passphrase")

	// ErrCredentialInUse indicates the credential cannot be deleted because it is referenced by one or more resource connectors.
	ErrCredentialInUse = errors.New("credential: in use by one or more resource connectors")

	// ErrSystemCredentialRestricted indicates an attempt by a user to create, modify, or delete a system-managed credential.
	ErrSystemCredentialRestricted = errors.New("credential: system-managed credentials cannot be managed directly")
)
