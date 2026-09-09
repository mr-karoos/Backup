'use client';

import * as React from 'react';
import Link from 'next/link';
import { useParams } from 'next/navigation';
import { useQuery } from '@tanstack/react-query';
import { useAuth } from '@/lib/auth/auth-context';
import { apiClient } from '@/lib/api/api-client';
import { queryKeys } from '@/lib/query/query-client';
import { ApiError } from '@/types/api';
import { type MaintenanceJobDetailResponse } from '@/types/domain';
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Skeleton } from '@/components/ui/skeleton';
import { ErrorState } from '@/components/ui/error-state';
import {
  Table,
  TableHeader,
  TableBody,
  TableRow,
  TableHead,
  TableCell,
} from '@/components/ui/table';
import {
  formatDate,
  formatDuration,
  getStatusBadgeVariant,
  formatMaintenanceOperation,
} from '@/lib/format/formatters';
import { ArrowLeft, Wrench, RefreshCw } from 'lucide-react';

export default function MaintenanceJobDetailPage() {
  const params = useParams();
  const id = params?.id as string;
  const { activeOrgId } = useAuth();

  const isQueryEnabled = Boolean(activeOrgId && id);

  const { data: job, isLoading, isError, error, refetch } = useQuery<MaintenanceJobDetailResponse>({
    queryKey: activeOrgId && id ? queryKeys.org(activeOrgId).maintenance.detail(id) : ['disabled'],
    queryFn: ({ signal }) =>
      apiClient.get<MaintenanceJobDetailResponse>(`/maintenance-jobs/${id}`, {
        signal,
        tenantOrgId: activeOrgId!,
      }),
    enabled: isQueryEnabled,
    // Conservative polling (3s) only while job is pending or running
    refetchInterval: (query) => {
      if (!isQueryEnabled) return false;
      const currentJob = query.state.data;
      const isActive = currentJob?.status === 'running' || currentJob?.status === 'pending';
      return isActive ? 3000 : false;
    },
  });

  const errorMessage = React.useMemo(() => {
    if (!error) return null;
    if (error instanceof ApiError) {
      if (error.status === 404) {
        return 'Maintenance job not found.';
      }
      if (error.status === 403) {
        return 'You do not have permission to view this maintenance job.';
      }
      if (error.status === 503) {
        return 'Maintenance service is temporarily unavailable.';
      }
      return 'Unable to load maintenance job details. Please try again.';
    }
    return 'Unable to load maintenance job details. Please try again.';
  }, [error]);

  if (isLoading) {
    return (
      <div className="space-y-6">
        <div className="flex items-center gap-2">
          <Skeleton className="h-9 w-24" />
        </div>
        <Card>
          <CardHeader>
            <Skeleton className="h-6 w-48" />
            <Skeleton className="h-4 w-72" />
          </CardHeader>
          <CardContent className="space-y-4">
            <Skeleton className="h-24 w-full" />
            <Skeleton className="h-48 w-full" />
          </CardContent>
        </Card>
      </div>
    );
  }

  if (isError || !job) {
    return (
      <div className="space-y-6">
        <div>
          <Link
            href="/maintenance"
            className="inline-flex items-center text-xs font-medium text-muted-foreground hover:text-foreground gap-1"
          >
            <ArrowLeft className="h-3.5 w-3.5" />
            Back to Maintenance
          </Link>
        </div>
        <ErrorState
          title="Could not load maintenance job"
          error={errorMessage || 'Maintenance job not found.'}
          onRetry={() => refetch()}
        />
      </div>
    );
  }

  const statusInfo = getStatusBadgeVariant(job.status);
  const opLabel = formatMaintenanceOperation(job.operation_type);
  const runs = job.runs || [];

  return (
    <div className="space-y-6">
      {/* Back Navigation & Header */}
      <div className="flex flex-col gap-2">
        <div>
          <Link
            href="/maintenance"
            className="inline-flex items-center text-xs font-medium text-muted-foreground hover:text-foreground gap-1"
          >
            <ArrowLeft className="h-3.5 w-3.5" />
            Back to Maintenance
          </Link>
        </div>
        <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-3">
          <div className="flex items-center gap-3">
            <div className="flex h-10 w-10 items-center justify-center rounded-lg bg-primary/10 text-primary">
              <Wrench className="h-5 w-5" />
            </div>
            <div>
              <div className="flex items-center gap-2">
                <h1 className="text-xl font-bold tracking-tight text-foreground">
                  {opLabel}
                </h1>
                <Badge variant={statusInfo.variant} className="capitalize">
                  {statusInfo.label}
                </Badge>
              </div>
              <p className="text-xs font-mono text-muted-foreground mt-0.5">
                Job ID: {job.id}
              </p>
            </div>
          </div>

          <div className="flex items-center gap-2">
            <Button
              variant="outline"
              size="sm"
              onClick={() => refetch()}
              className="text-xs gap-1.5"
              aria-label="Refresh job data"
            >
              <RefreshCw className="h-3.5 w-3.5" />
              Refresh
            </Button>
          </div>
        </div>
      </div>

      {/* Job Specifications Card */}
      <Card>
        <CardHeader className="pb-3">
          <CardTitle className="text-base font-semibold">Job Details</CardTitle>
        </CardHeader>
        <CardContent>
          <dl className="grid grid-cols-1 sm:grid-cols-2 md:grid-cols-3 gap-4 text-xs">
            <div>
              <dt className="text-muted-foreground font-medium">Operation</dt>
              <dd className="mt-1 font-semibold text-foreground">{opLabel}</dd>
            </div>
            <div>
              <dt className="text-muted-foreground font-medium">Status</dt>
              <dd className="mt-1">
                <Badge variant={statusInfo.variant} className="capitalize">
                  {statusInfo.label}
                </Badge>
              </dd>
            </div>
            <div>
              <dt className="text-muted-foreground font-medium">Repository ID</dt>
              <dd className="mt-1 font-mono text-foreground break-all" title={job.repository_id}>
                {job.repository_id}
              </dd>
            </div>

            {job.artifact_id && (
              <div>
                <dt className="text-muted-foreground font-medium">Artifact ID</dt>
                <dd className="mt-1 font-mono text-foreground break-all">{job.artifact_id}</dd>
              </div>
            )}

            {job.snapshot_id && (
              <div>
                <dt className="text-muted-foreground font-medium">Snapshot ID</dt>
                <dd className="mt-1 font-mono text-foreground break-all">{job.snapshot_id}</dd>
              </div>
            )}

            {(job.subset_index !== null && job.subset_index !== undefined) && (
              <div>
                <dt className="text-muted-foreground font-medium">Subset Index / Total</dt>
                <dd className="mt-1 font-mono text-foreground">
                  {job.subset_index} / {job.subset_total ?? '—'}
                </dd>
              </div>
            )}

            <div>
              <dt className="text-muted-foreground font-medium">Attempt Count / Max</dt>
              <dd className="mt-1 font-mono text-foreground">
                {job.attempt_count} / {job.max_attempts}
              </dd>
            </div>

            <div>
              <dt className="text-muted-foreground font-medium">Phase</dt>
              <dd className="mt-1 text-foreground">{job.phase || '—'}</dd>
            </div>

            <div>
              <dt className="text-muted-foreground font-medium">Next Attempt</dt>
              <dd className="mt-1 text-foreground">{formatDate(job.next_attempt_at)}</dd>
            </div>

            <div>
              <dt className="text-muted-foreground font-medium">Created</dt>
              <dd className="mt-1 text-foreground">{formatDate(job.created_at)}</dd>
            </div>

            <div>
              <dt className="text-muted-foreground font-medium">Updated</dt>
              <dd className="mt-1 text-foreground">{formatDate(job.updated_at)}</dd>
            </div>

            <div>
              <dt className="text-muted-foreground font-medium">Completed</dt>
              <dd className="mt-1 text-foreground">{formatDate(job.completed_at)}</dd>
            </div>
          </dl>
        </CardContent>
      </Card>

      {/* Execution Attempts Section */}
      <Card>
        <CardHeader className="pb-3">
          <CardTitle className="text-base font-semibold">Execution Attempts</CardTitle>
        </CardHeader>
        <CardContent className="p-0">
          {runs.length === 0 ? (
            <div className="p-6 text-center text-xs text-muted-foreground">
              No execution attempts recorded.
            </div>
          ) : (
            <div className="relative overflow-x-auto">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Attempt</TableHead>
                    <TableHead>Status</TableHead>
                    <TableHead>Started</TableHead>
                    <TableHead>Ended</TableHead>
                    <TableHead>Duration</TableHead>
                    <TableHead>Heartbeat</TableHead>
                    <TableHead>Error Summary</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {runs.map((run) => {
                    const runStatusInfo = getStatusBadgeVariant(run.status);
                    const durationSecs =
                      run.duration_ms !== null && run.duration_ms !== undefined
                        ? Math.round(run.duration_ms / 1000)
                        : null;

                    return (
                      <TableRow key={run.id} className="hover:bg-muted/50 transition-colors">
                        <TableCell className="font-mono text-xs font-semibold text-foreground">
                          #{run.attempt_number}
                        </TableCell>
                        <TableCell>
                          <Badge variant={runStatusInfo.variant} className="capitalize">
                            {runStatusInfo.label}
                          </Badge>
                        </TableCell>
                        <TableCell className="text-xs text-muted-foreground whitespace-nowrap">
                          {formatDate(run.started_at)}
                        </TableCell>
                        <TableCell className="text-xs text-muted-foreground whitespace-nowrap">
                          {formatDate(run.ended_at)}
                        </TableCell>
                        <TableCell className="text-xs text-muted-foreground font-mono">
                          {formatDuration(durationSecs)}
                        </TableCell>
                        <TableCell className="text-xs text-muted-foreground whitespace-nowrap">
                          {formatDate(run.heartbeat_at)}
                        </TableCell>
                        <TableCell className="text-xs max-w-xs">
                          {run.error_summary ? (
                            <span className="text-destructive font-mono text-xs break-words block">
                              {/* Escaped plain text only */}
                              {String(run.error_summary)}
                            </span>
                          ) : (
                            <span className="text-muted-foreground">—</span>
                          )}
                        </TableCell>
                      </TableRow>
                    );
                  })}
                </TableBody>
              </Table>
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
