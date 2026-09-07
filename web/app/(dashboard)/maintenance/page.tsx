'use client';

import { useState, useMemo } from 'react';
import Link from 'next/link';
import { useQuery } from '@tanstack/react-query';
import { useAuth } from '@/lib/auth/auth-context';
import { apiClient } from '@/lib/api/api-client';
import { queryKeys } from '@/lib/query/query-client';
import { ApiError } from '@/types/api';
import { type MaintenanceJobResponse } from '@/types/domain';
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
  getStatusBadgeVariant,
  truncateId,
  formatMaintenanceOperation,
} from '@/lib/format/formatters';
import { Wrench, ChevronRight, ChevronLeft, AlertCircle, X } from 'lucide-react';

const STATUS_OPTIONS = [
  { value: 'all', label: 'All Statuses' },
  { value: 'pending', label: 'Pending' },
  { value: 'running', label: 'Running' },
  { value: 'completed', label: 'Completed' },
  { value: 'failed', label: 'Failed' },
  { value: 'cancelled', label: 'Cancelled' },
];

const OPERATION_OPTIONS = [
  { value: 'all', label: 'All Operations' },
  { value: 'restic_forget', label: 'Forget' },
  { value: 'restic_prune', label: 'Prune' },
  { value: 'restic_deep_check', label: 'Deep Check' },
];

const PAGE_SIZE_OPTIONS = [
  { value: '25', label: '25 per page' },
  { value: '50', label: '50 per page' },
  { value: '100', label: '100 per page' },
];

const UUID_REGEX = /^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$/;

export default function MaintenancePage() {
  const { activeOrgId } = useAuth();
  const [status, setStatus] = useState<string>('all');
  const [operationType, setOperationType] = useState<string>('all');
  const [repositoryId, setRepositoryId] = useState<string>('');
  const [limit, setLimit] = useState<number>(50);
  const [pagination, setPagination] = useState<{
    orgId: string | null;
    cursor: string | null;
    cursorHistory: (string | null)[];
  }>({
    orgId: activeOrgId ?? null,
    cursor: null,
    cursorHistory: [],
  });

  // Deterministic, race-safe pagination resolution strictly bound to activeOrgId.
  // If activeOrgId changes, cursor and history evaluate to null/[] instantly with zero render-time setState.
  const isPaginationInSync = pagination.orgId === activeOrgId;
  const cursor = isPaginationInSync ? pagination.cursor : null;
  const cursorHistory = isPaginationInSync ? pagination.cursorHistory : [];

  const trimmedRepoId = repositoryId.trim();
  const repoIdError = useMemo(() => {
    if (trimmedRepoId && !UUID_REGEX.test(trimmedRepoId)) {
      return 'Invalid Repository ID format (must be a valid UUID).';
    }
    return null;
  }, [trimmedRepoId]);

  const hasActiveFilters =
    status !== 'all' || operationType !== 'all' || Boolean(trimmedRepoId) || limit !== 50;

  const resetPagination = () => {
    setPagination({
      orgId: activeOrgId ?? null,
      cursor: null,
      cursorHistory: [],
    });
  };

  const handleStatusChange = (val: string) => {
    setStatus(val);
    resetPagination();
  };

  const handleOperationChange = (val: string) => {
    setOperationType(val);
    resetPagination();
  };

  const handleRepositoryIdChange = (val: string) => {
    setRepositoryId(val);
    resetPagination();
  };

  const handleLimitChange = (val: string) => {
    const num = parseInt(val, 10);
    setLimit(isNaN(num) ? 50 : num);
    resetPagination();
  };

  const handleClearFilters = () => {
    setStatus('all');
    setOperationType('all');
    setRepositoryId('');
    setLimit(50);
    resetPagination();
  };

  // Compute exact server-affecting query parameters and tenant cache keys
  const activeFilters = useMemo(() => {
    const filters: Record<string, unknown> = {};
    if (status !== 'all') {
      filters.status = status;
    }
    if (operationType !== 'all') {
      filters.operation_type = operationType;
    }
    if (trimmedRepoId && !repoIdError) {
      filters.repository_id = trimmedRepoId;
    }
    if (limit !== 50) {
      filters.limit = limit;
    }
    if (cursor) {
      filters.cursor = cursor;
    }
    return filters;
  }, [status, operationType, trimmedRepoId, repoIdError, limit, cursor]);

  const queryPath = useMemo(() => {
    const params = new URLSearchParams();
    if (status !== 'all') {
      params.set('status', status);
    }
    if (operationType !== 'all') {
      params.set('operation_type', operationType);
    }
    if (trimmedRepoId && !repoIdError) {
      params.set('repository_id', trimmedRepoId);
    }
    if (limit !== 50) {
      params.set('limit', String(limit));
    }
    if (cursor) {
      params.set('cursor', cursor);
    }
    const qs = params.toString();
    return qs ? `/maintenance-jobs?${qs}` : '/maintenance-jobs';
  }, [status, operationType, trimmedRepoId, repoIdError, limit, cursor]);

  // Request is dispatched only when activeOrgId exists and repository ID is valid
  const isQueryEnabled = Boolean(activeOrgId && !repoIdError);

  const { data: result, isLoading, isError, error, refetch } = useQuery({
    queryKey: activeOrgId ? queryKeys.org(activeOrgId).maintenance.list(activeFilters) : ['disabled'],
    queryFn: ({ signal }) => {
      if (!isQueryEnabled) {
        return Promise.reject(new Error('Query is disabled due to invalid parameters.'));
      }
      return apiClient.getPaginated<MaintenanceJobResponse>(queryPath, {
        signal,
        tenantOrgId: activeOrgId!,
      });
    },
    enabled: isQueryEnabled,
    // Conservative polling (3s) only if active pending/running jobs exist and query is valid
    refetchInterval: (query) => {
      if (!isQueryEnabled) return false;
      const paginatedData = query.state.data;
      const jobList = paginatedData?.data;
      const hasActive = jobList?.some((j) => j.status === 'running' || j.status === 'pending');
      return hasActive ? 3000 : false;
    },
  });

  const jobs = result?.data || [];
  const pageMeta = result?.page;

  const handleNextPage = () => {
    if (pageMeta?.has_more && pageMeta?.next_cursor) {
      setPagination({
        orgId: activeOrgId ?? null,
        cursor: pageMeta.next_cursor,
        cursorHistory: [...cursorHistory, cursor],
      });
    }
  };

  const handlePrevPage = () => {
    if (cursorHistory.length > 0) {
      const prevCursor = cursorHistory[cursorHistory.length - 1];
      setPagination({
        orgId: activeOrgId ?? null,
        cursor: prevCursor ?? null,
        cursorHistory: cursorHistory.slice(0, -1),
      });
    }
  };

  const errorMessage = useMemo(() => {
    if (!error) return null;
    if (error instanceof ApiError) {
      if (error.status === 403) {
        return 'You do not have permission to view maintenance jobs.';
      }
      if (error.status === 503) {
        return 'Maintenance service is temporarily unavailable.';
      }
      return 'Unable to load maintenance jobs. Please try again.';
    }
    return 'Unable to load maintenance jobs. Please try again.';
  }, [error]);

  return (
    <div className="space-y-6">
      {/* Header & Filter Controls */}
      <div className="flex flex-col gap-4">
        <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-2">
          <div>
            <h1 className="text-2xl font-bold tracking-tight text-foreground">
              Repository Maintenance
            </h1>
            <p className="text-sm text-muted-foreground">
              Read-only monitoring of repository maintenance operations.
            </p>
          </div>
        </div>

        {/* Filter Bar */}
        <div className="rounded-lg border bg-card p-4 shadow-sm">
          <div className="flex flex-col sm:flex-row flex-wrap items-start sm:items-end gap-3">
            {/* Status Filter */}
            <div className="w-full sm:w-44 space-y-1.5">
              <label htmlFor="maintenance-status-filter" className="text-xs font-medium text-muted-foreground">
                Status
              </label>
              <Select
                id="maintenance-status-filter"
                value={status}
                onChange={(e) => handleStatusChange(e.target.value)}
                options={STATUS_OPTIONS}
                className="h-9 text-xs"
                aria-label="Filter maintenance jobs by status"
              />
            </div>

            {/* Operation Filter */}
            <div className="w-full sm:w-44 space-y-1.5">
              <label htmlFor="maintenance-operation-filter" className="text-xs font-medium text-muted-foreground">
                Operation
              </label>
              <Select
                id="maintenance-operation-filter"
                value={operationType}
                onChange={(e) => handleOperationChange(e.target.value)}
                options={OPERATION_OPTIONS}
                className="h-9 text-xs"
                aria-label="Filter maintenance jobs by operation type"
              />
            </div>

            {/* Repository ID Filter */}
            <div className="w-full sm:w-64 space-y-1.5">
              <label htmlFor="maintenance-repo-filter" className="text-xs font-medium text-muted-foreground">
                Repository ID
              </label>
              <Input
                id="maintenance-repo-filter"
                placeholder="Filter by UUID..."
                value={repositoryId}
                onChange={(e) => handleRepositoryIdChange(e.target.value)}
                className="h-9 text-xs font-mono"
                aria-label="Filter maintenance jobs by repository UUID"
              />
            </div>

            {/* Page Size Filter */}
            <div className="w-full sm:w-36 space-y-1.5">
              <label htmlFor="maintenance-limit-filter" className="text-xs font-medium text-muted-foreground">
                Page Size
              </label>
              <Select
                id="maintenance-limit-filter"
                value={String(limit)}
                onChange={(e) => handleLimitChange(e.target.value)}
                options={PAGE_SIZE_OPTIONS}
                className="h-9 text-xs"
                aria-label="Items per page"
              />
            </div>

            {/* Clear Filters Button */}
            {hasActiveFilters && (
              <div className="sm:ml-auto">
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
          {repoIdError && (
            <div className="mt-2 text-xs text-destructive flex items-center gap-1.5" role="alert" aria-live="polite">
              <AlertCircle className="h-3.5 w-3.5 shrink-0" />
              <span>{repoIdError}</span>
            </div>
          )}
        </div>
      </div>

      {/* Main Content Card */}
      <Card>
        <CardHeader className="pb-3">
          <CardTitle className="text-base font-semibold">Maintenance Records</CardTitle>
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
                title="Could not load maintenance jobs"
                error={errorMessage}
                onRetry={isQueryEnabled ? () => refetch() : undefined}
              />
            </div>
          ) : jobs.length === 0 ? (
            <div className="p-6">
              {hasActiveFilters ? (
                <div className="text-center py-8 space-y-3">
                  <Wrench className="h-8 w-8 mx-auto text-muted-foreground opacity-50" />
                  <div className="space-y-1">
                    <h3 className="text-sm font-semibold text-foreground">
                      No maintenance jobs found
                    </h3>
                    <p className="text-xs text-muted-foreground max-w-sm mx-auto">
                      Try adjusting or clearing your filters to view other maintenance records.
                    </p>
                  </div>
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={handleClearFilters}
                    className="text-xs"
                    aria-label="Clear filters and show all maintenance jobs"
                  >
                    Clear Filters
                  </Button>
                </div>
              ) : (
                <EmptyState
                  title="No maintenance jobs found."
                  description="Repository maintenance operations will appear here once executed by the background scheduler."
                  icon={Wrench}
                />
              )}
            </div>
          ) : (
            <div className="relative overflow-x-auto">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Operation</TableHead>
                    <TableHead>Status</TableHead>
                    <TableHead>Repository</TableHead>
                    <TableHead>Phase</TableHead>
                    <TableHead>Attempts</TableHead>
                    <TableHead>Created</TableHead>
                    <TableHead>Updated / Completed</TableHead>
                    <TableHead className="text-right">Action</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {jobs.map((job) => {
                    const statusInfo = getStatusBadgeVariant(job.status);
                    const opLabel = formatMaintenanceOperation(job.operation_type);

                    return (
                      <TableRow key={job.id} className="hover:bg-muted/50 transition-colors">
                        <TableCell className="font-medium text-foreground">
                          {opLabel}
                        </TableCell>
                        <TableCell>
                          <Badge variant={statusInfo.variant} className="capitalize">
                            {statusInfo.label}
                          </Badge>
                        </TableCell>
                        <TableCell>
                          <span
                            className="font-mono text-xs text-muted-foreground"
                            title={job.repository_id}
                          >
                            {truncateId(job.repository_id)}
                          </span>
                        </TableCell>
                        <TableCell className="text-muted-foreground text-xs">
                          {job.phase || '—'}
                        </TableCell>
                        <TableCell className="text-xs font-mono text-muted-foreground">
                          {job.attempt_count} / {job.max_attempts}
                        </TableCell>
                        <TableCell className="text-xs text-muted-foreground whitespace-nowrap">
                          {formatDate(job.created_at)}
                        </TableCell>
                        <TableCell className="text-xs text-muted-foreground whitespace-nowrap">
                          {formatDate(job.completed_at || job.updated_at)}
                        </TableCell>
                        <TableCell className="text-right">
                          <Link
                            href={`/maintenance/${job.id}`}
                            className="inline-flex items-center text-xs font-medium text-primary hover:underline gap-0.5"
                            aria-label={`View details for maintenance job ${truncateId(job.id)}`}
                          >
                            View details
                            <ChevronRight className="h-3.5 w-3.5" />
                          </Link>
                        </TableCell>
                      </TableRow>
                    );
                  })}
                </TableBody>
              </Table>
            </div>
          )}

          {/* Keyset Pagination Controls */}
          {!isLoading && !isError && jobs.length > 0 && (
            <div className="flex items-center justify-between border-t px-4 py-3 sm:px-6">
              <div className="text-xs text-muted-foreground">
                Page {cursorHistory.length + 1}
              </div>
              <div className="flex items-center gap-2">
                <Button
                  variant="outline"
                  size="sm"
                  onClick={handlePrevPage}
                  disabled={cursorHistory.length === 0}
                  className="h-8 text-xs gap-1"
                  aria-label="Go to previous page"
                >
                  <ChevronLeft className="h-3.5 w-3.5" />
                  Previous
                </Button>
                <Button
                  variant="outline"
                  size="sm"
                  onClick={handleNextPage}
                  disabled={!pageMeta?.has_more || !pageMeta?.next_cursor}
                  className="h-8 text-xs gap-1"
                  aria-label="Go to next page"
                >
                  Next
                  <ChevronRight className="h-3.5 w-3.5" />
                </Button>
              </div>
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
