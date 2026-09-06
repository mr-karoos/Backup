'use client';

import * as React from 'react';
import { useRouter } from 'next/navigation';
import Link from 'next/link';
import { useQuery } from '@tanstack/react-query';
import { useForm, Controller } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { ArrowLeft, Cloud, ShieldAlert, AlertCircle } from 'lucide-react';
import { apiClient } from '@/lib/api/api-client';
import { queryKeys } from '@/lib/query/query-client';
import { useAuth } from '@/lib/auth/auth-context';
import { usePermissions } from '@/lib/auth/permissions';
import { useTenantFormGuard } from '@/lib/hooks/use-tenant-form-guard';
import { useUnsavedChanges } from '@/lib/hooks/use-unsaved-changes';
import { useCreateStorageTarget } from '@/lib/api/mutations';
import { FormField } from '@/components/ui/form-field';
import { Select } from '@/components/ui/select';
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/card';
import {
  storageCreateSchema,
  type StorageCreateFormValues,
} from '@/lib/forms/schemas';
import type {
  CreateStorageTargetRequest,
  CredentialListItemResponse,
} from '@/types/domain';

export default function NewStorageTargetPage() {
  const router = useRouter();
  const { activeOrgId } = useAuth();
  const { canManageStorage, canViewCredentials } = usePermissions();
  const createTarget = useCreateStorageTarget();

  const {
    register,
    handleSubmit,
    control,
    reset,
    formState: { errors, isDirty, isSubmitting },
  } = useForm<StorageCreateFormValues>({
    resolver: zodResolver(storageCreateSchema),
    defaultValues: {
      name: '',
      type: 's3',
      bucket: '',
      region: 'us-east-1',
      endpoint: '',
      force_path_style: false,
      credential_id: '',
    },
  });

  // Fetch S3 credentials strictly when user has credential read access (active Org Admin)
  const { data: credentials } = useQuery<CredentialListItemResponse[]>({
    queryKey: activeOrgId ? queryKeys.org(activeOrgId).credentials.all() : ['disabled'],
    queryFn: () => apiClient.get<CredentialListItemResponse[]>('/credentials'),
    enabled: Boolean(activeOrgId && canViewCredentials),
  });

  const s3Credentials = React.useMemo(() => {
    if (!credentials) return [];
    return credentials.filter((c) => c.type === 's3_credentials');
  }, [credentials]);

  const { bypassGuard, safeNavigate } = useUnsavedChanges(isDirty);

  useTenantFormGuard({
    onTenantChanged: () => {
      reset();
      router.push('/storage');
    },
  });

  if (!canManageStorage) {
    return (
      <div className="space-y-6">
        <h1 className="text-2xl font-bold tracking-tight text-foreground">Add Storage Target</h1>
        <Card className="border-destructive/20 bg-destructive/5 p-6 text-center">
          <div className="flex flex-col items-center space-y-3">
            <div className="flex h-12 w-12 items-center justify-center rounded-full bg-destructive/10 text-destructive">
              <ShieldAlert className="h-6 w-6" />
            </div>
            <h3 className="text-base font-semibold text-foreground">Permission Denied</h3>
            <p className="text-sm text-muted-foreground max-w-md">
              You do not have permission to configure storage destinations for this organization.
            </p>
            <Link
              href="/storage"
              className="inline-flex items-center gap-2 text-sm text-primary hover:underline mt-2"
            >
              <ArrowLeft className="h-4 w-4" /> Back to Storage
            </Link>
          </div>
        </Card>
      </div>
    );
  }

  const onSubmit = async (values: StorageCreateFormValues) => {
    const payload: CreateStorageTargetRequest = {
      name: values.name.trim(),
      type: 's3',
      s3_config: {
        bucket: values.bucket.trim(),
        region: values.region.trim(),
        endpoint: values.endpoint?.trim() || '',
        force_path_style: values.force_path_style,
      },
      credential_id: values.credential_id,
    };

    try {
      bypassGuard();
      await createTarget.mutateAsync(payload);
      router.push('/storage');
    } catch {
      // Handled by onError toast
    }
  };

  return (
    <div className="max-w-2xl space-y-6">
      <div className="flex items-center gap-3">
        <button
          type="button"
          onClick={() => safeNavigate('/storage')}
          className="rounded-md p-1 text-muted-foreground hover:bg-muted hover:text-foreground transition-colors"
          aria-label="Back to storage"
        >
          <ArrowLeft className="h-5 w-5" />
        </button>
        <div>
          <h1 className="text-2xl font-bold tracking-tight text-foreground">Add Storage Target</h1>
          <p className="text-sm text-muted-foreground">
            Configure an Amazon S3 cloud storage destination for backup archives
          </p>
        </div>
      </div>

      <div className="rounded-lg border border-amber-800/40 bg-amber-950/20 p-4 text-xs text-amber-300 flex items-start gap-2.5">
        <AlertCircle className="h-4 w-4 shrink-0 mt-0.5" />
        <p>
          Note: Local storage volumes are automatically provisioned and managed by the platform. You can configure external Amazon S3 destinations here. Custom S3-compatible endpoints are deferred to a future update.
        </p>
      </div>

      <Card>
        <CardHeader>
          <CardTitle className="text-base font-semibold flex items-center gap-2">
            <Cloud className="h-4 w-4 text-primary" />
            Storage Target Configuration
          </CardTitle>
        </CardHeader>
        <CardContent>
          <form onSubmit={handleSubmit(onSubmit)} className="space-y-4">
            <FormField
              label="Target Name"
              htmlFor="target-name"
              required
              error={errors.name?.message}
            >
              <input
                id="target-name"
                type="text"
                {...register('name')}
                placeholder="e.g. AWS Production Cold Storage"
                aria-invalid={Boolean(errors.name)}
                className="w-full rounded-md border border-input bg-background px-3 py-2 text-sm text-foreground focus:outline-none focus:ring-2 focus:ring-ring"
              />
            </FormField>

            <FormField label="Storage Type" htmlFor="target-type" required>
              <Select
                id="target-type"
                value="s3"
                disabled
                options={[
                  { value: 's3', label: 'Amazon S3 (Standard)' },
                ]}
              />
            </FormField>

            <div className="grid grid-cols-2 gap-3">
              <FormField
                label="Bucket Name"
                htmlFor="s3-bucket"
                required
                error={errors.bucket?.message}
              >
                <input
                  id="s3-bucket"
                  type="text"
                  {...register('bucket')}
                  placeholder="my-backup-bucket"
                  aria-invalid={Boolean(errors.bucket)}
                  className="w-full rounded-md border border-input bg-background px-3 py-2 text-sm text-foreground font-mono focus:outline-none focus:ring-2 focus:ring-ring"
                />
              </FormField>

              <FormField
                label="Region"
                htmlFor="s3-region"
                required
                error={errors.region?.message}
              >
                <input
                  id="s3-region"
                  type="text"
                  {...register('region')}
                  placeholder="us-east-1"
                  aria-invalid={Boolean(errors.region)}
                  className="w-full rounded-md border border-input bg-background px-3 py-2 text-sm text-foreground font-mono focus:outline-none focus:ring-2 focus:ring-ring"
                />
              </FormField>
            </div>

            <FormField
              label="S3 Credential"
              htmlFor="s3-cred"
              required
              error={errors.credential_id?.message}
              description={
                !canViewCredentials
                  ? 'Organization Administrator privileges are required to browse credential vault.'
                  : s3Credentials.length === 0
                  ? 'No S3 credentials found in the Vault. Please add an S3 Credential in Credentials Vault first.'
                  : 'Select AWS / S3 access keys from the Vault'
              }
            >
              <Controller
                name="credential_id"
                control={control}
                render={({ field }) => (
                  <Select
                    id="s3-cred"
                    value={field.value}
                    onChange={field.onChange}
                    options={[
                      { value: '', label: '-- Select S3 Credential --' },
                      ...s3Credentials.map((c) => ({
                        value: c.id,
                        label: `${c.name} (${c.fingerprint ? c.fingerprint.slice(0, 10) : 'v' + c.key_version})`,
                      })),
                    ]}
                  />
                )}
              />
            </FormField>

            <div className="pt-2">
              <label className="flex items-center gap-2 text-sm text-foreground cursor-pointer">
                <input
                  type="checkbox"
                  {...register('force_path_style')}
                  className="rounded border-input text-primary focus:ring-ring"
                />
                <span>Force Path Style</span>
              </label>
            </div>

            <div className="flex justify-end gap-3 pt-4 border-t">
              <button
                type="button"
                onClick={() => safeNavigate('/storage')}
                className="rounded-md border border-input bg-background px-4 py-2 text-sm font-medium hover:bg-accent hover:text-accent-foreground transition-colors"
              >
                Cancel
              </button>
              <button
                type="submit"
                disabled={isSubmitting || createTarget.isPending}
                className="inline-flex items-center justify-center gap-2 rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground hover:bg-primary/90 focus:outline-none disabled:opacity-50 transition-colors"
              >
                {createTarget.isPending || isSubmitting ? 'Creating...' : 'Create Storage Target'}
              </button>
            </div>
          </form>
        </CardContent>
      </Card>
    </div>
  );
}

