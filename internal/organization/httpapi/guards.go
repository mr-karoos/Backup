package httpapi

import (
	"log/slog"
	"net/http"

	identityDomain "backup-platform/internal/identity/domain"
	identityHttpapi "backup-platform/internal/identity/httpapi"
	"backup-platform/internal/organization/authz"
	orgDomain "backup-platform/internal/organization/domain"
	orgRepo "backup-platform/internal/organization/repository"
	"backup-platform/internal/platform/database"
	"backup-platform/internal/platform/httpapi"
	"backup-platform/internal/platform/logger"
	"backup-platform/pkg/uuid"
)

// RequirePermission ensures the authenticated user's current organization role possesses the specified permission.
func RequirePermission(permission authz.Permission, log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Pragma", "no-cache")

			tenantCtx, ok := TenantContextFromRequest(r)
			if !ok || tenantCtx == nil ||
				tenantCtx.UserID == uuid.Nil ||
				tenantCtx.OrganizationID == uuid.Nil ||
				tenantCtx.MembershipStatus != orgDomain.MemberStatusActive {
				reqLogger := logger.FromContext(r.Context(), log)
				reqLogger.Warn("permission guard executed without valid or active tenant context")
				httpapi.WriteError(w, r, http.StatusForbidden, "FORBIDDEN", "access denied", nil)
				return
			}

			if !authz.HasPermission(tenantCtx.Role, permission) {
				httpapi.WriteError(w, r, http.StatusForbidden, "INSUFFICIENT_PERMISSIONS", "insufficient permissions for this operation", nil)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// RequireSystemAdmin ensures the authenticated user holds platform-level system administrator privileges and is active.
func RequireSystemAdmin(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Pragma", "no-cache")

			authCtx, ok := identityHttpapi.AuthContextFromRequest(r)
			if !ok || authCtx == nil {
				httpapi.WriteError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required", nil)
				return
			}

			if !authCtx.IsSystemAdmin || authCtx.Status != identityDomain.UserStatusActive {
				httpapi.WriteError(w, r, http.StatusForbidden, "FORBIDDEN", "system administrator privileges required", nil)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// RequireOrganizationAdmin ensures the authenticated user is an active administrator of the tenant organization.
func RequireOrganizationAdmin(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Pragma", "no-cache")

			tenantCtx, ok := TenantContextFromRequest(r)
			if !ok || tenantCtx == nil ||
				tenantCtx.UserID == uuid.Nil ||
				tenantCtx.OrganizationID == uuid.Nil ||
				tenantCtx.MembershipStatus != orgDomain.MemberStatusActive {
				reqLogger := logger.FromContext(r.Context(), log)
				reqLogger.Warn("organization admin guard executed without valid active tenant context")
				httpapi.WriteError(w, r, http.StatusForbidden, "FORBIDDEN", "access denied", nil)
				return
			}

			if tenantCtx.Role != orgDomain.RoleAdmin {
				httpapi.WriteError(w, r, http.StatusForbidden, "FORBIDDEN", "organization administrator privileges required", nil)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// RequirePathOrganizationMatch ensures the route's path parameter matches the resolved TenantContext organization ID.
func RequirePathOrganizationMatch(pathParamName string, log *slog.Logger) func(http.Handler) http.Handler {
	if pathParamName == "" {
		pathParamName = "id"
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Pragma", "no-cache")

			pathVal := r.PathValue(pathParamName)
			pathOrgID, err := uuid.Parse(pathVal)
			if err != nil || pathOrgID == uuid.Nil {
				httpapi.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "invalid organization identifier", nil)
				return
			}

			tenantCtx, ok := TenantContextFromRequest(r)
			if !ok || tenantCtx == nil || tenantCtx.OrganizationID == uuid.Nil || tenantCtx.UserID == uuid.Nil {
				reqLogger := logger.FromContext(r.Context(), log)
				reqLogger.Warn("path match guard executed without valid tenant context")
				httpapi.WriteError(w, r, http.StatusNotFound, "ORGANIZATION_NOT_FOUND", "organization not found", nil)
				return
			}

			if pathOrgID != tenantCtx.OrganizationID {
				reqLogger := logger.FromContext(r.Context(), log)
				reqLogger.Warn("cross-tenant path mismatch detected")
				httpapi.WriteError(w, r, http.StatusNotFound, "ORGANIZATION_NOT_FOUND", "organization not found", nil)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// AuthorizeOrganizationUpdate authorizes PUT /organizations/{id} via either Active System Admin or Active Org Admin.
func AuthorizeOrganizationUpdate(memberRepo orgRepo.MemberRepository, txManager database.TxManager, log *slog.Logger) func(http.Handler) http.Handler {
	orgCtxMw := NewOrganizationContextMiddleware(memberRepo, txManager, log)
	pathMatchGuard := RequirePathOrganizationMatch("id", log)
	orgAdminGuard := RequireOrganizationAdmin(log)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Pragma", "no-cache")

			authCtx, ok := identityHttpapi.AuthContextFromRequest(r)
			if !ok || authCtx == nil {
				httpapi.WriteError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required", nil)
				return
			}

			// Path 1: System Admin (DB-backed active status) directly authorized to update any active organization via Path ID
			if authCtx.IsSystemAdmin && authCtx.Status == identityDomain.UserStatusActive {
				next.ServeHTTP(w, r)
				return
			}

			// Path 2: Regular user must be an active Organization Admin with valid matching Tenant Context
			orgCtxMw(pathMatchGuard(orgAdminGuard(next))).ServeHTTP(w, r)
		})
	}
}
