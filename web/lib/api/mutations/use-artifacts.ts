'use client';

import { useMutation, useQueryClient } from '@tanstack/react-query';
import { apiClient } from '@/lib/api/api-client';
import { useAuth } from '@/lib/auth/auth-context';
import { queryKeys } from '@/lib/query/query-client';
import { useToast } from '@/lib/toast/toast-context';
import { ApiError } from '@/types/api';

export function useDeleteBackupArtifact() {
  const queryClient = useQueryClient();
  const { activeOrgId } = useAuth();
  const { toast } = useToast();

  return useMutation({
    onMutate: () => ({ tenantOrgId: activeOrgId }),
    mutationFn: async (artifactId: string) => {
      const tenantOrgId = activeOrgId;
      if (!tenantOrgId) throw new Error('No active organization selected.');
      return apiClient.delete(`/backup-artifacts/${artifactId}`, { tenantOrgId });
    },
    onSuccess: (_data, _vars, context) => {
      const targetOrg = context?.tenantOrgId || activeOrgId;
      if (targetOrg) {
        queryClient.invalidateQueries({
          queryKey: queryKeys.org(targetOrg).artifacts.all(),
        });
        queryClient.invalidateQueries({
          queryKey: queryKeys.org(targetOrg).runs.all(),
        });
      }
      toast({
        title: 'Artifact deleted',
        description: 'Backup artifact has been permanently removed from storage.',
        variant: 'success',
      });
    },
    onError: (err: unknown) => {
      let message = 'Failed to delete artifact.';
      if (err instanceof ApiError) {
        message = err.message;
      }
      toast({
        title: 'Deletion failed',
        description: message,
        variant: 'destructive',
      });
    },
  });
}
