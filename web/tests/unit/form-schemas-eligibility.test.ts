import { describe, it, expect } from 'vitest';
import {
  credentialCreateSchema,
  resourceCreateSchema,
  storageCreateSchema,
  backupPlanSchema,
  backupPlanEditSchema,
  organizationEditSchema,
} from '@/lib/forms/schemas';

describe('Form Schemas & Resource Eligibility Validations', () => {
  describe('Storage Target Schema', () => {
    it('accepts valid standard S3 target payload', () => {
      const validS3 = {
        name: 'Production S3',
        type: 's3' as const,
        bucket: 'my-backups',
        region: 'eu-central-1',
        credential_id: 'cred-123',
        force_path_style: false,
      };

      const parsed = storageCreateSchema.safeParse(validS3);
      expect(parsed.success).toBe(true);
    });

    it('rejects s3_compatible type in create schema per F1B contract freeze', () => {
      const s3Compatible = {
        name: 'MinIO Target',
        type: 's3_compatible',
        bucket: 'minio-bucket',
        region: 'us-east-1',
        credential_id: 'cred-123',
      };

      const parsed = storageCreateSchema.safeParse(s3Compatible);
      expect(parsed.success).toBe(false);
      if (!parsed.success) {
        expect(parsed.error.issues[0]?.path).toContain('type');
      }
    });

    it('requires bucket and region for S3 targets', () => {
      const invalid = {
        name: 'Incomplete S3',
        type: 's3' as const,
        bucket: '',
        region: '',
        credential_id: 'cred-123',
      };

      const parsed = storageCreateSchema.safeParse(invalid);
      expect(parsed.success).toBe(false);
      if (!parsed.success) {
        const paths = parsed.error.issues.map((i) => i.path[0]);
        expect(paths).toContain('bucket');
        expect(paths).toContain('region');
      }
    });
  });

  describe('Resource Creation Schema', () => {
    it('accepts valid ubuntu_ssh resource configuration', () => {
      const validUbuntu = {
        name: 'Database Server 01',
        type: 'ubuntu_ssh' as const,
        host: '192.168.1.50',
        port: 22,
        username: 'ubuntu',
        credential_id: 'cred-ssh',
        auth_type: 'ssh_key',
        connection_timeout_seconds: 15,
      };

      const parsed = resourceCreateSchema.safeParse(validUbuntu);
      expect(parsed.success).toBe(true);
    });

    it('rejects invalid port numbers (below 1 or above 65535)', () => {
      const invalidPort = {
        name: 'Invalid Server',
        type: 'ubuntu_ssh' as const,
        host: '10.0.0.1',
        port: 70000,
        username: 'root',
        credential_id: 'cred-1',
        auth_type: 'ssh_key',
      };

      const parsed = resourceCreateSchema.safeParse(invalidPort);
      expect(parsed.success).toBe(false);
      if (!parsed.success) {
        expect(parsed.error.issues[0]?.path).toContain('port');
      }
    });
  });

  describe('Backup Plan Schemas', () => {
    it('enforces 5-field cron expression when schedule is enabled', () => {
      const planWithInvalidCron = {
        name: 'Scheduled MySQL Plan',
        resource_id: 'res-1',
        backup_type: 'mysql_database' as const,
        storage_target_id: 'st-1',
        is_enabled: true,
        cron_expression: 'invalid cron format',
        timezone: 'UTC',
        db_mode: 'all' as const,
        selected_databases: [],
      };

      const parsed = backupPlanSchema.safeParse(planWithInvalidCron);
      expect(parsed.success).toBe(false);
      if (!parsed.success) {
        expect(parsed.error.issues[0]?.path).toContain('cron_expression');
      }

      // With valid 5-field cron expression
      const planWithValidCron = {
        ...planWithInvalidCron,
        cron_expression: '0 2 * * *',
      };
      const validParsed = backupPlanSchema.safeParse(planWithValidCron);
      expect(validParsed.success).toBe(true);
    });

    it('allows empty cron expression when schedule is disabled (is_enabled = false)', () => {
      const manualPlan = {
        name: 'Manual Plan',
        resource_id: 'res-1',
        backup_type: 'mysql_database' as const,
        storage_target_id: 'st-1',
        is_enabled: false,
        cron_expression: '',
        timezone: 'UTC',
        db_mode: 'all' as const,
        selected_databases: [],
      };

      const parsed = backupPlanSchema.safeParse(manualPlan);
      expect(parsed.success).toBe(true);
    });

    it('requires selected databases when db_mode is selected', () => {
      const planMissingDbs = {
        name: 'Specific DB Plan',
        resource_id: 'res-1',
        backup_type: 'mysql_database' as const,
        storage_target_id: 'st-1',
        is_enabled: false,
        timezone: 'UTC',
        db_mode: 'selected' as const,
        selected_databases: [],
        manual_databases: '',
      };

      const parsed = backupPlanSchema.safeParse(planMissingDbs);
      expect(parsed.success).toBe(false);
      if (!parsed.success) {
        expect(parsed.error.issues[0]?.path).toContain('selected_databases');
      }

      // Valid when database is provided
      const validPlan = {
        ...planMissingDbs,
        selected_databases: ['production_db'],
      };
      expect(backupPlanSchema.safeParse(validPlan).success).toBe(true);
    });

    it('requires directory paths for website_files backup type', () => {
      const filesPlanMissingPath = {
        name: 'Files Backup',
        resource_id: 'res-1',
        backup_type: 'website_files' as const,
        storage_target_id: 'st-1',
        is_enabled: false,
        timezone: 'UTC',
        paths: '',
      };

      const parsed = backupPlanSchema.safeParse(filesPlanMissingPath);
      expect(parsed.success).toBe(false);
      if (!parsed.success) {
        expect(parsed.error.issues[0]?.path).toContain('paths');
      }

      const validFilesPlan = {
        ...filesPlanMissingPath,
        paths: '/var/www/html',
      };
      expect(backupPlanSchema.safeParse(validFilesPlan).success).toBe(true);
    });

    it('allows creating backup plan without explicit storage_target_id (auto-provisioned fallback)', () => {
      const planWithoutStorage = {
        name: 'Auto Storage Plan',
        resource_id: 'res-1',
        backup_type: 'mysql_database' as const,
        is_enabled: false,
        timezone: 'UTC',
        db_mode: 'all' as const,
        selected_databases: [],
      };

      const parsed = backupPlanSchema.safeParse(planWithoutStorage);
      expect(parsed.success).toBe(true);
      if (parsed.success) {
        expect(parsed.data.storage_target_id).toBeUndefined();
      }

      const planWithEmptyStorage = {
        ...planWithoutStorage,
        storage_target_id: '',
      };
      const parsedEmpty = backupPlanSchema.safeParse(planWithEmptyStorage);
      expect(parsedEmpty.success).toBe(true);
      if (parsedEmpty.success) {
        expect(parsedEmpty.data.storage_target_id).toBe('');
      }
    });

    it('preserves explicit storage_target_id when provided in backup plan creation', () => {
      const planWithExplicitStorage = {
        name: 'Explicit Storage Plan',
        resource_id: 'res-1',
        backup_type: 'mysql_database' as const,
        storage_target_id: 'st-explicit-uuid',
        is_enabled: false,
        timezone: 'UTC',
        db_mode: 'all' as const,
        selected_databases: [],
      };

      const parsed = backupPlanSchema.safeParse(planWithExplicitStorage);
      expect(parsed.success).toBe(true);
      if (parsed.success) {
        expect(parsed.data.storage_target_id).toBe('st-explicit-uuid');
      }
    });

    it('validates backup plan edit schema with schedule toggle', () => {
      const editWithEnabledInvalidCron = {
        name: 'Updated Plan Name',
        is_enabled: true,
        status: 'active' as const,
        cron_expression: 'bad',
        timezone: 'UTC',
      };

      const parsed = backupPlanEditSchema.safeParse(editWithEnabledInvalidCron);
      expect(parsed.success).toBe(false);

      const editDisabled = {
        name: 'Updated Plan Name',
        is_enabled: false,
        status: 'active' as const,
        cron_expression: '',
        timezone: 'UTC',
      };
      expect(backupPlanEditSchema.safeParse(editDisabled).success).toBe(true);
    });
  });

  describe('Credential Schemas', () => {
    it('requires private key for ssh_private_key credential', () => {
      const missingKey = {
        name: 'SSH Key',
        type: 'ssh_private_key' as const,
        private_key: '',
      };

      const parsed = credentialCreateSchema.safeParse(missingKey);
      expect(parsed.success).toBe(false);
      if (!parsed.success) {
        expect(parsed.error.issues[0]?.path).toContain('private_key');
      }
    });


    it('requires access_key_id and secret_access_key for s3_credentials', () => {
      const validS3Cred = {
        name: 'AWS S3 Credential',
        type: 's3_credentials' as const,
        access_key_id: 'AKIAIOSFODNN7EXAMPLE',
        secret_access_key: 'wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY',
        session_token: 'optional-session-token',
      };

      const parsed = credentialCreateSchema.safeParse(validS3Cred);
      expect(parsed.success).toBe(true);
    });
  });

  describe('Organization Edit Schema', () => {
    it('validates organization name presence and trimming', () => {
      expect(organizationEditSchema.safeParse({ name: 'Acme Corp' }).success).toBe(true);
      expect(organizationEditSchema.safeParse({ name: '   ' }).success).toBe(false);
      expect(organizationEditSchema.safeParse({ name: '' }).success).toBe(false);
    });
  });
});
