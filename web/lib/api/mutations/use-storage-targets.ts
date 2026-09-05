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
    mutationFn: async (data: CreateStorageTargetRequest) => {
      return apiClient.post<StorageTargetResponse>('/storage-targets', data);
    },
    onSuccess: (res) => {
      if (activeOrgId) {
        queryClient.invalidateQueries({
          queryKey: queryKeys.org(activeOrgId).storage.all(),
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
    mutationFn: async ({ id, data }: { id: string; data: UpdateStorageTargetRequest }) => {
      return apiClient.put<StorageTargetResponse>(`/storage-targets/${id}`, data);
    },
    onSuccess: (res) => {
      if (activeOrgId) {
        queryClient.invalidateQueries({
          queryKey: queryKeys.org(activeOrgId).storage.all(),
        });
        queryClient.invalidateQueries({
          queryKey: queryKeys.org(activeOrgId).storage.detail(res.id),
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
    mutationFn: async (id: string) => {
      return apiClient.delete(`/storage-targets/${id}`);
    },
    onSuccess: () => {
      if (activeOrgId) {
        queryClient.invalidateQueries({
          queryKey: queryKeys.org(activeOrgId).storage.all(),
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
