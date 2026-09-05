'use client';

import * as React from 'react';
import { useRouter } from 'next/navigation';
import Link from 'next/link';
import { useQuery } from '@tanstack/react-query';
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
import type {
  StorageTargetType,
  CreateStorageTargetRequest,
  CredentialListItemResponse,
} from '@/types/domain';

export default function NewStorageTargetPage() {
  const router = useRouter();
  const { activeOrgId, userRole, isSystemAdmin } = useAuth();
  const { canManageStorage } = usePermissions();
  const createTarget = useCreateStorageTarget();

  const isAdmin = userRole === 'admin' || isSystemAdmin;

  // Form State
  const [name, setName] = React.useState('');
  const [type, setType] = React.useState<StorageTargetType>('s3');
  const [bucket, setBucket] = React.useState('');
  const [region, setRegion] = React.useState('us-east-1');
  const [endpoint, setEndpoint] = React.useState('');
  const [forcePathStyle, setForcePathStyle] = React.useState(false);
  const [credentialId, setCredentialId] = React.useState('');
  const [validationError, setValidationError] = React.useState<string | null>(null);

  // Fetch S3 credentials
  const { data: credentials } = useQuery<CredentialListItemResponse[]>({
    queryKey: activeOrgId ? queryKeys.org(activeOrgId).credentials.all() : ['disabled'],
    queryFn: () => apiClient.get<CredentialListItemResponse[]>('/credentials'),
    enabled: !!activeOrgId && isAdmin,
  });

  const s3Credentials = React.useMemo(() => {
    if (!credentials) return [];
    return credentials.filter((c) => c.type === 's3_credentials');
  }, [credentials]);

  const isDirty = Boolean(name || bucket || endpoint || credentialId);
  useUnsavedChanges(isDirty);

  useTenantFormGuard({
    onTenantChanged: () => {
      setName('');
      setBucket('');
      setEndpoint('');
      setCredentialId('');
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

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setValidationError(null);

    if (!name.trim()) {
      setValidationError('Storage target name is required.');
      return;
    }
    if (!bucket.trim()) {
      setValidationError('S3 Bucket name is required.');
      return;
    }
    if (!region.trim()) {
      setValidationError('Region is required.');
      return;
    }
    if (!credentialId) {
      setValidationError('Please select an S3 Credential.');
      return;
    }

    const payload: CreateStorageTargetRequest = {
      name: name.trim(),
      type,
      s3_config: {
        bucket: bucket.trim(),
        region: region.trim(),
        endpoint: endpoint.trim(),
        force_path_style: forcePathStyle,
      },
      credential_id: credentialId,
    };

    try {
      await createTarget.mutateAsync(payload);
      router.push('/storage');
    } catch {
      // Handled by onError toast
    }
  };

  return (
    <div className="max-w-2xl space-y-6">
      <div className="flex items-center gap-3">
        <Link
          href="/storage"
          className="rounded-md p-1 text-muted-foreground hover:bg-muted hover:text-foreground transition-colors"
          aria-label="Back to storage"
        >
          <ArrowLeft className="h-5 w-5" />
        </Link>
        <div>
          <h1 className="text-2xl font-bold tracking-tight text-foreground">Add Storage Target</h1>
          <p className="text-sm text-muted-foreground">
            Configure an S3-compatible cloud storage destination for backup archives
          </p>
        </div>
      </div>

      <div className="rounded-lg border border-amber-800/40 bg-amber-950/20 p-4 text-xs text-amber-300 flex items-start gap-2.5">
        <AlertCircle className="h-4 w-4 shrink-0 mt-0.5" />
        <p>
          Note: Local storage volumes are automatically provisioned and managed by the platform. You can configure external AWS S3 or MinIO / Ceph S3-compatible destinations here.
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
          {validationError && (
            <div className="mb-4 rounded-md border border-destructive/50 bg-destructive/10 p-3 text-sm text-destructive">
              {validationError}
            </div>
          )}

          <form onSubmit={handleSubmit} className="space-y-4">
            <FormField label="Target Name" htmlFor="target-name" required>
              <input
                id="target-name"
                type="text"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="e.g. AWS Production Cold Storage"
                className="w-full rounded-md border border-input bg-background px-3 py-2 text-sm text-foreground focus:outline-none focus:ring-2 focus:ring-ring"
              />
            </FormField>

            <FormField label="Storage Type" htmlFor="target-type" required>
              <Select
                id="target-type"
                value={type}
                onChange={(e) => setType(e.target.value as StorageTargetType)}
                options={[
                  { value: 's3', label: 'Amazon S3' },
                  { value: 's3_compatible', label: 'S3-Compatible (MinIO, Wasabi, Ceph)' },
                ]}
              />
            </FormField>

            <div className="grid grid-cols-2 gap-3">
              <FormField label="Bucket Name" htmlFor="s3-bucket" required>
                <input
                  id="s3-bucket"
                  type="text"
                  value={bucket}
                  onChange={(e) => setBucket(e.target.value)}
                  placeholder="my-backup-bucket"
                  className="w-full rounded-md border border-input bg-background px-3 py-2 text-sm text-foreground font-mono focus:outline-none focus:ring-2 focus:ring-ring"
                />
              </FormField>

              <FormField label="Region" htmlFor="s3-region" required>
                <input
                  id="s3-region"
                  type="text"
                  value={region}
                  onChange={(e) => setRegion(e.target.value)}
                  placeholder="us-east-1"
                  className="w-full rounded-md border border-input bg-background px-3 py-2 text-sm text-foreground font-mono focus:outline-none focus:ring-2 focus:ring-ring"
                />
              </FormField>
            </div>

            {type === 's3_compatible' && (
              <FormField
                label="Custom Endpoint URL"
                htmlFor="s3-endpoint"
                description="e.g. https://minio.company.internal:9000"
              >
                <input
                  id="s3-endpoint"
                  type="text"
                  value={endpoint}
                  onChange={(e) => setEndpoint(e.target.value)}
                  placeholder="https://s3.example.com"
                  className="w-full rounded-md border border-input bg-background px-3 py-2 text-sm text-foreground font-mono focus:outline-none focus:ring-2 focus:ring-ring"
                />
              </FormField>
            )}

            <FormField
              label="S3 Credential"
              htmlFor="s3-cred"
              required
              description={
                s3Credentials.length === 0
                  ? 'No S3 credentials found in the Vault. Please add an S3 Credential in Credentials Vault first.'
                  : 'Select AWS / S3 access keys from the Vault'
              }
            >
              <Select
                id="s3-cred"
                value={credentialId}
                onChange={(e) => setCredentialId(e.target.value)}
                options={[
                  { value: '', label: '-- Select S3 Credential --' },
                  ...s3Credentials.map((c) => ({
                    value: c.id,
                    label: `${c.name} (${c.fingerprint ? c.fingerprint.slice(0, 10) : 'v' + c.key_version})`,
                  })),
                ]}
              />
            </FormField>

            <div className="pt-2">
              <label className="flex items-center gap-2 text-sm text-foreground cursor-pointer">
                <input
                  type="checkbox"
                  checked={forcePathStyle}
                  onChange={(e) => setForcePathStyle(e.target.checked)}
                  className="rounded border-input text-primary focus:ring-ring"
                />
                <span>Force Path Style (Required for most MinIO/Ceph setups)</span>
              </label>
            </div>

            <div className="flex justify-end gap-3 pt-4 border-t">
              <Link
                href="/storage"
                className="rounded-md border border-input bg-background px-4 py-2 text-sm font-medium hover:bg-accent hover:text-accent-foreground transition-colors"
              >
                Cancel
              </Link>
              <button
                type="submit"
                disabled={createTarget.isPending}
                className="inline-flex items-center justify-center gap-2 rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground hover:bg-primary/90 focus:outline-none disabled:opacity-50 transition-colors"
              >
                {createTarget.isPending ? 'Creating...' : 'Create Storage Target'}
              </button>
            </div>
          </form>
        </CardContent>
      </Card>
    </div>
  );
}
