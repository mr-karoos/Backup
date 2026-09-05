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

func TestMigrations_StepA4_Integration(t *testing.T) {
	testDBURL := os.Getenv("TEST_DATABASE_URL")
	if testDBURL == "" {
		t.Skip("skipping migration Step A.4 integration test: TEST_DATABASE_URL not set")
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

	// 1. Migrate to version 8 (Step A.3 baseline)
	if err := m.Migrate(8); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		t.Fatalf("failed migrating to version 8 baseline: %v", err)
	}

	orgA := uuid.New()
	orgB := uuid.New()
	resA := uuid.New()
	resB := uuid.New()
	targetA := uuid.New()
	credA := uuid.New()
	repoA := uuid.New()
	jobA := uuid.New()
	runA := uuid.New()
	directArtID := uuid.New()
	resticArtID := uuid.New()

	cleanup := func() {
		cleanupCtx := context.Background()
		_, _ = connPool.Exec(cleanupCtx, "DELETE FROM backup_artifacts WHERE organization_id IN ($1, $2)", orgA, orgB)
		_, _ = connPool.Exec(cleanupCtx, "DELETE FROM backup_runs WHERE organization_id IN ($1, $2)", orgA, orgB)
		_, _ = connPool.Exec(cleanupCtx, "DELETE FROM backup_jobs WHERE organization_id IN ($1, $2)", orgA, orgB)
		_, _ = connPool.Exec(cleanupCtx, "DELETE FROM backup_repositories WHERE organization_id IN ($1, $2)", orgA, orgB)
		_, _ = connPool.Exec(cleanupCtx, "DELETE FROM credentials WHERE organization_id IN ($1, $2)", orgA, orgB)
		_, _ = connPool.Exec(cleanupCtx, "DELETE FROM storage_targets WHERE organization_id IN ($1, $2)", orgA, orgB)
		_, _ = connPool.Exec(cleanupCtx, "DELETE FROM resources WHERE organization_id IN ($1, $2)", orgA, orgB)
		_, _ = connPool.Exec(cleanupCtx, "DELETE FROM organizations WHERE id IN ($1, $2)", orgA, orgB)
	}
	cleanup()
	defer cleanup()

	// 2. Seed baseline schema at v8
	slugA := fmt.Sprintf("org-a4-a-%s", orgA.String()[:8])
	slugB := fmt.Sprintf("org-a4-b-%s", orgB.String()[:8])
	_, err = connPool.Exec(ctx, `
		INSERT INTO organizations (id, name, slug, status, metadata, created_at, updated_at)
		VALUES ($1, 'Org A4 A', $2, 'active', '{}'::jsonb, NOW(), NOW()),
		       ($3, 'Org A4 B', $4, 'active', '{}'::jsonb, NOW(), NOW())`,
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

	_, err = connPool.Exec(ctx, `
		INSERT INTO credentials (id, organization_id, name, type, managed_by, encrypted_secret, nonce, auth_tag, key_version, created_at, updated_at)
		VALUES ($1, $2, 'Restic Key A', 'restic_repository_key', 'system', '\x010203', '\x040506', '\x070809', 1, NOW(), NOW())`,
		credA, orgA)
	if err != nil {
		t.Fatalf("failed seeding credentials: %v", err)
	}

	_, err = connPool.Exec(ctx, `
		INSERT INTO backup_repositories (id, organization_id, resource_id, storage_target_id, credential_id, repository_type, repository_locator, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, 'local', 'organizations/' || $2 || '/resources/' || $3 || '/restic', 'ready', NOW(), NOW())`,
		repoA, orgA, resA, targetA, credA)
	if err != nil {
		t.Fatalf("failed seeding backup_repository: %v", err)
	}

	_, err = connPool.Exec(ctx, `
		INSERT INTO backup_jobs (id, organization_id, resource_id, backup_type, status, created_at, updated_at)
		VALUES ($1, $2, $3, 'mysql_database', 'completed', NOW(), NOW())`,
		jobA, orgA, resA)
	if err != nil {
		t.Fatalf("failed seeding backup_jobs: %v", err)
	}

	_, err = connPool.Exec(ctx, `
		INSERT INTO backup_runs (id, organization_id, job_id, status, run_type, trigger_source, created_at, updated_at)
		VALUES ($1, $2, $3, 'completed', 'manual', 'user', NOW(), NOW())`,
		runA, orgA, jobA)
	if err != nil {
		t.Fatalf("failed seeding backup_runs: %v", err)
	}

	// Seed pre-existing direct_stream artifact in v8
	_, err = connPool.Exec(ctx, `
		INSERT INTO backup_artifacts (id, organization_id, run_id, resource_id, storage_target_id, artifact_type, format, target_name, storage_reference, size_bytes, checksum_algorithm, checksum_hash, verification_status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, 'database_dump', 'direct_stream', 'mydb', 'mydb_dump.sql.gz', 2048, 'sha256', 'hash123', 'verified', NOW(), NOW())`,
		directArtID, orgA, runA, resA, targetA)
	if err != nil {
		t.Fatalf("failed seeding pre-existing direct_stream artifact: %v", err)
	}

	// 3. Migrate UP to version 9 (Step A.4)
	if err := m.Migrate(9); err != nil {
		t.Fatalf("failed migrating to version 9: %v", err)
	}

	// Verify pre-existing direct_stream artifact retained data and has null restic columns
	var format, storRef, chkAlg, chkHash string
	var sizeBytes int64
	var repoID *uuid.UUID
	var snapID *string
	var logicalSizeBytes *int64
	err = connPool.QueryRow(ctx, `
		SELECT format, storage_reference, size_bytes, checksum_algorithm, checksum_hash, repository_id, snapshot_id, logical_size_bytes
		FROM backup_artifacts WHERE id = $1`, directArtID).Scan(&format, &storRef, &sizeBytes, &chkAlg, &chkHash, &repoID, &snapID, &logicalSizeBytes)
	if err != nil {
		t.Fatalf("failed querying migrated artifact: %v", err)
	}
	if format != "direct_stream" || storRef != "mydb_dump.sql.gz" || sizeBytes != 2048 {
		t.Errorf("direct_stream artifact fields corrupted: format=%s, ref=%s, size=%d", format, storRef, sizeBytes)
	}
	if repoID != nil || snapID != nil || logicalSizeBytes != nil {
		t.Errorf("expected null restic fields for direct_stream artifact, got repoID=%v, snapID=%v, logicalSize=%v", repoID, snapID, logicalSizeBytes)
	}

	// 4. Test Constraints Matrix

	// Scenario 1: Valid restic_snapshot artifact insertion succeeds
	validSnapID := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	logicalSizeVal := int64(1048576)
	_, err = connPool.Exec(ctx, `
		INSERT INTO backup_artifacts (id, organization_id, run_id, resource_id, storage_target_id, artifact_type, format, target_name, storage_reference, size_bytes, checksum_algorithm, checksum_hash, repository_id, snapshot_id, logical_size_bytes, verification_status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, 'database_dump', 'restic_snapshot', 'mydb', '', 0, 'sha256', '', $6, $7, $8, 'verified', NOW(), NOW())`,
		resticArtID, orgA, runA, resA, targetA, repoA, validSnapID, logicalSizeVal)
	if err != nil {
		t.Fatalf("failed inserting valid restic_snapshot artifact: %v", err)
	}

	// Scenario 2: restic_snapshot with non-empty storage_reference -> REJECTED
	badArtID2 := uuid.New()
	_, err = connPool.Exec(ctx, `
		INSERT INTO backup_artifacts (id, organization_id, run_id, resource_id, storage_target_id, artifact_type, format, target_name, storage_reference, size_bytes, checksum_algorithm, checksum_hash, repository_id, snapshot_id, logical_size_bytes, verification_status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, 'database_dump', 'restic_snapshot', 'mydb', 'forbidden_ref.tar', 0, 'sha256', '', $6, $7, $8, 'verified', NOW(), NOW())`,
		badArtID2, orgA, runA, resA, targetA, repoA, validSnapID, logicalSizeVal)
	if err == nil {
		t.Fatalf("expected failure for restic_snapshot with non-empty storage_reference")
	}

	// Scenario 3: restic_snapshot with non-zero size_bytes -> REJECTED
	badArtID3 := uuid.New()
	_, err = connPool.Exec(ctx, `
		INSERT INTO backup_artifacts (id, organization_id, run_id, resource_id, storage_target_id, artifact_type, format, target_name, storage_reference, size_bytes, checksum_algorithm, checksum_hash, repository_id, snapshot_id, logical_size_bytes, verification_status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, 'database_dump', 'restic_snapshot', 'mydb', '', 100, 'sha256', '', $6, $7, $8, 'verified', NOW(), NOW())`,
		badArtID3, orgA, runA, resA, targetA, repoA, validSnapID, logicalSizeVal)
	if err == nil {
		t.Fatalf("expected failure for restic_snapshot with non-zero size_bytes")
	}

	// Scenario 4: restic_snapshot with null repository_id -> REJECTED
	badArtID4 := uuid.New()
	_, err = connPool.Exec(ctx, `
		INSERT INTO backup_artifacts (id, organization_id, run_id, resource_id, storage_target_id, artifact_type, format, target_name, storage_reference, size_bytes, checksum_algorithm, checksum_hash, repository_id, snapshot_id, logical_size_bytes, verification_status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, 'database_dump', 'restic_snapshot', 'mydb', '', 0, 'sha256', '', NULL, $6, $7, 'verified', NOW(), NOW())`,
		badArtID4, orgA, runA, resA, targetA, validSnapID, logicalSizeVal)
	if err == nil {
		t.Fatalf("expected failure for restic_snapshot with NULL repository_id")
	}

	// Scenario 5: restic_snapshot with null snapshot_id -> REJECTED
	badArtID5 := uuid.New()
	_, err = connPool.Exec(ctx, `
		INSERT INTO backup_artifacts (id, organization_id, run_id, resource_id, storage_target_id, artifact_type, format, target_name, storage_reference, size_bytes, checksum_algorithm, checksum_hash, repository_id, snapshot_id, logical_size_bytes, verification_status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, 'database_dump', 'restic_snapshot', 'mydb', '', 0, 'sha256', '', $6, NULL, $7, 'verified', NOW(), NOW())`,
		badArtID5, orgA, runA, resA, targetA, repoA, logicalSizeVal)
	if err == nil {
		t.Fatalf("expected failure for restic_snapshot with NULL snapshot_id")
	}

	// Scenario 6: restic_snapshot with invalid hex snapshot_id -> REJECTED
	badArtID6 := uuid.New()
	_, err = connPool.Exec(ctx, `
		INSERT INTO backup_artifacts (id, organization_id, run_id, resource_id, storage_target_id, artifact_type, format, target_name, storage_reference, size_bytes, checksum_algorithm, checksum_hash, repository_id, snapshot_id, logical_size_bytes, verification_status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, 'database_dump', 'restic_snapshot', 'mydb', '', 0, 'sha256', '', $6, 'not-a-valid-hex-id!', $7, 'verified', NOW(), NOW())`,
		badArtID6, orgA, runA, resA, targetA, repoA, logicalSizeVal)
	if err == nil {
		t.Fatalf("expected failure for restic_snapshot with invalid hex snapshot_id")
	}

	// Scenario 7: direct_stream with empty storage_reference -> REJECTED
	badArtID7 := uuid.New()
	_, err = connPool.Exec(ctx, `
		INSERT INTO backup_artifacts (id, organization_id, run_id, resource_id, storage_target_id, artifact_type, format, target_name, storage_reference, size_bytes, checksum_algorithm, checksum_hash, verification_status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, 'database_dump', 'direct_stream', 'mydb', '', 2048, 'sha256', 'hash', 'verified', NOW(), NOW())`,
		badArtID7, orgA, runA, resA, targetA)
	if err == nil {
		t.Fatalf("expected failure for direct_stream with empty storage_reference")
	}

	// Scenario 8: direct_stream with repository_id set -> REJECTED
	badArtID8 := uuid.New()
	_, err = connPool.Exec(ctx, `
		INSERT INTO backup_artifacts (id, organization_id, run_id, resource_id, storage_target_id, artifact_type, format, target_name, storage_reference, size_bytes, checksum_algorithm, checksum_hash, repository_id, verification_status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, 'database_dump', 'direct_stream', 'mydb', 'ref.gz', 2048, 'sha256', 'hash', $6, 'verified', NOW(), NOW())`,
		badArtID8, orgA, runA, resA, targetA, repoA)
	if err == nil {
		t.Fatalf("expected failure for direct_stream with repository_id set")
	}

	// Scenario 9: direct_stream with snapshot_id set -> REJECTED
	badArtID9 := uuid.New()
	_, err = connPool.Exec(ctx, `
		INSERT INTO backup_artifacts (id, organization_id, run_id, resource_id, storage_target_id, artifact_type, format, target_name, storage_reference, size_bytes, checksum_algorithm, checksum_hash, snapshot_id, verification_status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, 'database_dump', 'direct_stream', 'mydb', 'ref.gz', 2048, 'sha256', 'hash', $6, 'verified', NOW(), NOW())`,
		badArtID9, orgA, runA, resA, targetA, validSnapID)
	if err == nil {
		t.Fatalf("expected failure for direct_stream with snapshot_id set")
	}

	// Scenario 10: Deduplication unique index: duplicate (repository_id, snapshot_id, artifact_type) -> REJECTED
	dupArtID := uuid.New()
	_, err = connPool.Exec(ctx, `
		INSERT INTO backup_artifacts (id, organization_id, run_id, resource_id, storage_target_id, artifact_type, format, target_name, storage_reference, size_bytes, checksum_algorithm, checksum_hash, repository_id, snapshot_id, logical_size_bytes, verification_status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, 'database_dump', 'restic_snapshot', 'mydb_dup', '', 0, 'sha256', '', $6, $7, $8, 'verified', NOW(), NOW())`,
		dupArtID, orgA, runA, resA, targetA, repoA, validSnapID, logicalSizeVal)
	if err == nil {
		t.Fatalf("expected failure for duplicate snapshot in same repository and artifact_type")
	}

	// 5. Fail-Closed DOWN Migration Safety Check
	// Attempting down migration with restic_snapshot artifact present MUST FAIL with RAISE EXCEPTION
	downErr := m.Migrate(8)
	if downErr == nil {
		t.Fatalf("SAFETY VIOLATION: down migration succeeded while restic_snapshot artifacts existed!")
	}
	if !strings.Contains(downErr.Error(), "cannot downgrade schema with restic_snapshot artifacts present") {
		t.Fatalf("expected fail-closed exception message, got: %v", downErr)
	}

	// Clean up restic artifact
	_, err = connPool.Exec(ctx, "DELETE FROM backup_artifacts WHERE id = $1", resticArtID)
	if err != nil {
		t.Fatalf("failed deleting restic artifact: %v", err)
	}

	// Now DOWN migration to 8 should succeed
	if err := m.Migrate(8); err != nil {
		t.Fatalf("expected down migration to succeed after deleting restic artifacts, got: %v", err)
	}

	// 6. Re-apply UP to version 9 should succeed cleanly
	if err := m.Migrate(9); err != nil {
		t.Fatalf("expected re-applying migration 9 to succeed, got: %v", err)
	}
}
