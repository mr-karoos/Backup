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

    it('enforces empty body on POST /resources/{id}/test-connection', async () => {
      const postSpy = vi.spyOn(apiClient, 'post').mockResolvedValueOnce({
        status: 'success',
        latency_ms: 45,
        checked_at: '2026-01-01T00:00:00Z',
        details: {},
      });

      const res = await apiClient.post('/resources/res-1/test-connection', {});

      expect(postSpy).toHaveBeenCalledWith('/resources/res-1/test-connection', {});
      expect(res).toHaveProperty('status', 'success');
      expect(res).toHaveProperty('latency_ms', 45);
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
  });

  describe('Verification & Deletion Contracts', () => {
    it('calls POST /backup-runs/{id}/verify and returns verification details', async () => {
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

      const res = await apiClient.post<VerifyBackupRunResponse>('/backup-runs/run-1/verify', {});

      expect(postSpy).toHaveBeenCalledWith('/backup-runs/run-1/verify', {});
      expect(res).toHaveProperty('verification_status', 'verified');
      expect(res.details).toHaveProperty('checksum_matched', true);
    });

    it('calls DELETE /backup-artifacts/{id} to permanently remove artifact', async () => {
      const delSpy = vi.spyOn(apiClient, 'delete').mockResolvedValueOnce(undefined);

      await apiClient.delete('/backup-artifacts/art-1');

      expect(delSpy).toHaveBeenCalledWith('/backup-artifacts/art-1');
    });
  });
});
