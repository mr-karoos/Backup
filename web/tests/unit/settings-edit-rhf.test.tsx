import React from 'react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { organizationEditSchema } from '@/lib/forms/schemas';
import OrganizationSettingsPage from '@/app/(dashboard)/settings/page';
import * as AuthContextModule from '@/lib/auth/auth-context';
import { apiClient } from '@/lib/api/api-client';
import { queryKeys } from '@/lib/query/query-client';
import type { OrganizationDetail } from '@/types/auth';

// Mock useRouter with a stable object reference across renders
const mockRouter = {
  push: vi.fn(),
  replace: vi.fn(),
  prefetch: vi.fn(),
};
vi.mock('next/navigation', () => ({
  useRouter: () => mockRouter,
}));

// Mock useToast to prevent background auto-dismiss setTimeout handles
const mockToast = vi.fn();
vi.mock('@/lib/toast/toast-context', () => ({
  useToast: () => ({
    toast: mockToast,
    dismiss: vi.fn(),
  }),
  ToastProvider: ({ children }: { children: React.ReactNode }) => <>{children}</>,
}));

function createWrapper(queryClient: QueryClient) {
  return function Wrapper({ children }: { children: React.ReactNode }) {
    return (
      <QueryClientProvider client={queryClient}>
        {children}
      </QueryClientProvider>
    );
  };
}

describe('Organization Settings Edit with Unsaved Guard & Cache Update', () => {
  let queryClient: QueryClient;

  const sampleOrg: OrganizationDetail = {
    id: 'org-1',
    name: 'Acme Corporation',
    slug: 'acme-corp',
    is_default_internal: true,
    status: 'active',
    metadata: { env: 'production' },
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
  };

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
        organization_name: 'Acme Corporation',
        organization_slug: 'acme-corp',
        is_default_internal: true,
        role: 'admin',
        status: 'active',
        permissions: ['org:read', 'org:write', 'user:read'],
      },
      isSystemAdmin: false,
      userRole: 'admin',
      login: vi.fn(),
      logout: vi.fn(),
      switchOrganization: vi.fn(),
    });
  });

  describe('organizationEditSchema Validation', () => {
    it('accepts valid organization name', () => {
      const parsed = organizationEditSchema.safeParse({
        name: 'Updated Acme Global',
      });
      expect(parsed.success).toBe(true);
      if (parsed.success) {
        expect(parsed.data.name).toBe('Updated Acme Global');
      }
    });

    it('rejects empty or whitespace-only name', () => {
      const empty = organizationEditSchema.safeParse({ name: '' });
      expect(empty.success).toBe(false);

      const whitespace = organizationEditSchema.safeParse({ name: '   ' });
      expect(whitespace.success).toBe(false);
    });

    it('rejects name exceeding 255 characters', () => {
      const longName = 'a'.repeat(256);
      const parsed = organizationEditSchema.safeParse({ name: longName });
      expect(parsed.success).toBe(false);
    });
  });

  describe('OrganizationSettingsPage Component Lifecycle, Unsaved Guard & Cache Update', () => {
    it('renders organization details and opens edit modal prefilled with current name', async () => {
      vi.spyOn(apiClient, 'get').mockResolvedValueOnce(sampleOrg);

      render(<OrganizationSettingsPage />, {
        wrapper: createWrapper(queryClient),
      });

      await waitFor(() => {
        expect(screen.getByText('Acme Corporation')).toBeInTheDocument();
      });

      const editBtn = screen.getByRole('button', { name: /edit organization/i });
      fireEvent.click(editBtn);

      await waitFor(() => {
        expect(screen.getByLabelText(/organization name/i)).toHaveValue('Acme Corporation');
      });
    });

    it('closes modal directly on Cancel when form is pristine without window.confirm', async () => {
      vi.spyOn(apiClient, 'get').mockResolvedValueOnce(sampleOrg);
      const confirmSpy = vi.spyOn(window, 'confirm');

      render(<OrganizationSettingsPage />, {
        wrapper: createWrapper(queryClient),
      });

      await waitFor(() => {
        expect(screen.getByText('Acme Corporation')).toBeInTheDocument();
      });

      fireEvent.click(screen.getByRole('button', { name: /edit organization/i }));

      await waitFor(() => {
        expect(screen.getByLabelText(/organization name/i)).toHaveValue('Acme Corporation');
      });

      fireEvent.click(screen.getByRole('button', { name: /cancel/i }));

      expect(confirmSpy).not.toHaveBeenCalled();
      await waitFor(() => {
        expect(screen.queryByLabelText(/organization name/i)).not.toBeInTheDocument();
      });
    });

    it('warns with window.confirm on Cancel when form is dirty; stays open if discarded is rejected', async () => {
      vi.spyOn(apiClient, 'get').mockResolvedValueOnce(sampleOrg);
      const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(false);

      render(<OrganizationSettingsPage />, {
        wrapper: createWrapper(queryClient),
      });

      await waitFor(() => {
        expect(screen.getByText('Acme Corporation')).toBeInTheDocument();
      });

      fireEvent.click(screen.getByRole('button', { name: /edit organization/i }));

      const input = await screen.findByLabelText(/organization name/i);
      fireEvent.change(input, { target: { value: 'Acme Modified' } });

      fireEvent.click(screen.getByRole('button', { name: /cancel/i }));

      expect(confirmSpy).toHaveBeenCalledWith(
        'You have unsaved changes. Are you sure you want to discard them?'
      );
      expect(screen.getByLabelText(/organization name/i)).toHaveValue('Acme Modified');
    });

    it('closes and resets form when dirty Cancel is confirmed by user', async () => {
      vi.spyOn(apiClient, 'get').mockResolvedValueOnce(sampleOrg);
      vi.spyOn(window, 'confirm').mockReturnValue(true);

      render(<OrganizationSettingsPage />, {
        wrapper: createWrapper(queryClient),
      });

      await waitFor(() => {
        expect(screen.getByText('Acme Corporation')).toBeInTheDocument();
      });

      fireEvent.click(screen.getByRole('button', { name: /edit organization/i }));

      const input = await screen.findByLabelText(/organization name/i);
      fireEvent.change(input, { target: { value: 'Acme Modified' } });

      fireEvent.click(screen.getByRole('button', { name: /cancel/i }));

      await waitFor(() => {
        expect(screen.queryByLabelText(/organization name/i)).not.toBeInTheDocument();
      });
    });

    it('keeps dialog open and preserves edits on backend update failure', async () => {
      vi.spyOn(apiClient, 'get').mockResolvedValueOnce(sampleOrg);
      vi.spyOn(apiClient, 'put').mockRejectedValueOnce(new Error('Update failed'));

      render(<OrganizationSettingsPage />, {
        wrapper: createWrapper(queryClient),
      });

      await waitFor(() => {
        expect(screen.getByText('Acme Corporation')).toBeInTheDocument();
      });

      fireEvent.click(screen.getByRole('button', { name: /edit organization/i }));

      const input = await screen.findByLabelText(/organization name/i);
      fireEvent.change(input, { target: { value: 'Acme Failed Update' } });

      fireEvent.click(screen.getByRole('button', { name: /save changes/i }));

      await waitFor(() => {
        expect(screen.getByLabelText(/organization name/i)).toHaveValue('Acme Failed Update');
      });
    });

    it('submits valid changes, updates cache immediately, and makes exactly 1 PUT with 0 duplicate settings GET calls', async () => {
      const updatedOrg: OrganizationDetail = {
        ...sampleOrg,
        name: 'Acme Worldwide Corp',
      };

      vi.spyOn(apiClient, 'get').mockResolvedValueOnce(sampleOrg);
      const putSpy = vi.spyOn(apiClient, 'put').mockResolvedValueOnce(updatedOrg);

      render(<OrganizationSettingsPage />, {
        wrapper: createWrapper(queryClient),
      });

      await waitFor(() => {
        expect(screen.getByText('Acme Corporation')).toBeInTheDocument();
      });

      // Clear the initial GET mock history
      const getSpy = vi.spyOn(apiClient, 'get');

      fireEvent.click(screen.getByRole('button', { name: /edit organization/i }));

      const input = await screen.findByLabelText(/organization name/i);
      fireEvent.change(input, { target: { value: 'Acme Worldwide Corp' } });

      fireEvent.click(screen.getByRole('button', { name: /save changes/i }));

      await waitFor(() => {
        expect(putSpy).toHaveBeenCalledTimes(1);
        expect(putSpy).toHaveBeenCalledWith(
          '/organizations/org-1',
          {
            name: 'Acme Worldwide Corp',
            metadata: { env: 'production' },
          },
          { tenantOrgId: 'org-1' }
        );
        expect(screen.queryByLabelText(/organization name/i)).not.toBeInTheDocument();
      });

      // Verify 0 additional GET /organizations/org-1 calls after PUT
      const subsequentGetCalls = getSpy.mock.calls.filter(([url]) =>
        (url as string).includes('/organizations/org-1')
      );
      expect(subsequentGetCalls.length).toBe(0);

      // Verify authoritative TanStack query cache update
      const cached = queryClient.getQueryData<OrganizationDetail>(
        queryKeys.org('org-1').settings()
      );
      expect(cached).toEqual(updatedOrg);

      // Verify other tenant org cache is untouched
      const otherCached = queryClient.getQueryData(
        queryKeys.org('org-2').settings()
      );
      expect(otherCached).toBeUndefined();
    });

    it('resets and closes safely on tenant switch without confirmation prompt', async () => {
      vi.spyOn(apiClient, 'get').mockResolvedValueOnce(sampleOrg);
      const confirmSpy = vi.spyOn(window, 'confirm');

      let authState = {
        status: 'authenticated' as const,
        user: { id: 'u1', email: 'admin@domain.com', full_name: 'Admin', is_system_admin: false },
        memberships: [],
        activeOrgId: 'org-1',
        activeMembership: {
          organization_id: 'org-1',
          organization_name: 'Acme Corporation',
          organization_slug: 'acme-corp',
          is_default_internal: true,
          role: 'admin' as const,
          status: 'active' as const,
          permissions: ['org:read', 'org:write', 'user:read'],
        },
        isSystemAdmin: false,
        userRole: 'admin' as const,
        login: vi.fn(),
        logout: vi.fn(),
        switchOrganization: vi.fn(),
      };

      vi.spyOn(AuthContextModule, 'useAuth').mockImplementation(() => authState);

      const { rerender } = render(<OrganizationSettingsPage />, {
        wrapper: createWrapper(queryClient),
      });

      await waitFor(() => {
        expect(screen.getByText('Acme Corporation')).toBeInTheDocument();
      });

      fireEvent.click(screen.getByRole('button', { name: /edit organization/i }));

      const input = await screen.findByLabelText(/organization name/i);
      fireEvent.change(input, { target: { value: 'Dirty Organization Name' } });

      // Switch active tenant
      authState = {
        ...authState,
        activeOrgId: 'org-2',
        activeMembership: {
          ...authState.activeMembership,
          organization_id: 'org-2',
        },
      };

      rerender(<OrganizationSettingsPage />);

      await waitFor(() => {
        expect(screen.queryByLabelText(/organization name/i)).not.toBeInTheDocument();
        expect(confirmSpy).not.toHaveBeenCalled();
      });
    });
  });
});
