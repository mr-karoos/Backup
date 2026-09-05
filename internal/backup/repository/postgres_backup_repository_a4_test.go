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

func TestPostgresBackupRepository_StepA4_Integration(t *testing.T) {
	testDBURL := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	if testDBURL == "" {
		t.Skip("skipping Step A.4 repository integration test: TEST_DATABASE_URL not set")
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

	// Migrate through version 9 (Step A.4 polymorphic artifacts)
	if err := m.Migrate(9); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		t.Fatalf("failed migrating to version 9: %v", err)
	}

	pool, err := database.New(ctx, testDBURL)
	if err != nil {
		t.Fatalf("failed initializing database pool: %v", err)
	}
	defer pool.Close()

	repo := NewPostgresBackupRepository(pool)

	orgA := uuid.New()
	orgB := uuid.New()
	resA := uuid.New()
	resB := uuid.New()
	targetA := uuid.New()
	targetB := uuid.New()
	credA := uuid.New()
	credB := uuid.New()
	resticRepoA := uuid.New()
	resticRepoB := uuid.New()
	jobA := uuid.New()
	runA := uuid.New()

	cleanup := func() {
		cleanupCtx := context.Background()
		_, _ = pool.Querier().Exec(cleanupCtx, "DELETE FROM backup_artifacts WHERE organization_id IN ($1, $2)", orgA, orgB)
		_, _ = pool.Querier().Exec(cleanupCtx, "DELETE FROM backup_runs WHERE organization_id IN ($1, $2)", orgA, orgB)
		_, _ = pool.Querier().Exec(cleanupCtx, "DELETE FROM backup_jobs WHERE organization_id IN ($1, $2)", orgA, orgB)
		_, _ = pool.Querier().Exec(cleanupCtx, "DELETE FROM backup_repositories WHERE organization_id IN ($1, $2)", orgA, orgB)
		_, _ = pool.Querier().Exec(cleanupCtx, "DELETE FROM credentials WHERE organization_id IN ($1, $2)", orgA, orgB)
		_, _ = pool.Querier().Exec(cleanupCtx, "DELETE FROM storage_targets WHERE organization_id IN ($1, $2)", orgA, orgB)
		_, _ = pool.Querier().Exec(cleanupCtx, "DELETE FROM resources WHERE organization_id IN ($1, $2)", orgA, orgB)
		_, _ = pool.Querier().Exec(cleanupCtx, "DELETE FROM organizations WHERE id IN ($1, $2)", orgA, orgB)
	}
	cleanup()
	defer cleanup()

	// 1. Seed Organizations
	slugA := fmt.Sprintf("org-repo-a4-a-%s", orgA.String()[:8])
	slugB := fmt.Sprintf("org-repo-a4-b-%s", orgB.String()[:8])
	_, err = pool.Querier().Exec(ctx, `
		INSERT INTO organizations (id, name, slug, status, metadata, created_at, updated_at)
		VALUES ($1, 'Org A4 A', $2, 'active', '{}'::jsonb, NOW(), NOW()),
		       ($3, 'Org A4 B', $4, 'active', '{}'::jsonb, NOW(), NOW())`,
		orgA, slugA, orgB, slugB)
	if err != nil {
		t.Fatalf("failed seeding organizations: %v", err)
	}

	// 2. Seed Resources
	_, err = pool.Querier().Exec(ctx, `
		INSERT INTO resources (id, organization_id, name, type, status, metadata, created_at, updated_at)
		VALUES ($1, $2, 'Resource A', 'ubuntu_ssh', 'active', '{}'::jsonb, NOW(), NOW()),
		       ($3, $4, 'Resource B', 'ubuntu_ssh', 'active', '{}'::jsonb, NOW(), NOW())`,
		resA, orgA, resB, orgB)
	if err != nil {
		t.Fatalf("failed seeding resources: %v", err)
	}

	// 3. Seed Storage Targets
	_, err = pool.Querier().Exec(ctx, `
		INSERT INTO storage_targets (id, organization_id, name, type, status, is_default, config, created_at, updated_at)
		VALUES ($1, $2, 'Target A', 'local', 'active', true, '{}'::jsonb, NOW(), NOW()),
		       ($3, $4, 'Target B', 'local', 'active', true, '{}'::jsonb, NOW(), NOW())`,
		targetA, orgA, targetB, orgB)
	if err != nil {
		t.Fatalf("failed seeding storage targets: %v", err)
	}

	// 4. Seed Credentials
	_, err = pool.Querier().Exec(ctx, `
		INSERT INTO credentials (id, organization_id, name, type, managed_by, encrypted_secret, nonce, auth_tag, key_version, created_at, updated_at)
		VALUES ($1, $2, 'Cred A', 'restic_repository_key', 'system', '\x01', '\x02', '\x03', 1, NOW(), NOW()),
		       ($3, $4, 'Cred B', 'restic_repository_key', 'system', '\x01', '\x02', '\x03', 1, NOW(), NOW())`,
		credA, orgA, credB, orgB)
	if err != nil {
		t.Fatalf("failed seeding credentials: %v", err)
	}

	// 5. Seed Repositories
	_, err = pool.Querier().Exec(ctx, `
		INSERT INTO backup_repositories (id, organization_id, resource_id, storage_target_id, credential_id, repository_type, repository_locator, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, 'local', 'orgs/' || $2 || '/res/' || $3, 'ready', NOW(), NOW()),
		       ($6, $7, $8, $9, $10, 'local', 'orgs/' || $7 || '/res/' || $8, 'ready', NOW(), NOW())`,
		resticRepoA, orgA, resA, targetA, credA,
		resticRepoB, orgB, resB, targetB, credB)
	if err != nil {
		t.Fatalf("failed seeding backup_repositories: %v", err)
	}

	// 6. Seed Backup Job and Run
	_, err = pool.Querier().Exec(ctx, `
		INSERT INTO backup_jobs (id, organization_id, resource_id, backup_type, status, created_at, updated_at)
		VALUES ($1, $2, $3, 'mysql_database', 'completed', NOW(), NOW())`,
		jobA, orgA, resA)
	if err != nil {
		t.Fatalf("failed seeding backup_job: %v", err)
	}

	_, err = pool.Querier().Exec(ctx, `
		INSERT INTO backup_runs (id, organization_id, job_id, status, run_type, trigger_source, created_at, updated_at)
		VALUES ($1, $2, $3, 'completed', 'manual', 'user', NOW(), NOW())`,
		runA, orgA, jobA)
	if err != nil {
		t.Fatalf("failed seeding backup_run: %v", err)
	}

	// Test 1: Create direct_stream Artifact
	directArtID := uuid.New()
	directArt := &domain.BackupArtifact{
		ID:                 directArtID,
		OrganizationID:     orgA,
		RunID:              runA,
		ResourceID:         resA,
		StorageTargetID:    targetA,
		ArtifactType:       domain.ArtifactTypeDatabaseDump,
		Format:             domain.ArtifactFormatSQLGzip,
		TargetName:         "production_db",
		StorageReference:   "direct_dump.sql.gz",
		SizeBytes:          5242880,
		ChecksumAlgorithm:  domain.ChecksumAlgorithmSHA256,
		ChecksumHash:       "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		VerificationStatus: domain.VerificationStatusUnverified,
	}

	createdDirect, err := repo.CreateArtifact(ctx, directArt)
	if err != nil {
		t.Fatalf("failed creating direct_stream artifact: %v", err)
	}
	if createdDirect.ID != directArtID {
		t.Fatalf("expected artifact ID %s, got %s", directArtID, createdDirect.ID)
	}

	// Test 2: Create restic_snapshot Artifact
	resticArtID := uuid.New()
	snapID := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	logicalSize := int64(10485760)
	engineMeta, _ := json.Marshal(map[string]any{"snapshot_id": snapID})

	resticArt := &domain.BackupArtifact{
		ID:                 resticArtID,
		OrganizationID:     orgA,
		RunID:              runA,
		ResourceID:         resA,
		StorageTargetID:    targetA,
		ArtifactType:       domain.ArtifactTypeDatabaseDump,
		Format:             domain.ArtifactFormatResticSnapshot,
		TargetName:         "production_db",
		StorageReference:   "",
		SizeBytes:          0,
		ChecksumAlgorithm:  domain.ChecksumAlgorithmSHA256,
		ChecksumHash:       "",
		RepositoryID:       &resticRepoA,
		SnapshotID:         snapID,
		LogicalSizeBytes:   &logicalSize,
		EngineMetadata:     engineMeta,
		VerificationStatus: domain.VerificationStatusUnverified,
	}

	createdRestic, err := repo.CreateArtifact(ctx, resticArt)
	if err != nil {
		t.Fatalf("failed creating restic_snapshot artifact: %v", err)
	}
	if createdRestic.SnapshotID != snapID {
		t.Fatalf("expected snapshot ID %s, got %s", snapID, createdRestic.SnapshotID)
	}

	// Test 3: Tenant Isolation on Creation: attempting to reference another org's repository fails
	crossTenantArt := &domain.BackupArtifact{
		ID:                 uuid.New(),
		OrganizationID:     orgA,
		RunID:              runA,
		ResourceID:         resA,
		StorageTargetID:    targetA,
		ArtifactType:       domain.ArtifactTypeDatabaseDump,
		Format:             domain.ArtifactFormatResticSnapshot,
		TargetName:         "production_db",
		StorageReference:   "",
		SizeBytes:          0,
		ChecksumAlgorithm:  domain.ChecksumAlgorithmSHA256,
		ChecksumHash:       "",
		RepositoryID:       &resticRepoB, // Belongs to Org B!
		SnapshotID:         "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		LogicalSizeBytes:   &logicalSize,
		VerificationStatus: domain.VerificationStatusUnverified,
	}
	_, err = repo.CreateArtifact(ctx, crossTenantArt)
	if err == nil {
		t.Fatalf("SECURITY VIOLATION: successfully created artifact pointing to another tenant's repository!")
	}

	// Test 4: GetArtifactByID retrieves polymorphic fields
	retrievedDirect, err := repo.GetArtifactByID(ctx, orgA, directArtID)
	if err != nil {
		t.Fatalf("failed retrieving direct_stream artifact: %v", err)
	}
	if retrievedDirect.Format != domain.ArtifactFormatSQLGzip || retrievedDirect.RepositoryID != nil {
		t.Fatalf("unexpected fields for direct_stream artifact: %+v", retrievedDirect)
	}

	retrievedRestic, err := repo.GetArtifactByID(ctx, orgA, resticArtID)
	if err != nil {
		t.Fatalf("failed retrieving restic_snapshot artifact: %v", err)
	}
	if retrievedRestic.Format != domain.ArtifactFormatResticSnapshot || retrievedRestic.SnapshotID != snapID || *retrievedRestic.LogicalSizeBytes != logicalSize {
		t.Fatalf("unexpected fields for restic_snapshot artifact: %+v", retrievedRestic)
	}

	// Test 5: Tenant Isolation on Retrieval: Org B cannot retrieve Org A's artifact
	_, err = repo.GetArtifactByID(ctx, orgB, resticArtID)
	if !errors.Is(err, domain.ErrArtifactNotFound) {
		t.Fatalf("expected ErrArtifactNotFound when querying cross-tenant artifact, got: %v", err)
	}

	// Test 6: GetRunArtifacts returns both polymorphic artifacts
	arts, err := repo.GetRunArtifacts(ctx, orgA, runA)
	if err != nil {
		t.Fatalf("failed listing artifacts by run: %v", err)
	}
	if len(arts) != 2 {
		t.Fatalf("expected 2 artifacts, got: %d", len(arts))
	}

	// Test 7: ListArtifacts lists all organization artifacts
	allArts, err := repo.ListArtifacts(ctx, orgA)
	if err != nil {
		t.Fatalf("failed listing all artifacts: %v", err)
	}
	if len(allArts) != 2 {
		t.Fatalf("expected 2 artifacts, got: %d", len(allArts))
	}

	// Test 8: UpdateArtifactVerification works for restic artifacts
	verMsg := "verified: level-1 checks pass"
	err = repo.UpdateArtifactVerification(ctx, orgA, resticArtID, domain.VerificationStatusVerified, &verMsg)
	if err != nil {
		t.Fatalf("failed updating artifact verification: %v", err)
	}

	retrievedUpdated, err := repo.GetArtifactByID(ctx, orgA, resticArtID)
	if err != nil {
		t.Fatalf("failed querying updated artifact: %v", err)
	}
	if retrievedUpdated.VerificationStatus != domain.VerificationStatusVerified {
		t.Fatalf("expected status verified, got: %s", retrievedUpdated.VerificationStatus)
	}
}
