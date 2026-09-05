'use client';

import { useMutation, useQueryClient } from '@tanstack/react-query';
import { apiClient } from '@/lib/api/api-client';
import { useAuth } from '@/lib/auth/auth-context';
import { queryKeys } from '@/lib/query/query-client';
import { useToast } from '@/lib/toast/toast-context';
import { ApiError } from '@/types/api';
import type {
  CreateCredentialRequest,
  UpdateCredentialRequest,
  CredentialCreateResponse,
  CredentialUpdateResponse,
} from '@/types/domain';

export function useCreateCredential() {
  const queryClient = useQueryClient();
  const { activeOrgId } = useAuth();
  const { toast } = useToast();

  return useMutation({
    mutationFn: async (data: CreateCredentialRequest) => {
      return apiClient.post<CredentialCreateResponse>('/credentials', data);
    },
    onSuccess: (res) => {
      if (activeOrgId) {
        queryClient.invalidateQueries({
          queryKey: queryKeys.org(activeOrgId).credentials.all(),
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
}

export function useUpdateCredential() {
  const queryClient = useQueryClient();
  const { activeOrgId } = useAuth();
  const { toast } = useToast();

  return useMutation({
    mutationFn: async ({ id, data }: { id: string; data: UpdateCredentialRequest }) => {
      return apiClient.put<CredentialUpdateResponse>(`/credentials/${id}`, data);
    },
    onSuccess: (res) => {
      if (activeOrgId) {
        queryClient.invalidateQueries({
          queryKey: queryKeys.org(activeOrgId).credentials.all(),
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
}

export function useDeleteCredential() {
  const queryClient = useQueryClient();
  const { activeOrgId } = useAuth();
  const { toast } = useToast();

  return useMutation({
    mutationFn: async (id: string) => {
      return apiClient.delete(`/credentials/${id}`);
    },
    onSuccess: () => {
      if (activeOrgId) {
        queryClient.invalidateQueries({
          queryKey: queryKeys.org(activeOrgId).credentials.all(),
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
