import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { ToastProvider } from '@/lib/toast/toast-context';
import { ManualBackupDialog } from '@/components/backup/manual-backup-dialog';
import * as AuthContextModule from '@/lib/auth/auth-context';
import { apiClient } from '@/lib/api/api-client';

vi.mock('next/navigation', () => ({
  useRouter: () => ({ push: vi.fn() }),
}));

describe('ManualBackupDialog - Storage Target Auto-Provisioning Fallback', () => {
  let queryClient: QueryClient;

  beforeEach(() => {
    vi.clearAllMocks();
    queryClient = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
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
        permissions: ['backup_job:execute', 'backup_plan:execute'],
      },
      isSystemAdmin: false,
      userRole: 'admin',
      login: vi.fn(),
      logout: vi.fn(),
      switchOrganization: vi.fn(),
    });
  });

  const renderDialog = (props: {
    open: boolean;
    onOpenChange: (open: boolean) => void;
    resource?: { id: string; name: string; type?: string };
    plan?: { id: string; name: string; resourceName: string; backupType: string };
  }) => {
    return render(
      <QueryClientProvider client={queryClient}>
        <ToastProvider>
          <ManualBackupDialog {...props} />
        </ToastProvider>
      </QueryClientProvider>
    );
  };

  it('allows submit with zero storage targets and omits storage_target_id in request payload', async () => {
    vi.spyOn(apiClient, 'get').mockResolvedValue([]);
    const postSpy = vi.spyOn(apiClient, 'post').mockResolvedValue({
      id: 'job-1',
      status: 'pending',
    });

    renderDialog({
      open: true,
      onOpenChange: vi.fn(),
      resource: { id: 'res-1', name: 'Web Server 1', type: 'ubuntu_ssh' },
    });

    await waitFor(() => {
      expect(
        screen.getByText('Platform Default Local Storage (auto-provisioned)')
      ).toBeInTheDocument();
    });

    const dbInput = screen.getByLabelText(/Database Names/i);
    fireEvent.change(dbInput, { target: { value: 'app_production' } });

    const submitBtn = screen.getByRole('button', { name: /Run Backup Now/i });
    fireEvent.click(submitBtn);

    await waitFor(() => {
      expect(postSpy).toHaveBeenCalledTimes(1);
    });

    const callArgs = postSpy.mock.calls[0]!;
    expect(callArgs[0]).toBe('/backup-jobs');
    const payload = callArgs[1] as Record<string, unknown>;

    expect(payload).toEqual({
      resource_id: 'res-1',
      backup_type: 'mysql_database',
      engine_type: 'direct_stream',
      target_spec: { databases: ['app_production'] },
    });
    expect(payload).not.toHaveProperty('storage_target_id');
  });

  it('submits with active storage target ID when active storage target is selected', async () => {
    vi.spyOn(apiClient, 'get').mockResolvedValue([
      {
        id: 'target-s3-1',
        name: 'Wasabi S3',
        type: 's3',
        status: 'active',
        is_default: true,
      },
      {
        id: 'target-local-1',
        name: 'Local Backup Dir',
        type: 'local',
        status: 'active',
        is_default: false,
      },
    ]);

    const postSpy = vi.spyOn(apiClient, 'post').mockResolvedValue({
      id: 'job-2',
      status: 'pending',
    });

    renderDialog({
      open: true,
      onOpenChange: vi.fn(),
      resource: { id: 'res-1', name: 'Web Server 1', type: 'ubuntu_ssh' },
    });

    await waitFor(() => {
      expect(screen.getByText('Wasabi S3 (s3) [Default]')).toBeInTheDocument();
    });

    const dbInput = screen.getByLabelText(/Database Names/i);
    fireEvent.change(dbInput, { target: { value: 'crm_db' } });

    const submitBtn = screen.getByRole('button', { name: /Run Backup Now/i });
    fireEvent.click(submitBtn);

    await waitFor(() => {
      expect(postSpy).toHaveBeenCalledTimes(1);
    });

    const callArgs = postSpy.mock.calls[0]!;
    expect(callArgs[0]).toBe('/backup-jobs');
    const payload = callArgs[1] as Record<string, unknown>;

    expect(payload).toEqual({
      resource_id: 'res-1',
      backup_type: 'mysql_database',
      engine_type: 'direct_stream',
      storage_target_id: 'target-s3-1',
      target_spec: { databases: ['crm_db'] },
    });
  });

  it('allows user to explicitly choose Platform Default Local Storage even when active targets exist', async () => {
    vi.spyOn(apiClient, 'get').mockResolvedValue([
      {
        id: 'target-s3-1',
        name: 'Wasabi S3',
        type: 's3',
        status: 'active',
        is_default: true,
      },
    ]);

    const postSpy = vi.spyOn(apiClient, 'post').mockResolvedValue({
      id: 'job-3',
      status: 'pending',
    });

    renderDialog({
      open: true,
      onOpenChange: vi.fn(),
      resource: { id: 'res-1', name: 'Web Server 1', type: 'ubuntu_ssh' },
    });

    await waitFor(() => {
      expect(screen.getByText('Wasabi S3 (s3) [Default]')).toBeInTheDocument();
    });

    // Select the auto-provisioned default local storage option
    const select = screen.getByLabelText(/Storage Target/i);
    fireEvent.change(select, { target: { value: '' } });

    const dbInput = screen.getByLabelText(/Database Names/i);
    fireEvent.change(dbInput, { target: { value: 'store_db' } });

    const submitBtn = screen.getByRole('button', { name: /Run Backup Now/i });
    fireEvent.click(submitBtn);

    await waitFor(() => {
      expect(postSpy).toHaveBeenCalledTimes(1);
    });

    const payload = postSpy.mock.calls[0]![1] as Record<string, unknown>;
    expect(payload).not.toHaveProperty('storage_target_id');
    expect(payload).toEqual({
      resource_id: 'res-1',
      backup_type: 'mysql_database',
      engine_type: 'direct_stream',
      target_spec: { databases: ['store_db'] },
    });
  });
});
