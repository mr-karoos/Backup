-- ==============================================================================
-- 000006_storage_engine_evolution_a1.up.sql
-- Storage & Backup Engine Evolution (Future Phase A - Step A.1)
-- Canonical References: ADR-031, ADR-032, ADR-033, docs/DECISIONS.md
-- ==============================================================================

-- 1. Update storage_targets status check constraint to include 'archived'
ALTER TABLE storage_targets DROP CONSTRAINT IF EXISTS storage_targets_status_check;
ALTER TABLE storage_targets DROP CONSTRAINT IF EXISTS chk_storage_targets_status;
ALTER TABLE storage_targets ADD CONSTRAINT chk_storage_targets_status CHECK (status IN ('active', 'disabled', 'error', 'archived'));

-- 2. Ensure each organization has exactly one ACTIVE DEFAULT LOCAL storage target:
-- 2a. If an org has a non-local target marked as default while having a local target, demote the non-local target:
UPDATE storage_targets st
SET is_default = false
WHERE st.is_default = true
  AND st.type != 'local'
  AND EXISTS (
      SELECT 1 FROM storage_targets loc
      WHERE loc.organization_id = st.organization_id AND loc.type = 'local'
  );

-- 2b. For orgs with existing local target(s), ensure exactly one is active and default, demoting others:
WITH ranked_local AS (
    SELECT id, organization_id,
           ROW_NUMBER() OVER (PARTITION BY organization_id ORDER BY is_default DESC, created_at ASC) as rn
    FROM storage_targets
    WHERE type = 'local'
)
UPDATE storage_targets st
SET is_default = (rl.rn = 1),
    status = CASE WHEN rl.rn = 1 THEN 'active' ELSE st.status END,
    updated_at = NOW()
FROM ranked_local rl
WHERE st.id = rl.id;

-- 2c. For orgs with NO local target at all, demote any existing default (e.g. S3):
UPDATE storage_targets st
SET is_default = false
WHERE st.is_default = true
  AND NOT EXISTS (
      SELECT 1 FROM storage_targets loc
      WHERE loc.organization_id = st.organization_id AND loc.type = 'local'
  );

-- 2d. Insert an active default local storage target for every organization that has no local target:
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
    SELECT 1 FROM storage_targets loc
    WHERE loc.organization_id = o.id AND loc.type = 'local'
);

-- 3. Add engine_type and storage_target_id columns to backup_plans
ALTER TABLE backup_plans ADD COLUMN IF NOT EXISTS engine_type VARCHAR(50) NULL;
ALTER TABLE backup_plans ADD COLUMN IF NOT EXISTS storage_target_id UUID NULL;

-- 4. Backfill historical and existing backup_plans from the organization's active default local target
UPDATE backup_plans bp
SET
    engine_type = 'direct_stream',
    storage_target_id = st.id
FROM storage_targets st
WHERE st.organization_id = bp.organization_id
  AND st.is_default = true
  AND st.type = 'local'
  AND st.status = 'active'
  AND (bp.engine_type IS NULL OR bp.storage_target_id IS NULL);

-- 5. Explicitly validate that no unresolved NULLs remain in backup_plans before setting NOT NULL
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM backup_plans WHERE engine_type IS NULL OR storage_target_id IS NULL) THEN
        RAISE EXCEPTION 'migration 000006 failed: unresolved NULL engine_type or storage_target_id in backup_plans';
    END IF;
END $$;

-- 6. Enforce NOT NULL, check constraint and composite FK on backup_plans
ALTER TABLE backup_plans ALTER COLUMN engine_type SET NOT NULL;
ALTER TABLE backup_plans ALTER COLUMN storage_target_id SET NOT NULL;
ALTER TABLE backup_plans ADD CONSTRAINT chk_backup_plans_engine_type CHECK (engine_type IN ('direct_stream'));
ALTER TABLE backup_plans ADD CONSTRAINT fk_backup_plans_org_storage FOREIGN KEY (organization_id, storage_target_id) REFERENCES storage_targets(organization_id, id) ON DELETE RESTRICT;

-- 7. Add engine_type and storage_target_id columns to backup_jobs
ALTER TABLE backup_jobs ADD COLUMN IF NOT EXISTS engine_type VARCHAR(50) NULL;
ALTER TABLE backup_jobs ADD COLUMN IF NOT EXISTS storage_target_id UUID NULL;

-- 8. Backfill historical and existing backup_jobs from the organization's active default local target
UPDATE backup_jobs bj
SET
    engine_type = 'direct_stream',
    storage_target_id = st.id
FROM storage_targets st
WHERE st.organization_id = bj.organization_id
  AND st.is_default = true
  AND st.type = 'local'
  AND st.status = 'active'
  AND (bj.engine_type IS NULL OR bj.storage_target_id IS NULL);

-- 9. Explicitly validate that no unresolved NULLs remain in backup_jobs before setting NOT NULL
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM backup_jobs WHERE engine_type IS NULL OR storage_target_id IS NULL) THEN
        RAISE EXCEPTION 'migration 000006 failed: unresolved NULL engine_type or storage_target_id in backup_jobs';
    END IF;
END $$;

-- 10. Enforce NOT NULL, check constraint and composite FK on backup_jobs
ALTER TABLE backup_jobs ALTER COLUMN engine_type SET NOT NULL;
ALTER TABLE backup_jobs ALTER COLUMN storage_target_id SET NOT NULL;
ALTER TABLE backup_jobs ADD CONSTRAINT chk_backup_jobs_engine_type CHECK (engine_type IN ('direct_stream'));
ALTER TABLE backup_jobs ADD CONSTRAINT fk_backup_jobs_org_storage FOREIGN KEY (organization_id, storage_target_id) REFERENCES storage_targets(organization_id, id) ON DELETE RESTRICT;

-- 11. Add composite indexes for high-performance organization-scoped storage lookups
CREATE INDEX IF NOT EXISTS idx_backup_plans_org_storage ON backup_plans(organization_id, storage_target_id);
CREATE INDEX IF NOT EXISTS idx_backup_jobs_org_storage ON backup_jobs(organization_id, storage_target_id);
