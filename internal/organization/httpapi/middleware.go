package httpapi

import (
	"log/slog"
	"net/http"
	"strings"

	identityHttpapi "backup-platform/internal/identity/httpapi"
	orgRepo "backup-platform/internal/organization/repository"
	"backup-platform/internal/platform/database"
	"backup-platform/internal/platform/httpapi"
	"backup-platform/internal/platform/logger"
	"backup-platform/pkg/uuid"
)

const HeaderXOrganizationID = "X-Organization-ID"

// NewOrganizationContextMiddleware creates middleware that validates X-Organization-ID and active membership in an active organization.
func NewOrganizationContextMiddleware(
	memberRepo orgRepo.MemberRepository,
	txManager database.TxManager,
	log *slog.Logger,
) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Ensure all responses are non-cacheable
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Pragma", "no-cache")

			// 1. Verify user authentication context exists (fails closed if authentication middleware did not run)
			authCtx, ok := identityHttpapi.AuthContextFromRequest(r)
			if !ok || authCtx == nil {
				httpapi.WriteError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required", nil)
				return
			}

			// 2. Strict X-Organization-ID header extraction and parsing
			headerValues := r.Header.Values(HeaderXOrganizationID)
			if len(headerValues) == 0 {
				httpapi.WriteError(w, r, http.StatusBadRequest, "INVALID_ORGANIZATION_CONTEXT", "valid X-Organization-ID header is required", nil)
				return
			}
			if len(headerValues) > 1 {
				httpapi.WriteError(w, r, http.StatusBadRequest, "INVALID_ORGANIZATION_CONTEXT", "valid X-Organization-ID header is required", nil)
				return
			}

			rawOrgID := strings.TrimSpace(headerValues[0])
			if rawOrgID == "" || strings.Contains(rawOrgID, ",") {
				httpapi.WriteError(w, r, http.StatusBadRequest, "INVALID_ORGANIZATION_CONTEXT", "valid X-Organization-ID header is required", nil)
				return
			}

			orgID, err := uuid.Parse(rawOrgID)
			if err != nil {
				httpapi.WriteError(w, r, http.StatusBadRequest, "INVALID_ORGANIZATION_CONTEXT", "valid X-Organization-ID header is required", nil)
				return
			}

			// 3. Resolve active membership in active organization from current authoritative database state
			q := txManager.Querier()
			activeMember, err := memberRepo.FindActiveMembershipWithOrg(r.Context(), q, orgID, authCtx.UserID)
			if err != nil {
				reqLogger := logger.FromContext(r.Context(), log)
				reqLogger.Error("organization context resolution database failure")
				httpapi.WriteError(w, r, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "service temporarily unavailable", nil)
				return
			}

			// Anti-enumeration: If org does not exist, is archived/suspended, or user membership does not exist or is inactive
			if activeMember == nil {
				httpapi.WriteError(w, r, http.StatusNotFound, "ORGANIZATION_NOT_FOUND", "organization not found", nil)
				return
			}

			// 4. Inject authoritative TenantContext into request context
			tenantCtx := &TenantContext{
				UserID:            authCtx.UserID,
				OrganizationID:    activeMember.OrganizationID,
				OrganizationName:  activeMember.OrganizationName,
				OrganizationSlug:  activeMember.Slug,
				IsDefaultInternal: activeMember.IsDefaultInternal,
				Role:              activeMember.Role,
				MembershipStatus:  activeMember.Status,
			}

			ctx := WithTenantContext(r.Context(), tenantCtx)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
