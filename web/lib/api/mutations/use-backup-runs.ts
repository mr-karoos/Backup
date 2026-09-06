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
    onMutate: () => ({ tenantOrgId: activeOrgId }),
    mutationFn: async (runId: string) => {
      const tenantOrgId = activeOrgId;
      if (!tenantOrgId) throw new Error('No active organization selected.');
      return apiClient.post<VerifyBackupRunResponse>(
        `/backup-runs/${runId}/verify`,
        undefined,
        { tenantOrgId }
      );
    },
    onSuccess: (res, runId, context) => {
      const targetOrg = context?.tenantOrgId || activeOrgId;
      if (targetOrg) {
        queryClient.invalidateQueries({
          queryKey: queryKeys.org(targetOrg).runs.all(),
        });
        queryClient.invalidateQueries({
          queryKey: queryKeys.org(targetOrg).runs.detail(runId),
        });
        queryClient.invalidateQueries({
          queryKey: queryKeys.org(targetOrg).artifacts.all(),
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
