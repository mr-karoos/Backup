'use client';

import { useMutation, useQueryClient } from '@tanstack/react-query';
import { apiClient } from '@/lib/api/api-client';
import { useAuth } from '@/lib/auth/auth-context';
import { queryKeys } from '@/lib/query/query-client';
import { useToast } from '@/lib/toast/toast-context';
import { ApiError } from '@/types/api';
import type {
  CreateBackupPlanRequest,
  UpdateBackupPlanRequest,
  CreateBackupPlanResponse,
  BackupPlanResponse,
} from '@/types/domain';

export function useCreateBackupPlan() {
  const queryClient = useQueryClient();
  const { activeOrgId } = useAuth();
  const { toast } = useToast();

  return useMutation({
    mutationFn: async (data: CreateBackupPlanRequest) => {
      return apiClient.post<CreateBackupPlanResponse>('/backup-plans', data);
    },
    onSuccess: (res) => {
      if (activeOrgId) {
        queryClient.invalidateQueries({
          queryKey: queryKeys.org(activeOrgId).plans.all(),
        });
      }
      toast({
        title: 'Backup plan created',
        description: `Plan "${res.name}" created successfully.`,
        variant: 'success',
      });
    },
    onError: (err: unknown) => {
      let message = 'Failed to create backup plan.';
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

export function useUpdateBackupPlan() {
  const queryClient = useQueryClient();
  const { activeOrgId } = useAuth();
  const { toast } = useToast();

  return useMutation({
    mutationFn: async ({ id, data }: { id: string; data: UpdateBackupPlanRequest }) => {
      return apiClient.put<BackupPlanResponse>(`/backup-plans/${id}`, data);
    },
    onSuccess: (res) => {
      if (activeOrgId) {
        queryClient.invalidateQueries({
          queryKey: queryKeys.org(activeOrgId).plans.all(),
        });
        queryClient.invalidateQueries({
          queryKey: queryKeys.org(activeOrgId).plans.detail(res.id),
        });
      }
      toast({
        title: 'Backup plan updated',
        description: `Plan "${res.name}" updated successfully.`,
        variant: 'success',
      });
    },
    onError: (err: unknown) => {
      let message = 'Failed to update backup plan.';
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

export function useArchiveBackupPlan() {
  const queryClient = useQueryClient();
  const { activeOrgId } = useAuth();
  const { toast } = useToast();

  return useMutation({
    mutationFn: async (id: string) => {
      return apiClient.delete(`/backup-plans/${id}`);
    },
    onSuccess: () => {
      if (activeOrgId) {
        queryClient.invalidateQueries({
          queryKey: queryKeys.org(activeOrgId).plans.all(),
        });
      }
      toast({
        title: 'Backup plan archived',
        description: 'Backup plan has been archived successfully.',
        variant: 'success',
      });
    },
    onError: (err: unknown) => {
      let message = 'Failed to archive backup plan.';
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
