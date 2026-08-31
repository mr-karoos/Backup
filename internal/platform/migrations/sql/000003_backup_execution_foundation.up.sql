-- ==============================================================================
-- 000003_backup_execution_foundation.up.sql
-- Schema for Backup Execution Foundation (Phase 5)
-- Canonical References: docs/DATA_MODEL.md (Entities 7, 8, 9, 10, 11),
-- ADR-010, ADR-011, ADR-012, ADR-014, ADR-018, ADR-021
-- ==============================================================================

-- 1. Backup Plans Table (Schedule & Retention policies per resource)
CREATE TABLE IF NOT EXISTS backup_plans (
    id UUID PRIMARY KEY,
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE RESTRICT,
    resource_id UUID NOT NULL,
    name VARCHAR(100) NOT NULL,
    backup_type VARCHAR(50) NOT NULL CHECK (backup_type IN ('mysql_database', 'website_files', 'both')),
    target_spec JSONB NOT NULL,
    schedule_cron VARCHAR(100) NULL,
    schedule_timezone VARCHAR(50) NOT NULL DEFAULT 'UTC',
    is_schedule_enabled BOOLEAN NOT NULL DEFAULT true,
    retention_count INTEGER NULL,
    retention_days INTEGER NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'paused', 'archived')),
    next_run_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT fk_backup_plans_org_resource FOREIGN KEY (organization_id, resource_id) REFERENCES resources(organization_id, id) ON DELETE CASCADE,
    CONSTRAINT chk_backup_plans_schedule_cron_required CHECK (
        is_schedule_enabled = false
        OR (
            schedule_cron IS NOT NULL
            AND BTRIM(schedule_cron) <> ''
        )
    ),
    CONSTRAINT uq_backup_plans_org_id_id UNIQUE (organization_id, id)
);

CREATE INDEX IF NOT EXISTS idx_backup_plans_org_resource ON backup_plans(organization_id, resource_id);
CREATE INDEX IF NOT EXISTS idx_backup_plans_status ON backup_plans(status);

-- 2. Storage Targets Table (Physical / Cloud storage destinations)
CREATE TABLE IF NOT EXISTS storage_targets (
    id UUID PRIMARY KEY,
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE RESTRICT,
    name VARCHAR(100) NOT NULL,
    type VARCHAR(50) NOT NULL CHECK (type IN ('local', 's3', 's3_compatible', 'remote_ssh')),
    status VARCHAR(20) NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled', 'error')),
    is_default BOOLEAN NOT NULL DEFAULT false,
    credential_id UUID NULL,
    config JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT fk_storage_targets_org_credential FOREIGN KEY (organization_id, credential_id) REFERENCES credentials(organization_id, id) ON DELETE RESTRICT,
    CONSTRAINT uq_storage_targets_org_id_id UNIQUE (organization_id, id)
);

CREATE INDEX IF NOT EXISTS idx_storage_targets_org_id ON storage_targets(organization_id);
CREATE UNIQUE INDEX IF NOT EXISTS uq_storage_targets_default_per_org ON storage_targets (organization_id) WHERE is_default = true;

-- 3. Backup Jobs Table (Logical backup requests / durable queue)
CREATE TABLE IF NOT EXISTS backup_jobs (
    id UUID PRIMARY KEY,
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE RESTRICT,
    resource_id UUID NOT NULL,
    backup_plan_id UUID NULL,
    trigger_type VARCHAR(30) NOT NULL CHECK (trigger_type IN ('manual', 'scheduled')),
    created_by_user_id UUID NULL REFERENCES users(id) ON DELETE SET NULL,
    backup_type VARCHAR(50) NOT NULL CHECK (backup_type IN ('mysql_database', 'website_files', 'both')),
    target_spec JSONB NOT NULL,
    status VARCHAR(30) NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'running', 'completed', 'failed', 'cancelled')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT fk_backup_jobs_org_resource FOREIGN KEY (organization_id, resource_id) REFERENCES resources(organization_id, id) ON DELETE RESTRICT,
    CONSTRAINT fk_backup_jobs_org_plan FOREIGN KEY (organization_id, backup_plan_id) REFERENCES backup_plans(organization_id, id) ON DELETE SET NULL (backup_plan_id),
    CONSTRAINT uq_backup_jobs_org_id_id UNIQUE (organization_id, id)
);

CREATE INDEX IF NOT EXISTS idx_backup_jobs_status_created_at ON backup_jobs(status, created_at ASC);
CREATE INDEX IF NOT EXISTS idx_backup_jobs_org_resource ON backup_jobs(organization_id, resource_id);
CREATE UNIQUE INDEX IF NOT EXISTS uq_backup_jobs_manual_active_resource ON backup_jobs (organization_id, resource_id) WHERE trigger_type = 'manual' AND status IN ('pending', 'running');

-- 4. Backup Runs Table (Physical execution attempts per job)
CREATE TABLE IF NOT EXISTS backup_runs (
    id UUID PRIMARY KEY,
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE RESTRICT,
    job_id UUID NOT NULL,
    attempt_number INTEGER NOT NULL CHECK (attempt_number >= 1),
    status VARCHAR(30) NOT NULL DEFAULT 'running' CHECK (status IN ('pending', 'running', 'success', 'failed', 'cancelled')),
    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ended_at TIMESTAMPTZ NULL,
    heartbeat_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    lease_until TIMESTAMPTZ NOT NULL DEFAULT (NOW() + INTERVAL '2 minutes'),
    error_message TEXT NULL,
    logs_summary JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT fk_backup_runs_org_job FOREIGN KEY (organization_id, job_id) REFERENCES backup_jobs(organization_id, id) ON DELETE RESTRICT,
    CONSTRAINT uq_backup_runs_org_id_id UNIQUE (organization_id, id),
    CONSTRAINT uq_backup_runs_job_attempt UNIQUE (job_id, attempt_number)
);

CREATE INDEX IF NOT EXISTS idx_backup_runs_job_id ON backup_runs(job_id);
CREATE INDEX IF NOT EXISTS idx_backup_runs_status_lease_until ON backup_runs(status, lease_until);

-- 5. Backup Artifacts Table (Physical backup output files & metadata)
CREATE TABLE IF NOT EXISTS backup_artifacts (
    id UUID PRIMARY KEY,
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE RESTRICT,
    run_id UUID NOT NULL,
    resource_id UUID NOT NULL,
    storage_target_id UUID NOT NULL,
    artifact_type VARCHAR(50) NOT NULL CHECK (artifact_type IN ('database_dump', 'files_archive')),
    format VARCHAR(30) NOT NULL CHECK (format IN ('sql_gzip', 'tar_gzip')),
    target_name VARCHAR(255) NOT NULL,
    storage_reference VARCHAR(500) NOT NULL,
    size_bytes BIGINT NOT NULL CHECK (size_bytes > 0),
    checksum_algorithm VARCHAR(30) NOT NULL DEFAULT 'sha256',
    checksum_hash VARCHAR(128) NOT NULL,
    verification_status VARCHAR(30) NOT NULL DEFAULT 'unverified' CHECK (verification_status IN ('unverified', 'verified', 'failed')),
    verified_at TIMESTAMPTZ NULL,
    verification_details TEXT NULL,
    is_deleted BOOLEAN NOT NULL DEFAULT false,
    deleted_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT fk_backup_artifacts_org_run FOREIGN KEY (organization_id, run_id) REFERENCES backup_runs(organization_id, id) ON DELETE RESTRICT,
    CONSTRAINT fk_backup_artifacts_org_resource FOREIGN KEY (organization_id, resource_id) REFERENCES resources(organization_id, id) ON DELETE RESTRICT,
    CONSTRAINT fk_backup_artifacts_org_storage FOREIGN KEY (organization_id, storage_target_id) REFERENCES storage_targets(organization_id, id) ON DELETE RESTRICT,
    CONSTRAINT chk_backup_artifacts_checksum_algorithm CHECK (checksum_algorithm = 'sha256'),
    CONSTRAINT chk_backup_artifacts_checksum_hash_format CHECK (checksum_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT uq_backup_artifacts_org_id_id UNIQUE (organization_id, id)
);

CREATE INDEX IF NOT EXISTS idx_backup_artifacts_run_id ON backup_artifacts(run_id);
CREATE INDEX IF NOT EXISTS idx_backup_artifacts_resource_id ON backup_artifacts(resource_id);
CREATE INDEX IF NOT EXISTS idx_backup_artifacts_org_id ON backup_artifacts(organization_id);
