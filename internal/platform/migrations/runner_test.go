package migrations

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5"

	"backup-platform/pkg/uuid"
)

func TestMigrations_EmbeddedFilesExistAndValid(t *testing.T) {
	// Verify directory can be read
	entries, err := fs.ReadDir(FS, "sql")
	if err != nil {
		t.Fatalf("failed to read embedded sql directory: %v", err)
	}

	if len(entries) < 10 {
		t.Fatalf("expected at least 10 migration files (.up.sql and .down.sql for v1, v2, v3, v4 & v5), found: %d", len(entries))
	}

	expectedFiles := map[string]bool{
		"000001_identity_foundation.up.sql":               false,
		"000001_identity_foundation.down.sql":             false,
		"000002_resource_credential_foundation.up.sql":    false,
		"000002_resource_credential_foundation.down.sql":  false,
		"000003_backup_execution_foundation.up.sql":       false,
		"000003_backup_execution_foundation.down.sql":     false,
		"000004_backup_plan_scheduler_integrity.up.sql":   false,
		"000004_backup_plan_scheduler_integrity.down.sql": false,
		"000005_artifact_lifecycle_audit.up.sql":          false,
		"000005_artifact_lifecycle_audit.down.sql":        false,
	}

	for _, entry := range entries {
		if _, ok := expectedFiles[entry.Name()]; ok {
			expectedFiles[entry.Name()] = true
		}
	}

	for filename, found := range expectedFiles {
		if !found {
			t.Errorf("expected migration file %s to be present in embedded FS", filename)
		}
	}
}

func TestMigrations_UpScriptContainsCanonicalTables(t *testing.T) {
	upContent, err := fs.ReadFile(FS, "sql/000001_identity_foundation.up.sql")
	if err != nil {
		t.Fatalf("failed to read up migration file: %v", err)
	}

	sqlStr := string(upContent)
	requiredTables := []string{
		"CREATE TABLE IF NOT EXISTS organizations",
		"CREATE TABLE IF NOT EXISTS users",
		"CREATE TABLE IF NOT EXISTS user_sessions",
		"CREATE TABLE IF NOT EXISTS organization_members",
	}

	for _, tbl := range requiredTables {
		if !strings.Contains(sqlStr, tbl) {
			t.Errorf("up migration is missing required table definition: '%s'", tbl)
		}
	}

	// Verify key constraints
	requiredConstraints := []string{
		"is_system_admin BOOLEAN NOT NULL DEFAULT false",
		"refresh_token_hash VARCHAR(64) NOT NULL UNIQUE",
		"role VARCHAR(50) NOT NULL CHECK (role IN ('admin', 'member', 'viewer'))",
		"is_default_internal BOOLEAN NOT NULL DEFAULT false",
		"REFERENCES users(id) ON DELETE CASCADE",
		"REFERENCES organizations(id) ON DELETE CASCADE",
	}

	for _, c := range requiredConstraints {
		if !strings.Contains(sqlStr, c) {
			t.Errorf("up migration is missing required constraint: '%s'", c)
		}
	}
}

func TestMigrations_DownScriptContainsReversal(t *testing.T) {
	downContent, err := fs.ReadFile(FS, "sql/000001_identity_foundation.down.sql")
	if err != nil {
		t.Fatalf("failed to read down migration file: %v", err)
	}

	sqlStr := string(downContent)
	requiredDrops := []string{
		"DROP TABLE IF EXISTS organization_members",
		"DROP TABLE IF EXISTS user_sessions",
		"DROP TABLE IF EXISTS users",
		"DROP TABLE IF EXISTS organizations",
	}

	for _, drop := range requiredDrops {
		if !strings.Contains(sqlStr, drop) {
			t.Errorf("down migration is missing drop statement: '%s'", drop)
		}
	}
}

func TestMigrations_Phase3A_UpScriptContainsCanonicalTables(t *testing.T) {
	upContent, err := fs.ReadFile(FS, "sql/000002_resource_credential_foundation.up.sql")
	if err != nil {
		t.Fatalf("failed to read up migration 000002 file: %v", err)
	}

	sqlStr := string(upContent)
	requiredTables := []string{
		"CREATE TABLE IF NOT EXISTS credentials",
		"CREATE TABLE IF NOT EXISTS resources",
		"CREATE TABLE IF NOT EXISTS resource_connectors",
	}

	for _, tbl := range requiredTables {
		if !strings.Contains(sqlStr, tbl) {
			t.Errorf("up migration 000002 is missing required table definition: '%s'", tbl)
		}
	}

	// Verify key constraints and cross-tenant isolation enforcement
	requiredConstraints := []string{
		"CONSTRAINT uq_credentials_org_id_id UNIQUE (organization_id, id)",
		"CONSTRAINT uq_resources_org_id_id UNIQUE (organization_id, id)",
		"CONSTRAINT uq_resource_connectors_resource_id UNIQUE (resource_id)",
		"CONSTRAINT fk_resource_connectors_org_resource FOREIGN KEY (organization_id, resource_id) REFERENCES resources(organization_id, id) ON DELETE CASCADE",
		"CONSTRAINT fk_resource_connectors_org_credential FOREIGN KEY (organization_id, credential_id) REFERENCES credentials(organization_id, id) ON DELETE RESTRICT",
		"encrypted_secret BYTEA NOT NULL",
		"nonce BYTEA NOT NULL",
		"auth_tag BYTEA NOT NULL",
		"key_version INTEGER NOT NULL CHECK (key_version >= 1)",
		"type VARCHAR(50) NOT NULL CHECK (type IN ('ubuntu_ssh', 'cpanel'))",
		"status VARCHAR(30) NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'unreachable', 'disabled', 'error', 'archived'))",
		"connector_type VARCHAR(50) NOT NULL CHECK (connector_type IN ('ubuntu_ssh', 'cpanel'))",
		"auth_type VARCHAR(50) NOT NULL CHECK (auth_type IN ('ssh_key', 'ssh_password', 'cpanel_api_token', 'cpanel_password'))",
		"port INTEGER NOT NULL CHECK (port >= 1 AND port <= 65535)",
	}

	for _, c := range requiredConstraints {
		if !strings.Contains(sqlStr, c) {
			t.Errorf("up migration 000002 is missing required constraint: '%s'", c)
		}
	}

	// Verify no plaintext secret columns exist
	forbiddenPlaintextCols := []string{
		"password VARCHAR",
		"secret VARCHAR",
		"token VARCHAR",
		"plaintext_secret",
		"private_key VARCHAR",
	}
	for _, col := range forbiddenPlaintextCols {
		if strings.Contains(sqlStr, col) {
			t.Errorf("SECURITY FLAW: found potential plaintext column in credentials table: '%s'", col)
		}
	}
}

func TestMigrations_Phase3A_DownScriptContainsReversal(t *testing.T) {
	downContent, err := fs.ReadFile(FS, "sql/000002_resource_credential_foundation.down.sql")
	if err != nil {
		t.Fatalf("failed to read down migration 000002 file: %v", err)
	}

	sqlStr := string(downContent)
	requiredDrops := []string{
		"DROP TABLE IF EXISTS resource_connectors;",
		"DROP TABLE IF EXISTS resources;",
		"DROP TABLE IF EXISTS credentials;",
	}

	for _, drop := range requiredDrops {
		if !strings.Contains(sqlStr, drop) {
			t.Errorf("down migration 000002 is missing drop statement: '%s'", drop)
		}
	}
}

func TestMigrations_Phase5_UpScriptContainsCanonicalTables(t *testing.T) {
	upContent, err := fs.ReadFile(FS, "sql/000003_backup_execution_foundation.up.sql")
	if err != nil {
		t.Fatalf("failed to read up migration 000003 file: %v", err)
	}

	sqlStr := string(upContent)
	requiredTables := []string{
		"CREATE TABLE IF NOT EXISTS backup_plans",
		"CREATE TABLE IF NOT EXISTS storage_targets",
		"CREATE TABLE IF NOT EXISTS backup_jobs",
		"CREATE TABLE IF NOT EXISTS backup_runs",
		"CREATE TABLE IF NOT EXISTS backup_artifacts",
	}

	for _, tbl := range requiredTables {
		if !strings.Contains(sqlStr, tbl) {
			t.Errorf("up migration 000003 is missing required table definition: '%s'", tbl)
		}
	}

	// Verify key constraints and cross-tenant isolation enforcement
	requiredConstraints := []string{
		"CONSTRAINT fk_backup_plans_org_resource FOREIGN KEY (organization_id, resource_id) REFERENCES resources(organization_id, id) ON DELETE CASCADE",
		"CONSTRAINT chk_backup_plans_schedule_cron_required CHECK",
		"CONSTRAINT uq_backup_plans_org_id_id UNIQUE (organization_id, id)",
		"CONSTRAINT fk_storage_targets_org_credential FOREIGN KEY (organization_id, credential_id) REFERENCES credentials(organization_id, id) ON DELETE RESTRICT",
		"CONSTRAINT uq_storage_targets_org_id_id UNIQUE (organization_id, id)",
		"CREATE UNIQUE INDEX IF NOT EXISTS uq_storage_targets_default_per_org ON storage_targets (organization_id) WHERE is_default = true;",
		"CONSTRAINT fk_backup_jobs_org_resource FOREIGN KEY (organization_id, resource_id) REFERENCES resources(organization_id, id) ON DELETE RESTRICT",
		"CONSTRAINT fk_backup_jobs_org_plan FOREIGN KEY (organization_id, backup_plan_id) REFERENCES backup_plans(organization_id, id) ON DELETE SET NULL (backup_plan_id)",
		"CONSTRAINT uq_backup_jobs_org_id_id UNIQUE (organization_id, id)",
		"CREATE UNIQUE INDEX IF NOT EXISTS uq_backup_jobs_manual_active_resource ON backup_jobs (organization_id, resource_id) WHERE trigger_type = 'manual' AND status IN ('pending', 'running');",
		"CONSTRAINT fk_backup_runs_org_job FOREIGN KEY (organization_id, job_id) REFERENCES backup_jobs(organization_id, id) ON DELETE RESTRICT",
		"CONSTRAINT uq_backup_runs_org_id_id UNIQUE (organization_id, id)",
		"CONSTRAINT uq_backup_runs_job_attempt UNIQUE (job_id, attempt_number)",
		"size_bytes BIGINT NOT NULL CHECK (size_bytes > 0)",
		"CONSTRAINT chk_backup_artifacts_checksum_algorithm CHECK (checksum_algorithm = 'sha256')",
		"CONSTRAINT chk_backup_artifacts_checksum_hash_format CHECK (checksum_hash ~ '^[0-9a-f]{64}$')",
		"CONSTRAINT fk_backup_artifacts_org_run FOREIGN KEY (organization_id, run_id) REFERENCES backup_runs(organization_id, id) ON DELETE RESTRICT",
		"CONSTRAINT fk_backup_artifacts_org_resource FOREIGN KEY (organization_id, resource_id) REFERENCES resources(organization_id, id) ON DELETE RESTRICT",
		"CONSTRAINT fk_backup_artifacts_org_storage FOREIGN KEY (organization_id, storage_target_id) REFERENCES storage_targets(organization_id, id) ON DELETE RESTRICT",
		"CONSTRAINT uq_backup_artifacts_org_id_id UNIQUE (organization_id, id)",
	}

	for _, c := range requiredConstraints {
		if !strings.Contains(sqlStr, c) {
			t.Errorf("up migration 000003 is missing required constraint: '%s'", c)
		}
	}
}

func TestMigrations_Phase5_DownScriptContainsReversal(t *testing.T) {
	downContent, err := fs.ReadFile(FS, "sql/000003_backup_execution_foundation.down.sql")
	if err != nil {
		t.Fatalf("failed to read down migration 000003 file: %v", err)
	}

	sqlStr := string(downContent)
	requiredDrops := []string{
		"DROP TABLE IF EXISTS backup_artifacts;",
		"DROP TABLE IF EXISTS backup_runs;",
		"DROP TABLE IF EXISTS backup_jobs;",
		"DROP TABLE IF EXISTS storage_targets;",
		"DROP TABLE IF EXISTS backup_plans;",
	}

	for _, drop := range requiredDrops {
		if !strings.Contains(sqlStr, drop) {
			t.Errorf("down migration 000003 is missing drop statement: '%s'", drop)
		}
	}
}

func TestMigrations_Phase7A_UpScriptContainsCanonicalObjects(t *testing.T) {
	upContent, err := fs.ReadFile(FS, "sql/000004_backup_plan_scheduler_integrity.up.sql")
	if err != nil {
		t.Fatalf("failed to read up migration 000004 file: %v", err)
	}

	sqlStr := string(upContent)
	requiredObjects := []string{
		"CREATE UNIQUE INDEX IF NOT EXISTS uq_backup_jobs_scheduled_pending_plan",
		"ON backup_jobs (organization_id, backup_plan_id)",
		"WHERE trigger_type = 'scheduled'",
		"AND status = 'pending'",
		"AND backup_plan_id IS NOT NULL",
		"ALTER TABLE backup_plans",
		"ADD CONSTRAINT chk_backup_plans_retention_count_positive",
		"CHECK (retention_count IS NULL OR retention_count > 0)",
		"ADD CONSTRAINT chk_backup_plans_retention_days_positive",
		"CHECK (retention_days IS NULL OR retention_days > 0)",
		"CREATE INDEX IF NOT EXISTS idx_backup_plans_due_schedule",
		"ON backup_plans (next_run_at ASC, id ASC)",
		"WHERE status = 'active'",
		"AND is_schedule_enabled = true",
		"AND next_run_at IS NOT NULL",
	}

	for _, obj := range requiredObjects {
		if !strings.Contains(sqlStr, obj) {
			t.Errorf("up migration 000004 is missing required object definition: '%s'", obj)
		}
	}
}

func TestMigrations_Phase7A_DownScriptContainsReversal(t *testing.T) {
	downContent, err := fs.ReadFile(FS, "sql/000004_backup_plan_scheduler_integrity.down.sql")
	if err != nil {
		t.Fatalf("failed to read down migration 000004 file: %v", err)
	}

	sqlStr := string(downContent)
	requiredDrops := []string{
		"DROP INDEX IF EXISTS idx_backup_plans_due_schedule;",
		"ALTER TABLE backup_plans",
		"DROP CONSTRAINT IF EXISTS chk_backup_plans_retention_days_positive;",
		"DROP CONSTRAINT IF EXISTS chk_backup_plans_retention_count_positive;",
		"DROP INDEX IF EXISTS uq_backup_jobs_scheduled_pending_plan;",
	}

	for _, drop := range requiredDrops {
		if !strings.Contains(sqlStr, drop) {
			t.Errorf("down migration 000004 is missing drop statement: '%s'", drop)
		}
	}
}

func TestMigrations_IOFSDriverInitialization(t *testing.T) {
	d, err := iofs.New(FS, "sql")
	if err != nil {
		t.Fatalf("failed to initialize iofs driver with embedded migration FS: %v", err)
	}
	defer func() {
		_ = d.Close()
	}()

	first, err := d.First()
	if err != nil {
		t.Fatalf("failed to read first migration version from iofs driver: %v", err)
	}

	if first != 1 {
		t.Errorf("expected first migration version 1, got: %d", first)
	}

	next, err := d.Next(first)
	if err != nil {
		t.Fatalf("failed to read second migration version: %v", err)
	}
	if next != 2 {
		t.Errorf("expected second migration version 2, got: %d", next)
	}

	third, err := d.Next(next)
	if err != nil {
		t.Fatalf("failed to read third migration version: %v", err)
	}
	if third != 3 {
		t.Errorf("expected third migration version 3, got: %d", third)
	}

	fourth, err := d.Next(third)
	if err != nil {
		t.Fatalf("failed to read fourth migration version: %v", err)
	}
	if fourth != 4 {
		t.Errorf("expected fourth migration version 4, got: %d", fourth)
	}

	fifth, err := d.Next(fourth)
	if err != nil {
		t.Fatalf("failed to read fifth migration version: %v", err)
	}
	if fifth != 5 {
		t.Errorf("expected fifth migration version 5, got: %d", fifth)
	}
}

func TestMigrations_LoadUpAndDownViaDriver(t *testing.T) {
	d, err := iofs.New(FS, "sql")
	if err != nil {
		t.Fatalf("failed to initialize iofs driver: %v", err)
	}
	defer func() {
		_ = d.Close()
	}()

	versions := []struct {
		v          uint
		identifier string
	}{
		{v: 1, identifier: "identity_foundation"},
		{v: 2, identifier: "resource_credential_foundation"},
		{v: 3, identifier: "backup_execution_foundation"},
		{v: 4, identifier: "backup_plan_scheduler_integrity"},
		{v: 5, identifier: "artifact_lifecycle_audit"},
		{v: 6, identifier: "storage_engine_evolution_a1"},
	}

	for _, tc := range versions {
		// 1. Verify Up migration stream is loadable and non-empty
		upReader, identifier, err := d.ReadUp(tc.v)
		if err != nil {
			t.Fatalf("failed to read Up migration for version %d: %v", tc.v, err)
		}

		if identifier != tc.identifier {
			t.Errorf("expected migration identifier '%s', got: '%s'", tc.identifier, identifier)
		}

		upBytes, err := io.ReadAll(upReader)
		_ = upReader.Close()
		if err != nil || len(upBytes) == 0 {
			t.Fatalf("expected non-empty Up migration body for v%d, read error: %v, len: %d", tc.v, err, len(upBytes))
		}

		// 2. Verify Down migration stream is loadable and non-empty
		downReader, downIdentifier, err := d.ReadDown(tc.v)
		if err != nil {
			t.Fatalf("failed to read Down migration for version %d: %v", tc.v, err)
		}

		if downIdentifier != tc.identifier {
			t.Errorf("expected migration identifier '%s', got: '%s'", tc.identifier, downIdentifier)
		}

		downBytes, err := io.ReadAll(downReader)
		_ = downReader.Close()
		if err != nil || len(downBytes) == 0 {
			t.Fatalf("expected non-empty Down migration body for v%d, read error: %v, len: %d", tc.v, err, len(downBytes))
		}
	}
}

func TestMigrations_Phase3A_ResourceStatusIncludesArchived(t *testing.T) {
	upContent, err := fs.ReadFile(FS, "sql/000002_resource_credential_foundation.up.sql")
	if err != nil {
		t.Fatalf("failed to read up migration 000002 file: %v", err)
	}

	sqlStr := string(upContent)
	expectedStatuses := []string{"'active'", "'unreachable'", "'disabled'", "'error'", "'archived'"}
	for _, status := range expectedStatuses {
		if !strings.Contains(sqlStr, status) {
			t.Errorf("resources table status constraint is missing canonical status: %s", status)
		}
	}
}

func TestMigrations_Phase8_AuditLogsTableDefinition(t *testing.T) {
	upContent, err := fs.ReadFile(FS, "sql/000005_artifact_lifecycle_audit.up.sql")
	if err != nil {
		t.Fatalf("failed to read up migration 000005 file: %v", err)
	}

	sqlStr := string(upContent)
	required := []string{
		"CREATE TABLE IF NOT EXISTS audit_logs",
		"id UUID PRIMARY KEY",
		"organization_id UUID NULL REFERENCES organizations(id) ON DELETE RESTRICT",
		"user_id UUID NULL REFERENCES users(id) ON DELETE SET NULL",
		"action VARCHAR(100) NOT NULL",
		"entity_type VARCHAR(50) NOT NULL",
		"entity_id UUID NULL",
		"metadata JSONB NOT NULL DEFAULT '{}'::jsonb",
		"created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()",
		"idx_audit_logs_org_created_at",
		"idx_audit_logs_action",
		"idx_audit_logs_entity",
	}

	for _, item := range required {
		if !strings.Contains(sqlStr, item) {
			t.Errorf("000005 migration missing expected element: %s", item)
		}
	}
}

func TestValidatePostgresVersionNum(t *testing.T) {
	cases := []struct {
		input       string
		expectedErr error
	}{
		{input: "140000", expectedErr: ErrUnsupportedPostgresVersion},
		{input: "149999", expectedErr: ErrUnsupportedPostgresVersion},
		{input: "150000", expectedErr: nil},
		{input: "150001", expectedErr: nil},
		{input: "160000", expectedErr: nil},
		{input: "170000", expectedErr: nil},
		{input: "", expectedErr: ErrPostgresVersionCheckFailed},
		{input: "   ", expectedErr: ErrPostgresVersionCheckFailed},
		{input: "invalid_str", expectedErr: ErrPostgresVersionCheckFailed},
		{input: "15.0", expectedErr: ErrPostgresVersionCheckFailed},
	}

	for _, tc := range cases {
		t.Run("Version_"+tc.input, func(t *testing.T) {
			err := ValidatePostgresVersionNum(tc.input)
			if !errors.Is(err, tc.expectedErr) {
				t.Errorf("ValidatePostgresVersionNum(%q) = %v, expected %v", tc.input, err, tc.expectedErr)
			}
		})
	}
}

func TestMigrations_StepA1_StorageEngineEvolutionDefinition(t *testing.T) {
	upContent, err := fs.ReadFile(FS, "sql/000006_storage_engine_evolution_a1.up.sql")
	if err != nil {
		t.Fatalf("failed to read up migration 000006 file: %v", err)
	}

	sqlStr := string(upContent)
	required := []string{
		"chk_storage_targets_status CHECK (status IN ('active', 'disabled', 'error', 'archived'))",
		"ranked_local",
		"INSERT INTO storage_targets",
		"ALTER TABLE backup_plans ADD COLUMN IF NOT EXISTS engine_type VARCHAR(50) NULL;",
		"ALTER TABLE backup_plans ADD COLUMN IF NOT EXISTS storage_target_id UUID NULL;",
		"UPDATE backup_plans bp",
		"chk_backup_plans_engine_type CHECK (engine_type IN ('direct_stream'))",
		"CONSTRAINT fk_backup_plans_org_storage FOREIGN KEY (organization_id, storage_target_id) REFERENCES storage_targets(organization_id, id) ON DELETE RESTRICT",
		"ALTER TABLE backup_jobs ADD COLUMN IF NOT EXISTS engine_type VARCHAR(50) NULL;",
		"ALTER TABLE backup_jobs ADD COLUMN IF NOT EXISTS storage_target_id UUID NULL;",
		"UPDATE backup_jobs bj",
		"chk_backup_jobs_engine_type CHECK (engine_type IN ('direct_stream'))",
		"CONSTRAINT fk_backup_jobs_org_storage FOREIGN KEY (organization_id, storage_target_id) REFERENCES storage_targets(organization_id, id) ON DELETE RESTRICT",
		"CREATE INDEX IF NOT EXISTS idx_backup_plans_org_storage",
		"CREATE INDEX IF NOT EXISTS idx_backup_jobs_org_storage",
	}

	for _, item := range required {
		if !strings.Contains(sqlStr, item) {
			t.Errorf("000006 migration missing expected element: %s", item)
		}
	}

	downContent, err := fs.ReadFile(FS, "sql/000006_storage_engine_evolution_a1.down.sql")
	if err != nil {
		t.Fatalf("failed to read down migration 000006 file: %v", err)
	}

	downStr := string(downContent)
	requiredDown := []string{
		"DROP INDEX IF EXISTS idx_backup_jobs_org_storage;",
		"DROP INDEX IF EXISTS idx_backup_plans_org_storage;",
		"ALTER TABLE backup_jobs DROP CONSTRAINT IF EXISTS fk_backup_jobs_org_storage;",
		"ALTER TABLE backup_jobs DROP COLUMN IF EXISTS storage_target_id;",
		"ALTER TABLE backup_jobs DROP COLUMN IF EXISTS engine_type;",
		"ALTER TABLE backup_plans DROP CONSTRAINT IF EXISTS fk_backup_plans_org_storage;",
		"ALTER TABLE backup_plans DROP COLUMN IF EXISTS storage_target_id;",
		"ALTER TABLE backup_plans DROP COLUMN IF EXISTS engine_type;",
		"UPDATE storage_targets SET status = 'disabled' WHERE status = 'archived';",
		"ALTER TABLE storage_targets DROP CONSTRAINT IF EXISTS chk_storage_targets_status;",
	}

	for _, item := range requiredDown {
		if !strings.Contains(downStr, item) {
			t.Errorf("000006 down migration missing expected element: %s", item)
		}
	}
}

func TestMigrations_StepA1_Integration(t *testing.T) {
	testDBURL := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	if testDBURL == "" {
		t.Skip("skipping migration integration test: TEST_DATABASE_URL not set")
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

	// 1. Bring database to version 5 (pre-Step A.1 baseline)
	if err := m.Migrate(5); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		t.Fatalf("failed migrating to version 5 baseline: %v", err)
	}

	// 2. Setup isolated test organizations and legacy data
	org1ID := uuid.New()
	org2ID := uuid.New()
	res1ID := uuid.New()
	res2ID := uuid.New()
	plan1ID := uuid.New()
	plan2ID := uuid.New()
	job1ID := uuid.New()
	job2ID := uuid.New()

	cleanup := func() {
		cleanupCtx := context.Background()
		_, errJobs := conn.Exec(cleanupCtx, "DELETE FROM backup_jobs WHERE organization_id IN ($1, $2)", org1ID, org2ID)
		_, errPlans := conn.Exec(cleanupCtx, "DELETE FROM backup_plans WHERE organization_id IN ($1, $2)", org1ID, org2ID)
		_, errTargets := conn.Exec(cleanupCtx, "DELETE FROM storage_targets WHERE organization_id IN ($1, $2)", org1ID, org2ID)
		_, errRes := conn.Exec(cleanupCtx, "DELETE FROM resources WHERE organization_id IN ($1, $2)", org1ID, org2ID)
		_, errOrgs := conn.Exec(cleanupCtx, "DELETE FROM organizations WHERE id IN ($1, $2)", org1ID, org2ID)
		if errJobs != nil || errPlans != nil || errTargets != nil || errRes != nil || errOrgs != nil {
			t.Errorf("cleanup failed: jobs=%v, plans=%v, targets=%v, res=%v, orgs=%v", errJobs, errPlans, errTargets, errRes, errOrgs)
		}
	}
	cleanup()
	defer cleanup()

	// Insert organizations using exact v1 schema
	_, err = conn.Exec(ctx, `
		INSERT INTO organizations (id, name, slug, status, metadata, created_at, updated_at)
		VALUES ($1, 'Org One', $3, 'active', '{}'::jsonb, NOW(), NOW()),
		       ($2, 'Org Two', $4, 'active', '{}'::jsonb, NOW(), NOW())`,
		org1ID, org2ID, "org-one-"+org1ID.String()[:8], "org-two-"+org2ID.String()[:8])
	if err != nil {
		t.Fatalf("failed inserting test organizations: %v", err)
	}

	// Insert resources using exact v2 schema
	_, err = conn.Exec(ctx, `
		INSERT INTO resources (id, organization_id, name, type, status, metadata, created_at, updated_at)
		VALUES ($1, $2, 'Res 1', 'ubuntu_ssh', 'active', '{}'::jsonb, NOW(), NOW()),
		       ($3, $4, 'Res 2', 'ubuntu_ssh', 'active', '{}'::jsonb, NOW(), NOW())`,
		res1ID, org1ID, res2ID, org2ID)
	if err != nil {
		t.Fatalf("failed inserting test resources: %v", err)
	}

	// Case A: Org 1 has NO storage targets at all
	// Case B: Org 2 has a legacy non-local (s3) default target AND a disabled local target
	s3TargetID := uuid.New()
	disabledLocalTargetID := uuid.New()
	_, err = conn.Exec(ctx, `
		INSERT INTO storage_targets (id, organization_id, name, type, status, is_default, config, created_at, updated_at)
		VALUES ($1, $2, 'Legacy S3 Default', 's3', 'active', true, '{}'::jsonb, NOW(), NOW()),
		       ($3, $2, 'Disabled Local', 'local', 'disabled', false, '{}'::jsonb, NOW(), NOW())`,
		s3TargetID, org2ID, disabledLocalTargetID)
	if err != nil {
		t.Fatalf("failed inserting Org 2 storage targets: %v", err)
	}

	// Insert historical plans using exact v3/v4 schema (no engine_type or storage_target_id)
	_, err = conn.Exec(ctx, `
		INSERT INTO backup_plans (
			id, organization_id, resource_id, name, backup_type, target_spec,
			schedule_cron, schedule_timezone, is_schedule_enabled, retention_count,
			status, next_run_at, created_at, updated_at
		) VALUES (
			$1, $2, $3, 'Plan 1', 'mysql_database', '{"databases":["testdb"]}'::jsonb,
			'0 0 * * *', 'UTC', true, 5,
			'active', NOW(), NOW(), NOW()
		), (
			$4, $5, $6, 'Plan 2', 'mysql_database', '{"databases":["testdb"]}'::jsonb,
			'0 0 * * *', 'UTC', true, 5,
			'active', NOW(), NOW(), NOW()
		)`,
		plan1ID, org1ID, res1ID, plan2ID, org2ID, res2ID)
	if err != nil {
		t.Fatalf("failed inserting v5 backup plans: %v", err)
	}

	// Insert historical jobs using exact v3 schema (no engine_type or storage_target_id)
	_, err = conn.Exec(ctx, `
		INSERT INTO backup_jobs (
			id, organization_id, resource_id, backup_plan_id, trigger_type,
			backup_type, target_spec, status, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, 'scheduled',
			'mysql_database', '{"databases":["testdb"]}'::jsonb, 'completed', NOW(), NOW()
		), (
			$5, $6, $7, $8, 'scheduled',
			'mysql_database', '{"databases":["testdb"]}'::jsonb, 'completed', NOW(), NOW()
		)`,
		job1ID, org1ID, res1ID, plan1ID,
		job2ID, org2ID, res2ID, plan2ID)
	if err != nil {
		t.Fatalf("failed inserting v5 backup jobs: %v", err)
	}

	// 3. Apply migration 000006 (Step A.1 Up)
	if err := m.Migrate(6); err != nil {
		t.Fatalf("migration 000006 up failed: %v", err)
	}

	// Scenario A: Org 1 with no storage target: v6 creates one active default Local target, historical Plan and Job backfill to it
	var org1TargetID uuid.UUID
	var org1TargetType, org1TargetStatus string
	var org1IsDefault bool
	err = conn.QueryRow(ctx, `
		SELECT id, type, status, is_default FROM storage_targets
		WHERE organization_id = $1 AND is_default = true`, org1ID).Scan(&org1TargetID, &org1TargetType, &org1TargetStatus, &org1IsDefault)
	if err != nil {
		t.Fatalf("failed querying Org 1 default storage target: %v", err)
	}
	if org1TargetType != "local" || org1TargetStatus != "active" || !org1IsDefault {
		t.Errorf("expected Org 1 default target to be local/active/true, got type=%s status=%s is_default=%v", org1TargetType, org1TargetStatus, org1IsDefault)
	}

	var p1Engine string
	var p1StorageID uuid.UUID
	err = conn.QueryRow(ctx, `SELECT engine_type, storage_target_id FROM backup_plans WHERE id = $1`, plan1ID).Scan(&p1Engine, &p1StorageID)
	if err != nil {
		t.Fatalf("failed querying Org 1 backfilled plan: %v", err)
	}
	if p1Engine != "direct_stream" || p1StorageID != org1TargetID {
		t.Errorf("expected plan 1 engine=direct_stream storage=%s, got engine=%s storage=%s", org1TargetID, p1Engine, p1StorageID)
	}

	var j1Engine string
	var j1StorageID uuid.UUID
	err = conn.QueryRow(ctx, `SELECT engine_type, storage_target_id FROM backup_jobs WHERE id = $1`, job1ID).Scan(&j1Engine, &j1StorageID)
	if err != nil {
		t.Fatalf("failed querying Org 1 backfilled job: %v", err)
	}
	if j1Engine != "direct_stream" || j1StorageID != org1TargetID {
		t.Errorf("expected job 1 engine=direct_stream storage=%s, got engine=%s storage=%s", org1TargetID, j1Engine, j1StorageID)
	}

	// Scenario B: Org 2 with S3 default and disabled Local target:
	// After v6: S3 is no longer default, Local becomes active default, historical Plan and Job point to that Local target.
	var org2TargetID uuid.UUID
	var org2TargetType, org2TargetStatus string
	var org2IsDefault bool
	err = conn.QueryRow(ctx, `
		SELECT id, type, status, is_default FROM storage_targets
		WHERE organization_id = $1 AND is_default = true`, org2ID).Scan(&org2TargetID, &org2TargetType, &org2TargetStatus, &org2IsDefault)
	if err != nil {
		t.Fatalf("failed querying Org 2 default storage target: %v", err)
	}
	if org2TargetID != disabledLocalTargetID || org2TargetType != "local" || org2TargetStatus != "active" || !org2IsDefault {
		t.Errorf("expected Org 2 local target promoted to active default, got id=%s type=%s status=%s is_default=%v", org2TargetID, org2TargetType, org2TargetStatus, org2IsDefault)
	}

	var s3IsDefault bool
	err = conn.QueryRow(ctx, `SELECT is_default FROM storage_targets WHERE id = $1`, s3TargetID).Scan(&s3IsDefault)
	if err != nil {
		t.Fatalf("failed querying legacy S3 target: %v", err)
	}
	if s3IsDefault {
		t.Errorf("expected legacy S3 target to be demoted to is_default=false")
	}

	var p2Engine string
	var p2StorageID uuid.UUID
	err = conn.QueryRow(ctx, `SELECT engine_type, storage_target_id FROM backup_plans WHERE id = $1`, plan2ID).Scan(&p2Engine, &p2StorageID)
	if err != nil {
		t.Fatalf("failed querying Org 2 backfilled plan: %v", err)
	}
	if p2Engine != "direct_stream" || p2StorageID != disabledLocalTargetID {
		t.Errorf("expected plan 2 engine=direct_stream storage=%s, got engine=%s storage=%s", disabledLocalTargetID, p2Engine, p2StorageID)
	}

	var j2Engine string
	var j2StorageID uuid.UUID
	err = conn.QueryRow(ctx, `SELECT engine_type, storage_target_id FROM backup_jobs WHERE id = $1`, job2ID).Scan(&j2Engine, &j2StorageID)
	if err != nil {
		t.Fatalf("failed querying Org 2 backfilled job: %v", err)
	}
	if j2Engine != "direct_stream" || j2StorageID != disabledLocalTargetID {
		t.Errorf("expected job 2 engine=direct_stream storage=%s, got engine=%s storage=%s", disabledLocalTargetID, j2Engine, j2StorageID)
	}

	// Scenario C: Multiple organizations - no cross-org backfill
	if p1StorageID == p2StorageID || j1StorageID == j2StorageID {
		t.Fatalf("cross-org backfill detected: Org 1 and Org 2 have same storage target")
	}
	if p1StorageID != org1TargetID || j1StorageID != org1TargetID {
		t.Errorf("Org 1 plan or job storage target does not match Org 1 target")
	}
	if p2StorageID != disabledLocalTargetID || j2StorageID != disabledLocalTargetID {
		t.Errorf("Org 2 plan or job storage target does not match Org 2 target")
	}

	// Scenario D: Backfilled values: engine_type = direct_stream, storage_target_id NOT NULL
	// Verify NOT NULL constraint on backup_plans and backup_jobs
	_, err = conn.Exec(ctx, `UPDATE backup_plans SET storage_target_id = NULL WHERE id = $1`, plan1ID)
	if err == nil {
		t.Errorf("expected NOT NULL constraint violation when setting backup_plans.storage_target_id = NULL")
	}
	_, err = conn.Exec(ctx, `UPDATE backup_jobs SET storage_target_id = NULL WHERE id = $1`, job1ID)
	if err == nil {
		t.Errorf("expected NOT NULL constraint violation when setting backup_jobs.storage_target_id = NULL")
	}

	// Scenario E: Composite FK: attempt to set Org A plan/job storage_target_id to Org B target must fail.
	// Test BOTH:
	// - backup_plans FK
	_, err = conn.Exec(ctx, `UPDATE backup_plans SET storage_target_id = $1 WHERE id = $2`, disabledLocalTargetID, plan1ID)
	if err == nil {
		t.Fatalf("expected cross-tenant FK violation on backup_plans when setting Org 1 plan to Org 2 storage target, got success")
	}
	// - backup_jobs FK
	_, err = conn.Exec(ctx, `UPDATE backup_jobs SET storage_target_id = $1 WHERE id = $2`, disabledLocalTargetID, job1ID)
	if err == nil {
		t.Fatalf("expected cross-tenant FK violation on backup_jobs when setting Org 1 job to Org 2 storage target, got success")
	}

	// Scenario F: Lifecycle rollback:
	// - set a target to archived under v6
	_, err = conn.Exec(ctx, `UPDATE storage_targets SET status = 'archived' WHERE id = $1`, s3TargetID)
	if err != nil {
		t.Fatalf("failed setting storage target to archived under v6: %v", err)
	}
	// - migrate DOWN to v5
	if err := m.Migrate(5); err != nil {
		t.Fatalf("migration down to version 5 failed with archived target: %v", err)
	}
	// - DOWN succeeds, archived becomes disabled
	var rolledBackStatus string
	err = conn.QueryRow(ctx, `SELECT status FROM storage_targets WHERE id = $1`, s3TargetID).Scan(&rolledBackStatus)
	if err != nil {
		t.Fatalf("failed querying rolled back target status: %v", err)
	}
	if rolledBackStatus != "disabled" {
		t.Errorf("expected archived target to be converted to disabled on rollback, got: %s", rolledBackStatus)
	}
	// - legacy CHECK is valid: inserting or setting status to 'archived' under v5 must fail
	_, err = conn.Exec(ctx, `UPDATE storage_targets SET status = 'archived' WHERE id = $1`, s3TargetID)
	if err == nil {
		t.Errorf("expected status 'archived' to be rejected by legacy CHECK constraint under v5, but update succeeded")
	}

	// Scenario G: Re-apply v6: succeeds again
	if err := m.Migrate(6); err != nil {
		t.Fatalf("re-applying migration 000006 failed: %v", err)
	}
}
