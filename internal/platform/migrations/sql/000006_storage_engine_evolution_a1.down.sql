-- ==============================================================================
-- 000006_storage_engine_evolution_a1.down.sql
-- Rollback for Storage & Backup Engine Evolution (Future Phase A - Step A.1)
-- ==============================================================================

DROP INDEX IF EXISTS idx_backup_jobs_org_storage;
DROP INDEX IF EXISTS idx_backup_plans_org_storage;

ALTER TABLE backup_jobs DROP CONSTRAINT IF EXISTS fk_backup_jobs_org_storage;
ALTER TABLE backup_jobs DROP CONSTRAINT IF EXISTS chk_backup_jobs_engine_type;
ALTER TABLE backup_jobs DROP COLUMN IF EXISTS storage_target_id;
ALTER TABLE backup_jobs DROP COLUMN IF EXISTS engine_type;

ALTER TABLE backup_plans DROP CONSTRAINT IF EXISTS fk_backup_plans_org_storage;
ALTER TABLE backup_plans DROP CONSTRAINT IF EXISTS chk_backup_plans_engine_type;
ALTER TABLE backup_plans DROP COLUMN IF EXISTS storage_target_id;
ALTER TABLE backup_plans DROP COLUMN IF EXISTS engine_type;

-- Before restoring the old status CHECK (which excludes 'archived'), convert existing 'archived' status to 'disabled'
UPDATE storage_targets SET status = 'disabled' WHERE status = 'archived';

ALTER TABLE storage_targets DROP CONSTRAINT IF EXISTS chk_storage_targets_status;
ALTER TABLE storage_targets DROP CONSTRAINT IF EXISTS storage_targets_status_check;
ALTER TABLE storage_targets ADD CONSTRAINT storage_targets_status_check CHECK (status IN ('active', 'disabled', 'error'));
