package migrations

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
	"github.com/jackc/pgx/v5/pgxpool"

	"backup-platform/pkg/uuid"
)

func TestMigrations_StepA3_Integration(t *testing.T) {
	testDBURL := os.Getenv("TEST_DATABASE_URL")
	if testDBURL == "" {
		t.Skip("skipping migration Step A.3 integration test: TEST_DATABASE_URL not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	connPool, err := pgxpool.New(ctx, testDBURL)
	if err != nil {
		t.Fatalf("failed connecting to test database: %v", err)
	}
	defer connPool.Close()

	d, err := iofs.New(FS, "sql")
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

	// 1. Migrate to version 7 (Step A.2 baseline)
	if err := m.Migrate(7); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		t.Fatalf("failed migrating to version 7 baseline: %v", err)
	}

	orgA := uuid.New()
	orgB := uuid.New()
	resA := uuid.New()
	resB := uuid.New()
	targetA := uuid.New()
	userCredA := uuid.New()
	systemCredA := uuid.New()
	systemCredB := uuid.New()
	repoA := uuid.New()

	cleanup := func() {
		cleanupCtx := context.Background()
		_, _ = connPool.Exec(cleanupCtx, "DELETE FROM backup_repositories WHERE organization_id IN ($1, $2)", orgA, orgB)
		_, _ = connPool.Exec(cleanupCtx, "DELETE FROM credentials WHERE organization_id IN ($1, $2)", orgA, orgB)
		_, _ = connPool.Exec(cleanupCtx, "DELETE FROM storage_targets WHERE organization_id IN ($1, $2)", orgA, orgB)
		_, _ = connPool.Exec(cleanupCtx, "DELETE FROM resources WHERE organization_id IN ($1, $2)", orgA, orgB)
		_, _ = connPool.Exec(cleanupCtx, "DELETE FROM organizations WHERE id IN ($1, $2)", orgA, orgB)
	}
	cleanup()
	defer cleanup()

	// 2. Seed Org A and Org B in version 7 schema
	slugA := fmt.Sprintf("org-a3-a-%s", orgA.String()[:8])
	slugB := fmt.Sprintf("org-a3-b-%s", orgB.String()[:8])
	_, err = connPool.Exec(ctx, `
		INSERT INTO organizations (id, name, slug, status, metadata, created_at, updated_at)
		VALUES ($1, 'Org A3 A', $2, 'active', '{}'::jsonb, NOW(), NOW()),
		       ($3, 'Org A3 B', $4, 'active', '{}'::jsonb, NOW(), NOW())`,
		orgA, slugA, orgB, slugB)
	if err != nil {
		t.Fatalf("failed seeding organizations: %v", err)
	}

	_, err = connPool.Exec(ctx, `
		INSERT INTO resources (id, organization_id, name, type, status, metadata, created_at, updated_at)
		VALUES ($1, $2, 'Res A', 'ubuntu_ssh', 'active', '{}'::jsonb, NOW(), NOW()),
		       ($3, $4, 'Res B', 'ubuntu_ssh', 'active', '{}'::jsonb, NOW(), NOW())`,
		resA, orgA, resB, orgB)
	if err != nil {
		t.Fatalf("failed seeding resources: %v", err)
	}

	_, err = connPool.Exec(ctx, `
		INSERT INTO storage_targets (id, organization_id, name, type, status, is_default, config, created_at, updated_at)
		VALUES ($1, $2, 'Local Target A', 'local', 'active', true, '{}'::jsonb, NOW(), NOW())`,
		targetA, orgA)
	if err != nil {
		t.Fatalf("failed seeding storage target: %v", err)
	}

	// Seed pre-existing credential in v7 (no managed_by column yet)
	_, err = connPool.Exec(ctx, `
		INSERT INTO credentials (id, organization_id, name, type, encrypted_secret, nonce, auth_tag, key_version, created_at, updated_at)
		VALUES ($1, $2, 'Pre-existing User Cred', 'ssh_password', '\x010203', '\x040506', '\x070809', 1, NOW(), NOW())`,
		userCredA, orgA)
	if err != nil {
		t.Fatalf("failed seeding pre-existing credential: %v", err)
	}

	// 3. Migrate UP to version 8 (Step A.3)
	if err := m.Migrate(8); err != nil {
		t.Fatalf("failed migrating to version 8: %v", err)
	}

	// Verify existing credential backfilled to managed_by = 'user'
	var managedBy string
	err = connPool.QueryRow(ctx, "SELECT managed_by FROM credentials WHERE id = $1", userCredA).Scan(&managedBy)
	if err != nil {
		t.Fatalf("failed querying managed_by of backfilled credential: %v", err)
	}
	if managedBy != "user" {
		t.Errorf("expected backfilled managed_by to be 'user', got: %s", managedBy)
	}

	// 4. Test constraint chk_credentials_restic_key: restic_repository_key MUST be system
	_, err = connPool.Exec(ctx, `
		INSERT INTO credentials (id, organization_id, name, type, managed_by, encrypted_secret, nonce, auth_tag, key_version, created_at, updated_at)
		VALUES ($1, $2, 'Illegal User Restic Key', 'restic_repository_key', 'user', '\x01', '\x02', '\x03', 1, NOW(), NOW())`,
		uuid.New(), orgA)
	if err == nil {
		t.Errorf("expected constraint violation when inserting restic_repository_key with managed_by='user'")
	}

	// 5. Insert valid system restic_repository_key
	_, err = connPool.Exec(ctx, `
		INSERT INTO credentials (id, organization_id, name, type, managed_by, encrypted_secret, nonce, auth_tag, key_version, created_at, updated_at)
		VALUES ($1, $2, 'System Restic Key A', 'restic_repository_key', 'system', '\x01', '\x02', '\x03', 1, NOW(), NOW()),
		       ($3, $4, 'System Restic Key B', 'restic_repository_key', 'system', '\x01', '\x02', '\x03', 1, NOW(), NOW())`,
		systemCredA, orgA, systemCredB, orgB)
	if err != nil {
		t.Fatalf("failed inserting system restic credential: %v", err)
	}

	// 6. Test backup_repositories tenant isolation & composite FKs
	// 6a. Cross-tenant resource rejection (Org A repository with Org B resource)
	_, err = connPool.Exec(ctx, `
		INSERT INTO backup_repositories (id, organization_id, resource_id, storage_target_id, credential_id, repository_locator, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, 'loc', NOW(), NOW())`,
		uuid.New(), orgA, resB, targetA, systemCredA)
	if err == nil {
		t.Errorf("expected FK violation for cross-tenant resource_id")
	}

	// 6b. Cross-tenant credential rejection (Org A repository with Org B credential)
	_, err = connPool.Exec(ctx, `
		INSERT INTO backup_repositories (id, organization_id, resource_id, storage_target_id, credential_id, repository_locator, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, 'loc', NOW(), NOW())`,
		uuid.New(), orgA, resA, targetA, systemCredB)
	if err == nil {
		t.Errorf("expected FK violation for cross-tenant credential_id")
	}

	// 6c. Successful insertion of valid same-org repository
	locatorA := fmt.Sprintf("repositories/organizations/%s/resources/%s/restic", orgA, resA)
	_, err = connPool.Exec(ctx, `
		INSERT INTO backup_repositories (id, organization_id, resource_id, storage_target_id, credential_id, repository_locator, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, NOW(), NOW())`,
		repoA, orgA, resA, targetA, systemCredA, locatorA)
	if err != nil {
		t.Fatalf("failed inserting valid backup repository: %v", err)
	}

	// 6d. Uniqueness: exactly one repository per resource (cannot insert second repo for resA)
	_, err = connPool.Exec(ctx, `
		INSERT INTO backup_repositories (id, organization_id, resource_id, storage_target_id, credential_id, repository_locator, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, 'loc2', NOW(), NOW())`,
		uuid.New(), orgA, resA, targetA, systemCredA)
	if err == nil {
		t.Errorf("expected unique constraint violation for duplicate repository on resource_id")
	}

	// 6e. Uniqueness on credential_id: a single restic key cannot be shared across repositories
	resC := uuid.New()
	_, err = connPool.Exec(ctx, `
		INSERT INTO resources (id, organization_id, name, type, status, metadata, created_at, updated_at)
		VALUES ($1, $2, 'Res C', 'ubuntu_ssh', 'active', '{}'::jsonb, NOW(), NOW())`,
		resC, orgA)
	if err != nil {
		t.Fatalf("failed seeding Res C: %v", err)
	}

	_, err = connPool.Exec(ctx, `
		INSERT INTO backup_repositories (id, organization_id, resource_id, storage_target_id, credential_id, repository_locator, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, 'loc-c', NOW(), NOW())`,
		uuid.New(), orgA, resC, targetA, systemCredA)
	if err == nil {
		t.Errorf("expected unique constraint violation for duplicate credential_id on backup_repositories")
	}

	// 6f. Uniqueness on (storage_target_id, repository_locator): physical locator cannot be claimed twice
	systemCredC := uuid.New()
	_, err = connPool.Exec(ctx, `
		INSERT INTO credentials (id, organization_id, name, type, managed_by, encrypted_secret, nonce, auth_tag, key_version, created_at, updated_at)
		VALUES ($1, $2, 'System Restic Key C', 'restic_repository_key', 'system', '\x01', '\x02', '\x03', 1, NOW(), NOW())`,
		systemCredC, orgA)
	if err != nil {
		t.Fatalf("failed seeding System Restic Key C: %v", err)
	}

	_, err = connPool.Exec(ctx, `
		INSERT INTO backup_repositories (id, organization_id, resource_id, storage_target_id, credential_id, repository_locator, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, NOW(), NOW())`,
		uuid.New(), orgA, resC, targetA, systemCredC, locatorA)
	if err == nil {
		t.Errorf("expected unique constraint violation for duplicate (storage_target_id, repository_locator) on backup_repositories")
	}

	// 7. Test DOWN migration fails closed when backup_repositories exist
	downErr := m.Migrate(7)
	if downErr == nil {
		t.Fatalf("expected DOWN migration to fail while backup_repositories exist")
	}
	if !strings.Contains(downErr.Error(), "cannot downgrade while backup_repositories exist") {
		t.Errorf("unexpected down migration error message: %v", downErr)
	}

	// Clear dirty migration flag if left dirty by the intentional failure
	_ = m.Force(8)

	// Delete backup_repositories row, but leave system credential
	_, err = connPool.Exec(ctx, "DELETE FROM backup_repositories WHERE id = $1", repoA)
	if err != nil {
		t.Fatalf("failed deleting backup_repository: %v", err)
	}

	// Test DOWN migration fails closed when system restic credentials exist
	downErr2 := m.Migrate(7)
	if downErr2 == nil {
		t.Fatalf("expected DOWN migration to fail while system credentials exist")
	}
	if !strings.Contains(downErr2.Error(), "cannot downgrade while system restic_repository_key credentials exist") {
		t.Errorf("unexpected down migration error message: %v", downErr2)
	}

	_ = m.Force(8)

	// Delete system credentials
	_, err = connPool.Exec(ctx, "DELETE FROM credentials WHERE id IN ($1, $2)", systemCredA, systemCredB)
	if err != nil {
		t.Fatalf("failed deleting system credentials: %v", err)
	}

	// 8. With A.3 records removed, DOWN migration to v7 must succeed
	if err := m.Migrate(7); err != nil {
		t.Fatalf("expected DOWN migration to v7 to succeed cleanly after removing A.3 data, got: %v", err)
	}

	// 9. Re-apply UP to v8 must succeed cleanly
	if err := m.Migrate(8); err != nil {
		t.Fatalf("expected re-applying UP to v8 to succeed, got: %v", err)
	}
}
