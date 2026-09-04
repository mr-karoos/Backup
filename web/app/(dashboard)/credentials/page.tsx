'use client';

import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { useAuth } from '@/lib/auth/auth-context';
import { apiClient } from '@/lib/api/api-client';
import { queryKeys } from '@/lib/query/query-client';
import { type CredentialListItemResponse } from '@/types/domain';
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
import { Skeleton } from '@/components/ui/skeleton';
import { ErrorState } from '@/components/ui/error-state';
import { EmptyState } from '@/components/ui/empty-state';
import { formatDate } from '@/lib/format/formatters';
import { KeyRound, Shield, Eye, Lock } from 'lucide-react';

export default function CredentialsPage() {
  const { activeOrgId, userRole, isSystemAdmin } = useAuth();
  const [selectedCred, setSelectedCred] = useState<CredentialListItemResponse | null>(null);

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

  return (
    <div className="space-y-6">
      {/* Header */}
      <div>
        <h1 className="text-2xl font-bold tracking-tight text-foreground">Credentials Vault</h1>
        <p className="text-sm text-muted-foreground mt-0.5">
          Encrypted authentication secrets and keys for SSH servers and S3 storage
        </p>
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
                      <TableHead className="w-[80px]"></TableHead>
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
                          <Button
                            variant="ghost"
                            size="icon"
                            onClick={() => setSelectedCred(cred)}
                            className="h-8 w-8 text-muted-foreground hover:text-foreground"
                            aria-label={`View metadata for ${cred.name}`}
                          >
                            <Eye className="h-4 w-4" />
                          </Button>
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
                    onClick={() => setSelectedCred(cred)}
                    className="p-4 hover:bg-muted/30 transition-colors cursor-pointer space-y-1"
                  >
                    <div className="flex items-center justify-between">
                      <div className="flex items-center gap-2 font-medium">
                        <KeyRound className="h-4 w-4 text-muted-foreground shrink-0" />
                        <span className="truncate text-foreground">{cred.name}</span>
                      </div>
                      <Badge variant="outline" className="capitalize text-[10px]">
                        {cred.type.replace(/_/g, ' ')}
                      </Badge>
                    </div>
                    <div className="flex items-center justify-between text-xs text-muted-foreground">
                      <span>Version: v{cred.key_version}</span>
                      <span>{formatDate(cred.created_at)}</span>
                    </div>
                  </div>
                ))}
              </div>
            </>
          )}
        </CardContent>
      </Card>

      {/* Safe Metadata Detail Dialog */}
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
    </div>
  );
}
