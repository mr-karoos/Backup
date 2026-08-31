package httpapi

import (
	"context"
	"net/http"

	orgDomain "backup-platform/internal/organization/domain"
	"backup-platform/pkg/uuid"
)

type tenantContextKey struct{}

var tenantCtxKey = tenantContextKey{}

// TenantContext encapsulates the active organization and user membership details for an authenticated tenant-scoped request.
type TenantContext struct {
	UserID            uuid.UUID
	OrganizationID    uuid.UUID
	OrganizationName  string
	OrganizationSlug  string
	IsDefaultInternal bool
	Role              orgDomain.Role
	MembershipStatus  orgDomain.MemberStatus
}

// WithTenantContext attaches a TenantContext to the given context.
func WithTenantContext(ctx context.Context, tenantCtx *TenantContext) context.Context {
	return context.WithValue(ctx, tenantCtxKey, tenantCtx)
}

// TenantContextFromContext extracts the TenantContext from the context.
func TenantContextFromContext(ctx context.Context) (*TenantContext, bool) {
	if ctx == nil {
		return nil, false
	}
	tc, ok := ctx.Value(tenantCtxKey).(*TenantContext)
	return tc, ok && tc != nil
}

// TenantContextFromRequest extracts the TenantContext from the HTTP request context.
func TenantContextFromRequest(r *http.Request) (*TenantContext, bool) {
	if r == nil {
		return nil, false
	}
	return TenantContextFromContext(r.Context())
}
