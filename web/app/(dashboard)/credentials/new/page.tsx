'use client';

import * as React from 'react';
import { useRouter } from 'next/navigation';
import Link from 'next/link';
import { useForm, Controller } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { ArrowLeft, KeyRound, ShieldAlert } from 'lucide-react';
import { usePermissions } from '@/lib/auth/permissions';
import { useTenantFormGuard } from '@/lib/hooks/use-tenant-form-guard';
import { useUnsavedChanges } from '@/lib/hooks/use-unsaved-changes';
import { useCreateCredential } from '@/lib/api/mutations';
import { FormField } from '@/components/ui/form-field';
import { Select } from '@/components/ui/select';
import { SecretInput } from '@/components/ui/secret-input';
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/card';
import {
  credentialCreateSchema,
  type CredentialCreateFormValues,
} from '@/lib/forms/schemas';
import type { CredentialType, CreateCredentialRequest } from '@/types/domain';

const CREDENTIAL_TYPE_OPTIONS = [
  { value: 'ssh_private_key', label: 'SSH Private Key (with optional passphrase)' },
  { value: 'ssh_password', label: 'SSH Password (password authentication)' },
  { value: 'cpanel_api_token', label: 'cPanel API Token (token authentication)' },
  { value: 'cpanel_password', label: 'cPanel Account Password' },
  { value: 's3_credentials', label: 'AWS / S3 Storage Credentials (Key + Secret)' },
];

export default function NewCredentialPage() {
  const router = useRouter();
  const { canManageCredentials } = usePermissions();
  const createCredential = useCreateCredential();

  const {
    register,
    handleSubmit,
    watch,
    control,
    reset,
    formState: { errors, isDirty, isSubmitting },
  } = useForm<CredentialCreateFormValues>({
    resolver: zodResolver(credentialCreateSchema),
    defaultValues: {
      name: '',
      type: 'ssh_private_key',
      password: '',
      private_key: '',
      passphrase: '',
      api_token: '',
      access_key_id: '',
      secret_access_key: '',
      session_token: '',
    },
  });

  const selectedType = watch('type');

  const { bypassGuard, safeNavigate } = useUnsavedChanges(isDirty);

  const clearSecrets = React.useCallback(() => {
    reset({
      name: '',
      type: 'ssh_private_key',
      password: '',
      private_key: '',
      passphrase: '',
      api_token: '',
      access_key_id: '',
      secret_access_key: '',
      session_token: '',
    });
  }, [reset]);

  useTenantFormGuard({
    onTenantChanged: () => {
      clearSecrets();
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

  const onSubmit = async (values: CredentialCreateFormValues) => {
    const payload: CreateCredentialRequest = {
      name: values.name.trim(),
      type: values.type as CredentialType,
    };

    if (values.type === 'ssh_private_key') {
      payload.secret = values.private_key?.trim();
      if (values.passphrase) {
        payload.passphrase = values.passphrase;
      }
    } else if (values.type === 'ssh_password' || values.type === 'cpanel_password') {
      payload.secret = values.password;
    } else if (values.type === 'cpanel_api_token') {
      payload.secret = values.api_token;
    } else if (values.type === 's3_credentials') {
      payload.access_key_id = values.access_key_id?.trim();
      payload.secret_access_key = values.secret_access_key?.trim();
      if (values.session_token?.trim()) {
        payload.session_token = values.session_token.trim();
      }
    }

    try {
      bypassGuard();
      clearSecrets();
      await createCredential.mutateWithSecret(payload);
      router.push('/credentials');
    } catch {
      // toast shown
    }
  };

  return (
    <div className="max-w-2xl space-y-6">
      <div className="flex items-center gap-3">
        <button
          type="button"
          onClick={() => safeNavigate('/credentials')}
          className="rounded-md p-1 text-muted-foreground hover:bg-muted hover:text-foreground transition-colors"
          aria-label="Back to credentials"
        >
          <ArrowLeft className="h-5 w-5" />
        </button>
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
          <form onSubmit={handleSubmit(onSubmit)} className="space-y-4">
            <FormField
              label="Credential Name"
              htmlFor="cred-name"
              required
              error={errors.name?.message}
            >
              <input
                id="cred-name"
                type="text"
                {...register('name')}
                placeholder="e.g. Production Web Bastion SSH Key"
                aria-invalid={Boolean(errors.name)}
                className="w-full rounded-md border border-input bg-background px-3 py-2 text-sm text-foreground placeholder:text-muted-foreground focus:outline-none focus:ring-2 focus:ring-ring"
              />
            </FormField>

            <FormField
              label="Credential Type"
              htmlFor="cred-type"
              required
              error={errors.type?.message}
            >
              <Controller
                name="type"
                control={control}
                render={({ field }) => (
                  <Select
                    id="cred-type"
                    value={field.value}
                    onChange={(e) => field.onChange(e.target.value as CredentialType)}
                    options={CREDENTIAL_TYPE_OPTIONS}
                  />
                )}
              />
            </FormField>

            {/* Dynamic Type-Specific Secret Fields */}
            <div className="pt-2 border-t space-y-4">
              <h3 className="text-xs font-semibold text-muted-foreground uppercase tracking-wider">
                Secret Material (Write-Only)
              </h3>

              {selectedType === 'ssh_password' && (
                <FormField
                  label="SSH Password"
                  htmlFor="ssh-password"
                  required
                  error={errors.password?.message}
                >
                  <Controller
                    name="password"
                    control={control}
                    render={({ field }) => (
                      <SecretInput
                        id="ssh-password"
                        value={field.value || ''}
                        onChange={field.onChange}
                        placeholder="Enter server password"
                      />
                    )}
                  />
                </FormField>
              )}

              {selectedType === 'ssh_private_key' && (
                <>
                  <FormField
                    label="Private Key (PEM Format)"
                    htmlFor="ssh-key"
                    required
                    error={errors.private_key?.message}
                    description="Paste your OpenSSH or RSA private key including the BEGIN and END header lines"
                  >
                    <textarea
                      id="ssh-key"
                      rows={5}
                      {...register('private_key')}
                      placeholder="-----BEGIN OPENSSH PRIVATE KEY-----&#10;...&#10;-----END OPENSSH PRIVATE KEY-----"
                      aria-invalid={Boolean(errors.private_key)}
                      className="w-full rounded-md border border-input bg-background px-3 py-2 font-mono text-xs text-foreground placeholder:text-muted-foreground focus:outline-none focus:ring-2 focus:ring-ring"
                    />
                  </FormField>

                  <FormField
                    label="Key Passphrase"
                    htmlFor="ssh-passphrase"
                    description="Leave blank if your private key is not protected with a passphrase"
                  >
                    <Controller
                      name="passphrase"
                      control={control}
                      render={({ field }) => (
                        <SecretInput
                          id="ssh-passphrase"
                          value={field.value || ''}
                          onChange={field.onChange}
                          placeholder="Passphrase (optional)"
                        />
                      )}
                    />
                  </FormField>
                </>
              )}

              {selectedType === 'cpanel_api_token' && (
                <FormField
                  label="cPanel API Token"
                  htmlFor="cpanel-token"
                  required
                  error={errors.api_token?.message}
                  description="Generate a user API token with backup privileges in cPanel > Security > Manage API Tokens"
                >
                  <Controller
                    name="api_token"
                    control={control}
                    render={({ field }) => (
                      <SecretInput
                        id="cpanel-token"
                        value={field.value || ''}
                        onChange={field.onChange}
                        placeholder="e.g. WHM9ABC123..."
                      />
                    )}
                  />
                </FormField>
              )}

              {selectedType === 'cpanel_password' && (
                <FormField
                  label="cPanel Account Password"
                  htmlFor="cpanel-password"
                  required
                  error={errors.password?.message}
                >
                  <Controller
                    name="password"
                    control={control}
                    render={({ field }) => (
                      <SecretInput
                        id="cpanel-password"
                        value={field.value || ''}
                        onChange={field.onChange}
                        placeholder="Account password"
                      />
                    )}
                  />
                </FormField>
              )}

              {selectedType === 's3_credentials' && (
                <>
                  <FormField
                    label="AWS Access Key ID"
                    htmlFor="s3-access-key"
                    required
                    error={errors.access_key_id?.message}
                  >
                    <input
                      id="s3-access-key"
                      type="text"
                      {...register('access_key_id')}
                      placeholder="e.g. AKIAIOSFODNN7EXAMPLE"
                      aria-invalid={Boolean(errors.access_key_id)}
                      className="w-full rounded-md border border-input bg-background px-3 py-2 font-mono text-xs text-foreground placeholder:text-muted-foreground focus:outline-none focus:ring-2 focus:ring-ring"
                    />
                  </FormField>

                  <FormField
                    label="AWS Secret Access Key"
                    htmlFor="s3-secret-key"
                    required
                    error={errors.secret_access_key?.message}
                  >
                    <Controller
                      name="secret_access_key"
                      control={control}
                      render={({ field }) => (
                        <SecretInput
                          id="s3-secret-key"
                          value={field.value || ''}
                          onChange={field.onChange}
                          placeholder="wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"
                        />
                      )}
                    />
                  </FormField>

                  <FormField
                    label="AWS Session Token (Optional)"
                    htmlFor="s3-session-token"
                    description="Provide only if using temporary STS security credentials"
                  >
                    <Controller
                      name="session_token"
                      control={control}
                      render={({ field }) => (
                        <SecretInput
                          id="s3-session-token"
                          value={field.value || ''}
                          onChange={field.onChange}
                          placeholder="Temporary STS token (optional)"
                        />
                      )}
                    />
                  </FormField>
                </>
              )}
            </div>

            <div className="flex justify-end gap-3 pt-4 border-t">
              <button
                type="button"
                onClick={() => safeNavigate('/credentials')}
                className="rounded-md border border-input bg-background px-4 py-2 text-sm font-medium hover:bg-accent hover:text-accent-foreground transition-colors"
              >
                Cancel
              </button>
              <button
                type="submit"
                disabled={isSubmitting || createCredential.isPending}
                className="inline-flex items-center justify-center gap-2 rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground hover:bg-primary/90 transition-colors disabled:opacity-50"
              >
                {createCredential.isPending || isSubmitting ? 'Saving...' : 'Save Credential'}
              </button>
            </div>
          </form>
        </CardContent>
      </Card>
    </div>
  );
}
