-- ==============================================================================
-- 000004_backup_plan_scheduler_integrity.down.sql
-- Rollback for Backup Plans & Scheduler Integrity (Phase 7A)
-- ==============================================================================

DROP INDEX IF EXISTS idx_backup_plans_due_schedule;

ALTER TABLE backup_plans
DROP CONSTRAINT IF EXISTS chk_backup_plans_retention_days_positive;

ALTER TABLE backup_plans
DROP CONSTRAINT IF EXISTS chk_backup_plans_retention_count_positive;

DROP INDEX IF EXISTS uq_backup_jobs_scheduled_pending_plan;
