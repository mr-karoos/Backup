'use client';

import { useState, useMemo } from 'react';
import Link from 'next/link';
import { useQuery } from '@tanstack/react-query';
import { useAuth } from '@/lib/auth/auth-context';
import { apiClient } from '@/lib/api/api-client';
import { queryKeys } from '@/lib/query/query-client';
import { type BackupRunResponse } from '@/types/domain';
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Select } from '@/components/ui/select';
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
import { cn } from '@/lib/utils';
import { History, ChevronRight, AlertCircle, X } from 'lucide-react';

const STATUS_OPTIONS: { value: string; label: string }[] = [
  { value: 'all', label: 'All Statuses' },
  { value: 'pending', label: 'Pending' },
  { value: 'running', label: 'Running' },
  { value: 'success', label: 'Success' },
  { value: 'failed', label: 'Failed' },
  { value: 'cancelled', label: 'Cancelled' },
];

export default function BackupRunsPage() {
  const { activeOrgId } = useAuth();
  const [status, setStatus] = useState<string>('all');
  const [fromDate, setFromDate] = useState<string>('');
  const [toDate, setToDate] = useState<string>('');

  // Validate date range boundaries: from_date must be earlier than or equal to to_date
  const dateError = useMemo(() => {
    if (fromDate && toDate && fromDate > toDate) {
      return 'From date cannot be after To date.';
    }
    return null;
  }, [fromDate, toDate]);

  const hasActiveFilters = status !== 'all' || Boolean(fromDate) || Boolean(toDate);

  // Compute exact server-affecting query parameters and cache keys
  const activeFilters = useMemo(() => {
    const filters: Record<string, string> = {};
    if (status !== 'all') {
      filters.status = status;
    }
    if (!dateError) {
      if (fromDate) {
        filters.from_date = `${fromDate}T00:00:00Z`;
      }
      if (toDate) {
        filters.to_date = `${toDate}T23:59:59Z`;
      }
    }
    return filters;
  }, [status, fromDate, toDate, dateError]);

  const queryPath = useMemo(() => {
    const params = new URLSearchParams();
    if (activeFilters.status) {
      params.set('status', activeFilters.status);
    }
    if (activeFilters.from_date) {
      params.set('from_date', activeFilters.from_date);
    }
    if (activeFilters.to_date) {
      params.set('to_date', activeFilters.to_date);
    }
    const qs = params.toString();
    return qs ? `/backup-runs?${qs}` : '/backup-runs';
  }, [activeFilters]);

  const { data, isLoading, isError, error, refetch } = useQuery<BackupRunResponse[]>({
    queryKey: activeOrgId ? queryKeys.org(activeOrgId).runs.all(activeFilters) : ['disabled'],
    queryFn: () => apiClient.get<BackupRunResponse[]>(queryPath),
    enabled: !!activeOrgId,
    // Conservative polling if active jobs exist
    refetchInterval: (query) => {
      const runs = query.state.data;
      const hasActive = runs?.some((r) => r.status === 'running' || r.status === 'pending');
      return hasActive ? 3000 : false;
    },
  });

  const runs = data || [];

  const handleClearFilters = () => {
    setStatus('all');
    setFromDate('');
    setToDate('');
  };

  return (
    <div className="space-y-6">
      {/* Header & Filter Controls */}
      <div className="flex flex-col gap-4">
        <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-2">
          <div>
            <h1 className="text-2xl font-bold tracking-tight text-foreground">Backup Run History</h1>
            <p className="text-sm text-muted-foreground mt-0.5">
              Chronological audit of backup executions and completion outcomes
            </p>
          </div>
        </div>

        {/* Operational Filter Bar */}
        <div className="rounded-lg border bg-card p-3 shadow-xs">
          <div className="flex flex-col sm:flex-row sm:items-end gap-3">
            {/* Status Filter */}
            <div className="flex flex-col gap-1 w-full sm:w-44">
              <label htmlFor="status-filter" className="text-xs font-medium text-muted-foreground">
                Status
              </label>
              <Select
                id="status-filter"
                value={status}
                onChange={(e) => setStatus(e.target.value)}
                options={STATUS_OPTIONS}
                aria-label="Filter runs by status"
                className="h-9 text-xs"
              />
            </div>

            {/* From Date */}
            <div className="flex flex-col gap-1 w-full sm:w-40">
              <label htmlFor="from-date-filter" className="text-xs font-medium text-muted-foreground">
                From Date
              </label>
              <Input
                id="from-date-filter"
                type="date"
                value={fromDate}
                onChange={(e) => setFromDate(e.target.value)}
                aria-label="Filter runs from date"
                className={cn(
                  'h-9 text-xs',
                  dateError && 'border-destructive focus-visible:ring-destructive'
                )}
              />
            </div>

            {/* To Date */}
            <div className="flex flex-col gap-1 w-full sm:w-40">
              <label htmlFor="to-date-filter" className="text-xs font-medium text-muted-foreground">
                To Date
              </label>
              <Input
                id="to-date-filter"
                type="date"
                value={toDate}
                onChange={(e) => setToDate(e.target.value)}
                aria-label="Filter runs to date"
                className={cn(
                  'h-9 text-xs',
                  dateError && 'border-destructive focus-visible:ring-destructive'
                )}
              />
            </div>

            {/* Clear Filters Button */}
            {hasActiveFilters && (
              <div className="flex items-center">
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={handleClearFilters}
                  className="h-9 text-xs gap-1.5 text-muted-foreground hover:text-foreground"
                  aria-label="Clear all active filters"
                >
                  <X className="h-3.5 w-3.5" />
                  Clear Filters
                </Button>
              </div>
            )}
          </div>

          {/* Validation Feedback */}
          {dateError && (
            <div className="mt-2 text-xs text-destructive flex items-center gap-1.5" role="alert">
              <AlertCircle className="h-3.5 w-3.5 shrink-0" />
              <span>{dateError}</span>
            </div>
          )}
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
              {hasActiveFilters ? (
                <div className="text-center py-8 space-y-3">
                  <History className="h-8 w-8 mx-auto text-muted-foreground opacity-50" />
                  <div className="space-y-1">
                    <h3 className="text-sm font-semibold text-foreground">
                      No runs match the selected filters
                    </h3>
                    <p className="text-xs text-muted-foreground max-w-sm mx-auto">
                      Try adjusting or clearing your filters to view other execution records.
                    </p>
                  </div>
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={handleClearFilters}
                    className="text-xs"
                    aria-label="Clear filters and show all runs"
                  >
                    Clear Filters
                  </Button>
                </div>
              ) : (
                <EmptyState
                  icon={History}
                  title="No backup runs have been recorded"
                  description="Execution records will appear here as backup jobs are scheduled or executed."
                />
              )}
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
                    {runs.map((run) => {
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
                {runs.map((run) => {
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
