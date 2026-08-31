-- ==============================================================================
-- 000001_identity_foundation.down.sql
-- Reverse Schema for Identity, Multi-Tenancy & User Sessions (Phase 1B)
-- ==============================================================================

DROP TABLE IF EXISTS organization_members;
DROP TABLE IF EXISTS user_sessions;
DROP TABLE IF EXISTS users;
DROP TABLE IF EXISTS organizations;
