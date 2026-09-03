package repository

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5"

	"backup-platform/internal/backup/domain"
	"backup-platform/internal/platform/database"
	"backup-platform/internal/platform/migrations"
	"backup-platform/pkg/uuid"
)

func TestPostgresBackupRepository_StepA3_Integration(t *testing.T) {
	testDBURL := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	if testDBURL == "" {
		t.Skip("skipping Step A.3 repository integration test: TEST_DATABASE_URL not set")
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

	// Ensure database is migrated through version 8 (Step A.3: restic repository foundation)
	if err := m.Migrate(8); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		t.Fatalf("failed migrating to version 8: %v", err)
	}

	pool, err := database.New(ctx, testDBURL)
	if err != nil {
		t.Fatalf("failed initializing database pool: %v", err)
	}
	defer pool.Close()

	repo := NewPostgresBackupRepository(pool)

	orgA := uuid.New()
	orgB := uuid.New()
	resA1 := uuid.New()
	resA2 := uuid.New()
	resB1 := uuid.New()
	targetLocalA := uuid.New()
	targetS3A := uuid.New()
	targetDisabledA := uuid.New()
	targetLocalB := uuid.New()
	systemCredA1 := uuid.New()
	systemCredA2 := uuid.New()
	userCredA := uuid.New()
	systemCredB1 := uuid.New()

	cleanup := func() {
		cleanupCtx := context.Background()
		_, _ = pool.Querier().Exec(cleanupCtx, "DELETE FROM backup_repositories WHERE organization_id IN ($1, $2)", orgA, orgB)
		_, _ = pool.Querier().Exec(cleanupCtx, "DELETE FROM storage_targets WHERE organization_id IN ($1, $2)", orgA, orgB)
		_, _ = pool.Querier().Exec(cleanupCtx, "DELETE FROM credentials WHERE organization_id IN ($1, $2)", orgA, orgB)
		_, _ = pool.Querier().Exec(cleanupCtx, "DELETE FROM resources WHERE organization_id IN ($1, $2)", orgA, orgB)
		_, _ = pool.Querier().Exec(cleanupCtx, "DELETE FROM organizations WHERE id IN ($1, $2)", orgA, orgB)
	}
	cleanup()
	defer cleanup()

	// 1. Seed Organizations
	slugA := fmt.Sprintf("org-repo-a3-a-%s", orgA.String()[:8])
	slugB := fmt.Sprintf("org-repo-a3-b-%s", orgB.String()[:8])
	_, err = pool.Querier().Exec(ctx, `
		INSERT INTO organizations (id, name, slug, status, metadata, created_at, updated_at)
		VALUES ($1, 'Org A', $2, 'active', '{}'::jsonb, NOW(), NOW()),
		       ($3, 'Org B', $4, 'active', '{}'::jsonb, NOW(), NOW())`,
		orgA, slugA, orgB, slugB)
	if err != nil {
		t.Fatalf("failed seeding organizations: %v", err)
	}

	// 2. Seed Resources
	_, err = pool.Querier().Exec(ctx, `
		INSERT INTO resources (id, organization_id, name, type, status, metadata, created_at, updated_at)
		VALUES ($1, $2, 'Resource A1', 'ubuntu_ssh', 'active', '{}'::jsonb, NOW(), NOW()),
		       ($3, $2, 'Resource A2', 'ubuntu_ssh', 'active', '{}'::jsonb, NOW(), NOW()),
		       ($4, $5, 'Resource B1', 'ubuntu_ssh', 'active', '{}'::jsonb, NOW(), NOW())`,
		resA1, orgA, resA2, resB1, orgB)
	if err != nil {
		t.Fatalf("failed seeding resources: %v", err)
	}

	// 3. Seed Credentials
	// systemCredA1: restic_repository_key, system
	// systemCredA2: restic_repository_key, system
	// userCredA: s3_credentials, user
	// systemCredB1: restic_repository_key, system (Org B)
	_, err = pool.Querier().Exec(ctx, `
		INSERT INTO credentials (id, organization_id, name, type, managed_by, encrypted_secret, nonce, auth_tag, key_version, created_at, updated_at)
		VALUES ($1, $2, 'System Key A1', 'restic_repository_key', 'system', '\x01', '\x02', '\x03', 1, NOW(), NOW()),
		       ($3, $2, 'System Key A2', 'restic_repository_key', 'system', '\x01', '\x02', '\x03', 1, NOW(), NOW()),
		       ($4, $2, 'User S3 Cred A', 's3_credentials', 'user', '\x01', '\x02', '\x03', 1, NOW(), NOW()),
		       ($5, $6, 'System Key B1', 'restic_repository_key', 'system', '\x01', '\x02', '\x03', 1, NOW(), NOW())`,
		systemCredA1, orgA, systemCredA2, userCredA, systemCredB1, orgB)
	if err != nil {
		t.Fatalf("failed seeding credentials: %v", err)
	}

	// 4. Seed Storage Targets
	_, err = pool.Querier().Exec(ctx, `
		INSERT INTO storage_targets (id, organization_id, name, type, status, is_default, config, created_at, updated_at)
		VALUES ($1, $2, 'Local Target A', 'local', 'active', true, '{}'::jsonb, NOW(), NOW()),
		       ($3, $2, 'S3 Target A', 's3', 'active', false, '{"bucket":"my-bucket","region":"us-east-1"}'::jsonb, NOW(), NOW()),
		       ($4, $2, 'Disabled Target A', 'local', 'disabled', false, '{}'::jsonb, NOW(), NOW()),
		       ($5, $6, 'Local Target B', 'local', 'active', true, '{}'::jsonb, NOW(), NOW())`,
		targetLocalA, orgA, targetS3A, targetDisabledA, targetLocalB, orgB)
	if err != nil {
		t.Fatalf("failed seeding storage targets: %v", err)
	}

	repoA1ID := uuid.New()
	repoA2ID := uuid.New()

	t.Run("successfully creates local dedicated backup repository", func(t *testing.T) {
		newRepo := &domain.BackupRepository{
			ID:                repoA1ID,
			OrganizationID:    orgA,
			ResourceID:        resA1,
			StorageTargetID:   targetLocalA,
			CredentialID:      systemCredA1,
			RepositoryLocator: fmt.Sprintf("repositories/organizations/%s/resources/%s/restic", orgA, resA1),
			Status:            domain.BackupRepositoryStatusActive,
		}

		created, err := repo.CreateRepository(ctx, newRepo)
		if err != nil {
			t.Fatalf("expected success, got: %v", err)
		}
		if created.ID != repoA1ID || created.OrganizationID != orgA || created.ResourceID != resA1 {
			t.Errorf("unexpected created repository: %+v", created)
		}
		if created.Status != domain.BackupRepositoryStatusActive {
			t.Errorf("expected active status, got: %s", created.Status)
		}
	})

	t.Run("successfully creates S3 dedicated backup repository", func(t *testing.T) {
		newRepo := &domain.BackupRepository{
			ID:                repoA2ID,
			OrganizationID:    orgA,
			ResourceID:        resA2,
			StorageTargetID:   targetS3A,
			CredentialID:      systemCredA2,
			RepositoryLocator: fmt.Sprintf("organizations/%s/resources/%s/restic", orgA, resA2),
			Status:            domain.BackupRepositoryStatusActive,
		}

		created, err := repo.CreateRepository(ctx, newRepo)
		if err != nil {
			t.Fatalf("expected success, got: %v", err)
		}
		if created.ID != repoA2ID || created.OrganizationID != orgA || created.ResourceID != resA2 {
			t.Errorf("unexpected created repository: %+v", created)
		}
	})

	t.Run("rejects duplicate repository for the same resource (one repo per resource invariant)", func(t *testing.T) {
		dupRepo := &domain.BackupRepository{
			ID:                uuid.New(),
			OrganizationID:    orgA,
			ResourceID:        resA1, // already has repoA1ID
			StorageTargetID:   targetLocalA,
			CredentialID:      systemCredA1,
			RepositoryLocator: "duplicate/locator",
			Status:            domain.BackupRepositoryStatusActive,
		}

		_, err := repo.CreateRepository(ctx, dupRepo)
		if !errors.Is(err, domain.ErrRepositoryAlreadyExists) {
			t.Errorf("expected ErrRepositoryAlreadyExists, got: %v", err)
		}
	})

	t.Run("rejects cross-tenant resource binding", func(t *testing.T) {
		crossRepo := &domain.BackupRepository{
			ID:                uuid.New(),
			OrganizationID:    orgA,
			ResourceID:        resB1, // belongs to Org B
			StorageTargetID:   targetLocalA,
			CredentialID:      systemCredA1,
			RepositoryLocator: "cross/res",
			Status:            domain.BackupRepositoryStatusActive,
		}

		_, err := repo.CreateRepository(ctx, crossRepo)
		if !errors.Is(err, domain.ErrInvalidRepositoryBinding) {
			t.Errorf("expected ErrInvalidRepositoryBinding for cross-tenant resource, got: %v", err)
		}
	})

	t.Run("rejects cross-tenant storage target binding", func(t *testing.T) {
		crossRepo := &domain.BackupRepository{
			ID:                uuid.New(),
			OrganizationID:    orgA,
			ResourceID:        uuid.New(),
			StorageTargetID:   targetLocalB, // belongs to Org B
			CredentialID:      systemCredA1,
			RepositoryLocator: "cross/target",
			Status:            domain.BackupRepositoryStatusActive,
		}

		_, err := repo.CreateRepository(ctx, crossRepo)
		if !errors.Is(err, domain.ErrInvalidRepositoryBinding) {
			t.Errorf("expected ErrInvalidRepositoryBinding for cross-tenant storage target, got: %v", err)
		}
	})

	t.Run("rejects cross-tenant credential binding", func(t *testing.T) {
		crossRepo := &domain.BackupRepository{
			ID:                uuid.New(),
			OrganizationID:    orgA,
			ResourceID:        uuid.New(),
			StorageTargetID:   targetLocalA,
			CredentialID:      systemCredB1, // belongs to Org B
			RepositoryLocator: "cross/cred",
			Status:            domain.BackupRepositoryStatusActive,
		}

		_, err := repo.CreateRepository(ctx, crossRepo)
		if !errors.Is(err, domain.ErrInvalidRepositoryBinding) {
			t.Errorf("expected ErrInvalidRepositoryBinding for cross-tenant credential, got: %v", err)
		}
	})

	t.Run("rejects non-system credential or non-restic_repository_key type", func(t *testing.T) {
		badCredRepo := &domain.BackupRepository{
			ID:                uuid.New(),
			OrganizationID:    orgA,
			ResourceID:        uuid.New(),
			StorageTargetID:   targetLocalA,
			CredentialID:      userCredA, // user-managed s3_credentials
			RepositoryLocator: "bad/cred",
			Status:            domain.BackupRepositoryStatusActive,
		}

		_, err := repo.CreateRepository(ctx, badCredRepo)
		if !errors.Is(err, domain.ErrInvalidRepositoryBinding) {
			t.Errorf("expected ErrInvalidRepositoryBinding for user-managed credential, got: %v", err)
		}
	})

	t.Run("rejects inactive storage target", func(t *testing.T) {
		inactiveTargetRepo := &domain.BackupRepository{
			ID:                uuid.New(),
			OrganizationID:    orgA,
			ResourceID:        uuid.New(),
			StorageTargetID:   targetDisabledA, // disabled target
			CredentialID:      systemCredA1,
			RepositoryLocator: "inactive/target",
			Status:            domain.BackupRepositoryStatusActive,
		}

		_, err := repo.CreateRepository(ctx, inactiveTargetRepo)
		if !errors.Is(err, domain.ErrInvalidRepositoryBinding) {
			t.Errorf("expected ErrInvalidRepositoryBinding for disabled storage target, got: %v", err)
		}
	})

	t.Run("GetRepositoryByResourceID retrieves canonical repository", func(t *testing.T) {
		found, err := repo.GetRepositoryByResourceID(ctx, orgA, resA1)
		if err != nil {
			t.Fatalf("expected to find repository, got: %v", err)
		}
		if found.ID != repoA1ID || found.ResourceID != resA1 {
			t.Errorf("unexpected repository found: %+v", found)
		}
	})

	t.Run("GetRepositoryByResourceID enforces organization isolation", func(t *testing.T) {
		// Org B querying Org A's resource repository
		_, err := repo.GetRepositoryByResourceID(ctx, orgB, resA1)
		if !errors.Is(err, domain.ErrRepositoryNotFound) {
			t.Errorf("expected ErrRepositoryNotFound for cross-tenant resource query, got: %v", err)
		}
	})

	t.Run("GetRepositoryByID retrieves repository by id", func(t *testing.T) {
		found, err := repo.GetRepositoryByID(ctx, orgA, repoA1ID)
		if err != nil {
			t.Fatalf("expected to find repository by ID, got: %v", err)
		}
		if found.ID != repoA1ID || found.OrganizationID != orgA {
			t.Errorf("unexpected repository: %+v", found)
		}
	})

	t.Run("GetRepositoryByID enforces organization isolation", func(t *testing.T) {
		// Org B querying Org A's repository ID
		_, err := repo.GetRepositoryByID(ctx, orgB, repoA1ID)
		if !errors.Is(err, domain.ErrRepositoryNotFound) {
			t.Errorf("expected ErrRepositoryNotFound for cross-tenant repo ID query, got: %v", err)
		}
	})
}
