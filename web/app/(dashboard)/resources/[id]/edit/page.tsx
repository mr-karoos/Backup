'use client';

import * as React from 'react';
import { useParams, useRouter } from 'next/navigation';
import Link from 'next/link';
import { useQuery } from '@tanstack/react-query';
import { useForm, Controller } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { ArrowLeft, Server, ShieldAlert } from 'lucide-react';
import { apiClient } from '@/lib/api/api-client';
import { queryKeys } from '@/lib/query/query-client';
import { useAuth } from '@/lib/auth/auth-context';
import { usePermissions } from '@/lib/auth/permissions';
import { useTenantFormGuard } from '@/lib/hooks/use-tenant-form-guard';
import { useUnsavedChanges } from '@/lib/hooks/use-unsaved-changes';
import { useUpdateResource } from '@/lib/api/mutations';
import { FormField } from '@/components/ui/form-field';
import { Select } from '@/components/ui/select';
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/card';
import { Skeleton } from '@/components/ui/skeleton';
import { ErrorState } from '@/components/ui/error-state';
import {
  resourceEditSchema,
  type ResourceEditFormValues,
} from '@/lib/forms/schemas';
import type {
  ResourceResponse,
  UpdateResourceRequest,
  CredentialListItemResponse,
} from '@/types/domain';

function ResourceEditForm({
  id,
  resource,
  credentials,
  canViewCredentials,
}: {
  id: string;
  resource: ResourceResponse;
  credentials: CredentialListItemResponse[];
  canViewCredentials: boolean;
}) {
  const router = useRouter();
  const updateResource = useUpdateResource();

  const {
    register,
    handleSubmit,
    control,
    formState: { errors, isDirty, isSubmitting },
  } = useForm<ResourceEditFormValues>({
    resolver: zodResolver(resourceEditSchema),
    defaultValues: {
      name: resource.name,
      host: resource.connector?.host || '',
      port: resource.connector?.port || (resource.type === 'ubuntu_ssh' ? 22 : 2083),
      username: resource.connector?.username || '',
      auth_type:
        resource.connector?.auth_type ||
        (resource.type === 'ubuntu_ssh' ? 'ssh_key' : 'cpanel_api_token'),
      credential_id: resource.connector?.credential_id || '',
      host_key_fingerprint: resource.connector?.host_key_fingerprint || '',
      connection_timeout_seconds:
        resource.connector?.config?.connection_timeout_seconds || 15,
      use_https:
        resource.connector?.config?.use_https !== undefined
          ? resource.connector.config.use_https
          : true,
    },
  });

  const compatibleCredentials = React.useMemo(() => {
    if (resource.type === 'ubuntu_ssh') {
      return credentials.filter(
        (c) => c.type === 'ssh_private_key' || c.type === 'ssh_password'
      );
    } else {
      return credentials.filter(
        (c) => c.type === 'cpanel_api_token' || c.type === 'cpanel_password'
      );
    }
  }, [credentials, resource.type]);

  const { bypassGuard, safeNavigate } = useUnsavedChanges(isDirty);

  useTenantFormGuard({
    onTenantChanged: () => {
      router.push('/resources');
    },
  });

  const onSubmit = async (values: ResourceEditFormValues) => {
    const payload: UpdateResourceRequest = {
      name: values.name.trim(),
      connector: {
        host: values.host.trim(),
        port: Number(values.port),
        auth_type: values.auth_type,
        username: values.username.trim(),
        credential_id: values.credential_id,
        ...(values.host_key_fingerprint?.trim()
          ? { host_key_fingerprint: values.host_key_fingerprint.trim() }
          : {}),
        config: {
          connection_timeout_seconds: Number(values.connection_timeout_seconds || 15),
          ...(resource.type === 'cpanel' ? { use_https: Boolean(values.use_https) } : {}),
        },
      },
    };

    try {
      await updateResource.mutateAsync({ id, data: payload });
      bypassGuard();
      router.push(`/resources/${id}`);
    } catch {
      // Handled by onError toast
    }
  };

  return (
    <div className="max-w-2xl space-y-6">
      <div className="flex items-center gap-3">
        <button
          type="button"
          onClick={() => safeNavigate(`/resources/${id}`)}
          className="rounded-md p-1 text-muted-foreground hover:bg-muted hover:text-foreground transition-colors"
          aria-label="Back to resource"
        >
          <ArrowLeft className="h-5 w-5" />
        </button>
        <div>
          <h1 className="text-2xl font-bold tracking-tight text-foreground">Edit Resource</h1>
          <p className="text-sm text-muted-foreground">
            Update connection parameters and configuration for {resource.name}
          </p>
        </div>
      </div>

      <Card>
        <CardHeader>
          <CardTitle className="text-base font-semibold flex items-center gap-2">
            <Server className="h-4 w-4 text-primary" />
            Resource Configuration
          </CardTitle>
        </CardHeader>
        <CardContent>
          <form onSubmit={handleSubmit(onSubmit)} className="space-y-4">
            <FormField
              label="Resource Name"
              htmlFor="res-name"
              required
              error={errors.name?.message}
            >
              <input
                id="res-name"
                type="text"
                {...register('name')}
                aria-invalid={Boolean(errors.name)}
                className="w-full rounded-md border border-input bg-background px-3 py-2 text-sm text-foreground focus:outline-none focus:ring-2 focus:ring-ring"
              />
            </FormField>

            <div className="grid grid-cols-3 gap-3">
              <div className="col-span-2">
                <FormField
                  label="Host / IP Address"
                  htmlFor="res-host"
                  required
                  error={errors.host?.message}
                >
                  <input
                    id="res-host"
                    type="text"
                    {...register('host')}
                    aria-invalid={Boolean(errors.host)}
                    className="w-full rounded-md border border-input bg-background px-3 py-2 text-sm text-foreground font-mono focus:outline-none focus:ring-2 focus:ring-ring"
                  />
                </FormField>
              </div>
              <div>
                <FormField
                  label="Port"
                  htmlFor="res-port"
                  required
                  error={errors.port?.message}
                >
                  <input
                    id="res-port"
                    type="number"
                    {...register('port')}
                    aria-invalid={Boolean(errors.port)}
                    className="w-full rounded-md border border-input bg-background px-3 py-2 text-sm text-foreground font-mono focus:outline-none focus:ring-2 focus:ring-ring"
                  />
                </FormField>
              </div>
            </div>

            <div className="grid grid-cols-2 gap-3">
              <FormField
                label="Username"
                htmlFor="res-user"
                required
                error={errors.username?.message}
              >
                <input
                  id="res-user"
                  type="text"
                  {...register('username')}
                  aria-invalid={Boolean(errors.username)}
                  className="w-full rounded-md border border-input bg-background px-3 py-2 text-sm text-foreground font-mono focus:outline-none focus:ring-2 focus:ring-ring"
                />
              </FormField>

              <FormField
                label="Auth Method"
                htmlFor="res-auth-type"
                required
                error={errors.auth_type?.message}
              >
                <Controller
                  name="auth_type"
                  control={control}
                  render={({ field }) => (
                    <Select
                      id="res-auth-type"
                      value={field.value}
                      onChange={field.onChange}
                      options={
                        resource.type === 'ubuntu_ssh'
                          ? [
                              { value: 'ssh_key', label: 'SSH Key' },
                              { value: 'ssh_password', label: 'SSH Password' },
                            ]
                          : [
                              { value: 'cpanel_api_token', label: 'cPanel API Token' },
                              { value: 'cpanel_password', label: 'cPanel Password' },
                            ]
                      }
                    />
                  )}
                />
              </FormField>
            </div>

            <FormField
              label="Authentication Credential"
              htmlFor="res-cred"
              required
              error={errors.credential_id?.message}
              description={
                !canViewCredentials
                  ? 'Organization Administrator privileges are required to browse credential vault.'
                  : compatibleCredentials.length === 0
                  ? 'No compatible credentials found. Please add a credential in the Vault first.'
                  : undefined
              }
            >
              <Controller
                name="credential_id"
                control={control}
                render={({ field }) => (
                  <Select
                    id="res-cred"
                    value={field.value}
                    onChange={field.onChange}
                    options={[
                      { value: '', label: '-- Select Credential --' },
                      ...compatibleCredentials.map((c) => ({
                        value: c.id,
                        label: `${c.name} (${c.type.replace(/_/g, ' ')})`,
                      })),
                    ]}
                  />
                )}
              />
            </FormField>

            {resource.type === 'ubuntu_ssh' && (
              <FormField
                label="Host Key Fingerprint (Optional)"
                htmlFor="res-fingerprint"
                error={errors.host_key_fingerprint?.message}
              >
                <input
                  id="res-fingerprint"
                  type="text"
                  {...register('host_key_fingerprint')}
                  placeholder="SHA256:..."
                  className="w-full rounded-md border border-input bg-background px-3 py-2 text-sm text-foreground font-mono focus:outline-none focus:ring-2 focus:ring-ring"
                />
              </FormField>
            )}

            <div className="grid grid-cols-2 gap-3 pt-2">
              <FormField
                label="Connection Timeout (seconds)"
                htmlFor="res-timeout"
                error={errors.connection_timeout_seconds?.message}
              >
                <input
                  id="res-timeout"
                  type="number"
                  min={5}
                  max={60}
                  {...register('connection_timeout_seconds')}
                  className="w-full rounded-md border border-input bg-background px-3 py-2 text-sm text-foreground font-mono focus:outline-none focus:ring-2 focus:ring-ring"
                />
              </FormField>

              {resource.type === 'cpanel' && (
                <div className="flex flex-col justify-end pb-2">
                  <label className="flex items-center gap-2 text-sm text-foreground cursor-pointer">
                    <input
                      type="checkbox"
                      {...register('use_https')}
                      className="rounded border-input text-primary focus:ring-ring"
                    />
                    <span>Use HTTPS (SSL/TLS)</span>
                  </label>
                </div>
              )}
            </div>

            <div className="flex justify-end gap-3 pt-4 border-t">
              <button
                type="button"
                onClick={() => safeNavigate(`/resources/${id}`)}
                className="rounded-md border border-input bg-background px-4 py-2 text-sm font-medium hover:bg-accent hover:text-accent-foreground transition-colors"
              >
                Cancel
              </button>
              <button
                type="submit"
                disabled={isSubmitting || updateResource.isPending}
                className="inline-flex items-center justify-center gap-2 rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground hover:bg-primary/90 focus:outline-none disabled:opacity-50 transition-colors"
              >
                {updateResource.isPending || isSubmitting ? 'Saving...' : 'Save Changes'}
              </button>
            </div>
          </form>
        </CardContent>
      </Card>
    </div>
  );
}

export default function EditResourcePage() {
  const params = useParams();
  const id = params?.id as string;
  const { activeOrgId } = useAuth();
  const { canEditResource, canViewCredentials } = usePermissions();

  // Fetch resource details
  const {
    data: resource,
    isLoading: loadingResource,
    isError,
    error,
    refetch,
  } = useQuery<ResourceResponse>({
    queryKey: activeOrgId && id ? queryKeys.org(activeOrgId).resources.detail(id) : ['disabled'],
    queryFn: () => apiClient.get<ResourceResponse>(`/resources/${id}`),
    enabled: !!activeOrgId && !!id,
  });

  // Fetch credentials strictly when user has credential read access (active Org Admin)
  const { data: credentials } = useQuery<CredentialListItemResponse[]>({
    queryKey: activeOrgId ? queryKeys.org(activeOrgId).credentials.all() : ['disabled'],
    queryFn: () => apiClient.get<CredentialListItemResponse[]>('/credentials'),
    enabled: Boolean(activeOrgId && canViewCredentials),
  });

  if (!canEditResource) {
    return (
      <div className="space-y-6">
        <h1 className="text-2xl font-bold tracking-tight text-foreground">Edit Resource</h1>
        <Card className="border-destructive/20 bg-destructive/5 p-6 text-center">
          <div className="flex flex-col items-center space-y-3">
            <div className="flex h-12 w-12 items-center justify-center rounded-full bg-destructive/10 text-destructive">
              <ShieldAlert className="h-6 w-6" />
            </div>
            <h3 className="text-base font-semibold text-foreground">Permission Denied</h3>
            <p className="text-sm text-muted-foreground max-w-md">
              You do not have permission to modify protected resources.
            </p>
            <Link
              href={`/resources/${id}`}
              className="inline-flex items-center gap-2 text-sm text-primary hover:underline mt-2"
            >
              <ArrowLeft className="h-4 w-4" /> Back to Resource
            </Link>
          </div>
        </Card>
      </div>
    );
  }

  if (loadingResource) {
    return (
      <div className="space-y-6 max-w-2xl">
        <Skeleton className="h-8 w-48" />
        <Skeleton className="h-64 w-full" />
      </div>
    );
  }

  if (isError || !resource) {
    return (
      <div className="space-y-6 max-w-2xl">
        <ErrorState
          title="Could not load resource for editing"
          error={error}
          onRetry={() => refetch()}
        />
      </div>
    );
  }

  return (
    <ResourceEditForm
      key={resource.id}
      id={id}
      resource={resource}
      credentials={credentials || []}
      canViewCredentials={canViewCredentials}
    />
  );
}

