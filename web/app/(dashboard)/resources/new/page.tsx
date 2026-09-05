'use client';

import * as React from 'react';
import { useRouter } from 'next/navigation';
import Link from 'next/link';
import { useQuery } from '@tanstack/react-query';
import { ArrowLeft, Server, ShieldAlert } from 'lucide-react';
import { apiClient } from '@/lib/api/api-client';
import { queryKeys } from '@/lib/query/query-client';
import { useAuth } from '@/lib/auth/auth-context';
import { usePermissions } from '@/lib/auth/permissions';
import { useTenantFormGuard } from '@/lib/hooks/use-tenant-form-guard';
import { useUnsavedChanges } from '@/lib/hooks/use-unsaved-changes';
import { useCreateResource } from '@/lib/api/mutations';
import { FormField } from '@/components/ui/form-field';
import { Select } from '@/components/ui/select';
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/card';
import type {
  ResourceType,
  CreateResourceRequest,
  CredentialListItemResponse,
} from '@/types/domain';

export default function NewResourcePage() {
  const router = useRouter();
  const { activeOrgId, userRole, isSystemAdmin } = useAuth();
  const { canCreateResource } = usePermissions();
  const createResource = useCreateResource();

  const isAdmin = userRole === 'admin' || isSystemAdmin;

  // Form State
  const [name, setName] = React.useState('');
  const [type, setType] = React.useState<ResourceType>('ubuntu_ssh');
  const [host, setHost] = React.useState('');
  const [port, setPort] = React.useState<number>(22);
  const [username, setUsername] = React.useState('root');
  const [authType, setAuthType] = React.useState<string>('ssh_key');
  const [credentialId, setCredentialId] = React.useState('');
  const [hostKeyFingerprint, setHostKeyFingerprint] = React.useState('');
  const [connectionTimeout, setConnectionTimeout] = React.useState<number>(15);
  const [useHttps, setUseHttps] = React.useState(true);
  const [validationError, setValidationError] = React.useState<string | null>(null);

  // Fetch available credentials if user has credential read access
  const { data: credentials } = useQuery<CredentialListItemResponse[]>({
    queryKey: activeOrgId ? queryKeys.org(activeOrgId).credentials.all() : ['disabled'],
    queryFn: () => apiClient.get<CredentialListItemResponse[]>('/credentials'),
    enabled: !!activeOrgId && isAdmin,
  });

  // Filter credentials based on resource type
  const compatibleCredentials = React.useMemo(() => {
    if (!credentials) return [];
    if (type === 'ubuntu_ssh') {
      return credentials.filter(
        (c) => c.type === 'ssh_private_key' || c.type === 'ssh_password'
      );
    } else {
      return credentials.filter(
        (c) => c.type === 'cpanel_api_token' || c.type === 'cpanel_password'
      );
    }
  }, [credentials, type]);

  const handleTypeChange = (newType: ResourceType) => {
    setType(newType);
    if (newType === 'ubuntu_ssh') {
      setPort(22);
      setAuthType('ssh_key');
      setUsername('root');
    } else {
      setPort(2083);
      setAuthType('cpanel_api_token');
      setUsername('');
    }
    setCredentialId('');
  };

  const isDirty = Boolean(name || host || credentialId);
  useUnsavedChanges(isDirty);

  useTenantFormGuard({
    onTenantChanged: () => {
      setName('');
      setHost('');
      setCredentialId('');
      router.push('/resources');
    },
  });

  if (!canCreateResource) {
    return (
      <div className="space-y-6">
        <h1 className="text-2xl font-bold tracking-tight text-foreground">Add Resource</h1>
        <Card className="border-destructive/20 bg-destructive/5 p-6 text-center">
          <div className="flex flex-col items-center space-y-3">
            <div className="flex h-12 w-12 items-center justify-center rounded-full bg-destructive/10 text-destructive">
              <ShieldAlert className="h-6 w-6" />
            </div>
            <h3 className="text-base font-semibold text-foreground">Permission Denied</h3>
            <p className="text-sm text-muted-foreground max-w-md">
              You do not have permission to register protected resources in this organization.
            </p>
            <Link
              href="/resources"
              className="inline-flex items-center gap-2 text-sm text-primary hover:underline mt-2"
            >
              <ArrowLeft className="h-4 w-4" /> Back to Resources
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
      setValidationError('Resource name is required.');
      return;
    }
    if (!host.trim()) {
      setValidationError('Host/IP address is required.');
      return;
    }
    if (!username.trim()) {
      setValidationError('Username is required.');
      return;
    }
    if (!credentialId) {
      setValidationError('Please select a compatible credential.');
      return;
    }

    const payload: CreateResourceRequest = {
      name: name.trim(),
      type,
      connector: {
        host: host.trim(),
        port: Number(port),
        auth_type: authType,
        username: username.trim(),
        credential_id: credentialId,
        ...(hostKeyFingerprint.trim()
          ? { host_key_fingerprint: hostKeyFingerprint.trim() }
          : {}),
        config: {
          connection_timeout_seconds: Number(connectionTimeout),
          ...(type === 'cpanel' ? { use_https: useHttps } : {}),
        },
      },
    };

    try {
      await createResource.mutateAsync(payload);
      router.push('/resources');
    } catch {
      // Handled by onError toast
    }
  };

  return (
    <div className="max-w-2xl space-y-6">
      <div className="flex items-center gap-3">
        <Link
          href="/resources"
          className="rounded-md p-1 text-muted-foreground hover:bg-muted hover:text-foreground transition-colors"
          aria-label="Back to resources"
        >
          <ArrowLeft className="h-5 w-5" />
        </Link>
        <div>
          <h1 className="text-2xl font-bold tracking-tight text-foreground">Register Resource</h1>
          <p className="text-sm text-muted-foreground">
            Connect an infrastructure server or asset for automated backup policies
          </p>
        </div>
      </div>

      <Card>
        <CardHeader>
          <CardTitle className="text-base font-semibold flex items-center gap-2">
            <Server className="h-4 w-4 text-primary" />
            Resource Specification
          </CardTitle>
        </CardHeader>
        <CardContent>
          {validationError && (
            <div className="mb-4 rounded-md border border-destructive/50 bg-destructive/10 p-3 text-sm text-destructive">
              {validationError}
            </div>
          )}

          <form onSubmit={handleSubmit} className="space-y-4">
            <FormField label="Resource Name" htmlFor="res-name" required>
              <input
                id="res-name"
                type="text"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="e.g. Primary App Server 01"
                className="w-full rounded-md border border-input bg-background px-3 py-2 text-sm text-foreground focus:outline-none focus:ring-2 focus:ring-ring"
              />
            </FormField>

            <FormField label="Resource Type" htmlFor="res-type" required>
              <Select
                id="res-type"
                value={type}
                onChange={(e) => handleTypeChange(e.target.value as ResourceType)}
                options={[
                  { value: 'ubuntu_ssh', label: 'Ubuntu Linux Server (SSH)' },
                  { value: 'cpanel', label: 'cPanel / WHM Hosting Server' },
                ]}
              />
            </FormField>

            <div className="grid grid-cols-3 gap-3">
              <div className="col-span-2">
                <FormField label="Host / IP Address" htmlFor="res-host" required>
                  <input
                    id="res-host"
                    type="text"
                    value={host}
                    onChange={(e) => setHost(e.target.value)}
                    placeholder="192.168.1.100 or server.domain.com"
                    className="w-full rounded-md border border-input bg-background px-3 py-2 text-sm text-foreground font-mono focus:outline-none focus:ring-2 focus:ring-ring"
                  />
                </FormField>
              </div>
              <div>
                <FormField label="Port" htmlFor="res-port" required>
                  <input
                    id="res-port"
                    type="number"
                    value={port}
                    onChange={(e) => setPort(Number(e.target.value))}
                    className="w-full rounded-md border border-input bg-background px-3 py-2 text-sm text-foreground font-mono focus:outline-none focus:ring-2 focus:ring-ring"
                  />
                </FormField>
              </div>
            </div>

            <div className="grid grid-cols-2 gap-3">
              <FormField label="Username" htmlFor="res-user" required>
                <input
                  id="res-user"
                  type="text"
                  value={username}
                  onChange={(e) => setUsername(e.target.value)}
                  placeholder="root or cpanel_user"
                  className="w-full rounded-md border border-input bg-background px-3 py-2 text-sm text-foreground font-mono focus:outline-none focus:ring-2 focus:ring-ring"
                />
              </FormField>

              <FormField label="Auth Method" htmlFor="res-auth-type" required>
                <Select
                  id="res-auth-type"
                  value={authType}
                  onChange={(e) => setAuthType(e.target.value)}
                  options={
                    type === 'ubuntu_ssh'
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
              </FormField>
            </div>

            <FormField
              label="Authentication Credential"
              htmlFor="res-cred"
              required
              description={
                compatibleCredentials.length === 0
                  ? 'No compatible credentials found. Please add a credential in the Vault first.'
                  : 'Select an encrypted credential from the Vault'
              }
            >
              <Select
                id="res-cred"
                value={credentialId}
                onChange={(e) => setCredentialId(e.target.value)}
                options={[
                  { value: '', label: '-- Select Credential --' },
                  ...compatibleCredentials.map((c) => ({
                    value: c.id,
                    label: `${c.name} (${c.type.replace(/_/g, ' ')})`,
                  })),
                ]}
              />
            </FormField>

            {type === 'ubuntu_ssh' && (
              <FormField
                label="Host Key Fingerprint (Optional)"
                htmlFor="res-fingerprint"
                description="SHA-256 fingerprint for SSH host verification"
              >
                <input
                  id="res-fingerprint"
                  type="text"
                  value={hostKeyFingerprint}
                  onChange={(e) => setHostKeyFingerprint(e.target.value)}
                  placeholder="SHA256:abc123xyz..."
                  className="w-full rounded-md border border-input bg-background px-3 py-2 text-sm text-foreground font-mono focus:outline-none focus:ring-2 focus:ring-ring"
                />
              </FormField>
            )}

            <div className="grid grid-cols-2 gap-3 pt-2">
              <FormField
                label="Connection Timeout (seconds)"
                htmlFor="res-timeout"
                description="Default: 15s"
              >
                <input
                  id="res-timeout"
                  type="number"
                  min={5}
                  max={60}
                  value={connectionTimeout}
                  onChange={(e) => setConnectionTimeout(Number(e.target.value))}
                  className="w-full rounded-md border border-input bg-background px-3 py-2 text-sm text-foreground font-mono focus:outline-none focus:ring-2 focus:ring-ring"
                />
              </FormField>

              {type === 'cpanel' && (
                <div className="flex flex-col justify-end pb-2">
                  <label className="flex items-center gap-2 text-sm text-foreground cursor-pointer">
                    <input
                      type="checkbox"
                      checked={useHttps}
                      onChange={(e) => setUseHttps(e.target.checked)}
                      className="rounded border-input text-primary focus:ring-ring"
                    />
                    <span>Use HTTPS (SSL/TLS)</span>
                  </label>
                </div>
              )}
            </div>

            <div className="flex justify-end gap-3 pt-4 border-t">
              <Link
                href="/resources"
                className="rounded-md border border-input bg-background px-4 py-2 text-sm font-medium hover:bg-accent hover:text-accent-foreground transition-colors"
              >
                Cancel
              </Link>
              <button
                type="submit"
                disabled={createResource.isPending}
                className="inline-flex items-center justify-center gap-2 rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground hover:bg-primary/90 focus:outline-none disabled:opacity-50 transition-colors"
              >
                {createResource.isPending ? 'Registering...' : 'Register Resource'}
              </button>
            </div>
          </form>
        </CardContent>
      </Card>
    </div>
  );
}
