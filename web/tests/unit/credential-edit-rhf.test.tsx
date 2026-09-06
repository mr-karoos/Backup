import React from 'react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { credentialEditSchema } from '@/lib/forms/schemas';
import { EditCredentialDialog } from '@/app/(dashboard)/credentials/page';
import * as AuthContextModule from '@/lib/auth/auth-context';
import { apiClient } from '@/lib/api/api-client';
import { ToastProvider } from '@/lib/toast/toast-context';
import type { CredentialListItemResponse } from '@/types/domain';

// Mock useRouter
vi.mock('next/navigation', () => ({
  useRouter: () => ({
    push: vi.fn(),
    replace: vi.fn(),
    prefetch: vi.fn(),
  }),
}));

function createWrapper(queryClient: QueryClient) {
  return function Wrapper({ children }: { children: React.ReactNode }) {
    return (
      <QueryClientProvider client={queryClient}>
        <ToastProvider>
          {children}
        </ToastProvider>
      </QueryClientProvider>
    );
  };
}

describe('Credential Edit with React Hook Form + Zod', () => {
  let queryClient: QueryClient;

  beforeEach(() => {
    vi.clearAllMocks();
    queryClient = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false },
      },
    });

    vi.spyOn(AuthContextModule, 'useAuth').mockReturnValue({
      status: 'authenticated',
      user: { id: 'u1', email: 'admin@domain.com', full_name: 'Admin', is_system_admin: false },
      memberships: [],
      activeOrgId: 'org-1',
      activeMembership: {
        organization_id: 'org-1',
        organization_name: 'Org 1',
        organization_slug: 'org-1',
        is_default_internal: true,
        role: 'admin',
        status: 'active',
        permissions: ['credential:read', 'credential:write'],
      },
      isSystemAdmin: false,
      userRole: 'admin',
      login: vi.fn(),
      logout: vi.fn(),
      switchOrganization: vi.fn(),
    });
  });

  describe('credentialEditSchema Validation', () => {
    it('accepts valid name-only update without secrets', () => {
      const parsed = credentialEditSchema.safeParse({
        name: 'Updated Credential Name',
      });
      expect(parsed.success).toBe(true);
      if (parsed.success) {
        expect(parsed.data.name).toBe('Updated Credential Name');
      }
    });

    it('rejects empty or whitespace-only name', () => {
      const empty = credentialEditSchema.safeParse({ name: '' });
      expect(empty.success).toBe(false);

      const whitespace = credentialEditSchema.safeParse({ name: '   ' });
      expect(whitespace.success).toBe(false);
    });

    it('rejects name exceeding 255 characters', () => {
      const longName = 'a'.repeat(256);
      const parsed = credentialEditSchema.safeParse({ name: longName });
      expect(parsed.success).toBe(false);
    });

    it('accepts valid SSH password replacement', () => {
      const parsed = credentialEditSchema.safeParse({
        name: 'SSH Server',
        password: 'new-secure-password-123',
      });
      expect(parsed.success).toBe(true);
    });

    it('accepts valid SSH private key with optional passphrase', () => {
      const parsed = credentialEditSchema.safeParse({
        name: 'SSH Key',
        private_key: '-----BEGIN OPENSSH PRIVATE KEY-----\nkey\n-----END OPENSSH PRIVATE KEY-----',
        passphrase: 'key-passphrase',
      });
      expect(parsed.success).toBe(true);
    });

    it('accepts valid cPanel API token replacement', () => {
      const parsed = credentialEditSchema.safeParse({
        name: 'cPanel Host',
        api_token: 'CPANELTOKEN1234567890',
      });
      expect(parsed.success).toBe(true);
    });

    it('accepts valid complete S3 credentials replacement', () => {
      const parsed = credentialEditSchema.safeParse({
        name: 'S3 Target',
        access_key_id: 'AKIAEXAMPLEKEY123',
        secret_access_key: 'SECRETKEY1234567890',
        session_token: 'OPTIONAL_SESSION_TOKEN',
      });
      expect(parsed.success).toBe(true);
    });

    it('rejects incomplete S3 credentials (access_key_id without secret_access_key)', () => {
      const parsed = credentialEditSchema.safeParse({
        name: 'S3 Target',
        access_key_id: 'AKIAEXAMPLEKEY123',
      });
      expect(parsed.success).toBe(false);
      if (!parsed.success) {
        expect(parsed.error.issues[0]?.path).toContain('secret_access_key');
      }
    });

    it('rejects incomplete S3 credentials (secret_access_key without access_key_id)', () => {
      const parsed = credentialEditSchema.safeParse({
        name: 'S3 Target',
        secret_access_key: 'SECRETKEY1234567890',
      });
      expect(parsed.success).toBe(false);
      if (!parsed.success) {
        expect(parsed.error.issues[0]?.path).toContain('access_key_id');
      }
    });
  });

  describe('EditCredentialDialog Component Lifecycle & Security', () => {
    const mockSshPasswordCred: CredentialListItemResponse = {
      id: 'cred-pwd-1',
      name: 'Production SSH Password',
      type: 'ssh_password',
      fingerprint: null,
      key_version: 1,
      created_at: '2026-01-01T00:00:00Z',
    };

    const mockS3Cred: CredentialListItemResponse = {
      id: 'cred-s3-1',
      name: 'Backup Bucket S3',
      type: 's3_credentials',
      fingerprint: 'AKIA...',
      key_version: 1,
      created_at: '2026-01-01T00:00:00Z',
    };

    it('populates name but keeps secret inputs strictly empty (never pre-populated)', () => {
      render(
        <EditCredentialDialog
          cred={mockSshPasswordCred}
          open={true}
          onClose={vi.fn()}
        />,
        { wrapper: createWrapper(queryClient) }
      );

      const nameInput = screen.getByLabelText(/Credential Name/i) as HTMLInputElement;
      expect(nameInput.value).toBe('Production SSH Password');

      const passwordInput = screen.getByPlaceholderText(/Enter new password/i) as HTMLInputElement;
      expect(passwordInput.value).toBe('');
    });

    it('renders appropriate fields for S3 credentials type with empty secrets', () => {
      render(
        <EditCredentialDialog
          cred={mockS3Cred}
          open={true}
          onClose={vi.fn()}
        />,
        { wrapper: createWrapper(queryClient) }
      );

      const nameInput = screen.getByLabelText(/Credential Name/i) as HTMLInputElement;
      expect(nameInput.value).toBe('Backup Bucket S3');

      const accessKeyInput = screen.getByPlaceholderText(/AKIA.../i) as HTMLInputElement;
      expect(accessKeyInput.value).toBe('');

      const secretKeyInput = screen.getByPlaceholderText(/New Secret Key/i) as HTMLInputElement;
      expect(secretKeyInput.value).toBe('');

      const sessionTokenInput = screen.getByPlaceholderText(/New session token/i) as HTMLInputElement;
      expect(sessionTokenInput.value).toBe('');
    });

    it('submits name-only update without sending any secret fields', async () => {
      const putSpy = vi.spyOn(apiClient, 'put').mockResolvedValueOnce({
        id: 'cred-pwd-1',
        name: 'Renamed SSH Password',
        type: 'ssh_password',
        fingerprint: null,
        key_version: 1,
        created_at: '2026-01-01T00:00:00Z',
        updated_at: '2026-01-02T00:00:00Z',
      });

      const onClose = vi.fn();

      render(
        <EditCredentialDialog
          cred={mockSshPasswordCred}
          open={true}
          onClose={onClose}
        />,
        { wrapper: createWrapper(queryClient) }
      );

      const nameInput = screen.getByLabelText(/Credential Name/i);
      fireEvent.change(nameInput, { target: { value: 'Renamed SSH Password' } });

      const saveBtn = screen.getByRole('button', { name: /Save Changes/i });
      fireEvent.click(saveBtn);

      await waitFor(() => {
        expect(putSpy).toHaveBeenCalledTimes(1);
      });

      expect(putSpy).toHaveBeenCalledWith(
        '/credentials/cred-pwd-1',
        { name: 'Renamed SSH Password' },
        { tenantOrgId: 'org-1' }
      );

      expect(onClose).toHaveBeenCalledTimes(1);
    });

    it('replaces secret material and ensures TanStack MutationCache contains zero plaintext secrets', async () => {
      const secretSentinel = 'super-secret-ssh-pwd-999';

      const putSpy = vi.spyOn(apiClient, 'put').mockResolvedValueOnce({
        id: 'cred-pwd-1',
        name: 'Production SSH Password',
        type: 'ssh_password',
        fingerprint: null,
        key_version: 2,
        created_at: '2026-01-01T00:00:00Z',
        updated_at: '2026-01-02T00:00:00Z',
      });

      const onClose = vi.fn();

      render(
        <EditCredentialDialog
          cred={mockSshPasswordCred}
          open={true}
          onClose={onClose}
        />,
        { wrapper: createWrapper(queryClient) }
      );

      const passwordInput = screen.getByPlaceholderText(/Enter new password/i);
      fireEvent.change(passwordInput, { target: { value: secretSentinel } });

      const saveBtn = screen.getByRole('button', { name: /Save Changes/i });
      fireEvent.click(saveBtn);

      await waitFor(() => {
        expect(putSpy).toHaveBeenCalledTimes(1);
      });

      // Assert that API received the secret payload
      expect(putSpy).toHaveBeenCalledWith(
        '/credentials/cred-pwd-1',
        { name: 'Production SSH Password', secret: secretSentinel },
        { tenantOrgId: 'org-1' }
      );

      // Verify that TanStack MutationCache does NOT leak the secretSentinel
      const mutations = queryClient.getMutationCache().getAll();
      expect(mutations.length).toBeGreaterThan(0);

      for (const mut of mutations) {
        const serialized = JSON.stringify(mut.state.variables || {});
        expect(serialized).not.toContain(secretSentinel);
      }
    });

    it('on mutation failure, retains typed values in memory so user can retry', async () => {
      vi.spyOn(apiClient, 'put').mockRejectedValueOnce(new Error('Network failure'));

      const onClose = vi.fn();

      render(
        <EditCredentialDialog
          cred={mockSshPasswordCred}
          open={true}
          onClose={onClose}
        />,
        { wrapper: createWrapper(queryClient) }
      );

      const passwordInput = screen.getByPlaceholderText(/Enter new password/i) as HTMLInputElement;
      fireEvent.change(passwordInput, { target: { value: 'my-retry-password' } });

      const saveBtn = screen.getByRole('button', { name: /Save Changes/i });
      fireEvent.click(saveBtn);

      await waitFor(() => {
        expect(passwordInput.value).toBe('my-retry-password');
      });

      // Dialog must remain open
      expect(onClose).not.toHaveBeenCalled();
    });

    it('prompts confirmation when user clicks Cancel on dirty form', async () => {
      const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(false);
      const onClose = vi.fn();

      render(
        <EditCredentialDialog
          cred={mockSshPasswordCred}
          open={true}
          onClose={onClose}
        />,
        { wrapper: createWrapper(queryClient) }
      );

      const nameInput = screen.getByLabelText(/Credential Name/i);
      fireEvent.change(nameInput, { target: { value: 'Changed Name' } });

      const cancelBtn = screen.getByRole('button', { name: /Cancel/i });
      fireEvent.click(cancelBtn);

      expect(confirmSpy).toHaveBeenCalledWith('You have unsaved changes. Are you sure you want to discard them?');
      expect(onClose).not.toHaveBeenCalled();

      // When confirmed, closes dialog
      confirmSpy.mockReturnValue(true);
      fireEvent.click(cancelBtn);
      expect(onClose).toHaveBeenCalledTimes(1);
    });
  });
});
