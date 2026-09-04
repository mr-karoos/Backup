'use client';

import Link from 'next/link';
import { useParams } from 'next/navigation';
import { useQuery } from '@tanstack/react-query';
import { useAuth } from '@/lib/auth/auth-context';
import { apiClient } from '@/lib/api/api-client';
import { queryKeys } from '@/lib/query/query-client';
import { type BackupPlanResponse } from '@/types/domain';
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Skeleton } from '@/components/ui/skeleton';
import { ErrorState } from '@/components/ui/error-state';
import { formatDate, getStatusBadgeVariant } from '@/lib/format/formatters';
import { ArrowLeft, Calendar, Clock, Database, FolderArchive } from 'lucide-react';

export default function BackupPlanDetailPage() {
  const params = useParams();
  const id = params?.id as string;
  const { activeOrgId } = useAuth();

  const { data, isLoading, isError, error, refetch } = useQuery<BackupPlanResponse>({
    queryKey: activeOrgId && id ? queryKeys.org(activeOrgId).plans.detail(id) : ['disabled'],
    queryFn: () => apiClient.get<BackupPlanResponse>(`/backup-plans/${id}`),
    enabled: !!activeOrgId && !!id,
  });

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
        <Link href="/plans">
          <Button variant="ghost" size="sm" className="gap-2">
            <ArrowLeft className="h-4 w-4" />
            Back to Backup Plans
          </Button>
        </Link>
        <ErrorState
          title="Could not load backup plan details"
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
        <Link href="/plans" className="w-fit">
          <Button variant="ghost" size="sm" className="gap-2 -ml-2 text-muted-foreground hover:text-foreground">
            <ArrowLeft className="h-4 w-4" />
            Back to Backup Plans
          </Button>
        </Link>
        <div className="flex flex-wrap items-center justify-between gap-4">
          <div className="flex items-center gap-3">
            <div className="flex h-10 w-10 items-center justify-center rounded-lg bg-primary/10 text-primary">
              <Calendar className="h-5 w-5" />
            </div>
            <div>
              <h1 className="text-2xl font-bold tracking-tight text-foreground">{data.name}</h1>
              <p className="text-xs font-mono text-muted-foreground">ID: {data.id}</p>
            </div>
          </div>
          <Badge variant={variant} className="capitalize text-sm px-3 py-1">
            {label}
          </Badge>
        </div>
      </div>

      {/* Detail Cards */}
      <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
        {/* Core Configuration Card */}
        <Card>
          <CardHeader>
            <CardTitle className="text-base font-semibold flex items-center gap-2">
              <Calendar className="h-4 w-4 text-muted-foreground" />
              Plan Specifications
            </CardTitle>
          </CardHeader>
          <CardContent className="space-y-4 text-sm">
            <div className="grid grid-cols-2 gap-2 border-b pb-3">
              <span className="text-muted-foreground">Attached Resource</span>
              <span className="font-medium text-foreground">
                {data.resource_name || '—'}
              </span>
            </div>
            <div className="grid grid-cols-2 gap-2 border-b pb-3">
              <span className="text-muted-foreground">Backup Type</span>
              <span className="font-medium capitalize text-foreground">
                {data.backup_type.replace(/_/g, ' ')}
              </span>
            </div>
            <div className="grid grid-cols-2 gap-2 border-b pb-3">
              <span className="text-muted-foreground">Engine Type</span>
              <span className="font-mono font-medium text-foreground">
                {data.engine_type}
              </span>
            </div>
            <div className="grid grid-cols-2 gap-2 border-b pb-3">
              <span className="text-muted-foreground">Storage Target ID</span>
              <span className="font-mono text-xs text-foreground truncate">
                {data.storage_target_id}
              </span>
            </div>
            <div className="grid grid-cols-2 gap-2">
              <span className="text-muted-foreground">Created</span>
              <span className="font-medium text-foreground">{formatDate(data.created_at)}</span>
            </div>
          </CardContent>
        </Card>

        {/* Schedule & Retention Card */}
        <Card>
          <CardHeader>
            <CardTitle className="text-base font-semibold flex items-center gap-2">
              <Clock className="h-4 w-4 text-muted-foreground" />
              Schedule & Retention
            </CardTitle>
          </CardHeader>
          <CardContent className="space-y-4 text-sm">
            <div className="grid grid-cols-2 gap-2 border-b pb-3">
              <span className="text-muted-foreground">Automated Schedule</span>
              <span className="font-medium text-foreground">
                {data.schedule.is_enabled ? 'Enabled' : 'Disabled'}
              </span>
            </div>
            <div className="grid grid-cols-2 gap-2 border-b pb-3">
              <span className="text-muted-foreground">Cron Expression</span>
              <span className="font-mono font-medium text-foreground">
                {data.schedule.cron_expression || '—'}
              </span>
            </div>
            <div className="grid grid-cols-2 gap-2 border-b pb-3">
              <span className="text-muted-foreground">Timezone</span>
              <span className="font-medium text-foreground">{data.schedule.timezone}</span>
            </div>
            <div className="grid grid-cols-2 gap-2 border-b pb-3">
              <span className="text-muted-foreground">Next Scheduled Run</span>
              <span className="font-medium text-foreground">
                {data.schedule.next_run_at ? formatDate(data.schedule.next_run_at) : 'Calculated by scheduler'}
              </span>
            </div>
            <div className="grid grid-cols-2 gap-2">
              <span className="text-muted-foreground">Retention Policy</span>
              <span className="font-medium text-foreground">
                {data.retention_policy
                  ? `${data.retention_policy.keep_last_n ?? '—'} copies, ${data.retention_policy.keep_days ?? '—'} days`
                  : 'Standard'}
              </span>
            </div>
          </CardContent>
        </Card>

        {/* Target Scope Card (Full width on md+) */}
        <Card className="md:col-span-2">
          <CardHeader>
            <CardTitle className="text-base font-semibold flex items-center gap-2">
              {data.backup_type === 'mysql_database' ? (
                <Database className="h-4 w-4 text-muted-foreground" />
              ) : (
                <FolderArchive className="h-4 w-4 text-muted-foreground" />
              )}
              Target Selection Details
            </CardTitle>
          </CardHeader>
          <CardContent className="space-y-4 text-sm">
            {data.database_selection ? (
              <div>
                <span className="text-muted-foreground block text-xs mb-1">
                  Database Selection Mode: <span className="font-medium uppercase text-foreground">{data.database_selection.mode}</span>
                </span>
                {data.database_selection.databases && data.database_selection.databases.length > 0 ? (
                  <div className="flex flex-wrap gap-1.5 mt-2">
                    {data.database_selection.databases.map((dbName) => (
                      <span
                        key={dbName}
                        className="rounded bg-muted px-2.5 py-1 text-xs font-mono text-foreground"
                      >
                        {dbName}
                      </span>
                    ))}
                  </div>
                ) : (
                  <p className="text-xs text-muted-foreground italic">All discovered databases are backed up.</p>
                )}
              </div>
            ) : data.file_selection ? (
              <div className="space-y-3">
                <div>
                  <span className="text-xs text-muted-foreground block mb-1">Included Paths:</span>
                  <div className="flex flex-wrap gap-1.5">
                    {data.file_selection.paths.map((p) => (
                      <span key={p} className="rounded bg-muted px-2.5 py-1 text-xs font-mono text-foreground">
                        {p}
                      </span>
                    ))}
                  </div>
                </div>
                {data.file_selection.exclude_patterns && data.file_selection.exclude_patterns.length > 0 && (
                  <div>
                    <span className="text-xs text-muted-foreground block mb-1">Excluded Patterns:</span>
                    <div className="flex flex-wrap gap-1.5">
                      {data.file_selection.exclude_patterns.map((pat) => (
                        <span key={pat} className="rounded bg-destructive/10 text-destructive px-2.5 py-1 text-xs font-mono">
                          {pat}
                        </span>
                      ))}
                    </div>
                  </div>
                )}
              </div>
            ) : (
              <p className="text-xs text-muted-foreground">Standard backup target specification applies.</p>
            )}
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
