import { render, screen } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { ToastProvider } from '@/lib/toast/toast-context';
import ResourceDetailPage from '@/app/(dashboard)/resources/[id]/page';
import * as AuthContextModule from '@/lib/auth/auth-context';
import { apiClient } from '@/lib/api/api-client';

// Mock next/navigation
vi.mock('next/navigation', () => ({
  useParams: () => ({ id: 'res-1' }),
  useRouter: () => ({ push: vi.fn() }),
  usePathname: () => '/resources/res-1',
}));

describe('Resource Eligibility UI Controls', () => {
  let queryClient: QueryClient;

  beforeEach(() => {
    vi.clearAllMocks();
    queryClient = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
      },
    });
  });

  const renderWithProviders = (ui: React.ReactElement) => {
    return render(
      <QueryClientProvider client={queryClient}>
        <ToastProvider>{ui}</ToastProvider>
      </QueryClientProvider>
    );
  };


  it('renders Run Backup button for active ubuntu_ssh resource when user has execute permission', async () => {
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
        permissions: ['resource:read', 'resource:write', 'backup_job:execute'],
      },
      isSystemAdmin: false,
      userRole: 'admin',
      login: vi.fn(),
      logout: vi.fn(),
      switchOrganization: vi.fn(),
    });

    vi.spyOn(apiClient, 'get').mockImplementation((url) => {
      if (url === '/resources/res-1') {
        return Promise.resolve({
          id: 'res-1',
          name: 'Ubuntu Web Server',
          type: 'ubuntu_ssh',
          status: 'active',
          connector: {
            host: '10.0.0.5',
            port: 22,
            auth_type: 'ssh_key',
            username: 'ubuntu',
            credential_id: 'cred-1',
          },
          created_at: '2026-01-01T00:00:00Z',
        });
      }
      return Promise.resolve([]);
    });

    renderWithProviders(<ResourceDetailPage />);

    // Should display Run Backup button
    const runBackupBtn = await screen.findByRole('button', { name: /run backup/i });
    expect(runBackupBtn).toBeInTheDocument();

    // Should display Discover Databases button
    const discoverBtn = screen.getByRole('button', { name: /discover databases/i });
    expect(discoverBtn).toBeInTheDocument();
  });

  it('does NOT render Run Backup or Discover Databases for cPanel resources', async () => {
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
        permissions: ['resource:read', 'resource:write', 'backup_job:execute'],
      },
      isSystemAdmin: false,
      userRole: 'admin',
      login: vi.fn(),
      logout: vi.fn(),
      switchOrganization: vi.fn(),
    });

    vi.spyOn(apiClient, 'get').mockImplementation((url) => {
      if (url === '/resources/res-1') {
        return Promise.resolve({
          id: 'res-1',
          name: 'cPanel Host',
          type: 'cpanel',
          status: 'active',
          connector: {
            host: 'cpanel.domain.com',
            port: 2083,
            auth_type: 'cpanel_token',
            username: 'cpaneluser',
            credential_id: 'cred-2',
          },
          created_at: '2026-01-01T00:00:00Z',
        });
      }
      return Promise.resolve([]);
    });

    renderWithProviders(<ResourceDetailPage />);

    // Wait for resource details to load
    await screen.findByText('cPanel Host');

    // Run Backup button must NOT be present
    expect(screen.queryByRole('button', { name: /run backup/i })).toBeNull();
    // Discover Databases button must NOT be present
    expect(screen.queryByRole('button', { name: /discover databases/i })).toBeNull();
  });

  it('does NOT render Run Backup button for viewer role on ubuntu_ssh', async () => {
    vi.spyOn(AuthContextModule, 'useAuth').mockReturnValue({
      status: 'authenticated',
      user: { id: 'u2', email: 'viewer@domain.com', full_name: 'Viewer', is_system_admin: false },
      memberships: [],
      activeOrgId: 'org-1',
      activeMembership: {
        organization_id: 'org-1',
        organization_name: 'Org 1',
        organization_slug: 'org-1',
        is_default_internal: true,
        role: 'viewer',
        status: 'active',
        permissions: ['resource:read'],
      },
      isSystemAdmin: false,

      userRole: 'viewer',
      login: vi.fn(),
      logout: vi.fn(),
      switchOrganization: vi.fn(),
    });

    vi.spyOn(apiClient, 'get').mockImplementation((url) => {
      if (url === '/resources/res-1') {
        return Promise.resolve({
          id: 'res-1',
          name: 'Ubuntu Web Server',
          type: 'ubuntu_ssh',
          status: 'active',
          connector: {
            host: '10.0.0.5',
            port: 22,
            auth_type: 'ssh_key',
            username: 'ubuntu',
            credential_id: 'cred-1',
          },
          created_at: '2026-01-01T00:00:00Z',
        });
      }
      return Promise.resolve([]);
    });

    renderWithProviders(<ResourceDetailPage />);

    await screen.findByText('Ubuntu Web Server');

    // Viewer must NOT see Run Backup, Test Connection, or Discover Databases
    expect(screen.queryByRole('button', { name: /run backup/i })).toBeNull();
    expect(screen.queryByRole('button', { name: /test connection/i })).toBeNull();
    expect(screen.queryByRole('button', { name: /discover databases/i })).toBeNull();
  });
});
