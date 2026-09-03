package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5"

	"backup-platform/internal/backup/domain"
	backupRepo "backup-platform/internal/backup/repository"
	"backup-platform/internal/backup/restic"
	credRepo "backup-platform/internal/credential/repository"
	"backup-platform/internal/credential/secretcrypto"
	credService "backup-platform/internal/credential/service"
	"backup-platform/internal/platform/database"
	"backup-platform/internal/platform/migrations"
	resDomain "backup-platform/internal/resource/domain"
	resRepo "backup-platform/internal/resource/repository"
	"backup-platform/pkg/uuid"
)

func TestRepositoryService_RealPostgresAndRestic_E2E(t *testing.T) {
	testDBURL := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	if testDBURL == "" {
		t.Skip("skipping real postgres and restic test: TEST_DATABASE_URL not set")
	}

	binaryPath := os.Getenv("RESTIC_BINARY_PATH")
	if binaryPath == "" {
		candidates := []string{
			filepath.Join(os.TempDir(), "restic-bin", "restic.exe"),
			"/usr/local/bin/restic",
		}
		for _, c := range candidates {
			if _, err := os.Stat(c); err == nil {
				binaryPath = c
				break
			}
		}
	}
	if binaryPath == "" {
		t.Skip("skipping real postgres and restic test: restic binary not found")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	conn, err := pgx.Connect(ctx, testDBURL)
	if err != nil {
		t.Fatalf("failed connecting to test database: %v", err)
	}
	defer func() {
		_ = conn.Close(ctx)
	}()

	d, err := iofs.New(migrations.FS, "sql")
	if err != nil {
		t.Fatalf("failed creating iofs driver: %v", err)
	}

	m, err := migrate.NewWithSourceInstance("iofs", d, testDBURL)
	if err != nil {
		t.Fatalf("failed initializing migrate instance: %v", err)
	}
	defer func() {
		_, _ = m.Close()
	}()

	if err := m.Migrate(8); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		t.Fatalf("failed migrating database to version 8: %v", err)
	}

	pool, err := database.New(ctx, testDBURL)
	if err != nil {
		t.Fatalf("failed initializing database pool: %v", err)
	}
	defer pool.Close()

	orgID := uuid.New()
	resID := uuid.New()
	targetLocalID := uuid.New()
	targetLocal2ID := uuid.New()

	cleanup := func() {
		cleanupCtx := context.Background()
		_, _ = pool.Querier().Exec(cleanupCtx, "DELETE FROM backup_repositories WHERE organization_id = $1", orgID)
		_, _ = pool.Querier().Exec(cleanupCtx, "DELETE FROM credentials WHERE organization_id = $1", orgID)
		_, _ = pool.Querier().Exec(cleanupCtx, "DELETE FROM storage_targets WHERE organization_id = $1", orgID)
		_, _ = pool.Querier().Exec(cleanupCtx, "DELETE FROM resources WHERE organization_id = $1", orgID)
		_, _ = pool.Querier().Exec(cleanupCtx, "DELETE FROM organizations WHERE id = $1", orgID)
	}
	cleanup()
	defer cleanup()

	// Seed Organization
	slug := fmt.Sprintf("org-e2e-restic-%s", orgID.String()[:8])
	_, err = pool.Querier().Exec(ctx, `
		INSERT INTO organizations (id, name, slug, status, metadata, created_at, updated_at)
		VALUES ($1, 'Restic E2E Org', $2, 'active', '{}'::jsonb, NOW(), NOW())`,
		orgID, slug)
	if err != nil {
		t.Fatalf("failed inserting org: %v", err)
	}

	// Seed Resource with connector and credential
	connCredID := uuid.New()
	_, err = pool.Querier().Exec(ctx, `
		INSERT INTO credentials (id, organization_id, name, type, managed_by, encrypted_secret, nonce, auth_tag, key_version, created_at, updated_at)
		VALUES ($1, $2, 'SSH Password', 'ssh_password', 'user', '\x01', '\x02', '\x03', 1, NOW(), NOW())`,
		connCredID, orgID)
	if err != nil {
		t.Fatalf("failed inserting connector credential: %v", err)
	}

	_, err = pool.Querier().Exec(ctx, `
		INSERT INTO resources (id, organization_id, name, type, status, metadata, created_at, updated_at)
		VALUES ($1, $2, 'Ubuntu Database Server', 'ubuntu_ssh', 'active', '{}'::jsonb, NOW(), NOW())`,
		resID, orgID)
	if err != nil {
		t.Fatalf("failed inserting resource: %v", err)
	}

	_, err = pool.Querier().Exec(ctx, `
		INSERT INTO resource_connectors (id, organization_id, resource_id, connector_type, credential_id, host, port, auth_type, host_key_fingerprint, config, created_at, updated_at)
		VALUES ($1, $2, $3, 'ubuntu_ssh', $4, '10.0.0.1', 22, 'ssh_password', NULL, '{"username":"ubuntu"}'::jsonb, NOW(), NOW())`,
		uuid.New(), orgID, resID, connCredID)
	if err != nil {
		t.Fatalf("failed inserting resource connector: %v", err)
	}

	// Seed Local Storage Target 1 and 2
	_, err = pool.Querier().Exec(ctx, `
		INSERT INTO storage_targets (id, organization_id, name, type, status, is_default, config, created_at, updated_at)
		VALUES ($1, $2, 'Local Target 1', 'local', 'active', true, '{}'::jsonb, NOW(), NOW()),
		       ($3, $2, 'Local Target 2', 'local', 'active', false, '{}'::jsonb, NOW(), NOW())`,
		targetLocalID, orgID, targetLocal2ID)
	if err != nil {
		t.Fatalf("failed inserting storage target: %v", err)
	}

	// Setup Real VaultService
	masterKey := []byte("12345678901234567890123456789012") // 32 bytes AES-256
	keyProvider, err := secretcrypto.NewStaticKeyProvider(masterKey, 1)
	if err != nil {
		t.Fatalf("failed creating static key provider: %v", err)
	}
	cryptoEngine, err := secretcrypto.NewAESGCMEngine(keyProvider)
	if err != nil {
		t.Fatalf("failed creating crypto engine: %v", err)
	}

	credentialRepository := credRepo.NewPostgresCredentialRepository()
	vaultService := credService.NewVaultService(cryptoEngine, credentialRepository, pool, nil)

	// Setup Resource and Storage Repositories
	storageTargetRepo := backupRepo.NewPostgresBackupRepository(pool)
	resourceRepository := resRepo.NewPostgresResourceRepository()
	resFinder := &testResourceFinderAdapter{repo: resourceRepository, db: pool}

	tempStorageRoot := t.TempDir()
	targetResolver := restic.NewTargetResolver(storageTargetRepo, vaultService, tempStorageRoot, false, nil)
	resticRunner := restic.NewResticRunner(binaryPath, nil)

	repoService := NewRepositoryService(
		storageTargetRepo,
		storageTargetRepo,
		resFinder,
		vaultService,
		targetResolver,
		resticRunner,
		nil,
	)

	t.Run("first EnsureRepository provisions physical restic repository and DB records", func(t *testing.T) {
		repo, err := repoService.EnsureRepository(ctx, orgID, resID, targetLocalID)
		if err != nil {
			t.Fatalf("expected EnsureRepository to succeed, got: %v", err)
		}

		if repo.OrganizationID != orgID || repo.ResourceID != resID || repo.StorageTargetID != targetLocalID {
			t.Errorf("unexpected repository data: %+v", repo)
		}

		// Verify physical repository files exist on filesystem
		expectedRepoPath := filepath.Join(tempStorageRoot, "repositories", "organizations", orgID.String(), "resources", resID.String(), "restic")
		configFilePath := filepath.Join(expectedRepoPath, "config")
		if _, err := os.Stat(configFilePath); err != nil {
			t.Fatalf("expected physical restic config at %s: %v", configFilePath, err)
		}

		// Verify system credential in database
		var credType string
		var credManagedBy string
		err = pool.Querier().QueryRow(ctx, "SELECT type, managed_by FROM credentials WHERE id = $1", repo.CredentialID).Scan(&credType, &credManagedBy)
		if err != nil {
			t.Fatalf("failed querying system credential from DB: %v", err)
		}
		if credType != "restic_repository_key" || credManagedBy != "system" {
			t.Errorf("expected type 'restic_repository_key' and managed_by 'system', got type=%s managed_by=%s", credType, credManagedBy)
		}
	})

	t.Run("second EnsureRepository on same resource probes and returns existing repository", func(t *testing.T) {
		repo, err := repoService.EnsureRepository(ctx, orgID, resID, targetLocalID)
		if err != nil {
			t.Fatalf("expected second EnsureRepository to succeed, got: %v", err)
		}
		if repo.ResourceID != resID {
			t.Errorf("expected repository for resource %s, got %s", resID, repo.ResourceID)
		}
	})

	t.Run("EnsureRepository rejects changing storage target on existing repository", func(t *testing.T) {
		_, err := repoService.EnsureRepository(ctx, orgID, resID, targetLocal2ID)
		if !errors.Is(err, domain.ErrRepositoryTargetMismatch) {
			t.Errorf("expected ErrRepositoryTargetMismatch, got: %v", err)
		}
	})
}

type testResourceFinderAdapter struct {
	repo resRepo.ResourceRepository
	db   database.TxManager
}

func (a *testResourceFinderAdapter) GetByID(ctx context.Context, orgID, resourceID uuid.UUID) (*resDomain.Resource, error) {
	resWithConn, err := a.repo.FindByIDForOrganization(ctx, a.db.Querier(), orgID, resourceID)
	if err != nil {
		return nil, err
	}
	return resWithConn.Resource, nil
}
