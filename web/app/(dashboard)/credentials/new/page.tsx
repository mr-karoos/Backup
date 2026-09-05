'use client';

import * as React from 'react';
import { useRouter } from 'next/navigation';
import Link from 'next/link';
import { ArrowLeft, KeyRound, ShieldAlert } from 'lucide-react';
import { usePermissions } from '@/lib/auth/permissions';
import { useTenantFormGuard } from '@/lib/hooks/use-tenant-form-guard';
import { useUnsavedChanges } from '@/lib/hooks/use-unsaved-changes';
import { useCreateCredential } from '@/lib/api/mutations';
import { FormField } from '@/components/ui/form-field';
import { Select } from '@/components/ui/select';
import { SecretInput } from '@/components/ui/secret-input';
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/card';
import type { CredentialType, CreateCredentialRequest } from '@/types/domain';

export default function NewCredentialPage() {
  const router = useRouter();
  const { canManageCredentials } = usePermissions();
  const createCredential = useCreateCredential();

  // Form State
  const [name, setName] = React.useState('');
  const [type, setType] = React.useState<CredentialType>('ssh_private_key');

  // Secrets state (in-memory only)
  const [privateKey, setPrivateKey] = React.useState('');
  const [passphrase, setPassphrase] = React.useState('');
  const [sshPassword, setSshPassword] = React.useState('');
  const [cpanelApiToken, setCpanelApiToken] = React.useState('');
  const [cpanelPassword, setCpanelPassword] = React.useState('');
  const [accessKeyId, setAccessKeyId] = React.useState('');
  const [secretAccessKey, setSecretAccessKey] = React.useState('');
  const [sessionToken, setSessionToken] = React.useState('');

  const [validationError, setValidationError] = React.useState<string | null>(null);

  const isDirty = Boolean(
    name ||
    privateKey ||
    passphrase ||
    sshPassword ||
    cpanelApiToken ||
    cpanelPassword ||
    accessKeyId ||
    secretAccessKey ||
    sessionToken
  );

  useUnsavedChanges(isDirty);

  useTenantFormGuard({
    onTenantChanged: () => {
      setName('');
      setPrivateKey('');
      setPassphrase('');
      setSshPassword('');
      setCpanelApiToken('');
      setCpanelPassword('');
      setAccessKeyId('');
      setSecretAccessKey('');
      setSessionToken('');
      router.push('/credentials');
    },
  });

  if (!canManageCredentials) {
    return (
      <div className="space-y-6">
        <h1 className="text-2xl font-bold tracking-tight text-foreground">Add Credential</h1>
        <Card className="border-destructive/20 bg-destructive/5 p-6 text-center">
          <div className="flex flex-col items-center space-y-3">
            <div className="flex h-12 w-12 items-center justify-center rounded-full bg-destructive/10 text-destructive">
              <ShieldAlert className="h-6 w-6" />
            </div>
            <h3 className="text-base font-semibold text-foreground">Permission Denied</h3>
            <p className="text-sm text-muted-foreground max-w-md">
              Only Organization Administrators can register encryption credentials.
            </p>
            <Link
              href="/credentials"
              className="inline-flex items-center gap-2 text-sm text-primary hover:underline mt-2"
            >
              <ArrowLeft className="h-4 w-4" /> Back to Credentials
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
      setValidationError('Credential name is required.');
      return;
    }

    const payload: CreateCredentialRequest = {
      name: name.trim(),
      type,
    };

    switch (type) {
      case 'ssh_private_key':
        if (!privateKey.trim()) {
          setValidationError('SSH Private Key is required.');
          return;
        }
        payload.secret = privateKey.trim();
        if (passphrase) {
          payload.passphrase = passphrase;
        }
        break;

      case 'ssh_password':
        if (!sshPassword) {
          setValidationError('SSH Password is required.');
          return;
        }
        payload.secret = sshPassword;
        break;

      case 'cpanel_api_token':
        if (!cpanelApiToken) {
          setValidationError('cPanel API Token is required.');
          return;
        }
        payload.secret = cpanelApiToken;
        break;

      case 'cpanel_password':
        if (!cpanelPassword) {
          setValidationError('cPanel Password is required.');
          return;
        }
        payload.secret = cpanelPassword;
        break;

      case 's3_credentials':
        if (!accessKeyId.trim() || !secretAccessKey.trim()) {
          setValidationError('Both Access Key ID and Secret Access Key are required.');
          return;
        }
        payload.access_key_id = accessKeyId.trim();
        payload.secret_access_key = secretAccessKey.trim();
        if (sessionToken.trim()) {
          payload.session_token = sessionToken.trim();
        }
        break;
    }

    try {
      await createCredential.mutateAsync(payload);
      // Clean up sensitive state immediately
      setPrivateKey('');
      setPassphrase('');
      setSshPassword('');
      setCpanelApiToken('');
      setCpanelPassword('');
      setAccessKeyId('');
      setSecretAccessKey('');
      setSessionToken('');
      router.push('/credentials');
    } catch {
      // Handled by mutation onError toast
    }
  };

  return (
    <div className="max-w-2xl space-y-6">
      <div className="flex items-center gap-3">
        <Link
          href="/credentials"
          className="rounded-md p-1 text-muted-foreground hover:bg-muted hover:text-foreground transition-colors"
          aria-label="Back to credentials"
        >
          <ArrowLeft className="h-5 w-5" />
        </Link>
        <div>
          <h1 className="text-2xl font-bold tracking-tight text-foreground">Add New Credential</h1>
          <p className="text-sm text-muted-foreground">
            Configure encrypted authentication secrets for your resources and storage
          </p>
        </div>
      </div>

      <Card>
        <CardHeader>
          <CardTitle className="text-base font-semibold flex items-center gap-2">
            <KeyRound className="h-4 w-4 text-primary" />
            Credential Details
          </CardTitle>
        </CardHeader>
        <CardContent>
          {validationError && (
            <div className="mb-4 rounded-md border border-destructive/50 bg-destructive/10 p-3 text-sm text-destructive">
              {validationError}
            </div>
          )}

          <form onSubmit={handleSubmit} className="space-y-4">
            <FormField label="Credential Name" htmlFor="cred-name" required>
              <input
                id="cred-name"
                type="text"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="e.g. Production Web Bastion SSH Key"
                className="w-full rounded-md border border-input bg-background px-3 py-2 text-sm text-foreground placeholder:text-muted-foreground focus:outline-none focus:ring-2 focus:ring-ring"
              />
            </FormField>

            <FormField label="Credential Type" htmlFor="cred-type" required>
              <Select
                id="cred-type"
                value={type}
                onChange={(e) => setType(e.target.value as CredentialType)}
                options={[
                  { value: 'ssh_private_key', label: 'SSH Private Key' },
                  { value: 'ssh_password', label: 'SSH Password' },
                  { value: 'cpanel_api_token', label: 'cPanel API Token' },
                  { value: 'cpanel_password', label: 'cPanel Password' },
                  { value: 's3_credentials', label: 'S3 Credentials' },
                ]}
              />
            </FormField>

            {/* Type-Specific Secret Inputs */}
            {type === 'ssh_private_key' && (
              <>
                <FormField
                  label="Private Key (PEM format)"
                  htmlFor="ssh-key"
                  required
                  description="Begins with -----BEGIN OPENSSH PRIVATE KEY----- or -----BEGIN RSA PRIVATE KEY-----"
                >
                  <textarea
                    id="ssh-key"
                    rows={6}
                    value={privateKey}
                    onChange={(e) => setPrivateKey(e.target.value)}
                    placeholder="-----BEGIN OPENSSH PRIVATE KEY-----&#10;...&#10;-----END OPENSSH PRIVATE KEY-----"
                    className="w-full rounded-md border border-input bg-background px-3 py-2 font-mono text-xs text-foreground placeholder:text-muted-foreground focus:outline-none focus:ring-2 focus:ring-ring"
                  />
                </FormField>

                <FormField
                  label="Passphrase (Optional)"
                  htmlFor="ssh-passphrase"
                  description="Leave empty if key has no passphrase"
                >
                  <SecretInput
                    id="ssh-passphrase"
                    value={passphrase}
                    onChange={(e) => setPassphrase(e.target.value)}
                    placeholder="Passphrase"
                  />
                </FormField>
              </>
            )}

            {type === 'ssh_password' && (
              <FormField label="SSH Password" htmlFor="ssh-password" required>
                <SecretInput
                  id="ssh-password"
                  value={sshPassword}
                  onChange={(e) => setSshPassword(e.target.value)}
                  placeholder="SSH account password"
                />
              </FormField>
            )}

            {type === 'cpanel_api_token' && (
              <FormField
                label="cPanel API Token"
                htmlFor="cpanel-token"
                required
                description="Generate from cPanel > Security > Manage API Tokens"
              >
                <SecretInput
                  id="cpanel-token"
                  value={cpanelApiToken}
                  onChange={(e) => setCpanelApiToken(e.target.value)}
                  placeholder="API Token"
                />
              </FormField>
            )}

            {type === 'cpanel_password' && (
              <FormField label="cPanel Password" htmlFor="cpanel-password" required>
                <SecretInput
                  id="cpanel-password"
                  value={cpanelPassword}
                  onChange={(e) => setCpanelPassword(e.target.value)}
                  placeholder="cPanel password"
                />
              </FormField>
            )}

            {type === 's3_credentials' && (
              <>
                <FormField label="Access Key ID" htmlFor="s3-access-key" required>
                  <input
                    id="s3-access-key"
                    type="text"
                    value={accessKeyId}
                    onChange={(e) => setAccessKeyId(e.target.value)}
                    placeholder="AKIAIOSFODNN7EXAMPLE"
                    className="w-full rounded-md border border-input bg-background px-3 py-2 text-sm text-foreground placeholder:text-muted-foreground focus:outline-none focus:ring-2 focus:ring-ring font-mono"
                  />
                </FormField>

                <FormField label="Secret Access Key" htmlFor="s3-secret-key" required>
                  <SecretInput
                    id="s3-secret-key"
                    value={secretAccessKey}
                    onChange={(e) => setSecretAccessKey(e.target.value)}
                    placeholder="Secret Access Key"
                  />
                </FormField>

                <FormField
                  label="Session Token (Optional)"
                  htmlFor="s3-session-token"
                  description="Only required for temporary STS credentials"
                >
                  <SecretInput
                    id="s3-session-token"
                    value={sessionToken}
                    onChange={(e) => setSessionToken(e.target.value)}
                    placeholder="Session token"
                  />
                </FormField>
              </>
            )}

            <div className="flex justify-end gap-3 pt-4 border-t">
              <Link
                href="/credentials"
                className="rounded-md border border-input bg-background px-4 py-2 text-sm font-medium hover:bg-accent hover:text-accent-foreground transition-colors"
              >
                Cancel
              </Link>
              <button
                type="submit"
                disabled={createCredential.isPending}
                className="inline-flex items-center justify-center gap-2 rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground hover:bg-primary/90 focus:outline-none disabled:opacity-50 transition-colors"
              >
                {createCredential.isPending ? 'Saving...' : 'Save Credential'}
              </button>
            </div>
          </form>
        </CardContent>
      </Card>
    </div>
  );
}
