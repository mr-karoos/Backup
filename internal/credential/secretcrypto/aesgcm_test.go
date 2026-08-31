package secretcrypto

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"testing"

	"backup-platform/pkg/uuid"
)

func setupTestEngine(t *testing.T) (*AESGCMEngine, *StaticKeyProvider, []byte) {
	t.Helper()
	masterKey := make([]byte, 32)
	if _, err := rand.Read(masterKey); err != nil {
		t.Fatalf("failed to generate random test key: %v", err)
	}

	provider, err := NewStaticKeyProvider(masterKey, 1)
	if err != nil {
		t.Fatalf("failed to construct static key provider: %v", err)
	}

	engine, err := NewAESGCMEngine(provider)
	if err != nil {
		t.Fatalf("failed to construct AESGCMEngine: %v", err)
	}

	return engine, provider, masterKey
}

func TestAESGCMEngine_RoundTrip(t *testing.T) {
	engine, _, _ := setupTestEngine(t)
	orgID := uuid.New()
	credID := uuid.New()
	ctx := EncryptionContext{
		OrganizationID: orgID,
		CredentialID:   credID,
	}

	testPlaintexts := [][]byte{
		[]byte("simple-password-123"),
		[]byte("-----BEGIN RSA PRIVATE KEY-----\nMIIEowIBAAKCAQEA0...\n-----END RSA PRIVATE KEY-----"),
		bytes.Repeat([]byte{0xDE, 0xAD, 0xBE, 0xEF}, 1024),
	}

	for _, pt := range testPlaintexts {
		enc, err := engine.Encrypt(pt, ctx)
		if err != nil {
			t.Fatalf("Encrypt failed: %v", err)
		}

		dec, err := engine.Decrypt(*enc, ctx)
		if err != nil {
			t.Fatalf("Decrypt failed: %v", err)
		}

		if !bytes.Equal(dec, pt) {
			t.Errorf("decrypted plaintext does not match original")
		}
	}
}

func TestAESGCMEngine_NonceRandomness(t *testing.T) {
	engine, _, _ := setupTestEngine(t)
	ctx := EncryptionContext{
		OrganizationID: uuid.New(),
		CredentialID:   uuid.New(),
	}
	plaintext := []byte("constant-secret-value")

	enc1, err := engine.Encrypt(plaintext, ctx)
	if err != nil {
		t.Fatalf("first encryption failed: %v", err)
	}

	enc2, err := engine.Encrypt(plaintext, ctx)
	if err != nil {
		t.Fatalf("second encryption failed: %v", err)
	}

	if bytes.Equal(enc1.Nonce, enc2.Nonce) {
		t.Errorf("SECURITY FLAW: two subsequent encryptions produced identical nonces")
	}

	if bytes.Equal(enc1.Ciphertext, enc2.Ciphertext) {
		t.Errorf("subsequent encryptions with different nonces produced identical ciphertext")
	}
}

func TestAESGCMEngine_DatabaseFieldSeparation(t *testing.T) {
	engine, _, masterKey := setupTestEngine(t)
	ctx := EncryptionContext{
		OrganizationID: uuid.New(),
		CredentialID:   uuid.New(),
	}
	plaintext := []byte("database-field-separation-test-data")

	enc, err := engine.Encrypt(plaintext, ctx)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	block, err := aes.NewCipher(masterKey)
	if err != nil {
		t.Fatalf("aes cipher initialization failed: %v", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatalf("gcm initialization failed: %v", err)
	}

	// In AES-GCM, ciphertext length exactly equals plaintext length when tag is separated
	if len(enc.Ciphertext) != len(plaintext) {
		t.Errorf("expected ciphertext length %d, got %d", len(plaintext), len(enc.Ciphertext))
	}

	// Standard GCM nonce size
	if len(enc.Nonce) != gcm.NonceSize() {
		t.Errorf("expected nonce length %d, got %d", gcm.NonceSize(), len(enc.Nonce))
	}

	// Standard GCM overhead (auth tag)
	if len(enc.AuthTag) != gcm.Overhead() {
		t.Errorf("expected auth tag length %d, got %d", gcm.Overhead(), len(enc.AuthTag))
	}

	if enc.KeyVersion != 1 {
		t.Errorf("expected key version 1, got %d", enc.KeyVersion)
	}
}

func TestAESGCMEngine_TamperDetection(t *testing.T) {
	engine, _, _ := setupTestEngine(t)
	orgID := uuid.New()
	credID := uuid.New()
	ctx := EncryptionContext{
		OrganizationID: orgID,
		CredentialID:   credID,
	}
	plaintext := []byte("tamper-resistance-secret")

	enc, err := engine.Encrypt(plaintext, ctx)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	t.Run("ciphertext tampering rejected", func(t *testing.T) {
		corrupted := *enc
		corrupted.Ciphertext = make([]byte, len(enc.Ciphertext))
		copy(corrupted.Ciphertext, enc.Ciphertext)
		corrupted.Ciphertext[0] ^= 0xFF

		_, err := engine.Decrypt(corrupted, ctx)
		if !errors.Is(err, ErrDecryptionFailed) {
			t.Errorf("expected ErrDecryptionFailed on tampered ciphertext, got: %v", err)
		}
	})

	t.Run("auth tag tampering rejected", func(t *testing.T) {
		corrupted := *enc
		corrupted.AuthTag = make([]byte, len(enc.AuthTag))
		copy(corrupted.AuthTag, enc.AuthTag)
		corrupted.AuthTag[0] ^= 0xFF

		_, err := engine.Decrypt(corrupted, ctx)
		if !errors.Is(err, ErrDecryptionFailed) {
			t.Errorf("expected ErrDecryptionFailed on tampered auth tag, got: %v", err)
		}
	})

	t.Run("nonce tampering rejected", func(t *testing.T) {
		corrupted := *enc
		corrupted.Nonce = make([]byte, len(enc.Nonce))
		copy(corrupted.Nonce, enc.Nonce)
		corrupted.Nonce[0] ^= 0xFF

		_, err := engine.Decrypt(corrupted, ctx)
		if !errors.Is(err, ErrDecryptionFailed) {
			t.Errorf("expected ErrDecryptionFailed on tampered nonce, got: %v", err)
		}
	})
}

func TestAESGCMEngine_CrossRecordSwapProtection(t *testing.T) {
	engine, _, _ := setupTestEngine(t)
	orgA := uuid.New()
	orgB := uuid.New()
	cred1 := uuid.New()
	cred2 := uuid.New()

	ctxA1 := EncryptionContext{OrganizationID: orgA, CredentialID: cred1}
	ctxA2 := EncryptionContext{OrganizationID: orgA, CredentialID: cred2}
	ctxB1 := EncryptionContext{OrganizationID: orgB, CredentialID: cred1}

	encA1, err := engine.Encrypt([]byte("secret-for-org-a-cred-1"), ctxA1)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	t.Run("decrypting with different credential ID in same org fails", func(t *testing.T) {
		_, err := engine.Decrypt(*encA1, ctxA2)
		if !errors.Is(err, ErrDecryptionFailed) {
			t.Errorf("expected ErrDecryptionFailed on cross-credential decrypt, got: %v", err)
		}
	})

	t.Run("decrypting with different organization ID fails", func(t *testing.T) {
		_, err := engine.Decrypt(*encA1, ctxB1)
		if !errors.Is(err, ErrDecryptionFailed) {
			t.Errorf("expected ErrDecryptionFailed on cross-organization decrypt, got: %v", err)
		}
	})
}

func TestAESGCMEngine_WrongKey(t *testing.T) {
	keyA := bytes.Repeat([]byte{0x11}, 32)
	keyB := bytes.Repeat([]byte{0x22}, 32)

	providerA, _ := NewStaticKeyProvider(keyA, 1)
	providerB, _ := NewStaticKeyProvider(keyB, 1)

	engineA, _ := NewAESGCMEngine(providerA)
	engineB, _ := NewAESGCMEngine(providerB)

	ctx := EncryptionContext{OrganizationID: uuid.New(), CredentialID: uuid.New()}
	enc, err := engineA.Encrypt([]byte("secret-payload"), ctx)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	_, err = engineB.Decrypt(*enc, ctx)
	if !errors.Is(err, ErrDecryptionFailed) {
		t.Errorf("expected ErrDecryptionFailed when decrypting with wrong key, got: %v", err)
	}
}

func TestAESGCMEngine_KeyVersionHandling(t *testing.T) {
	engine, _, _ := setupTestEngine(t)
	ctx := EncryptionContext{OrganizationID: uuid.New(), CredentialID: uuid.New()}

	enc, err := engine.Encrypt([]byte("secret-data"), ctx)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	t.Run("unknown key version returns ErrUnknownKeyVersion", func(t *testing.T) {
		corrupted := *enc
		corrupted.KeyVersion = 99

		_, err := engine.Decrypt(corrupted, ctx)
		if !errors.Is(err, ErrUnknownKeyVersion) {
			t.Errorf("expected ErrUnknownKeyVersion for version 99, got: %v", err)
		}
	})

	t.Run("invalid key version less than 1 returns ErrInvalidKeyVersion", func(t *testing.T) {
		corrupted := *enc
		corrupted.KeyVersion = 0

		_, err := engine.Decrypt(corrupted, ctx)
		if !errors.Is(err, ErrInvalidKeyVersion) {
			t.Errorf("expected ErrInvalidKeyVersion for version 0, got: %v", err)
		}
	})
}

func TestAESGCMEngine_ValidationAndMalformedPayloads(t *testing.T) {
	engine, _, _ := setupTestEngine(t)
	validOrgID := uuid.New()
	validCredID := uuid.New()
	validCtx := EncryptionContext{OrganizationID: validOrgID, CredentialID: validCredID}

	t.Run("nil key provider constructor fails", func(t *testing.T) {
		_, err := NewAESGCMEngine(nil)
		if !errors.Is(err, ErrNilKeyProvider) {
			t.Errorf("expected ErrNilKeyProvider, got: %v", err)
		}
	})

	t.Run("empty plaintext rejected", func(t *testing.T) {
		_, err := engine.Encrypt(nil, validCtx)
		if !errors.Is(err, ErrEmptyPlaintext) {
			t.Errorf("expected ErrEmptyPlaintext for nil slice, got: %v", err)
		}

		_, err = engine.Encrypt([]byte{}, validCtx)
		if !errors.Is(err, ErrEmptyPlaintext) {
			t.Errorf("expected ErrEmptyPlaintext for empty slice, got: %v", err)
		}
	})

	t.Run("invalid encryption context rejected on Encrypt and Decrypt", func(t *testing.T) {
		invalidContexts := []EncryptionContext{
			{OrganizationID: uuid.Nil, CredentialID: validCredID},
			{OrganizationID: validOrgID, CredentialID: uuid.Nil},
			{OrganizationID: uuid.Nil, CredentialID: uuid.Nil},
		}

		dummyEnc := EncryptedSecret{
			Ciphertext: []byte("cipher"),
			Nonce:      make([]byte, 12),
			AuthTag:    make([]byte, 16),
			KeyVersion: 1,
		}

		for _, ic := range invalidContexts {
			_, err := engine.Encrypt([]byte("data"), ic)
			if !errors.Is(err, ErrInvalidContext) {
				t.Errorf("expected ErrInvalidContext on Encrypt, got: %v", err)
			}

			_, err = engine.Decrypt(dummyEnc, ic)
			if !errors.Is(err, ErrInvalidContext) {
				t.Errorf("expected ErrInvalidContext on Decrypt, got: %v", err)
			}
		}
	})

	t.Run("empty ciphertext rejected on Decrypt", func(t *testing.T) {
		enc := EncryptedSecret{
			Ciphertext: nil,
			Nonce:      make([]byte, 12),
			AuthTag:    make([]byte, 16),
			KeyVersion: 1,
		}
		_, err := engine.Decrypt(enc, validCtx)
		if !errors.Is(err, ErrInvalidCiphertext) {
			t.Errorf("expected ErrInvalidCiphertext, got: %v", err)
		}
	})

	t.Run("invalid nonce length rejected on Decrypt", func(t *testing.T) {
		invalidNonces := [][]byte{
			nil,
			{},
			make([]byte, 8),
			make([]byte, 16),
		}

		for _, n := range invalidNonces {
			enc := EncryptedSecret{
				Ciphertext: []byte("cipher"),
				Nonce:      n,
				AuthTag:    make([]byte, 16),
				KeyVersion: 1,
			}
			_, err := engine.Decrypt(enc, validCtx)
			if !errors.Is(err, ErrInvalidNonce) {
				t.Errorf("expected ErrInvalidNonce for nonce len %d, got: %v", len(n), err)
			}
		}
	})

	t.Run("invalid auth tag length rejected on Decrypt", func(t *testing.T) {
		invalidTags := [][]byte{
			nil,
			{},
			make([]byte, 8),
			make([]byte, 12),
			make([]byte, 32),
		}

		for _, tag := range invalidTags {
			enc := EncryptedSecret{
				Ciphertext: []byte("cipher"),
				Nonce:      make([]byte, 12),
				AuthTag:    tag,
				KeyVersion: 1,
			}
			_, err := engine.Decrypt(enc, validCtx)
			if !errors.Is(err, ErrInvalidAuthTag) {
				t.Errorf("expected ErrInvalidAuthTag for tag len %d, got: %v", len(tag), err)
			}
		}
	})
}

type fakeErrorKeyProvider struct {
	currentErr   error
	byVersionErr error
}

func (f *fakeErrorKeyProvider) Current() ([]byte, int, error) {
	if f.currentErr != nil {
		return nil, 0, f.currentErr
	}
	return bytes.Repeat([]byte{0x42}, 32), 1, nil
}

func (f *fakeErrorKeyProvider) ByVersion(version int) ([]byte, error) {
	if f.byVersionErr != nil {
		return nil, f.byVersionErr
	}
	return bytes.Repeat([]byte{0x42}, 32), nil
}

func TestAESGCMEngine_ProviderErrorLeakage(t *testing.T) {
	ctx := EncryptionContext{
		OrganizationID: uuid.New(),
		CredentialID:   uuid.New(),
	}

	t.Run("Encrypt does not leak raw KeyProvider error details", func(t *testing.T) {
		fakeProvider := &fakeErrorKeyProvider{
			currentErr: errors.New("vault backend failed at secret/prod/master-key"),
		}
		engine, err := NewAESGCMEngine(fakeProvider)
		if err != nil {
			t.Fatalf("engine constructor failed: %v", err)
		}

		_, err = engine.Encrypt([]byte("sensitive-secret"), ctx)
		if err == nil {
			t.Fatalf("expected Encrypt to fail on provider error")
		}
		if !errors.Is(err, ErrEncryptionFailed) {
			t.Errorf("expected ErrEncryptionFailed, got: %v", err)
		}
		if bytes.Contains([]byte(err.Error()), []byte("vault")) ||
			bytes.Contains([]byte(err.Error()), []byte("secret/prod/master-key")) {
			t.Errorf("SECURITY FLAW: raw KeyProvider error leaked in Encrypt: %s", err.Error())
		}
	})

	t.Run("Decrypt does not leak raw KeyProvider error details", func(t *testing.T) {
		fakeProvider := &fakeErrorKeyProvider{
			byVersionErr: errors.New("kms connection refused on region us-east-1"),
		}
		engine, err := NewAESGCMEngine(fakeProvider)
		if err != nil {
			t.Fatalf("engine constructor failed: %v", err)
		}

		enc := EncryptedSecret{
			Ciphertext: []byte("cipher"),
			Nonce:      make([]byte, 12),
			AuthTag:    make([]byte, 16),
			KeyVersion: 1,
		}

		_, err = engine.Decrypt(enc, ctx)
		if err == nil {
			t.Fatalf("expected Decrypt to fail on provider error")
		}
		if !errors.Is(err, ErrDecryptionFailed) {
			t.Errorf("expected ErrDecryptionFailed, got: %v", err)
		}
		if bytes.Contains([]byte(err.Error()), []byte("kms")) ||
			bytes.Contains([]byte(err.Error()), []byte("us-east-1")) {
			t.Errorf("SECURITY FLAW: raw KeyProvider error leaked in Decrypt: %s", err.Error())
		}
	})
}

type spyKeyProvider struct {
	currentKeyRef   []byte
	byVersionKeyRef []byte
	version         int
}

func newSpyKeyProvider(masterKey []byte, version int) *spyKeyProvider {
	k1 := make([]byte, len(masterKey))
	copy(k1, masterKey)
	k2 := make([]byte, len(masterKey))
	copy(k2, masterKey)
	return &spyKeyProvider{
		currentKeyRef:   k1,
		byVersionKeyRef: k2,
		version:         version,
	}
}

func (s *spyKeyProvider) Current() ([]byte, int, error) {
	// Returns reference to slice we monitor
	return s.currentKeyRef, s.version, nil
}

func (s *spyKeyProvider) ByVersion(version int) ([]byte, error) {
	if version != s.version {
		return nil, ErrUnknownKeyVersion
	}
	// Returns reference to slice we monitor
	return s.byVersionKeyRef, nil
}

func TestAESGCMEngine_LocalKeyCopyZeroization(t *testing.T) {
	ctx := EncryptionContext{
		OrganizationID: uuid.New(),
		CredentialID:   uuid.New(),
	}
	masterKey := bytes.Repeat([]byte{0x77}, 32)
	zeroedSlice := make([]byte, 32)

	t.Run("Encrypt zeroes local key copy returned by KeyProvider", func(t *testing.T) {
		spy := newSpyKeyProvider(masterKey, 1)
		engine, err := NewAESGCMEngine(spy)
		if err != nil {
			t.Fatalf("engine constructor failed: %v", err)
		}

		_, err = engine.Encrypt([]byte("secret-payload"), ctx)
		if err != nil {
			t.Fatalf("Encrypt failed: %v", err)
		}

		if !bytes.Equal(spy.currentKeyRef, zeroedSlice) {
			t.Errorf("SECURITY FLAW: local key copy returned by Current() was not zeroed after Encrypt")
		}
	})

	t.Run("Decrypt zeroes local key copy returned by KeyProvider", func(t *testing.T) {
		// First generate a valid ciphertext with a normal engine
		normalProvider, _ := NewStaticKeyProvider(masterKey, 1)
		normalEngine, _ := NewAESGCMEngine(normalProvider)
		enc, err := normalEngine.Encrypt([]byte("secret-payload"), ctx)
		if err != nil {
			t.Fatalf("setup encrypt failed: %v", err)
		}

		spy := newSpyKeyProvider(masterKey, 1)
		engine, err := NewAESGCMEngine(spy)
		if err != nil {
			t.Fatalf("engine constructor failed: %v", err)
		}

		dec, err := engine.Decrypt(*enc, ctx)
		if err != nil {
			t.Fatalf("Decrypt failed: %v", err)
		}
		if !bytes.Equal(dec, []byte("secret-payload")) {
			t.Errorf("decrypted output mismatch")
		}

		if !bytes.Equal(spy.byVersionKeyRef, zeroedSlice) {
			t.Errorf("SECURITY FLAW: local key copy returned by ByVersion() was not zeroed after Decrypt")
		}
	})
}
