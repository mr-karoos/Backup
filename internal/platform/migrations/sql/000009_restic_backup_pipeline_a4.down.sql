-- ==============================================================================
-- 000009_restic_backup_pipeline_a4.down.sql
-- Rollback Polymorphic Backup Artifacts (Future Phase A - Step A.4)
-- Canonical References: ADR-031, ADR-033, docs/DECISIONS.md
-- ==============================================================================

-- 1. Fail-closed check: abort if any restic_snapshot artifact exists
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM backup_artifacts WHERE format = 'restic_snapshot') THEN
        RAISE EXCEPTION 'cannot rollback migration 000009: live restic snapshot artifacts exist';
    END IF;
END $$;

-- 2. Drop Restic constraints and indexes
ALTER TABLE backup_artifacts DROP CONSTRAINT IF EXISTS chk_backup_artifacts_restic_fields;
ALTER TABLE backup_artifacts DROP CONSTRAINT IF EXISTS chk_backup_artifacts_direct_stream_fields;
ALTER TABLE backup_artifacts DROP CONSTRAINT IF EXISTS fk_backup_artifacts_org_repository;
DROP INDEX IF EXISTS idx_backup_artifacts_snapshot_id;
DROP INDEX IF EXISTS idx_backup_artifacts_repository_id;

-- 3. Drop Restic polymorphic columns
ALTER TABLE backup_artifacts DROP COLUMN IF EXISTS logical_size_bytes;
ALTER TABLE backup_artifacts DROP COLUMN IF EXISTS snapshot_id;
ALTER TABLE backup_artifacts DROP COLUMN IF EXISTS repository_id;

-- 4. Restore original format and checksum constraints
ALTER TABLE backup_artifacts DROP CONSTRAINT IF EXISTS chk_backup_artifacts_checksum_hash_format;
ALTER TABLE backup_artifacts ADD CONSTRAINT chk_backup_artifacts_checksum_hash_format CHECK (checksum_hash ~ '^[0-9a-f]{64}$');

ALTER TABLE backup_artifacts DROP CONSTRAINT IF EXISTS chk_backup_artifacts_checksum_algorithm;
ALTER TABLE backup_artifacts ADD CONSTRAINT chk_backup_artifacts_checksum_algorithm CHECK (checksum_algorithm = 'sha256');

ALTER TABLE backup_artifacts DROP CONSTRAINT IF EXISTS chk_backup_artifacts_format;
ALTER TABLE backup_artifacts ADD CONSTRAINT chk_backup_artifacts_format CHECK (format IN ('sql_gzip', 'tar_gzip'));

ALTER TABLE backup_artifacts DROP CONSTRAINT IF EXISTS chk_backup_artifacts_size_bytes;
ALTER TABLE backup_artifacts ADD CONSTRAINT chk_backup_artifacts_size_bytes CHECK (size_bytes > 0);

-- 5. Restore NOT NULL constraints
ALTER TABLE backup_artifacts ALTER COLUMN checksum_hash SET NOT NULL;
ALTER TABLE backup_artifacts ALTER COLUMN checksum_algorithm SET NOT NULL;
ALTER TABLE backup_artifacts ALTER COLUMN size_bytes SET NOT NULL;
ALTER TABLE backup_artifacts ALTER COLUMN storage_reference SET NOT NULL;
