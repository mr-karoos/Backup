'use client';

import { useQuery } from '@tanstack/react-query';
import { apiClient } from '@/lib/api/api-client';
import { queryKeys } from '@/lib/query/query-client';
import { type HealthResponse } from '@/types/domain';
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Skeleton } from '@/components/ui/skeleton';
import { Activity, CheckCircle2, AlertCircle, RefreshCw } from 'lucide-react';

export default function SystemHealthPage() {
  const { data, isLoading, isError, refetch, isFetching, dataUpdatedAt } = useQuery<HealthResponse>({
    queryKey: queryKeys.health(),
    queryFn: () => apiClient.get<HealthResponse>('/health', { skipAuth: true, skipOrgHeader: true }),
    refetchInterval: 30 * 1000,
  });

  const isHealthy = !isError && data?.status === 'ok';

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
        <div>
          <h1 className="text-2xl font-bold tracking-tight text-foreground">System Health</h1>
          <p className="text-sm text-muted-foreground mt-0.5">
            Real-time liveness probe and service connectivity status
          </p>
        </div>
        <Button
          variant="outline"
          size="sm"
          onClick={() => refetch()}
          disabled={isFetching}
          className="gap-2 self-start"
        >
          <RefreshCw className={`h-4 w-4 ${isFetching ? 'animate-spin' : ''}`} />
          Refresh Probe
        </Button>
      </div>

      {/* Main Health Card */}
      <Card className="max-w-2xl">
        <CardHeader>
          <CardTitle className="text-base font-semibold flex items-center gap-2">
            <Activity className="h-4 w-4 text-muted-foreground" />
            Backend Service Probe (`/api/v1/health`)
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-6">
          {isLoading ? (
            <div className="space-y-3">
              <Skeleton className="h-12 w-full" />
              <Skeleton className="h-6 w-48" />
            </div>
          ) : (
            <>
              <div
                className={`flex items-center gap-4 rounded-xl border p-5 ${
                  isHealthy
                    ? 'border-emerald-500/20 bg-emerald-500/5'
                    : 'border-destructive/20 bg-destructive/5'
                }`}
              >
                <div
                  className={`flex h-12 w-12 items-center justify-center rounded-xl shrink-0 ${
                    isHealthy
                      ? 'bg-emerald-600 text-white dark:bg-emerald-500'
                      : 'bg-destructive text-destructive-foreground'
                  }`}
                >
                  {isHealthy ? (
                    <CheckCircle2 className="h-6 w-6" />
                  ) : (
                    <AlertCircle className="h-6 w-6" />
                  )}
                </div>
                <div>
                  <div className="flex items-center gap-2">
                    <h2 className="text-lg font-semibold text-foreground">
                      {isHealthy ? 'System Operational' : 'Service Unavailable'}
                    </h2>
                    <Badge variant={isHealthy ? 'success' : 'destructive'} className="uppercase text-xs">
                      {isHealthy ? 'OK' : 'DEGRADED'}
                    </Badge>
                  </div>
                  <p className="text-xs text-muted-foreground mt-0.5">
                    {isHealthy
                      ? 'Database connectivity verified and HTTP API responding normally.'
                      : 'Backend server or underlying database probe failed.'}
                  </p>
                </div>
              </div>

              <div className="space-y-2 border-t pt-4 text-xs text-muted-foreground">
                <div className="flex justify-between">
                  <span>Probe Endpoint:</span>
                  <span className="font-mono text-foreground">/api/v1/health</span>
                </div>
                <div className="flex justify-between">
                  <span>Backend Status Value:</span>
                  <span className="font-mono font-medium text-foreground capitalize">
                    {data?.status || 'unavailable'}
                  </span>
                </div>
                <div className="flex justify-between">
                  <span>Last Verified:</span>
                  <span className="font-mono text-foreground">
                    {dataUpdatedAt ? new Date(dataUpdatedAt).toLocaleTimeString() : '—'}
                  </span>
                </div>
              </div>
            </>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
