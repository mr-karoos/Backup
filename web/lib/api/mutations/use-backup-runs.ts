'use client';

import { useMutation, useQueryClient } from '@tanstack/react-query';
import { apiClient } from '@/lib/api/api-client';
import { useAuth } from '@/lib/auth/auth-context';
import { queryKeys } from '@/lib/query/query-client';
import { useToast } from '@/lib/toast/toast-context';
import { ApiError } from '@/types/api';
import type { VerifyBackupRunResponse } from '@/types/domain';

export function useVerifyBackupRun() {
  const queryClient = useQueryClient();
  const { activeOrgId } = useAuth();
  const { toast } = useToast();

  return useMutation({
    mutationFn: async (runId: string) => {
      return apiClient.post<VerifyBackupRunResponse>(`/backup-runs/${runId}/verify`, {});
    },
    onSuccess: (res, runId) => {
      if (activeOrgId) {
        queryClient.invalidateQueries({
          queryKey: queryKeys.org(activeOrgId).runs.all(),
        });
        queryClient.invalidateQueries({
          queryKey: queryKeys.org(activeOrgId).runs.detail(runId),
        });
        queryClient.invalidateQueries({
          queryKey: queryKeys.org(activeOrgId).artifacts.all(),
        });
      }
      if (res.verification_status === 'verified') {
        toast({
          title: 'Verification passed',
          description: `Backup run verified successfully (Checksum: ${res.details.checksum_matched ? 'Valid' : 'Failed'}).`,
          variant: 'success',
        });
      } else {
        toast({
          title: 'Verification failed',
          description: `Run verification status: ${res.verification_status}. ${res.details.archive_integrity}`,
          variant: 'destructive',
        });
      }
    },
    onError: (err: unknown) => {
      let message = 'Failed to verify backup run.';
      if (err instanceof ApiError) {
        message = err.message;
      }
      toast({
        title: 'Verification error',
        description: message,
        variant: 'destructive',
      });
    },
  });
}
