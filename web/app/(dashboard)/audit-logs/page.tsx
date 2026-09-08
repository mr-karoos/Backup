'use client';

import { useState, useMemo } from 'react';
import { useQuery } from '@tanstack/react-query';
import { useAuth } from '@/lib/auth/auth-context';
import { usePermissions } from '@/lib/auth/permissions';
import { apiClient } from '@/lib/api/api-client';
import { queryKeys } from '@/lib/query/query-client';
import { ApiError } from '@/types/api';
import { type AuditLogDTO } from '@/types/domain';
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
import { formatDate, truncateId } from '@/lib/format/formatters';
import { cn } from '@/lib/utils';
import {
  FileText,
  ChevronRight,
  ChevronLeft,
  AlertCircle,
  X,
  RefreshCw,
  Shield,
} from 'lucide-react';

const PAGE_SIZE_OPTIONS = [
  { value: '25', label: '25 per page' },
  { value: '50', label: '50 per page' },
  { value: '100', label: '100 per page' },
];

const UUID_REGEX = /^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$/;
const NIL_UUID = '00000000-0000-0000-0000-000000000000';

function isValidUUID(val: string): boolean {
  return UUID_REGEX.test(val) && val !== NIL_UUID;
}

export default function AuditLogsPage() {
  const { activeOrgId } = useAuth();
  const { canViewAuditLogs } = usePermissions();

  const [action, setAction] = useState<string>('');
  const [entityType, setEntityType] = useState<string>('');
  const [entityId, setEntityId] = useState<string>('');
  const [userId, setUserId] = useState<string>('');
  const [fromDate, setFromDate] = useState<string>('');
  const [toDate, setToDate] = useState<string>('');
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

  const actionIsBlank = action.trim().length === 0;
  const entityTypeIsBlank = entityType.trim().length === 0;
  const entityIdIsBlank = entityId.trim().length === 0;
  const userIdIsBlank = userId.trim().length === 0;

  // Validations: length applies to the raw value sent to Backend
  const actionError = useMemo(() => {
    if (!actionIsBlank && action.length > 100) {
      return 'Action filter must be 100 characters or fewer.';
    }
    return null;
  }, [action, actionIsBlank]);

  const entityTypeError = useMemo(() => {
    if (!entityTypeIsBlank && entityType.length > 50) {
      return 'Entity type filter must be 50 characters or fewer.';
    }
    return null;
  }, [entityType, entityTypeIsBlank]);

  const entityIdError = useMemo(() => {
    if (!entityIdIsBlank && !isValidUUID(entityId)) {
      return 'Invalid Entity ID format (must be a valid non-nil UUID).';
    }
    return null;
  }, [entityId, entityIdIsBlank]);

  const userIdError = useMemo(() => {
    if (!userIdIsBlank && !isValidUUID(userId)) {
      return 'Invalid User ID format (must be a valid non-nil UUID).';
    }
    return null;
  }, [userId, userIdIsBlank]);

  const dateError = useMemo(() => {
    if (fromDate && toDate && fromDate > toDate) {
      return 'From date cannot be after To date.';
    }
    return null;
  }, [fromDate, toDate]);

  const validationErrors = useMemo(() => {
    const errors: string[] = [];
    if (actionError) errors.push(actionError);
    if (entityTypeError) errors.push(entityTypeError);
    if (entityIdError) errors.push(entityIdError);
    if (userIdError) errors.push(userIdError);
    if (dateError) errors.push(dateError);
    return errors;
  }, [actionError, entityTypeError, entityIdError, userIdError, dateError]);

  const hasValidationErrors = validationErrors.length > 0;

  const hasActiveFilters =
    !actionIsBlank ||
    !entityTypeIsBlank ||
    !entityIdIsBlank ||
    !userIdIsBlank ||
    Boolean(fromDate) ||
    Boolean(toDate) ||
    limit !== 50;

  const resetPagination = () => {
    setPagination({
      orgId: activeOrgId ?? null,
      cursor: null,
      cursorHistory: [],
    });
  };

  const handleActionChange = (val: string) => {
    setAction(val);
    resetPagination();
  };

  const handleEntityTypeChange = (val: string) => {
    setEntityType(val);
    resetPagination();
  };

  const handleEntityIdChange = (val: string) => {
    setEntityId(val);
    resetPagination();
  };

  const handleUserIdChange = (val: string) => {
    setUserId(val);
    resetPagination();
  };

  const handleFromDateChange = (val: string) => {
    setFromDate(val);
    resetPagination();
  };

  const handleToDateChange = (val: string) => {
    setToDate(val);
    resetPagination();
  };

  const handleLimitChange = (val: string) => {
    const num = parseInt(val, 10);
    setLimit(isNaN(num) ? 50 : num);
    resetPagination();
  };

  const handleClearFilters = () => {
    setAction('');
    setEntityType('');
    setEntityId('');
    setUserId('');
    setFromDate('');
    setToDate('');
    setLimit(50);
    resetPagination();
  };

  // Compute exact server-affecting query parameters and tenant cache keys
  const activeFilters = useMemo(() => {
    const filters: Record<string, unknown> = {};
    if (!actionIsBlank && !actionError) {
      filters.action = action;
    }
    if (!entityTypeIsBlank && !entityTypeError) {
      filters.entity_type = entityType;
    }
    if (!entityIdIsBlank && !entityIdError) {
      filters.entity_id = entityId;
    }
    if (!userIdIsBlank && !userIdError) {
      filters.user_id = userId;
    }
    if (fromDate && !dateError) {
      filters.from = `${fromDate}T00:00:00Z`;
    }
    if (toDate && !dateError) {
      filters.to = `${toDate}T23:59:59.999999999Z`;
    }
    if (limit !== 50) {
      filters.limit = limit;
    }
    if (cursor) {
      filters.cursor = cursor;
    }
    return filters;
  }, [
    action,
    actionIsBlank,
    actionError,
    entityType,
    entityTypeIsBlank,
    entityTypeError,
    entityId,
    entityIdIsBlank,
    entityIdError,
    userId,
    userIdIsBlank,
    userIdError,
    fromDate,
    toDate,
    dateError,
    limit,
    cursor,
  ]);

  const queryPath = useMemo(() => {
    const params = new URLSearchParams();
    if (!actionIsBlank && !actionError) {
      params.set('action', action);
    }
    if (!entityTypeIsBlank && !entityTypeError) {
      params.set('entity_type', entityType);
    }
    if (!entityIdIsBlank && !entityIdError) {
      params.set('entity_id', entityId);
    }
    if (!userIdIsBlank && !userIdError) {
      params.set('user_id', userId);
    }
    if (fromDate && !dateError) {
      params.set('from', `${fromDate}T00:00:00Z`);
    }
    if (toDate && !dateError) {
      params.set('to', `${toDate}T23:59:59.999999999Z`);
    }
    if (limit !== 50) {
      params.set('limit', String(limit));
    }
    if (cursor) {
      params.set('cursor', cursor);
    }
    const qs = params.toString();
    return qs ? `/audit-logs?${qs}` : '/audit-logs';
  }, [
    action,
    actionIsBlank,
    actionError,
    entityType,
    entityTypeIsBlank,
    entityTypeError,
    entityId,
    entityIdIsBlank,
    entityIdError,
    userId,
    userIdIsBlank,
    userIdError,
    fromDate,
    toDate,
    dateError,
    limit,
    cursor,
  ]);

  // Query is dispatched ONLY when activeOrgId exists, caller has permission, and inputs are valid
  const isQueryEnabled = Boolean(activeOrgId && canViewAuditLogs && !hasValidationErrors);

  const { data: result, isLoading, isError, error, refetch, isFetching } = useQuery({
    queryKey: activeOrgId ? queryKeys.org(activeOrgId).auditLogs.list(activeFilters) : ['disabled'],
    queryFn: ({ signal }) => {
      if (!isQueryEnabled) {
        return Promise.reject(new Error('Query is disabled due to invalid parameters or lack of permission.'));
      }
      return apiClient.getPaginated<AuditLogDTO>(queryPath, {
        signal,
        tenantOrgId: activeOrgId!,
      });
    },
    enabled: isQueryEnabled,
  });

  const logs = result?.data || [];
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
        return 'You do not have permission to view audit logs.';
      }
      if (error.status === 503) {
        return 'Audit service is temporarily unavailable.';
      }
      if (error.status === 400) {
        return 'Invalid audit log query parameters. Please check your filters.';
      }
      if (error.status === 404) {
        return 'Organization or audit log resource not found.';
      }
      return error.message || 'Unable to load audit logs. Please try again.';
    }
    return 'Unable to load audit logs. Please try again.';
  }, [error]);

  // Access Control Guard: Render access-denied state if caller lacks permission
  if (!canViewAuditLogs) {
    return (
      <div className="space-y-6">
        <div className="flex flex-col gap-1">
          <h1 className="text-2xl font-bold tracking-tight text-foreground">Audit Logs</h1>
          <p className="text-sm text-muted-foreground">
            Security and operational audit trail.
          </p>
        </div>
        <Card className="border-destructive/20 bg-destructive/5 p-8 text-center">
          <div className="flex flex-col items-center space-y-3">
            <div className="flex h-12 w-12 items-center justify-center rounded-full bg-destructive/10 text-destructive">
              <Shield className="h-6 w-6" aria-hidden="true" />
            </div>
            <h3 className="text-base font-semibold text-foreground">Access Restricted</h3>
            <p className="text-sm text-muted-foreground max-w-md">
              You do not have permission to view audit logs. This feature requires the audit_log:read permission.
            </p>
          </div>
        </Card>
      </div>
    );
  }

  return (
    <div className="space-y-6">
      {/* Header & Controls */}
      <div className="flex flex-col gap-4">
        <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-2">
          <div>
            <h1 className="text-2xl font-bold tracking-tight text-foreground">
              Audit Logs
            </h1>
            <p className="text-sm text-muted-foreground">
              Read-only security and operational audit trail.
            </p>
          </div>
          <div>
            <Button
              variant="outline"
              size="sm"
              onClick={() => refetch()}
              disabled={isLoading || isFetching || !isQueryEnabled}
              className="h-9 text-xs gap-1.5"
              aria-label="Refresh audit logs"
            >
              <RefreshCw className={cn('h-3.5 w-3.5', isFetching && 'animate-spin')} />
              Refresh
            </Button>
          </div>
        </div>

        {/* Filter Bar */}
        <div className="rounded-lg border bg-card p-4 shadow-xs">
          <div className="grid grid-cols-1 sm:grid-cols-2 md:grid-cols-3 lg:grid-cols-4 gap-3 items-end">
            {/* Action Filter */}
            <div className="space-y-1.5">
              <label htmlFor="audit-action-filter" className="text-xs font-medium text-muted-foreground">
                Action
              </label>
              <Input
                id="audit-action-filter"
                placeholder="e.g. backup_plan.create"
                value={action}
                onChange={(e) => handleActionChange(e.target.value)}
                className="h-9 text-xs"
                aria-label="Filter by action"
              />
            </div>

            {/* Entity Type Filter */}
            <div className="space-y-1.5">
              <label htmlFor="audit-entity-type-filter" className="text-xs font-medium text-muted-foreground">
                Entity Type
              </label>
              <Input
                id="audit-entity-type-filter"
                placeholder="e.g. backup_plan"
                value={entityType}
                onChange={(e) => handleEntityTypeChange(e.target.value)}
                className="h-9 text-xs"
                aria-label="Filter by entity type"
              />
            </div>

            {/* Entity ID Filter */}
            <div className="space-y-1.5">
              <label htmlFor="audit-entity-id-filter" className="text-xs font-medium text-muted-foreground">
                Entity ID
              </label>
              <Input
                id="audit-entity-id-filter"
                placeholder="Filter by UUID..."
                value={entityId}
                onChange={(e) => handleEntityIdChange(e.target.value)}
                className="h-9 text-xs font-mono"
                aria-label="Filter by entity UUID"
              />
            </div>

            {/* User ID Filter */}
            <div className="space-y-1.5">
              <label htmlFor="audit-user-id-filter" className="text-xs font-medium text-muted-foreground">
                User ID
              </label>
              <Input
                id="audit-user-id-filter"
                placeholder="Filter by User UUID..."
                value={userId}
                onChange={(e) => handleUserIdChange(e.target.value)}
                className="h-9 text-xs font-mono"
                aria-label="Filter by user UUID"
              />
            </div>

            {/* From Date */}
            <div className="space-y-1.5">
              <label htmlFor="audit-from-date-filter" className="text-xs font-medium text-muted-foreground">
                From Date
              </label>
              <Input
                id="audit-from-date-filter"
                type="date"
                value={fromDate}
                onChange={(e) => handleFromDateChange(e.target.value)}
                className="h-9 text-xs"
                aria-label="Filter from date"
              />
            </div>

            {/* To Date */}
            <div className="space-y-1.5">
              <label htmlFor="audit-to-date-filter" className="text-xs font-medium text-muted-foreground">
                To Date
              </label>
              <Input
                id="audit-to-date-filter"
                type="date"
                value={toDate}
                onChange={(e) => handleToDateChange(e.target.value)}
                className="h-9 text-xs"
                aria-label="Filter to date"
              />
            </div>

            {/* Page Size */}
            <div className="space-y-1.5">
              <label htmlFor="audit-limit-filter" className="text-xs font-medium text-muted-foreground">
                Page Size
              </label>
              <Select
                id="audit-limit-filter"
                value={String(limit)}
                onChange={(e) => handleLimitChange(e.target.value)}
                options={PAGE_SIZE_OPTIONS}
                className="h-9 text-xs"
                aria-label="Items per page"
              />
            </div>

            {/* Clear Filters Button */}
            {hasActiveFilters && (
              <div className="flex items-end">
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
          {hasValidationErrors && (
            <div className="mt-3 space-y-1" role="alert" aria-live="polite">
              {validationErrors.map((errMsg, idx) => (
                <div key={idx} className="text-xs text-destructive flex items-center gap-1.5">
                  <AlertCircle className="h-3.5 w-3.5 shrink-0" />
                  <span>{errMsg}</span>
                </div>
              ))}
            </div>
          )}
        </div>
      </div>

      {/* Main Content Card */}
      <Card>
        <CardHeader className="pb-3">
          <CardTitle className="text-base font-semibold">Audit Records</CardTitle>
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
                title="Could not load audit logs"
                error={errorMessage}
                onRetry={isQueryEnabled ? () => refetch() : undefined}
              />
            </div>
          ) : logs.length === 0 ? (
            <div className="p-6">
              {hasActiveFilters ? (
                <div className="text-center py-8 space-y-3">
                  <FileText className="h-8 w-8 mx-auto text-muted-foreground opacity-50" />
                  <div className="space-y-1">
                    <h3 className="text-sm font-semibold text-foreground">
                      No audit logs found
                    </h3>
                    <p className="text-xs text-muted-foreground max-w-sm mx-auto">
                      Try adjusting or clearing your filters to view other audit records.
                    </p>
                  </div>
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={handleClearFilters}
                    className="text-xs"
                    aria-label="Clear filters and show all audit logs"
                  >
                    Clear Filters
                  </Button>
                </div>
              ) : (
                <EmptyState
                  title="No audit logs found."
                  description="Audit log events will appear here as actions are performed in this organization."
                  icon={FileText}
                />
              )}
            </div>
          ) : (
            <div className="relative overflow-x-auto">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Created At</TableHead>
                    <TableHead>Action</TableHead>
                    <TableHead>Entity Type</TableHead>
                    <TableHead>Entity ID</TableHead>
                    <TableHead>User ID</TableHead>
                    <TableHead>IP Address</TableHead>
                    <TableHead>User Agent</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {logs.map((log) => (
                    <TableRow key={log.id} className="hover:bg-muted/50 transition-colors">
                      <TableCell className="text-xs text-muted-foreground whitespace-nowrap">
                        {formatDate(log.created_at)}
                      </TableCell>
                      <TableCell className="font-medium text-foreground">
                        <Badge variant="outline" className="font-mono text-xs">
                          {log.action}
                        </Badge>
                      </TableCell>
                      <TableCell className="text-xs font-mono text-muted-foreground">
                        {log.entity_type}
                      </TableCell>
                      <TableCell>
                        {log.entity_id ? (
                          <span
                            className="font-mono text-xs text-muted-foreground"
                            title={log.entity_id}
                          >
                            {truncateId(log.entity_id)}
                          </span>
                        ) : (
                          <span className="text-muted-foreground text-xs">—</span>
                        )}
                      </TableCell>
                      <TableCell>
                        {log.user_id ? (
                          <span
                            className="font-mono text-xs text-muted-foreground"
                            title={log.user_id}
                          >
                            {truncateId(log.user_id)}
                          </span>
                        ) : (
                          <span className="text-muted-foreground text-xs">—</span>
                        )}
                      </TableCell>
                      <TableCell className="text-xs font-mono text-muted-foreground whitespace-nowrap">
                        {log.ip_address || '—'}
                      </TableCell>
                      <TableCell className="text-xs text-muted-foreground">
                        {log.user_agent ? (
                          <span
                            className="block max-w-[200px] truncate"
                            title={log.user_agent}
                          >
                            {log.user_agent}
                          </span>
                        ) : (
                          '—'
                        )}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
          )}

          {/* Keyset Pagination Controls */}
          {!isLoading && !isError && logs.length > 0 && (
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
