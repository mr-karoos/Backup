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
    onMutate: () => ({ tenantOrgId: activeOrgId }),
    mutationFn: async (data: CreateBackupPlanRequest) => {
      const tenantOrgId = activeOrgId;
      if (!tenantOrgId) throw new Error('No active organization selected.');
      return apiClient.post<CreateBackupPlanResponse>('/backup-plans', data, { tenantOrgId });
    },
    onSuccess: (res, _vars, context) => {
      const targetOrg = context?.tenantOrgId || activeOrgId;
      if (targetOrg) {
        queryClient.invalidateQueries({
          queryKey: queryKeys.org(targetOrg).plans.all(),
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
    onMutate: () => ({ tenantOrgId: activeOrgId }),
    mutationFn: async ({ id, data }: { id: string; data: UpdateBackupPlanRequest }) => {
      const tenantOrgId = activeOrgId;
      if (!tenantOrgId) throw new Error('No active organization selected.');
      return apiClient.put<BackupPlanResponse>(`/backup-plans/${id}`, data, { tenantOrgId });
    },
    onSuccess: (res, _vars, context) => {
      const targetOrg = context?.tenantOrgId || activeOrgId;
      if (targetOrg) {
        queryClient.invalidateQueries({
          queryKey: queryKeys.org(targetOrg).plans.all(),
        });
        queryClient.invalidateQueries({
          queryKey: queryKeys.org(targetOrg).plans.detail(res.id),
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
    onMutate: () => ({ tenantOrgId: activeOrgId }),
    mutationFn: async (id: string) => {
      const tenantOrgId = activeOrgId;
      if (!tenantOrgId) throw new Error('No active organization selected.');
      return apiClient.delete(`/backup-plans/${id}`, { tenantOrgId });
    },
    onSuccess: (_data, _vars, context) => {
      const targetOrg = context?.tenantOrgId || activeOrgId;
      if (targetOrg) {
        queryClient.invalidateQueries({
          queryKey: queryKeys.org(targetOrg).plans.all(),
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
