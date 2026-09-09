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
    onMutate: () => ({ tenantOrgId: activeOrgId }),
    mutationFn: async ({ id, data }: { id: string; data: UpdateOrganizationRequest }) => {
      return apiClient.put<OrganizationDetail>(`/organizations/${id}`, data, {
        tenantOrgId: id,
      });
    },
    onSuccess: (res, _vars, context) => {
      queryClient.invalidateQueries({
        queryKey: queryKeys.auth.me(),
      });
      const targetOrg = context?.tenantOrgId || activeOrgId;
      if (targetOrg) {
        queryClient.setQueryData(
          queryKeys.org(targetOrg).settings(),
          res
        );
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
