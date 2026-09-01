package authz

import (
	orgDomain "backup-platform/internal/organization/domain"
)

// Permission represents a fine-grained capability within a tenant organization.
type Permission string

const (
	PermissionResourceRead           Permission = "resource:read"
	PermissionResourceWrite          Permission = "resource:write"
	PermissionCredentialWrite        Permission = "credential:write"
	PermissionBackupPlanRead         Permission = "backup_plan:read"
	PermissionBackupPlanWrite        Permission = "backup_plan:write"
	PermissionBackupJobExecute       Permission = "backup_job:execute"
	PermissionBackupRunVerify        Permission = "backup_run:verify"
	PermissionBackupArtifactDownload Permission = "backup_artifact:download"
	PermissionBackupArtifactDelete   Permission = "backup_artifact:delete"
	PermissionAuditLogRead           Permission = "audit_log:read"
)

var adminPermissions = []Permission{
	PermissionResourceRead,
	PermissionResourceWrite,
	PermissionCredentialWrite,
	PermissionBackupPlanRead,
	PermissionBackupPlanWrite,
	PermissionBackupJobExecute,
	PermissionBackupRunVerify,
	PermissionBackupArtifactDownload,
	PermissionBackupArtifactDelete,
	PermissionAuditLogRead,
}

var memberPermissions = []Permission{
	PermissionResourceRead,
	PermissionBackupPlanRead,
	PermissionBackupJobExecute,
	PermissionBackupRunVerify,
	PermissionBackupArtifactDownload,
}

var viewerPermissions = []Permission{
	PermissionResourceRead,
	PermissionBackupPlanRead,
}

// PermissionsForRole returns the canonical V1 typed permission set for a given organization role.
func PermissionsForRole(role orgDomain.Role) []Permission {
	switch role {
	case orgDomain.RoleAdmin:
		res := make([]Permission, len(adminPermissions))
		copy(res, adminPermissions)
		return res
	case orgDomain.RoleMember:
		res := make([]Permission, len(memberPermissions))
		copy(res, memberPermissions)
		return res
	case orgDomain.RoleViewer:
		res := make([]Permission, len(viewerPermissions))
		copy(res, viewerPermissions)
		return res
	default:
		return []Permission{}
	}
}

// PermissionStringsForRole returns the string representations of the permissions for JSON DTO serialization.
func PermissionStringsForRole(role orgDomain.Role) []string {
	perms := PermissionsForRole(role)
	res := make([]string, len(perms))
	for i, p := range perms {
		res[i] = string(p)
	}
	return res
}

// HasPermission checks if the given role possesses the specified permission.
func HasPermission(role orgDomain.Role, permission Permission) bool {
	perms := PermissionsForRole(role)
	for _, p := range perms {
		if p == permission {
			return true
		}
	}
	return false
}
