package httpapi

import (
	"context"
	"net/http"

	identityDomain "backup-platform/internal/identity/domain"
	"backup-platform/pkg/uuid"
)

type contextKey int

const authContextKey contextKey = 1

// AuthContext encapsulates the authenticated user and active session in the request context.
type AuthContext struct {
	UserID        uuid.UUID
	SessionID     uuid.UUID
	Email         string
	FullName      string
	IsSystemAdmin bool
	Status        identityDomain.UserStatus
}

// WithAuthContext attaches the AuthContext to the provided context.
func WithAuthContext(ctx context.Context, authCtx *AuthContext) context.Context {
	return context.WithValue(ctx, authContextKey, authCtx)
}

// AuthContextFromRequest extracts the AuthContext from the HTTP request context.
func AuthContextFromRequest(r *http.Request) (*AuthContext, bool) {
	if r == nil {
		return nil, false
	}
	val, ok := r.Context().Value(authContextKey).(*AuthContext)
	return val, ok && val != nil
}
