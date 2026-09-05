'use client';

import { useMutation, useQueryClient } from '@tanstack/react-query';
import { apiClient } from '@/lib/api/api-client';
import { useAuth } from '@/lib/auth/auth-context';
import { queryKeys } from '@/lib/query/query-client';
import { useToast } from '@/lib/toast/toast-context';
import { ApiError } from '@/types/api';
import type {
  CreateResourceRequest,
  UpdateResourceRequest,
  ResourceCreateResponse,
  ResourceUpdateResponse,
  ConnectionTestResponse,
  DiscoveredDatabaseResponse,
} from '@/types/domain';

export function useCreateResource() {
  const queryClient = useQueryClient();
  const { activeOrgId } = useAuth();
  const { toast } = useToast();

  return useMutation({
    mutationFn: async (data: CreateResourceRequest) => {
      return apiClient.post<ResourceCreateResponse>('/resources', data);
    },
    onSuccess: (res) => {
      if (activeOrgId) {
        queryClient.invalidateQueries({
          queryKey: queryKeys.org(activeOrgId).resources.all(),
        });
      }
      toast({
        title: 'Resource registered',
        description: `Resource "${res.name}" registered successfully.`,
        variant: 'success',
      });
    },
    onError: (err: unknown) => {
      let message = 'Failed to register resource.';
      if (err instanceof ApiError) {
        message = err.message;
      }
      toast({
        title: 'Registration failed',
        description: message,
        variant: 'destructive',
      });
    },
  });
}

export function useUpdateResource() {
  const queryClient = useQueryClient();
  const { activeOrgId } = useAuth();
  const { toast } = useToast();

  return useMutation({
    mutationFn: async ({ id, data }: { id: string; data: UpdateResourceRequest }) => {
      return apiClient.put<ResourceUpdateResponse>(`/resources/${id}`, data);
    },
    onSuccess: (res) => {
      if (activeOrgId) {
        queryClient.invalidateQueries({
          queryKey: queryKeys.org(activeOrgId).resources.all(),
        });
        queryClient.invalidateQueries({
          queryKey: queryKeys.org(activeOrgId).resources.detail(res.id),
        });
      }
      toast({
        title: 'Resource updated',
        description: `Resource "${res.name}" updated successfully.`,
        variant: 'success',
      });
    },
    onError: (err: unknown) => {
      let message = 'Failed to update resource.';
      if (err instanceof ApiError) {
        message = err.message;
      }
      toast({
        title: 'Update failed',
        description: message,
        variant: 'destructive',
      });
    },
  });
}

export function useArchiveResource() {
  const queryClient = useQueryClient();
  const { activeOrgId } = useAuth();
  const { toast } = useToast();

  return useMutation({
    mutationFn: async (id: string) => {
      return apiClient.delete(`/resources/${id}`);
    },
    onSuccess: () => {
      if (activeOrgId) {
        queryClient.invalidateQueries({
          queryKey: queryKeys.org(activeOrgId).resources.all(),
        });
      }
      toast({
        title: 'Resource archived',
        description: 'Resource has been archived successfully.',
        variant: 'success',
      });
    },
    onError: (err: unknown) => {
      let message = 'Failed to archive resource.';
      if (err instanceof ApiError) {
        message = err.message;
      }
      toast({
        title: 'Archive failed',
        description: message,
        variant: 'destructive',
      });
    },
  });
}

export function useTestResourceConnection() {
  const { toast } = useToast();

  return useMutation({
    mutationFn: async (id: string) => {
      return apiClient.post<ConnectionTestResponse>(`/resources/${id}/test-connection`, {});
    },
    onSuccess: (res) => {
      if (res.status === 'success') {
        toast({
          title: 'Connection successful',
          description: `Connected successfully in ${res.latency_ms}ms.`,
          variant: 'success',
        });
      } else {
        toast({
          title: 'Connection failed',
          description: `Connection test returned status: ${res.status}. Latency: ${res.latency_ms}ms.`,
          variant: 'destructive',
        });
      }
    },
    onError: (err: unknown) => {
      let message = 'Failed to test connection.';
      if (err instanceof ApiError) {
        message = err.message;
      }
      toast({
        title: 'Connection test error',
        description: message,
        variant: 'destructive',
      });
    },
  });
}

export function useDiscoverDatabases() {
  const { toast } = useToast();

  return useMutation({
    mutationFn: async (resourceId: string) => {
      return apiClient.get<DiscoveredDatabaseResponse[]>(`/resources/${resourceId}/databases`);
    },
    onSuccess: (databases) => {
      toast({
        title: 'Databases discovered',
        description: `Discovered ${databases.length} database(s).`,
        variant: 'success',
      });
    },
    onError: (err: unknown) => {
      let message = 'Failed to discover databases.';
      if (err instanceof ApiError) {
        message = err.message;
      }
      toast({
        title: 'Discovery failed',
        description: message,
        variant: 'destructive',
      });
    },
  });
}
