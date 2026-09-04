'use client';

import { useState } from 'react';
import Link from 'next/link';
import { useQuery } from '@tanstack/react-query';
import { useAuth } from '@/lib/auth/auth-context';
import { apiClient } from '@/lib/api/api-client';
import { queryKeys } from '@/lib/query/query-client';
import { type BackupPlanResponse } from '@/types/domain';
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
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
import { formatDate, getStatusBadgeVariant } from '@/lib/format/formatters';
import { Calendar, Search, ChevronRight, Clock } from 'lucide-react';

export default function BackupPlansPage() {
  const { activeOrgId } = useAuth();
  const [search, setSearch] = useState('');

  const { data, isLoading, isError, error, refetch } = useQuery<BackupPlanResponse[]>({
    queryKey: activeOrgId ? queryKeys.org(activeOrgId).plans.all() : ['disabled'],
    queryFn: () => apiClient.get<BackupPlanResponse[]>('/backup-plans'),
    enabled: !!activeOrgId,
  });

  const plans = data || [];
  const filtered = plans.filter(
    (p) =>
      p.name.toLowerCase().includes(search.toLowerCase()) ||
      p.resource_name?.toLowerCase().includes(search.toLowerCase()) ||
      p.backup_type.toLowerCase().includes(search.toLowerCase())
  );

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
        <div>
          <h1 className="text-2xl font-bold tracking-tight text-foreground">Backup Plans</h1>
          <p className="text-sm text-muted-foreground mt-0.5">
            Automated schedules, targets, and retention policies
          </p>
        </div>
        <div className="relative w-full sm:w-64">
          <Search className="absolute left-3 top-2.5 h-4 w-4 text-muted-foreground" />
          <Input
            placeholder="Search plans..."
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            className="pl-9 h-9"
          />
        </div>
      </div>

      {/* Main Content */}
      <Card>
        <CardHeader className="pb-3">
          <CardTitle className="text-base font-semibold">Configured Plans</CardTitle>
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
                title="Could not load backup plans"
                error={error}
                onRetry={() => refetch()}
              />
            </div>
          ) : plans.length === 0 ? (
            <div className="p-6">
              <EmptyState
                icon={Calendar}
                title="No backup plans have been configured"
                description="Backup schedules will appear here once defined for your protected resources."
              />
            </div>
          ) : filtered.length === 0 ? (
            <div className="p-6 text-center text-sm text-muted-foreground">
              No backup plans match your search criteria.
            </div>
          ) : (
            <>
              {/* Desktop Table */}
              <div className="hidden md:block">
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>Plan Name</TableHead>
                      <TableHead>Resource</TableHead>
                      <TableHead>Backup Type</TableHead>
                      <TableHead>Schedule / Cron</TableHead>
                      <TableHead>Status</TableHead>
                      <TableHead>Created</TableHead>
                      <TableHead className="w-[80px]"></TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {filtered.map((plan) => {
                      const { label, variant } = getStatusBadgeVariant(plan.status);
                      return (
                        <TableRow key={plan.id}>
                          <TableCell className="font-medium text-foreground">
                            <Link
                              href={`/plans/${plan.id}`}
                              className="hover:underline flex items-center gap-2"
                            >
                              <Calendar className="h-4 w-4 text-muted-foreground" />
                              {plan.name}
                            </Link>
                          </TableCell>
                          <TableCell className="text-muted-foreground text-xs">
                            {plan.resource_name || '—'}
                          </TableCell>
                          <TableCell className="capitalize text-xs font-mono">
                            {plan.backup_type.replace(/_/g, ' ')}
                          </TableCell>
                          <TableCell className="text-xs">
                            <div className="flex items-center gap-1.5 font-mono">
                              <Clock className="h-3.5 w-3.5 text-muted-foreground" />
                              <span>{plan.schedule.cron_expression || 'Manual'}</span>
                              <span className="text-muted-foreground">({plan.schedule.timezone})</span>
                            </div>
                          </TableCell>
                          <TableCell>
                            <Badge variant={variant} className="capitalize">
                              {label}
                            </Badge>
                          </TableCell>
                          <TableCell className="text-xs text-muted-foreground">
                            {formatDate(plan.created_at)}
                          </TableCell>
                          <TableCell className="text-right">
                            <Link
                              href={`/plans/${plan.id}`}
                              className="text-muted-foreground hover:text-foreground inline-flex p-1"
                              aria-label={`View plan ${plan.name}`}
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
                {filtered.map((plan) => {
                  const { label, variant } = getStatusBadgeVariant(plan.status);
                  return (
                    <Link
                      key={plan.id}
                      href={`/plans/${plan.id}`}
                      className="block p-4 hover:bg-muted/30 transition-colors"
                    >
                      <div className="flex items-start justify-between gap-2">
                        <div className="flex items-center gap-2 font-medium">
                          <Calendar className="h-4 w-4 text-muted-foreground shrink-0" />
                          <span className="truncate text-foreground">{plan.name}</span>
                        </div>
                        <Badge variant={variant} className="capitalize shrink-0">
                          {label}
                        </Badge>
                      </div>
                      <div className="mt-2 text-xs text-muted-foreground space-y-1">
                        <div>
                          <span>Resource: </span>
                          <span className="text-foreground">{plan.resource_name}</span>
                        </div>
                        <div className="flex items-center gap-1 font-mono">
                          <Clock className="h-3 w-3" />
                          <span>{plan.schedule.cron_expression || 'Manual'}</span>
                          <span>({plan.schedule.timezone})</span>
                        </div>
                      </div>
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
