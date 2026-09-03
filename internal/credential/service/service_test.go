package service

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"backup-platform/internal/credential/domain"
	"backup-platform/internal/credential/secretcrypto"
	"backup-platform/internal/platform/database"
	"backup-platform/pkg/uuid"
)

type fakeTxManager struct{}

func (f *fakeTxManager) Querier() database.Querier {
	return nil
}

func (f *fakeTxManager) WithinTx(ctx context.Context, fn func(q database.Querier) error) error {
	return fn(nil)
}

type fakeRepo struct {
	createErr                  error
	findErr                    error
	findMetaErr                error
	listErr                    error
	updateNameErr              error
	updateEncryptedErr         error
	deleteErr                  error
	savedCred                  *domain.Credential
	listItems                  []*domain.CredentialMetadata
	createCalls                int
	findCalls                  int
	findMetaCalls              int
	listCalls                  int
	updateNameCalls            int
	updateEncryptedCalls       int
	deleteCalls                int
	bufferSnapshotAtCreateTime []byte
	bufferSnapshotAtUpdateTime []byte
	monitoredBuffer            *[]byte
}

func (f *fakeRepo) Create(ctx context.Context, q database.Querier, cred *domain.Credential) error {
	f.createCalls++
	f.savedCred = cred
	if f.monitoredBuffer != nil && *f.monitoredBuffer != nil {
		buf := *f.monitoredBuffer
		snap := make([]byte, len(buf))
		copy(snap, buf)
		f.bufferSnapshotAtCreateTime = snap
	}
	return f.createErr
}

func (f *fakeRepo) FindEncryptedByIDForOrganization(ctx context.Context, q database.Querier, orgID uuid.UUID, credID uuid.UUID) (*domain.Credential, error) {
	f.findCalls++
	if f.findErr != nil {
		return nil, f.findErr
	}
	if f.savedCred != nil {
		if f.savedCred.OrganizationID != orgID || f.savedCred.ID != credID {
			return nil, domain.ErrCredentialNotFound
		}
		return f.savedCred, nil
	}
	return nil, domain.ErrCredentialNotFound
}

func (f *fakeRepo) FindMetadataForOrganization(ctx context.Context, q database.Querier, orgID uuid.UUID, credID uuid.UUID) (*domain.CredentialMetadata, error) {
	f.findMetaCalls++
	if f.findMetaErr != nil {
		return nil, f.findMetaErr
	}
	if f.savedCred != nil {
		if f.savedCred.OrganizationID != orgID || f.savedCred.ID != credID {
			return nil, domain.ErrCredentialNotFound
		}
		return &domain.CredentialMetadata{
			ID:             f.savedCred.ID,
			OrganizationID: f.savedCred.OrganizationID,
			Name:           f.savedCred.Name,
			Type:           f.savedCred.Type,
			ManagedBy:      f.savedCred.ManagedBy,
			Fingerprint:    f.savedCred.Fingerprint,
			KeyVersion:     f.savedCred.KeyVersion,
			CreatedAt:      f.savedCred.CreatedAt,
			UpdatedAt:      f.savedCred.UpdatedAt,
		}, nil
	}
	return nil, domain.ErrCredentialNotFound
}

func (f *fakeRepo) ListMetadataForOrganization(ctx context.Context, q database.Querier, orgID uuid.UUID) ([]*domain.CredentialMetadata, error) {
	f.listCalls++
	if f.listErr != nil {
		return nil, f.listErr
	}
	if f.listItems != nil {
		return f.listItems, nil
	}
	return []*domain.CredentialMetadata{}, nil
}

func (f *fakeRepo) UpdateNameForOrganization(ctx context.Context, q database.Querier, orgID, credID uuid.UUID, name string) (*domain.CredentialMetadata, error) {
	f.updateNameCalls++
	if f.updateNameErr != nil {
		return nil, f.updateNameErr
	}
	if f.savedCred != nil {
		if f.savedCred.OrganizationID != orgID || f.savedCred.ID != credID {
			return nil, domain.ErrCredentialNotFound
		}
		f.savedCred.Name = name
		f.savedCred.UpdatedAt = time.Now().UTC()
		return &domain.CredentialMetadata{
			ID:             f.savedCred.ID,
			OrganizationID: f.savedCred.OrganizationID,
			Name:           f.savedCred.Name,
			Type:           f.savedCred.Type,
			Fingerprint:    f.savedCred.Fingerprint,
			KeyVersion:     f.savedCred.KeyVersion,
			CreatedAt:      f.savedCred.CreatedAt,
			UpdatedAt:      f.savedCred.UpdatedAt,
		}, nil
	}
	return nil, domain.ErrCredentialNotFound
}

func (f *fakeRepo) UpdateEncryptedForOrganization(ctx context.Context, q database.Querier, cred *domain.Credential) (*domain.CredentialMetadata, error) {
	f.updateEncryptedCalls++
	f.savedCred = cred
	if f.monitoredBuffer != nil && *f.monitoredBuffer != nil {
		buf := *f.monitoredBuffer
		snap := make([]byte, len(buf))
		copy(snap, buf)
		f.bufferSnapshotAtUpdateTime = snap
	}
	if f.updateEncryptedErr != nil {
		return nil, f.updateEncryptedErr
	}
	return &domain.CredentialMetadata{
		ID:             cred.ID,
		OrganizationID: cred.OrganizationID,
		Name:           cred.Name,
		Type:           cred.Type,
		Fingerprint:    cred.Fingerprint,
		KeyVersion:     cred.KeyVersion,
		CreatedAt:      cred.CreatedAt,
		UpdatedAt:      cred.UpdatedAt,
	}, nil
}

func (f *fakeRepo) DeleteForOrganization(ctx context.Context, q database.Querier, orgID, credID uuid.UUID) error {
	f.deleteCalls++
	if f.deleteErr != nil {
		return f.deleteErr
	}
	if f.savedCred != nil {
		if f.savedCred.OrganizationID != orgID || f.savedCred.ID != credID {
			return domain.ErrCredentialNotFound
		}
		f.savedCred = nil
		return nil
	}
	return domain.ErrCredentialNotFound
}

type spyCryptoEngine struct {
	encryptErr            error
	decryptErr            error
	encryptCalls          int
	decryptCalls          int
	receivedPlaintextRef  []byte
	capturedEncryptCtx    secretcrypto.EncryptionContext
	capturedDecryptSecret secretcrypto.EncryptedSecret
	capturedDecryptCtx    secretcrypto.EncryptionContext
}

func (s *spyCryptoEngine) Encrypt(plaintext []byte, ctx secretcrypto.EncryptionContext) (*secretcrypto.EncryptedSecret, error) {
	s.encryptCalls++
	s.receivedPlaintextRef = plaintext
	s.capturedEncryptCtx = ctx
	if s.encryptErr != nil {
		return nil, s.encryptErr
	}
	return &secretcrypto.EncryptedSecret{
		Ciphertext: []byte("mock-ciphertext-bytes"),
		Nonce:      make([]byte, 12),
		AuthTag:    make([]byte, 16),
		KeyVersion: 1,
	}, nil
}

func (s *spyCryptoEngine) Decrypt(encrypted secretcrypto.EncryptedSecret, ctx secretcrypto.EncryptionContext) ([]byte, error) {
	s.decryptCalls++
	s.capturedDecryptSecret = encrypted
	s.capturedDecryptCtx = ctx
	if s.decryptErr != nil {
		return nil, s.decryptErr
	}
	return []byte("decrypted-mock-plaintext"), nil
}

func TestVaultService_CreateCredential_Success(t *testing.T) {
	ctx := context.Background()
	crypto := &spyCryptoEngine{}
	repo := &fakeRepo{}
	svc := NewVaultService(crypto, repo, &fakeTxManager{}, nil)

	orgID := uuid.New()
	secret := []byte("TOP-SECRET-SSH-PASSWORD")
	fp := "SHA256:somefingerprint"

	meta, err := svc.CreateCredential(ctx, orgID, "Production Key", domain.TypeSSHPrivateKey, secret, &fp)
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}

	if meta.ID == uuid.Nil {
		t.Errorf("expected non-nil generated ID")
	}
	if meta.OrganizationID != orgID {
		t.Errorf("expected org ID %v, got %v", orgID, meta.OrganizationID)
	}
	if meta.Name != "Production Key" {
		t.Errorf("expected name 'Production Key', got %s", meta.Name)
	}
	if meta.Type != domain.TypeSSHPrivateKey {
		t.Errorf("expected type %s, got %s", domain.TypeSSHPrivateKey, meta.Type)
	}
	if meta.Fingerprint == nil || *meta.Fingerprint != fp {
		t.Errorf("expected fingerprint %s, got %v", fp, meta.Fingerprint)
	}
	if meta.KeyVersion != 1 {
		t.Errorf("expected key version 1, got %d", meta.KeyVersion)
	}

	// Verify encryption context matched the generated ID and org ID
	if crypto.capturedEncryptCtx.OrganizationID != orgID {
		t.Errorf("expected encryption context org %v, got %v", orgID, crypto.capturedEncryptCtx.OrganizationID)
	}
	if crypto.capturedEncryptCtx.CredentialID != meta.ID {
		t.Errorf("expected encryption context cred ID %v, got %v", meta.ID, crypto.capturedEncryptCtx.CredentialID)
	}

	// Verify repo received encrypted fields
	if repo.createCalls != 1 || repo.savedCred == nil {
		t.Fatalf("expected 1 repo create call")
	}
	if repo.savedCred.ID != meta.ID {
		t.Errorf("repo saved ID does not match generated ID")
	}
	if repo.savedCred.OrganizationID != orgID {
		t.Errorf("repo saved OrgID does not match")
	}
	if !bytes.Equal(repo.savedCred.EncryptedSecret, []byte("mock-ciphertext-bytes")) {
		t.Errorf("repo did not receive expected ciphertext")
	}
	if repo.savedCred.Fingerprint == nil || *repo.savedCred.Fingerprint != fp {
		t.Errorf("repo did not receive expected fingerprint")
	}
}

func TestVaultService_CreateCredential_PlaintextNotPersisted(t *testing.T) {
	ctx := context.Background()
	crypto := &spyCryptoEngine{}
	repo := &fakeRepo{}
	svc := NewVaultService(crypto, repo, &fakeTxManager{}, nil)

	orgID := uuid.New()
	secret := []byte("TOP-SECRET-SSH-PASSWORD")

	_, err := svc.CreateCredential(ctx, orgID, "Test", domain.TypeSSHPassword, secret, nil)
	if err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	if bytes.Contains(repo.savedCred.EncryptedSecret, secret) {
		t.Errorf("SECURITY FLAW: plaintext secret found inside EncryptedSecret field")
	}
	if bytes.Contains(repo.savedCred.Nonce, secret) || bytes.Contains(repo.savedCred.AuthTag, secret) {
		t.Errorf("SECURITY FLAW: plaintext secret found inside nonce or auth tag")
	}
}

func TestVaultService_CreateCredential_LocalPlaintextZeroization(t *testing.T) {
	ctx := context.Background()
	crypto := &spyCryptoEngine{}
	repo := &fakeRepo{
		monitoredBuffer: &crypto.receivedPlaintextRef,
	}
	svc := NewVaultService(crypto, repo, &fakeTxManager{}, nil)

	orgID := uuid.New()
	originalCallerSlice := []byte("IMMUTABLE-CALLER-SECRET")
	callerSliceCopy := make([]byte, len(originalCallerSlice))
	copy(callerSliceCopy, originalCallerSlice)

	_, err := svc.CreateCredential(ctx, orgID, "Test", domain.TypeSSHPassword, callerSliceCopy, nil)
	if err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	// 1. Caller slice must not be modified by service
	if !bytes.Equal(callerSliceCopy, originalCallerSlice) {
		t.Errorf("SECURITY FLAW: service modified the caller's slice directly")
	}

	// 2. When repository Create was invoked, the plaintext buffer was ALREADY zeroed
	allZero := make([]byte, len(originalCallerSlice))
	if !bytes.Equal(repo.bufferSnapshotAtCreateTime, allZero) {
		t.Errorf("SECURITY FLAW: plaintext buffer was not zeroed before repository persistence (snapshot at Create: %v)", repo.bufferSnapshotAtCreateTime)
	}

	// 3. Service local buffer passed to crypto engine must be zeroed after CreateCredential completes
	if !bytes.Equal(crypto.receivedPlaintextRef, allZero) {
		t.Errorf("SECURITY FLAW: local buffer was not zeroed after encryption")
	}
}

func TestVaultService_CreateCredential_EncryptFailure(t *testing.T) {
	ctx := context.Background()
	crypto := &spyCryptoEngine{
		encryptErr: errors.New("raw internal crypto crash at /vault/keys"),
	}
	repo := &fakeRepo{}
	svc := NewVaultService(crypto, repo, &fakeTxManager{}, nil)

	orgID := uuid.New()
	originalCallerSlice := []byte("secret-to-zero")
	callerSliceCopy := make([]byte, len(originalCallerSlice))
	copy(callerSliceCopy, originalCallerSlice)

	_, err := svc.CreateCredential(ctx, orgID, "Test", domain.TypeSSHPassword, callerSliceCopy, nil)
	if err == nil {
		t.Fatalf("expected error on encryption failure")
	}

	if !errors.Is(err, domain.ErrCredentialEncryptionFailed) {
		t.Errorf("expected domain.ErrCredentialEncryptionFailed, got: %v", err)
	}

	if strings.Contains(err.Error(), "/vault/keys") {
		t.Errorf("SECURITY FLAW: raw crypto error details leaked in service error: %s", err.Error())
	}

	if repo.createCalls != 0 {
		t.Errorf("expected 0 repo create calls on encryption failure, got %d", repo.createCalls)
	}

	// Plaintext buffer must be zeroed immediately upon encryption failure
	allZero := make([]byte, len(originalCallerSlice))
	if !bytes.Equal(crypto.receivedPlaintextRef, allZero) {
		t.Errorf("SECURITY FLAW: local plaintext buffer was not zeroed on encryption failure")
	}

	// Caller slice remains untouched
	if !bytes.Equal(callerSliceCopy, originalCallerSlice) {
		t.Errorf("SECURITY FLAW: caller slice was mutated on encryption failure")
	}
}

func TestVaultService_CreateCredential_RepositoryFailure(t *testing.T) {
	ctx := context.Background()
	crypto := &spyCryptoEngine{}
	repo := &fakeRepo{
		createErr: errors.New("pq: connection reset by peer at 10.0.0.1:5432"),
	}
	repo.monitoredBuffer = &crypto.receivedPlaintextRef
	svc := NewVaultService(crypto, repo, &fakeTxManager{}, nil)

	orgID := uuid.New()
	originalCallerSlice := []byte("secret-for-failed-db")
	callerSliceCopy := make([]byte, len(originalCallerSlice))
	copy(callerSliceCopy, originalCallerSlice)

	_, err := svc.CreateCredential(ctx, orgID, "Test", domain.TypeSSHPassword, callerSliceCopy, nil)
	if err == nil {
		t.Fatalf("expected error on repo failure")
	}

	if !errors.Is(err, domain.ErrCredentialServiceUnavailable) {
		t.Errorf("expected domain.ErrCredentialServiceUnavailable, got: %v", err)
	}

	if strings.Contains(err.Error(), "10.0.0.1") || strings.Contains(err.Error(), "connection reset") {
		t.Errorf("SECURITY FLAW: raw database error details leaked in service error: %s", err.Error())
	}

	// Plaintext buffer was already zeroed when repo was called
	allZero := make([]byte, len(originalCallerSlice))
	if !bytes.Equal(repo.bufferSnapshotAtCreateTime, allZero) {
		t.Errorf("SECURITY FLAW: local buffer was not zeroed prior to repository failure")
	}

	// Caller slice remains untouched
	if !bytes.Equal(callerSliceCopy, originalCallerSlice) {
		t.Errorf("SECURITY FLAW: caller slice was mutated on repo failure")
	}
}

func TestVaultService_CreateCredential_InputValidations(t *testing.T) {
	ctx := context.Background()
	crypto := &spyCryptoEngine{}
	repo := &fakeRepo{}
	svc := NewVaultService(crypto, repo, &fakeTxManager{}, nil)
	validOrg := uuid.New()

	t.Run("nil org ID rejected", func(t *testing.T) {
		_, err := svc.CreateCredential(ctx, uuid.Nil, "Name", domain.TypeSSHPassword, []byte("secret"), nil)
		if !errors.Is(err, domain.ErrInvalidOrganizationID) {
			t.Errorf("expected ErrInvalidOrganizationID, got: %v", err)
		}
	})

	t.Run("invalid name rejected", func(t *testing.T) {
		_, err := svc.CreateCredential(ctx, validOrg, "   ", domain.TypeSSHPassword, []byte("secret"), nil)
		if !errors.Is(err, domain.ErrInvalidCredentialName) {
			t.Errorf("expected ErrInvalidCredentialName, got: %v", err)
		}
	})

	t.Run("invalid type rejected", func(t *testing.T) {
		_, err := svc.CreateCredential(ctx, validOrg, "Name", "unknown_type", []byte("secret"), nil)
		if !errors.Is(err, domain.ErrInvalidCredentialType) {
			t.Errorf("expected ErrInvalidCredentialType, got: %v", err)
		}
	})

	t.Run("empty secret payload rejected", func(t *testing.T) {
		_, err := svc.CreateCredential(ctx, validOrg, "Name", domain.TypeSSHPassword, nil, nil)
		if !errors.Is(err, domain.ErrEmptyPlaintextSecret) {
			t.Errorf("expected ErrEmptyPlaintextSecret for nil, got: %v", err)
		}

		_, err = svc.CreateCredential(ctx, validOrg, "Name", domain.TypeSSHPassword, []byte{}, nil)
		if !errors.Is(err, domain.ErrEmptyPlaintextSecret) {
			t.Errorf("expected ErrEmptyPlaintextSecret for empty slice, got: %v", err)
		}
	})
}

func TestVaultService_ListCredentialMetadata(t *testing.T) {
	ctx := context.Background()
	orgID := uuid.New()
	now := time.Now().UTC()
	fp := "SHA256:fingerprint-test"

	t.Run("successfully retrieves metadata list", func(t *testing.T) {
		repo := &fakeRepo{
			listItems: []*domain.CredentialMetadata{
				{
					ID:             uuid.New(),
					OrganizationID: orgID,
					Name:           "Key 1",
					Type:           domain.TypeSSHPrivateKey,
					Fingerprint:    &fp,
					KeyVersion:     1,
					CreatedAt:      now,
					UpdatedAt:      now,
				},
			},
		}
		svc := NewVaultService(&spyCryptoEngine{}, repo, &fakeTxManager{}, nil)

		items, err := svc.ListCredentialMetadata(ctx, orgID)
		if err != nil {
			t.Fatalf("expected success, got: %v", err)
		}

		if len(items) != 1 {
			t.Fatalf("expected 1 item, got %d", len(items))
		}
		if items[0].Name != "Key 1" || *items[0].Fingerprint != fp {
			t.Errorf("unexpected item: %+v", items[0])
		}
	})

	t.Run("rejects nil organization ID", func(t *testing.T) {
		svc := NewVaultService(&spyCryptoEngine{}, &fakeRepo{}, &fakeTxManager{}, nil)
		_, err := svc.ListCredentialMetadata(ctx, uuid.Nil)
		if !errors.Is(err, domain.ErrInvalidOrganizationID) {
			t.Errorf("expected ErrInvalidOrganizationID, got: %v", err)
		}
	})

	t.Run("converts repository failure to safe ErrCredentialServiceUnavailable", func(t *testing.T) {
		repo := &fakeRepo{
			listErr: errors.New("db connection down at 192.168.1.1:5432"),
		}
		svc := NewVaultService(&spyCryptoEngine{}, repo, &fakeTxManager{}, nil)

		_, err := svc.ListCredentialMetadata(ctx, orgID)
		if !errors.Is(err, domain.ErrCredentialServiceUnavailable) {
			t.Errorf("expected ErrCredentialServiceUnavailable, got: %v", err)
		}
		if strings.Contains(err.Error(), "192.168.1.1") {
			t.Errorf("SECURITY FLAW: raw repo error leaked: %s", err.Error())
		}
	})
}

func TestVaultService_LoadSecretForUse(t *testing.T) {
	ctx := context.Background()
	orgID := uuid.New()
	credID := uuid.New()
	now := time.Now().UTC()

	t.Run("successfully retrieves and decrypts secret", func(t *testing.T) {
		crypto := &spyCryptoEngine{}
		repo := &fakeRepo{
			savedCred: &domain.Credential{
				ID:              credID,
				OrganizationID:  orgID,
				Name:            "SSH Key",
				Type:            domain.TypeSSHPrivateKey,
				EncryptedSecret: []byte("enc-secret"),
				Nonce:           make([]byte, 12),
				AuthTag:         make([]byte, 16),
				KeyVersion:      1,
				CreatedAt:       now,
				UpdatedAt:       now,
			},
		}
		svc := NewVaultService(crypto, repo, &fakeTxManager{}, nil)

		pt, err := svc.LoadSecretForUse(ctx, orgID, credID)
		if err != nil {
			t.Fatalf("expected success, got: %v", err)
		}
		if !bytes.Equal(pt, []byte("decrypted-mock-plaintext")) {
			t.Errorf("unexpected decrypted plaintext: %s", string(pt))
		}
		if crypto.capturedDecryptCtx.OrganizationID != orgID || crypto.capturedDecryptCtx.CredentialID != credID {
			t.Errorf("unexpected decrypt context: %+v", crypto.capturedDecryptCtx)
		}
	})

	t.Run("returns ErrCredentialNotFound when repo returns not found or org mismatch", func(t *testing.T) {
		crypto := &spyCryptoEngine{}
		repo := &fakeRepo{
			savedCred: &domain.Credential{
				ID:             credID,
				OrganizationID: orgID, // Org A
			},
		}
		svc := NewVaultService(crypto, repo, &fakeTxManager{}, nil)

		otherOrgID := uuid.New() // Org B
		_, err := svc.LoadSecretForUse(ctx, otherOrgID, credID)
		if !errors.Is(err, domain.ErrCredentialNotFound) {
			t.Errorf("expected ErrCredentialNotFound on tenant mismatch, got: %v", err)
		}
	})

	t.Run("returns ErrCredentialSecretUnavailable when decryption fails", func(t *testing.T) {
		crypto := &spyCryptoEngine{
			decryptErr: errors.New("raw gcm open failure: tag mismatch"),
		}
		repo := &fakeRepo{
			savedCred: &domain.Credential{
				ID:             credID,
				OrganizationID: orgID,
			},
		}
		svc := NewVaultService(crypto, repo, &fakeTxManager{}, nil)

		_, err := svc.LoadSecretForUse(ctx, orgID, credID)
		if !errors.Is(err, domain.ErrCredentialSecretUnavailable) {
			t.Errorf("expected ErrCredentialSecretUnavailable, got: %v", err)
		}
		if strings.Contains(err.Error(), "gcm open") {
			t.Errorf("SECURITY FLAW: raw crypto error leaked: %s", err.Error())
		}
	})

	t.Run("returns ErrCredentialServiceUnavailable when db fails", func(t *testing.T) {
		crypto := &spyCryptoEngine{}
		repo := &fakeRepo{
			findErr: errors.New("db disk full"),
		}
		svc := NewVaultService(crypto, repo, &fakeTxManager{}, nil)

		_, err := svc.LoadSecretForUse(ctx, orgID, credID)
		if !errors.Is(err, domain.ErrCredentialServiceUnavailable) {
			t.Errorf("expected ErrCredentialServiceUnavailable, got: %v", err)
		}
	})

	t.Run("validates input IDs", func(t *testing.T) {
		svc := NewVaultService(&spyCryptoEngine{}, &fakeRepo{}, &fakeTxManager{}, nil)

		_, err := svc.LoadSecretForUse(ctx, uuid.Nil, credID)
		if !errors.Is(err, domain.ErrInvalidOrganizationID) {
			t.Errorf("expected ErrInvalidOrganizationID, got: %v", err)
		}

		_, err = svc.LoadSecretForUse(ctx, orgID, uuid.Nil)
		if !errors.Is(err, domain.ErrInvalidCredentialID) {
			t.Errorf("expected ErrInvalidCredentialID, got: %v", err)
		}
	})
}

func TestVaultService_AADIntegrity_WithRealCryptoEngine(t *testing.T) {
	ctx := context.Background()
	masterKey := bytes.Repeat([]byte{0x55}, 32)
	provider, err := secretcrypto.NewStaticKeyProvider(masterKey, 1)
	if err != nil {
		t.Fatalf("provider setup failed: %v", err)
	}
	realEngine, err := secretcrypto.NewAESGCMEngine(provider)
	if err != nil {
		t.Fatalf("real crypto engine setup failed: %v", err)
	}

	repo := &fakeRepo{}
	svc := NewVaultService(realEngine, repo, &fakeTxManager{}, nil)

	orgA := uuid.New()
	originalSecret := []byte("REAL-CRYPTO-TEST-SECRET-123")

	meta, err := svc.CreateCredential(ctx, orgA, "Production Secret", domain.TypeSSHPassword, originalSecret, nil)
	if err != nil {
		t.Fatalf("CreateCredential failed: %v", err)
	}

	// 1. Decrypt with correct Org and CredID succeeds
	decrypted, err := svc.LoadSecretForUse(ctx, orgA, meta.ID)
	if err != nil {
		t.Fatalf("LoadSecretForUse failed: %v", err)
	}
	if !bytes.Equal(decrypted, originalSecret) {
		t.Errorf("decrypted secret mismatch")
	}

	// 2. Attempting to decrypt the stored ciphertext under another organization fails at the crypto AAD level
	encSecret := secretcrypto.EncryptedSecret{
		Ciphertext: repo.savedCred.EncryptedSecret,
		Nonce:      repo.savedCred.Nonce,
		AuthTag:    repo.savedCred.AuthTag,
		KeyVersion: repo.savedCred.KeyVersion,
	}

	orgB := uuid.New()
	wrongAADCtx := secretcrypto.EncryptionContext{
		OrganizationID: orgB,
		CredentialID:   meta.ID,
	}

	_, err = realEngine.Decrypt(encSecret, wrongAADCtx)
	if !errors.Is(err, secretcrypto.ErrDecryptionFailed) {
		t.Errorf("expected ErrDecryptionFailed when tampering AAD organization ID, got: %v", err)
	}

	wrongCredCtx := secretcrypto.EncryptionContext{
		OrganizationID: orgA,
		CredentialID:   uuid.New(),
	}
	_, err = realEngine.Decrypt(encSecret, wrongCredCtx)
	if !errors.Is(err, secretcrypto.ErrDecryptionFailed) {
		t.Errorf("expected ErrDecryptionFailed when tampering AAD credential ID, got: %v", err)
	}
}

func TestVaultService_GetCredentialMetadata(t *testing.T) {
	ctx := context.Background()
	orgID := uuid.New()
	credID := uuid.New()
	repo := &fakeRepo{
		savedCred: &domain.Credential{
			ID:             credID,
			OrganizationID: orgID,
			Name:           "Test Key",
			Type:           domain.TypeSSHPrivateKey,
		},
	}
	svc := NewVaultService(&spyCryptoEngine{}, repo, &fakeTxManager{}, nil)

	t.Run("successfully retrieves metadata", func(t *testing.T) {
		meta, err := svc.GetCredentialMetadata(ctx, orgID, credID)
		if err != nil {
			t.Fatalf("expected success, got: %v", err)
		}
		if meta.ID != credID || meta.Name != "Test Key" {
			t.Errorf("unexpected metadata: %+v", meta)
		}
	})

	t.Run("returns ErrCredentialNotFound when not found", func(t *testing.T) {
		_, err := svc.GetCredentialMetadata(ctx, orgID, uuid.New())
		if !errors.Is(err, domain.ErrCredentialNotFound) {
			t.Errorf("expected ErrCredentialNotFound, got: %v", err)
		}
	})

	t.Run("returns ErrCredentialServiceUnavailable on db failure", func(t *testing.T) {
		repoWithErr := &fakeRepo{findMetaErr: errors.New("db down")}
		svcErr := NewVaultService(&spyCryptoEngine{}, repoWithErr, &fakeTxManager{}, nil)
		_, err := svcErr.GetCredentialMetadata(ctx, orgID, credID)
		if !errors.Is(err, domain.ErrCredentialServiceUnavailable) {
			t.Errorf("expected ErrCredentialServiceUnavailable, got: %v", err)
		}
	})
}

func TestVaultService_UpdateCredentialName(t *testing.T) {
	ctx := context.Background()
	orgID := uuid.New()
	credID := uuid.New()
	crypto := &spyCryptoEngine{}
	repo := &fakeRepo{
		savedCred: &domain.Credential{
			ID:              credID,
			OrganizationID:  orgID,
			Name:            "Old Name",
			Type:            domain.TypeSSHPassword,
			EncryptedSecret: []byte("cipher"),
		},
	}
	svc := NewVaultService(crypto, repo, &fakeTxManager{}, nil)

	t.Run("successfully updates name without crypto operations", func(t *testing.T) {
		meta, err := svc.UpdateCredentialName(ctx, orgID, credID, "New Name")
		if err != nil {
			t.Fatalf("expected success, got: %v", err)
		}
		if meta.Name != "New Name" {
			t.Errorf("expected updated name 'New Name', got %s", meta.Name)
		}
		if crypto.encryptCalls != 0 || crypto.decryptCalls != 0 {
			t.Errorf("SECURITY FLAW: crypto operations invoked during name-only update (encrypt: %d, decrypt: %d)", crypto.encryptCalls, crypto.decryptCalls)
		}
		if repo.updateNameCalls != 1 {
			t.Errorf("expected 1 updateName call")
		}
	})

	t.Run("fails validation on invalid name", func(t *testing.T) {
		_, err := svc.UpdateCredentialName(ctx, orgID, credID, "")
		if !errors.Is(err, domain.ErrInvalidCredentialName) {
			t.Errorf("expected ErrInvalidCredentialName, got: %v", err)
		}
	})

	t.Run("returns ErrCredentialNotFound when credential does not exist", func(t *testing.T) {
		_, err := svc.UpdateCredentialName(ctx, orgID, uuid.New(), "Valid Name")
		if !errors.Is(err, domain.ErrCredentialNotFound) {
			t.Errorf("expected ErrCredentialNotFound, got: %v", err)
		}
	})
}

func TestVaultService_ReplaceCredentialSecret(t *testing.T) {
	ctx := context.Background()
	orgID := uuid.New()
	credID := uuid.New()
	crypto := &spyCryptoEngine{}
	repo := &fakeRepo{
		savedCred: &domain.Credential{
			ID:              credID,
			OrganizationID:  orgID,
			Name:            "Original Key",
			Type:            domain.TypeSSHPrivateKey,
			EncryptedSecret: []byte("old-cipher"),
			KeyVersion:      1,
		},
	}
	svc := NewVaultService(crypto, repo, &fakeTxManager{}, nil)

	t.Run("successfully replaces secret and preserves existing ID in AAD", func(t *testing.T) {
		newSecret := []byte("new-secret-payload")
		newFP := "SHA256:newfp"
		newName := "Updated Key Name"

		meta, err := svc.ReplaceCredentialSecret(ctx, orgID, credID, &newName, newSecret, &newFP)
		if err != nil {
			t.Fatalf("expected success, got: %v", err)
		}

		if meta.ID != credID {
			t.Errorf("credential ID changed! expected %v, got %v", credID, meta.ID)
		}
		if meta.Name != "Updated Key Name" {
			t.Errorf("expected updated name, got %s", meta.Name)
		}
		if meta.Fingerprint == nil || *meta.Fingerprint != newFP {
			t.Errorf("expected updated fingerprint, got %v", meta.Fingerprint)
		}

		// AAD check
		if crypto.capturedEncryptCtx.OrganizationID != orgID || crypto.capturedEncryptCtx.CredentialID != credID {
			t.Errorf("AAD mismatch during re-encryption: %+v", crypto.capturedEncryptCtx)
		}
	})

	t.Run("preserves existing name if name is nil", func(t *testing.T) {
		newSecret := []byte("another-secret")
		meta, err := svc.ReplaceCredentialSecret(ctx, orgID, credID, nil, newSecret, nil)
		if err != nil {
			t.Fatalf("expected success, got: %v", err)
		}
		if meta.Name != "Updated Key Name" {
			t.Errorf("expected existing name preserved, got %s", meta.Name)
		}
	})

	t.Run("zeroes local defensive copy during replacement", func(t *testing.T) {
		repo.monitoredBuffer = &crypto.receivedPlaintextRef
		newSecret := []byte("CONFIDENTIAL-ROTATED-SECRET")

		_, err := svc.ReplaceCredentialSecret(ctx, orgID, credID, nil, newSecret, nil)
		if err != nil {
			t.Fatalf("expected success, got: %v", err)
		}

		allZero := make([]byte, len(repo.bufferSnapshotAtUpdateTime))
		if !bytes.Equal(repo.bufferSnapshotAtUpdateTime, allZero) {
			t.Errorf("SECURITY FLAW: local plaintext buffer was not zeroed before repository update")
		}
	})

	t.Run("returns ErrEmptyPlaintextSecret when payload is empty", func(t *testing.T) {
		_, err := svc.ReplaceCredentialSecret(ctx, orgID, credID, nil, []byte{}, nil)
		if !errors.Is(err, domain.ErrEmptyPlaintextSecret) {
			t.Errorf("expected ErrEmptyPlaintextSecret, got: %v", err)
		}
	})
}

func TestVaultService_DeleteCredential(t *testing.T) {
	ctx := context.Background()
	orgID := uuid.New()
	credID := uuid.New()
	crypto := &spyCryptoEngine{}
	repo := &fakeRepo{
		savedCred: &domain.Credential{
			ID:             credID,
			OrganizationID: orgID,
			Name:           "To Delete",
			Type:           domain.TypeSSHPassword,
		},
	}
	svc := NewVaultService(crypto, repo, &fakeTxManager{}, nil)

	t.Run("successfully deletes unreferenced credential without crypto calls", func(t *testing.T) {
		err := svc.DeleteCredential(ctx, orgID, credID)
		if err != nil {
			t.Fatalf("expected success, got: %v", err)
		}
		if repo.savedCred != nil {
			t.Errorf("expected credential to be deleted from repo")
		}
		if crypto.decryptCalls != 0 || crypto.encryptCalls != 0 {
			t.Errorf("SECURITY FLAW: crypto operations invoked during deletion (decrypt: %d, encrypt: %d)", crypto.decryptCalls, crypto.encryptCalls)
		}
	})

	t.Run("returns ErrCredentialNotFound when deleting non-existent credential", func(t *testing.T) {
		err := svc.DeleteCredential(ctx, orgID, uuid.New())
		if !errors.Is(err, domain.ErrCredentialNotFound) {
			t.Errorf("expected ErrCredentialNotFound, got: %v", err)
		}
	})

	t.Run("returns ErrCredentialInUse when referenced by connector", func(t *testing.T) {
		repoInUse := &fakeRepo{
			deleteErr: domain.ErrCredentialInUse,
		}
		svcInUse := NewVaultService(crypto, repoInUse, &fakeTxManager{}, nil)

		err := svcInUse.DeleteCredential(ctx, orgID, credID)
		if !errors.Is(err, domain.ErrCredentialInUse) {
			t.Errorf("expected ErrCredentialInUse, got: %v", err)
		}
	})

	t.Run("returns ErrCredentialServiceUnavailable on infrastructure failure", func(t *testing.T) {
		repoErr := &fakeRepo{
			deleteErr: errors.New("db disconnect"),
		}
		svcErr := NewVaultService(crypto, repoErr, &fakeTxManager{}, nil)

		err := svcErr.DeleteCredential(ctx, orgID, credID)
		if !errors.Is(err, domain.ErrCredentialServiceUnavailable) {
			t.Errorf("expected ErrCredentialServiceUnavailable, got: %v", err)
		}
	})
}
