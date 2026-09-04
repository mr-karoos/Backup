import { QueryClient } from '@tanstack/react-query';
import { ApiError } from '@/types/api';

export const queryKeys = {
  health: () => ['health'] as const,
  auth: {
    me: () => ['auth', 'me'] as const,
    organizations: () => ['auth', 'organizations'] as const,
  },
  org: (orgId: string) => ({
    all: ['org', orgId] as const,
    settings: () => ['org', orgId, 'settings'] as const,
    resources: {
      all: () => ['org', orgId, 'resources'] as const,
      detail: (id: string) => ['org', orgId, 'resources', id] as const,
    },
    plans: {
      all: (filters?: Record<string, unknown>) => ['org', orgId, 'plans', filters ?? {}] as const,
      detail: (id: string) => ['org', orgId, 'plans', id] as const,
    },
    runs: {
      all: (filters?: Record<string, unknown>) => ['org', orgId, 'runs', filters ?? {}] as const,
      detail: (id: string) => ['org', orgId, 'runs', id] as const,
    },
    artifacts: {
      all: () => ['org', orgId, 'artifacts'] as const,
      detail: (id: string) => ['org', orgId, 'artifacts', id] as const,
    },
    storageTargets: {
      all: () => ['org', orgId, 'storage-targets'] as const,
      detail: (id: string) => ['org', orgId, 'storage-targets', id] as const,
    },
    credentials: {
      all: () => ['org', orgId, 'credentials'] as const,
    },
  }),
};

export function createQueryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: {
      queries: {
        staleTime: 30 * 1000, // 30 seconds default
        gcTime: 5 * 60 * 1000, // 5 minutes garbage collection
        refetchOnWindowFocus: false, // Prevent aggressive background requests
        retry: (failureCount, error) => {
          if (error instanceof ApiError) {
            // Never retry client-side errors
            if (
              error.status === 400 ||
              error.status === 401 ||
              error.status === 403 ||
              error.status === 404 ||
              error.status === 422
            ) {
              return false;
            }
          }
          // Max 1 retry for transient network/5xx failures
          return failureCount < 1;
        },
      },
    },
  });
}
