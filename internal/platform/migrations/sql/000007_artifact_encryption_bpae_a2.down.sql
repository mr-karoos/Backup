-- ==============================================================================
-- 000007_artifact_encryption_bpae_a2.down.sql
-- Rollback for Direct Stream Artifact Encryption at Rest (Future Phase A - Step A.2)
-- ==============================================================================

-- Fail closed if any encrypted artifact records exist
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM backup_artifacts WHERE stored_size_bytes IS NOT NULL) THEN
        RAISE EXCEPTION 'migration 000007 rollback failed: cannot downgrade while encrypted artifacts exist';
    END IF;
END $$;

ALTER TABLE backup_artifacts DROP CONSTRAINT IF EXISTS chk_backup_artifacts_ciphertext_hash;
ALTER TABLE backup_artifacts DROP CONSTRAINT IF EXISTS chk_backup_artifacts_stored_size;
ALTER TABLE backup_artifacts DROP COLUMN IF EXISTS engine_metadata;
ALTER TABLE backup_artifacts DROP COLUMN IF EXISTS stored_size_bytes;
