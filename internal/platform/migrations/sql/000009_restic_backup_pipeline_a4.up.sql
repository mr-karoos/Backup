-- ==============================================================================
-- 000009_restic_backup_pipeline_a4.up.sql
-- Polymorphic Backup Artifacts & Restic Snapshot Pipeline (Future Phase A - Step A.4)
-- Canonical References: ADR-031, ADR-033, ADR-034, ADR-035, docs/DECISIONS.md
-- ==============================================================================

-- 1. Drop NOT NULL on legacy Direct Stream specific columns
ALTER TABLE backup_artifacts ALTER COLUMN storage_reference DROP NOT NULL;
ALTER TABLE backup_artifacts ALTER COLUMN size_bytes DROP NOT NULL;
ALTER TABLE backup_artifacts ALTER COLUMN checksum_algorithm DROP NOT NULL;
ALTER TABLE backup_artifacts ALTER COLUMN checksum_hash DROP NOT NULL;

-- 2. Drop legacy format and size constraints to allow polymorphic evolution
DO $$
DECLARE
    r RECORD;
BEGIN
    FOR r IN (
        SELECT conname
        FROM pg_constraint
        WHERE conrelid = 'backup_artifacts'::regclass
          AND contype = 'c'
          AND (conname LIKE '%format%' OR conname LIKE '%size_bytes%')
    ) LOOP
        EXECUTE 'ALTER TABLE backup_artifacts DROP CONSTRAINT ' || quote_ident(r.conname);
    END LOOP;
END $$;

-- 3. Add format check constraint supporting restic_snapshot
ALTER TABLE backup_artifacts ADD CONSTRAINT chk_backup_artifacts_format
    CHECK (format IN ('sql_gzip', 'tar_gzip', 'restic_snapshot'));

-- 4. Add polymorphic Restic columns to backup_artifacts
ALTER TABLE backup_artifacts ADD COLUMN IF NOT EXISTS repository_id UUID NULL;
ALTER TABLE backup_artifacts ADD COLUMN IF NOT EXISTS snapshot_id VARCHAR(64) NULL;
ALTER TABLE backup_artifacts ADD COLUMN IF NOT EXISTS logical_size_bytes BIGINT NULL;

-- 5. Add tenant-safe composite foreign key linking artifact to backup_repositories
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'fk_backup_artifacts_org_repository'
    ) THEN
        ALTER TABLE backup_artifacts ADD CONSTRAINT fk_backup_artifacts_org_repository
            FOREIGN KEY (organization_id, repository_id)
            REFERENCES backup_repositories(organization_id, id)
            ON DELETE RESTRICT;
    END IF;
END $$;

-- 6. Add indexes on new polymorphic columns
CREATE INDEX IF NOT EXISTS idx_backup_artifacts_repository_id ON backup_artifacts(repository_id);
CREATE INDEX IF NOT EXISTS idx_backup_artifacts_snapshot_id ON backup_artifacts(snapshot_id);

-- 7. Update checksum constraints to allow NULL for restic_snapshot
ALTER TABLE backup_artifacts DROP CONSTRAINT IF EXISTS chk_backup_artifacts_checksum_algorithm;
ALTER TABLE backup_artifacts ADD CONSTRAINT chk_backup_artifacts_checksum_algorithm CHECK (
    (format = 'restic_snapshot' AND checksum_algorithm IS NULL) OR
    (format IN ('sql_gzip', 'tar_gzip') AND checksum_algorithm = 'sha256')
);

ALTER TABLE backup_artifacts DROP CONSTRAINT IF EXISTS chk_backup_artifacts_checksum_hash_format;
ALTER TABLE backup_artifacts ADD CONSTRAINT chk_backup_artifacts_checksum_hash_format CHECK (
    (format = 'restic_snapshot' AND checksum_hash IS NULL) OR
    (format IN ('sql_gzip', 'tar_gzip') AND checksum_hash ~ '^[0-9a-f]{64}$')
);

-- 8. Add polymorphic constraint for Direct Stream artifacts
ALTER TABLE backup_artifacts DROP CONSTRAINT IF EXISTS chk_backup_artifacts_direct_stream_fields;
ALTER TABLE backup_artifacts ADD CONSTRAINT chk_backup_artifacts_direct_stream_fields CHECK (
    format NOT IN ('sql_gzip', 'tar_gzip') OR (
        storage_reference IS NOT NULL AND
        size_bytes IS NOT NULL AND size_bytes > 0 AND
        checksum_algorithm = 'sha256' AND
        checksum_hash IS NOT NULL AND
        repository_id IS NULL AND
        snapshot_id IS NULL AND
        logical_size_bytes IS NULL
    )
);

-- 9. Add polymorphic constraint for Restic snapshot artifacts
ALTER TABLE backup_artifacts DROP CONSTRAINT IF EXISTS chk_backup_artifacts_restic_fields;
ALTER TABLE backup_artifacts ADD CONSTRAINT chk_backup_artifacts_restic_fields CHECK (
    format != 'restic_snapshot' OR (
        storage_reference IS NULL AND
        size_bytes IS NULL AND
        stored_size_bytes IS NULL AND
        checksum_algorithm IS NULL AND
        checksum_hash IS NULL AND
        repository_id IS NOT NULL AND
        snapshot_id IS NOT NULL AND snapshot_id ~ '^[0-9a-f]{8,64}$' AND
        logical_size_bytes IS NOT NULL AND logical_size_bytes > 0
    )
);
