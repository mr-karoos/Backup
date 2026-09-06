'use client';

import { useMutation, useQueryClient } from '@tanstack/react-query';
import { apiClient } from '@/lib/api/api-client';
import { useAuth } from '@/lib/auth/auth-context';
import { queryKeys } from '@/lib/query/query-client';
import { useToast } from '@/lib/toast/toast-context';
import { ApiError } from '@/types/api';
import type {
  CreateStorageTargetRequest,
  UpdateStorageTargetRequest,
  StorageTargetResponse,
} from '@/types/domain';

export function useCreateStorageTarget() {
  const queryClient = useQueryClient();
  const { activeOrgId } = useAuth();
  const { toast } = useToast();

  return useMutation({
    onMutate: () => ({ tenantOrgId: activeOrgId }),
    mutationFn: async (data: CreateStorageTargetRequest) => {
      const tenantOrgId = activeOrgId;
      if (!tenantOrgId) throw new Error('No active organization selected.');
      return apiClient.post<StorageTargetResponse>('/storage-targets', data, { tenantOrgId });
    },
    onSuccess: (res, _vars, context) => {
      const targetOrg = context?.tenantOrgId || activeOrgId;
      if (targetOrg) {
        queryClient.invalidateQueries({
          queryKey: queryKeys.org(targetOrg).storage.all(),
        });
      }
      toast({
        title: 'Storage target created',
        description: `Storage target "${res.name}" created successfully.`,
        variant: 'success',
      });
    },
    onError: (err: unknown) => {
      let message = 'Failed to create storage target.';
      if (err instanceof ApiError) {
        message = err.message;
      }
      toast({
        title: 'Creation failed',
        description: message,
        variant: 'destructive',
      });
    },
  });
}

export function useUpdateStorageTarget() {
  const queryClient = useQueryClient();
  const { activeOrgId } = useAuth();
  const { toast } = useToast();

  return useMutation({
    onMutate: () => ({ tenantOrgId: activeOrgId }),
    mutationFn: async ({ id, data }: { id: string; data: UpdateStorageTargetRequest }) => {
      const tenantOrgId = activeOrgId;
      if (!tenantOrgId) throw new Error('No active organization selected.');
      return apiClient.put<StorageTargetResponse>(`/storage-targets/${id}`, data, { tenantOrgId });
    },
    onSuccess: (res, _vars, context) => {
      const targetOrg = context?.tenantOrgId || activeOrgId;
      if (targetOrg) {
        queryClient.invalidateQueries({
          queryKey: queryKeys.org(targetOrg).storage.all(),
        });
        queryClient.invalidateQueries({
          queryKey: queryKeys.org(targetOrg).storage.detail(res.id),
        });
      }
      toast({
        title: 'Storage target updated',
        description: `Storage target "${res.name}" updated successfully.`,
        variant: 'success',
      });
    },
    onError: (err: unknown) => {
      let message = 'Failed to update storage target.';
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

export function useDeleteStorageTarget() {
  const queryClient = useQueryClient();
  const { activeOrgId } = useAuth();
  const { toast } = useToast();

  return useMutation({
    onMutate: () => ({ tenantOrgId: activeOrgId }),
    mutationFn: async (id: string) => {
      const tenantOrgId = activeOrgId;
      if (!tenantOrgId) throw new Error('No active organization selected.');
      return apiClient.delete(`/storage-targets/${id}`, { tenantOrgId });
    },
    onSuccess: (_data, _vars, context) => {
      const targetOrg = context?.tenantOrgId || activeOrgId;
      if (targetOrg) {
        queryClient.invalidateQueries({
          queryKey: queryKeys.org(targetOrg).storage.all(),
        });
      }
      toast({
        title: 'Storage target deleted',
        description: 'Storage target has been removed successfully.',
        variant: 'success',
      });
    },
    onError: (err: unknown) => {
      let message = 'Failed to delete storage target.';
      if (err instanceof ApiError) {
        if (err.status === 409) {
          message = err.message || 'Storage target cannot be deleted because it is in use or is the default target.';
        } else {
          message = err.message;
        }
      }
      toast({
        title: 'Deletion failed',
        description: message,
        variant: 'destructive',
      });
    },
  });
}
