import { z } from 'zod';

// =============================================================
// Credentials Schemas
// =============================================================
export const credentialCreateSchema = z
  .object({
    name: z.string().trim().min(1, 'Credential name is required').max(255),
    type: z.enum([
      'ssh_password',
      'ssh_private_key',
      'cpanel_api_token',
      'cpanel_password',
      's3_credentials',
    ]),
    password: z.string().optional(),
    private_key: z.string().optional(),
    passphrase: z.string().optional(),
    api_token: z.string().optional(),
    access_key_id: z.string().optional(),
    secret_access_key: z.string().optional(),
    session_token: z.string().optional(),
  })
  .superRefine((val, ctx) => {
    if (val.type === 'ssh_password') {
      if (!val.password || !val.password.trim()) {
        ctx.addIssue({
          code: z.ZodIssueCode.custom,
          message: 'Password is required',
          path: ['password'],
        });
      }
    } else if (val.type === 'ssh_private_key') {
      if (!val.private_key || !val.private_key.trim()) {
        ctx.addIssue({
          code: z.ZodIssueCode.custom,
          message: 'Private key PEM is required',
          path: ['private_key'],
        });
      }
    } else if (val.type === 'cpanel_api_token') {
      if (!val.api_token || !val.api_token.trim()) {
        ctx.addIssue({
          code: z.ZodIssueCode.custom,
          message: 'API token is required',
          path: ['api_token'],
        });
      }
    } else if (val.type === 'cpanel_password') {
      if (!val.password || !val.password.trim()) {
        ctx.addIssue({
          code: z.ZodIssueCode.custom,
          message: 'cPanel password is required',
          path: ['password'],
        });
      }
    } else if (val.type === 's3_credentials') {
      if (!val.access_key_id || !val.access_key_id.trim()) {
        ctx.addIssue({
          code: z.ZodIssueCode.custom,
          message: 'Access Key ID is required',
          path: ['access_key_id'],
        });
      }
      if (!val.secret_access_key || !val.secret_access_key.trim()) {
        ctx.addIssue({
          code: z.ZodIssueCode.custom,
          message: 'Secret Access Key is required',
          path: ['secret_access_key'],
        });
      }
    }
  });

export type CredentialCreateFormValues = z.infer<typeof credentialCreateSchema>;

export const credentialEditSchema = z
  .object({
    name: z.string().trim().min(1, 'Credential name is required').max(255),
    password: z.string().optional(),
    private_key: z.string().optional(),
    passphrase: z.string().optional(),
    api_token: z.string().optional(),
    access_key_id: z.string().optional(),
    secret_access_key: z.string().optional(),
    session_token: z.string().optional(),
  })
  .superRefine((val, ctx) => {
    const hasKeyId = Boolean(val.access_key_id && val.access_key_id.trim());
    const hasSecretKey = Boolean(val.secret_access_key && val.secret_access_key.trim());

    if (hasKeyId && !hasSecretKey) {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        message: 'Secret Access Key is required when updating Access Key ID',
        path: ['secret_access_key'],
      });
    } else if (!hasKeyId && hasSecretKey) {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        message: 'Access Key ID is required when updating Secret Access Key',
        path: ['access_key_id'],
      });
    }
  });

export type CredentialEditFormValues = z.infer<typeof credentialEditSchema>;

// =============================================================
// Resource Schemas
// =============================================================
export const resourceCreateSchema = z.object({
  name: z.string().trim().min(1, 'Resource name is required').max(255),
  type: z.enum(['ubuntu_ssh', 'cpanel']),
  host: z.string().trim().min(1, 'Host address is required'),
  port: z.coerce.number().int().min(1, 'Port must be >= 1').max(65535, 'Port must be <= 65535'),
  username: z.string().trim().min(1, 'Username is required'),
  credential_id: z.string().min(1, 'Please select a credential'),
  auth_type: z.string().min(1, 'Authentication type is required'),
  host_key_fingerprint: z.string().optional(),
  connection_timeout_seconds: z.coerce.number().int().min(1).max(300).optional(),
  use_https: z.boolean().optional(),
});

export type ResourceCreateFormValues = z.infer<typeof resourceCreateSchema>;

export const resourceEditSchema = z.object({
  name: z.string().trim().min(1, 'Resource name is required').max(255),
  host: z.string().trim().min(1, 'Host address is required'),
  port: z.coerce.number().int().min(1, 'Port must be >= 1').max(65535, 'Port must be <= 65535'),
  username: z.string().trim().min(1, 'Username is required'),
  credential_id: z.string().min(1, 'Please select a credential'),
  auth_type: z.string().min(1, 'Authentication type is required'),
  host_key_fingerprint: z.string().optional(),
  connection_timeout_seconds: z.coerce.number().int().min(1).max(300).optional(),
  use_https: z.boolean().optional(),
});

export type ResourceEditFormValues = z.infer<typeof resourceEditSchema>;

// =============================================================
// Storage Target Schemas
// =============================================================
// Note: Per F1B review requirements, only 's3' creation is supported in this baseline.
// 's3_compatible' mutation is deferred.
export const storageCreateSchema = z.object({
  name: z.string().trim().min(1, 'Storage target name is required').max(255),
  type: z.literal('s3'),
  bucket: z.string().trim().min(1, 'Bucket name is required'),
  region: z.string().trim().min(1, 'Region is required'),
  endpoint: z.string().trim().optional(),
  force_path_style: z.boolean().default(false),
  credential_id: z.string().min(1, 'Please select an S3 Credential'),
});

export type StorageCreateFormValues = z.infer<typeof storageCreateSchema>;

export const storageEditSchema = z.object({
  name: z.string().trim().min(1, 'Target name is required').max(255),
  credential_id: z.string().optional(),
});

export type StorageEditFormValues = z.infer<typeof storageEditSchema>;

// =============================================================
// Backup Plan Schemas
// =============================================================
export const backupPlanSchema = z
  .object({
    name: z.string().trim().min(1, 'Plan name is required').max(255),
    resource_id: z.string().min(1, 'Target resource is required'),
    backup_type: z.enum(['mysql_database', 'website_files']),
    storage_target_id: z.string().min(1, 'Storage target is required'),
    is_enabled: z.boolean().default(true),
    cron_expression: z.string().trim().optional(),
    timezone: z.string().trim().min(1, 'Timezone is required').default('UTC'),
    keep_last_n: z.coerce.number().int().min(1).optional().nullable(),
    keep_days: z.coerce.number().int().min(1).optional().nullable(),
    db_mode: z.enum(['all', 'selected']).default('all'),
    selected_databases: z.array(z.string()).default([]),
    manual_databases: z.string().optional(),
    paths: z.string().optional(),
    exclude_patterns: z.string().optional(),
  })
  .superRefine((val, ctx) => {
    if (val.is_enabled) {
      if (!val.cron_expression || val.cron_expression.trim().split(/\s+/).length !== 5) {
        ctx.addIssue({
          code: z.ZodIssueCode.custom,
          message: 'Valid 5-field cron expression required when schedule is enabled',
          path: ['cron_expression'],
        });
      }
    }
    if (val.backup_type === 'mysql_database') {
      if (val.db_mode === 'selected') {
        const manual = (val.manual_databases || '')
          .split(/[,\n]/)
          .map((s) => s.trim())
          .filter(Boolean);
        if (val.selected_databases.length === 0 && manual.length === 0) {
          ctx.addIssue({
            code: z.ZodIssueCode.custom,
            message: 'Please select or specify at least one database',
            path: ['selected_databases'],
          });
        }
      }
    } else {
      const p = (val.paths || '')
        .split(/[,\n]/)
        .map((s) => s.trim())
        .filter(Boolean);
      if (p.length === 0) {
        ctx.addIssue({
          code: z.ZodIssueCode.custom,
          message: 'Please specify at least one directory path',
          path: ['paths'],
        });
      }
    }
  });

export type BackupPlanFormValues = z.infer<typeof backupPlanSchema>;

export const backupPlanEditSchema = z
  .object({
    name: z.string().trim().min(1, 'Plan name is required').max(255),
    is_enabled: z.boolean(),
    status: z.enum(['active', 'paused', 'archived']),
    cron_expression: z.string().trim().optional(),
    timezone: z.string().trim().min(1, 'Timezone is required'),
    storage_target_id: z.string().optional(),
    keep_last_n: z.coerce.number().int().min(0).optional().nullable(),
    keep_days: z.coerce.number().int().min(0).optional().nullable(),
    db_mode: z.enum(['all', 'selected']).optional(),
    selected_databases: z.string().optional(),
    paths: z.string().optional(),
    exclude_patterns: z.string().optional(),
  })
  .superRefine((val, ctx) => {
    if (val.is_enabled) {
      if (!val.cron_expression || val.cron_expression.trim().split(/\s+/).length !== 5) {
        ctx.addIssue({
          code: z.ZodIssueCode.custom,
          message: 'Valid 5-field cron expression required when schedule is enabled',
          path: ['cron_expression'],
        });
      }
    }
  });


export type BackupPlanEditFormValues = z.infer<typeof backupPlanEditSchema>;

// =============================================================
// Organization Settings Schema
// =============================================================
export const organizationEditSchema = z.object({
  name: z.string().trim().min(1, 'Organization name is required').max(255),
});

export type OrganizationEditFormValues = z.infer<typeof organizationEditSchema>;
