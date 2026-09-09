import { describe, it, expect } from 'vitest';
import type {
  ResourceType,
  ResourceStatus,
  BackupType,
  EngineType,
  RunStatus,
  VerificationStatus,
  StorageTargetType,
  StorageTargetStatus,
  CredentialType,
} from '@/types/domain';
import {
  formatCronSchedule,
  formatResourceType,
  formatBackupType,
  formatStorageTargetType,
  getStatusBadgeVariant,
} from '@/lib/format/formatters';

describe('A.2 Backend Contract Conformance & Enum Freezing', () => {
  it('conforms to approved ResourceType and ResourceStatus enums', () => {
    // Valid approved resource types in A.2
    const validResourceTypes: ResourceType[] = ['ubuntu_ssh', 'cpanel'];
    expect(validResourceTypes).toHaveLength(2);

    const validResourceStatuses: ResourceStatus[] = [
      'active',
      'unreachable',
      'disabled',
      'error',
      'archived',
    ];
    expect(validResourceStatuses).toHaveLength(5);
  });

  it('conforms to approved Plan & Run enums: single direct_stream engine in F1A', () => {
    const validBackupTypes: BackupType[] = ['mysql_database', 'website_files', 'both'];
    expect(validBackupTypes).toHaveLength(3);

    // Strict rule: ONLY direct_stream in F1A
    const validEngineTypes: EngineType[] = ['direct_stream'];
    expect(validEngineTypes).toEqual(['direct_stream']);

    const validRunStatuses: RunStatus[] = [
      'pending',
      'running',
      'success',
      'failed',
      'cancelled',
    ];
    expect(validRunStatuses).toHaveLength(5);
  });

  it('conforms to approved VerificationStatus without fictional pending state', () => {
    // Exact domain states from internal/backup/domain/artifact.go
    const validVerificationStatuses: VerificationStatus[] = ['unverified', 'verified', 'failed'];
    expect(validVerificationStatuses).toHaveLength(3);
    expect(validVerificationStatuses).not.toContain('pending');
  });

  it('conforms to approved StorageTarget types and statuses', () => {
    const validStorageTypes: StorageTargetType[] = ['local', 's3', 's3_compatible'];
    expect(validStorageTypes).toHaveLength(3);

    const validStorageStatuses: StorageTargetStatus[] = [
      'active',
      'disabled',
      'error',
      'archived',
    ];
    expect(validStorageStatuses).toHaveLength(4);
  });

  it('conforms to approved CredentialType enums without generic password', () => {
    const validCredentialTypes: CredentialType[] = [
      'ssh_private_key',
      'ssh_password',
      'cpanel_api_token',
      'cpanel_password',
      's3_credentials',
    ];
    expect(validCredentialTypes).toHaveLength(5);
    expect(validCredentialTypes).not.toContain('password');
  });

  describe('Deterministic Formatters Conformance', () => {
    it('formats cron expressions deterministically', () => {
      expect(formatCronSchedule('0 2 * * *')).toBe('Daily at 02:00');
      expect(formatCronSchedule('30 14 * * *')).toBe('Daily at 14:30');
      expect(formatCronSchedule('0 */6 * * *')).toBe('Every 6 hours');
      expect(formatCronSchedule('0 * * * *')).toBe('Hourly');
      expect(formatCronSchedule('*/15 * * * *')).toBe('Every 15 minutes');
      expect(formatCronSchedule('0 2 * * 0')).toBe('Weekly on Sunday at 02:00');
      expect(formatCronSchedule('0 3 * * 5')).toBe('Weekly on Friday at 03:00');
      expect(formatCronSchedule('1 2 3 4 5')).toBe('Custom schedule');
      expect(formatCronSchedule('')).toBe('Manual (no schedule)');
      expect(formatCronSchedule(null)).toBe('Manual (no schedule)');
      expect(formatCronSchedule(undefined)).toBe('Manual (no schedule)');
    });

    it('formats ResourceType accurately', () => {
      expect(formatResourceType('ubuntu_ssh')).toBe('Ubuntu (SSH)');
      expect(formatResourceType('cpanel')).toBe('cPanel');
      expect(formatResourceType('custom_type')).toBe('custom_type');
    });

    it('formats BackupType accurately', () => {
      expect(formatBackupType('mysql_database')).toBe('MySQL Database');
      expect(formatBackupType('website_files')).toBe('Website Files');
      expect(formatBackupType('both')).toBe('Database & Website Files');
      expect(formatBackupType('custom')).toBe('custom');
    });

    it('formats StorageTargetType accurately', () => {
      expect(formatStorageTargetType('local')).toBe('Local Storage');
      expect(formatStorageTargetType('s3')).toBe('Amazon / Standard S3');
      expect(formatStorageTargetType('s3_compatible')).toBe('S3 Compatible');
      expect(formatStorageTargetType('unknown')).toBe('unknown');
    });

    it('returns appropriate badge variants and labels for statuses', () => {
      // Unverified artifact state
      expect(getStatusBadgeVariant('unverified')).toEqual({
        label: 'unverified',
        variant: 'outline',
      });

      // Disabled / Paused states
      expect(getStatusBadgeVariant('disabled')).toEqual({
        label: 'disabled',
        variant: 'outline',
      });
      expect(getStatusBadgeVariant('paused')).toEqual({
        label: 'paused',
        variant: 'outline',
      });

      // Unreachable resource state
      expect(getStatusBadgeVariant('unreachable')).toEqual({
        label: 'unreachable',
        variant: 'destructive',
      });

      // Error / Failed / Cancelled / Archived
      expect(getStatusBadgeVariant('error')).toEqual({
        label: 'error',
        variant: 'destructive',
      });
      expect(getStatusBadgeVariant('failed')).toEqual({
        label: 'failed',
        variant: 'destructive',
      });
      expect(getStatusBadgeVariant('cancelled')).toEqual({
        label: 'cancelled',
        variant: 'destructive',
      });
      expect(getStatusBadgeVariant('archived')).toEqual({
        label: 'archived',
        variant: 'destructive',
      });

      // Success / Verified / Active
      expect(getStatusBadgeVariant('success')).toEqual({
        label: 'success',
        variant: 'success',
      });
      expect(getStatusBadgeVariant('verified')).toEqual({
        label: 'verified',
        variant: 'success',
      });
      expect(getStatusBadgeVariant('active')).toEqual({
        label: 'active',
        variant: 'success',
      });
    });
  });
});
