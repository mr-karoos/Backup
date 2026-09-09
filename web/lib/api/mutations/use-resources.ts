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
    onMutate: () => ({ tenantOrgId: activeOrgId }),
    mutationFn: async (data: CreateResourceRequest) => {
      const tenantOrgId = activeOrgId;
      if (!tenantOrgId) throw new Error('No active organization selected.');
      return apiClient.post<ResourceCreateResponse>('/resources', data, { tenantOrgId });
    },
    onSuccess: (res, _vars, context) => {
      const targetOrg = context?.tenantOrgId || activeOrgId;
      if (targetOrg) {
        queryClient.invalidateQueries({
          queryKey: queryKeys.org(targetOrg).resources.all(),
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
    onMutate: () => ({ tenantOrgId: activeOrgId }),
    mutationFn: async ({ id, data }: { id: string; data: UpdateResourceRequest }) => {
      const tenantOrgId = activeOrgId;
      if (!tenantOrgId) throw new Error('No active organization selected.');
      return apiClient.put<ResourceUpdateResponse>(`/resources/${id}`, data, { tenantOrgId });
    },
    onSuccess: (res, _vars, context) => {
      const targetOrg = context?.tenantOrgId || activeOrgId;
      if (targetOrg) {
        queryClient.invalidateQueries({
          queryKey: queryKeys.org(targetOrg).resources.all(),
        });
        queryClient.invalidateQueries({
          queryKey: queryKeys.org(targetOrg).resources.detail(res.id),
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
    onMutate: () => ({ tenantOrgId: activeOrgId }),
    mutationFn: async (id: string) => {
      const tenantOrgId = activeOrgId;
      if (!tenantOrgId) throw new Error('No active organization selected.');
      return apiClient.delete(`/resources/${id}`, { tenantOrgId });
    },
    onSuccess: (_data, _vars, context) => {
      const targetOrg = context?.tenantOrgId || activeOrgId;
      if (targetOrg) {
        queryClient.invalidateQueries({
          queryKey: queryKeys.org(targetOrg).resources.all(),
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
  const { activeOrgId } = useAuth();
  const { toast } = useToast();

  return useMutation({
    onMutate: () => ({ tenantOrgId: activeOrgId }),
    mutationFn: async (id: string) => {
      const tenantOrgId = activeOrgId;
      if (!tenantOrgId) throw new Error('No active organization selected.');
      return apiClient.post<ConnectionTestResponse>(`/resources/${id}/test-connection`, undefined, {
        tenantOrgId,
      });
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
          title: 'Connection test failed',
          description: 'The resource could not be reached with the configured credentials.',
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
  const { activeOrgId } = useAuth();
  const { toast } = useToast();

  return useMutation({
    onMutate: () => ({ tenantOrgId: activeOrgId }),
    mutationFn: async (id: string) => {
      const tenantOrgId = activeOrgId;
      if (!tenantOrgId) throw new Error('No active organization selected.');
      return apiClient.get<DiscoveredDatabaseResponse[]>(`/resources/${id}/databases`, {
        tenantOrgId,
      });
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
