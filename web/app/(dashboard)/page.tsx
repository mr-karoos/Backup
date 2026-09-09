'use client';

import Link from 'next/link';
import { useQuery } from '@tanstack/react-query';
import { useAuth } from '@/lib/auth/auth-context';
import { apiClient } from '@/lib/api/api-client';
import { queryKeys } from '@/lib/query/query-client';
import {
  type ResourceResponse,
  type BackupPlanResponse,
  type BackupRunResponse,
  type StorageTargetResponse,
} from '@/types/domain';
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { Skeleton } from '@/components/ui/skeleton';
import { ErrorState } from '@/components/ui/error-state';
import { EmptyState } from '@/components/ui/empty-state';
import { formatDate, formatDuration, getStatusBadgeVariant, truncateId } from '@/lib/format/formatters';
import {
  Server,
  Calendar,
  History,
  HardDrive,
  CheckCircle2,
  AlertTriangle,
  PlayCircle,
  ArrowRight,
} from 'lucide-react';

export default function DashboardPage() {
  const { activeOrgId } = useAuth();

  // Queries for tenant entities
  const resourcesQuery = useQuery<ResourceResponse[]>({
    queryKey: activeOrgId ? queryKeys.org(activeOrgId).resources.all() : ['disabled'],
    queryFn: () => apiClient.get<ResourceResponse[]>('/resources'),
    enabled: !!activeOrgId,
  });

  const plansQuery = useQuery<BackupPlanResponse[]>({
    queryKey: activeOrgId ? queryKeys.org(activeOrgId).plans.all() : ['disabled'],
    queryFn: () => apiClient.get<BackupPlanResponse[]>('/backup-plans'),
    enabled: !!activeOrgId,
  });

  const runsQuery = useQuery<BackupRunResponse[]>({
    queryKey: activeOrgId ? queryKeys.org(activeOrgId).runs.all() : ['disabled'],
    queryFn: () => apiClient.get<BackupRunResponse[]>('/backup-runs'),
    enabled: !!activeOrgId,
    // Poll every 5s if active runs exist
    refetchInterval: (query) => {
      const runs = query.state.data;
      const hasActive = runs?.some((r) => r.status === 'running' || r.status === 'pending');
      return hasActive ? 5000 : false;
    },
  });

  const storageQuery = useQuery<StorageTargetResponse[]>({
    queryKey: activeOrgId ? queryKeys.org(activeOrgId).storageTargets.all() : ['disabled'],
    queryFn: () => apiClient.get<StorageTargetResponse[]>('/storage-targets'),
    enabled: !!activeOrgId,
  });

  const isLoading =
    resourcesQuery.isLoading ||
    plansQuery.isLoading ||
    runsQuery.isLoading ||
    storageQuery.isLoading;

  const isError =
    resourcesQuery.isError ||
    plansQuery.isError ||
    runsQuery.isError ||
    storageQuery.isError;

  if (isError) {
    return (
      <div className="space-y-6">
        <h1 className="text-2xl font-bold tracking-tight">Dashboard Overview</h1>
        <ErrorState
          title="Failed to load dashboard data"
          error={
            resourcesQuery.error ||
            plansQuery.error ||
            runsQuery.error ||
            storageQuery.error
          }
          onRetry={() => {
            resourcesQuery.refetch();
            plansQuery.refetch();
            runsQuery.refetch();
            storageQuery.refetch();
          }}
        />
      </div>
    );
  }

  const resources = resourcesQuery.data || [];
  const plans = plansQuery.data || [];
  const runs = runsQuery.data || [];
  const storageTargets = storageQuery.data || [];

  // Aggregated metrics derived strictly from backend source data
  const activeResourcesCount = resources.filter((r) => r.status === 'active').length;
  const activePlansCount = plans.filter((p) => p.status === 'active').length;
  const runningRunsCount = runs.filter(
    (r) => r.status === 'running' || r.status === 'pending'
  ).length;
  const successRunsCount = runs.filter((r) => r.status === 'success').length;
  const failedRunsCount = runs.filter((r) => r.status === 'failed').length;

  const recentRuns = [...runs]
    .sort((a, b) => new Date(b.started_at).getTime() - new Date(a.started_at).getTime())
    .slice(0, 5);

  const failedRuns = runs.filter((r) => r.status === 'failed').slice(0, 3);

  return (
    <div className="space-y-8">
      {/* Header */}
      <div>
        <h1 className="text-2xl md:text-3xl font-bold tracking-tight text-foreground">
          Dashboard Overview
        </h1>
        <p className="mt-1 text-sm text-muted-foreground">
          Real-time operational summary of protected infrastructure and backup executions
        </p>
      </div>

      {/* KPI Metric Cards */}
      <div className="grid grid-cols-2 md:grid-cols-3 lg:grid-cols-6 gap-4">
        {/* Active Resources */}
        <Card>
          <CardHeader className="flex flex-row items-center justify-between pb-2 space-y-0">
            <CardTitle className="text-xs font-medium text-muted-foreground">
              Protected Resources
            </CardTitle>
            <Server className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            {isLoading ? (
              <Skeleton className="h-7 w-12" />
            ) : (
              <div className="text-2xl font-bold">{activeResourcesCount}</div>
            )}
            <p className="text-[11px] text-muted-foreground mt-1">
              {resources.length} total registered
            </p>
          </CardContent>
        </Card>

        {/* Active Plans */}
        <Card>
          <CardHeader className="flex flex-row items-center justify-between pb-2 space-y-0">
            <CardTitle className="text-xs font-medium text-muted-foreground">
              Backup Plans
            </CardTitle>
            <Calendar className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            {isLoading ? (
              <Skeleton className="h-7 w-12" />
            ) : (
              <div className="text-2xl font-bold">{activePlansCount}</div>
            )}
            <p className="text-[11px] text-muted-foreground mt-1">
              {plans.length} total plans
            </p>
          </CardContent>
        </Card>

        {/* Running Jobs */}
        <Card>
          <CardHeader className="flex flex-row items-center justify-between pb-2 space-y-0">
            <CardTitle className="text-xs font-medium text-muted-foreground">
              Running Jobs
            </CardTitle>
            <PlayCircle className="h-4 w-4 text-primary" />
          </CardHeader>
          <CardContent>
            {isLoading ? (
              <Skeleton className="h-7 w-12" />
            ) : (
              <div className="text-2xl font-bold text-primary">{runningRunsCount}</div>
            )}
            <p className="text-[11px] text-muted-foreground mt-1">In progress</p>
          </CardContent>
        </Card>

        {/* Successful Runs */}
        <Card>
          <CardHeader className="flex flex-row items-center justify-between pb-2 space-y-0">
            <CardTitle className="text-xs font-medium text-muted-foreground">
              Successful Runs
            </CardTitle>
            <CheckCircle2 className="h-4 w-4 text-emerald-700 dark:text-emerald-400" />
          </CardHeader>
          <CardContent>
            {isLoading ? (
              <Skeleton className="h-7 w-12" />
            ) : (
              <div className="text-2xl font-bold text-emerald-700 dark:text-emerald-400">
                {successRunsCount}
              </div>
            )}
            <p className="text-[11px] text-muted-foreground mt-1">Completed safely</p>
          </CardContent>
        </Card>

        {/* Failed Runs */}
        <Card>
          <CardHeader className="flex flex-row items-center justify-between pb-2 space-y-0">
            <CardTitle className="text-xs font-medium text-muted-foreground">
              Failed Runs
            </CardTitle>
            <AlertTriangle className="h-4 w-4 text-destructive" />
          </CardHeader>
          <CardContent>
            {isLoading ? (
              <Skeleton className="h-7 w-12" />
            ) : (
              <div className="text-2xl font-bold text-destructive">{failedRunsCount}</div>
            )}
            <p className="text-[11px] text-muted-foreground mt-1">Require attention</p>
          </CardContent>
        </Card>

        {/* Storage Targets */}
        <Card>
          <CardHeader className="flex flex-row items-center justify-between pb-2 space-y-0">
            <CardTitle className="text-xs font-medium text-muted-foreground">
              Storage Targets
            </CardTitle>
            <HardDrive className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            {isLoading ? (
              <Skeleton className="h-7 w-12" />
            ) : (
              <div className="text-2xl font-bold">{storageTargets.length}</div>
            )}
            <p className="text-[11px] text-muted-foreground mt-1">Destinations active</p>
          </CardContent>
        </Card>
      </div>

      {/* Failures Requiring Attention Section */}
      {failedRuns.length > 0 && (
        <div className="rounded-lg border border-destructive/20 bg-destructive/5 p-4 space-y-3">
          <div className="flex items-center gap-2 text-destructive font-semibold text-sm">
            <AlertTriangle className="h-4 w-4 shrink-0" />
            <span>Failures Requiring Attention</span>
          </div>
          <div className="divide-y divide-destructive/10">
            {failedRuns.map((run) => (
              <div
                key={run.id}
                className="py-2.5 flex flex-col sm:flex-row sm:items-center justify-between gap-2 text-xs"
              >
                <div>
                  <span className="font-mono font-medium text-foreground">
                    Run {truncateId(run.id)}
                  </span>
                  <span className="text-muted-foreground mx-1.5">•</span>
                  <span className="text-muted-foreground">{formatDate(run.started_at)}</span>
                  {run.error_message && (
                    <p className="text-destructive font-mono text-[11px] mt-0.5 truncate max-w-md">
                      {run.error_message}
                    </p>
                  )}
                </div>
                <Link
                  href={`/runs/${run.id}`}
                  className="inline-flex items-center gap-1 text-primary hover:underline font-medium"
                >
                  View Details
                  <ArrowRight className="h-3 w-3" />
                </Link>
              </div>
            ))}
          </div>
        </div>
      )}

      {/* Recent Activity Table & Quick Links */}
      <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
        {/* Recent Runs List */}
        <Card className="lg:col-span-2">
          <CardHeader className="flex flex-row items-center justify-between pb-3">
            <div>
              <CardTitle className="text-base font-semibold">Recent Backup Activity</CardTitle>
              <p className="text-xs text-muted-foreground mt-0.5">
                Latest execution attempts across all plans
              </p>
            </div>
            <Link
              href="/runs"
              className="text-xs text-primary hover:underline flex items-center gap-1 font-medium"
            >
              View all
              <ArrowRight className="h-3 w-3" />
            </Link>
          </CardHeader>
          <CardContent>
            {isLoading ? (
              <div className="space-y-3">
                <Skeleton className="h-10 w-full" />
                <Skeleton className="h-10 w-full" />
                <Skeleton className="h-10 w-full" />
              </div>
            ) : recentRuns.length === 0 ? (
              <EmptyState
                icon={History}
                title="No backup runs yet"
                description="When backup jobs are triggered, execution history will appear here."
              />
            ) : (
              <div className="divide-y text-xs">
                {recentRuns.map((run) => {
                  const { label, variant } = getStatusBadgeVariant(run.status);
                  return (
                    <div
                      key={run.id}
                      className="py-3 flex items-center justify-between gap-4 hover:bg-muted/30 px-2 rounded-md transition-colors"
                    >
                      <div className="flex items-center gap-3 min-w-0">
                        <Badge variant={variant} className="capitalize shrink-0">
                          {label}
                        </Badge>
                        <div className="flex flex-col min-w-0">
                          <Link
                            href={`/runs/${run.id}`}
                            className="font-mono font-medium hover:underline text-foreground truncate"
                          >
                            {truncateId(run.id)}
                          </Link>
                          <span className="text-[11px] text-muted-foreground">
                            {formatDate(run.started_at)}
                          </span>
                        </div>
                      </div>
                      <div className="flex items-center gap-3 shrink-0 text-right">
                        <span className="text-muted-foreground">
                          {formatDuration(run.duration_seconds)}
                        </span>
                        <Link
                          href={`/runs/${run.id}`}
                          className="text-muted-foreground hover:text-foreground"
                          aria-label={`View run ${run.id}`}
                        >
                          <ArrowRight className="h-3.5 w-3.5" />
                        </Link>
                      </div>
                    </div>
                  );
                })}
              </div>
            )}
          </CardContent>
        </Card>

        {/* System Overview Side Card */}
        <Card>
          <CardHeader className="pb-3">
            <CardTitle className="text-base font-semibold">Infrastructure Status</CardTitle>
            <p className="text-xs text-muted-foreground mt-0.5">
              Current environment resources and storage
            </p>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="rounded-lg border p-3.5 space-y-2">
              <div className="flex items-center justify-between text-xs">
                <span className="font-medium text-foreground">Resources Online</span>
                <span className="font-semibold text-emerald-700 dark:text-emerald-400">
                  {activeResourcesCount} / {resources.length}
                </span>
              </div>
              <div className="flex items-center justify-between text-xs">
                <span className="font-medium text-foreground">Active Schedules</span>
                <span className="font-semibold text-foreground">
                  {activePlansCount} / {plans.length}
                </span>
              </div>
              <div className="flex items-center justify-between text-xs">
                <span className="font-medium text-foreground">Storage Targets</span>
                <span className="font-semibold text-foreground">
                  {storageTargets.length} configured
                </span>
              </div>
            </div>

            <div className="space-y-2 text-xs">
              <span className="font-semibold text-foreground block">Quick Navigation</span>
              <div className="grid grid-cols-2 gap-2">
                <Link
                  href="/resources"
                  className="flex items-center gap-2 rounded-md border p-2 hover:bg-muted transition-colors"
                >
                  <Server className="h-4 w-4 text-muted-foreground" />
                  <span className="truncate">Resources</span>
                </Link>
                <Link
                  href="/plans"
                  className="flex items-center gap-2 rounded-md border p-2 hover:bg-muted transition-colors"
                >
                  <Calendar className="h-4 w-4 text-muted-foreground" />
                  <span className="truncate">Plans</span>
                </Link>
                <Link
                  href="/runs"
                  className="flex items-center gap-2 rounded-md border p-2 hover:bg-muted transition-colors"
                >
                  <History className="h-4 w-4 text-muted-foreground" />
                  <span className="truncate">Runs</span>
                </Link>
                <Link
                  href="/storage"
                  className="flex items-center gap-2 rounded-md border p-2 hover:bg-muted transition-colors"
                >
                  <HardDrive className="h-4 w-4 text-muted-foreground" />
                  <span className="truncate">Storage</span>
                </Link>
              </div>
            </div>
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
