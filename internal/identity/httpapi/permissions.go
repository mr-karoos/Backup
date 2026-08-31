package httpapi

import (
	"backup-platform/internal/organization/authz"
	orgDomain "backup-platform/internal/organization/domain"
)

// PermissionsForRole delegates to the central organization authorization package.
func PermissionsForRole(role orgDomain.Role) []string {
	return authz.PermissionStringsForRole(role)
}
