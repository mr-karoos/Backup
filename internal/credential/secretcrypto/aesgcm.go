package secretcrypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"io"

	"backup-platform/pkg/uuid"
)

const (
	// aadPrefix defines the deterministic cryptographic domain separation tag.
	aadPrefix = "backup-platform:credential:v1"
)

// EncryptedSecret represents the database-compatible storage format for an encrypted credential secret.
type EncryptedSecret struct {
	Ciphertext []byte
	Nonce      []byte
	AuthTag    []byte
	KeyVersion int
}

// EncryptionContext holds the cryptographic binding parameters (AAD) for a credential.
// Binds the ciphertext specifically to an organization and credential record.
type EncryptionContext struct {
	OrganizationID uuid.UUID
	CredentialID   uuid.UUID
}

// Engine defines the cryptographic operations for credential secrets.
type Engine interface {
	Encrypt(plaintext []byte, ctx EncryptionContext) (*EncryptedSecret, error)
	Decrypt(encrypted EncryptedSecret, ctx EncryptionContext) ([]byte, error)
}

// AESGCMEngine implements Engine using AES-256-GCM authenticated encryption.
type AESGCMEngine struct {
	keyProvider KeyProvider
}

// NewAESGCMEngine constructs a new AESGCMEngine with the given KeyProvider.
func NewAESGCMEngine(provider KeyProvider) (*AESGCMEngine, error) {
	if provider == nil {
		return nil, ErrNilKeyProvider
	}
	return &AESGCMEngine{
		keyProvider: provider,
	}, nil
}

// buildAAD constructs the deterministic Additional Authenticated Data slice.
// Layout: [29-byte prefix][16-byte org ID][16-byte cred ID]
func buildAAD(ctx EncryptionContext) ([]byte, error) {
	if ctx.OrganizationID == uuid.Nil || ctx.CredentialID == uuid.Nil {
		return nil, ErrInvalidContext
	}

	orgBytes := ctx.OrganizationID
	credBytes := ctx.CredentialID

	aad := make([]byte, 0, len(aadPrefix)+len(orgBytes)+len(credBytes))
	aad = append(aad, aadPrefix...)
	aad = append(aad, orgBytes[:]...)
	aad = append(aad, credBytes[:]...)

	return aad, nil
}

// Encrypt performs AES-256-GCM encryption on the given plaintext, generating a fresh random nonce,
// cryptographically binding the result to the EncryptionContext, and separating the ciphertext and auth tag.
func (e *AESGCMEngine) Encrypt(plaintext []byte, ctx EncryptionContext) (*EncryptedSecret, error) {
	if len(plaintext) == 0 {
		return nil, ErrEmptyPlaintext
	}

	aad, err := buildAAD(ctx)
	if err != nil {
		return nil, err
	}

	key, version, err := e.keyProvider.Current()
	if err != nil {
		return nil, ErrEncryptionFailed
	}
	defer ZeroBytes(key)

	if len(key) != 32 {
		return nil, ErrInvalidKeyLength
	}
	if version < 1 {
		return nil, ErrInvalidKeyVersion
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, ErrEncryptionFailed
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, ErrEncryptionFailed
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, ErrEncryptionFailed
	}

	sealed := gcm.Seal(nil, nonce, plaintext, aad)
	defer ZeroBytes(sealed)

	tagLen := gcm.Overhead()
	if len(sealed) < tagLen {
		return nil, ErrEncryptionFailed
	}

	ciphertextLen := len(sealed) - tagLen
	ciphertext := make([]byte, ciphertextLen)
	copy(ciphertext, sealed[:ciphertextLen])

	authTag := make([]byte, tagLen)
	copy(authTag, sealed[ciphertextLen:])

	return &EncryptedSecret{
		Ciphertext: ciphertext,
		Nonce:      nonce,
		AuthTag:    authTag,
		KeyVersion: version,
	}, nil
}

// Decrypt verifies the integrity and authenticates the ciphertext using AES-256-GCM and the specified EncryptionContext.
// Returns the decrypted plaintext on success, or a safe generic error on tampering or authentication failure.
func (e *AESGCMEngine) Decrypt(encrypted EncryptedSecret, ctx EncryptionContext) ([]byte, error) {
	if encrypted.KeyVersion < 1 {
		return nil, ErrInvalidKeyVersion
	}
	if len(encrypted.Ciphertext) == 0 {
		return nil, ErrInvalidCiphertext
	}
	if len(encrypted.Nonce) == 0 {
		return nil, ErrInvalidNonce
	}
	if len(encrypted.AuthTag) == 0 {
		return nil, ErrInvalidAuthTag
	}

	aad, err := buildAAD(ctx)
	if err != nil {
		return nil, err
	}

	key, err := e.keyProvider.ByVersion(encrypted.KeyVersion)
	if err != nil {
		if errors.Is(err, ErrUnknownKeyVersion) || errors.Is(err, ErrInvalidKeyVersion) {
			return nil, err
		}
		return nil, ErrDecryptionFailed
	}
	defer ZeroBytes(key)

	if len(key) != 32 {
		return nil, ErrInvalidKeyLength
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, ErrDecryptionFailed
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, ErrDecryptionFailed
	}

	if len(encrypted.Nonce) != gcm.NonceSize() {
		return nil, ErrInvalidNonce
	}
	if len(encrypted.AuthTag) != gcm.Overhead() {
		return nil, ErrInvalidAuthTag
	}

	combined := make([]byte, len(encrypted.Ciphertext)+len(encrypted.AuthTag))
	copy(combined, encrypted.Ciphertext)
	copy(combined[len(encrypted.Ciphertext):], encrypted.AuthTag)
	defer ZeroBytes(combined)

	plaintext, err := gcm.Open(nil, encrypted.Nonce, combined, aad)
	if err != nil {
		return nil, ErrDecryptionFailed
	}

	out := make([]byte, len(plaintext))
	copy(out, plaintext)
	ZeroBytes(plaintext)

	return out, nil
}
