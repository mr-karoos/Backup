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
    mutationFn: async (data: CreateBackupJobRequest) => {
      return apiClient.post<BackupJobResponse>('/backup-jobs', data);
    },
    onSuccess: () => {
      if (activeOrgId) {
        queryClient.invalidateQueries({
          queryKey: queryKeys.org(activeOrgId).runs.all(),
        });
        queryClient.invalidateQueries({
          queryKey: queryKeys.org(activeOrgId).overview(),
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
