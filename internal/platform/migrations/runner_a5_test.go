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

func TestMigrations_StepA5_Integration(t *testing.T) {
	testDBURL := os.Getenv("TEST_DATABASE_URL")
	if testDBURL == "" {
		t.Skip("skipping migration Step A.5 integration test: TEST_DATABASE_URL not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
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

	// 1. Ensure clean baseline at version 9 (Step A.4 baseline)
	if err := m.Migrate(9); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		t.Fatalf("failed migrating to version 9 baseline: %v", err)
	}

	// 2. Migrate up to version 10
	if err := m.Migrate(10); err != nil {
		t.Fatalf("failed migrating to version 10: %v", err)
	}

	// 3. Assert repository_maintenance_jobs and repository_maintenance_runs tables exist
	var jobsTableExists, runsTableExists bool
	err = connPool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.tables 
			WHERE table_name = 'repository_maintenance_jobs' AND table_schema = 'public'
		);
	`).Scan(&jobsTableExists)
	if err != nil || !jobsTableExists {
		t.Fatalf("expected repository_maintenance_jobs table to exist, got %v (err: %v)", jobsTableExists, err)
	}

	err = connPool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.tables 
			WHERE table_name = 'repository_maintenance_runs' AND table_schema = 'public'
		);
	`).Scan(&runsTableExists)
	if err != nil || !runsTableExists {
		t.Fatalf("expected repository_maintenance_runs table to exist, got %v (err: %v)", runsTableExists, err)
	}
	t.Log("Verified Migration 10 tables exist")

	orgID := uuid.New()
	resID := uuid.New()
	targetID := uuid.New()
	credID := uuid.New()
	repoID := uuid.New()

	cleanup := func() {
		cleanupCtx := context.Background()
		_, _ = connPool.Exec(cleanupCtx, "DELETE FROM repository_maintenance_runs WHERE job_id IN (SELECT id FROM repository_maintenance_jobs WHERE organization_id = $1)", orgID)
		_, _ = connPool.Exec(cleanupCtx, "DELETE FROM repository_maintenance_jobs WHERE organization_id = $1", orgID)
		_, _ = connPool.Exec(cleanupCtx, "DELETE FROM backup_repositories WHERE organization_id = $1", orgID)
		_, _ = connPool.Exec(cleanupCtx, "DELETE FROM credentials WHERE organization_id = $1", orgID)
		_, _ = connPool.Exec(cleanupCtx, "DELETE FROM storage_targets WHERE organization_id = $1", orgID)
		_, _ = connPool.Exec(cleanupCtx, "DELETE FROM resources WHERE organization_id = $1", orgID)
		_, _ = connPool.Exec(cleanupCtx, "DELETE FROM organizations WHERE id = $1", orgID)
	}
	cleanup()
	defer cleanup()

	slug := fmt.Sprintf("org-mig-a5-%s", orgID.String()[:8])
	if _, err := connPool.Exec(ctx, `INSERT INTO organizations (id, name, slug, status, metadata, created_at, updated_at) VALUES ($1, 'Org Mig A5', $2, 'active', '{}'::jsonb, NOW(), NOW());`, orgID, slug); err != nil {
		t.Fatalf("failed seeding organization: %v", err)
	}
	if _, err := connPool.Exec(ctx, `INSERT INTO resources (id, organization_id, name, type, status, metadata, created_at, updated_at) VALUES ($1, $2, 'Res Mig A5', 'ubuntu_ssh', 'active', '{}'::jsonb, NOW(), NOW());`, resID, orgID); err != nil {
		t.Fatalf("failed seeding resource: %v", err)
	}
	if _, err := connPool.Exec(ctx, `INSERT INTO storage_targets (id, organization_id, name, type, config, status, created_at, updated_at) VALUES ($1, $2, 'Target Mig A5', 'local', '{"storage_path":"/tmp/mig"}'::jsonb, 'active', NOW(), NOW());`, targetID, orgID); err != nil {
		t.Fatalf("failed seeding storage target: %v", err)
	}
	if _, err := connPool.Exec(ctx, `INSERT INTO credentials (id, organization_id, name, type, managed_by, encrypted_secret, nonce, auth_tag, key_version, created_at, updated_at) VALUES ($1, $2, 'Cred Mig A5', 'restic_repository_key', 'system', '\x01', '\x02', '\x03', 1, NOW(), NOW());`, credID, orgID); err != nil {
		t.Fatalf("failed seeding credential: %v", err)
	}
	if _, err := connPool.Exec(ctx, `INSERT INTO backup_repositories (id, organization_id, resource_id, storage_target_id, credential_id, repository_locator, status, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, '/tmp/mig-repo', 'active', NOW(), NOW());`, repoID, orgID, resID, targetID, credID); err != nil {
		t.Fatalf("failed seeding backup repository: %v", err)
	}

	// 4. Test Constraints & Deduplication
	jobID1 := uuid.New()
	_, err = connPool.Exec(ctx, `
		INSERT INTO repository_maintenance_jobs (id, organization_id, repository_id, operation_type, status)
		VALUES ($1, $2, $3, 'restic_prune', 'pending');
	`, jobID1, orgID, repoID)
	if err != nil {
		t.Fatalf("failed inserting valid maintenance job: %v", err)
	}

	// Deduplication check: duplicate active restic_prune must fail unique index
	jobIDDup := uuid.New()
	_, err = connPool.Exec(ctx, `
		INSERT INTO repository_maintenance_jobs (id, organization_id, repository_id, operation_type, status)
		VALUES ($1, $2, $3, 'restic_prune', 'pending');
	`, jobIDDup, orgID, repoID)
	if err == nil {
		t.Fatalf("expected unique constraint violation on duplicate active prune job, got nil")
	}
	t.Log("Verified partial unique index deduplication for pending/running jobs")

	// Invalid operation type check
	jobIDInvalid := uuid.New()
	_, err = connPool.Exec(ctx, `
		INSERT INTO repository_maintenance_jobs (id, organization_id, repository_id, operation_type, status)
		VALUES ($1, $2, $3, 'invalid_op', 'pending');
	`, jobIDInvalid, orgID, repoID)
	if err == nil {
		t.Fatalf("expected check constraint violation on invalid operation type, got nil")
	}
	t.Log("Verified operation_type check constraint")

	// Insert run
	runID1 := uuid.New()
	_, err = connPool.Exec(ctx, `
		INSERT INTO repository_maintenance_runs (id, organization_id, job_id, attempt_number, status, started_at)
		VALUES ($1, $2, $3, 1, 'running', NOW());
	`, runID1, orgID, jobID1)
	if err != nil {
		t.Fatalf("failed inserting maintenance run: %v", err)
	}

	// 5. Test Rollback Guard: down migration must abort while jobs/runs exist
	err = m.Migrate(9)
	if err == nil {
		t.Fatalf("expected down migration to fail closed when jobs/runs exist, got nil")
	}
	if !strings.Contains(err.Error(), "cannot rollback migration 000010") {
		t.Fatalf("expected fail-closed guard error message, got: %v", err)
	}
	t.Logf("Verified Migration 10 rollback guard prevented drop: %v", err)

	// Clean up data and reset dirty flag
	if _, err := connPool.Exec(ctx, "DELETE FROM repository_maintenance_runs WHERE organization_id = $1;", orgID); err != nil {
		t.Fatalf("failed deleting maintenance runs: %v", err)
	}
	if _, err := connPool.Exec(ctx, "DELETE FROM repository_maintenance_jobs WHERE organization_id = $1;", orgID); err != nil {
		t.Fatalf("failed deleting maintenance jobs: %v", err)
	}

	if err := m.Force(10); err != nil {
		t.Fatalf("failed resetting dirty flag to version 10: %v", err)
	}

	// 6. Rollback to version 9 cleanly
	if err := m.Migrate(9); err != nil {
		t.Fatalf("failed rolling back to version 9 after cleanup: %v", err)
	}

	// Verify tables are dropped
	var jobsExistAfterDrop bool
	_ = connPool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.tables 
			WHERE table_name = 'repository_maintenance_jobs' AND table_schema = 'public'
		);
	`).Scan(&jobsExistAfterDrop)
	if jobsExistAfterDrop {
		t.Fatalf("expected repository_maintenance_jobs table to be dropped after rollback to v9")
	}
	t.Log("Verified Migration 10 down drops tables cleanly")

	// 7. Re-migrate to version 10 cleanly
	if err := m.Migrate(10); err != nil {
		t.Fatalf("failed re-migrating to version 10: %v", err)
	}
	t.Log("Verified re-migration to version 10 succeeded cleanly")
}
