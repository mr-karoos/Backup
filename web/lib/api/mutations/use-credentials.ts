'use client';

import { useMutation, useQueryClient } from '@tanstack/react-query';
import { apiClient } from '@/lib/api/api-client';
import { useAuth } from '@/lib/auth/auth-context';
import { queryKeys } from '@/lib/query/query-client';
import { useToast } from '@/lib/toast/toast-context';
import {
  storeEphemeralSecret,
  consumeEphemeralSecret,
  clearEphemeralSecret,
} from '@/lib/security/ephemeral-secret-store';
import { ApiError } from '@/types/api';
import type {
  CredentialType,
  CreateCredentialRequest,
  UpdateCredentialRequest,
  CredentialCreateResponse,
  CredentialUpdateResponse,
} from '@/types/domain';

export interface EphemeralCreateCredentialVariables {
  name: string;
  type: CredentialType;
  invocationId: string;
}

export interface EphemeralUpdateCredentialVariables {
  id: string;
  name?: string;
  invocationId: string;
}

export function useCreateCredential() {
  const queryClient = useQueryClient();
  const { activeOrgId } = useAuth();
  const { toast } = useToast();

  const mutation = useMutation({
    onMutate: () => ({ tenantOrgId: activeOrgId }),
    mutationFn: async (vars: EphemeralCreateCredentialVariables) => {
      const secret = consumeEphemeralSecret<Record<string, unknown>>(vars.invocationId);
      const tenantOrgId = activeOrgId;
      if (!tenantOrgId) throw new Error('No active organization selected.');
      try {
        const payload: CreateCredentialRequest = {
          name: vars.name,
          type: vars.type,
          ...secret,
        };
        return await apiClient.post<CredentialCreateResponse>('/credentials', payload, {
          tenantOrgId,
        });
      } finally {
        clearEphemeralSecret(vars.invocationId);
      }
    },
    onSuccess: (res, _vars, context) => {
      const targetOrg = context?.tenantOrgId || activeOrgId;
      if (targetOrg) {
        queryClient.invalidateQueries({
          queryKey: queryKeys.org(targetOrg).credentials.all(),
        });
      }
      toast({
        title: 'Credential created',
        description: `Credential "${res.name}" registered successfully.`,
        variant: 'success',
      });
    },
    onError: (err: unknown) => {
      let message = 'Failed to create credential.';
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

  const mutateWithSecret = async (data: CreateCredentialRequest) => {
    const { name, type, ...secrets } = data;
    const invocationId =
      typeof crypto !== 'undefined' && crypto.randomUUID
        ? crypto.randomUUID()
        : `inv-${Date.now()}-${Math.random().toString(36).substring(2, 9)}`;
    storeEphemeralSecret(invocationId, secrets);
    return mutation.mutateAsync({ name, type, invocationId });
  };

  return {
    ...mutation,
    mutateWithSecret,
  };
}

export function useUpdateCredential() {
  const queryClient = useQueryClient();
  const { activeOrgId } = useAuth();
  const { toast } = useToast();

  const mutation = useMutation({
    onMutate: () => ({ tenantOrgId: activeOrgId }),
    mutationFn: async (vars: EphemeralUpdateCredentialVariables) => {
      const secret = consumeEphemeralSecret<Record<string, unknown>>(vars.invocationId);
      const tenantOrgId = activeOrgId;
      if (!tenantOrgId) throw new Error('No active organization selected.');
      try {
        const payload: UpdateCredentialRequest = {
          ...(vars.name !== undefined ? { name: vars.name } : {}),
          ...secret,
        };
        return await apiClient.put<CredentialUpdateResponse>(`/credentials/${vars.id}`, payload, {
          tenantOrgId,
        });
      } finally {
        clearEphemeralSecret(vars.invocationId);
      }
    },
    onSuccess: (res, _vars, context) => {
      const targetOrg = context?.tenantOrgId || activeOrgId;
      if (targetOrg) {
        queryClient.invalidateQueries({
          queryKey: queryKeys.org(targetOrg).credentials.all(),
        });
      }
      toast({
        title: 'Credential updated',
        description: `Credential "${res.name}" updated successfully.`,
        variant: 'success',
      });
    },
    onError: (err: unknown) => {
      let message = 'Failed to update credential.';
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

  const mutateWithSecret = async ({
    id,
    data,
  }: {
    id: string;
    data: UpdateCredentialRequest;
  }) => {
    const { name, ...secrets } = data;
    const invocationId =
      typeof crypto !== 'undefined' && crypto.randomUUID
        ? crypto.randomUUID()
        : `inv-${Date.now()}-${Math.random().toString(36).substring(2, 9)}`;
    storeEphemeralSecret(invocationId, secrets);
    return mutation.mutateAsync({ id, name, invocationId });
  };

  return {
    ...mutation,
    mutateWithSecret,
  };
}

export function useDeleteCredential() {
  const queryClient = useQueryClient();
  const { activeOrgId } = useAuth();
  const { toast } = useToast();

  return useMutation({
    onMutate: () => ({ tenantOrgId: activeOrgId }),
    mutationFn: async (id: string) => {
      const tenantOrgId = activeOrgId;
      if (!tenantOrgId) throw new Error('No active organization selected.');
      return apiClient.delete(`/credentials/${id}`, { tenantOrgId });
    },
    onSuccess: (_data, _vars, context) => {
      const targetOrg = context?.tenantOrgId || activeOrgId;
      if (targetOrg) {
        queryClient.invalidateQueries({
          queryKey: queryKeys.org(targetOrg).credentials.all(),
        });
      }
      toast({
        title: 'Credential deleted',
        description: 'Credential has been permanently removed.',
        variant: 'success',
      });
    },
    onError: (err: unknown) => {
      let message = 'Failed to delete credential.';
      if (err instanceof ApiError) {
        if (err.code === 'CREDENTIAL_IN_USE' || err.status === 409) {
          message = 'This credential is currently in use and cannot be deleted.';
        } else {
          message = err.message;
        }
      }
      toast({
        title: 'Deletion failed',
        description: message,
        variant: 'destructive',
      });
    },
  });
}
