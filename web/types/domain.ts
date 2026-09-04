/**
 * Domain entity types matching internal/resource, internal/backup, internal/credential contracts.
 */

// -------------------------------------------------------------
// Resources & Connectors
// -------------------------------------------------------------
export type ResourceType = 'ubuntu_ssh' | 'cpanel';
export type ResourceStatus = 'active' | 'unreachable' | 'disabled' | 'error' | 'archived';

export interface ConnectorResponse {
  host?: string;
  port?: number;
  auth_type?: string;
  username?: string;
  host_key_fingerprint?: string | null;
  credential_id?: string;
  credential_name?: string;
}

export interface ResourceResponse {
  id: string;
  name: string;
  type: ResourceType;
  status: ResourceStatus;
  last_connection_test_at?: string | null;
  last_connection_status?: string | null;
  connector?: ConnectorResponse | null;
  created_at: string;
}

// -------------------------------------------------------------
// Backup Plans
// -------------------------------------------------------------
export type BackupType = 'mysql_database' | 'website_files' | 'both';
export type EngineType = 'direct_stream';
export type PlanStatus = 'active' | 'paused' | 'archived';

export interface DatabaseSelectionDTO {
  mode: 'all' | 'selected';
  databases?: string[];
}

export interface FileSelectionDTO {
  paths: string[];
  exclude_patterns?: string[];
}

export interface ScheduleDTO {
  is_enabled: boolean;
  cron_expression?: string | null;
  timezone: string;
  next_run_at?: string | null;
}

export interface RetentionPolicyDTO {
  keep_last_n?: number | null;
  keep_days?: number | null;
}

export interface BackupPlanResponse {
  id: string;
  resource_id: string;
  resource_name: string;
  name: string;
  backup_type: BackupType;
  engine_type: EngineType;
  storage_target_id: string;
  status: PlanStatus;
  database_selection?: DatabaseSelectionDTO | null;
  file_selection?: FileSelectionDTO | null;
  schedule: ScheduleDTO;
  retention_policy?: RetentionPolicyDTO | null;
  created_at: string;
  updated_at?: string | null;
}

// -------------------------------------------------------------
// Backup Runs
// -------------------------------------------------------------
export type RunStatus = 'pending' | 'running' | 'success' | 'failed' | 'cancelled';

export interface BackupRunResponse {
  id: string;
  job_id: string;
  resource_id: string;
  attempt_number: number;
  status: RunStatus;
  started_at: string;
  ended_at?: string | null;
  duration_seconds?: number | null;
  total_artifact_size_bytes: number;
  error_message?: string | null;
  artifacts_count: number;
  created_at: string;
}

// -------------------------------------------------------------
// Backup Artifacts
// -------------------------------------------------------------
export type VerificationStatus = 'unverified' | 'verified' | 'failed';

export interface BackupArtifactResponse {
  id: string;
  run_id: string;
  resource_id: string;
  artifact_name: string;
  size_bytes: number;
  checksum_sha256: string;
  compression_type: string;
  verification_status: VerificationStatus;
  verified_at?: string | null;
  created_at: string;
}

// -------------------------------------------------------------
// Storage Targets
// -------------------------------------------------------------
export type StorageTargetType = 'local' | 's3' | 's3_compatible';
export type StorageTargetStatus = 'active' | 'disabled' | 'error' | 'archived';

export interface S3TargetConfigDTO {
  bucket: string;
  endpoint: string;
  region: string;
  force_path_style: boolean;
}

export interface StorageTargetResponse {
  id: string;
  name: string;
  type: StorageTargetType;
  status: StorageTargetStatus;
  is_default: boolean;
  s3_config?: S3TargetConfigDTO | null;
  credential_id?: string | null;
  created_at: string;
  updated_at: string;
}

// -------------------------------------------------------------
// Credentials (Read-Only Admin Metadata)
// -------------------------------------------------------------
export type CredentialType =
  | 'ssh_private_key'
  | 'ssh_password'
  | 'cpanel_api_token'
  | 'cpanel_password'
  | 's3_credentials';

export interface CredentialListItemResponse {
  id: string;
  name: string;
  type: CredentialType;
  fingerprint?: string | null;
  key_version: number;
  created_at: string;
}

// -------------------------------------------------------------
// System Health
// -------------------------------------------------------------
export interface HealthResponse {
  status: 'ok' | 'unavailable';
}
