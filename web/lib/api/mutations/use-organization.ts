'use client';

import { useMutation, useQueryClient } from '@tanstack/react-query';
import { apiClient } from '@/lib/api/api-client';
import { useAuth } from '@/lib/auth/auth-context';
import { queryKeys } from '@/lib/query/query-client';
import { useToast } from '@/lib/toast/toast-context';
import { ApiError } from '@/types/api';
import type { UpdateOrganizationRequest } from '@/types/domain';
import type { OrganizationDetail } from '@/types/auth';

export function useUpdateOrganization() {
  const queryClient = useQueryClient();
  const { activeOrgId } = useAuth();
  const { toast } = useToast();

  return useMutation({
    mutationFn: async ({ id, data }: { id: string; data: UpdateOrganizationRequest }) => {
      return apiClient.put<OrganizationDetail>(`/organizations/${id}`, data);
    },
    onSuccess: (res) => {
      queryClient.invalidateQueries({
        queryKey: queryKeys.auth.me(),
      });
      if (activeOrgId) {
        queryClient.invalidateQueries({
          queryKey: queryKeys.org(activeOrgId).settings(),
        });
      }
      toast({
        title: 'Organization updated',
        description: `Organization "${res.name}" updated successfully.`,
        variant: 'success',
      });
    },
    onError: (err: unknown) => {
      let message = 'Failed to update organization.';
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
