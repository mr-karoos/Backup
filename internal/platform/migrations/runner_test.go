package migrations

import (
	"errors"
	"io"
	"io/fs"
	"strings"
	"testing"

	"github.com/golang-migrate/migrate/v4/source/iofs"
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
