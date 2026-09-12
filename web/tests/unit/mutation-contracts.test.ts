import { describe, it, expect, vi, beforeEach } from 'vitest';
import { apiClient } from '@/lib/api/api-client';
import { ApiError } from '@/types/api';
import type {
  CreateCredentialRequest,
  CreateResourceRequest,
  CreateStorageTargetRequest,
  CreateBackupPlanRequest,
  CreateBackupJobRequest,
  VerifyBackupRunResponse,
} from '@/types/domain';

describe('Mutation Contract Conformance', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  describe('Credential Contracts', () => {
    it('serializes SSH private key credential payload correctly', async () => {
      const postSpy = vi.spyOn(apiClient, 'post').mockResolvedValueOnce({
        id: 'cred-1',
        name: 'SSH Key 1',
        type: 'ssh_private_key',
        fingerprint: 'SHA256:abc',
        key_version: 1,
        created_at: '2026-01-01T00:00:00Z',
      });

      const payload: CreateCredentialRequest = {
        name: 'SSH Key 1',
        type: 'ssh_private_key',
        secret: '-----BEGIN OPENSSH PRIVATE KEY-----\nkey\n-----END OPENSSH PRIVATE KEY-----',
        passphrase: 'secret-passphrase',
      };

      const res = await apiClient.post('/credentials', payload);

      expect(postSpy).toHaveBeenCalledWith('/credentials', payload);
      expect(res).toHaveProperty('id', 'cred-1');
      expect(res).toHaveProperty('key_version', 1);
    });

    it('serializes S3 credential payload correctly', async () => {
      const postSpy = vi.spyOn(apiClient, 'post').mockResolvedValueOnce({
        id: 'cred-2',
        name: 'S3 Keys',
        type: 's3_credentials',
        fingerprint: 'AKIA...',
        key_version: 1,
        created_at: '2026-01-01T00:00:00Z',
      });

      const payload: CreateCredentialRequest = {
        name: 'S3 Keys',
        type: 's3_credentials',
        access_key_id: 'AKIAIOSFODNN7EXAMPLE',
        secret_access_key: 'wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY',
      };

      const res = await apiClient.post('/credentials', payload);

      expect(postSpy).toHaveBeenCalledWith('/credentials', payload);
      expect(res).toHaveProperty('type', 's3_credentials');
    });

    it('handles 409 CREDENTIAL_IN_USE conflict error cleanly', async () => {
      vi.spyOn(apiClient, 'delete').mockRejectedValueOnce(
        new ApiError(409, 'CREDENTIAL_IN_USE', 'credential is currently in use')
      );

      await expect(apiClient.delete('/credentials/cred-in-use')).rejects.toThrow(
        'credential is currently in use'
      );
    });
  });

  describe('Resource Contracts', () => {
    it('serializes Ubuntu SSH resource payload correctly', async () => {
      const postSpy = vi.spyOn(apiClient, 'post').mockResolvedValueOnce({
        id: 'res-1',
        name: 'Ubuntu 01',
        type: 'ubuntu_ssh',
        status: 'active',
        created_at: '2026-01-01T00:00:00Z',
      });

      const payload: CreateResourceRequest = {
        name: 'Ubuntu 01',
        type: 'ubuntu_ssh',
        connector: {
          host: '10.0.0.1',
          port: 22,
          auth_type: 'ssh_key',
          username: 'ubuntu',
          credential_id: 'cred-1',
          config: {
            connection_timeout_seconds: 15,
          },
        },
      };

      const res = await apiClient.post('/resources', payload);

      expect(postSpy).toHaveBeenCalledWith('/resources', payload);
      expect(res).toHaveProperty('name', 'Ubuntu 01');
    });

    it('enforces bodyless POST on /resources/{id}/test-connection', async () => {
      const postSpy = vi.spyOn(apiClient, 'post').mockResolvedValueOnce({
        status: 'success',
        latency_ms: 45,
        checked_at: '2026-01-01T00:00:00Z',
        details: {},
      });

      const res = await apiClient.post('/resources/res-1/test-connection', undefined);

      expect(postSpy).toHaveBeenCalledWith('/resources/res-1/test-connection', undefined);
      expect(res).toHaveProperty('status', 'success');
      expect(res).toHaveProperty('latency_ms', 45);
    });

    it('rejects with 400 if body is unexpectedly provided to test-connection', async () => {
      vi.spyOn(apiClient, 'post').mockRejectedValueOnce(
        new ApiError(400, 'INVALID_BODY', 'request body must be empty for test-connection')
      );

      await expect(
        apiClient.post('/resources/res-1/test-connection', { unexpected: true })
      ).rejects.toThrow('request body must be empty for test-connection');
    });
  });


  describe('Storage Target Contracts', () => {
    it('serializes S3 storage target creation payload correctly', async () => {
      const postSpy = vi.spyOn(apiClient, 'post').mockResolvedValueOnce({
        id: 'st-1',
        name: 'AWS S3 Bucket',
        type: 's3',
        status: 'active',
        is_default: false,
        created_at: '2026-01-01T00:00:00Z',
      });

      const payload: CreateStorageTargetRequest = {
        name: 'AWS S3 Bucket',
        type: 's3',
        s3_config: {
          bucket: 'production-backups',
          endpoint: '',
          region: 'us-east-1',
          force_path_style: false,
        },
        credential_id: 'cred-2',
      };

      const res = await apiClient.post('/storage-targets', payload);

      expect(postSpy).toHaveBeenCalledWith('/storage-targets', payload);
      expect(res).toHaveProperty('name', 'AWS S3 Bucket');
    });
  });

  describe('Backup Plan & Job Execution Contracts', () => {
    it('serializes Backup Plan creation with schedule & retention correctly', async () => {
      const postSpy = vi.spyOn(apiClient, 'post').mockResolvedValueOnce({
        id: 'plan-1',
        name: 'Daily MySQL',
        resource_id: 'res-1',
        engine_type: 'direct_stream',
        storage_target_id: 'st-1',
        status: 'active',
        created_at: '2026-01-01T00:00:00Z',
      });

      const payload: CreateBackupPlanRequest = {
        name: 'Daily MySQL',
        resource_id: 'res-1',
        backup_type: 'mysql_database',
        engine_type: 'direct_stream',
        storage_target_id: 'st-1',
        database_selection: {
          mode: 'all',
        },
        schedule: {
          is_enabled: true,
          cron_expression: '0 2 * * *',
          timezone: 'UTC',
        },
        retention_policy: {
          keep_last_n: 7,
          keep_days: 30,
        },
      };

      const res = await apiClient.post('/backup-plans', payload);

      expect(postSpy).toHaveBeenCalledWith('/backup-plans', payload);
      expect(res).toHaveProperty('id', 'plan-1');
    });

    it('serializes Backup Plan creation without explicit storage_target_id (omitted for auto-provisioning)', async () => {
      const postSpy = vi.spyOn(apiClient, 'post').mockResolvedValueOnce({
        id: 'plan-auto-1',
        name: 'Auto Storage Plan',
        resource_id: 'res-1',
        engine_type: 'direct_stream',
        storage_target_id: 'default-local-target-uuid',
        status: 'active',
        created_at: '2026-01-01T00:00:00Z',
      });

      const payload: CreateBackupPlanRequest = {
        name: 'Auto Storage Plan',
        resource_id: 'res-1',
        backup_type: 'mysql_database',
        engine_type: 'direct_stream',
        database_selection: {
          mode: 'all',
        },
        schedule: {
          is_enabled: false,
          timezone: 'UTC',
        },
      };

      const res = await apiClient.post('/backup-plans', payload);

      expect(postSpy).toHaveBeenCalledWith('/backup-plans', payload);
      expect(payload).not.toHaveProperty('storage_target_id');
      expect(res).toHaveProperty('id', 'plan-auto-1');
    });

    it('serializes Backup Job execution request correctly and expects 202 status response', async () => {
      const postSpy = vi.spyOn(apiClient, 'post').mockResolvedValueOnce({
        id: 'job-1',
        resource_id: 'res-1',
        backup_plan_id: 'plan-1',
        backup_type: 'mysql_database',
        engine_type: 'direct_stream',
        storage_target_id: 'st-1',
        target_spec: {},
        status: 'pending',
        trigger_type: 'manual',
        created_at: '2026-01-01T00:00:00Z',
      });

      const payload: CreateBackupJobRequest = {
        backup_plan_id: 'plan-1',
      };

      const res = await apiClient.post('/backup-jobs', payload);

      expect(postSpy).toHaveBeenCalledWith('/backup-jobs', payload);
      expect(res).toHaveProperty('id', 'job-1');
      expect(res).toHaveProperty('status', 'pending');
    });

    it('serializes ad-hoc Backup Job execution omitting storage_target_id when default local storage is used', async () => {
      const postSpy = vi.spyOn(apiClient, 'post').mockResolvedValueOnce({
        id: 'job-adhoc-auto',
        resource_id: 'res-1',
        backup_type: 'mysql_database',
        engine_type: 'direct_stream',
        storage_target_id: 'default-local-uuid',
        target_spec: { databases: ['prod_db'] },
        status: 'pending',
        trigger_type: 'manual',
        created_at: '2026-01-01T00:00:00Z',
      });

      const payload: CreateBackupJobRequest = {
        resource_id: 'res-1',
        backup_type: 'mysql_database',
        engine_type: 'direct_stream',
        target_spec: { databases: ['prod_db'] },
      };

      const res = await apiClient.post('/backup-jobs', payload);

      expect(postSpy).toHaveBeenCalledWith('/backup-jobs', payload);
      expect(payload).not.toHaveProperty('storage_target_id');
      expect(res).toHaveProperty('id', 'job-adhoc-auto');
    });

    it('serializes ad-hoc Backup Job execution with explicit storage_target_id', async () => {
      const postSpy = vi.spyOn(apiClient, 'post').mockResolvedValueOnce({
        id: 'job-adhoc-explicit',
        resource_id: 'res-1',
        backup_type: 'mysql_database',
        engine_type: 'direct_stream',
        storage_target_id: 'st-explicit-uuid',
        target_spec: { databases: ['prod_db'] },
        status: 'pending',
        trigger_type: 'manual',
        created_at: '2026-01-01T00:00:00Z',
      });

      const payload: CreateBackupJobRequest = {
        resource_id: 'res-1',
        backup_type: 'mysql_database',
        engine_type: 'direct_stream',
        storage_target_id: 'st-explicit-uuid',
        target_spec: { databases: ['prod_db'] },
      };

      const res = await apiClient.post('/backup-jobs', payload);

      expect(postSpy).toHaveBeenCalledWith('/backup-jobs', payload);
      expect(payload.storage_target_id).toBe('st-explicit-uuid');
      expect(res).toHaveProperty('id', 'job-adhoc-explicit');
    });
  });

  describe('Verification & Deletion Contracts', () => {
    it('calls POST /backup-runs/{id}/verify with undefined body and returns verification details', async () => {
      const postSpy = vi.spyOn(apiClient, 'post').mockResolvedValueOnce({
        run_id: 'run-1',
        verification_status: 'verified',
        verified_at: '2026-01-01T00:00:00Z',
        details: {
          checksum_matched: true,
          archive_integrity: 'tar valid',
          compression_valid: true,
          extracted_sample_check: 'header verified',
        },
      });

      const res = await apiClient.post<VerifyBackupRunResponse>('/backup-runs/run-1/verify', undefined);

      expect(postSpy).toHaveBeenCalledWith('/backup-runs/run-1/verify', undefined);
      expect(res).toHaveProperty('verification_status', 'verified');
      expect(res.details).toHaveProperty('checksum_matched', true);
    });

    it('rejects with 400 if body is unexpectedly provided to verify endpoint', async () => {
      vi.spyOn(apiClient, 'post').mockRejectedValueOnce(
        new ApiError(400, 'INVALID_BODY', 'request body must be empty for verify')
      );

      await expect(
        apiClient.post('/backup-runs/run-1/verify', { payload: 'invalid' })
      ).rejects.toThrow('request body must be empty for verify');
    });

    it('calls DELETE /backup-artifacts/{id} to permanently remove artifact', async () => {
      const delSpy = vi.spyOn(apiClient, 'delete').mockResolvedValueOnce(undefined);

      await apiClient.delete('/backup-artifacts/art-1');

      expect(delSpy).toHaveBeenCalledWith('/backup-artifacts/art-1');
    });
  });
});

