package secretcrypto

import "errors"

var (
	// ErrInvalidKeyLength indicates the encryption key is not exactly 32 bytes (256 bits).
	ErrInvalidKeyLength = errors.New("secretcrypto: master key must be exactly 32 bytes")

	// ErrInvalidKeyVersion indicates the key version number is less than 1.
	ErrInvalidKeyVersion = errors.New("secretcrypto: key version must be greater than or equal to 1")

	// ErrUnknownKeyVersion indicates a decryption request for a key version not present in the key provider.
	ErrUnknownKeyVersion = errors.New("secretcrypto: unknown key version")

	// ErrNilKeyProvider indicates a nil KeyProvider was passed to the crypto engine constructor.
	ErrNilKeyProvider = errors.New("secretcrypto: key provider cannot be nil")

	// ErrEmptyPlaintext indicates an encryption request was made with empty plaintext data.
	ErrEmptyPlaintext = errors.New("secretcrypto: plaintext cannot be empty")

	// ErrInvalidContext indicates the encryption context has missing or zero OrganizationID/CredentialID.
	ErrInvalidContext = errors.New("secretcrypto: encryption context requires valid non-nil organization_id and credential_id")

	// ErrInvalidCiphertext indicates the ciphertext payload is empty or malformed.
	ErrInvalidCiphertext = errors.New("secretcrypto: ciphertext is empty or invalid")

	// ErrInvalidNonce indicates the nonce size does not match the required GCM standard nonce length.
	ErrInvalidNonce = errors.New("secretcrypto: nonce length is invalid")

	// ErrInvalidAuthTag indicates the authentication tag size does not match the required GCM tag overhead.
	ErrInvalidAuthTag = errors.New("secretcrypto: auth tag length is invalid")

	// ErrEncryptionFailed indicates an unexpected failure during encryption or nonce generation.
	ErrEncryptionFailed = errors.New("secretcrypto: encryption operation failed")

	// ErrDecryptionFailed indicates authentication failure, tampering, or incorrect key/context during decryption.
	ErrDecryptionFailed = errors.New("secretcrypto: decryption operation failed")
)
