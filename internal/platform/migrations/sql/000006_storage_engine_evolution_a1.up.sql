-- ==============================================================================
-- 000006_storage_engine_evolution_a1.up.sql
-- Storage & Backup Engine Evolution (Future Phase A - Step A.1)
-- Canonical References: ADR-031, ADR-032, ADR-033, docs/DECISIONS.md
-- ==============================================================================

-- 1. Ensure a default local storage target exists for every organization that does not have one
INSERT INTO storage_targets (
    id, organization_id, name, type, status, is_default, credential_id, config, created_at, updated_at
)
SELECT
    gen_random_uuid(),
    o.id,
    'Default Local Storage',
    'local',
    'active',
    true,
    NULL,
    '{}'::jsonb,
    NOW(),
    NOW()
FROM organizations o
WHERE NOT EXISTS (
    SELECT 1 FROM storage_targets st
    WHERE st.organization_id = o.id AND st.is_default = true
)
ON CONFLICT (organization_id) WHERE is_default = true DO NOTHING;

-- 2. Update storage_targets status check constraint to include 'archived'
ALTER TABLE storage_targets DROP CONSTRAINT IF EXISTS storage_targets_status_check;
ALTER TABLE storage_targets DROP CONSTRAINT IF EXISTS chk_storage_targets_status;
ALTER TABLE storage_targets ADD CONSTRAINT chk_storage_targets_status CHECK (status IN ('active', 'disabled', 'error', 'archived'));

-- 3. Add engine_type and storage_target_id columns to backup_plans
ALTER TABLE backup_plans ADD COLUMN IF NOT EXISTS engine_type VARCHAR(50) NULL;
ALTER TABLE backup_plans ADD COLUMN IF NOT EXISTS storage_target_id UUID NULL;

-- 4. Backfill historical and existing backup_plans to direct_stream and the organization's default local target
UPDATE backup_plans bp
SET
    engine_type = 'direct_stream',
    storage_target_id = st.id
FROM storage_targets st
WHERE st.organization_id = bp.organization_id
  AND st.is_default = true
  AND (bp.engine_type IS NULL OR bp.storage_target_id IS NULL);

-- 5. Enforce NOT NULL, check constraint and composite FK on backup_plans
ALTER TABLE backup_plans ALTER COLUMN engine_type SET NOT NULL;
ALTER TABLE backup_plans ALTER COLUMN storage_target_id SET NOT NULL;
ALTER TABLE backup_plans ADD CONSTRAINT chk_backup_plans_engine_type CHECK (engine_type IN ('direct_stream'));
ALTER TABLE backup_plans ADD CONSTRAINT fk_backup_plans_org_storage FOREIGN KEY (organization_id, storage_target_id) REFERENCES storage_targets(organization_id, id) ON DELETE RESTRICT;

-- 6. Add engine_type and storage_target_id columns to backup_jobs
ALTER TABLE backup_jobs ADD COLUMN IF NOT EXISTS engine_type VARCHAR(50) NULL;
ALTER TABLE backup_jobs ADD COLUMN IF NOT EXISTS storage_target_id UUID NULL;

-- 7. Backfill historical and existing backup_jobs to direct_stream and the organization's default local target
UPDATE backup_jobs bj
SET
    engine_type = 'direct_stream',
    storage_target_id = st.id
FROM storage_targets st
WHERE st.organization_id = bj.organization_id
  AND st.is_default = true
  AND (bj.engine_type IS NULL OR bj.storage_target_id IS NULL);

-- 8. Enforce NOT NULL, check constraint and composite FK on backup_jobs
ALTER TABLE backup_jobs ALTER COLUMN engine_type SET NOT NULL;
ALTER TABLE backup_jobs ALTER COLUMN storage_target_id SET NOT NULL;
ALTER TABLE backup_jobs ADD CONSTRAINT chk_backup_jobs_engine_type CHECK (engine_type IN ('direct_stream'));
ALTER TABLE backup_jobs ADD CONSTRAINT fk_backup_jobs_org_storage FOREIGN KEY (organization_id, storage_target_id) REFERENCES storage_targets(organization_id, id) ON DELETE RESTRICT;

-- 9. Add composite indexes for high-performance organization-scoped storage lookups
CREATE INDEX IF NOT EXISTS idx_backup_plans_org_storage ON backup_plans(organization_id, storage_target_id);
CREATE INDEX IF NOT EXISTS idx_backup_jobs_org_storage ON backup_jobs(organization_id, storage_target_id);
