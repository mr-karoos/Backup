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

	orgID := uuid.New()
	resID := uuid.New()
	targetID := uuid.New()
	planID := uuid.New()
	jobID := uuid.New()
	runID := uuid.New()

	cleanup := func() {
		cleanupCtx := context.Background()
		_, _ = conn.Exec(cleanupCtx, "DELETE FROM backup_artifacts WHERE organization_id = $1", orgID)
		_, _ = conn.Exec(cleanupCtx, "DELETE FROM backup_runs WHERE organization_id = $1", orgID)
		_, _ = conn.Exec(cleanupCtx, "DELETE FROM backup_jobs WHERE organization_id = $1", orgID)
		_, _ = conn.Exec(cleanupCtx, "DELETE FROM backup_plans WHERE organization_id = $1", orgID)
		_, _ = conn.Exec(cleanupCtx, "DELETE FROM storage_targets WHERE organization_id = $1", orgID)
		_, _ = conn.Exec(cleanupCtx, "DELETE FROM resources WHERE organization_id = $1", orgID)
		_, _ = conn.Exec(cleanupCtx, "DELETE FROM organizations WHERE id = $1", orgID)
	}
	cleanup()
	defer cleanup()

	// 1. Insert organization
	slug := fmt.Sprintf("org-a2-%s", orgID)
	_, err = conn.Exec(ctx, `
		INSERT INTO organizations (id, name, slug, status, metadata, created_at, updated_at)
		VALUES ($1, 'Org A2', $2, 'active', '{}'::jsonb, NOW(), NOW())`,
		orgID, slug,
	)
	if err != nil {
		t.Fatalf("failed inserting org: %v", err)
	}

	// 2. Insert resource
	_, err = conn.Exec(ctx, `
		INSERT INTO resources (id, organization_id, name, type, status, metadata, created_at, updated_at)
		VALUES ($1, $2, 'Resource A2', 'ubuntu_ssh', 'active', '{}'::jsonb, NOW(), NOW())`,
		resID, orgID,
	)
	if err != nil {
		t.Fatalf("failed inserting resource: %v", err)
	}

	// 3. Insert storage target
	_, err = conn.Exec(ctx, `
		INSERT INTO storage_targets (
			id, organization_id, name, type, config,
			is_default, status, created_at, updated_at
		) VALUES (
			$1, $2, 'Default Local A2', 'local',
			'{"directory":"/var/backups"}'::jsonb,
			true, 'active', NOW(), NOW()
		)`,
		targetID, orgID,
	)
	if err != nil {
		t.Fatalf("failed inserting storage target: %v", err)
	}

	// 4. Insert backup plan
	_, err = conn.Exec(ctx, `
		INSERT INTO backup_plans (
			id, organization_id, resource_id, name, backup_type,
			schedule_cron, schedule_timezone, is_schedule_enabled,
			retention_days, status, target_spec, storage_target_id, engine_type,
			created_at, updated_at
		) VALUES (
			$1, $2, $3, 'Plan A2', 'mysql_database',
			'0 0 * * *', 'UTC', true,
			30, 'active', '{"databases":["testdb"]}'::jsonb, $4, 'direct_stream',
			NOW(), NOW()
		)`,
		planID, orgID, resID, targetID,
	)
	if err != nil {
		t.Fatalf("failed inserting backup plan: %v", err)
	}

	// 5. Insert backup job
	_, err = conn.Exec(ctx, `
		INSERT INTO backup_jobs (
			id, organization_id, resource_id, backup_plan_id, trigger_type,
			backup_type, target_spec, status, storage_target_id, engine_type,
			created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, 'manual',
			'mysql_database', '{"databases":["testdb"]}'::jsonb, 'running', $5, 'direct_stream',
			NOW(), NOW()
		)`,
		jobID, orgID, resID, planID, targetID,
	)
	if err != nil {
		t.Fatalf("failed inserting backup job: %v", err)
	}

	// 6. Insert backup run
	_, err = conn.Exec(ctx, `
		INSERT INTO backup_runs (
			id, organization_id, job_id, attempt_number, status,
			started_at, created_at, updated_at
		) VALUES (
			$1, $2, $3, 1, 'running',
			NOW(), NOW(), NOW()
		)`,
		runID, orgID, jobID,
	)
	if err != nil {
		t.Fatalf("failed inserting backup run: %v", err)
	}

	validCiphertextSHA256 := "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
	storedSize := int64(1048576)
	engineMetaJSON, _ := json.Marshal(map[string]string{
		"ciphertext_sha256": validCiphertextSHA256,
	})

	encryptedArtID := uuid.New()
	encryptedArt := &domain.BackupArtifact{
		ID:                 encryptedArtID,
		OrganizationID:     orgID,
		RunID:              runID,
		ResourceID:         resID,
		StorageTargetID:    targetID,
		ArtifactType:       domain.ArtifactTypeDatabaseDump,
		Format:             domain.ArtifactFormatSQLGzip,
		TargetName:         "testdb",
		StorageReference:   fmt.Sprintf("organizations/%s/artifacts/%s.sql.gz", orgID, encryptedArtID),
		SizeBytes:          524288,
		ChecksumAlgorithm:  domain.ChecksumAlgorithmSHA256,
		ChecksumHash:       "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		StoredSizeBytes:    &storedSize,
		EngineMetadata:     engineMetaJSON,
		VerificationStatus: domain.VerificationStatusUnverified,
	}

	// Scenario A: Direct insert with valid BPAE engine_metadata and stored_size_bytes
	t.Run("Scenario A: Direct insert with valid BPAE engine_metadata and stored_size_bytes", func(t *testing.T) {
		created, createErr := repo.CreateArtifact(ctx, encryptedArt)
		if createErr != nil {
			t.Fatalf("failed creating encrypted artifact: %v", createErr)
		}
		if created.StoredSizeBytes == nil || *created.StoredSizeBytes != storedSize {
			t.Fatalf("expected stored_size_bytes %d, got %v", storedSize, created.StoredSizeBytes)
		}
		if len(created.EngineMetadata) == 0 {
			t.Fatalf("expected non-empty engine_metadata")
		}
	})

	// Scenario B: Retrieve artifact by ID
	t.Run("Scenario B: Retrieve artifact by ID verifies engine_metadata and stored_size_bytes", func(t *testing.T) {
		retrieved, getErr := repo.GetArtifactByID(ctx, orgID, encryptedArtID)
		if getErr != nil {
			t.Fatalf("failed retrieving artifact by ID: %v", getErr)
		}
		if retrieved.StoredSizeBytes == nil || *retrieved.StoredSizeBytes != storedSize {
			t.Fatalf("stored_size_bytes mismatch: expected %d, got %v", storedSize, retrieved.StoredSizeBytes)
		}
		var meta map[string]string
		if unmarshalErr := json.Unmarshal(retrieved.EngineMetadata, &meta); unmarshalErr != nil {
			t.Fatalf("failed deserializing engine_metadata: %v", unmarshalErr)
		}
		if meta["ciphertext_sha256"] != validCiphertextSHA256 {
			t.Fatalf("ciphertext_sha256 mismatch: expected %s, got %s", validCiphertextSHA256, meta["ciphertext_sha256"])
		}
	})

	// Scenario C: Legacy artifact with stored_size_bytes = NULL and engine_metadata = NULL (or default)
	legacyArtID := uuid.New()
	t.Run("Scenario C: Legacy artifact backwards compatibility", func(t *testing.T) {
		legacyArt := &domain.BackupArtifact{
			ID:                 legacyArtID,
			OrganizationID:     orgID,
			RunID:              runID,
			ResourceID:         resID,
			StorageTargetID:    targetID,
			ArtifactType:       domain.ArtifactTypeDatabaseDump,
			Format:             domain.ArtifactFormatSQLGzip,
			TargetName:         "legacydb",
			StorageReference:   fmt.Sprintf("organizations/%s/artifacts/%s.sql.gz", orgID, legacyArtID),
			SizeBytes:          262144,
			ChecksumAlgorithm:  domain.ChecksumAlgorithmSHA256,
			ChecksumHash:       "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
			StoredSizeBytes:    nil,
			EngineMetadata:     nil,
			VerificationStatus: domain.VerificationStatusUnverified,
		}
		createdLegacy, createErr := repo.CreateArtifact(ctx, legacyArt)
		if createErr != nil {
			t.Fatalf("failed creating legacy artifact: %v", createErr)
		}
		if createdLegacy.StoredSizeBytes != nil {
			t.Fatalf("expected nil stored_size_bytes for legacy artifact, got %v", createdLegacy.StoredSizeBytes)
		}

		retrievedLegacy, getErr := repo.GetArtifactByID(ctx, orgID, legacyArtID)
		if getErr != nil {
			t.Fatalf("failed retrieving legacy artifact: %v", getErr)
		}
		if retrievedLegacy.StoredSizeBytes != nil {
			t.Fatalf("expected nil stored_size_bytes on retrieved legacy artifact, got %v", retrievedLegacy.StoredSizeBytes)
		}
	})

	// Scenario D: Malformed engine_metadata rejection if enforced or safe deserialization
	t.Run("Scenario D: Malformed engine_metadata rejected by database CHECK constraint", func(t *testing.T) {
		malformedArtID := uuid.New()
		malformedSize := int64(4096)
		malformedArt := &domain.BackupArtifact{
			ID:                 malformedArtID,
			OrganizationID:     orgID,
			RunID:              runID,
			ResourceID:         resID,
			StorageTargetID:    targetID,
			ArtifactType:       domain.ArtifactTypeDatabaseDump,
			Format:             domain.ArtifactFormatSQLGzip,
			TargetName:         "malformeddb",
			StorageReference:   fmt.Sprintf("organizations/%s/artifacts/%s.sql.gz", orgID, malformedArtID),
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

	// Scenario E: Filter / query artifacts by run
	t.Run("Scenario E: Query run artifacts returns correct metadata for all artifacts", func(t *testing.T) {
		runArts, listErr := repo.GetRunArtifacts(ctx, orgID, runID)
		if listErr != nil {
			t.Fatalf("failed listing run artifacts: %v", listErr)
		}
		if len(runArts) < 2 {
			t.Fatalf("expected at least 2 artifacts for run, got %d", len(runArts))
		}

		var foundEncrypted, foundLegacy bool
		for _, a := range runArts {
			if a.ID == encryptedArtID {
				foundEncrypted = true
				if a.StoredSizeBytes == nil || *a.StoredSizeBytes != storedSize {
					t.Errorf("encrypted artifact stored_size_bytes mismatch in list: %v", a.StoredSizeBytes)
				}
				var meta map[string]string
				_ = json.Unmarshal(a.EngineMetadata, &meta)
				if meta["ciphertext_sha256"] != validCiphertextSHA256 {
					t.Errorf("encrypted artifact ciphertext_sha256 mismatch in list: %v", meta["ciphertext_sha256"])
				}
			}
			if a.ID == legacyArtID {
				foundLegacy = true
				if a.StoredSizeBytes != nil {
					t.Errorf("legacy artifact stored_size_bytes must be nil in list, got %v", a.StoredSizeBytes)
				}
			}
		}
		if !foundEncrypted || !foundLegacy {
			t.Fatalf("expected to find both encrypted (%v) and legacy (%v) artifacts in run", foundEncrypted, foundLegacy)
		}
	})

	// Scenario F: Verification status updates preserve stored_size_bytes and engine_metadata
	t.Run("Scenario F: UpdateArtifactVerification preserves stored_size_bytes and engine_metadata", func(t *testing.T) {
		verMsg := `{"checksum_matched":true,"archive_integrity":"passed","compression_valid":true,"extracted_sample_check":"passed"}`
		updateErr := repo.UpdateArtifactVerification(ctx, orgID, encryptedArtID, domain.VerificationStatusVerified, &verMsg)
		if updateErr != nil {
			t.Fatalf("failed updating artifact verification: %v", updateErr)
		}

		afterUpdate, getErr := repo.GetArtifactByID(ctx, orgID, encryptedArtID)
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
		if unmarshalErr := json.Unmarshal(afterUpdate.EngineMetadata, &meta); unmarshalErr != nil || meta["ciphertext_sha256"] != validCiphertextSHA256 {
			t.Fatalf("engine_metadata was corrupted or cleared after verification update: %s", string(afterUpdate.EngineMetadata))
		}
	})

	// Scenario G: Soft delete / tombstone preserves stored_size_bytes and engine_metadata
	t.Run("Scenario G: TombstoneArtifact preserves stored_size_bytes and engine_metadata", func(t *testing.T) {
		tombErr := repo.TombstoneArtifact(ctx, orgID, encryptedArtID)
		if tombErr != nil {
			t.Fatalf("failed tombstoning artifact: %v", tombErr)
		}

		afterTomb, getErr := repo.GetArtifactByID(ctx, orgID, encryptedArtID)
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
		if unmarshalErr := json.Unmarshal(afterTomb.EngineMetadata, &meta); unmarshalErr != nil || meta["ciphertext_sha256"] != validCiphertextSHA256 {
			t.Fatalf("engine_metadata was corrupted or cleared after tombstone: %s", string(afterTomb.EngineMetadata))
		}
	})
}
