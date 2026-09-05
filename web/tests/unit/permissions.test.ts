import { describe, it, expect } from 'vitest';
import { hasPermission, isOrganizationAdmin } from '@/lib/auth/permissions';
import type { MembershipSummary } from '@/types/auth';

describe('Permissions & RBAC Utilities', () => {
  const adminMembership: MembershipSummary = {
    organization_id: 'org-1',
    organization_name: 'Primary Org',
    organization_slug: 'primary-org',
    is_default_internal: false,
    role: 'admin',
    status: 'active',
    permissions: [
      'resource:write',
      'resource:read',
      'storage_target:write',
      'storage_target:read',
      'backup_plan:write',
      'backup_plan:read',
      'backup_job:execute',
      'backup_run:verify',
      'backup_artifact:delete',
      'organization:update',
    ],
  };

  const memberMembership: MembershipSummary = {
    organization_id: 'org-1',
    organization_name: 'Primary Org',
    organization_slug: 'primary-org',
    is_default_internal: false,
    role: 'member',
    status: 'active',
    permissions: [
      'resource:read',
      'storage_target:read',
      'backup_plan:read',
      'backup_job:execute',
      'backup_run:verify',
    ],
  };

  const viewerMembership: MembershipSummary = {
    organization_id: 'org-1',
    organization_name: 'Primary Org',
    organization_slug: 'primary-org',
    is_default_internal: false,
    role: 'viewer',
    status: 'active',
    permissions: [
      'resource:read',
      'storage_target:read',
      'backup_plan:read',
    ],
  };

  const suspendedMembership: MembershipSummary = {
    organization_id: 'org-1',
    organization_name: 'Primary Org',
    organization_slug: 'primary-org',
    is_default_internal: false,
    role: 'admin',
    status: 'suspended',
    permissions: ['resource:read'],
  };

  describe('isOrganizationAdmin', () => {
    it('returns true for active org admin role', () => {
      expect(isOrganizationAdmin(adminMembership)).toBe(true);
    });

    it('returns false for suspended admin', () => {
      expect(isOrganizationAdmin(suspendedMembership)).toBe(false);
    });

    it('returns false for regular member or viewer', () => {
      expect(isOrganizationAdmin(memberMembership)).toBe(false);
      expect(isOrganizationAdmin(viewerMembership)).toBe(false);
    });

    it('returns false when membership is null or undefined', () => {
      expect(isOrganizationAdmin(null)).toBe(false);
      expect(isOrganizationAdmin(undefined)).toBe(false);
    });
  });

  describe('hasPermission', () => {
    it('returns true when permission is present in membership', () => {
      expect(hasPermission(adminMembership, 'resource:write')).toBe(true);
      expect(hasPermission(memberMembership, 'backup_job:execute')).toBe(true);
      expect(hasPermission(memberMembership, 'backup_run:verify')).toBe(true);
    });

    it('returns false when permission is missing in membership', () => {
      expect(hasPermission(memberMembership, 'resource:write')).toBe(false);
      expect(hasPermission(memberMembership, 'backup_artifact:delete')).toBe(false);
      expect(hasPermission(viewerMembership, 'backup_job:execute')).toBe(false);
      expect(hasPermission(viewerMembership, 'backup_run:verify')).toBe(false);
    });

    it('returns false when membership is null or has no permissions', () => {
      expect(hasPermission(null, 'resource:read')).toBe(false);
      expect(hasPermission(undefined, 'resource:read')).toBe(false);
      expect(hasPermission({} as any, 'resource:read')).toBe(false);
    });
  });
});
