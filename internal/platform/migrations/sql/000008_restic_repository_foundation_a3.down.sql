-- ==============================================================================
-- 000008_restic_repository_foundation_a3.down.sql
-- Rollback for Restic Repository Entity & System Credentials (Future Phase A - Step A.3)
-- ==============================================================================

-- Fail closed if any backup_repositories exist
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM backup_repositories) THEN
        RAISE EXCEPTION 'migration 000008 rollback failed: cannot downgrade while backup_repositories exist';
    END IF;

    IF EXISTS (SELECT 1 FROM credentials WHERE managed_by = 'system' OR type = 'restic_repository_key') THEN
        RAISE EXCEPTION 'migration 000008 rollback failed: cannot downgrade while system restic_repository_key credentials exist';
    END IF;
END $$;

DROP TABLE IF EXISTS backup_repositories;
ALTER TABLE credentials DROP CONSTRAINT IF EXISTS chk_credentials_restic_key;
ALTER TABLE credentials DROP CONSTRAINT IF EXISTS chk_credentials_managed_by;
DROP INDEX IF EXISTS idx_credentials_org_managed_by;
ALTER TABLE credentials DROP COLUMN IF EXISTS managed_by;
