import { useAuth } from './auth-context';
import type { MembershipSummary } from '@/types/auth';

export type PlatformPermission =
  | 'resource:read'
  | 'resource:write'
  | 'credential:read'
  | 'credential:write'
  | 'backup_plan:read'
  | 'backup_plan:write'
  | 'backup_job:execute'
  | 'backup_run:verify'
  | 'backup_artifact:download'
  | 'backup_artifact:delete'
  | 'storage_target:read'
  | 'storage_target:write'
  | 'audit_log:read';

/**
 * Checks whether a given membership contains a specific backend permission.
 */
export function hasPermission(
  membership: MembershipSummary | null | undefined,
  permission: PlatformPermission | string
): boolean {
  if (!membership || !membership.permissions) {
    return false;
  }
  return membership.permissions.includes(permission);
}

/**
 * Checks whether the membership is an active Organization Administrator.
 */
export function isOrganizationAdmin(
  membership: MembershipSummary | null | undefined
): boolean {
  return membership?.role === 'admin' && membership?.status === 'active';
}

/**
 * Reactive hook providing role and permission capabilities for current active tenant context.
 * Used strictly for UX visibility — backend remains the security authority.
 */
export function usePermissions() {
  const { activeMembership, isSystemAdmin, userRole } = useAuth();

  const isOrgAdmin = isOrganizationAdmin(activeMembership);

  const checkPermission = (perm: PlatformPermission | string): boolean => {
    return hasPermission(activeMembership, perm);
  };

  return {
    role: userRole,
    isOrgAdmin,
    isSystemAdmin,
    hasPermission: checkPermission,

    // High-level UX capability flags
    canViewCredentials: isOrgAdmin,
    canManageCredentials: isOrgAdmin,

    canCreateResource: checkPermission('resource:write'),
    canEditResource: checkPermission('resource:write'),
    canArchiveResource: checkPermission('resource:write'),
    canTestConnection: checkPermission('resource:write'),
    canDiscoverDatabases: checkPermission('resource:write'),

    canManageStorage: checkPermission('storage_target:write'),

    canCreatePlan: checkPermission('backup_plan:write'),
    canEditPlan: checkPermission('backup_plan:write'),
    canArchivePlan: checkPermission('backup_plan:write'),

    // Plan-backed manual backup requires backup_job:execute
    canExecutePlanBackup: checkPermission('backup_job:execute'),
    // Ad-hoc manual backup without a plan requires Admin in addition to execute permission
    canExecuteAdHocBackup: isOrgAdmin && checkPermission('backup_job:execute'),

    canVerifyRun: checkPermission('backup_run:verify'),
    canDeleteArtifact: checkPermission('backup_artifact:delete'),

    canUpdateOrganization: isOrgAdmin || isSystemAdmin,
  };
}
