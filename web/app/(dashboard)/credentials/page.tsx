'use client';

import { useState } from 'react';
import Link from 'next/link';
import { useQuery } from '@tanstack/react-query';
import { useAuth } from '@/lib/auth/auth-context';
import { apiClient } from '@/lib/api/api-client';
import { queryKeys } from '@/lib/query/query-client';
import { usePermissions } from '@/lib/auth/permissions';
import { useDeleteCredential, useUpdateCredential } from '@/lib/api/mutations';
import { type CredentialListItemResponse, type UpdateCredentialRequest } from '@/types/domain';
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import {
  Table,
  TableHeader,
  TableBody,
  TableRow,
  TableHead,
  TableCell,
} from '@/components/ui/table';
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from '@/components/ui/dialog';
import { ConfirmDialog } from '@/components/ui/confirm-dialog';
import { FormField } from '@/components/ui/form-field';
import { SecretInput } from '@/components/ui/secret-input';
import { Skeleton } from '@/components/ui/skeleton';
import { ErrorState } from '@/components/ui/error-state';
import { EmptyState } from '@/components/ui/empty-state';
import { formatDate } from '@/lib/format/formatters';
import { KeyRound, Shield, Eye, Lock, Plus, Pencil, Trash2 } from 'lucide-react';

export default function CredentialsPage() {
  const { activeOrgId, userRole, isSystemAdmin } = useAuth();
  const { canManageCredentials } = usePermissions();
  const [selectedCred, setSelectedCred] = useState<CredentialListItemResponse | null>(null);
  const [editingCred, setEditingCred] = useState<CredentialListItemResponse | null>(null);
  const [deletingCred, setDeletingCred] = useState<CredentialListItemResponse | null>(null);

  // Edit form state
  const [editName, setEditName] = useState('');
  const [editPassword, setEditPassword] = useState('');
  const [editPrivateKey, setEditPrivateKey] = useState('');
  const [editPassphrase, setEditPassphrase] = useState('');
  const [editApiToken, setEditApiToken] = useState('');
  const [editAccessKeyId, setEditAccessKeyId] = useState('');
  const [editSecretAccessKey, setEditSecretAccessKey] = useState('');

  const deleteCred = useDeleteCredential();
  const updateCred = useUpdateCredential();

  const isAdmin = userRole === 'admin' || isSystemAdmin;

  const { data, isLoading, isError, error, refetch } = useQuery<CredentialListItemResponse[]>({
    queryKey: activeOrgId ? queryKeys.org(activeOrgId).credentials.all() : ['disabled'],
    queryFn: () => apiClient.get<CredentialListItemResponse[]>('/credentials'),
    enabled: !!activeOrgId && isAdmin,
  });

  // Access Control Guard
  if (!isAdmin) {
    return (
      <div className="space-y-6">
        <h1 className="text-2xl font-bold tracking-tight text-foreground">Credentials Vault</h1>
        <Card className="border-destructive/20 bg-destructive/5 p-6 text-center">
          <div className="flex flex-col items-center space-y-3">
            <div className="flex h-12 w-12 items-center justify-center rounded-full bg-destructive/10 text-destructive">
              <Shield className="h-6 w-6" />
            </div>
            <h3 className="text-base font-semibold text-foreground">Access Restricted</h3>
            <p className="text-sm text-muted-foreground max-w-md">
              Only Organization Administrators and System Administrators are authorized to view credentials metadata.
            </p>
          </div>
        </Card>
      </div>
    );
  }

  const credentials = data || [];

  const handleOpenEdit = (cred: CredentialListItemResponse) => {
    setEditingCred(cred);
    setEditName(cred.name);
    setEditPassword('');
    setEditPrivateKey('');
    setEditPassphrase('');
    setEditApiToken('');
    setEditAccessKeyId('');
    setEditSecretAccessKey('');
  };

  const handleUpdate = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!editingCred) return;

    const payload: UpdateCredentialRequest = {
      name: editName.trim() || undefined,
    };

    if (editingCred.type === 'ssh_password' && editPassword) {
      payload.secret = editPassword;
    } else if (editingCred.type === 'ssh_private_key' && editPrivateKey) {
      payload.secret = editPrivateKey.trim();
      if (editPassphrase) {
        payload.passphrase = editPassphrase;
      }
    } else if (editingCred.type === 'cpanel_api_token' && editApiToken) {
      payload.secret = editApiToken;
    } else if (editingCred.type === 'cpanel_password' && editPassword) {
      payload.secret = editPassword;
    } else if (editingCred.type === 's3_credentials' && editAccessKeyId && editSecretAccessKey) {
      payload.access_key_id = editAccessKeyId.trim();
      payload.secret_access_key = editSecretAccessKey.trim();
    }

    await updateCred.mutateAsync({ id: editingCred.id, data: payload });
    setEditingCred(null);
  };

  const handleDelete = async () => {
    if (!deletingCred) return;
    await deleteCred.mutateAsync(deletingCred.id);
    setDeletingCred(null);
  };

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h1 className="text-2xl font-bold tracking-tight text-foreground">Credentials Vault</h1>
          <p className="text-sm text-muted-foreground mt-0.5">
            Encrypted authentication secrets and keys for SSH servers and S3 storage
          </p>
        </div>
        {canManageCredentials && (
          <Link
            href="/credentials/new"
            className="inline-flex items-center justify-center gap-2 rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground hover:bg-primary/90 transition-colors"
          >
            <Plus className="h-4 w-4" /> Add Credential
          </Link>
        )}
      </div>

      {/* Main Content */}
      <Card>
        <CardHeader className="pb-3">
          <CardTitle className="text-base font-semibold">Registered Credentials</CardTitle>
        </CardHeader>
        <CardContent className="p-0">
          {isLoading ? (
            <div className="p-6 space-y-4">
              <Skeleton className="h-10 w-full" />
              <Skeleton className="h-10 w-full" />
              <Skeleton className="h-10 w-full" />
            </div>
          ) : isError ? (
            <div className="p-6">
              <ErrorState
                title="Could not load credentials"
                error={error}
                onRetry={() => refetch()}
              />
            </div>
          ) : credentials.length === 0 ? (
            <div className="p-6">
              <EmptyState
                icon={KeyRound}
                title="No credentials configured"
                description="Secure credentials will appear here once saved by an administrator."
              />
            </div>
          ) : (
            <>
              {/* Desktop Table */}
              <div className="hidden md:block">
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>Credential Name</TableHead>
                      <TableHead>Type</TableHead>
                      <TableHead>Fingerprint</TableHead>
                      <TableHead>Key Version</TableHead>
                      <TableHead>Created</TableHead>
                      <TableHead className="w-[120px] text-right">Actions</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {credentials.map((cred) => (
                      <TableRow key={cred.id}>
                        <TableCell className="font-medium text-foreground">
                          <button
                            onClick={() => setSelectedCred(cred)}
                            className="hover:underline flex items-center gap-2 text-left"
                          >
                            <KeyRound className="h-4 w-4 text-muted-foreground shrink-0" />
                            {cred.name}
                          </button>
                        </TableCell>
                        <TableCell className="capitalize text-xs font-mono">
                          {cred.type.replace(/_/g, ' ')}
                        </TableCell>
                        <TableCell className="font-mono text-xs text-muted-foreground">
                          {cred.fingerprint ? `${cred.fingerprint.slice(0, 16)}...` : '—'}
                        </TableCell>
                        <TableCell className="text-xs text-muted-foreground font-mono">
                          v{cred.key_version}
                        </TableCell>
                        <TableCell className="text-xs text-muted-foreground">
                          {formatDate(cred.created_at)}
                        </TableCell>
                        <TableCell className="text-right">
                          <div className="flex items-center justify-end gap-1">
                            <Button
                              variant="ghost"
                              size="icon"
                              onClick={() => setSelectedCred(cred)}
                              className="h-8 w-8 text-muted-foreground hover:text-foreground"
                              aria-label={`View metadata for ${cred.name}`}
                            >
                              <Eye className="h-4 w-4" />
                            </Button>
                            {canManageCredentials && (
                              <>
                                <Button
                                  variant="ghost"
                                  size="icon"
                                  onClick={() => handleOpenEdit(cred)}
                                  className="h-8 w-8 text-muted-foreground hover:text-foreground"
                                  aria-label={`Edit ${cred.name}`}
                                >
                                  <Pencil className="h-4 w-4" />
                                </Button>
                                <Button
                                  variant="ghost"
                                  size="icon"
                                  onClick={() => setDeletingCred(cred)}
                                  className="h-8 w-8 text-rose-500 hover:text-rose-400 hover:bg-rose-950/20"
                                  aria-label={`Delete ${cred.name}`}
                                >
                                  <Trash2 className="h-4 w-4" />
                                </Button>
                              </>
                            )}
                          </div>
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </div>

              {/* Mobile Stacked Cards */}
              <div className="md:hidden divide-y">
                {credentials.map((cred) => (
                  <div
                    key={cred.id}
                    className="p-4 hover:bg-muted/30 transition-colors space-y-2"
                  >
                    <div className="flex items-center justify-between">
                      <button
                        onClick={() => setSelectedCred(cred)}
                        className="flex items-center gap-2 font-medium hover:underline text-left"
                      >
                        <KeyRound className="h-4 w-4 text-muted-foreground shrink-0" />
                        <span className="truncate text-foreground">{cred.name}</span>
                      </button>
                      <Badge variant="outline" className="capitalize text-[10px]">
                        {cred.type.replace(/_/g, ' ')}
                      </Badge>
                    </div>
                    <div className="flex items-center justify-between text-xs text-muted-foreground">
                      <span>Version: v{cred.key_version}</span>
                      <span>{formatDate(cred.created_at)}</span>
                    </div>
                    {canManageCredentials && (
                      <div className="flex justify-end gap-2 pt-1">
                        <Button
                          variant="outline"
                          size="sm"
                          onClick={() => handleOpenEdit(cred)}
                          className="h-7 text-xs"
                        >
                          Edit
                        </Button>
                        <Button
                          variant="destructive"
                          size="sm"
                          onClick={() => setDeletingCred(cred)}
                          className="h-7 text-xs"
                        >
                          Delete
                        </Button>
                      </div>
                    )}
                  </div>
                ))}
              </div>
            </>
          )}
        </CardContent>
      </Card>

      {/* View Metadata Detail Dialog */}
      <Dialog open={!!selectedCred} onOpenChange={(open) => !open && setSelectedCred(null)}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <KeyRound className="h-5 w-5 text-primary" />
              Credential Metadata
            </DialogTitle>
            <DialogDescription>
              Public cryptographic attributes and administrative identifiers.
            </DialogDescription>
          </DialogHeader>

          {selectedCred && (
            <div className="space-y-4 text-sm py-2">
              <div className="grid grid-cols-2 gap-2 border-b pb-2.5">
                <span className="text-muted-foreground">Credential Name</span>
                <span className="font-semibold text-foreground">{selectedCred.name}</span>
              </div>
              <div className="grid grid-cols-2 gap-2 border-b pb-2.5">
                <span className="text-muted-foreground">Credential Type</span>
                <span className="font-mono capitalize text-foreground">
                  {selectedCred.type.replace(/_/g, ' ')}
                </span>
              </div>
              <div className="grid grid-cols-2 gap-2 border-b pb-2.5">
                <span className="text-muted-foreground">Master Key Version</span>
                <span className="font-mono text-foreground">v{selectedCred.key_version}</span>
              </div>
              <div className="grid grid-cols-2 gap-2 border-b pb-2.5">
                <span className="text-muted-foreground">Created At</span>
                <span className="text-foreground">{formatDate(selectedCred.created_at)}</span>
              </div>
              <div className="space-y-1 border-b pb-2.5">
                <span className="text-xs text-muted-foreground block">Credential ID</span>
                <span className="font-mono text-xs bg-muted p-2 rounded block break-all text-foreground">
                  {selectedCred.id}
                </span>
              </div>
              {selectedCred.fingerprint && (
                <div className="space-y-1">
                  <span className="text-xs text-muted-foreground block">Key Fingerprint</span>
                  <span className="font-mono text-xs bg-muted p-2 rounded block break-all text-foreground">
                    {selectedCred.fingerprint}
                  </span>
                </div>
              )}

              <div className="rounded-lg border bg-muted/30 p-3 flex items-start gap-2.5 text-xs text-muted-foreground">
                <Lock className="h-4 w-4 shrink-0 text-muted-foreground mt-0.5" />
                <p>
                  Secret material is encrypted at rest using AES-256-GCM envelope encryption and is never returned in plaintext by API queries.
                </p>
              </div>
            </div>
          )}
        </DialogContent>
      </Dialog>

      {/* Edit Credential Dialog */}
      <Dialog open={!!editingCred} onOpenChange={(open) => !open && setEditingCred(null)}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <Pencil className="h-5 w-5 text-primary" />
              Edit Credential
            </DialogTitle>
            <DialogDescription>
              Update credential name or replace secret material.
            </DialogDescription>
          </DialogHeader>

          {editingCred && (
            <form onSubmit={handleUpdate} className="space-y-4 py-2">
              <FormField label="Credential Name" htmlFor="edit-cred-name" required>
                <input
                  id="edit-cred-name"
                  type="text"
                  value={editName}
                  onChange={(e) => setEditName(e.target.value)}
                  className="w-full rounded-md border border-input bg-background px-3 py-2 text-sm text-foreground focus:outline-none focus:ring-2 focus:ring-ring"
                />
              </FormField>

              {/* Secret Replacement (Write-Only) */}
              <div className="pt-2 border-t space-y-3">
                <p className="text-xs font-semibold text-muted-foreground">
                  Replace Secret (Leave blank to keep existing secret)
                </p>

                {(editingCred.type === 'ssh_password' || editingCred.type === 'cpanel_password') && (
                  <FormField label="New Password" htmlFor="edit-password">
                    <SecretInput
                      id="edit-password"
                      value={editPassword}
                      onChange={(e) => setEditPassword(e.target.value)}
                      placeholder="Enter new password"
                    />
                  </FormField>
                )}

                {editingCred.type === 'ssh_private_key' && (
                  <>
                    <FormField label="New Private Key (PEM)" htmlFor="edit-pem">
                      <textarea
                        id="edit-pem"
                        rows={4}
                        value={editPrivateKey}
                        onChange={(e) => setEditPrivateKey(e.target.value)}
                        placeholder="-----BEGIN OPENSSH PRIVATE KEY-----"
                        className="w-full rounded-md border border-input bg-background px-3 py-2 font-mono text-xs text-foreground focus:outline-none focus:ring-2 focus:ring-ring"
                      />
                    </FormField>
                    <FormField label="New Passphrase (Optional)" htmlFor="edit-passphrase">
                      <SecretInput
                        id="edit-passphrase"
                        value={editPassphrase}
                        onChange={(e) => setEditPassphrase(e.target.value)}
                        placeholder="New passphrase"
                      />
                    </FormField>
                  </>
                )}

                {editingCred.type === 'cpanel_api_token' && (
                  <FormField label="New API Token" htmlFor="edit-token">
                    <SecretInput
                      id="edit-token"
                      value={editApiToken}
                      onChange={(e) => setEditApiToken(e.target.value)}
                      placeholder="Enter new API token"
                    />
                  </FormField>
                )}

                {editingCred.type === 's3_credentials' && (
                  <>
                    <FormField label="New Access Key ID" htmlFor="edit-access-key">
                      <input
                        id="edit-access-key"
                        type="text"
                        value={editAccessKeyId}
                        onChange={(e) => setEditAccessKeyId(e.target.value)}
                        placeholder="AKIA..."
                        className="w-full rounded-md border border-input bg-background px-3 py-2 font-mono text-xs text-foreground focus:outline-none focus:ring-2 focus:ring-ring"
                      />
                    </FormField>
                    <FormField label="New Secret Access Key" htmlFor="edit-secret-key">
                      <SecretInput
                        id="edit-secret-key"
                        value={editSecretAccessKey}
                        onChange={(e) => setEditSecretAccessKey(e.target.value)}
                        placeholder="New Secret Key"
                      />
                    </FormField>
                  </>
                )}
              </div>

              <div className="flex justify-end gap-3 pt-4 border-t">
                <Button
                  type="button"
                  variant="outline"
                  onClick={() => setEditingCred(null)}
                >
                  Cancel
                </Button>
                <Button
                  type="submit"
                  disabled={updateCred.isPending}
                >
                  {updateCred.isPending ? 'Saving...' : 'Save Changes'}
                </Button>
              </div>
            </form>
          )}
        </DialogContent>
      </Dialog>

      {/* Delete Confirmation Dialog */}
      <ConfirmDialog
        open={!!deletingCred}
        onOpenChange={(open) => !open && setDeletingCred(null)}
        title="Delete Credential"
        description="Are you sure you want to permanently delete this credential? This action cannot be undone."
        objectName={deletingCred?.name}
        confirmText="Delete Credential"
        destructive
        isLoading={deleteCred.isPending}
        onConfirm={handleDelete}
      />
    </div>
  );
}
