'use client';

import { useState } from 'react';
import Link from 'next/link';
import { useQuery } from '@tanstack/react-query';
import { useAuth } from '@/lib/auth/auth-context';
import { apiClient } from '@/lib/api/api-client';
import { queryKeys } from '@/lib/query/query-client';
import { type BackupRunResponse } from '@/types/domain';
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import {
  Table,
  TableHeader,
  TableBody,
  TableRow,
  TableHead,
  TableCell,
} from '@/components/ui/table';
import { Skeleton } from '@/components/ui/skeleton';
import { ErrorState } from '@/components/ui/error-state';
import { EmptyState } from '@/components/ui/empty-state';
import {
  formatDate,
  formatDuration,
  formatBytes,
  getStatusBadgeVariant,
  truncateId,
} from '@/lib/format/formatters';
import { History, ChevronRight, AlertCircle } from 'lucide-react';

export default function BackupRunsPage() {
  const { activeOrgId } = useAuth();
  const [statusFilter, setStatusFilter] = useState<string>('all');

  const { data, isLoading, isError, error, refetch } = useQuery<BackupRunResponse[]>({
    queryKey: activeOrgId ? queryKeys.org(activeOrgId).runs.all() : ['disabled'],
    queryFn: () => apiClient.get<BackupRunResponse[]>('/backup-runs'),
    enabled: !!activeOrgId,
    // Conservative polling if active jobs exist
    refetchInterval: (query) => {
      const runs = query.state.data;
      const hasActive = runs?.some((r) => r.status === 'running' || r.status === 'pending');
      return hasActive ? 3000 : false;
    },
  });

  const runs = data || [];
  const filtered = runs.filter((r) => {
    if (statusFilter === 'all') return true;
    return r.status === statusFilter;
  });

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
        <div>
          <h1 className="text-2xl font-bold tracking-tight text-foreground">Backup Run History</h1>
          <p className="text-sm text-muted-foreground mt-0.5">
            Chronological audit of backup executions and completion outcomes
          </p>
        </div>

        {/* Filter Pills */}
        <div className="flex flex-wrap gap-1.5 bg-muted p-1 rounded-lg text-xs self-start">
          {['all', 'running', 'success', 'failed'].map((filterVal) => (
            <button
              key={filterVal}
              onClick={() => setStatusFilter(filterVal)}
              className={`px-3 py-1.5 rounded-md font-medium capitalize transition-colors ${
                statusFilter === filterVal
                  ? 'bg-background text-foreground shadow-xs'
                  : 'text-muted-foreground hover:text-foreground'
              }`}
            >
              {filterVal}
            </button>
          ))}
        </div>
      </div>

      {/* Main Content */}
      <Card>
        <CardHeader className="pb-3">
          <CardTitle className="text-base font-semibold">Execution Records</CardTitle>
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
                title="Could not load backup runs"
                error={error}
                onRetry={() => refetch()}
              />
            </div>
          ) : runs.length === 0 ? (
            <div className="p-6">
              <EmptyState
                icon={History}
                title="No backup runs have been recorded"
                description="Execution records will appear here as backup jobs are scheduled or executed."
              />
            </div>
          ) : filtered.length === 0 ? (
            <div className="p-6 text-center text-sm text-muted-foreground">
              No runs match the selected filter.
            </div>
          ) : (
            <>
              {/* Desktop Table */}
              <div className="hidden md:block">
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>Run ID</TableHead>
                      <TableHead>Status</TableHead>
                      <TableHead>Attempt</TableHead>
                      <TableHead>Started At</TableHead>
                      <TableHead>Duration</TableHead>
                      <TableHead>Artifacts</TableHead>
                      <TableHead>Total Size</TableHead>
                      <TableHead className="w-[80px]"></TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {filtered.map((run) => {
                      const { label, variant } = getStatusBadgeVariant(run.status);
                      return (
                        <TableRow key={run.id}>
                          <TableCell className="font-mono text-xs font-medium text-foreground">
                            <Link
                              href={`/runs/${run.id}`}
                              className="hover:underline flex items-center gap-1.5"
                            >
                              {truncateId(run.id)}
                            </Link>
                          </TableCell>
                          <TableCell>
                            <Badge variant={variant} className="capitalize">
                              {label}
                            </Badge>
                          </TableCell>
                          <TableCell className="text-xs text-muted-foreground">
                            #{run.attempt_number}
                          </TableCell>
                          <TableCell className="text-xs text-muted-foreground">
                            {formatDate(run.started_at)}
                          </TableCell>
                          <TableCell className="text-xs text-muted-foreground">
                            {formatDuration(run.duration_seconds)}
                          </TableCell>
                          <TableCell className="text-xs text-muted-foreground">
                            {run.artifacts_count}
                          </TableCell>
                          <TableCell className="text-xs text-muted-foreground font-mono">
                            {formatBytes(run.total_artifact_size_bytes)}
                          </TableCell>
                          <TableCell className="text-right">
                            <Link
                              href={`/runs/${run.id}`}
                              className="text-muted-foreground hover:text-foreground inline-flex p-1"
                              aria-label={`View run ${run.id}`}
                            >
                              <ChevronRight className="h-4 w-4" />
                            </Link>
                          </TableCell>
                        </TableRow>
                      );
                    })}
                  </TableBody>
                </Table>
              </div>

              {/* Mobile Stacked Cards */}
              <div className="md:hidden divide-y">
                {filtered.map((run) => {
                  const { label, variant } = getStatusBadgeVariant(run.status);
                  return (
                    <Link
                      key={run.id}
                      href={`/runs/${run.id}`}
                      className="block p-4 hover:bg-muted/30 transition-colors"
                    >
                      <div className="flex items-start justify-between gap-2">
                        <div className="flex flex-col">
                          <span className="font-mono font-medium text-sm text-foreground">
                            Run {truncateId(run.id)}
                          </span>
                          <span className="text-[11px] text-muted-foreground">
                            {formatDate(run.started_at)}
                          </span>
                        </div>
                        <Badge variant={variant} className="capitalize shrink-0">
                          {label}
                        </Badge>
                      </div>

                      <div className="mt-2 grid grid-cols-2 gap-1 text-xs text-muted-foreground">
                        <div>
                          <span>Duration: </span>
                          <span className="text-foreground">{formatDuration(run.duration_seconds)}</span>
                        </div>
                        <div className="text-right font-mono">
                          <span>{formatBytes(run.total_artifact_size_bytes)}</span>
                        </div>
                      </div>

                      {run.error_message && (
                        <div className="mt-2 flex items-center gap-1.5 text-destructive text-xs font-mono truncate">
                          <AlertCircle className="h-3.5 w-3.5 shrink-0" />
                          <span className="truncate">{run.error_message}</span>
                        </div>
                      )}
                    </Link>
                  );
                })}
              </div>
            </>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
