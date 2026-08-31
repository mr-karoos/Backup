-- ==============================================================================
-- 000004_backup_plan_scheduler_integrity.up.sql
-- Schema for Backup Plans & Scheduler Integrity (Phase 7A)
-- Canonical References: docs/DATA_MODEL.md (Entities 7, 8), ADR-016
-- ==============================================================================

-- 1. Unique partial index to prevent duplicate pending scheduled jobs per plan
CREATE UNIQUE INDEX IF NOT EXISTS uq_backup_jobs_scheduled_pending_plan
ON backup_jobs (organization_id, backup_plan_id)
WHERE trigger_type = 'scheduled'
  AND status = 'pending'
  AND backup_plan_id IS NOT NULL;

-- 2. Check constraints for strictly positive retention policy values
ALTER TABLE backup_plans
ADD CONSTRAINT chk_backup_plans_retention_count_positive
CHECK (retention_count IS NULL OR retention_count > 0);

ALTER TABLE backup_plans
ADD CONSTRAINT chk_backup_plans_retention_days_positive
CHECK (retention_days IS NULL OR retention_days > 0);

-- 3. Composite partial index for high-performance due-plan scheduler evaluation
CREATE INDEX IF NOT EXISTS idx_backup_plans_due_schedule
ON backup_plans (next_run_at ASC, id ASC)
WHERE status = 'active'
  AND is_schedule_enabled = true
  AND next_run_at IS NOT NULL;
