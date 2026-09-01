package authz

import (
	"testing"

	orgDomain "backup-platform/internal/organization/domain"
)

func TestPermissionsForRole(t *testing.T) {
	t.Run("admin role receives all 10 canonical permissions", func(t *testing.T) {
		perms := PermissionsForRole(orgDomain.RoleAdmin)
		if len(perms) != 10 {
			t.Fatalf("expected 10 permissions for admin, got %d", len(perms))
		}

		expected := map[Permission]bool{
			PermissionResourceRead:           true,
			PermissionResourceWrite:          true,
			PermissionCredentialWrite:        true,
			PermissionBackupPlanRead:         true,
			PermissionBackupPlanWrite:        true,
			PermissionBackupJobExecute:       true,
			PermissionBackupRunVerify:        true,
			PermissionBackupArtifactDownload: true,
			PermissionBackupArtifactDelete:   true,
			PermissionAuditLogRead:           true,
		}

		for _, p := range perms {
			if !expected[p] {
				t.Errorf("unexpected permission for admin: %s", p)
			}
		}
	})

	t.Run("member role permissions restricted to 5 canonical permissions", func(t *testing.T) {
		perms := PermissionsForRole(orgDomain.RoleMember)
		if len(perms) != 5 {
			t.Fatalf("expected 5 permissions for member, got %d", len(perms))
		}

		// Member must NOT have write/delete permissions
		for _, p := range perms {
			if p == PermissionResourceWrite || p == PermissionCredentialWrite ||
				p == PermissionBackupPlanWrite || p == PermissionBackupArtifactDelete ||
				p == PermissionAuditLogRead {
				t.Errorf("SECURITY FLAW: member role granted forbidden admin permission: %s", p)
			}
		}

		if !HasPermission(orgDomain.RoleMember, PermissionResourceRead) ||
			!HasPermission(orgDomain.RoleMember, PermissionBackupPlanRead) ||
			!HasPermission(orgDomain.RoleMember, PermissionBackupJobExecute) ||
			!HasPermission(orgDomain.RoleMember, PermissionBackupRunVerify) ||
			!HasPermission(orgDomain.RoleMember, PermissionBackupArtifactDownload) {
			t.Errorf("member role missing expected canonical permissions")
		}
	})

	t.Run("viewer role permissions restricted to read-only", func(t *testing.T) {
		perms := PermissionsForRole(orgDomain.RoleViewer)
		if len(perms) != 2 {
			t.Fatalf("expected 2 permissions for viewer, got %d", len(perms))
		}

		if !HasPermission(orgDomain.RoleViewer, PermissionResourceRead) ||
			!HasPermission(orgDomain.RoleViewer, PermissionBackupPlanRead) {
			t.Errorf("viewer role missing expected read permissions")
		}

		if HasPermission(orgDomain.RoleViewer, PermissionBackupJobExecute) ||
			HasPermission(orgDomain.RoleViewer, PermissionBackupRunVerify) ||
			HasPermission(orgDomain.RoleViewer, PermissionBackupArtifactDownload) {
			t.Errorf("SECURITY FLAW: viewer granted operational permissions")
		}
	})

	t.Run("no role contains non-canonical restore:execute permission", func(t *testing.T) {
		roles := []orgDomain.Role{orgDomain.RoleAdmin, orgDomain.RoleMember, orgDomain.RoleViewer}
		for _, r := range roles {
			perms := PermissionsForRole(r)
			for _, p := range perms {
				if string(p) == "restore:execute" {
					t.Errorf("SECURITY FLAW: role %s contains deprecated restore:execute", r)
				}
			}
		}
	})

	t.Run("unknown or empty roles return empty slice and fail closed", func(t *testing.T) {
		unknownRoles := []orgDomain.Role{"owner", "superadmin", "operator", "root", ""}
		for _, r := range unknownRoles {
			perms := PermissionsForRole(r)
			if len(perms) != 0 {
				t.Errorf("expected empty permissions for unknown role %q, got: %v", r, perms)
			}
			if HasPermission(r, PermissionResourceRead) {
				t.Errorf("unknown role %q should have no permissions", r)
			}
		}
	})

	t.Run("PermissionStringsForRole returns valid string slice matching typed permissions", func(t *testing.T) {
		adminStrs := PermissionStringsForRole(orgDomain.RoleAdmin)
		if len(adminStrs) != 10 {
			t.Errorf("expected 10 strings for admin, got: %d", len(adminStrs))
		}
		if adminStrs[0] != "resource:read" {
			t.Errorf("expected string representation match")
		}
	})
}
