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

func TestMigrations_StepA2_Integration(t *testing.T) {
	testDBURL := os.Getenv("TEST_DATABASE_URL")
	if testDBURL == "" {
		t.Skip("skipping migration Step A.2 integration test: TEST_DATABASE_URL not set")
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

	// 1. Migrate to version 6 (Step A.1 baseline)
	if err := m.Migrate(6); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		t.Fatalf("failed migrating to version 6 baseline: %v", err)
	}

	orgID := uuid.New()
	resID := uuid.New()
	targetID := uuid.New()
	jobID := uuid.New()
	runID := uuid.New()
	legacyArtID := uuid.New()
	encryptedArtID := uuid.New()

	cleanup := func() {
		cleanupCtx := context.Background()
		_, _ = connPool.Exec(cleanupCtx, "DELETE FROM backup_artifacts WHERE organization_id = $1", orgID)
		_, _ = connPool.Exec(cleanupCtx, "DELETE FROM backup_runs WHERE organization_id = $1", orgID)
		_, _ = connPool.Exec(cleanupCtx, "DELETE FROM backup_jobs WHERE organization_id = $1", orgID)
		_, _ = connPool.Exec(cleanupCtx, "DELETE FROM storage_targets WHERE organization_id = $1", orgID)
		_, _ = connPool.Exec(cleanupCtx, "DELETE FROM resources WHERE organization_id = $1", orgID)
		_, _ = connPool.Exec(cleanupCtx, "DELETE FROM organizations WHERE id = $1", orgID)
	}
	cleanup()
	defer cleanup()

	// Seed Org, Resource, Target, Job, Run
	slug := fmt.Sprintf("org-a2-%s", orgID.String()[:8])
	_, err = connPool.Exec(ctx, `
		INSERT INTO organizations (id, name, slug, status, metadata, created_at, updated_at)
		VALUES ($1, 'Org A2', $2, 'active', '{}'::jsonb, NOW(), NOW())`,
		orgID, slug)
	if err != nil {
		t.Fatalf("failed inserting org: %v", err)
	}

	_, err = connPool.Exec(ctx, `
		INSERT INTO resources (id, organization_id, name, type, status, metadata, created_at, updated_at)
		VALUES ($1, $2, 'Res A2', 'ubuntu_ssh', 'active', '{}'::jsonb, NOW(), NOW())`,
		resID, orgID)
	if err != nil {
		t.Fatalf("failed inserting resource: %v", err)
	}

	_, err = connPool.Exec(ctx, `
		INSERT INTO storage_targets (id, organization_id, name, type, status, is_default, config, created_at, updated_at)
		VALUES ($1, $2, 'Local Target', 'local', 'active', true, '{}'::jsonb, NOW(), NOW())`,
		targetID, orgID)
	if err != nil {
		t.Fatalf("failed inserting storage target: %v", err)
	}

	_, err = connPool.Exec(ctx, `
		INSERT INTO backup_jobs (id, organization_id, resource_id, trigger_type, backup_type, engine_type, storage_target_id, target_spec, status, created_at, updated_at)
		VALUES ($1, $2, $3, 'scheduled', 'mysql_database', 'direct_stream', $4, '{"databases":["testdb"]}'::jsonb, 'completed', NOW(), NOW())`,
		jobID, orgID, resID, targetID)
	if err != nil {
		t.Fatalf("failed inserting job: %v", err)
	}

	_, err = connPool.Exec(ctx, `
		INSERT INTO backup_runs (id, organization_id, job_id, attempt_number, status, started_at, created_at, updated_at)
		VALUES ($1, $2, $3, 1, 'success', NOW(), NOW(), NOW())`,
		runID, orgID, jobID)
	if err != nil {
		t.Fatalf("failed inserting run: %v", err)
	}

	// Insert legacy unencrypted artifact (v6 schema: no stored_size_bytes or engine_metadata)
	legacyStorageRef := fmt.Sprintf("%s/%s/%s/%s.sql.gz", orgID, resID, runID, legacyArtID)
	legacyChecksum := "1111111111111111111111111111111111111111111111111111111111111111"
	_, err = connPool.Exec(ctx, `
		INSERT INTO backup_artifacts (
			id, organization_id, run_id, resource_id, storage_target_id,
			artifact_type, format, target_name, storage_reference, size_bytes,
			checksum_algorithm, checksum_hash, verification_status, is_deleted,
			created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5,
			'database_dump', 'sql_gzip', 'testdb', $6, 1024,
			'sha256', $7, 'verified', false,
			NOW(), NOW()
		)`,
		legacyArtID, orgID, runID, resID, targetID, legacyStorageRef, legacyChecksum)
	if err != nil {
		t.Fatalf("failed inserting legacy artifact: %v", err)
	}

	// 2. Apply Migration v7 (Step A.2)
	if err := m.Migrate(7); err != nil {
		t.Fatalf("failed applying migration v7: %v", err)
	}

	// 3. Verify legacy artifact in v7
	var gotRef, gotChecksum string
	var gotSize int64
	var gotStoredSize *int64
	var gotEngineMeta []byte

	err = connPool.QueryRow(ctx, `
		SELECT storage_reference, size_bytes, checksum_hash, stored_size_bytes, engine_metadata
		FROM backup_artifacts WHERE id = $1`, legacyArtID).Scan(
		&gotRef, &gotSize, &gotChecksum, &gotStoredSize, &gotEngineMeta)
	if err != nil {
		t.Fatalf("failed querying legacy artifact in v7: %v", err)
	}

	if gotRef != legacyStorageRef || gotSize != 1024 || gotChecksum != legacyChecksum {
		t.Errorf("legacy artifact data altered: ref=%s, size=%d, checksum=%s", gotRef, gotSize, gotChecksum)
	}
	if gotStoredSize != nil {
		t.Errorf("expected legacy stored_size_bytes to be NULL, got %d", *gotStoredSize)
	}
	if string(gotEngineMeta) != "{}" {
		t.Errorf("expected legacy engine_metadata to default to '{}', got %s", string(gotEngineMeta))
	}

	// 4. Insert valid encrypted Direct Stream artifact metadata
	encStorageRef := fmt.Sprintf("%s/%s/%s/%s.sql.gz", orgID, resID, runID, encryptedArtID)
	encPlainChecksum := "2222222222222222222222222222222222222222222222222222222222222222"
	encCiphertextHash := "3333333333333333333333333333333333333333333333333333333333333333"
	_, err = connPool.Exec(ctx, `
		INSERT INTO backup_artifacts (
			id, organization_id, run_id, resource_id, storage_target_id,
			artifact_type, format, target_name, storage_reference, size_bytes,
			checksum_algorithm, checksum_hash, verification_status, is_deleted,
			stored_size_bytes, engine_metadata, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5,
			'database_dump', 'sql_gzip', 'testdb', $6, 2048,
			'sha256', $7, 'verified', false,
			2154, $8::jsonb, NOW(), NOW()
		)`,
		encryptedArtID, orgID, runID, resID, targetID, encStorageRef, encPlainChecksum,
		fmt.Sprintf(`{"ciphertext_sha256": "%s"}`, encCiphertextHash))
	if err != nil {
		t.Fatalf("failed inserting valid encrypted artifact metadata: %v", err)
	}

	// 5. Verify invalid stored_size_bytes rejected (negative or zero)
	invArtID1 := uuid.New()
	_, err = connPool.Exec(ctx, `
		INSERT INTO backup_artifacts (
			id, organization_id, run_id, resource_id, storage_target_id,
			artifact_type, format, target_name, storage_reference, size_bytes,
			checksum_algorithm, checksum_hash, verification_status, is_deleted,
			stored_size_bytes, engine_metadata, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5,
			'database_dump', 'sql_gzip', 'testdb', 'ref', 100,
			'sha256', 'hash', 'unverified', false,
			-1, '{"ciphertext_sha256":"3333333333333333333333333333333333333333333333333333333333333333"}'::jsonb, NOW(), NOW()
		)`, invArtID1, orgID, runID, resID, targetID)
	if err == nil {
		t.Fatal("expected failure on negative stored_size_bytes, got nil")
	}

	// 6. Verify invalid ciphertext hash rejected
	invArtID2 := uuid.New()
	_, err = connPool.Exec(ctx, `
		INSERT INTO backup_artifacts (
			id, organization_id, run_id, resource_id, storage_target_id,
			artifact_type, format, target_name, storage_reference, size_bytes,
			checksum_algorithm, checksum_hash, verification_status, is_deleted,
			stored_size_bytes, engine_metadata, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5,
			'database_dump', 'sql_gzip', 'testdb', 'ref', 100,
			'sha256', 'hash', 'unverified', false,
			1000, '{"ciphertext_sha256":"not-a-valid-64-hex"}'::jsonb, NOW(), NOW()
		)`, invArtID2, orgID, runID, resID, targetID)
	if err == nil {
		t.Fatal("expected failure on invalid ciphertext hash, got nil")
	}

	// 7. Verify DOWN migration fails closed when encrypted artifacts exist
	downErr := m.Steps(-1)
	if downErr == nil {
		t.Fatal("expected down migration to fail closed with encrypted artifacts present, got nil")
	}
	if !strings.Contains(downErr.Error(), "cannot downgrade while encrypted artifacts exist") {
		t.Errorf("expected fail-closed downgrade error, got: %v", downErr)
	}

	// 8. Remove the encrypted artifact row and verify DOWN succeeds
	_, err = connPool.Exec(ctx, "DELETE FROM backup_artifacts WHERE id = $1", encryptedArtID)
	if err != nil {
		t.Fatalf("failed deleting encrypted artifact row: %v", err)
	}

	// Force version back to 7 if dirty from failed step
	_ = m.Force(7)

	if err := m.Steps(-1); err != nil {
		t.Fatalf("expected down migration to succeed after removing encrypted rows, got: %v", err)
	}

	// Verify columns dropped in v6
	var colCount int
	err = connPool.QueryRow(ctx, `
		SELECT COUNT(*) FROM information_schema.columns
		WHERE table_name = 'backup_artifacts' AND column_name IN ('stored_size_bytes', 'engine_metadata')`).Scan(&colCount)
	if err != nil {
		t.Fatalf("failed querying columns: %v", err)
	}
	if colCount != 0 {
		t.Errorf("expected 0 A.2 columns in v6, found %d", colCount)
	}

	// 9. Re-apply UP to v7
	if err := m.Migrate(7); err != nil {
		t.Fatalf("failed re-applying migration v7: %v", err)
	}
}
