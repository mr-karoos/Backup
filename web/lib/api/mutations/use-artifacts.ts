'use client';

import { useMutation, useQueryClient } from '@tanstack/react-query';
import { apiClient, triggerBlobDownload } from '@/lib/api/api-client';
import { useAuth } from '@/lib/auth/auth-context';
import { queryKeys } from '@/lib/query/query-client';
import { useToast } from '@/lib/toast/toast-context';
import { ApiError } from '@/types/api';

export function useDownloadBackupArtifact() {
  const { activeOrgId } = useAuth();
  const { toast } = useToast();

  return useMutation({
    mutationFn: async (artifactId: string) => {
      const tenantOrgId = activeOrgId;
      if (!tenantOrgId) throw new Error('No active organization selected.');
      const { blob, filename } = await apiClient.download(
        `/backup-artifacts/${artifactId}/download`,
        { tenantOrgId }
      );
      triggerBlobDownload(blob, filename);
      return filename;
    },
    onSuccess: (filename) => {
      toast({
        title: 'Download complete',
        description: `Downloaded ${filename} successfully.`,
        variant: 'success',
      });
    },
    onError: (err: unknown) => {
      let message = 'Failed to download artifact.';
      if (err instanceof ApiError) {
        if (err.status === 403) {
          message = 'You do not have permission to download this backup artifact.';
        } else if (err.status === 404) {
          message = 'Backup artifact not found or has been removed from storage.';
        } else {
          message = err.message;
        }
      }
      toast({
        title: 'Download failed',
        description: message,
        variant: 'destructive',
      });
    },
  });
}

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
