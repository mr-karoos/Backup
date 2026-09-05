'use client';

import * as React from 'react';
import Link from 'next/link';
import { useParams, useRouter } from 'next/navigation';
import { useQuery } from '@tanstack/react-query';
import { useAuth } from '@/lib/auth/auth-context';
import { apiClient } from '@/lib/api/api-client';
import { queryKeys } from '@/lib/query/query-client';
import { usePermissions } from '@/lib/auth/permissions';
import {
  useTestResourceConnection,
  useDiscoverDatabases,
  useArchiveResource,
} from '@/lib/api/mutations';
import { type ResourceResponse, type DiscoveredDatabaseResponse } from '@/types/domain';
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Skeleton } from '@/components/ui/skeleton';
import { ErrorState } from '@/components/ui/error-state';
import { ConfirmDialog } from '@/components/ui/confirm-dialog';
import { ManualBackupDialog } from '@/components/backup/manual-backup-dialog';
import {
  Table,
  TableHeader,
  TableBody,
  TableRow,
  TableHead,
  TableCell,
} from '@/components/ui/table';
import { formatDate, formatBytes, formatResourceType, getStatusBadgeVariant } from '@/lib/format/formatters';
import {
  ArrowLeft,
  Server,
  Shield,
  Network,
  Activity,
  Database,
  Play,
  Pencil,
  Archive,
  CheckCircle2,
  XCircle,
} from 'lucide-react';

export default function ResourceDetailPage() {
  const params = useParams();
  const id = params?.id as string;
  const router = useRouter();
  const { activeOrgId } = useAuth();
  const {
    canTestConnection,
    canDiscoverDatabases,
    canExecuteAdHocBackup,
    canEditResource,
    canArchiveResource,
  } = usePermissions();

  const [backupDialogOpen, setBackupDialogOpen] = React.useState(false);
  const [archiveDialogOpen, setArchiveDialogOpen] = React.useState(false);
  const [discoveredDbs, setDiscoveredDbs] = React.useState<DiscoveredDatabaseResponse[] | null>(null);

  const testConn = useTestResourceConnection();
  const discoverDbs = useDiscoverDatabases();
  const archiveResource = useArchiveResource();

  const { data, isLoading, isError, error, refetch } = useQuery<ResourceResponse>({
    queryKey: activeOrgId && id ? queryKeys.org(activeOrgId).resources.detail(id) : ['disabled'],
    queryFn: () => apiClient.get<ResourceResponse>(`/resources/${id}`),
    enabled: !!activeOrgId && !!id,
  });

  const handleTestConnection = async () => {
    await testConn.mutateAsync(id);
    refetch();
  };

  const handleDiscoverDatabases = async () => {
    const dbs = await discoverDbs.mutateAsync(id);
    setDiscoveredDbs(dbs);
  };

  const handleArchive = async () => {
    await archiveResource.mutateAsync(id);
    setArchiveDialogOpen(false);
    router.push('/resources');
  };

  if (isLoading) {
    return (
      <div className="space-y-6">
        <Skeleton className="h-8 w-48" />
        <Skeleton className="h-48 w-full" />
        <Skeleton className="h-48 w-full" />
      </div>
    );
  }

  if (isError || !data) {
    return (
      <div className="space-y-6">
        <Link href="/resources">
          <Button variant="ghost" size="sm" className="gap-2">
            <ArrowLeft className="h-4 w-4" />
            Back to Resources
          </Button>
        </Link>
        <ErrorState
          title="Could not load resource details"
          error={error}
          onRetry={() => refetch()}
        />
      </div>
    );
  }

  const { label, variant } = getStatusBadgeVariant(data.status);

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex flex-col gap-2">
        <Link href="/resources" className="w-fit">
          <Button variant="ghost" size="sm" className="gap-2 -ml-2 text-muted-foreground hover:text-foreground">
            <ArrowLeft className="h-4 w-4" />
            Back to Resources
          </Button>
        </Link>
        <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
          <div className="flex items-center gap-3">
            <div className="flex h-10 w-10 items-center justify-center rounded-lg bg-primary/10 text-primary">
              <Server className="h-5 w-5" />
            </div>
            <div>
              <div className="flex items-center gap-2">
                <h1 className="text-2xl font-bold tracking-tight text-foreground">{data.name}</h1>
                <Badge variant={variant} className="capitalize text-xs px-2.5 py-0.5">
                  {label}
                </Badge>
              </div>
              <p className="text-xs font-mono text-muted-foreground mt-0.5">ID: {data.id}</p>
            </div>
          </div>

          {/* Action Buttons */}
          <div className="flex flex-wrap items-center gap-2">
            {canTestConnection && (
              <Button
                variant="outline"
                size="sm"
                onClick={handleTestConnection}
                disabled={testConn.isPending}
                className="gap-1.5"
              >
                <Activity className="h-4 w-4 text-emerald-500" />
                {testConn.isPending ? 'Testing...' : 'Test Connection'}
              </Button>
            )}

            {canDiscoverDatabases && data.type === 'ubuntu_ssh' && (
              <Button
                variant="outline"
                size="sm"
                onClick={handleDiscoverDatabases}
                disabled={discoverDbs.isPending}
                className="gap-1.5"
              >
                <Database className="h-4 w-4 text-sky-500" />
                {discoverDbs.isPending ? 'Scanning...' : 'Discover Databases'}
              </Button>
            )}

            {canExecuteAdHocBackup && (
              <Button
                size="sm"
                onClick={() => setBackupDialogOpen(true)}
                className="gap-1.5 bg-emerald-600 hover:bg-emerald-500 text-white"
              >
                <Play className="h-4 w-4 fill-current" />
                Run Backup
              </Button>
            )}

            {canEditResource && (
              <Link href={`/resources/${data.id}/edit`}>
                <Button variant="outline" size="sm" className="gap-1.5">
                  <Pencil className="h-4 w-4" />
                  Edit
                </Button>
              </Link>
            )}

            {canArchiveResource && data.status !== 'archived' && (
              <Button
                variant="ghost"
                size="sm"
                onClick={() => setArchiveDialogOpen(true)}
                className="gap-1.5 text-rose-500 hover:text-rose-400 hover:bg-rose-950/20"
              >
                <Archive className="h-4 w-4" />
                Archive
              </Button>
            )}
          </div>
        </div>
      </div>

      {/* Connection Test Live Result Banner */}
      {testConn.data && (
        <div
          className={`rounded-lg border p-4 text-sm flex items-center justify-between ${
            testConn.data.status === 'success'
              ? 'border-emerald-800/50 bg-emerald-950/20 text-emerald-300'
              : 'border-rose-800/50 bg-rose-950/20 text-rose-300'
          }`}
        >
          <div className="flex items-center gap-2">
            {testConn.data.status === 'success' ? (
              <CheckCircle2 className="h-5 w-5 text-emerald-400 shrink-0" />
            ) : (
              <XCircle className="h-5 w-5 text-rose-400 shrink-0" />
            )}
            <div>
              <p className="font-semibold">
                {testConn.data.status === 'success'
                  ? 'Connection Test Successful'
                  : 'Connection Test Failed'}
              </p>
              <p className="text-xs opacity-80">
                Latency: {testConn.data.latency_ms}ms • Checked: {formatDate(testConn.data.checked_at)}
              </p>
            </div>
          </div>
        </div>
      )}

      {/* Grid Content */}
      <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
        {/* Basic Metadata Card */}
        <Card>
          <CardHeader>
            <CardTitle className="text-base font-semibold flex items-center gap-2">
              <Server className="h-4 w-4 text-muted-foreground" />
              Resource Information
            </CardTitle>
          </CardHeader>
          <CardContent className="space-y-4 text-sm">
            <div className="grid grid-cols-2 gap-2 border-b pb-3">
              <span className="text-muted-foreground">Resource Type</span>
              <span className="font-medium text-foreground">
                {formatResourceType(data.type)}
              </span>
            </div>
            <div className="grid grid-cols-2 gap-2 border-b pb-3">
              <span className="text-muted-foreground">Status</span>
              <span className="font-medium capitalize text-foreground">{data.status}</span>
            </div>
            <div className="grid grid-cols-2 gap-2 border-b pb-3">
              <span className="text-muted-foreground">Last Connection Test</span>
              <span className="font-medium text-foreground">
                {data.last_connection_test_at ? formatDate(data.last_connection_test_at) : 'Never'}
              </span>
            </div>
            <div className="grid grid-cols-2 gap-2 border-b pb-3">
              <span className="text-muted-foreground">Connection Status</span>
              <span className="font-medium capitalize text-foreground">
                {data.last_connection_status || 'Unknown'}
              </span>
            </div>
            <div className="grid grid-cols-2 gap-2">
              <span className="text-muted-foreground">Registered At</span>
              <span className="font-medium text-foreground">{formatDate(data.created_at)}</span>
            </div>
          </CardContent>
        </Card>

        {/* Connector Details Card */}
        <Card>
          <CardHeader>
            <CardTitle className="text-base font-semibold flex items-center gap-2">
              <Network className="h-4 w-4 text-muted-foreground" />
              Connector Configuration
            </CardTitle>
          </CardHeader>
          <CardContent className="space-y-4 text-sm">
            {data.connector ? (
              <>
                <div className="grid grid-cols-2 gap-2 border-b pb-3">
                  <span className="text-muted-foreground">Host / Port</span>
                  <span className="font-mono font-medium text-foreground">
                    {data.connector.host || '—'}:{data.connector.port || '—'}
                  </span>
                </div>
                {data.connector.auth_type && (
                  <div className="grid grid-cols-2 gap-2 border-b pb-3">
                    <span className="text-muted-foreground">Authentication Type</span>
                    <span className="font-medium capitalize text-foreground">
                      {data.connector.auth_type.replace(/_/g, ' ')}
                    </span>
                  </div>
                )}
                {data.connector.username && (
                  <div className="grid grid-cols-2 gap-2 border-b pb-3">
                    <span className="text-muted-foreground">Username</span>
                    <span className="font-mono text-foreground">{data.connector.username}</span>
                  </div>
                )}
                {data.connector.credential_name && (
                  <div className="grid grid-cols-2 gap-2 border-b pb-3">
                    <span className="text-muted-foreground">Attached Credential</span>
                    <span className="font-medium text-foreground">
                      {data.connector.credential_name}
                    </span>
                  </div>
                )}
                {data.connector.host_key_fingerprint && (
                  <div className="space-y-1">
                    <span className="text-xs text-muted-foreground block">Host Key Fingerprint</span>
                    <span className="font-mono text-xs bg-muted p-2 rounded block break-all text-foreground">
                      {data.connector.host_key_fingerprint}
                    </span>
                  </div>
                )}
              </>
            ) : (
              <div className="py-6 text-center text-muted-foreground">
                <Shield className="h-8 w-8 mx-auto mb-2 opacity-40" />
                <p>Connector details are restricted based on your role permissions.</p>
              </div>
            )}
          </CardContent>
        </Card>
      </div>

      {/* Discovered Databases Section (if scanned) */}
      {discoveredDbs && (
        <Card>
          <CardHeader>
            <CardTitle className="text-base font-semibold flex items-center gap-2">
              <Database className="h-4 w-4 text-primary" />
              Discovered Databases ({discoveredDbs.length})
            </CardTitle>
          </CardHeader>
          <CardContent className="p-0">
            {discoveredDbs.length === 0 ? (
              <div className="p-6 text-center text-sm text-muted-foreground">
                No active MySQL databases discovered on this host.
              </div>
            ) : (
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Database Name</TableHead>
                    <TableHead>Approximate Size</TableHead>
                    <TableHead>Tables Count</TableHead>
                    <TableHead>Status</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {discoveredDbs.map((db) => (
                    <TableRow key={db.name}>
                      <TableCell className="font-mono font-medium text-foreground">
                        {db.name}
                      </TableCell>
                      <TableCell className="text-xs text-muted-foreground">
                        {formatBytes(db.size_bytes)}
                      </TableCell>
                      <TableCell className="text-xs text-muted-foreground">
                        {db.tables_count !== null ? db.tables_count : '—'}
                      </TableCell>
                      <TableCell>
                        <Badge variant="outline" className="text-[10px] capitalize">
                          {db.status}
                        </Badge>
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            )}
          </CardContent>
        </Card>
      )}

      {/* Manual Ad-Hoc Backup Dialog */}
      <ManualBackupDialog
        open={backupDialogOpen}
        onOpenChange={setBackupDialogOpen}
        resource={{ id: data.id, name: data.name }}
      />

      {/* Archive Resource Confirmation Dialog */}
      <ConfirmDialog
        open={archiveDialogOpen}
        onOpenChange={setArchiveDialogOpen}
        title="Archive Resource"
        description="Are you sure you want to archive this resource? Existing backups and plans will remain intact, but new backups cannot be scheduled."
        objectName={data.name}
        confirmText="Archive Resource"
        destructive
        isLoading={archiveResource.isPending}
        onConfirm={handleArchive}
      />
    </div>
  );
}
