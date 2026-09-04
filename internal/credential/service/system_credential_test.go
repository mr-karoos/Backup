package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"backup-platform/internal/credential/domain"
	credRepo "backup-platform/internal/credential/repository"
	"backup-platform/internal/credential/secretcrypto"
	"backup-platform/internal/platform/database"
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

	t.Run("Public DeleteCredential rejects system credential", func(t *testing.T) {
		systemCredID := repo.savedCred.ID
		err := svc.DeleteCredential(ctx, orgID, systemCredID)
		if !errors.Is(err, domain.ErrSystemCredentialRestricted) {
			t.Errorf("expected ErrSystemCredentialRestricted, got: %v", err)
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

func TestVaultService_RealPostgres_SystemCredentialCleanup(t *testing.T) {
	testDBURL := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	if testDBURL == "" {
		t.Skip("skipping real postgres system credential test: TEST_DATABASE_URL not set")
	}

	ctx := context.Background()
	pool, err := database.New(ctx, testDBURL)
	if err != nil {
		t.Fatalf("failed initializing database pool: %v", err)
	}
	defer pool.Close()

	orgID := uuid.New()
	cleanup := func() {
		cleanupCtx := context.Background()
		_, _ = pool.Querier().Exec(cleanupCtx, "DELETE FROM credentials WHERE organization_id = $1", orgID)
		_, _ = pool.Querier().Exec(cleanupCtx, "DELETE FROM organizations WHERE id = $1", orgID)
	}
	cleanup()
	defer cleanup()

	// Seed Organization
	slug := fmt.Sprintf("org-syscred-%s", orgID.String()[:8])
	_, err = pool.Querier().Exec(ctx, `
		INSERT INTO organizations (id, name, slug, status, metadata, created_at, updated_at)
		VALUES ($1, 'SysCred Test Org', $2, 'active', '{}'::jsonb, NOW(), NOW())`,
		orgID, slug)
	if err != nil {
		t.Fatalf("failed inserting org: %v", err)
	}

	masterKey := []byte("12345678901234567890123456789012")
	keyProvider, err := secretcrypto.NewStaticKeyProvider(masterKey, 1)
	if err != nil {
		t.Fatalf("failed creating key provider: %v", err)
	}
	cryptoEngine, err := secretcrypto.NewAESGCMEngine(keyProvider)
	if err != nil {
		t.Fatalf("failed creating crypto engine: %v", err)
	}

	repo := credRepo.NewPostgresCredentialRepository()
	svc := NewVaultService(cryptoEngine, repo, pool, nil)

	// 1. Create real system Restic credential
	credSecret := []byte("high-entropy-restic-repo-password-32-bytes")
	meta, err := svc.CreateSystemCredential(ctx, orgID, "restic-key-res1", domain.TypeResticRepositoryKey, credSecret)
	if err != nil {
		t.Fatalf("failed creating system credential: %v", err)
	}

	// 2. Verify public DeleteCredential fails with ErrSystemCredentialRestricted
	err = svc.DeleteCredential(ctx, orgID, meta.ID)
	if !errors.Is(err, domain.ErrSystemCredentialRestricted) {
		t.Fatalf("expected ErrSystemCredentialRestricted on public delete, got: %v", err)
	}

	// Verify credential is still present in database
	var count int
	err = pool.Querier().QueryRow(ctx, "SELECT COUNT(*) FROM credentials WHERE id = $1", meta.ID).Scan(&count)
	if err != nil || count != 1 {
		t.Fatalf("expected credential to still exist, count=%d err=%v", count, err)
	}

	// 3. Internal DeleteSystemCredential succeeds
	err = svc.DeleteSystemCredential(ctx, orgID, meta.ID)
	if err != nil {
		t.Fatalf("expected internal DeleteSystemCredential to succeed, got: %v", err)
	}

	// 4. Verify credential is now absent from database
	err = pool.Querier().QueryRow(ctx, "SELECT COUNT(*) FROM credentials WHERE id = $1", meta.ID).Scan(&count)
	if err != nil || count != 0 {
		t.Fatalf("expected credential to be deleted, count=%d err=%v", count, err)
	}
}
