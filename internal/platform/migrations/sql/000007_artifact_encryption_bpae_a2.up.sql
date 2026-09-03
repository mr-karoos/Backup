-- ==============================================================================
-- 000007_artifact_encryption_bpae_a2.up.sql
-- Direct Stream Artifact Encryption at Rest using BPAE (Future Phase A - Step A.2)
-- Canonical References: ADR-020, ADR-034, docs/DECISIONS.md
-- ==============================================================================

-- 1. Add stored_size_bytes and engine_metadata to backup_artifacts
ALTER TABLE backup_artifacts ADD COLUMN IF NOT EXISTS stored_size_bytes BIGINT NULL;
ALTER TABLE backup_artifacts ADD COLUMN IF NOT EXISTS engine_metadata JSONB NOT NULL DEFAULT '{}'::jsonb;

-- 2. Add CHECK constraints:
-- Stored size must be positive if specified (NULL denotes legacy unencrypted gzip artifact)
ALTER TABLE backup_artifacts DROP CONSTRAINT IF EXISTS chk_backup_artifacts_stored_size;
ALTER TABLE backup_artifacts ADD CONSTRAINT chk_backup_artifacts_stored_size CHECK (
    stored_size_bytes IS NULL OR stored_size_bytes > 0
);

-- When stored_size_bytes denotes an encrypted artifact, engine_metadata must contain a valid 64-character lowercase hex ciphertext_sha256
ALTER TABLE backup_artifacts DROP CONSTRAINT IF EXISTS chk_backup_artifacts_ciphertext_hash;
ALTER TABLE backup_artifacts ADD CONSTRAINT chk_backup_artifacts_ciphertext_hash CHECK (
    stored_size_bytes IS NULL OR (
        (engine_metadata ? 'ciphertext_sha256') AND
        (engine_metadata->>'ciphertext_sha256' ~ '^[0-9a-f]{64}$')
    )
);
