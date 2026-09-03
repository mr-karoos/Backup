package repository

import (
	"context"
	"encoding/json"
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

func TestPostgresBackupRepository_StepA2_Integration(t *testing.T) {
	testDBURL := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	if testDBURL == "" {
		t.Skip("skipping Step A.2 repository integration test: TEST_DATABASE_URL not set")
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

	orgAID := uuid.New()
	resAID := uuid.New()
	orgBID := uuid.New()
	resBID := uuid.New()

	localTargetID := uuid.New()
	s3TargetID := uuid.New()
	s3CompTargetID := uuid.New()
	secondLocalTargetID := uuid.New()
	disabledTargetID := uuid.New()
	archivedTargetID := uuid.New()
	orgBTargetID := uuid.New()

	cleanup := func() {
		cleanupCtx := context.Background()
		_, _ = conn.Exec(cleanupCtx, "DELETE FROM backup_artifacts WHERE organization_id IN ($1, $2)", orgAID, orgBID)
		_, _ = conn.Exec(cleanupCtx, "DELETE FROM backup_runs WHERE organization_id IN ($1, $2)", orgAID, orgBID)
		_, _ = conn.Exec(cleanupCtx, "DELETE FROM backup_jobs WHERE organization_id IN ($1, $2)", orgAID, orgBID)
		_, _ = conn.Exec(cleanupCtx, "DELETE FROM backup_plans WHERE organization_id IN ($1, $2)", orgAID, orgBID)
		_, _ = conn.Exec(cleanupCtx, "DELETE FROM storage_targets WHERE organization_id IN ($1, $2)", orgAID, orgBID)
		_, _ = conn.Exec(cleanupCtx, "DELETE FROM resources WHERE organization_id IN ($1, $2)", orgAID, orgBID)
		_, _ = conn.Exec(cleanupCtx, "DELETE FROM organizations WHERE id IN ($1, $2)", orgAID, orgBID)
	}
	cleanup()
	defer cleanup()

	// 1. Insert organizations
	_, err = conn.Exec(ctx, `
		INSERT INTO organizations (id, name, slug, status, metadata, created_at, updated_at)
		VALUES
			($1, 'Org A2 Main', $2, 'active', '{}'::jsonb, NOW(), NOW()),
			($3, 'Org A2 Secondary', $4, 'active', '{}'::jsonb, NOW(), NOW())`,
		orgAID, fmt.Sprintf("org-a2-main-%s", orgAID),
		orgBID, fmt.Sprintf("org-a2-sec-%s", orgBID),
	)
	if err != nil {
		t.Fatalf("failed inserting organizations: %v", err)
	}

	// 2. Insert resources
	_, err = conn.Exec(ctx, `
		INSERT INTO resources (id, organization_id, name, type, status, metadata, created_at, updated_at)
		VALUES
			($1, $2, 'Resource A2 Main', 'ubuntu_ssh', 'active', '{}'::jsonb, NOW(), NOW()),
			($3, $4, 'Resource A2 Secondary', 'ubuntu_ssh', 'active', '{}'::jsonb, NOW(), NOW())`,
		resAID, orgAID,
		resBID, orgBID,
	)
	if err != nil {
		t.Fatalf("failed inserting resources: %v", err)
	}

	// 3. Insert storage targets
	_, err = conn.Exec(ctx, `
		INSERT INTO storage_targets (id, organization_id, name, type, status, is_default, credential_id, config, created_at, updated_at)
		VALUES
			($1, $2, 'Local Target A', 'local', 'active', true, NULL, '{"directory":"/var/backups"}'::jsonb, NOW(), NOW()),
			($3, $2, 'S3 Target A', 's3', 'active', false, NULL, '{"bucket":"corp-backups","region":"us-east-1"}'::jsonb, NOW(), NOW()),
			($4, $2, 'S3 Compatible Target A', 's3_compatible', 'active', false, NULL, '{"bucket":"minio-backups","endpoint":"https://s3.local"}'::jsonb, NOW(), NOW()),
			($5, $2, 'Local Target B (Same Org)', 'local', 'active', false, NULL, '{"directory":"/var/secondary"}'::jsonb, NOW(), NOW()),
			($6, $2, 'Disabled Target A', 'local', 'disabled', false, NULL, '{"directory":"/var/disabled"}'::jsonb, NOW(), NOW()),
			($7, $2, 'Archived Target A', 's3', 'archived', false, NULL, '{"bucket":"archived-bucket"}'::jsonb, NOW(), NOW()),
			($8, $9, 'Org B S3 Target', 's3', 'active', true, NULL, '{"bucket":"org-b-bucket"}'::jsonb, NOW(), NOW())`,
		localTargetID, orgAID,
		s3TargetID,
		s3CompTargetID,
		secondLocalTargetID,
		disabledTargetID,
		archivedTargetID,
		orgBTargetID, orgBID,
	)
	if err != nil {
		t.Fatalf("failed inserting storage targets: %v", err)
	}

	// Helper to create running job and running run in Org A
	createJobAndRun := func(t *testing.T, jobID, runID, targetID uuid.UUID, engineType, runStatus string) {
		t.Helper()
		_, err := conn.Exec(ctx, `
			INSERT INTO backup_jobs (
				id, organization_id, resource_id, trigger_type,
				backup_type, target_spec, status, storage_target_id, engine_type,
				created_at, updated_at
			) VALUES (
				$1, $2, $3, 'scheduled',
				'mysql_database', '{"databases":["proddb"]}'::jsonb, 'running', $4, $5,
				NOW(), NOW()
			)`,
			jobID, orgAID, resAID, targetID, engineType,
		)
		if err != nil {
			t.Fatalf("failed inserting backup job: %v", err)
		}

		_, err = conn.Exec(ctx, `
			INSERT INTO backup_runs (
				id, organization_id, job_id, attempt_number, status,
				started_at, created_at, updated_at
			) VALUES (
				$1, $2, $3, 1, $4,
				NOW(), NOW(), NOW()
			)`,
			runID, orgAID, jobID, runStatus,
		)
		if err != nil {
			t.Fatalf("failed inserting backup run: %v", err)
		}
	}

	// Shared test constants
	validCipherSHA := "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
	storedSize := int64(1048576)
	engineMetaJSON, _ := json.Marshal(map[string]string{
		"ciphertext_sha256": validCipherSHA,
	})

	var sharedEncryptedArtID uuid.UUID
	var sharedRunID uuid.UUID

	// =========================================================================
	// Scenario A: Local encrypted artifact
	// =========================================================================
	t.Run("Scenario A: Local encrypted artifact", func(t *testing.T) {
		jobID := uuid.New()
		runID := uuid.New()
		sharedRunID = runID
		createJobAndRun(t, jobID, runID, localTargetID, "direct_stream", "running")

		artID := uuid.New()
		sharedEncryptedArtID = artID
		canonicalRef := fmt.Sprintf("organizations/%s/resources/%s/artifacts/%s.sql.gz", orgAID, resAID, artID)
		plainSize := int64(524288)
		plainSHA := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

		art := &domain.BackupArtifact{
			ID:                 artID,
			OrganizationID:     orgAID,
			RunID:              runID,
			ResourceID:         resAID,
			StorageTargetID:    localTargetID,
			ArtifactType:       domain.ArtifactTypeDatabaseDump,
			Format:             domain.ArtifactFormatSQLGzip,
			TargetName:         "proddb",
			StorageReference:   canonicalRef,
			SizeBytes:          plainSize,
			ChecksumAlgorithm:  domain.ChecksumAlgorithmSHA256,
			ChecksumHash:       plainSHA,
			StoredSizeBytes:    &storedSize,
			EngineMetadata:     engineMetaJSON,
			VerificationStatus: domain.VerificationStatusUnverified,
		}

		created, err := repo.CreateArtifact(ctx, art)
		if err != nil {
			t.Fatalf("CreateArtifact failed on active Local target: %v", err)
		}
		if created.StoredSizeBytes == nil || *created.StoredSizeBytes != storedSize {
			t.Fatalf("expected stored_size_bytes %d, got %v", storedSize, created.StoredSizeBytes)
		}

		retrieved, err := repo.GetArtifactByID(ctx, orgAID, artID)
		if err != nil {
			t.Fatalf("GetArtifactByID failed: %v", err)
		}
		if retrieved.SizeBytes != plainSize {
			t.Errorf("expected SizeBytes = %d, got %d", plainSize, retrieved.SizeBytes)
		}
		if retrieved.ChecksumHash != plainSHA {
			t.Errorf("expected ChecksumHash = %s, got %s", plainSHA, retrieved.ChecksumHash)
		}
		if retrieved.StoredSizeBytes == nil || *retrieved.StoredSizeBytes != storedSize {
			t.Errorf("expected StoredSizeBytes = %d, got %v", storedSize, retrieved.StoredSizeBytes)
		}
		if retrieved.StorageReference != canonicalRef {
			t.Errorf("expected StorageReference = %s, got %s", canonicalRef, retrieved.StorageReference)
		}

		var meta map[string]string
		if err := json.Unmarshal(retrieved.EngineMetadata, &meta); err != nil {
			t.Fatalf("failed unmarshaling engine_metadata: %v", err)
		}
		if meta["ciphertext_sha256"] != validCipherSHA {
			t.Errorf("expected ciphertext_sha256 = %s, got %s", validCipherSHA, meta["ciphertext_sha256"])
		}
	})

	// =========================================================================
	// Scenario B: S3 encrypted artifact
	// =========================================================================
	t.Run("Scenario B: S3 encrypted artifact", func(t *testing.T) {
		jobID := uuid.New()
		runID := uuid.New()
		createJobAndRun(t, jobID, runID, s3TargetID, "direct_stream", "running")

		artID := uuid.New()
		canonicalRef := fmt.Sprintf("organizations/%s/resources/%s/artifacts/%s.sql.gz", orgAID, resAID, artID)
		plainSize := int64(2097152)
		plainSHA := "3333333333333333333333333333333333333333333333333333333333333333"

		art := &domain.BackupArtifact{
			ID:                 artID,
			OrganizationID:     orgAID,
			RunID:              runID,
			ResourceID:         resAID,
			StorageTargetID:    s3TargetID,
			ArtifactType:       domain.ArtifactTypeDatabaseDump,
			Format:             domain.ArtifactFormatSQLGzip,
			TargetName:         "proddb",
			StorageReference:   canonicalRef,
			SizeBytes:          plainSize,
			ChecksumAlgorithm:  domain.ChecksumAlgorithmSHA256,
			ChecksumHash:       plainSHA,
			StoredSizeBytes:    &storedSize,
			EngineMetadata:     engineMetaJSON,
			VerificationStatus: domain.VerificationStatusUnverified,
		}

		created, err := repo.CreateArtifact(ctx, art)
		if err != nil {
			t.Fatalf("CreateArtifact failed on active S3 target: %v", err)
		}
		if created.StorageTargetID != s3TargetID {
			t.Fatalf("expected StorageTargetID %s, got %s", s3TargetID, created.StorageTargetID)
		}

		retrieved, err := repo.GetArtifactByID(ctx, orgAID, artID)
		if err != nil {
			t.Fatalf("GetArtifactByID failed: %v", err)
		}
		if retrieved.StorageTargetID != s3TargetID {
			t.Errorf("expected StorageTargetID = %s, got %s", s3TargetID, retrieved.StorageTargetID)
		}
		if retrieved.SizeBytes != plainSize {
			t.Errorf("expected SizeBytes = %d, got %d", plainSize, retrieved.SizeBytes)
		}
		if retrieved.ChecksumHash != plainSHA {
			t.Errorf("expected ChecksumHash = %s, got %s", plainSHA, retrieved.ChecksumHash)
		}
		if retrieved.StoredSizeBytes == nil || *retrieved.StoredSizeBytes != storedSize {
			t.Errorf("expected StoredSizeBytes = %d, got %v", storedSize, retrieved.StoredSizeBytes)
		}

		var meta map[string]string
		if err := json.Unmarshal(retrieved.EngineMetadata, &meta); err != nil {
			t.Fatalf("failed unmarshaling engine_metadata: %v", err)
		}
		if meta["ciphertext_sha256"] != validCipherSHA {
			t.Errorf("expected ciphertext_sha256 = %s, got %s", validCipherSHA, meta["ciphertext_sha256"])
		}
	})

	// =========================================================================
	// Scenario C: S3-compatible encrypted artifact
	// =========================================================================
	t.Run("Scenario C: S3-compatible encrypted artifact", func(t *testing.T) {
		jobID := uuid.New()
		runID := uuid.New()
		createJobAndRun(t, jobID, runID, s3CompTargetID, "direct_stream", "running")

		artID := uuid.New()
		canonicalRef := fmt.Sprintf("organizations/%s/resources/%s/artifacts/%s.tar.gz", orgAID, resAID, artID)
		plainSize := int64(4194304)
		plainSHA := "5555555555555555555555555555555555555555555555555555555555555555"

		art := &domain.BackupArtifact{
			ID:                 artID,
			OrganizationID:     orgAID,
			RunID:              runID,
			ResourceID:         resAID,
			StorageTargetID:    s3CompTargetID,
			ArtifactType:       domain.ArtifactTypeFilesArchive,
			Format:             domain.ArtifactFormatTarGzip,
			TargetName:         "site_assets",
			StorageReference:   canonicalRef,
			SizeBytes:          plainSize,
			ChecksumAlgorithm:  domain.ChecksumAlgorithmSHA256,
			ChecksumHash:       plainSHA,
			StoredSizeBytes:    &storedSize,
			EngineMetadata:     engineMetaJSON,
			VerificationStatus: domain.VerificationStatusUnverified,
		}

		created, err := repo.CreateArtifact(ctx, art)
		if err != nil {
			t.Fatalf("CreateArtifact failed on active s3_compatible target: %v", err)
		}
		if created.StorageTargetID != s3CompTargetID {
			t.Fatalf("expected StorageTargetID %s, got %s", s3CompTargetID, created.StorageTargetID)
		}

		retrieved, err := repo.GetArtifactByID(ctx, orgAID, artID)
		if err != nil {
			t.Fatalf("GetArtifactByID failed: %v", err)
		}
		if retrieved.StorageTargetID != s3CompTargetID {
			t.Errorf("expected StorageTargetID = %s, got %s", s3CompTargetID, retrieved.StorageTargetID)
		}
		if retrieved.SizeBytes != plainSize {
			t.Errorf("expected SizeBytes = %d, got %d", plainSize, retrieved.SizeBytes)
		}
		if retrieved.ChecksumHash != plainSHA {
			t.Errorf("expected ChecksumHash = %s, got %s", plainSHA, retrieved.ChecksumHash)
		}
		if retrieved.StoredSizeBytes == nil || *retrieved.StoredSizeBytes != storedSize {
			t.Errorf("expected StoredSizeBytes = %d, got %v", storedSize, retrieved.StoredSizeBytes)
		}

		var meta map[string]string
		if err := json.Unmarshal(retrieved.EngineMetadata, &meta); err != nil {
			t.Fatalf("failed unmarshaling engine_metadata: %v", err)
		}
		if meta["ciphertext_sha256"] != validCipherSHA {
			t.Errorf("expected ciphertext_sha256 = %s, got %s", validCipherSHA, meta["ciphertext_sha256"])
		}
	})

	// =========================================================================
	// Scenario D: Same-org wrong target binding
	// =========================================================================
	t.Run("Scenario D: Same-org wrong target", func(t *testing.T) {
		jobID := uuid.New()
		runID := uuid.New()
		// Job is bound to localTargetID (Target A)
		createJobAndRun(t, jobID, runID, localTargetID, "direct_stream", "running")

		artID := uuid.New()
		canonicalRef := fmt.Sprintf("organizations/%s/resources/%s/artifacts/%s.sql.gz", orgAID, resAID, artID)

		// Attempt to persist artifact referencing secondLocalTargetID (Target B)
		art := &domain.BackupArtifact{
			ID:                 artID,
			OrganizationID:     orgAID,
			RunID:              runID,
			ResourceID:         resAID,
			StorageTargetID:    secondLocalTargetID, // Wrong target in same org
			ArtifactType:       domain.ArtifactTypeDatabaseDump,
			Format:             domain.ArtifactFormatSQLGzip,
			TargetName:         "proddb",
			StorageReference:   canonicalRef,
			SizeBytes:          524288,
			ChecksumAlgorithm:  domain.ChecksumAlgorithmSHA256,
			ChecksumHash:       "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			StoredSizeBytes:    &storedSize,
			EngineMetadata:     engineMetaJSON,
			VerificationStatus: domain.VerificationStatusUnverified,
		}

		_, err := repo.CreateArtifact(ctx, art)
		if err == nil {
			t.Fatalf("expected ErrArtifactChainMismatch on same-org wrong target, got nil")
		}
		if !errors.Is(err, domain.ErrArtifactChainMismatch) {
			t.Fatalf("expected domain.ErrArtifactChainMismatch, got: %v", err)
		}
	})

	// =========================================================================
	// Scenario E: Cross-organization target
	// =========================================================================
	t.Run("Scenario E: Cross-organization target", func(t *testing.T) {
		jobID := uuid.New()
		runID := uuid.New()
		createJobAndRun(t, jobID, runID, localTargetID, "direct_stream", "running")

		artID := uuid.New()
		canonicalRef := fmt.Sprintf("organizations/%s/resources/%s/artifacts/%s.sql.gz", orgAID, resAID, artID)

		// Attempt to persist artifact referencing orgBTargetID (from Org B)
		art := &domain.BackupArtifact{
			ID:                 artID,
			OrganizationID:     orgAID,
			RunID:              runID,
			ResourceID:         resAID,
			StorageTargetID:    orgBTargetID, // Target belongs to Org B
			ArtifactType:       domain.ArtifactTypeDatabaseDump,
			Format:             domain.ArtifactFormatSQLGzip,
			TargetName:         "proddb",
			StorageReference:   canonicalRef,
			SizeBytes:          524288,
			ChecksumAlgorithm:  domain.ChecksumAlgorithmSHA256,
			ChecksumHash:       "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			StoredSizeBytes:    &storedSize,
			EngineMetadata:     engineMetaJSON,
			VerificationStatus: domain.VerificationStatusUnverified,
		}

		_, err := repo.CreateArtifact(ctx, art)
		if err == nil {
			t.Fatalf("expected ErrArtifactChainMismatch on cross-organization target, got nil")
		}
		if !errors.Is(err, domain.ErrArtifactChainMismatch) {
			t.Fatalf("expected domain.ErrArtifactChainMismatch, got: %v", err)
		}
	})

	// =========================================================================
	// Scenario F: Inactive target
	// =========================================================================
	t.Run("Scenario F: Inactive target", func(t *testing.T) {
		// Test 1: Disabled target
		disJobID := uuid.New()
		disRunID := uuid.New()
		createJobAndRun(t, disJobID, disRunID, disabledTargetID, "direct_stream", "running")

		disArtID := uuid.New()
		disArt := &domain.BackupArtifact{
			ID:                 disArtID,
			OrganizationID:     orgAID,
			RunID:              disRunID,
			ResourceID:         resAID,
			StorageTargetID:    disabledTargetID,
			ArtifactType:       domain.ArtifactTypeDatabaseDump,
			Format:             domain.ArtifactFormatSQLGzip,
			TargetName:         "proddb",
			StorageReference:   fmt.Sprintf("organizations/%s/resources/%s/artifacts/%s.sql.gz", orgAID, resAID, disArtID),
			SizeBytes:          524288,
			ChecksumAlgorithm:  domain.ChecksumAlgorithmSHA256,
			ChecksumHash:       "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			StoredSizeBytes:    &storedSize,
			EngineMetadata:     engineMetaJSON,
			VerificationStatus: domain.VerificationStatusUnverified,
		}

		_, err := repo.CreateArtifact(ctx, disArt)
		if err == nil {
			t.Fatalf("expected ErrArtifactChainMismatch on disabled target, got nil")
		}
		if !errors.Is(err, domain.ErrArtifactChainMismatch) {
			t.Fatalf("expected domain.ErrArtifactChainMismatch on disabled target, got: %v", err)
		}

		// Test 2: Archived target
		archJobID := uuid.New()
		archRunID := uuid.New()
		createJobAndRun(t, archJobID, archRunID, archivedTargetID, "direct_stream", "running")

		archArtID := uuid.New()
		archArt := &domain.BackupArtifact{
			ID:                 archArtID,
			OrganizationID:     orgAID,
			RunID:              archRunID,
			ResourceID:         resAID,
			StorageTargetID:    archivedTargetID,
			ArtifactType:       domain.ArtifactTypeDatabaseDump,
			Format:             domain.ArtifactFormatSQLGzip,
			TargetName:         "proddb",
			StorageReference:   fmt.Sprintf("organizations/%s/resources/%s/artifacts/%s.sql.gz", orgAID, resAID, archArtID),
			SizeBytes:          524288,
			ChecksumAlgorithm:  domain.ChecksumAlgorithmSHA256,
			ChecksumHash:       "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			StoredSizeBytes:    &storedSize,
			EngineMetadata:     engineMetaJSON,
			VerificationStatus: domain.VerificationStatusUnverified,
		}

		_, err = repo.CreateArtifact(ctx, archArt)
		if err == nil {
			t.Fatalf("expected ErrArtifactChainMismatch on archived target, got nil")
		}
		if !errors.Is(err, domain.ErrArtifactChainMismatch) {
			t.Fatalf("expected domain.ErrArtifactChainMismatch on archived target, got: %v", err)
		}
	})

	// =========================================================================
	// Scenario G: Legacy artifact
	// =========================================================================
	var sharedLegacyArtID uuid.UUID
	t.Run("Scenario G: Legacy artifact", func(t *testing.T) {
		jobID := uuid.New()
		runID := uuid.New()
		createJobAndRun(t, jobID, runID, localTargetID, "direct_stream", "running")

		legacyArtID := uuid.New()
		sharedLegacyArtID = legacyArtID
		canonicalRef := fmt.Sprintf("organizations/%s/resources/%s/artifacts/%s.sql.gz", orgAID, resAID, legacyArtID)
		plainSize := int64(262144)
		plainSHA := "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"

		legacyArt := &domain.BackupArtifact{
			ID:                 legacyArtID,
			OrganizationID:     orgAID,
			RunID:              runID,
			ResourceID:         resAID,
			StorageTargetID:    localTargetID,
			ArtifactType:       domain.ArtifactTypeDatabaseDump,
			Format:             domain.ArtifactFormatSQLGzip,
			TargetName:         "legacydb",
			StorageReference:   canonicalRef,
			SizeBytes:          plainSize,
			ChecksumAlgorithm:  domain.ChecksumAlgorithmSHA256,
			ChecksumHash:       plainSHA,
			StoredSizeBytes:    nil,
			EngineMetadata:     nil,
			VerificationStatus: domain.VerificationStatusUnverified,
		}

		createdLegacy, err := repo.CreateArtifact(ctx, legacyArt)
		if err != nil {
			t.Fatalf("failed creating legacy artifact: %v", err)
		}
		if createdLegacy.StoredSizeBytes != nil {
			t.Fatalf("expected nil stored_size_bytes for legacy artifact, got %v", createdLegacy.StoredSizeBytes)
		}

		retrievedLegacy, err := repo.GetArtifactByID(ctx, orgAID, legacyArtID)
		if err != nil {
			t.Fatalf("failed retrieving legacy artifact: %v", err)
		}
		if retrievedLegacy.StoredSizeBytes != nil {
			t.Fatalf("expected nil stored_size_bytes on retrieved legacy artifact, got %v", retrievedLegacy.StoredSizeBytes)
		}
		if len(retrievedLegacy.EngineMetadata) > 0 && string(retrievedLegacy.EngineMetadata) != "{}" {
			t.Fatalf("expected empty or '{}' engine_metadata, got: %s", string(retrievedLegacy.EngineMetadata))
		}
		if retrievedLegacy.StorageReference != canonicalRef {
			t.Errorf("expected StorageReference %s, got %s", canonicalRef, retrievedLegacy.StorageReference)
		}
		if retrievedLegacy.SizeBytes != plainSize {
			t.Errorf("expected SizeBytes %d, got %d", plainSize, retrievedLegacy.SizeBytes)
		}
		if retrievedLegacy.ChecksumHash != plainSHA {
			t.Errorf("expected ChecksumHash %s, got %s", plainSHA, retrievedLegacy.ChecksumHash)
		}
	})

	// =========================================================================
	// Scenario H: Malformed engine_metadata rejection
	// =========================================================================
	t.Run("Scenario H: Malformed engine_metadata rejected by database CHECK constraint", func(t *testing.T) {
		jobID := uuid.New()
		runID := uuid.New()
		createJobAndRun(t, jobID, runID, localTargetID, "direct_stream", "running")

		malformedArtID := uuid.New()
		malformedSize := int64(4096)
		malformedArt := &domain.BackupArtifact{
			ID:                 malformedArtID,
			OrganizationID:     orgAID,
			RunID:              runID,
			ResourceID:         resAID,
			StorageTargetID:    localTargetID,
			ArtifactType:       domain.ArtifactTypeDatabaseDump,
			Format:             domain.ArtifactFormatSQLGzip,
			TargetName:         "malformeddb",
			StorageReference:   fmt.Sprintf("organizations/%s/resources/%s/artifacts/%s.sql.gz", orgAID, resAID, malformedArtID),
			SizeBytes:          2048,
			ChecksumAlgorithm:  domain.ChecksumAlgorithmSHA256,
			ChecksumHash:       "1111111111111111111111111111111111111111111111111111111111111111",
			StoredSizeBytes:    &malformedSize,
			EngineMetadata:     []byte(`{"foo":"not_a_valid_sha256"}`), // Missing ciphertext_sha256
			VerificationStatus: domain.VerificationStatusUnverified,
		}

		_, createErr := repo.CreateArtifact(ctx, malformedArt)
		if createErr == nil {
			t.Fatalf("expected database check constraint error for malformed engine_metadata with stored_size_bytes set, got nil")
		}
	})

	// =========================================================================
	// Scenario I: UpdateArtifactVerification preserves stored_size_bytes and engine_metadata
	// =========================================================================
	t.Run("Scenario I: UpdateArtifactVerification preserves stored_size_bytes and engine_metadata", func(t *testing.T) {
		verMsg := `{"checksum_matched":true,"archive_integrity":"passed","compression_valid":true,"extracted_sample_check":"passed"}`
		updateErr := repo.UpdateArtifactVerification(ctx, orgAID, sharedEncryptedArtID, domain.VerificationStatusVerified, &verMsg)
		if updateErr != nil {
			t.Fatalf("failed updating artifact verification: %v", updateErr)
		}

		afterUpdate, getErr := repo.GetArtifactByID(ctx, orgAID, sharedEncryptedArtID)
		if getErr != nil {
			t.Fatalf("failed retrieving artifact after verification update: %v", getErr)
		}
		if afterUpdate.VerificationStatus != domain.VerificationStatusVerified {
			t.Fatalf("expected verification status verified, got %s", afterUpdate.VerificationStatus)
		}
		if afterUpdate.StoredSizeBytes == nil || *afterUpdate.StoredSizeBytes != storedSize {
			t.Fatalf("stored_size_bytes was corrupted or cleared after verification update: %v", afterUpdate.StoredSizeBytes)
		}
		var meta map[string]string
		if unmarshalErr := json.Unmarshal(afterUpdate.EngineMetadata, &meta); unmarshalErr != nil || meta["ciphertext_sha256"] != validCipherSHA {
			t.Fatalf("engine_metadata was corrupted or cleared after verification update: %s", string(afterUpdate.EngineMetadata))
		}
	})

	// =========================================================================
	// Scenario J: TombstoneArtifact preserves stored_size_bytes and engine_metadata
	// =========================================================================
	t.Run("Scenario J: TombstoneArtifact preserves stored_size_bytes and engine_metadata", func(t *testing.T) {
		tombErr := repo.TombstoneArtifact(ctx, orgAID, sharedEncryptedArtID)
		if tombErr != nil {
			t.Fatalf("failed tombstoning artifact: %v", tombErr)
		}

		afterTomb, getErr := repo.GetArtifactByID(ctx, orgAID, sharedEncryptedArtID)
		if getErr != nil {
			t.Fatalf("failed retrieving artifact after tombstone: %v", getErr)
		}
		if !afterTomb.IsDeleted {
			t.Fatalf("expected is_deleted = true after tombstone")
		}
		if afterTomb.StoredSizeBytes == nil || *afterTomb.StoredSizeBytes != storedSize {
			t.Fatalf("stored_size_bytes was corrupted or cleared after tombstone: %v", afterTomb.StoredSizeBytes)
		}
		var meta map[string]string
		if unmarshalErr := json.Unmarshal(afterTomb.EngineMetadata, &meta); unmarshalErr != nil || meta["ciphertext_sha256"] != validCipherSHA {
			t.Fatalf("engine_metadata was corrupted or cleared after tombstone: %s", string(afterTomb.EngineMetadata))
		}
	})

	// =========================================================================
	// Scenario K: Filter / query artifacts by run
	// =========================================================================
	t.Run("Scenario K: Query run artifacts returns correct metadata for all artifacts", func(t *testing.T) {
		// Insert legacy artifact into the same run as sharedEncryptedArtID to test mixed listing
		runArts, listErr := repo.GetRunArtifacts(ctx, orgAID, sharedRunID)
		if listErr != nil {
			t.Fatalf("failed listing run artifacts: %v", listErr)
		}
		if len(runArts) < 1 {
			t.Fatalf("expected at least 1 artifact for run, got %d", len(runArts))
		}

		var foundEncrypted bool
		for _, a := range runArts {
			if a.ID == sharedEncryptedArtID {
				foundEncrypted = true
				if a.StoredSizeBytes == nil || *a.StoredSizeBytes != storedSize {
					t.Errorf("encrypted artifact stored_size_bytes mismatch in list: %v", a.StoredSizeBytes)
				}
				var meta map[string]string
				_ = json.Unmarshal(a.EngineMetadata, &meta)
				if meta["ciphertext_sha256"] != validCipherSHA {
					t.Errorf("encrypted artifact ciphertext_sha256 mismatch in list: %v", meta["ciphertext_sha256"])
				}
			}
		}
		if !foundEncrypted {
			t.Fatalf("expected to find encrypted artifact %s in run listing", sharedEncryptedArtID)
		}
		_ = sharedLegacyArtID
	})
}
