-- ==============================================================================
-- 000005_artifact_lifecycle_audit.down.sql
-- Rollback for Audit Logs (Phase 8 Step 1)
-- ==============================================================================

DROP TABLE IF EXISTS audit_logs;
