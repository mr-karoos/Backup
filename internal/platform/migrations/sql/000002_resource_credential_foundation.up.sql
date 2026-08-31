-- ==============================================================================
-- 000002_resource_credential_foundation.up.sql
-- Schema for External Credentials, Resources & Connectors (Phase 3A)
-- Canonical References: docs/DATA_MODEL.md (Entities 4, 5, 6), ADR-008, ADR-009
-- ==============================================================================

-- 1. Credentials Table (Encrypted external access credentials)
CREATE TABLE IF NOT EXISTS credentials (
    id UUID PRIMARY KEY,
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE RESTRICT,
    name VARCHAR(100) NOT NULL,
    type VARCHAR(50) NOT NULL,
    encrypted_secret BYTEA NOT NULL,
    nonce BYTEA NOT NULL,
    auth_tag BYTEA NOT NULL,
    key_version INTEGER NOT NULL CHECK (key_version >= 1),
    fingerprint VARCHAR(255) NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_credentials_org_id_id UNIQUE (organization_id, id)
);

CREATE INDEX IF NOT EXISTS idx_credentials_organization_id ON credentials(organization_id);
CREATE INDEX IF NOT EXISTS idx_credentials_org_type ON credentials(organization_id, type);

-- 2. Resources Table (Target servers, hosts, and external environments)
CREATE TABLE IF NOT EXISTS resources (
    id UUID PRIMARY KEY,
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE RESTRICT,
    name VARCHAR(100) NOT NULL,
    type VARCHAR(50) NOT NULL CHECK (type IN ('ubuntu_ssh', 'cpanel')),
    status VARCHAR(30) NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'unreachable', 'disabled', 'error', 'archived')),
    last_connection_test_at TIMESTAMPTZ NULL,
    last_connection_status VARCHAR(30) NULL CHECK (last_connection_status IS NULL OR last_connection_status IN ('success', 'failed')),
    last_connection_error TEXT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_resources_org_id_id UNIQUE (organization_id, id)
);

CREATE INDEX IF NOT EXISTS idx_resources_organization_id ON resources(organization_id);
CREATE INDEX IF NOT EXISTS idx_resources_org_status ON resources(organization_id, status);
CREATE INDEX IF NOT EXISTS idx_resources_org_type ON resources(organization_id, type);

-- 3. Resource Connectors Table (1:1 connection adapter configuration per resource)
CREATE TABLE IF NOT EXISTS resource_connectors (
    id UUID PRIMARY KEY,
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE RESTRICT,
    resource_id UUID NOT NULL,
    connector_type VARCHAR(50) NOT NULL CHECK (connector_type IN ('ubuntu_ssh', 'cpanel')),
    credential_id UUID NOT NULL,
    host VARCHAR(255) NOT NULL,
    port INTEGER NOT NULL CHECK (port >= 1 AND port <= 65535),
    auth_type VARCHAR(50) NOT NULL CHECK (auth_type IN ('ssh_key', 'ssh_password', 'cpanel_api_token', 'cpanel_password')),
    host_key_fingerprint VARCHAR(255) NULL,
    config JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_resource_connectors_resource_id UNIQUE (resource_id),
    CONSTRAINT fk_resource_connectors_org_resource FOREIGN KEY (organization_id, resource_id) REFERENCES resources(organization_id, id) ON DELETE CASCADE,
    CONSTRAINT fk_resource_connectors_org_credential FOREIGN KEY (organization_id, credential_id) REFERENCES credentials(organization_id, id) ON DELETE RESTRICT
);

CREATE INDEX IF NOT EXISTS idx_resource_connectors_organization_id ON resource_connectors(organization_id);
CREATE INDEX IF NOT EXISTS idx_resource_connectors_credential_id ON resource_connectors(credential_id);
