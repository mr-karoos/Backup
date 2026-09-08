import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import AuditLogsPage from '@/app/(dashboard)/audit-logs/page';
import { SidebarNav } from '@/components/layout/SidebarNav';
import * as AuthContextModule from '@/lib/auth/auth-context';
import { apiClient } from '@/lib/api/api-client';
import { ApiError } from '@/types/api';
import type { AuditLogDTO } from '@/types/domain';

vi.mock('next/navigation', () => ({
  usePathname: () => '/audit-logs',
  useRouter: () => ({
    push: vi.fn(),
    replace: vi.fn(),
    prefetch: vi.fn(),
  }),
}));

const mockLogs: AuditLogDTO[] = [
  {
    id: 'a0000000-0000-0000-0000-000000000001',
    user_id: '11111111-1111-1111-1111-111111111111',
    action: 'backup_plan.create',
    entity_type: 'backup_plan',
    entity_id: '22222222-2222-2222-2222-222222222222',
    ip_address: '192.168.1.50',
    user_agent: 'Mozilla/5.0 (Windows NT 10.0; Win64; x64)',
    created_at: '2026-09-08T10:00:00Z',
  },
  {
    id: 'a0000000-0000-0000-0000-000000000002',
    user_id: null,
    action: 'backup_run.complete',
    entity_type: 'backup_run',
    entity_id: null,
    ip_address: null,
    user_agent: null,
    created_at: '2026-09-08T10:30:00Z',
  },
];

describe('Frontend F2C: Audit Log Interface', () => {
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
        permissions: ['audit_log:read', 'resource:read'],
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

  // =========================================================================
  // 1. Navigation & RBAC Guard
  // =========================================================================
  describe('1. Navigation RBAC', () => {
    it('renders Audit Logs link for Admin with audit_log:read permission', () => {
      render(<SidebarNav />);
      const link = screen.getByRole('link', { name: /Audit Logs/i });
      expect(link).toBeInTheDocument();
      expect(link).toHaveAttribute('href', '/audit-logs');
    });

    it('hides Audit Logs link for Member without audit_log:read permission', () => {
      vi.spyOn(AuthContextModule, 'useAuth').mockReturnValue({
        status: 'authenticated',
        user: { id: 'u-2', email: 'member@example.com', full_name: 'Member User', is_system_admin: false },
        activeOrgId: 'org-1',
        userRole: 'member',
        activeMembership: {
          organization_id: 'org-1',
          organization_name: 'Test Org',
          organization_slug: 'test-org',
          role: 'member',
          status: 'active',
          is_default_internal: false,
          permissions: ['resource:read'],
        },
        memberships: [],
        isSystemAdmin: false,
        login: vi.fn(),
        logout: vi.fn(),
        switchOrganization: vi.fn(),
      });

      render(<SidebarNav />);
      expect(screen.queryByRole('link', { name: /Audit Logs/i })).toBeNull();
    });

    it('hides Audit Logs link for Viewer without audit_log:read permission', () => {
      vi.spyOn(AuthContextModule, 'useAuth').mockReturnValue({
        status: 'authenticated',
        user: { id: 'u-3', email: 'viewer@example.com', full_name: 'Viewer User', is_system_admin: false },
        activeOrgId: 'org-1',
        userRole: 'viewer',
        activeMembership: {
          organization_id: 'org-1',
          organization_name: 'Test Org',
          organization_slug: 'test-org',
          role: 'viewer',
          status: 'active',
          is_default_internal: false,
          permissions: ['resource:read'],
        },
        memberships: [],
        isSystemAdmin: false,
        login: vi.fn(),
        logout: vi.fn(),
        switchOrganization: vi.fn(),
      });

      render(<SidebarNav />);
      expect(screen.queryByRole('link', { name: /Audit Logs/i })).toBeNull();
    });
  });

  // =========================================================================
  // 2. Direct Page Access & RBAC
  // =========================================================================
  describe('2. Direct Page Access RBAC', () => {
    it('shows Access Restricted card for Member and dispatches zero API calls', () => {
      const getPaginatedSpy = vi.spyOn(apiClient, 'getPaginated');

      vi.spyOn(AuthContextModule, 'useAuth').mockReturnValue({
        status: 'authenticated',
        user: { id: 'u-2', email: 'member@example.com', full_name: 'Member User', is_system_admin: false },
        activeOrgId: 'org-1',
        userRole: 'member',
        activeMembership: {
          organization_id: 'org-1',
          organization_name: 'Test Org',
          organization_slug: 'test-org',
          role: 'member',
          status: 'active',
          is_default_internal: false,
          permissions: ['resource:read'],
        },
        memberships: [],
        isSystemAdmin: false,
        login: vi.fn(),
        logout: vi.fn(),
        switchOrganization: vi.fn(),
      });

      renderWithQuery(<AuditLogsPage />);

      expect(screen.getByText('Access Restricted')).toBeInTheDocument();
      expect(screen.getByText(/You do not have permission to view audit logs/i)).toBeInTheDocument();
      expect(getPaginatedSpy).not.toHaveBeenCalled();
    });

    it('shows Access Restricted card for Viewer and dispatches zero API calls', () => {
      const getPaginatedSpy = vi.spyOn(apiClient, 'getPaginated');

      vi.spyOn(AuthContextModule, 'useAuth').mockReturnValue({
        status: 'authenticated',
        user: { id: 'u-3', email: 'viewer@example.com', full_name: 'Viewer User', is_system_admin: false },
        activeOrgId: 'org-1',
        userRole: 'viewer',
        activeMembership: {
          organization_id: 'org-1',
          organization_name: 'Test Org',
          organization_slug: 'test-org',
          role: 'viewer',
          status: 'active',
          is_default_internal: false,
          permissions: ['resource:read'],
        },
        memberships: [],
        isSystemAdmin: false,
        login: vi.fn(),
        logout: vi.fn(),
        switchOrganization: vi.fn(),
      });

      renderWithQuery(<AuditLogsPage />);

      expect(screen.getByText('Access Restricted')).toBeInTheDocument();
      expect(getPaginatedSpy).not.toHaveBeenCalled();
    });
  });

  // =========================================================================
  // 3. Data Fetching, Tenant Scoping & Privacy
  // =========================================================================
  describe('3. Fetching, Tenant Scoping & Data Privacy', () => {
    it('dispatches GET /audit-logs with signal and tenantOrgId', async () => {
      const getPaginatedSpy = vi.spyOn(apiClient, 'getPaginated').mockResolvedValue({
        data: mockLogs,
        page: { next_cursor: null, has_more: false },
      });

      renderWithQuery(<AuditLogsPage />);

      await waitFor(() => {
        expect(getPaginatedSpy).toHaveBeenCalledWith(
          '/audit-logs',
          expect.objectContaining({
            tenantOrgId: 'org-1',
            signal: expect.any(AbortSignal),
          })
        );
      });
    });

    it('renders audit log entries with formatted dates, action badges, and null values as "—"', async () => {
      vi.spyOn(apiClient, 'getPaginated').mockResolvedValue({
        data: mockLogs,
        page: { next_cursor: null, has_more: false },
      });

      renderWithQuery(<AuditLogsPage />);

      await waitFor(() => {
        expect(screen.getByText('backup_plan.create')).toBeInTheDocument();
        expect(screen.getByText('backup_run.complete')).toBeInTheDocument();
      });

      // Check null fields rendered as "—"
      const dashes = screen.getAllByText('—');
      expect(dashes.length).toBeGreaterThanOrEqual(1);

      // Verify privacy: metadata and organization_id are never displayed
      expect(screen.queryByText(/metadata/i)).toBeNull();
      expect(screen.queryByText('org-1')).toBeNull();
    });
  });

  // =========================================================================
  // 4. Filters & Validation Defense-in-Depth
  // =========================================================================
  describe('4. Filters & Validation', () => {
    it('filters by action preserving exact raw value and proving it is not trimmed', async () => {
      const getPaginatedSpy = vi.spyOn(apiClient, 'getPaginated').mockResolvedValue({
        data: mockLogs,
        page: { next_cursor: null, has_more: false },
      });

      renderWithQuery(<AuditLogsPage />);

      const actionInput = screen.getByLabelText(/Filter by action/i);
      fireEvent.change(actionInput, { target: { value: ' backup.run.verified ' } });

      await waitFor(() => {
        expect(getPaginatedSpy.mock.calls.length).toBeGreaterThan(1);
        const lastCall = getPaginatedSpy.mock.calls[getPaginatedSpy.mock.calls.length - 1]![0];
        const parsedUrl = new URL('http://localhost' + lastCall);
        expect(parsedUrl.searchParams.get('action')).toBe(' backup.run.verified ');
      });
    });

    it('filters by entity_type preserving exact raw value and proving it is not trimmed', async () => {
      const getPaginatedSpy = vi.spyOn(apiClient, 'getPaginated').mockResolvedValue({
        data: mockLogs,
        page: { next_cursor: null, has_more: false },
      });

      renderWithQuery(<AuditLogsPage />);

      const entityTypeInput = screen.getByLabelText(/Filter by entity type/i);
      fireEvent.change(entityTypeInput, { target: { value: ' backup_run ' } });

      await waitFor(() => {
        expect(getPaginatedSpy.mock.calls.length).toBeGreaterThan(1);
        const lastCall = getPaginatedSpy.mock.calls[getPaginatedSpy.mock.calls.length - 1]![0];
        const parsedUrl = new URL('http://localhost' + lastCall);
        expect(parsedUrl.searchParams.get('entity_type')).toBe(' backup_run ');
      });
    });

    it('filters by valid entity_id UUID and includes entity_id in query', async () => {
      const getPaginatedSpy = vi.spyOn(apiClient, 'getPaginated').mockResolvedValue({
        data: mockLogs,
        page: { next_cursor: null, has_more: false },
      });

      renderWithQuery(<AuditLogsPage />);

      const entityIdInput = screen.getByLabelText(/Filter by entity UUID/i);
      fireEvent.change(entityIdInput, {
        target: { value: '22222222-2222-2222-2222-222222222222' },
      });

      await waitFor(() => {
        expect(getPaginatedSpy).toHaveBeenCalledWith(
          '/audit-logs?entity_id=22222222-2222-2222-2222-222222222222',
          expect.any(Object)
        );
      });
    });

    it('shows validation error for invalid entity_id format and disables query', async () => {
      const getPaginatedSpy = vi.spyOn(apiClient, 'getPaginated').mockResolvedValue({
        data: mockLogs,
        page: { next_cursor: null, has_more: false },
      });

      renderWithQuery(<AuditLogsPage />);
      getPaginatedSpy.mockClear();

      const entityIdInput = screen.getByLabelText(/Filter by entity UUID/i);
      fireEvent.change(entityIdInput, { target: { value: 'not-a-uuid' } });

      await waitFor(() => {
        expect(
          screen.getByText(/Invalid Entity ID format \(must be a valid non-nil UUID\)/i)
        ).toBeInTheDocument();
      });

      expect(getPaginatedSpy).not.toHaveBeenCalled();
    });

    it('shows validation error for nil UUID entity_id and disables query', async () => {
      const getPaginatedSpy = vi.spyOn(apiClient, 'getPaginated').mockResolvedValue({
        data: mockLogs,
        page: { next_cursor: null, has_more: false },
      });

      renderWithQuery(<AuditLogsPage />);
      getPaginatedSpy.mockClear();

      const entityIdInput = screen.getByLabelText(/Filter by entity UUID/i);
      fireEvent.change(entityIdInput, {
        target: { value: '00000000-0000-0000-0000-000000000000' },
      });

      await waitFor(() => {
        expect(
          screen.getByText(/Invalid Entity ID format \(must be a valid non-nil UUID\)/i)
        ).toBeInTheDocument();
      });

      expect(getPaginatedSpy).not.toHaveBeenCalled();
    });

    it('shows validation error for whitespace-padded entity_id and disables query', async () => {
      const getPaginatedSpy = vi.spyOn(apiClient, 'getPaginated').mockResolvedValue({
        data: mockLogs,
        page: { next_cursor: null, has_more: false },
      });

      renderWithQuery(<AuditLogsPage />);
      getPaginatedSpy.mockClear();

      const entityIdInput = screen.getByLabelText(/Filter by entity UUID/i);
      fireEvent.change(entityIdInput, {
        target: { value: ' aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa ' },
      });

      await waitFor(() => {
        expect(
          screen.getByText(/Invalid Entity ID format \(must be a valid non-nil UUID\)/i)
        ).toBeInTheDocument();
      });

      expect(getPaginatedSpy).not.toHaveBeenCalled();
    });

    it('ignores whitespace-only entity_id and sends clean /audit-logs query', async () => {
      const getPaginatedSpy = vi.spyOn(apiClient, 'getPaginated').mockResolvedValue({
        data: mockLogs,
        page: { next_cursor: null, has_more: false },
      });

      renderWithQuery(<AuditLogsPage />);

      const entityIdInput = screen.getByLabelText(/Filter by entity UUID/i);
      fireEvent.change(entityIdInput, {
        target: { value: '22222222-2222-2222-2222-222222222222' },
      });

      await waitFor(() => {
        expect(getPaginatedSpy.mock.calls.length).toBeGreaterThan(1);
        const url = new URL('http://localhost' + getPaginatedSpy.mock.calls[getPaginatedSpy.mock.calls.length - 1]![0]);
        expect(url.searchParams.get('entity_id')).toBe('22222222-2222-2222-2222-222222222222');
      });

      fireEvent.change(entityIdInput, { target: { value: '   ' } });

      await waitFor(() => {
        const lastCallUrl = new URL('http://localhost' + getPaginatedSpy.mock.calls[getPaginatedSpy.mock.calls.length - 1]![0]);
        expect(lastCallUrl.searchParams.has('entity_id')).toBe(false);
      });
    });

    it('filters by valid user_id UUID and includes user_id in query', async () => {
      const getPaginatedSpy = vi.spyOn(apiClient, 'getPaginated').mockResolvedValue({
        data: mockLogs,
        page: { next_cursor: null, has_more: false },
      });

      renderWithQuery(<AuditLogsPage />);

      const userIdInput = screen.getByLabelText(/Filter by user UUID/i);
      fireEvent.change(userIdInput, {
        target: { value: '11111111-1111-1111-1111-111111111111' },
      });

      await waitFor(() => {
        expect(getPaginatedSpy).toHaveBeenCalledWith(
          '/audit-logs?user_id=11111111-1111-1111-1111-111111111111',
          expect.any(Object)
        );
      });
    });

    it('shows validation error for invalid user_id format and disables query', async () => {
      const getPaginatedSpy = vi.spyOn(apiClient, 'getPaginated').mockResolvedValue({
        data: mockLogs,
        page: { next_cursor: null, has_more: false },
      });

      renderWithQuery(<AuditLogsPage />);
      getPaginatedSpy.mockClear();

      const userIdInput = screen.getByLabelText(/Filter by user UUID/i);
      fireEvent.change(userIdInput, { target: { value: 'invalid-user' } });

      await waitFor(() => {
        expect(
          screen.getByText(/Invalid User ID format \(must be a valid non-nil UUID\)/i)
        ).toBeInTheDocument();
      });

      expect(getPaginatedSpy).not.toHaveBeenCalled();
    });

    it('shows validation error for nil UUID user_id and disables query', async () => {
      const getPaginatedSpy = vi.spyOn(apiClient, 'getPaginated').mockResolvedValue({
        data: mockLogs,
        page: { next_cursor: null, has_more: false },
      });

      renderWithQuery(<AuditLogsPage />);
      getPaginatedSpy.mockClear();

      const userIdInput = screen.getByLabelText(/Filter by user UUID/i);
      fireEvent.change(userIdInput, {
        target: { value: '00000000-0000-0000-0000-000000000000' },
      });

      await waitFor(() => {
        expect(
          screen.getByText(/Invalid User ID format \(must be a valid non-nil UUID\)/i)
        ).toBeInTheDocument();
      });

      expect(getPaginatedSpy).not.toHaveBeenCalled();
    });

    it('shows validation error for whitespace-padded user_id and disables query', async () => {
      const getPaginatedSpy = vi.spyOn(apiClient, 'getPaginated').mockResolvedValue({
        data: mockLogs,
        page: { next_cursor: null, has_more: false },
      });

      renderWithQuery(<AuditLogsPage />);
      getPaginatedSpy.mockClear();

      const userIdInput = screen.getByLabelText(/Filter by user UUID/i);
      fireEvent.change(userIdInput, {
        target: { value: ' 33333333-3333-4333-8333-333333333333 ' },
      });

      await waitFor(() => {
        expect(
          screen.getByText(/Invalid User ID format \(must be a valid non-nil UUID\)/i)
        ).toBeInTheDocument();
      });

      expect(getPaginatedSpy).not.toHaveBeenCalled();
    });

    it('ignores whitespace-only user_id and sends clean /audit-logs query', async () => {
      const getPaginatedSpy = vi.spyOn(apiClient, 'getPaginated').mockResolvedValue({
        data: mockLogs,
        page: { next_cursor: null, has_more: false },
      });

      renderWithQuery(<AuditLogsPage />);

      const userIdInput = screen.getByLabelText(/Filter by user UUID/i);
      fireEvent.change(userIdInput, {
        target: { value: '11111111-1111-1111-1111-111111111111' },
      });

      await waitFor(() => {
        expect(getPaginatedSpy.mock.calls.length).toBeGreaterThan(1);
        const url = new URL('http://localhost' + getPaginatedSpy.mock.calls[getPaginatedSpy.mock.calls.length - 1]![0]);
        expect(url.searchParams.get('user_id')).toBe('11111111-1111-1111-1111-111111111111');
      });

      fireEvent.change(userIdInput, { target: { value: '   ' } });

      await waitFor(() => {
        const lastCallUrl = new URL('http://localhost' + getPaginatedSpy.mock.calls[getPaginatedSpy.mock.calls.length - 1]![0]);
        expect(lastCallUrl.searchParams.has('user_id')).toBe(false);
      });
    });

    it('serializes to date with full end-of-day RFC3339 precision .999999999Z', async () => {
      const getPaginatedSpy = vi.spyOn(apiClient, 'getPaginated').mockResolvedValue({
        data: mockLogs,
        page: { next_cursor: null, has_more: false },
      });

      renderWithQuery(<AuditLogsPage />);

      const toInput = screen.getByLabelText(/Filter to date/i);
      fireEvent.change(toInput, { target: { value: '2026-09-08' } });

      await waitFor(() => {
        expect(getPaginatedSpy.mock.calls.length).toBeGreaterThan(1);
        const lastCall = getPaginatedSpy.mock.calls[getPaginatedSpy.mock.calls.length - 1]![0];
        const url = new URL('http://localhost' + lastCall);
        expect(url.searchParams.get('to')).toBe('2026-09-08T23:59:59.999999999Z');
      });
    });

    it('serializes same-day range with 00:00:00Z and 23:59:59.999999999Z', async () => {
      const getPaginatedSpy = vi.spyOn(apiClient, 'getPaginated').mockResolvedValue({
        data: mockLogs,
        page: { next_cursor: null, has_more: false },
      });

      renderWithQuery(<AuditLogsPage />);

      const fromInput = screen.getByLabelText(/Filter from date/i);
      const toInput = screen.getByLabelText(/Filter to date/i);

      fireEvent.change(fromInput, { target: { value: '2026-09-08' } });
      fireEvent.change(toInput, { target: { value: '2026-09-08' } });

      await waitFor(() => {
        expect(getPaginatedSpy.mock.calls.length).toBeGreaterThan(1);
        const lastCall = getPaginatedSpy.mock.calls[getPaginatedSpy.mock.calls.length - 1]![0];
        const url = new URL('http://localhost' + lastCall);
        expect(url.searchParams.get('from')).toBe('2026-09-08T00:00:00Z');
        expect(url.searchParams.get('to')).toBe('2026-09-08T23:59:59.999999999Z');
      });
    });

    it('shows validation error when from date is after to date and disables query', async () => {
      const getPaginatedSpy = vi.spyOn(apiClient, 'getPaginated').mockResolvedValue({
        data: mockLogs,
        page: { next_cursor: null, has_more: false },
      });

      renderWithQuery(<AuditLogsPage />);
      getPaginatedSpy.mockClear();

      const fromInput = screen.getByLabelText(/Filter from date/i);
      const toInput = screen.getByLabelText(/Filter to date/i);

      fireEvent.change(fromInput, { target: { value: '2026-09-10' } });
      getPaginatedSpy.mockClear();
      fireEvent.change(toInput, { target: { value: '2026-09-05' } });

      await waitFor(() => {
        expect(screen.getByText(/From date cannot be after To date/i)).toBeInTheDocument();
      });

      expect(getPaginatedSpy).not.toHaveBeenCalled();
    });

    it('changes limit to 25 and sends limit=25 in query', async () => {
      const getPaginatedSpy = vi.spyOn(apiClient, 'getPaginated').mockResolvedValue({
        data: mockLogs,
        page: { next_cursor: null, has_more: false },
      });

      renderWithQuery(<AuditLogsPage />);

      const limitSelect = screen.getByLabelText(/Items per page/i);
      fireEvent.change(limitSelect, { target: { value: '25' } });

      await waitFor(() => {
        expect(getPaginatedSpy).toHaveBeenCalledWith(
          '/audit-logs?limit=25',
          expect.any(Object)
        );
      });
    });

    it('shows validation error when raw action exceeds 100 characters and disables query', async () => {
      const getPaginatedSpy = vi.spyOn(apiClient, 'getPaginated').mockResolvedValue({
        data: mockLogs,
        page: { next_cursor: null, has_more: false },
      });

      renderWithQuery(<AuditLogsPage />);
      getPaginatedSpy.mockClear();

      const actionInput = screen.getByLabelText(/Filter by action/i);
      // Raw 101 characters (raw length validation)
      fireEvent.change(actionInput, { target: { value: ' ' + 'a'.repeat(99) + ' ' } });

      await waitFor(() => {
        expect(
          screen.getByText(/Action filter must be 100 characters or fewer/i)
        ).toBeInTheDocument();
      });

      expect(getPaginatedSpy).not.toHaveBeenCalled();
    });

    it('shows validation error when raw entity_type exceeds 50 characters and disables query', async () => {
      const getPaginatedSpy = vi.spyOn(apiClient, 'getPaginated').mockResolvedValue({
        data: mockLogs,
        page: { next_cursor: null, has_more: false },
      });

      renderWithQuery(<AuditLogsPage />);
      getPaginatedSpy.mockClear();

      const entityTypeInput = screen.getByLabelText(/Filter by entity type/i);
      // Raw 51 characters (raw length validation)
      fireEvent.change(entityTypeInput, { target: { value: ' ' + 'e'.repeat(49) + ' ' } });

      await waitFor(() => {
        expect(
          screen.getByText(/Entity type filter must be 50 characters or fewer/i)
        ).toBeInTheDocument();
      });

      expect(getPaginatedSpy).not.toHaveBeenCalled();
    });

    it('ignores whitespace-only action and sends clean /audit-logs query', async () => {
      const getPaginatedSpy = vi.spyOn(apiClient, 'getPaginated').mockResolvedValue({
        data: mockLogs,
        page: { next_cursor: null, has_more: false },
      });

      renderWithQuery(<AuditLogsPage />);

      const actionInput = screen.getByLabelText(/Filter by action/i);
      fireEvent.change(actionInput, { target: { value: 'test.action' } });

      await waitFor(() => {
        expect(getPaginatedSpy.mock.calls.length).toBeGreaterThan(1);
        const url = new URL('http://localhost' + getPaginatedSpy.mock.calls[getPaginatedSpy.mock.calls.length - 1]![0]);
        expect(url.searchParams.get('action')).toBe('test.action');
      });

      fireEvent.change(actionInput, { target: { value: '   ' } });

      await waitFor(() => {
        const lastCallUrl = new URL('http://localhost' + getPaginatedSpy.mock.calls[getPaginatedSpy.mock.calls.length - 1]![0]);
        expect(lastCallUrl.searchParams.has('action')).toBe(false);
      });
    });

    it('ignores whitespace-only entity_type and sends clean /audit-logs query', async () => {
      const getPaginatedSpy = vi.spyOn(apiClient, 'getPaginated').mockResolvedValue({
        data: mockLogs,
        page: { next_cursor: null, has_more: false },
      });

      renderWithQuery(<AuditLogsPage />);

      const entityTypeInput = screen.getByLabelText(/Filter by entity type/i);
      fireEvent.change(entityTypeInput, { target: { value: 'test.type' } });

      await waitFor(() => {
        expect(getPaginatedSpy.mock.calls.length).toBeGreaterThan(1);
        const url = new URL('http://localhost' + getPaginatedSpy.mock.calls[getPaginatedSpy.mock.calls.length - 1]![0]);
        expect(url.searchParams.get('entity_type')).toBe('test.type');
      });

      fireEvent.change(entityTypeInput, { target: { value: '   ' } });

      await waitFor(() => {
        const lastCallUrl = new URL('http://localhost' + getPaginatedSpy.mock.calls[getPaginatedSpy.mock.calls.length - 1]![0]);
        expect(lastCallUrl.searchParams.has('entity_type')).toBe(false);
      });
    });

    it('clears all filters and resets to default query on clicking Clear Filters', async () => {
      const getPaginatedSpy = vi.spyOn(apiClient, 'getPaginated').mockResolvedValue({
        data: mockLogs,
        page: { next_cursor: null, has_more: false },
      });

      renderWithQuery(<AuditLogsPage />);

      const actionInput = screen.getByLabelText(/Filter by action/i);
      fireEvent.change(actionInput, { target: { value: 'test.action' } });

      const clearBtn = await screen.findByRole('button', { name: /Clear all active filters/i });
      fireEvent.click(clearBtn);

      await waitFor(() => {
        expect(getPaginatedSpy).toHaveBeenCalledWith('/audit-logs', expect.any(Object));
        expect((actionInput as HTMLInputElement).value).toBe('');
      });
    });
  });

  // =========================================================================
  // 5. Keyset Pagination
  // =========================================================================
  describe('5. Keyset Cursor Pagination', () => {
    it('disables Previous button on first page and enables Next button when next_cursor exists', async () => {
      vi.spyOn(apiClient, 'getPaginated').mockResolvedValue({
        data: mockLogs,
        page: { next_cursor: 'cursor-token-abc', has_more: true },
      });

      renderWithQuery(<AuditLogsPage />);

      await waitFor(() => {
        expect(screen.getByText('backup_plan.create')).toBeInTheDocument();
      });

      const prevBtn = screen.getByRole('button', { name: /Go to previous page/i });
      const nextBtn = screen.getByRole('button', { name: /Go to next page/i });

      expect(prevBtn).toBeDisabled();
      expect(nextBtn).toBeEnabled();
      expect(screen.getByText('Page 1')).toBeInTheDocument();
    });

    it('clicking Next fetches page with cursor and enables Previous button', async () => {
      const getPaginatedSpy = vi
        .spyOn(apiClient, 'getPaginated')
        .mockResolvedValueOnce({
          data: mockLogs,
          page: { next_cursor: 'cursor-page-2', has_more: true },
        })
        .mockResolvedValueOnce({
          data: [mockLogs[0]],
          page: { next_cursor: null, has_more: false },
        });

      renderWithQuery(<AuditLogsPage />);

      await waitFor(() => {
        expect(screen.getByText('backup_plan.create')).toBeInTheDocument();
      });

      const nextBtn = screen.getByRole('button', { name: /Go to next page/i });
      fireEvent.click(nextBtn);

      await waitFor(() => {
        expect(getPaginatedSpy).toHaveBeenCalledWith(
          '/audit-logs?cursor=cursor-page-2',
          expect.any(Object)
        );
        expect(screen.getByText('Page 2')).toBeInTheDocument();
      });

      const prevBtn = screen.getByRole('button', { name: /Go to previous page/i });
      expect(prevBtn).toBeEnabled();
    });

    it('clicking Previous returns to page 1', async () => {
      const getPaginatedSpy = vi
        .spyOn(apiClient, 'getPaginated')
        .mockResolvedValueOnce({
          data: mockLogs,
          page: { next_cursor: 'cursor-page-2', has_more: true },
        })
        .mockResolvedValueOnce({
          data: [mockLogs[0]],
          page: { next_cursor: null, has_more: false },
        })
        .mockResolvedValueOnce({
          data: mockLogs,
          page: { next_cursor: 'cursor-page-2', has_more: true },
        });

      renderWithQuery(<AuditLogsPage />);

      await waitFor(() => {
        expect(screen.getByText('backup_plan.create')).toBeInTheDocument();
      });

      const nextBtn = screen.getByRole('button', { name: /Go to next page/i });
      fireEvent.click(nextBtn);

      await waitFor(() => {
        expect(screen.getByText('Page 2')).toBeInTheDocument();
      });

      const prevBtn = screen.getByRole('button', { name: /Go to previous page/i });
      fireEvent.click(prevBtn);

      await waitFor(() => {
        expect(screen.getByText('Page 1')).toBeInTheDocument();
        expect(getPaginatedSpy).toHaveBeenLastCalledWith(
          '/audit-logs',
          expect.any(Object)
        );
      });
    });

    it('filter change resets cursor to null and returns to Page 1', async () => {
      const getPaginatedSpy = vi
        .spyOn(apiClient, 'getPaginated')
        .mockResolvedValueOnce({
          data: mockLogs,
          page: { next_cursor: 'cursor-page-2', has_more: true },
        })
        .mockResolvedValueOnce({
          data: [mockLogs[0]],
          page: { next_cursor: null, has_more: false },
        })
        .mockResolvedValueOnce({
          data: mockLogs,
          page: { next_cursor: null, has_more: false },
        });

      renderWithQuery(<AuditLogsPage />);

      await waitFor(() => {
        expect(screen.getByText('backup_plan.create')).toBeInTheDocument();
      });

      // Advance to page 2
      const nextBtn = screen.getByRole('button', { name: /Go to next page/i });
      fireEvent.click(nextBtn);

      await waitFor(() => {
        expect(screen.getByText('Page 2')).toBeInTheDocument();
      });

      // Changing filter should reset pagination to Page 1
      const actionInput = screen.getByLabelText(/Filter by action/i);
      fireEvent.change(actionInput, { target: { value: 'user.login' } });

      await waitFor(() => {
        expect(getPaginatedSpy).toHaveBeenLastCalledWith(
          '/audit-logs?action=user.login',
          expect.any(Object)
        );
        expect(screen.getByText('Page 1')).toBeInTheDocument();
      });
    });

    it('switching activeOrgId resets pagination and sends requests scoped to new tenant', async () => {
      const getPaginatedSpy = vi.spyOn(apiClient, 'getPaginated').mockResolvedValue({
        data: mockLogs,
        page: { next_cursor: 'cursor-p2', has_more: true },
      });

      const { rerender } = renderWithQuery(<AuditLogsPage />);

      await waitFor(() => {
        expect(getPaginatedSpy).toHaveBeenCalledWith(
          '/audit-logs',
          expect.objectContaining({ tenantOrgId: 'org-1' })
        );
        expect(screen.getByText('backup_plan.create')).toBeInTheDocument();
      });

      // Advance to page 2 in org-1
      const nextBtn = screen.getByRole('button', { name: /Go to next page/i });
      fireEvent.click(nextBtn);

      await waitFor(() => {
        expect(screen.getByText('Page 2')).toBeInTheDocument();
      });

      // Switch active tenant to org-2
      currentOrgId = 'org-2';
      rerender(
        <QueryClientProvider client={queryClient}>
          <AuditLogsPage />
        </QueryClientProvider>
      );

      await waitFor(() => {
        expect(getPaginatedSpy).toHaveBeenCalledWith(
          '/audit-logs',
          expect.objectContaining({ tenantOrgId: 'org-2' })
        );
        expect(screen.getByText('Page 1')).toBeInTheDocument();
      });
    });
  });

  // =========================================================================
  // 6. Loading, Empty & Error States
  // =========================================================================
  describe('6. UI States & Manual Refresh', () => {
    it('shows empty state when no audit records are found and filters are empty', async () => {
      vi.spyOn(apiClient, 'getPaginated').mockResolvedValue({
        data: [],
        page: { next_cursor: null, has_more: false },
      });

      renderWithQuery(<AuditLogsPage />);

      await waitFor(() => {
        expect(screen.getByText('No audit logs found.')).toBeInTheDocument();
        expect(
          screen.getByText(/Audit log events will appear here as actions are performed/i)
        ).toBeInTheDocument();
      });
    });

    it('shows filtered empty state with clear filters button when records are empty under active filter', async () => {
      vi.spyOn(apiClient, 'getPaginated').mockResolvedValue({
        data: [],
        page: { next_cursor: null, has_more: false },
      });

      renderWithQuery(<AuditLogsPage />);

      const actionInput = screen.getByLabelText(/Filter by action/i);
      fireEvent.change(actionInput, { target: { value: 'nonexistent.action' } });

      await waitFor(() => {
        expect(screen.getByText('No audit logs found')).toBeInTheDocument();
        expect(screen.getByText(/Try adjusting or clearing your filters/i)).toBeInTheDocument();
        expect(
          screen.getByRole('button', { name: /Clear filters and show all audit logs/i })
        ).toBeInTheDocument();
      });
    });

    it('renders error state with specific message for 400 Bad Request', async () => {
      vi.spyOn(apiClient, 'getPaginated').mockRejectedValue(
        new ApiError(400, 'BAD_REQUEST', 'Bad request')
      );

      renderWithQuery(<AuditLogsPage />);

      await waitFor(() => {
        expect(
          screen.getByText('Invalid audit log query parameters. Please check your filters.')
        ).toBeInTheDocument();
      });
    });

    it('renders error state with specific message for 403 Forbidden', async () => {
      vi.spyOn(apiClient, 'getPaginated').mockRejectedValue(
        new ApiError(403, 'FORBIDDEN', 'Forbidden')
      );

      renderWithQuery(<AuditLogsPage />);

      await waitFor(() => {
        expect(
          screen.getByText('You do not have permission to view audit logs.')
        ).toBeInTheDocument();
      });
    });

    it('renders error state with specific message for 404 Not Found', async () => {
      vi.spyOn(apiClient, 'getPaginated').mockRejectedValue(
        new ApiError(404, 'NOT_FOUND', 'Not found')
      );

      renderWithQuery(<AuditLogsPage />);

      await waitFor(() => {
        expect(
          screen.getByText('Organization or audit log resource not found.')
        ).toBeInTheDocument();
      });
    });

    it('renders error state with specific message for 503 Service Unavailable', async () => {
      vi.spyOn(apiClient, 'getPaginated').mockRejectedValue(
        new ApiError(503, 'SERVICE_UNAVAILABLE', 'Unavailable')
      );

      renderWithQuery(<AuditLogsPage />);

      await waitFor(() => {
        expect(
          screen.getByText('Audit service is temporarily unavailable.')
        ).toBeInTheDocument();
      });
    });

    it('clicking Retry on error state re-triggers query', async () => {
      const getPaginatedSpy = vi
        .spyOn(apiClient, 'getPaginated')
        .mockRejectedValueOnce(new ApiError(500, 'INTERNAL', 'Failed'))
        .mockResolvedValueOnce({
          data: mockLogs,
          page: { next_cursor: null, has_more: false },
        });

      renderWithQuery(<AuditLogsPage />);

      await waitFor(() => {
        expect(screen.getByText('Could not load audit logs')).toBeInTheDocument();
      });

      const retryBtn = screen.getByRole('button', { name: /Retry/i });
      fireEvent.click(retryBtn);

      await waitFor(() => {
        expect(getPaginatedSpy).toHaveBeenCalledTimes(2);
        expect(screen.getByText('backup_plan.create')).toBeInTheDocument();
      });
    });

    it('manual Refresh button in header calls refetch', async () => {
      const getPaginatedSpy = vi.spyOn(apiClient, 'getPaginated').mockResolvedValue({
        data: mockLogs,
        page: { next_cursor: null, has_more: false },
      });

      renderWithQuery(<AuditLogsPage />);

      await waitFor(() => {
        expect(screen.getByText('backup_plan.create')).toBeInTheDocument();
      });

      const refreshBtn = screen.getByRole('button', { name: /Refresh audit logs/i });
      fireEvent.click(refreshBtn);

      await waitFor(() => {
        expect(getPaginatedSpy).toHaveBeenCalledTimes(2);
      });
    });
  });
});
