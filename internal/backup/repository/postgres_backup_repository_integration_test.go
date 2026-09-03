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

func TestPostgresBackupRepository_CreateArtifact_Integration(t *testing.T) {
	testDBURL := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	if testDBURL == "" {
		t.Skip("skipping repository integration test: TEST_DATABASE_URL not set")
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

	// Ensure database is migrated through version 7 (Step A.2: artifact encryption metadata)
	if err := m.Migrate(7); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		t.Fatalf("failed migrating to version 7: %v", err)
	}

	pool, err := database.New(ctx, testDBURL)
	if err != nil {
		t.Fatalf("failed creating database pool: %v", err)
	}
	defer pool.Close()

	repo := NewPostgresBackupRepository(pool)

	org1ID := uuid.New()
	org2ID := uuid.New()
	res1ID := uuid.New()
	res1AltID := uuid.New()
	res2ID := uuid.New()

	localTargetID := uuid.New()
	s3TargetID := uuid.New()
	s3CompTargetID := uuid.New()
	disabledTargetID := uuid.New()
	org2TargetID := uuid.New()

	cleanup := func() {
		cleanupCtx := context.Background()
		_, _ = conn.Exec(cleanupCtx, "DELETE FROM backup_artifacts WHERE organization_id IN ($1, $2)", org1ID, org2ID)
		_, _ = conn.Exec(cleanupCtx, "DELETE FROM backup_runs WHERE organization_id IN ($1, $2)", org1ID, org2ID)
		_, _ = conn.Exec(cleanupCtx, "DELETE FROM backup_jobs WHERE organization_id IN ($1, $2)", org1ID, org2ID)
		_, _ = conn.Exec(cleanupCtx, "DELETE FROM backup_plans WHERE organization_id IN ($1, $2)", org1ID, org2ID)
		_, _ = conn.Exec(cleanupCtx, "DELETE FROM storage_targets WHERE organization_id IN ($1, $2)", org1ID, org2ID)
		_, _ = conn.Exec(cleanupCtx, "DELETE FROM resources WHERE organization_id IN ($1, $2)", org1ID, org2ID)
		_, _ = conn.Exec(cleanupCtx, "DELETE FROM organizations WHERE id IN ($1, $2)", org1ID, org2ID)
	}
	cleanup()
	defer cleanup()

	// 1. Insert organizations
	slug1 := fmt.Sprintf("org-1-%s", org1ID)
	slug2 := fmt.Sprintf("org-2-%s", org2ID)
	_, err = conn.Exec(ctx, `
		INSERT INTO organizations (id, name, slug, status, metadata, created_at, updated_at)
		VALUES ($1, 'Org 1', $2, 'active', '{}'::jsonb, NOW(), NOW()),
		       ($3, 'Org 2', $4, 'active', '{}'::jsonb, NOW(), NOW())`,
		org1ID, slug1, org2ID, slug2,
	)
	if err != nil {
		t.Fatalf("failed inserting organizations: %v", err)
	}

	// 2. Insert resources
	_, err = conn.Exec(ctx, `
		INSERT INTO resources (id, organization_id, name, type, status, metadata, created_at, updated_at)
		VALUES ($1, $2, 'Res 1', 'ubuntu_ssh', 'active', '{}'::jsonb, NOW(), NOW()),
		       ($3, $2, 'Res 1 Alt', 'ubuntu_ssh', 'active', '{}'::jsonb, NOW(), NOW()),
		       ($4, $5, 'Res 2', 'ubuntu_ssh', 'active', '{}'::jsonb, NOW(), NOW())`,
		res1ID, org1ID, res1AltID, res2ID, org2ID,
	)
	if err != nil {
		t.Fatalf("failed inserting resources: %v", err)
	}

	// 3. Insert storage targets
	_, err = conn.Exec(ctx, `
		INSERT INTO storage_targets (id, organization_id, name, type, status, is_default, credential_id, config, created_at, updated_at)
		VALUES
			($1, $2, 'Local Target', 'local', 'active', true, NULL, '{}'::jsonb, NOW(), NOW()),
			($3, $2, 'S3 Target', 's3', 'active', false, NULL, '{"bucket":"test-s3"}'::jsonb, NOW(), NOW()),
			($4, $2, 'S3 Comp Target', 's3_compatible', 'active', false, NULL, '{"bucket":"test-comp"}'::jsonb, NOW(), NOW()),
			($5, $2, 'Disabled S3 Target', 's3', 'disabled', false, NULL, '{"bucket":"test-dis"}'::jsonb, NOW(), NOW()),
			($6, $7, 'Org2 S3 Target', 's3', 'active', true, NULL, '{"bucket":"test-org2"}'::jsonb, NOW(), NOW())`,
		localTargetID, org1ID,
		s3TargetID,
		s3CompTargetID,
		disabledTargetID,
		org2TargetID, org2ID,
	)
	if err != nil {
		t.Fatalf("failed inserting storage targets: %v", err)
	}

	createJobAndRun := func(t *testing.T, jobID, runID, orgID, resID, targetID uuid.UUID, engineType, runStatus string) {
		_, err := conn.Exec(ctx, `
			INSERT INTO backup_jobs (id, organization_id, resource_id, trigger_type, backup_type, target_spec, status, engine_type, storage_target_id, created_at, updated_at)
			VALUES ($1, $2, $3, 'scheduled', 'mysql_database', '{"databases":["testdb"]}'::jsonb, 'running', $4, $5, NOW(), NOW())`,
			jobID, orgID, resID, engineType, targetID,
		)
		if err != nil {
			t.Fatalf("failed inserting backup job: %v", err)
		}
		_, err = conn.Exec(ctx, `
			INSERT INTO backup_runs (id, organization_id, job_id, attempt_number, status, started_at, created_at, updated_at)
			VALUES ($1, $2, $3, 1, $4, NOW(), NOW(), NOW())`,
			runID, orgID, jobID, runStatus,
		)
		if err != nil {
			t.Fatalf("failed inserting backup run: %v", err)
		}
	}

	// -------------------------------------------------------------------------
	// A. Local: direct_stream job + active Local target -> CreateArtifact succeeds
	// -------------------------------------------------------------------------
	t.Run("Scenario A: Local direct_stream job + active Local target succeeds", func(t *testing.T) {
		jobID := uuid.New()
		runID := uuid.New()
		createJobAndRun(t, jobID, runID, org1ID, res1ID, localTargetID, "direct_stream", "running")

		art := &domain.BackupArtifact{
			ID:                 uuid.New(),
			OrganizationID:     org1ID,
			RunID:              runID,
			ResourceID:         res1ID,
			StorageTargetID:    localTargetID,
			ArtifactType:       domain.ArtifactTypeDatabaseDump,
			Format:             domain.ArtifactFormatSQLGzip,
			TargetName:         "testdb",
			StorageReference:   fmt.Sprintf("organizations/%s/resources/%s/artifacts/%s.sql.gz", org1ID, res1ID, uuid.New()),
			SizeBytes:          1024,
			ChecksumAlgorithm:  domain.ChecksumAlgorithmSHA256,
			ChecksumHash:       "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			VerificationStatus: domain.VerificationStatusUnverified,
		}

		created, err := repo.CreateArtifact(ctx, art)
		if err != nil {
			t.Fatalf("expected CreateArtifact to succeed on Local target, got: %v", err)
		}
		if created.StorageTargetID != localTargetID {
			t.Errorf("expected storage_target_id %s, got %s", localTargetID, created.StorageTargetID)
		}
	})

	// -------------------------------------------------------------------------
	// B. S3: direct_stream job + active S3 target -> CreateArtifact succeeds
	// -------------------------------------------------------------------------
	t.Run("Scenario B: S3 direct_stream job + active S3 target succeeds", func(t *testing.T) {
		jobID := uuid.New()
		runID := uuid.New()
		createJobAndRun(t, jobID, runID, org1ID, res1ID, s3TargetID, "direct_stream", "running")

		art := &domain.BackupArtifact{
			ID:                 uuid.New(),
			OrganizationID:     org1ID,
			RunID:              runID,
			ResourceID:         res1ID,
			StorageTargetID:    s3TargetID,
			ArtifactType:       domain.ArtifactTypeDatabaseDump,
			Format:             domain.ArtifactFormatSQLGzip,
			TargetName:         "testdb",
			StorageReference:   fmt.Sprintf("organizations/%s/resources/%s/artifacts/%s.sql.gz", org1ID, res1ID, uuid.New()),
			SizeBytes:          2048,
			ChecksumAlgorithm:  domain.ChecksumAlgorithmSHA256,
			ChecksumHash:       "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			VerificationStatus: domain.VerificationStatusUnverified,
		}

		created, err := repo.CreateArtifact(ctx, art)
		if err != nil {
			t.Fatalf("expected CreateArtifact to succeed on S3 target, got: %v", err)
		}
		if created.StorageTargetID != s3TargetID {
			t.Errorf("expected storage_target_id %s, got %s", s3TargetID, created.StorageTargetID)
		}
	})

	// -------------------------------------------------------------------------
	// C. S3-compatible: direct_stream job + active s3_compatible target -> CreateArtifact succeeds
	// -------------------------------------------------------------------------
	t.Run("Scenario C: S3-compatible direct_stream job + active s3_compatible target succeeds", func(t *testing.T) {
		jobID := uuid.New()
		runID := uuid.New()
		createJobAndRun(t, jobID, runID, org1ID, res1ID, s3CompTargetID, "direct_stream", "running")

		art := &domain.BackupArtifact{
			ID:                 uuid.New(),
			OrganizationID:     org1ID,
			RunID:              runID,
			ResourceID:         res1ID,
			StorageTargetID:    s3CompTargetID,
			ArtifactType:       domain.ArtifactTypeDatabaseDump,
			Format:             domain.ArtifactFormatSQLGzip,
			TargetName:         "testdb",
			StorageReference:   fmt.Sprintf("organizations/%s/resources/%s/artifacts/%s.sql.gz", org1ID, res1ID, uuid.New()),
			SizeBytes:          4096,
			ChecksumAlgorithm:  domain.ChecksumAlgorithmSHA256,
			ChecksumHash:       "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			VerificationStatus: domain.VerificationStatusUnverified,
		}

		created, err := repo.CreateArtifact(ctx, art)
		if err != nil {
			t.Fatalf("expected CreateArtifact to succeed on s3_compatible target, got: %v", err)
		}
		if created.StorageTargetID != s3CompTargetID {
			t.Errorf("expected storage_target_id %s, got %s", s3CompTargetID, created.StorageTargetID)
		}
	})

	// -------------------------------------------------------------------------
	// D. Exact job-target binding:
	//    Job.storage_target_id = Target A
	//    Artifact.storage_target_id = Target B
	//    where both targets belong to SAME organization -> CreateArtifact fails
	// -------------------------------------------------------------------------
	t.Run("Scenario D: Same-org wrong-target binding fails", func(t *testing.T) {
		jobID := uuid.New()
		runID := uuid.New()
		createJobAndRun(t, jobID, runID, org1ID, res1ID, localTargetID, "direct_stream", "running")

		art := &domain.BackupArtifact{
			ID:                 uuid.New(),
			OrganizationID:     org1ID,
			RunID:              runID,
			ResourceID:         res1ID,
			StorageTargetID:    s3TargetID, // Target B (different target in org1)
			ArtifactType:       domain.ArtifactTypeDatabaseDump,
			Format:             domain.ArtifactFormatSQLGzip,
			TargetName:         "testdb",
			StorageReference:   fmt.Sprintf("organizations/%s/resources/%s/artifacts/%s.sql.gz", org1ID, res1ID, uuid.New()),
			SizeBytes:          1024,
			ChecksumAlgorithm:  domain.ChecksumAlgorithmSHA256,
			ChecksumHash:       "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			VerificationStatus: domain.VerificationStatusUnverified,
		}

		_, err := repo.CreateArtifact(ctx, art)
		if !errors.Is(err, domain.ErrArtifactChainMismatch) {
			t.Fatalf("expected ErrArtifactChainMismatch on wrong-target binding, got: %v", err)
		}
	})

	// -------------------------------------------------------------------------
	// E. Cross organization target -> fails
	// -------------------------------------------------------------------------
	t.Run("Scenario E: Cross organization target fails", func(t *testing.T) {
		jobID := uuid.New()
		runID := uuid.New()
		createJobAndRun(t, jobID, runID, org1ID, res1ID, localTargetID, "direct_stream", "running")

		art := &domain.BackupArtifact{
			ID:                 uuid.New(),
			OrganizationID:     org1ID,
			RunID:              runID,
			ResourceID:         res1ID,
			StorageTargetID:    org2TargetID, // Org 2's target passed for Org 1 run
			ArtifactType:       domain.ArtifactTypeDatabaseDump,
			Format:             domain.ArtifactFormatSQLGzip,
			TargetName:         "testdb",
			StorageReference:   fmt.Sprintf("organizations/%s/resources/%s/artifacts/%s.sql.gz", org1ID, res1ID, uuid.New()),
			SizeBytes:          1024,
			ChecksumAlgorithm:  domain.ChecksumAlgorithmSHA256,
			ChecksumHash:       "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			VerificationStatus: domain.VerificationStatusUnverified,
		}

		_, err := repo.CreateArtifact(ctx, art)
		if !errors.Is(err, domain.ErrArtifactChainMismatch) {
			t.Fatalf("expected ErrArtifactChainMismatch on cross-org target, got: %v", err)
		}
	})

	// -------------------------------------------------------------------------
	// F. Inactive target -> fails
	// -------------------------------------------------------------------------
	t.Run("Scenario F: Inactive target fails", func(t *testing.T) {
		// Case 1: Job created with disabled target (violates active status)
		jobID := uuid.New()
		runID := uuid.New()
		createJobAndRun(t, jobID, runID, org1ID, res1ID, disabledTargetID, "direct_stream", "running")

		art := &domain.BackupArtifact{
			ID:                 uuid.New(),
			OrganizationID:     org1ID,
			RunID:              runID,
			ResourceID:         res1ID,
			StorageTargetID:    disabledTargetID,
			ArtifactType:       domain.ArtifactTypeDatabaseDump,
			Format:             domain.ArtifactFormatSQLGzip,
			TargetName:         "testdb",
			StorageReference:   fmt.Sprintf("organizations/%s/resources/%s/artifacts/%s.sql.gz", org1ID, res1ID, uuid.New()),
			SizeBytes:          1024,
			ChecksumAlgorithm:  domain.ChecksumAlgorithmSHA256,
			ChecksumHash:       "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			VerificationStatus: domain.VerificationStatusUnverified,
		}

		_, err := repo.CreateArtifact(ctx, art)
		if !errors.Is(err, domain.ErrArtifactChainMismatch) {
			t.Fatalf("expected ErrArtifactChainMismatch on disabled target, got: %v", err)
		}

		// Case 2: Target becomes archived after job creation
		archivedTargetID := uuid.New()
		_, err = conn.Exec(ctx, `
			INSERT INTO storage_targets (id, organization_id, name, type, status, is_default, config, created_at, updated_at)
			VALUES ($1, $2, 'To Archive', 's3', 'active', false, '{}'::jsonb, NOW(), NOW())`,
			archivedTargetID, org1ID,
		)
		if err != nil {
			t.Fatalf("failed inserting target to archive: %v", err)
		}

		job2ID := uuid.New()
		run2ID := uuid.New()
		createJobAndRun(t, job2ID, run2ID, org1ID, res1ID, archivedTargetID, "direct_stream", "running")

		// Update target to archived before CreateArtifact
		_, err = conn.Exec(ctx, "UPDATE storage_targets SET status = 'archived' WHERE id = $1", archivedTargetID)
		if err != nil {
			t.Fatalf("failed setting target to archived: %v", err)
		}

		art2 := &domain.BackupArtifact{
			ID:                 uuid.New(),
			OrganizationID:     org1ID,
			RunID:              run2ID,
			ResourceID:         res1ID,
			StorageTargetID:    archivedTargetID,
			ArtifactType:       domain.ArtifactTypeDatabaseDump,
			Format:             domain.ArtifactFormatSQLGzip,
			TargetName:         "testdb",
			StorageReference:   fmt.Sprintf("organizations/%s/resources/%s/artifacts/%s.sql.gz", org1ID, res1ID, uuid.New()),
			SizeBytes:          1024,
			ChecksumAlgorithm:  domain.ChecksumAlgorithmSHA256,
			ChecksumHash:       "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			VerificationStatus: domain.VerificationStatusUnverified,
		}

		_, err = repo.CreateArtifact(ctx, art2)
		if !errors.Is(err, domain.ErrArtifactChainMismatch) {
			t.Fatalf("expected ErrArtifactChainMismatch on archived target, got: %v", err)
		}
	})

	// -------------------------------------------------------------------------
	// G. Non-running run -> fails
	// -------------------------------------------------------------------------
	t.Run("Scenario G: Non-running run fails", func(t *testing.T) {
		jobID := uuid.New()
		runID := uuid.New()
		createJobAndRun(t, jobID, runID, org1ID, res1ID, localTargetID, "direct_stream", "failed") // run status failed

		art := &domain.BackupArtifact{
			ID:                 uuid.New(),
			OrganizationID:     org1ID,
			RunID:              runID,
			ResourceID:         res1ID,
			StorageTargetID:    localTargetID,
			ArtifactType:       domain.ArtifactTypeDatabaseDump,
			Format:             domain.ArtifactFormatSQLGzip,
			TargetName:         "testdb",
			StorageReference:   fmt.Sprintf("organizations/%s/resources/%s/artifacts/%s.sql.gz", org1ID, res1ID, uuid.New()),
			SizeBytes:          1024,
			ChecksumAlgorithm:  domain.ChecksumAlgorithmSHA256,
			ChecksumHash:       "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			VerificationStatus: domain.VerificationStatusUnverified,
		}

		_, err := repo.CreateArtifact(ctx, art)
		if !errors.Is(err, domain.ErrArtifactChainMismatch) {
			t.Fatalf("expected ErrArtifactChainMismatch on failed run status, got: %v", err)
		}
	})

	// -------------------------------------------------------------------------
	// H. Resource mismatch -> fails
	// -------------------------------------------------------------------------
	t.Run("Scenario H: Resource mismatch fails", func(t *testing.T) {
		jobID := uuid.New()
		runID := uuid.New()
		createJobAndRun(t, jobID, runID, org1ID, res1ID, localTargetID, "direct_stream", "running")

		// Case 1: Pass different resource in same org
		art := &domain.BackupArtifact{
			ID:                 uuid.New(),
			OrganizationID:     org1ID,
			RunID:              runID,
			ResourceID:         res1AltID, // different resource
			StorageTargetID:    localTargetID,
			ArtifactType:       domain.ArtifactTypeDatabaseDump,
			Format:             domain.ArtifactFormatSQLGzip,
			TargetName:         "testdb",
			StorageReference:   fmt.Sprintf("organizations/%s/resources/%s/artifacts/%s.sql.gz", org1ID, res1ID, uuid.New()),
			SizeBytes:          1024,
			ChecksumAlgorithm:  domain.ChecksumAlgorithmSHA256,
			ChecksumHash:       "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			VerificationStatus: domain.VerificationStatusUnverified,
		}

		_, err := repo.CreateArtifact(ctx, art)
		if !errors.Is(err, domain.ErrArtifactChainMismatch) {
			t.Fatalf("expected ErrArtifactChainMismatch on resource mismatch (same org), got: %v", err)
		}

		// Case 2: Pass resource from different org
		art.ResourceID = res2ID
		_, err = repo.CreateArtifact(ctx, art)
		if !errors.Is(err, domain.ErrArtifactChainMismatch) {
			t.Fatalf("expected ErrArtifactChainMismatch on resource mismatch (cross org), got: %v", err)
		}
	})

	// -------------------------------------------------------------------------
	// I. Corrupted/unsupported job engine -> fails closed
	// -------------------------------------------------------------------------
	t.Run("Scenario I: Unsupported job engine fails closed", func(t *testing.T) {
		jobID := uuid.New()
		runID := uuid.New()
		createJobAndRun(t, jobID, runID, org1ID, res1ID, localTargetID, "direct_stream", "running")

		// Temporarily drop CHECK constraint on engine_type to test corrupted/unsupported engine value
		_, err := conn.Exec(ctx, "ALTER TABLE backup_jobs DROP CONSTRAINT chk_backup_jobs_engine_type")
		if err != nil {
			t.Fatalf("failed dropping engine_type check constraint: %v", err)
		}
		defer func() {
			_, _ = conn.Exec(ctx, "UPDATE backup_jobs SET engine_type = 'direct_stream' WHERE engine_type = 'corrupted_engine'")
			_, _ = conn.Exec(ctx, "ALTER TABLE backup_jobs ADD CONSTRAINT chk_backup_jobs_engine_type CHECK (engine_type IN ('direct_stream'))")
		}()

		_, err = conn.Exec(ctx, "UPDATE backup_jobs SET engine_type = 'corrupted_engine' WHERE id = $1", jobID)
		if err != nil {
			t.Fatalf("failed setting corrupted_engine: %v", err)
		}

		art := &domain.BackupArtifact{
			ID:                 uuid.New(),
			OrganizationID:     org1ID,
			RunID:              runID,
			ResourceID:         res1ID,
			StorageTargetID:    localTargetID,
			ArtifactType:       domain.ArtifactTypeDatabaseDump,
			Format:             domain.ArtifactFormatSQLGzip,
			TargetName:         "testdb",
			StorageReference:   fmt.Sprintf("organizations/%s/resources/%s/artifacts/%s.sql.gz", org1ID, res1ID, uuid.New()),
			SizeBytes:          1024,
			ChecksumAlgorithm:  domain.ChecksumAlgorithmSHA256,
			ChecksumHash:       "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			VerificationStatus: domain.VerificationStatusUnverified,
		}

		_, err = repo.CreateArtifact(ctx, art)
		if !errors.Is(err, domain.ErrArtifactChainMismatch) {
			t.Fatalf("expected ErrArtifactChainMismatch on corrupted job engine, got: %v", err)
		}
	})
}
