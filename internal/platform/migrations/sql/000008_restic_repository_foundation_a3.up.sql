-- ==============================================================================
-- 000008_restic_repository_foundation_a3.up.sql
-- Restic Repository Entity, System Credentials & Tenant Isolation (Future Phase A - Step A.3)
-- Canonical References: ADR-031, ADR-032, ADR-033, ADR-034, ADR-035, docs/DECISIONS.md
-- ==============================================================================

-- 1. Add managed_by discriminator and restic_repository_key constraints to credentials table
ALTER TABLE credentials ADD COLUMN IF NOT EXISTS managed_by VARCHAR(20) NOT NULL DEFAULT 'user';

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'chk_credentials_managed_by'
    ) THEN
        ALTER TABLE credentials ADD CONSTRAINT chk_credentials_managed_by CHECK (managed_by IN ('user', 'system'));
    END IF;

    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'chk_credentials_restic_key'
    ) THEN
        ALTER TABLE credentials ADD CONSTRAINT chk_credentials_restic_key CHECK (
            (type = 'restic_repository_key' AND managed_by = 'system') OR
            (type != 'restic_repository_key')
        );
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS idx_credentials_org_managed_by ON credentials(organization_id, managed_by);

-- 2. Create backup_repositories table for dedicated per-resource Restic repositories
CREATE TABLE IF NOT EXISTS backup_repositories (
    id UUID PRIMARY KEY,
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE RESTRICT,
    resource_id UUID NOT NULL,
    storage_target_id UUID NOT NULL,
    credential_id UUID NOT NULL,
    repository_locator TEXT NOT NULL,
    status VARCHAR(30) NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled', 'error')),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_backup_repositories_org_id_id UNIQUE (organization_id, id),
    CONSTRAINT uq_backup_repositories_resource_id UNIQUE (resource_id),
    CONSTRAINT uq_backup_repositories_credential_id UNIQUE (credential_id),
    CONSTRAINT uq_backup_repositories_target_locator UNIQUE (storage_target_id, repository_locator),
    CONSTRAINT fk_backup_repositories_resource FOREIGN KEY (organization_id, resource_id)
        REFERENCES resources(organization_id, id) ON DELETE RESTRICT,
    CONSTRAINT fk_backup_repositories_storage_target FOREIGN KEY (organization_id, storage_target_id)
        REFERENCES storage_targets(organization_id, id) ON DELETE RESTRICT,
    CONSTRAINT fk_backup_repositories_credential FOREIGN KEY (organization_id, credential_id)
        REFERENCES credentials(organization_id, id) ON DELETE RESTRICT
);

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'uq_backup_repositories_credential_id'
    ) THEN
        ALTER TABLE backup_repositories ADD CONSTRAINT uq_backup_repositories_credential_id UNIQUE (credential_id);
    END IF;

    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'uq_backup_repositories_target_locator'
    ) THEN
        ALTER TABLE backup_repositories ADD CONSTRAINT uq_backup_repositories_target_locator UNIQUE (storage_target_id, repository_locator);
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS idx_backup_repositories_org_id ON backup_repositories(organization_id);
CREATE INDEX IF NOT EXISTS idx_backup_repositories_resource_id ON backup_repositories(resource_id);
CREATE INDEX IF NOT EXISTS idx_backup_repositories_storage_target_id ON backup_repositories(storage_target_id);
CREATE INDEX IF NOT EXISTS idx_backup_repositories_credential_id ON backup_repositories(credential_id);
