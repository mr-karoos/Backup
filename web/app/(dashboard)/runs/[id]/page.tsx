'use client';

import Link from 'next/link';
import { useParams } from 'next/navigation';
import { useQuery } from '@tanstack/react-query';
import { useAuth } from '@/lib/auth/auth-context';
import { apiClient } from '@/lib/api/api-client';
import { queryKeys } from '@/lib/query/query-client';
import { type BackupRunResponse } from '@/types/domain';
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Skeleton } from '@/components/ui/skeleton';
import { ErrorState } from '@/components/ui/error-state';
import {
  formatDate,
  formatDuration,
  formatBytes,
  getStatusBadgeVariant,
} from '@/lib/format/formatters';
import { ArrowLeft, History, AlertTriangle, Clock, HardDrive } from 'lucide-react';

export default function BackupRunDetailPage() {
  const params = useParams();
  const id = params?.id as string;
  const { activeOrgId } = useAuth();

  const { data, isLoading, isError, error, refetch } = useQuery<BackupRunResponse>({
    queryKey: activeOrgId && id ? queryKeys.org(activeOrgId).runs.detail(id) : ['disabled'],
    queryFn: () => apiClient.get<BackupRunResponse>(`/backup-runs/${id}`),
    enabled: !!activeOrgId && !!id,
    // Poll active run
    refetchInterval: (query) => {
      const run = query.state.data;
      return run?.status === 'running' || run?.status === 'pending' ? 3000 : false;
    },
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
        <Link href="/runs">
          <Button variant="ghost" size="sm" className="gap-2">
            <ArrowLeft className="h-4 w-4" />
            Back to Runs
          </Button>
        </Link>
        <ErrorState
          title="Could not load backup run details"
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
        <Link href="/runs" className="w-fit">
          <Button variant="ghost" size="sm" className="gap-2 -ml-2 text-muted-foreground hover:text-foreground">
            <ArrowLeft className="h-4 w-4" />
            Back to Runs
          </Button>
        </Link>
        <div className="flex flex-wrap items-center justify-between gap-4">
          <div className="flex items-center gap-3">
            <div className="flex h-10 w-10 items-center justify-center rounded-lg bg-primary/10 text-primary">
              <History className="h-5 w-5" />
            </div>
            <div>
              <h1 className="text-2xl font-bold tracking-tight text-foreground font-mono">
                Run {data.id}
              </h1>
              <p className="text-xs text-muted-foreground">Attempt #{data.attempt_number}</p>
            </div>
          </div>
          <Badge variant={variant} className="capitalize text-sm px-3 py-1">
            {label}
          </Badge>
        </div>
      </div>

      {/* Safe Error Alert if Failed */}
      {data.error_message && (
        <div className="rounded-lg border border-destructive/20 bg-destructive/5 p-4 space-y-2">
          <div className="flex items-center gap-2 text-destructive font-semibold text-sm">
            <AlertTriangle className="h-4 w-4 shrink-0" />
            <span>Safe Error Summary</span>
          </div>
          {/* Rendered strictly as text content to prevent XSS */}
          <p className="text-xs font-mono text-destructive break-words">
            {String(data.error_message)}
          </p>
        </div>
      )}

      {/* Detail Cards */}
      <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
        {/* Execution Metadata Card */}
        <Card>
          <CardHeader>
            <CardTitle className="text-base font-semibold flex items-center gap-2">
              <Clock className="h-4 w-4 text-muted-foreground" />
              Execution Details
            </CardTitle>
          </CardHeader>
          <CardContent className="space-y-4 text-sm">
            <div className="grid grid-cols-2 gap-2 border-b pb-3">
              <span className="text-muted-foreground">Run Status</span>
              <span className="font-medium capitalize text-foreground">{data.status}</span>
            </div>
            <div className="grid grid-cols-2 gap-2 border-b pb-3">
              <span className="text-muted-foreground">Started At</span>
              <span className="font-medium text-foreground">{formatDate(data.started_at)}</span>
            </div>
            <div className="grid grid-cols-2 gap-2 border-b pb-3">
              <span className="text-muted-foreground">Ended At</span>
              <span className="font-medium text-foreground">
                {data.ended_at ? formatDate(data.ended_at) : 'In progress'}
              </span>
            </div>
            <div className="grid grid-cols-2 gap-2 border-b pb-3">
              <span className="text-muted-foreground">Duration</span>
              <span className="font-medium text-foreground">
                {formatDuration(data.duration_seconds)}
              </span>
            </div>
            <div className="grid grid-cols-2 gap-2">
              <span className="text-muted-foreground">Job Reference ID</span>
              <span className="font-mono text-xs text-foreground truncate">{data.job_id}</span>
            </div>
          </CardContent>
        </Card>

        {/* Artifacts Summary Card */}
        <Card>
          <CardHeader>
            <CardTitle className="text-base font-semibold flex items-center gap-2">
              <HardDrive className="h-4 w-4 text-muted-foreground" />
              Artifact Production
            </CardTitle>
          </CardHeader>
          <CardContent className="space-y-4 text-sm">
            <div className="grid grid-cols-2 gap-2 border-b pb-3">
              <span className="text-muted-foreground">Produced Artifacts</span>
              <span className="font-medium text-foreground">{data.artifacts_count}</span>
            </div>
            <div className="grid grid-cols-2 gap-2 border-b pb-3">
              <span className="text-muted-foreground">Total Output Size</span>
              <span className="font-mono font-medium text-foreground">
                {formatBytes(data.total_artifact_size_bytes)}
              </span>
            </div>
            <div className="grid grid-cols-2 gap-2 border-b pb-3">
              <span className="text-muted-foreground">Resource ID</span>
              <span className="font-mono text-xs text-foreground truncate">{data.resource_id}</span>
            </div>
            <div className="grid grid-cols-2 gap-2">
              <span className="text-muted-foreground">Created At</span>
              <span className="font-medium text-foreground">{formatDate(data.created_at)}</span>
            </div>
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
