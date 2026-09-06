import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { SidebarNav } from '@/components/layout/SidebarNav';
import * as AuthContextModule from '@/lib/auth/auth-context';

// Mock usePathname from next/navigation
vi.mock('next/navigation', () => ({
  usePathname: () => '/',
}));

describe('RBAC Navigation Visibility', () => {
  it('hides Credentials link when caller is a Viewer', () => {
    vi.spyOn(AuthContextModule, 'useAuth').mockReturnValue({
      status: 'authenticated',
      user: { id: 'u1', email: 'viewer@domain.com', full_name: 'Viewer', is_system_admin: false },
      memberships: [],
      activeOrgId: 'org-1',
      activeMembership: null,
      isSystemAdmin: false,
      userRole: 'viewer',
      login: vi.fn(),
      logout: vi.fn(),
      switchOrganization: vi.fn(),
    });

    render(<SidebarNav />);

    // Viewer should see Dashboard, Resources, Plans, Runs, Artifacts, Storage, Settings, Health
    expect(screen.getByText('Dashboard')).toBeInTheDocument();
    expect(screen.getByText('Resources')).toBeInTheDocument();
    expect(screen.getByText('Backup Plans')).toBeInTheDocument();
    expect(screen.getByText('Backup Runs')).toBeInTheDocument();
    expect(screen.getByText('Artifacts')).toBeInTheDocument();
    expect(screen.getByText('Storage Targets')).toBeInTheDocument();

    // Viewer must NOT see Credentials
    expect(screen.queryByText('Credentials')).toBeNull();

    // Deferred future features must NOT be in navigation
    expect(screen.queryByText('Team')).toBeNull();
    expect(screen.queryByText('Audit')).toBeNull();
    expect(screen.queryByText('Restore')).toBeNull();
    expect(screen.queryByText('Restic')).toBeNull();
  });

  it('exposes Credentials link when caller is an Organization Admin', () => {
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

    render(<SidebarNav />);

    // Admin should see Credentials
    expect(screen.getByText('Credentials')).toBeInTheDocument();

    // Deferred future features must STILL NOT be exposed
    expect(screen.queryByText('Team')).toBeNull();
    expect(screen.queryByText('Audit')).toBeNull();
    expect(screen.queryByText('Restore')).toBeNull();
  });

  it('hides Credentials link when caller is a System Admin without Org Admin membership', () => {
    vi.spyOn(AuthContextModule, 'useAuth').mockReturnValue({
      status: 'authenticated',
      user: { id: 'u2', email: 'sysadmin@domain.com', full_name: 'Sys Admin', is_system_admin: true },
      memberships: [],
      activeOrgId: 'org-1',
      activeMembership: {
        organization_id: 'org-1',
        organization_name: 'Org 1',
        organization_slug: 'org-1',
        is_default_internal: true,
        role: 'member',
        status: 'active',
        permissions: ['resource:read'],
      },
      isSystemAdmin: true,

      userRole: 'member',
      login: vi.fn(),
      logout: vi.fn(),
      switchOrganization: vi.fn(),
    });

    render(<SidebarNav />);

    // System admin without org admin role must NOT see Credentials in tenant context
    expect(screen.queryByText('Credentials')).toBeNull();
  });
});

