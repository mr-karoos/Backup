-- ==============================================================================
-- 000010_repository_maintenance_a5.down.sql
-- Rollback Durable Repository Maintenance Queue & Runs (Future Phase A - Step A.5)
-- Canonical References: ADR-031, ADR-032, ADR-033, ADR-034, ADR-035, docs/DECISIONS.md
-- ==============================================================================

-- 1. Fail-closed check: abort if any maintenance runs or jobs exist
DO $$
DECLARE
    has_runs BOOLEAN := false;
    has_jobs BOOLEAN := false;
BEGIN
    IF to_regclass('public.repository_maintenance_runs') IS NOT NULL THEN
        EXECUTE 'SELECT EXISTS (SELECT 1 FROM repository_maintenance_runs)' INTO has_runs;
        IF has_runs THEN
            RAISE EXCEPTION 'cannot rollback migration 000010: live repository maintenance runs exist';
        END IF;
    END IF;

    IF to_regclass('public.repository_maintenance_jobs') IS NOT NULL THEN
        EXECUTE 'SELECT EXISTS (SELECT 1 FROM repository_maintenance_jobs)' INTO has_jobs;
        IF has_jobs THEN
            RAISE EXCEPTION 'cannot rollback migration 000010: live repository maintenance jobs exist';
        END IF;
    END IF;
END $$;

-- 2. Drop tables in reverse dependency order
DROP TABLE IF EXISTS repository_maintenance_runs;
DROP TABLE IF EXISTS repository_maintenance_jobs;
