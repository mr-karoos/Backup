package httpapi

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	identityDomain "backup-platform/internal/identity/domain"
	identityHttpapi "backup-platform/internal/identity/httpapi"
	"backup-platform/internal/organization/authz"
	orgDomain "backup-platform/internal/organization/domain"
	orgRepo "backup-platform/internal/organization/repository"
	"backup-platform/pkg/uuid"
)

func TestRequirePermission(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	var handlerCalled bool
	dummyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("PERMITTED"))
	})

	orgID := uuid.New()
	userID := uuid.New()

	t.Run("admin role permitted for sensitive credential:write", func(t *testing.T) {
		handlerCalled = false
		guard := RequirePermission(authz.PermissionCredentialWrite, logger)(dummyHandler)

		tenantCtx := &TenantContext{
			UserID:            userID,
			OrganizationID:    orgID,
			OrganizationName:  "Test Org",
			OrganizationSlug:  "test-org",
			IsDefaultInternal: false,
			Role:              orgDomain.RoleAdmin,
			MembershipStatus:  orgDomain.MemberStatusActive,
		}

		req := httptest.NewRequest("POST", "/api/v1/credentials", nil)
		req = req.WithContext(WithTenantContext(req.Context(), tenantCtx))
		rec := httptest.NewRecorder()

		guard.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected 200 OK for admin credential:write, got: %d", rec.Code)
		}
		if !handlerCalled {
			t.Errorf("expected dummyHandler to be called")
		}
	})

	t.Run("member role rejected for credential:write with 403 INSUFFICIENT_PERMISSIONS", func(t *testing.T) {
		handlerCalled = false
		guard := RequirePermission(authz.PermissionCredentialWrite, logger)(dummyHandler)

		tenantCtx := &TenantContext{
			UserID:            userID,
			OrganizationID:    orgID,
			OrganizationName:  "Test Org",
			OrganizationSlug:  "test-org",
			IsDefaultInternal: false,
			Role:              orgDomain.RoleMember,
			MembershipStatus:  orgDomain.MemberStatusActive,
		}

		req := httptest.NewRequest("POST", "/api/v1/credentials", nil)
		req = req.WithContext(WithTenantContext(req.Context(), tenantCtx))
		rec := httptest.NewRecorder()

		guard.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("expected 403 Forbidden for member credential:write, got: %d", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "INSUFFICIENT_PERMISSIONS") {
			t.Errorf("expected code INSUFFICIENT_PERMISSIONS, got: %s", rec.Body.String())
		}
		if handlerCalled {
			t.Errorf("SECURITY FLAW: handler called on unauthorized request")
		}
	})

	t.Run("member role permitted for backup_job:execute", func(t *testing.T) {
		handlerCalled = false
		guard := RequirePermission(authz.PermissionBackupJobExecute, logger)(dummyHandler)

		tenantCtx := &TenantContext{
			UserID:            userID,
			OrganizationID:    orgID,
			OrganizationName:  "Test Org",
			OrganizationSlug:  "test-org",
			IsDefaultInternal: false,
			Role:              orgDomain.RoleMember,
			MembershipStatus:  orgDomain.MemberStatusActive,
		}

		req := httptest.NewRequest("POST", "/api/v1/backup-jobs", nil)
		req = req.WithContext(WithTenantContext(req.Context(), tenantCtx))
		rec := httptest.NewRecorder()

		guard.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected 200 OK for member backup_job:execute, got: %d", rec.Code)
		}
		if !handlerCalled {
			t.Errorf("expected dummyHandler to be called")
		}
	})

	t.Run("viewer role rejected for backup_job:execute with 403", func(t *testing.T) {
		handlerCalled = false
		guard := RequirePermission(authz.PermissionBackupJobExecute, logger)(dummyHandler)

		tenantCtx := &TenantContext{
			UserID:            userID,
			OrganizationID:    orgID,
			OrganizationName:  "Test Org",
			OrganizationSlug:  "test-org",
			IsDefaultInternal: false,
			Role:              orgDomain.RoleViewer,
			MembershipStatus:  orgDomain.MemberStatusActive,
		}

		req := httptest.NewRequest("POST", "/api/v1/backup-jobs", nil)
		req = req.WithContext(WithTenantContext(req.Context(), tenantCtx))
		rec := httptest.NewRecorder()

		guard.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("expected 403 Forbidden for viewer backup_job:execute, got: %d", rec.Code)
		}
		if handlerCalled {
			t.Errorf("SECURITY FLAW: handler called on unauthorized request")
		}
	})

	t.Run("viewer role permitted for resource:read", func(t *testing.T) {
		handlerCalled = false
		guard := RequirePermission(authz.PermissionResourceRead, logger)(dummyHandler)

		tenantCtx := &TenantContext{
			UserID:            userID,
			OrganizationID:    orgID,
			OrganizationName:  "Test Org",
			OrganizationSlug:  "test-org",
			IsDefaultInternal: false,
			Role:              orgDomain.RoleViewer,
			MembershipStatus:  orgDomain.MemberStatusActive,
		}

		req := httptest.NewRequest("GET", "/api/v1/resources", nil)
		req = req.WithContext(WithTenantContext(req.Context(), tenantCtx))
		rec := httptest.NewRecorder()

		guard.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected 200 OK for viewer resource:read, got: %d", rec.Code)
		}
		if !handlerCalled {
			t.Errorf("expected dummyHandler to be called")
		}
	})

	t.Run("unknown role rejected with 403", func(t *testing.T) {
		handlerCalled = false
		guard := RequirePermission(authz.PermissionResourceRead, logger)(dummyHandler)

		tenantCtx := &TenantContext{
			UserID:            userID,
			OrganizationID:    orgID,
			OrganizationName:  "Test Org",
			OrganizationSlug:  "test-org",
			IsDefaultInternal: false,
			Role:              orgDomain.Role("unknown_role"),
			MembershipStatus:  orgDomain.MemberStatusActive,
		}

		req := httptest.NewRequest("GET", "/api/v1/resources", nil)
		req = req.WithContext(WithTenantContext(req.Context(), tenantCtx))
		rec := httptest.NewRecorder()

		guard.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("expected 403 for unknown role, got: %d", rec.Code)
		}
		if handlerCalled {
			t.Errorf("SECURITY FLAW: handler called on unauthorized request")
		}
	})

	t.Run("missing TenantContext fails closed with 403 FORBIDDEN", func(t *testing.T) {
		handlerCalled = false
		guard := RequirePermission(authz.PermissionResourceRead, logger)(dummyHandler)

		req := httptest.NewRequest("GET", "/api/v1/resources", nil)
		// No TenantContext attached
		rec := httptest.NewRecorder()

		guard.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("expected 403 when TenantContext is missing, got: %d", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "FORBIDDEN") {
			t.Errorf("expected code FORBIDDEN, got: %s", rec.Body.String())
		}
		if handlerCalled {
			t.Errorf("SECURITY FLAW: handler called on missing TenantContext")
		}
	})

	// Defense-in-depth: Context Integrity Tests
	t.Run("tenant context with suspended membership status fails closed with 403", func(t *testing.T) {
		handlerCalled = false
		guard := RequirePermission(authz.PermissionCredentialWrite, logger)(dummyHandler)

		tenantCtx := &TenantContext{
			UserID:            userID,
			OrganizationID:    orgID,
			OrganizationName:  "Test Org",
			OrganizationSlug:  "test-org",
			IsDefaultInternal: false,
			Role:              orgDomain.RoleAdmin, // Role is Admin but membership is suspended!
			MembershipStatus:  orgDomain.MemberStatusSuspended,
		}

		req := httptest.NewRequest("POST", "/api/v1/credentials", nil)
		req = req.WithContext(WithTenantContext(req.Context(), tenantCtx))
		rec := httptest.NewRecorder()

		guard.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("expected 403 Forbidden for suspended membership status, got: %d", rec.Code)
		}
		if handlerCalled {
			t.Errorf("SECURITY FLAW: handler called on suspended membership status")
		}
	})

	t.Run("tenant context with invited membership status fails closed with 403", func(t *testing.T) {
		handlerCalled = false
		guard := RequirePermission(authz.PermissionCredentialWrite, logger)(dummyHandler)

		tenantCtx := &TenantContext{
			UserID:            userID,
			OrganizationID:    orgID,
			OrganizationName:  "Test Org",
			OrganizationSlug:  "test-org",
			IsDefaultInternal: false,
			Role:              orgDomain.RoleAdmin, // Role is Admin but membership is invited!
			MembershipStatus:  orgDomain.MemberStatusInvited,
		}

		req := httptest.NewRequest("POST", "/api/v1/credentials", nil)
		req = req.WithContext(WithTenantContext(req.Context(), tenantCtx))
		rec := httptest.NewRecorder()

		guard.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("expected 403 Forbidden for invited membership status, got: %d", rec.Code)
		}
		if handlerCalled {
			t.Errorf("SECURITY FLAW: handler called on invited membership status")
		}
	})

	t.Run("tenant context with zero UserID fails closed with 403", func(t *testing.T) {
		handlerCalled = false
		guard := RequirePermission(authz.PermissionCredentialWrite, logger)(dummyHandler)

		tenantCtx := &TenantContext{
			UserID:            uuid.Nil, // Zero UUID
			OrganizationID:    orgID,
			OrganizationName:  "Test Org",
			OrganizationSlug:  "test-org",
			IsDefaultInternal: false,
			Role:              orgDomain.RoleAdmin,
			MembershipStatus:  orgDomain.MemberStatusActive,
		}

		req := httptest.NewRequest("POST", "/api/v1/credentials", nil)
		req = req.WithContext(WithTenantContext(req.Context(), tenantCtx))
		rec := httptest.NewRecorder()

		guard.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("expected 403 Forbidden for zero UserID, got: %d", rec.Code)
		}
		if handlerCalled {
			t.Errorf("SECURITY FLAW: handler called on zero UserID")
		}
	})

	t.Run("tenant context with zero OrganizationID fails closed with 403", func(t *testing.T) {
		handlerCalled = false
		guard := RequirePermission(authz.PermissionCredentialWrite, logger)(dummyHandler)

		tenantCtx := &TenantContext{
			UserID:            userID,
			OrganizationID:    uuid.Nil, // Zero UUID
			OrganizationName:  "Test Org",
			OrganizationSlug:  "test-org",
			IsDefaultInternal: false,
			Role:              orgDomain.RoleAdmin,
			MembershipStatus:  orgDomain.MemberStatusActive,
		}

		req := httptest.NewRequest("POST", "/api/v1/credentials", nil)
		req = req.WithContext(WithTenantContext(req.Context(), tenantCtx))
		rec := httptest.NewRecorder()

		guard.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("expected 403 Forbidden for zero OrganizationID, got: %d", rec.Code)
		}
		if handlerCalled {
			t.Errorf("SECURITY FLAW: handler called on zero OrganizationID")
		}
	})
}

func TestRequireSystemAdmin(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	var handlerCalled bool
	dummyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ADMIN_OK"))
	})
	guard := RequireSystemAdmin(logger)(dummyHandler)

	t.Run("system admin with active status is granted platform access", func(t *testing.T) {
		handlerCalled = false
		authCtx := &identityHttpapi.AuthContext{
			UserID:        uuid.New(),
			SessionID:     uuid.New(),
			Email:         "sysadmin@example.com",
			FullName:      "Sys Admin",
			IsSystemAdmin: true,
			Status:        identityDomain.UserStatusActive,
		}

		req := httptest.NewRequest("POST", "/api/v1/organizations", nil)
		req = req.WithContext(identityHttpapi.WithAuthContext(req.Context(), authCtx))
		rec := httptest.NewRecorder()

		guard.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected 200 OK for active system admin, got: %d", rec.Code)
		}
		if !handlerCalled {
			t.Errorf("expected dummyHandler to be called")
		}
	})

	t.Run("system admin with inactive status rejected with 403 FORBIDDEN", func(t *testing.T) {
		handlerCalled = false
		authCtx := &identityHttpapi.AuthContext{
			UserID:        uuid.New(),
			SessionID:     uuid.New(),
			Email:         "sysadmin@example.com",
			FullName:      "Sys Admin",
			IsSystemAdmin: true,
			Status:        identityDomain.UserStatusInactive, // Inactive
		}

		req := httptest.NewRequest("POST", "/api/v1/organizations", nil)
		req = req.WithContext(identityHttpapi.WithAuthContext(req.Context(), authCtx))
		rec := httptest.NewRecorder()

		guard.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("expected 403 Forbidden for inactive system admin, got: %d", rec.Code)
		}
		if handlerCalled {
			t.Errorf("SECURITY FLAW: handler called on inactive system admin")
		}
	})

	t.Run("system admin with blocked status rejected with 403 FORBIDDEN", func(t *testing.T) {
		handlerCalled = false
		authCtx := &identityHttpapi.AuthContext{
			UserID:        uuid.New(),
			SessionID:     uuid.New(),
			Email:         "sysadmin@example.com",
			FullName:      "Sys Admin",
			IsSystemAdmin: true,
			Status:        identityDomain.UserStatusBlocked, // Blocked
		}

		req := httptest.NewRequest("POST", "/api/v1/organizations", nil)
		req = req.WithContext(identityHttpapi.WithAuthContext(req.Context(), authCtx))
		rec := httptest.NewRecorder()

		guard.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("expected 403 Forbidden for blocked system admin, got: %d", rec.Code)
		}
		if handlerCalled {
			t.Errorf("SECURITY FLAW: handler called on blocked system admin")
		}
	})

	t.Run("regular active user rejected from platform operation with 403 FORBIDDEN", func(t *testing.T) {
		handlerCalled = false
		authCtx := &identityHttpapi.AuthContext{
			UserID:        uuid.New(),
			SessionID:     uuid.New(),
			Email:         "user@example.com",
			FullName:      "Regular User",
			IsSystemAdmin: false, // Not system admin
			Status:        identityDomain.UserStatusActive,
		}

		req := httptest.NewRequest("POST", "/api/v1/organizations", nil)
		req = req.WithContext(identityHttpapi.WithAuthContext(req.Context(), authCtx))
		rec := httptest.NewRecorder()

		guard.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("expected 403 Forbidden for non-system admin, got: %d", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "FORBIDDEN") {
			t.Errorf("expected code FORBIDDEN, got: %s", rec.Body.String())
		}
		if handlerCalled {
			t.Errorf("SECURITY FLAW: handler called on non-system admin")
		}
	})

	t.Run("missing AuthContext fails closed with 401 UNAUTHORIZED", func(t *testing.T) {
		handlerCalled = false
		req := httptest.NewRequest("POST", "/api/v1/organizations", nil)
		rec := httptest.NewRecorder()

		guard.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("expected 401 Unauthorized when unauthenticated, got: %d", rec.Code)
		}
		if handlerCalled {
			t.Errorf("SECURITY FLAW: handler called on missing AuthContext")
		}
	})
}

func TestAuthorization_RoleDowngradeEnforcement(t *testing.T) {
	// Proves that when a user's role is downgraded in the database,
	// the very next request immediately reflects the downgraded permissions without requiring a new JWT.
	repo := newMockOrgMemberRepo()
	txManager := &mockTxManager{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	userID := uuid.New()
	orgID := uuid.New()

	// 1. User starts as active Admin in DB
	membershipKey := orgID.String() + ":" + userID.String()
	repo.memberships[membershipKey] = &orgRepo.UserMembershipWithOrg{
		OrganizationID:    orgID,
		OrganizationName:  "Target Org",
		Slug:              "target-org",
		IsDefaultInternal: false,
		Role:              orgDomain.RoleAdmin,
		Status:            orgDomain.MemberStatusActive,
	}

	authCtx := &identityHttpapi.AuthContext{
		UserID:        userID,
		SessionID:     uuid.New(),
		Email:         "user@example.com",
		FullName:      "User",
		IsSystemAdmin: false,
		Status:        identityDomain.UserStatusActive,
	}

	dummyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("SUCCESS"))
	})

	pipeline := NewOrganizationContextMiddleware(repo, txManager, logger)(
		RequirePermission(authz.PermissionCredentialWrite, logger)(dummyHandler),
	)

	// Request 1: User is Admin in DB -> credential:write succeeds
	req1 := httptest.NewRequest("POST", "/api/v1/credentials", nil)
	req1 = req1.WithContext(identityHttpapi.WithAuthContext(req1.Context(), authCtx))
	req1.Header.Set(HeaderXOrganizationID, orgID.String())
	rec1 := httptest.NewRecorder()

	pipeline.ServeHTTP(rec1, req1)

	if rec1.Code != http.StatusOK {
		t.Fatalf("expected request 1 as Admin to succeed (200), got: %d", rec1.Code)
	}

	// 2. Role is downgraded in DB to Viewer (no JWT change)
	repo.memberships[membershipKey].Role = orgDomain.RoleViewer

	// Request 2: Same user, same credentials -> credential:write immediately rejected with 403
	req2 := httptest.NewRequest("POST", "/api/v1/credentials", nil)
	req2 = req2.WithContext(identityHttpapi.WithAuthContext(req2.Context(), authCtx))
	req2.Header.Set(HeaderXOrganizationID, orgID.String())
	rec2 := httptest.NewRecorder()

	pipeline.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusForbidden {
		t.Fatalf("SECURITY FLAW: downgraded role was not immediately enforced from DB state, expected 403, got: %d", rec2.Code)
	}
	if !strings.Contains(rec2.Body.String(), "INSUFFICIENT_PERMISSIONS") {
		t.Errorf("expected code INSUFFICIENT_PERMISSIONS, got: %s", rec2.Body.String())
	}
}

func TestRequirePathOrganizationMatch(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	var handlerCalled bool
	dummyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("MATCHED"))
	})

	orgAID := uuid.New()
	orgBID := uuid.New()
	userID := uuid.New()

	guard := RequirePathOrganizationMatch("id", logger)(dummyHandler)

	t.Run("matching path ID and tenant context allows execution", func(t *testing.T) {
		handlerCalled = false
		tenantCtx := &TenantContext{
			UserID:            userID,
			OrganizationID:    orgAID,
			OrganizationName:  "Org A",
			OrganizationSlug:  "org-a",
			IsDefaultInternal: false,
			Role:              orgDomain.RoleAdmin,
			MembershipStatus:  orgDomain.MemberStatusActive,
		}

		req := httptest.NewRequest("GET", "/api/v1/organizations/"+orgAID.String(), nil)
		req.SetPathValue("id", orgAID.String())
		req = req.WithContext(WithTenantContext(req.Context(), tenantCtx))
		rec := httptest.NewRecorder()

		guard.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected 200 OK, got: %d", rec.Code)
		}
		if !handlerCalled {
			t.Errorf("expected dummyHandler to be called")
		}
	})

	t.Run("cross-tenant mismatch returns 404 ORGANIZATION_NOT_FOUND without leakage", func(t *testing.T) {
		handlerCalled = false
		tenantCtx := &TenantContext{
			UserID:            userID,
			OrganizationID:    orgAID, // User is member of Org A
			OrganizationName:  "Org A",
			OrganizationSlug:  "org-a",
			IsDefaultInternal: false,
			Role:              orgDomain.RoleAdmin,
			MembershipStatus:  orgDomain.MemberStatusActive,
		}

		// User attempts to query Org B via path
		req := httptest.NewRequest("GET", "/api/v1/organizations/"+orgBID.String(), nil)
		req.SetPathValue("id", orgBID.String())
		req = req.WithContext(WithTenantContext(req.Context(), tenantCtx))
		rec := httptest.NewRecorder()

		guard.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 Not Found for cross-tenant mismatch, got: %d", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "ORGANIZATION_NOT_FOUND") {
			t.Errorf("expected ORGANIZATION_NOT_FOUND code, got: %s", rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), orgAID.String()) || strings.Contains(rec.Body.String(), orgBID.String()) {
			t.Errorf("SECURITY FLAW: organization IDs leaked in error response: %s", rec.Body.String())
		}
		if handlerCalled {
			t.Errorf("SECURITY FLAW: handler executed on cross-tenant mismatch")
		}
	})

	t.Run("invalid path UUID returns 400 BAD_REQUEST", func(t *testing.T) {
		handlerCalled = false
		tenantCtx := &TenantContext{
			UserID:            userID,
			OrganizationID:    orgAID,
			OrganizationName:  "Org A",
			OrganizationSlug:  "org-a",
			IsDefaultInternal: false,
			Role:              orgDomain.RoleAdmin,
			MembershipStatus:  orgDomain.MemberStatusActive,
		}

		req := httptest.NewRequest("GET", "/api/v1/organizations/not-a-uuid", nil)
		req.SetPathValue("id", "not-a-uuid")
		req = req.WithContext(WithTenantContext(req.Context(), tenantCtx))
		rec := httptest.NewRecorder()

		guard.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400 Bad Request for invalid UUID, got: %d", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "BAD_REQUEST") {
			t.Errorf("expected BAD_REQUEST code, got: %s", rec.Body.String())
		}
		if handlerCalled {
			t.Errorf("handler should not be called")
		}
	})

	t.Run("missing tenant context fails closed with 404", func(t *testing.T) {
		handlerCalled = false
		req := httptest.NewRequest("GET", "/api/v1/organizations/"+orgAID.String(), nil)
		req.SetPathValue("id", orgAID.String())
		rec := httptest.NewRecorder()

		guard.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Errorf("expected 404 Not Found, got: %d", rec.Code)
		}
		if handlerCalled {
			t.Errorf("handler should not be called")
		}
	})
}

func TestRequireOrganizationAdmin(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	var handlerCalled bool
	dummyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ADMIN_OK"))
	})

	guard := RequireOrganizationAdmin(logger)(dummyHandler)
	orgID := uuid.New()
	userID := uuid.New()

	t.Run("admin role permitted", func(t *testing.T) {
		handlerCalled = false
		tenantCtx := &TenantContext{
			UserID:            userID,
			OrganizationID:    orgID,
			OrganizationName:  "Acme",
			OrganizationSlug:  "acme",
			IsDefaultInternal: false,
			Role:              orgDomain.RoleAdmin,
			MembershipStatus:  orgDomain.MemberStatusActive,
		}

		req := httptest.NewRequest("PUT", "/api/v1/organizations/"+orgID.String(), nil)
		req = req.WithContext(WithTenantContext(req.Context(), tenantCtx))
		rec := httptest.NewRecorder()

		guard.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected 200 OK for admin, got: %d", rec.Code)
		}
		if !handlerCalled {
			t.Errorf("expected handler to be called")
		}
	})

	t.Run("member role rejected with 403", func(t *testing.T) {
		handlerCalled = false
		tenantCtx := &TenantContext{
			UserID:            userID,
			OrganizationID:    orgID,
			OrganizationName:  "Acme",
			OrganizationSlug:  "acme",
			IsDefaultInternal: false,
			Role:              orgDomain.RoleMember,
			MembershipStatus:  orgDomain.MemberStatusActive,
		}

		req := httptest.NewRequest("PUT", "/api/v1/organizations/"+orgID.String(), nil)
		req = req.WithContext(WithTenantContext(req.Context(), tenantCtx))
		rec := httptest.NewRecorder()

		guard.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("expected 403 Forbidden for member, got: %d", rec.Code)
		}
		if handlerCalled {
			t.Errorf("handler should not be called for member")
		}
	})

	t.Run("viewer role rejected with 403", func(t *testing.T) {
		handlerCalled = false
		tenantCtx := &TenantContext{
			UserID:            userID,
			OrganizationID:    orgID,
			OrganizationName:  "Acme",
			OrganizationSlug:  "acme",
			IsDefaultInternal: false,
			Role:              orgDomain.RoleViewer,
			MembershipStatus:  orgDomain.MemberStatusActive,
		}

		req := httptest.NewRequest("PUT", "/api/v1/organizations/"+orgID.String(), nil)
		req = req.WithContext(WithTenantContext(req.Context(), tenantCtx))
		rec := httptest.NewRecorder()

		guard.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("expected 403 Forbidden for viewer, got: %d", rec.Code)
		}
		if handlerCalled {
			t.Errorf("handler should not be called for viewer")
		}
	})

	t.Run("inactive membership rejected with 403", func(t *testing.T) {
		handlerCalled = false
		tenantCtx := &TenantContext{
			UserID:            userID,
			OrganizationID:    orgID,
			OrganizationName:  "Acme",
			OrganizationSlug:  "acme",
			IsDefaultInternal: false,
			Role:              orgDomain.RoleAdmin,
			MembershipStatus:  orgDomain.MemberStatusSuspended,
		}

		req := httptest.NewRequest("PUT", "/api/v1/organizations/"+orgID.String(), nil)
		req = req.WithContext(WithTenantContext(req.Context(), tenantCtx))
		rec := httptest.NewRecorder()

		guard.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("expected 403 Forbidden for suspended membership, got: %d", rec.Code)
		}
		if handlerCalled {
			t.Errorf("handler should not be called for suspended membership")
		}
	})
}

func TestAuthorizeOrganizationUpdate(t *testing.T) {
	repo := newMockOrgMemberRepo()
	txManager := &mockTxManager{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	var handlerCalled bool
	dummyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("UPDATED"))
	})

	authWrapper := AuthorizeOrganizationUpdate(repo, txManager, logger)(dummyHandler)

	orgAID := uuid.New()
	orgBID := uuid.New()
	userAdminID := uuid.New()
	userMemberID := uuid.New()
	sysAdminID := uuid.New()

	// Seed active Admin membership in Org A
	repo.memberships[orgAID.String()+":"+userAdminID.String()] = &orgRepo.UserMembershipWithOrg{
		OrganizationID:     orgAID,
		OrganizationName:   "Org A",
		Slug:               "org-a",
		IsDefaultInternal:  false,
		OrganizationStatus: orgDomain.OrgStatusActive,
		Role:               orgDomain.RoleAdmin,
		Status:             orgDomain.MemberStatusActive,
	}

	// Seed active Member membership in Org A
	repo.memberships[orgAID.String()+":"+userMemberID.String()] = &orgRepo.UserMembershipWithOrg{
		OrganizationID:     orgAID,
		OrganizationName:   "Org A",
		Slug:               "org-a",
		IsDefaultInternal:  false,
		OrganizationStatus: orgDomain.OrgStatusActive,
		Role:               orgDomain.RoleMember,
		Status:             orgDomain.MemberStatusActive,
	}

	authCtxOrgAdmin := &identityHttpapi.AuthContext{
		UserID:        userAdminID,
		SessionID:     uuid.New(),
		Email:         "admin@org-a.com",
		FullName:      "Org Admin",
		IsSystemAdmin: false,
		Status:        identityDomain.UserStatusActive,
	}

	authCtxOrgMember := &identityHttpapi.AuthContext{
		UserID:        userMemberID,
		SessionID:     uuid.New(),
		Email:         "member@org-a.com",
		FullName:      "Org Member",
		IsSystemAdmin: false,
		Status:        identityDomain.UserStatusActive,
	}

	authCtxSysAdmin := &identityHttpapi.AuthContext{
		UserID:        sysAdminID,
		SessionID:     uuid.New(),
		Email:         "sysadmin@platform.local",
		FullName:      "System Admin",
		IsSystemAdmin: true,
		Status:        identityDomain.UserStatusActive,
	}

	t.Run("system admin can update organization without membership or X-Organization-ID", func(t *testing.T) {
		handlerCalled = false
		// Target is Org B (sysadmin has NO membership in Org B)
		req := httptest.NewRequest("PUT", "/api/v1/organizations/"+orgBID.String(), nil)
		req.SetPathValue("id", orgBID.String())
		req = req.WithContext(identityHttpapi.WithAuthContext(req.Context(), authCtxSysAdmin))
		rec := httptest.NewRecorder()

		authWrapper.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK for system admin bypass on PUT, got: %d (%s)", rec.Code, rec.Body.String())
		}
		if !handlerCalled {
			t.Errorf("expected dummyHandler to be called")
		}
	})

	t.Run("stale system admin JWT with DB status IsSystemAdmin=false fails closed", func(t *testing.T) {
		handlerCalled = false
		staleAuthCtx := &identityHttpapi.AuthContext{
			UserID:        sysAdminID,
			SessionID:     uuid.New(),
			Email:         "demoted@platform.local",
			FullName:      "Demoted Admin",
			IsSystemAdmin: false, // In DB demoted
			Status:        identityDomain.UserStatusActive,
		}

		// Attempts update on Org B where user has no membership
		req := httptest.NewRequest("PUT", "/api/v1/organizations/"+orgBID.String(), nil)
		req.SetPathValue("id", orgBID.String())
		req = req.WithContext(identityHttpapi.WithAuthContext(req.Context(), staleAuthCtx))
		req.Header.Set(HeaderXOrganizationID, orgBID.String())
		rec := httptest.NewRecorder()

		authWrapper.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Errorf("expected 404 Not Found for non-member demoted admin, got: %d", rec.Code)
		}
		if handlerCalled {
			t.Errorf("handler should not be called")
		}
	})

	t.Run("active org admin with matching header and path is authorized", func(t *testing.T) {
		handlerCalled = false
		req := httptest.NewRequest("PUT", "/api/v1/organizations/"+orgAID.String(), nil)
		req.SetPathValue("id", orgAID.String())
		req.Header.Set(HeaderXOrganizationID, orgAID.String())
		req = req.WithContext(identityHttpapi.WithAuthContext(req.Context(), authCtxOrgAdmin))
		rec := httptest.NewRecorder()

		authWrapper.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK for org admin, got: %d (%s)", rec.Code, rec.Body.String())
		}
		if !handlerCalled {
			t.Errorf("expected dummyHandler to be called")
		}
	})

	t.Run("active org member is rejected with 403", func(t *testing.T) {
		handlerCalled = false
		req := httptest.NewRequest("PUT", "/api/v1/organizations/"+orgAID.String(), nil)
		req.SetPathValue("id", orgAID.String())
		req.Header.Set(HeaderXOrganizationID, orgAID.String())
		req = req.WithContext(identityHttpapi.WithAuthContext(req.Context(), authCtxOrgMember))
		rec := httptest.NewRecorder()

		authWrapper.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 Forbidden for org member, got: %d", rec.Code)
		}
		if handlerCalled {
			t.Errorf("handler should not be called for member")
		}
	})

	t.Run("org admin cross-tenant path mismatch returns 404", func(t *testing.T) {
		handlerCalled = false
		// Header Org A, but path Org B
		req := httptest.NewRequest("PUT", "/api/v1/organizations/"+orgBID.String(), nil)
		req.SetPathValue("id", orgBID.String())
		req.Header.Set(HeaderXOrganizationID, orgAID.String())
		req = req.WithContext(identityHttpapi.WithAuthContext(req.Context(), authCtxOrgAdmin))
		rec := httptest.NewRecorder()

		authWrapper.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 Not Found for cross-tenant mismatch, got: %d", rec.Code)
		}
		if handlerCalled {
			t.Errorf("handler should not be called")
		}
	})

	t.Run("unauthenticated request returns 401", func(t *testing.T) {
		handlerCalled = false
		req := httptest.NewRequest("PUT", "/api/v1/organizations/"+orgAID.String(), nil)
		req.SetPathValue("id", orgAID.String())
		rec := httptest.NewRecorder()

		authWrapper.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("expected 401 Unauthorized, got: %d", rec.Code)
		}
		if handlerCalled {
			t.Errorf("handler should not be called")
		}
	})
}
