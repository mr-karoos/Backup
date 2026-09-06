import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import BackupRunsPage from '@/app/(dashboard)/runs/page';
import * as AuthContextModule from '@/lib/auth/auth-context';
import { apiClient } from '@/lib/api/api-client';
import { queryKeys } from '@/lib/query/query-client';
import type { BackupRunResponse } from '@/types/domain';

// Mock useRouter
const mockRouter = {
  push: vi.fn(),
  replace: vi.fn(),
  prefetch: vi.fn(),
};
vi.mock('next/navigation', () => ({
  useRouter: () => mockRouter,
  useParams: () => ({}),
}));

const mockRuns: BackupRunResponse[] = [
  {
    id: '11111111-1111-1111-1111-111111111111',
    job_id: 'job-1',
    resource_id: 'res-1',
    attempt_number: 1,
    status: 'success',
    started_at: '2026-09-02T10:00:00Z',
    ended_at: '2026-09-02T10:05:00Z',
    duration_seconds: 300,
    total_artifact_size_bytes: 1048576,
    error_message: null,
    artifacts_count: 2,
    created_at: '2026-09-02T10:00:00Z',
  },
  {
    id: '22222222-2222-2222-2222-222222222222',
    job_id: 'job-2',
    resource_id: 'res-1',
    attempt_number: 1,
    status: 'failed',
    started_at: '2026-09-03T12:00:00Z',
    ended_at: '2026-09-03T12:01:00Z',
    duration_seconds: 60,
    total_artifact_size_bytes: 0,
    error_message: 'SSH connection timeout',
    artifacts_count: 0,
    created_at: '2026-09-03T12:00:00Z',
  },
  {
    id: '33333333-3333-3333-3333-333333333333',
    job_id: 'job-3',
    resource_id: 'res-2',
    attempt_number: 1,
    status: 'running',
    started_at: '2026-09-04T15:00:00Z',
    ended_at: null,
    duration_seconds: null,
    total_artifact_size_bytes: 524288,
    error_message: null,
    artifacts_count: 1,
    created_at: '2026-09-04T15:00:00Z',
  },
];

describe('Frontend F2B: Runs Operational Filtering & Usability', () => {
  let queryClient: QueryClient;
  let currentOrgId = 'org-1';

  beforeEach(() => {
    vi.clearAllMocks();
    queryClient = new QueryClient({
      defaultOptions: {
        queries: {
          retry: false,
          gcTime: 0,
        },
      },
    });

    currentOrgId = 'org-1';
    vi.spyOn(AuthContextModule, 'useAuth').mockImplementation(() => ({
      status: 'authenticated',
      user: { id: 'u-1', email: 'admin@example.com', full_name: 'Admin User', is_system_admin: false },
      activeOrgId: currentOrgId,
      userRole: 'admin',
      activeMembership: {
        organization_id: currentOrgId,
        organization_name: 'Test Org',
        organization_slug: 'test-org',
        role: 'admin',
        status: 'active',
        is_default_internal: false,
        permissions: ['resource:read', 'backup_plan:read'],
      },
      memberships: [],
      isSystemAdmin: false,
      login: vi.fn(),
      logout: vi.fn(),
      switchOrganization: vi.fn(),
    }));
  });

  afterEach(() => {
    queryClient.clear();
  });

  function renderPage() {
    return render(
      <QueryClientProvider client={queryClient}>
        <BackupRunsPage />
      </QueryClientProvider>
    );
  }

  describe('A. Default Request (No Filters)', () => {
    it('fetches /backup-runs with zero query parameters when filters are empty', async () => {
      const getSpy = vi.spyOn(apiClient, 'get').mockResolvedValue(mockRuns);

      renderPage();

      await waitFor(() => {
        expect(getSpy).toHaveBeenCalledWith('/backup-runs');
      });

      // Confirm no empty query string exists in the called URL
      const firstCall = getSpy.mock.calls[0];
      expect(firstCall).toBeDefined();
      const calledUrl = firstCall![0];
      expect(calledUrl).not.toContain('?');
      expect(calledUrl).toBe('/backup-runs');
      expect(await screen.findByText('Execution Records')).toBeInTheDocument();
    });
  });

  describe('B. Status Filter', () => {
    it('sends exactly status=<value> when a status is selected', async () => {
      const getSpy = vi.spyOn(apiClient, 'get').mockResolvedValue([mockRuns[1]]);

      renderPage();

      await waitFor(() => {
        expect(getSpy).toHaveBeenCalledWith('/backup-runs');
      });

      const statusSelect = screen.getByLabelText('Filter runs by status');
      fireEvent.change(statusSelect, { target: { value: 'failed' } });

      await waitFor(() => {
        expect(getSpy).toHaveBeenCalledWith('/backup-runs?status=failed');
      });

      expect(getSpy).toHaveBeenLastCalledWith('/backup-runs?status=failed');
    });

    it('removes status parameter when set back to all', async () => {
      const getSpy = vi.spyOn(apiClient, 'get').mockResolvedValue(mockRuns);

      renderPage();

      const statusSelect = screen.getByLabelText('Filter runs by status');
      fireEvent.change(statusSelect, { target: { value: 'success' } });

      await waitFor(() => {
        expect(getSpy).toHaveBeenCalledWith('/backup-runs?status=success');
      });

      fireEvent.change(statusSelect, { target: { value: 'all' } });

      await waitFor(() => {
        expect(getSpy).toHaveBeenLastCalledWith('/backup-runs');
      });
    });
  });

  describe('C. Date Filters (RFC3339 Conversion)', () => {
    it('converts From and To dates to standard RFC3339 representations', async () => {
      const getSpy = vi.spyOn(apiClient, 'get').mockResolvedValue([mockRuns[0]]);

      renderPage();

      const fromInput = screen.getByLabelText('Filter runs from date');
      const toInput = screen.getByLabelText('Filter runs to date');

      fireEvent.change(fromInput, { target: { value: '2026-09-01' } });
      fireEvent.change(toInput, { target: { value: '2026-09-05' } });

      await waitFor(() => {
        expect(getSpy).toHaveBeenCalledWith(
          expect.stringContaining('/backup-runs?')
        );
      });

      const lastCall = getSpy.mock.calls[getSpy.mock.calls.length - 1];
      expect(lastCall).toBeDefined();
      expect(lastCall![0]).toContain('from_date=2026-09-01T00%3A00%3A00Z');
      expect(lastCall![0]).toContain('to_date=2026-09-05T23%3A59%3A59Z');
    });

    it('allows single date filtering (from_date only)', async () => {
      const getSpy = vi.spyOn(apiClient, 'get').mockResolvedValue(mockRuns);

      renderPage();

      const fromInput = screen.getByLabelText('Filter runs from date');
      fireEvent.change(fromInput, { target: { value: '2026-09-01' } });

      await waitFor(() => {
        expect(getSpy).toHaveBeenCalledWith(
          '/backup-runs?from_date=2026-09-01T00%3A00%3A00Z'
        );
      });
    });
  });

  describe('D. Invalid Date Range Validation (from > to)', () => {
    it('blocks request, prevents polling, and shows inline validation error when from date is after to date', async () => {
      const getSpy = vi.spyOn(apiClient, 'get').mockResolvedValue(mockRuns);

      renderPage();

      // 1 & 2. Allow initial request to settle
      await waitFor(() => {
        expect(getSpy).toHaveBeenCalledWith('/backup-runs');
      });
      expect(await screen.findByText('Execution Records')).toBeInTheDocument();

      // 3 & 4. Enter a valid From Date and allow that valid filtered request to settle
      const fromInput = screen.getByLabelText('Filter runs from date');
      const toInput = screen.getByLabelText('Filter runs to date');

      fireEvent.change(fromInput, { target: { value: '2026-09-01' } });
      await waitFor(() => {
        expect(getSpy).toHaveBeenCalledWith('/backup-runs?from_date=2026-09-01T00%3A00%3A00Z');
      });

      // 5. Record current apiClient.get call count
      const callCountBeforeInvalid = getSpy.mock.calls.length;

      // 6. Enter a To Date that makes from > to (e.g. from=2026-09-01, to=2026-08-20)
      fireEvent.change(toInput, { target: { value: '2026-08-20' } });

      // 7. Verify alert is displayed
      expect(await screen.findByRole('alert')).toHaveTextContent(
        'From date cannot be after To date.'
      );

      // 8. Verify apiClient.get call count does NOT increase at all
      expect(getSpy.mock.calls.length).toBe(callCountBeforeInvalid);

      // 9. Verify polling / request does not occur from the invalid query state
      const query = queryClient
        .getQueryCache()
        .findAll()
        .find((q) => q.queryKey[0] === 'org' && q.queryKey[1] === 'org-1');
      expect(query).toBeDefined();
      const observer = (query as any)?.observers?.[0];
      expect(observer?.options?.enabled).toBe(false);
      if (typeof observer?.options?.refetchInterval === 'function') {
        expect(observer.options.refetchInterval(query!)).toBe(false);
      }

      // 10. Correct the To Date to a valid date (e.g. 2026-09-05)
      fireEvent.change(toInput, { target: { value: '2026-09-05' } });

      // 11. Verify the valid RFC3339 filtered request resumes
      await waitFor(() => {
        expect(getSpy).toHaveBeenCalledWith(
          expect.stringContaining('from_date=2026-09-01T00%3A00%3A00Z')
        );
      });
      const lastCall = getSpy.mock.calls[getSpy.mock.calls.length - 1];
      expect(lastCall![0]).toContain('to_date=2026-09-05T23%3A59%3A59Z');
      expect(screen.queryByRole('alert')).not.toBeInTheDocument();
    });
  });

  describe('E. Clear Filters Action', () => {
    it('resets all controls and returns query to unfiltered endpoint', async () => {
      const getSpy = vi.spyOn(apiClient, 'get').mockResolvedValue(mockRuns);

      renderPage();

      // No clear button initially
      expect(screen.queryByLabelText('Clear all active filters')).not.toBeInTheDocument();

      // Apply status and date filters
      const statusSelect = screen.getByLabelText('Filter runs by status');
      const fromInput = screen.getByLabelText('Filter runs from date');
      fireEvent.change(statusSelect, { target: { value: 'failed' } });
      fireEvent.change(fromInput, { target: { value: '2026-09-01' } });

      // Clear button must appear
      const clearBtn = await screen.findByLabelText('Clear all active filters');
      expect(clearBtn).toBeInTheDocument();

      // Click Clear Filters
      fireEvent.click(clearBtn);

      // Verify inputs reset
      expect((statusSelect as HTMLSelectElement).value).toBe('all');
      expect((fromInput as HTMLInputElement).value).toBe('');

      // Verify query returns to unfiltered /backup-runs
      await waitFor(() => {
        expect(getSpy).toHaveBeenLastCalledWith('/backup-runs');
      });

      // Clear button disappears
      expect(screen.queryByLabelText('Clear all active filters')).not.toBeInTheDocument();
    });
  });

  describe('F. Tenant Isolation & Query Keys', () => {
    it('scopes query keys strictly by active organization ID with filter isolation', () => {
      const org1Key = queryKeys.org('org-1').runs.all({ status: 'failed' });
      const org2Key = queryKeys.org('org-2').runs.all({ status: 'failed' });
      const org1Unfiltered = queryKeys.org('org-1').runs.all({});

      expect(org1Key).toEqual(['org', 'org-1', 'runs', { status: 'failed' }]);
      expect(org2Key).toEqual(['org', 'org-2', 'runs', { status: 'failed' }]);
      expect(org1Key).not.toEqual(org2Key);
      expect(org1Key).not.toEqual(org1Unfiltered);
    });

    it('maintains strict cache and request isolation across tenants when filtered', async () => {
      const org1Run: BackupRunResponse = {
        ...mockRuns[0]!,
        id: '11111111-org1-test-000000000001',
        status: 'failed',
      };
      const org2Run: BackupRunResponse = {
        ...mockRuns[1]!,
        id: '22222222-org2-test-000000000002',
        status: 'failed',
      };

      const getSpy = vi.spyOn(apiClient, 'get').mockImplementation(async () => {
        return currentOrgId === 'org-1' ? [org1Run] : [org2Run];
      });

      // 1. Render under org-1 with filter
      const { unmount } = renderPage();

      const statusSelect = screen.getByLabelText('Filter runs by status');
      fireEvent.change(statusSelect, { target: { value: 'failed' } });

      await waitFor(() => {
        expect(getSpy).toHaveBeenCalledWith('/backup-runs?status=failed');
      });
      expect(await screen.findByText('11111111...')).toBeInTheDocument();

      // Verify org-1 cache entry exists under org-1 key
      const org1Cached = queryClient.getQueryData(['org', 'org-1', 'runs', { status: 'failed' }]);
      expect(org1Cached).toEqual([org1Run]);

      // 2. Unmount org-1 view and switch active tenant to org-2
      unmount();
      currentOrgId = 'org-2';

      // 3. Render under org-2 with filter
      renderPage();

      const statusSelectOrg2 = screen.getByLabelText('Filter runs by status');
      fireEvent.change(statusSelectOrg2, { target: { value: 'failed' } });

      await waitFor(() => {
        expect(screen.getByText('22222222...')).toBeInTheDocument();
      });

      // Verify org-2 cache entry is isolated and distinct from org-1
      const org2Cached = queryClient.getQueryData(['org', 'org-2', 'runs', { status: 'failed' }]);
      expect(org2Cached).toEqual([org2Run]);
      expect(org1Cached).not.toEqual(org2Cached);
    });
  });

  describe('G. Polling Semantics', () => {
    it('configures 3000ms polling when active runs exist, disables polling when completed only, and suspends polling on invalid date range', async () => {
      vi.spyOn(apiClient, 'get').mockResolvedValue(mockRuns);

      renderPage();

      await screen.findByText('Execution Records');

      // 1. Check query state in TanStack queryClient with active runs (mockRuns has 'running')
      const query = queryClient
        .getQueryCache()
        .find({ queryKey: ['org', 'org-1', 'runs', {}] });

      expect(query).toBeDefined();
      const observer = (query as any)?.observers?.[0];
      expect(observer).toBeDefined();
      const refetchIntervalFn = observer?.options?.refetchInterval;
      expect(typeof refetchIntervalFn).toBe('function');

      // Active runs -> 3000ms
      expect(refetchIntervalFn(query)).toBe(3000);

      // 2. Completed-only runs -> false
      const completedQueryMock = {
        state: {
          data: mockRuns.filter((r) => r.status === 'success' || r.status === 'failed'),
        },
      };
      expect(refetchIntervalFn(completedQueryMock)).toBe(false);

      // 3. Invalid date range -> false and query disabled
      const fromInput = screen.getByLabelText('Filter runs from date');
      const toInput = screen.getByLabelText('Filter runs to date');
      fireEvent.change(fromInput, { target: { value: '2026-09-10' } });
      fireEvent.change(toInput, { target: { value: '2026-09-05' } });

      expect(await screen.findByRole('alert')).toBeInTheDocument();
      expect(observer?.options?.enabled).toBe(false);
      expect(observer?.options?.refetchInterval(query)).toBe(false);
    });
  });

  describe('H. Filtered vs Unfiltered Empty States', () => {
    it('shows filter-aware empty message when filters are active and no runs match', async () => {
      vi.spyOn(apiClient, 'get').mockResolvedValue([]);

      renderPage();

      // Apply a filter
      const statusSelect = screen.getByLabelText('Filter runs by status');
      fireEvent.change(statusSelect, { target: { value: 'failed' } });

      expect(
        await screen.findByText('No runs match the selected filters')
      ).toBeInTheDocument();
      expect(
        screen.getByText('Try adjusting or clearing your filters to view other execution records.')
      ).toBeInTheDocument();
    });

    it('shows default empty state when no filters are active and tenant has no runs', async () => {
      vi.spyOn(apiClient, 'get').mockResolvedValue([]);

      renderPage();

      expect(
        await screen.findByText('No backup runs have been recorded')
      ).toBeInTheDocument();
      expect(
        screen.getByText('Execution records will appear here as backup jobs are scheduled or executed.')
      ).toBeInTheDocument();
    });
  });

  describe('I. Existing Runs UI Capabilities & Parity', () => {
    it('renders run details, attempt number, status badges, and navigation links correctly', async () => {
      vi.spyOn(apiClient, 'get').mockResolvedValue(mockRuns);

      renderPage();

      // Verify records appear
      expect(await screen.findByText('11111111...')).toBeInTheDocument();
      expect(screen.getByText('22222222...')).toBeInTheDocument();

      // Verify status badges
      expect(screen.getAllByText('Success').length).toBeGreaterThan(0);
      expect(screen.getAllByText('Failed').length).toBeGreaterThan(0);

      // Verify links to run detail
      const links = screen.getAllByRole('link', { name: /view run/i });
      expect(links.length).toBeGreaterThan(0);
      expect(links[0]).toHaveAttribute('href', '/runs/11111111-1111-1111-1111-111111111111');
    });
  });
});
