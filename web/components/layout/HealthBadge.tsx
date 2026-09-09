'use client';

import Link from 'next/link';
import { useQuery } from '@tanstack/react-query';
import { apiClient } from '@/lib/api/api-client';
import { queryKeys } from '@/lib/query/query-client';
import { type HealthResponse } from '@/types/domain';
import { Badge } from '@/components/ui/badge';
import { Activity } from 'lucide-react';

export function HealthBadge() {
  const { data, isError, isLoading } = useQuery<HealthResponse>({
    queryKey: queryKeys.health(),
    queryFn: () => apiClient.get<HealthResponse>('/health', { skipAuth: true, skipOrgHeader: true }),
    refetchInterval: 60 * 1000, // Conservative 60s polling
  });

  const isHealthy = !isError && data?.status === 'ok';

  return (
    <Link href="/health" title="View system health status" className="inline-flex items-center">
      <Badge
        variant={isLoading ? 'outline' : isHealthy ? 'success' : 'destructive'}
        className="gap-1.5 px-2.5 py-1 text-xs cursor-pointer hover:opacity-90 transition-opacity"
      >
        <Activity className={`h-3 w-3 ${isLoading ? 'animate-pulse' : ''}`} aria-hidden="true" />
        <span>{isLoading ? 'Checking...' : isHealthy ? 'System: OK' : 'System: Degraded'}</span>
      </Badge>
    </Link>
  );
}
