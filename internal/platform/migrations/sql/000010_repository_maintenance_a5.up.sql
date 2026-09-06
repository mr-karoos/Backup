-- ==============================================================================
-- 000010_repository_maintenance_a5.up.sql
-- Durable Repository Maintenance Queue & Runs (Future Phase A - Step A.5)
-- Canonical References: ADR-031, ADR-032, ADR-033, ADR-034, ADR-035, docs/DECISIONS.md
-- ==============================================================================

-- 1. Create repository_maintenance_jobs table
CREATE TABLE IF NOT EXISTS repository_maintenance_jobs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE RESTRICT,
    repository_id UUID NOT NULL,
    operation_type VARCHAR(50) NOT NULL CHECK (operation_type IN ('restic_forget', 'restic_prune', 'restic_deep_check')),
    status VARCHAR(30) NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'running', 'completed', 'failed', 'cancelled')),
    artifact_id UUID NULL,
    snapshot_id VARCHAR(64) NULL,
    subset_index INTEGER NULL,
    subset_total INTEGER NULL,
    attempt_count INTEGER NOT NULL DEFAULT 0,
    max_attempts INTEGER NOT NULL DEFAULT 3 CHECK (max_attempts >= 1),
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    phase VARCHAR(50) NULL,
    completed_at TIMESTAMPTZ NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_repo_maint_jobs_org_id
        UNIQUE (organization_id, id),
    CONSTRAINT fk_repo_maint_jobs_org_repo
        FOREIGN KEY (organization_id, repository_id)
        REFERENCES backup_repositories(organization_id, id)
        ON DELETE RESTRICT,
    CONSTRAINT fk_repo_maint_jobs_org_artifact
        FOREIGN KEY (organization_id, artifact_id)
        REFERENCES backup_artifacts(organization_id, id)
        ON DELETE RESTRICT,
    CONSTRAINT chk_repo_maint_jobs_operation_fields CHECK (
        (operation_type = 'restic_forget' AND artifact_id IS NOT NULL AND snapshot_id IS NOT NULL AND snapshot_id ~ '^[0-9a-f]{64}$' AND subset_index IS NULL AND subset_total IS NULL) OR
        (operation_type = 'restic_prune' AND artifact_id IS NULL AND snapshot_id IS NULL AND subset_index IS NULL AND subset_total IS NULL) OR
        (operation_type = 'restic_deep_check' AND artifact_id IS NULL AND snapshot_id IS NULL AND subset_index IS NOT NULL AND subset_index >= 1 AND subset_total IS NOT NULL AND subset_total >= subset_index AND subset_total <= 100)
    )
);

-- 2. Indexes for repository_maintenance_jobs
CREATE INDEX IF NOT EXISTS idx_repo_maint_jobs_claim ON repository_maintenance_jobs(status, next_attempt_at) WHERE status = 'pending';
CREATE INDEX IF NOT EXISTS idx_repo_maint_jobs_org_repo ON repository_maintenance_jobs(organization_id, repository_id);
CREATE INDEX IF NOT EXISTS idx_repo_maint_jobs_repo_op_status ON repository_maintenance_jobs(repository_id, operation_type, status);
CREATE INDEX IF NOT EXISTS idx_repo_maint_jobs_artifact_id ON repository_maintenance_jobs(artifact_id) WHERE artifact_id IS NOT NULL;

-- 3. Partial unique indexes for active job deduplication
CREATE UNIQUE INDEX IF NOT EXISTS uq_repo_maint_jobs_active_forget
    ON repository_maintenance_jobs (repository_id, snapshot_id)
    WHERE operation_type = 'restic_forget' AND status IN ('pending', 'running');

CREATE UNIQUE INDEX IF NOT EXISTS uq_repo_maint_jobs_active_prune
    ON repository_maintenance_jobs (repository_id)
    WHERE operation_type = 'restic_prune' AND status IN ('pending', 'running');

CREATE UNIQUE INDEX IF NOT EXISTS uq_repo_maint_jobs_active_deep_check
    ON repository_maintenance_jobs (repository_id, subset_index, subset_total)
    WHERE operation_type = 'restic_deep_check' AND status IN ('pending', 'running');

-- 4. Create repository_maintenance_runs table with composite tenant foreign key
CREATE TABLE IF NOT EXISTS repository_maintenance_runs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE RESTRICT,
    job_id UUID NOT NULL,
    attempt_number INTEGER NOT NULL CHECK (attempt_number >= 1),
    status VARCHAR(30) NOT NULL DEFAULT 'running' CHECK (status IN ('running', 'completed', 'failed', 'cancelled')),
    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ended_at TIMESTAMPTZ NULL,
    heartbeat_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    lease_until TIMESTAMPTZ NOT NULL DEFAULT (NOW() + INTERVAL '2 minutes'),
    error_message TEXT NULL,
    logs_summary JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT fk_repo_maint_runs_org_job
        FOREIGN KEY (organization_id, job_id)
        REFERENCES repository_maintenance_jobs(organization_id, id)
        ON DELETE RESTRICT,
    CONSTRAINT uq_repo_maint_runs_job_attempt UNIQUE (job_id, attempt_number)
);

-- 5. Indexes for repository_maintenance_runs
CREATE INDEX IF NOT EXISTS idx_repo_maint_runs_job_id ON repository_maintenance_runs(job_id);
CREATE INDEX IF NOT EXISTS idx_repo_maint_runs_org_status ON repository_maintenance_runs(organization_id, status);
CREATE INDEX IF NOT EXISTS idx_repo_maint_runs_lease ON repository_maintenance_runs(status, lease_until) WHERE status = 'running';

-- 6. Index for active repository cursor pagination
CREATE INDEX IF NOT EXISTS idx_backup_repositories_active_cursor ON backup_repositories (status, created_at, id) WHERE status = 'active';
