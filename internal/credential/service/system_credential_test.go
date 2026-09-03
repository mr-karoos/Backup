package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"backup-platform/internal/credential/domain"
	"backup-platform/pkg/uuid"
)

func TestVaultService_SystemCredential_Lifecycle(t *testing.T) {
	ctx := context.Background()
	orgID := uuid.New()
	crypto := &spyCryptoEngine{}
	repo := &fakeRepo{}
	svc := NewVaultService(crypto, repo, &fakeTxManager{}, nil)

	plaintextKey := []byte("high-entropy-restic-repo-password-32-bytes")

	t.Run("CreateSystemCredential persists with ManagedBySystem", func(t *testing.T) {
		meta, err := svc.CreateSystemCredential(ctx, orgID, "restic-repo-key-res1", domain.TypeResticRepositoryKey, plaintextKey)
		if err != nil {
			t.Fatalf("expected success, got: %v", err)
		}

		if meta.ManagedBy != domain.ManagedBySystem {
			t.Errorf("expected ManagedBySystem, got: %s", meta.ManagedBy)
		}
		if meta.Type != domain.TypeResticRepositoryKey {
			t.Errorf("expected TypeResticRepositoryKey, got: %s", meta.Type)
		}
		if repo.savedCred == nil {
			t.Fatalf("expected savedCred in repo")
		}
		if repo.savedCred.ManagedBy != domain.ManagedBySystem {
			t.Errorf("expected savedCred.ManagedBy to be system, got: %s", repo.savedCred.ManagedBy)
		}
	})

	t.Run("Public CreateCredential rejects restic_repository_key", func(t *testing.T) {
		_, err := svc.CreateCredential(ctx, orgID, "malicious-user-key", domain.TypeResticRepositoryKey, plaintextKey, nil)
		if !errors.Is(err, domain.ErrSystemCredentialRestricted) {
			t.Errorf("expected ErrSystemCredentialRestricted, got: %v", err)
		}
	})

	t.Run("Public GetCredentialMetadata hides system credential", func(t *testing.T) {
		systemCredID := repo.savedCred.ID
		_, err := svc.GetCredentialMetadata(ctx, orgID, systemCredID)
		if !errors.Is(err, domain.ErrCredentialNotFound) {
			t.Errorf("expected ErrCredentialNotFound for system credential in public getter, got: %v", err)
		}
	})

	t.Run("Internal GetSystemCredentialMetadata returns metadata for system credential", func(t *testing.T) {
		systemCredID := repo.savedCred.ID
		meta, err := svc.GetSystemCredentialMetadata(ctx, orgID, systemCredID)
		if err != nil {
			t.Fatalf("expected success from internal getter, got: %v", err)
		}
		if meta.ID != systemCredID || meta.ManagedBy != domain.ManagedBySystem {
			t.Errorf("unexpected metadata: %+v", meta)
		}
	})

	t.Run("Public UpdateCredentialName rejects system credential", func(t *testing.T) {
		systemCredID := repo.savedCred.ID
		_, err := svc.UpdateCredentialName(ctx, orgID, systemCredID, "Renamed System Key")
		if !errors.Is(err, domain.ErrSystemCredentialRestricted) {
			t.Errorf("expected ErrSystemCredentialRestricted, got: %v", err)
		}
	})

	t.Run("Public ReplaceCredentialSecret rejects system credential", func(t *testing.T) {
		systemCredID := repo.savedCred.ID
		newSecret := []byte("new-secret-bytes")
		_, err := svc.ReplaceCredentialSecret(ctx, orgID, systemCredID, nil, newSecret, nil)
		if !errors.Is(err, domain.ErrSystemCredentialRestricted) {
			t.Errorf("expected ErrSystemCredentialRestricted, got: %v", err)
		}
	})

	t.Run("Internal LoadCredentialForUse decrypts system credential", func(t *testing.T) {
		systemCredID := repo.savedCred.ID

		cType, decrypted, err := svc.LoadCredentialForUse(ctx, orgID, systemCredID)
		if err != nil {
			t.Fatalf("expected internal load to succeed, got: %v", err)
		}
		if cType != domain.TypeResticRepositoryKey {
			t.Errorf("expected TypeResticRepositoryKey, got: %s", cType)
		}
		if len(decrypted) == 0 {
			t.Errorf("expected non-empty decrypted secret")
		}
	})

	t.Run("Internal DeleteSystemCredential allows cleanup", func(t *testing.T) {
		systemCredID := repo.savedCred.ID
		err := svc.DeleteSystemCredential(ctx, orgID, systemCredID)
		if err != nil {
			t.Fatalf("expected DeleteSystemCredential to succeed, got: %v", err)
		}
	})
}

func init() {
	_ = time.Now()
}
