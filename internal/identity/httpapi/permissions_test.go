package httpapi

import (
	"slices"
	"testing"

	orgDomain "backup-platform/internal/organization/domain"
)

func TestPermissionsForRole(t *testing.T) {
	t.Run("admin role permissions", func(t *testing.T) {
		perms := PermissionsForRole(orgDomain.RoleAdmin)
		expected := []string{
			"resource:read",
			"resource:write",
			"credential:write",
			"backup_plan:read",
			"backup_plan:write",
			"backup_job:execute",
			"backup_run:verify",
			"backup_artifact:download",
			"backup_artifact:delete",
			"audit_log:read",
			"storage_target:read",
			"storage_target:write",
		}
		if len(perms) != len(expected) {
			t.Fatalf("expected %d admin perms, got %d", len(expected), len(perms))
		}
		for _, p := range expected {
			if !slices.Contains(perms, p) {
				t.Errorf("admin missing expected permission: %s", p)
			}
		}
	})

	t.Run("member role restrictions", func(t *testing.T) {
		perms := PermissionsForRole(orgDomain.RoleMember)
		expected := []string{
			"resource:read",
			"backup_plan:read",
			"backup_job:execute",
			"backup_run:verify",
			"backup_artifact:download",
			"storage_target:read",
		}
		if len(perms) != len(expected) {
			t.Fatalf("expected %d member perms, got %d", len(expected), len(perms))
		}
		for _, p := range expected {
			if !slices.Contains(perms, p) {
				t.Errorf("member missing expected permission: %s", p)
			}
		}

		forbiddenForMember := []string{
			"resource:write",
			"credential:write",
			"backup_plan:write",
			"backup_artifact:delete",
			"audit_log:read",
			"storage_target:write",
			"restore:execute",
		}
		for _, f := range forbiddenForMember {
			if slices.Contains(perms, f) {
				t.Errorf("member must NOT have permission: %s", f)
			}
		}
	})

	t.Run("viewer role restrictions", func(t *testing.T) {
		perms := PermissionsForRole(orgDomain.RoleViewer)
		expected := []string{
			"resource:read",
			"backup_plan:read",
			"storage_target:read",
		}
		if len(perms) != len(expected) {
			t.Fatalf("expected %d viewer perms, got %d", len(expected), len(perms))
		}
		for _, p := range expected {
			if !slices.Contains(perms, p) {
				t.Errorf("viewer missing expected permission: %s", p)
			}
		}
	})

	t.Run("no role contains restore:execute", func(t *testing.T) {
		roles := []orgDomain.Role{orgDomain.RoleAdmin, orgDomain.RoleMember, orgDomain.RoleViewer, "unknown"}
		for _, r := range roles {
			perms := PermissionsForRole(r)
			if slices.Contains(perms, "restore:execute") {
				t.Errorf("role %s must not contain 'restore:execute'", r)
			}
		}
	})

	t.Run("unknown role returns empty", func(t *testing.T) {
		perms := PermissionsForRole("nonexistent")
		if len(perms) != 0 {
			t.Errorf("expected empty permissions for invalid role, got: %v", perms)
		}
	})
}
