'use client';

import { useMutation, useQueryClient } from '@tanstack/react-query';
import { apiClient } from '@/lib/api/api-client';
import { useAuth } from '@/lib/auth/auth-context';
import { queryKeys } from '@/lib/query/query-client';
import { useToast } from '@/lib/toast/toast-context';
import { ApiError } from '@/types/api';
import type {
  CreateBackupJobRequest,
  BackupJobResponse,
} from '@/types/domain';

export function useCreateBackupJob() {
  const queryClient = useQueryClient();
  const { activeOrgId } = useAuth();
  const { toast } = useToast();

  return useMutation({
    onMutate: () => ({ tenantOrgId: activeOrgId }),
    mutationFn: async (data: CreateBackupJobRequest) => {
      const tenantOrgId = activeOrgId;
      if (!tenantOrgId) throw new Error('No active organization selected.');
      return apiClient.post<BackupJobResponse>('/backup-jobs', data, { tenantOrgId });
    },
    onSuccess: (_res, _vars, context) => {
      const targetOrg = context?.tenantOrgId || activeOrgId;
      if (targetOrg) {
        queryClient.invalidateQueries({
          queryKey: queryKeys.org(targetOrg).runs.all(),
        });
        queryClient.invalidateQueries({
          queryKey: queryKeys.org(targetOrg).overview(),
        });
      }
      toast({
        title: 'Backup job queued',
        description: 'Backup operation has been accepted and scheduled for execution.',
        variant: 'success',
      });
    },
    onError: (err: unknown) => {
      let message = 'Failed to trigger backup job.';
      if (err instanceof ApiError) {
        message = err.message;
      }
      toast({
        title: 'Backup trigger failed',
        description: message,
        variant: 'destructive',
      });
    },
  });
}
