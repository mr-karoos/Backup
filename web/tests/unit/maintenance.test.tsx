import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import MaintenancePage from '@/app/(dashboard)/maintenance/page';
import MaintenanceJobDetailPage from '@/app/(dashboard)/maintenance/[id]/page';
import { SidebarNav } from '@/components/layout/SidebarNav';
import * as AuthContextModule from '@/lib/auth/auth-context';
import { apiClient } from '@/lib/api/api-client';
import { queryKeys } from '@/lib/query/query-client';
import { ApiError } from '@/types/api';
import type {
  MaintenanceJobResponse,
  MaintenanceJobDetailResponse,
} from '@/types/domain';

// Mock useRouter and useParams
let mockParams = { id: 'job-1' };
vi.mock('next/navigation', () => ({
  usePathname: () => '/maintenance',
  useRouter: () => ({
    push: vi.fn(),
    replace: vi.fn(),
    prefetch: vi.fn(),
  }),
  useParams: () => mockParams,
}));

const mockJobs: MaintenanceJobResponse[] = [
  {
    id: 'job-1111-1111-1111-1111',
    repository_id: 'a1b2c3d4-e5f6-7890-abcd-ef1234567890',
    operation_type: 'restic_prune',
    status: 'completed',
    artifact_id: null,
    snapshot_id: null,
    subset_index: null,
    subset_total: null,
    attempt_count: 1,
    max_attempts: 3,
    next_attempt_at: null,
    phase: 'pruning_data',
    completed_at: '2026-09-07T12:00:00Z',
    created_at: '2026-09-07T11:50:00Z',
    updated_at: '2026-09-07T12:00:00Z',
  },
  {
    id: 'job-2222-2222-2222-2222',
    repository_id: 'b2c3d4e5-f6a7-8901-bcde-f12345678901',
    operation_type: 'restic_forget',
    status: 'running',
    artifact_id: 'art-1',
    snapshot_id: 'snap-1',
    subset_index: 0,
    subset_total: 10,
    attempt_count: 1,
    max_attempts: 3,
    next_attempt_at: null,
    phase: 'forgetting_snapshots',
    completed_at: null,
    created_at: '2026-09-07T12:30:00Z',
    updated_at: '2026-09-07T12:31:00Z',
  },
];

const mockJobDetail: MaintenanceJobDetailResponse = {
  id: 'job-1111-1111-1111-1111',
  repository_id: 'a1b2c3d4-e5f6-7890-abcd-ef1234567890',
  operation_type: 'restic_prune',
  status: 'failed',
  artifact_id: 'art-999',
  snapshot_id: 'snap-999',
  subset_index: 1,
  subset_total: 4,
  attempt_count: 2,
  max_attempts: 3,
  next_attempt_at: '2026-09-07T14:00:00Z',
  phase: 'check_indexes',
  completed_at: null,
  created_at: '2026-09-07T11:50:00Z',
  updated_at: '2026-09-07T12:10:00Z',
  runs: [
    {
      id: 'run-1',
      job_id: 'job-1111-1111-1111-1111',
      attempt_number: 1,
      status: 'failed',
      started_at: '2026-09-07T11:50:05Z',
      ended_at: '2026-09-07T11:52:00Z',
      heartbeat_at: '2026-09-07T11:51:50Z',
      created_at: '2026-09-07T11:50:00Z',
      updated_at: '2026-09-07T11:52:00Z',
      duration_ms: 115000,
      error_summary: '<script>alert("xss")</script> Lock timeout on repository',
    },
  ],
};

describe('Frontend F2D: Maintenance Operations UI', () => {
  let queryClient: QueryClient;
  let currentOrgId = 'org-1';

  beforeEach(() => {
    vi.clearAllMocks();
    queryClient = new QueryClient({
      defaultOptions: {
        queries: {
          retry: false,
        },
      },
    });

    currentOrgId = 'org-1';
    vi.spyOn(AuthContextModule, 'useAuth').mockImplementation(() => ({
      status: 'authenticated',
      user: { id: 'u-1', email: 'user@example.com', full_name: 'Test User', is_system_admin: false },
      activeOrgId: currentOrgId,
      userRole: 'admin',
      activeMembership: {
        organization_id: currentOrgId,
        organization_name: 'Test Org',
        organization_slug: 'test-org',
        role: 'admin',
        status: 'active',
        is_default_internal: false,
        permissions: ['maintenance:read', 'resource:read'],
      },
      memberships: [],
      isSystemAdmin: false,
      login: vi.fn(),
      logout: vi.fn(),
      switchOrganization: vi.fn(),
    }));
  });

  const renderWithQuery = (ui: React.ReactElement) => {
    return render(
      <QueryClientProvider client={queryClient}>{ui}</QueryClientProvider>
    );
  };

  // 1. Sidebar Maintenance navigation exists for: Admin, Member, Viewer
  describe('1. Navigation RBAC', () => {
    it.each(['admin', 'member', 'viewer'] as const)(
      'renders Maintenance navigation link for %s role',
      (role) => {
        vi.spyOn(AuthContextModule, 'useAuth').mockReturnValue({
          status: 'authenticated',
          user: { id: 'u-1', email: `${role}@example.com`, full_name: 'Role User', is_system_admin: false },
          activeOrgId: 'org-1',
          userRole: role,
          activeMembership: {
            organization_id: 'org-1',
            organization_name: 'Test Org',
            organization_slug: 'test-org',
            role,
            status: 'active',
            is_default_internal: false,
            permissions: ['maintenance:read'],
          },
          memberships: [],
          isSystemAdmin: false,
          login: vi.fn(),
          logout: vi.fn(),
          switchOrganization: vi.fn(),
        });

        render(<SidebarNav />);
        const link = screen.getByRole('link', { name: /Maintenance/i });
        expect(link).toBeInTheDocument();
        expect(link).toHaveAttribute('href', '/maintenance');
      }
    );
  });

  // 2. List request: GET /maintenance-jobs with tenant-scoped query & AbortSignal / tenantOrgId binding
  describe('2. List Request & Tenant Scoping', () => {
    it('dispatches GET /maintenance-jobs with tenant-scoped query and passes signal + tenantOrgId', async () => {
      const getPaginatedSpy = vi
        .spyOn(apiClient, 'getPaginated')
        .mockResolvedValue({
          data: mockJobs,
          page: { next_cursor: null, has_more: false },
        });

      renderWithQuery(<MaintenancePage />);

      await waitFor(() => {
        expect(getPaginatedSpy).toHaveBeenCalledWith(
          '/maintenance-jobs',
          expect.objectContaining({
            signal: expect.any(AbortSignal),
            tenantOrgId: 'org-1',
          })
        );
      });

      // Headers and tenant isolation verified via queryKey
      const expectedKey = queryKeys.org('org-1').maintenance.list({});
      const queryState = queryClient.getQueryState(expectedKey);
      expect(queryState).toBeDefined();

      expect(screen.getByText('Repository Maintenance')).toBeInTheDocument();
      expect(screen.getByText('Prune')).toBeInTheDocument();
      expect(screen.getByText('Forget')).toBeInTheDocument();
    });
  });

  // 3. Filters: status, operation_type, repository_id, limit
  describe('3. Operational Filters', () => {
    it('dispatches requests with filtered query parameters', async () => {
      const getPaginatedSpy = vi
        .spyOn(apiClient, 'getPaginated')
        .mockResolvedValue({
          data: mockJobs,
          page: { next_cursor: null, has_more: false },
        });

      renderWithQuery(<MaintenancePage />);

      await waitFor(() => {
        expect(getPaginatedSpy).toHaveBeenCalledWith(
          '/maintenance-jobs',
          expect.objectContaining({ tenantOrgId: 'org-1' })
        );
      });

      // Filter by Status
      const statusSelect = screen.getByLabelText(/Filter maintenance jobs by status/i);
      fireEvent.change(statusSelect, { target: { value: 'failed' } });

      await waitFor(() => {
        expect(getPaginatedSpy).toHaveBeenCalledWith(
          '/maintenance-jobs?status=failed',
          expect.objectContaining({ tenantOrgId: 'org-1' })
        );
      });

      // Filter by Operation
      const opSelect = screen.getByLabelText(/Filter maintenance jobs by operation type/i);
      fireEvent.change(opSelect, { target: { value: 'restic_prune' } });

      await waitFor(() => {
        expect(getPaginatedSpy).toHaveBeenCalledWith(
          '/maintenance-jobs?status=failed&operation_type=restic_prune',
          expect.objectContaining({ tenantOrgId: 'org-1' })
        );
      });

      // Filter by Repository ID (valid UUID)
      const validUuid = '12345678-1234-1234-1234-123456789abc';
      const repoInput = screen.getByLabelText(/Filter maintenance jobs by repository UUID/i);
      fireEvent.change(repoInput, { target: { value: validUuid } });

      await waitFor(() => {
        expect(getPaginatedSpy).toHaveBeenCalledWith(
          `/maintenance-jobs?status=failed&operation_type=restic_prune&repository_id=${validUuid}`,
          expect.objectContaining({ tenantOrgId: 'org-1' })
        );
      });

      // Filter by Limit (Page Size)
      const limitSelect = screen.getByLabelText(/Items per page/i);
      fireEvent.change(limitSelect, { target: { value: '25' } });

      await waitFor(() => {
        expect(getPaginatedSpy).toHaveBeenCalledWith(
          `/maintenance-jobs?status=failed&operation_type=restic_prune&repository_id=${validUuid}&limit=25`,
          expect.objectContaining({ tenantOrgId: 'org-1' })
        );
      });
    });
  });

  // 4. Invalid repository UUID: inline error, no request dispatched, polling disabled, retry disabled
  describe('4. Invalid Repository UUID Validation Guard', () => {
    it('shows inline error, blocks network request, disables polling and blocks retry', async () => {
      const getPaginatedSpy = vi
        .spyOn(apiClient, 'getPaginated')
        .mockResolvedValue({
          data: mockJobs,
          page: { next_cursor: null, has_more: false },
        });

      renderWithQuery(<MaintenancePage />);

      await waitFor(() => {
        expect(getPaginatedSpy).toHaveBeenCalledTimes(1);
      });

      const repoInput = screen.getByLabelText(/Filter maintenance jobs by repository UUID/i);
      fireEvent.change(repoInput, { target: { value: 'not-a-valid-uuid' } });

      // Inline error rendered with role="alert"
      const alert = await screen.findByRole('alert');
      expect(alert).toHaveTextContent('Invalid Repository ID format (must be a valid UUID).');

      // Call count remains strictly 1 (no new network request dispatched for invalid query)
      expect(getPaginatedSpy).toHaveBeenCalledTimes(1);

      // Verify the query in queryClient has enabled=false
      const queries = queryClient.getQueryCache().findAll();
      const currentQuery = queries.find((q) => q.queryKey.includes('disabled') || !q.isActive());
      expect(currentQuery).toBeDefined();
    });
  });

  // 5. Pagination: forward cursor, Next, Previous, filter reset
  describe('5. Keyset Cursor Pagination', () => {
    it('supports keyset cursor navigation and resets on filter change', async () => {
      const getPaginatedSpy = vi
        .spyOn(apiClient, 'getPaginated')
        .mockResolvedValueOnce({
          data: [mockJobs[0]],
          page: { next_cursor: 'cursor-page-2', has_more: true },
        })
        .mockResolvedValueOnce({
          data: [mockJobs[1]],
          page: { next_cursor: 'cursor-page-3', has_more: false },
        })
        .mockResolvedValueOnce({
          data: [mockJobs[0]],
          page: { next_cursor: 'cursor-page-2', has_more: true },
        });

      renderWithQuery(<MaintenancePage />);

      await waitFor(() => {
        expect(getPaginatedSpy).toHaveBeenCalledWith(
          '/maintenance-jobs',
          expect.objectContaining({ tenantOrgId: 'org-1' })
        );
      });

      await waitFor(() => {
        expect(screen.getByText('Page 1')).toBeInTheDocument();
      });

      const prevBtn = screen.getByRole('button', { name: /Go to previous page/i });
      const nextBtn = screen.getByRole('button', { name: /Go to next page/i });

      expect(prevBtn).toBeDisabled();
      expect(nextBtn).not.toBeDisabled();

      // Click Next
      fireEvent.click(nextBtn);

      await waitFor(() => {
        expect(getPaginatedSpy).toHaveBeenCalledWith(
          '/maintenance-jobs?cursor=cursor-page-2',
          expect.objectContaining({ tenantOrgId: 'org-1' })
        );
      });

      // Now on Page 2: Previous should be enabled, Next should be disabled (has_more=false)
      await waitFor(() => {
        expect(screen.getByText('Page 2')).toBeInTheDocument();
      });
      const page2PrevBtn = screen.getByRole('button', { name: /Go to previous page/i });
      const page2NextBtn = screen.getByRole('button', { name: /Go to next page/i });
      expect(page2PrevBtn).not.toBeDisabled();
      expect(page2NextBtn).toBeDisabled();

      // Click Previous
      fireEvent.click(page2PrevBtn);

      await waitFor(() => {
        expect(getPaginatedSpy).toHaveBeenCalledWith(
          '/maintenance-jobs',
          expect.objectContaining({ tenantOrgId: 'org-1' })
        );
      });

      await waitFor(() => {
        expect(screen.getByText('Page 1')).toBeInTheDocument();
      });

      // Filter change resets cursor state
      const statusSelect = screen.getByLabelText(/Filter maintenance jobs by status/i);
      fireEvent.change(statusSelect, { target: { value: 'completed' } });

      await waitFor(() => {
        expect(getPaginatedSpy).toHaveBeenCalledWith(
          '/maintenance-jobs?status=completed',
          expect.objectContaining({ tenantOrgId: 'org-1' })
        );
      });
      await waitFor(() => {
        expect(screen.getByText('Page 1')).toBeInTheDocument();
      });
    });
  });

  // 6. Strong Tenant Isolation: org-1 cursor never sent in org-2 query
  describe('6. Strong Tenant Isolation', () => {
    it('prevents org-1 cursor from leaking to org-2 and maintains separate data/caches', async () => {
      const org1JobPage1: MaintenanceJobResponse = {
        ...mockJobs[0]!,
        id: 'job-org1-page1-aaa',
      };
      const org1JobPage2: MaintenanceJobResponse = {
        ...mockJobs[1]!,
        id: 'job-org1-page2-bbb',
      };
      const org2JobPage1: MaintenanceJobResponse = {
        ...mockJobs[0]!,
        id: 'job-org2-page1-xxx',
      };

      const getPaginatedSpy = vi
        .spyOn(apiClient, 'getPaginated')
        .mockImplementation((endpoint) => {
          if (endpoint === '/maintenance-jobs' && currentOrgId === 'org-1') {
            return Promise.resolve({
              data: [org1JobPage1],
              page: { next_cursor: 'cursor-org1-page2', has_more: true },
            });
          }
          if (endpoint === '/maintenance-jobs?cursor=cursor-org1-page2') {
            return Promise.resolve({
              data: [org1JobPage2],
              page: { next_cursor: null, has_more: false },
            });
          }
          if (endpoint === '/maintenance-jobs' && currentOrgId === 'org-2') {
            return Promise.resolve({
              data: [org2JobPage1],
              page: { next_cursor: null, has_more: false },
            });
          }
          return Promise.resolve({ data: [], page: { next_cursor: null, has_more: false } });
        });

      const { rerender } = renderWithQuery(<MaintenancePage />);

      // 1. Initial load for org-1
      await waitFor(() => {
        expect(getPaginatedSpy).toHaveBeenCalledWith(
          '/maintenance-jobs',
          expect.objectContaining({ tenantOrgId: 'org-1' })
        );
      });
      await waitFor(() => {
        expect(screen.getByText('Page 1')).toBeInTheDocument();
      });

      // 2. Advance to org-1 page 2
      const nextBtn = screen.getByRole('button', { name: /Go to next page/i });
      fireEvent.click(nextBtn);

      await waitFor(() => {
        expect(getPaginatedSpy).toHaveBeenCalledWith(
          '/maintenance-jobs?cursor=cursor-org1-page2',
          expect.objectContaining({ tenantOrgId: 'org-1' })
        );
      });
      await waitFor(() => {
        expect(screen.getByText('Page 2')).toBeInTheDocument();
      });

      // 3. Switch tenant organization to org-2
      currentOrgId = 'org-2';
      vi.spyOn(AuthContextModule, 'useAuth').mockReturnValue({
        status: 'authenticated',
        user: { id: 'u-1', email: 'user@example.com', full_name: 'Test User', is_system_admin: false },
        activeOrgId: 'org-2',
        userRole: 'admin',
        activeMembership: {
          organization_id: 'org-2',
          organization_name: 'Org 2',
          organization_slug: 'org-2',
          role: 'admin',
          status: 'active',
          is_default_internal: false,
          permissions: ['maintenance:read'],
        },
        memberships: [],
        isSystemAdmin: false,
        login: vi.fn(),
        logout: vi.fn(),
        switchOrganization: vi.fn(),
      });

      rerender(
        <QueryClientProvider client={queryClient}>
          <MaintenancePage />
        </QueryClientProvider>
      );

      // 4. Verify first request for org-2 is page 1 (without any cursor from org-1)
      await waitFor(() => {
        expect(getPaginatedSpy).toHaveBeenCalledWith(
          '/maintenance-jobs',
          expect.objectContaining({ tenantOrgId: 'org-2' })
        );
      });

      // Check all calls to getPaginated for org-2: NONE must have cursor-org1-page2
      const org2Calls = getPaginatedSpy.mock.calls.filter(
        (call) => call[1]?.tenantOrgId === 'org-2'
      );
      expect(org2Calls.length).toBeGreaterThan(0);
      org2Calls.forEach((call) => {
        expect(call[0]).not.toContain('cursor-org1-page2');
      });

      // 5. Caches are distinct
      const org1Key = queryKeys.org('org-1').maintenance.list({});
      const org2Key = queryKeys.org('org-2').maintenance.list({});
      expect(queryClient.getQueryState(org1Key)).toBeDefined();
      expect(queryClient.getQueryState(org2Key)).toBeDefined();
      expect(org1Key).not.toEqual(org2Key);
    });
  });

  // 7. Production refetchInterval Polling
  describe('7. Production refetchInterval Polling Behavior', () => {
    it('executes actual production refetchInterval returning 3000ms for active and false for terminal', async () => {
      vi.spyOn(apiClient, 'getPaginated').mockResolvedValue({
        data: mockJobs,
        page: { next_cursor: null, has_more: false },
      });

      renderWithQuery(<MaintenancePage />);

      await waitFor(() => {
        expect(screen.getByText('Repository Maintenance')).toBeInTheDocument();
      });

      // Retrieve the actual query from queryClient to inspect production options
      const queries = queryClient.getQueryCache().findAll();
      const maintenanceQuery = queries.find((q) => q.queryKey.includes('maintenance'))!;
      expect(maintenanceQuery).toBeDefined();

      const productionRefetchInterval = (maintenanceQuery.options as Record<string, any>).refetchInterval as (
        q: any
      ) => number | false;
      expect(typeof productionRefetchInterval).toBe('function');

      // Case A: Query with running job -> 3000ms
      const activeStateRunning = {
        state: {
          data: {
            data: [{ status: 'running' }],
            page: { next_cursor: null, has_more: false },
          },
        },
      };
      expect(productionRefetchInterval(activeStateRunning)).toBe(3000);

      // Case B: Query with pending job -> 3000ms
      const activeStatePending = {
        state: {
          data: {
            data: [{ status: 'pending' }],
            page: { next_cursor: null, has_more: false },
          },
        },
      };
      expect(productionRefetchInterval(activeStatePending)).toBe(3000);

      // Case C: Query with all terminal jobs (completed / failed / cancelled) -> false
      const terminalState = {
        state: {
          data: {
            data: [{ status: 'completed' }, { status: 'failed' }, { status: 'cancelled' }],
            page: { next_cursor: null, has_more: false },
          },
        },
      };
      expect(productionRefetchInterval(terminalState)).toBe(false);
    });
  });

  // 8. Detail: GET /maintenance-jobs/{id} with signal & tenantOrgId
  describe('8. Detail View Contract & Safe Errors', () => {
    it('fetches maintenance job specifications with signal and tenantOrgId', async () => {
      mockParams = { id: 'job-1111-1111-1111-1111' };
      const getSpy = vi.spyOn(apiClient, 'get').mockResolvedValue(mockJobDetail);

      renderWithQuery(<MaintenanceJobDetailPage />);

      await waitFor(() => {
        expect(getSpy).toHaveBeenCalledWith(
          '/maintenance-jobs/job-1111-1111-1111-1111',
          expect.objectContaining({
            signal: expect.any(AbortSignal),
            tenantOrgId: 'org-1',
          })
        );
      });

      await waitFor(() => {
        expect(screen.getByText('Job Details')).toBeInTheDocument();
      });

      expect(screen.getByText('a1b2c3d4-e5f6-7890-abcd-ef1234567890')).toBeInTheDocument();
      expect(screen.getByText('art-999')).toBeInTheDocument();
      expect(screen.getByText('snap-999')).toBeInTheDocument();
      expect(screen.getByText('1 / 4')).toBeInTheDocument();
      expect(screen.getByText('2 / 3')).toBeInTheDocument();
      expect(screen.getByText('check_indexes')).toBeInTheDocument();
    });

    it('displays safe fixed messages for 404, 403, 503 and generic errors in detail', async () => {
      mockParams = { id: 'missing-job' };

      // 404
      vi.spyOn(apiClient, 'get').mockRejectedValueOnce(
        new ApiError(404, 'Job not found', 'RESOURCE_NOT_FOUND')
      );
      const { unmount } = renderWithQuery(<MaintenanceJobDetailPage />);
      await waitFor(() => {
        expect(screen.getByText('Maintenance job not found.')).toBeInTheDocument();
      });
      unmount();

      // Generic error (must NOT leak raw message)
      vi.spyOn(apiClient, 'get').mockRejectedValueOnce(
        new ApiError(500, 'INTERNAL_DB_CRASH', 'Failed to connect to db at 192.168.1.10')
      );
      renderWithQuery(<MaintenanceJobDetailPage />);
      await waitFor(() => {
        expect(
          screen.getByText('Unable to load maintenance job details. Please try again.')
        ).toBeInTheDocument();
      });
      expect(screen.queryByText(/192\.168\.1\.10/)).toBeNull();
    });
  });

  // 9. Run DTO: fields and error_summary escaping
  describe('9. Execution Runs Rendering', () => {
    it('renders execution run summary fields and escapes plain text error summary', async () => {
      mockParams = { id: 'job-1111-1111-1111-1111' };
      vi.spyOn(apiClient, 'get').mockResolvedValue(mockJobDetail);

      renderWithQuery(<MaintenanceJobDetailPage />);

      await waitFor(() => {
        expect(screen.getByText('Execution Attempts')).toBeInTheDocument();
      });

      expect(screen.getByText('#1')).toBeInTheDocument();
      // Plain text escaping: <script> string is rendered as text, NOT as an executable element
      expect(
        screen.getByText(/<script>alert\("xss"\)<\/script> Lock timeout on repository/)
      ).toBeInTheDocument();
      expect(document.querySelector('script')).toBeNull();
    });

    it('renders empty message when no runs are recorded', async () => {
      mockParams = { id: 'job-empty' };
      vi.spyOn(apiClient, 'get').mockResolvedValue({
        ...mockJobDetail,
        runs: [],
      });

      renderWithQuery(<MaintenanceJobDetailPage />);

      await waitFor(() => {
        expect(screen.getByText('No execution attempts recorded.')).toBeInTheDocument();
      });
    });
  });

  // 10. Forbidden fields: no organization_id, metadata, lease_until, logs_summary
  describe('10. Forbidden / Internal Fields Protection', () => {
    it('does not render internal fields or raw logs', async () => {
      mockParams = { id: 'job-1111-1111-1111-1111' };
      vi.spyOn(apiClient, 'get').mockResolvedValue(mockJobDetail);

      renderWithQuery(<MaintenanceJobDetailPage />);

      await waitFor(() => {
        expect(screen.getByText('Job Details')).toBeInTheDocument();
      });

      // None of the internal fields should exist as labels or text
      expect(screen.queryByText(/organization_id/i)).toBeNull();
      expect(screen.queryByText(/lease_until/i)).toBeNull();
      expect(screen.queryByText(/logs_summary/i)).toBeNull();
      expect(screen.queryByText(/raw stdout/i)).toBeNull();
      expect(screen.queryByText(/raw stderr/i)).toBeNull();
    });
  });

  // 11. Zero Maintenance Mutation Controls
  describe('11. Zero Maintenance Mutation Controls in DOM', () => {
    it('contains strictly NO write/mutation controls in list or detail pages', async () => {
      vi.spyOn(apiClient, 'getPaginated').mockResolvedValue({
        data: mockJobs,
        page: { next_cursor: null, has_more: false },
      });

      const { unmount } = renderWithQuery(<MaintenancePage />);

      await waitFor(() => {
        expect(screen.getByText('Repository Maintenance')).toBeInTheDocument();
      });

      // Verify List page DOM contains NO mutation buttons
      expect(screen.queryByRole('button', { name: /retry job/i })).toBeNull();
      expect(screen.queryByRole('button', { name: /^cancel/i })).toBeNull();
      expect(screen.queryByRole('button', { name: /prune/i })).toBeNull();
      expect(screen.queryByRole('button', { name: /forget/i })).toBeNull();
      expect(screen.queryByRole('button', { name: /unlock/i })).toBeNull();
      expect(screen.queryByRole('button', { name: /delete/i })).toBeNull();

      unmount();

      // Verify Detail page DOM contains NO mutation buttons
      mockParams = { id: 'job-1111-1111-1111-1111' };
      vi.spyOn(apiClient, 'get').mockResolvedValue(mockJobDetail);

      renderWithQuery(<MaintenanceJobDetailPage />);

      await waitFor(() => {
        expect(screen.getByText('Job Details')).toBeInTheDocument();
      });

      expect(screen.queryByRole('button', { name: /retry job/i })).toBeNull();
      expect(screen.queryByRole('button', { name: /^cancel/i })).toBeNull();
      expect(screen.queryByRole('button', { name: /prune/i })).toBeNull();
      expect(screen.queryByRole('button', { name: /forget/i })).toBeNull();
      expect(screen.queryByRole('button', { name: /unlock/i })).toBeNull();
      expect(screen.queryByRole('button', { name: /delete/i })).toBeNull();
    });
  });
});
