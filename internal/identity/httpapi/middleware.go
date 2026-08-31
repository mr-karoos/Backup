package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	identityDomain "backup-platform/internal/identity/domain"
	identityService "backup-platform/internal/identity/service"
	"backup-platform/internal/platform/httpapi"
	"backup-platform/internal/platform/logger"
)

// NewAuthMiddleware constructs an HTTP middleware for access token verification and active DB session validation.
func NewAuthMiddleware(
	jwtService identityService.TokenService,
	authService *identityService.AuthService,
	log *slog.Logger,
) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Ensure all auth middleware responses are strictly non-cacheable
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Pragma", "no-cache")

			authHeader := strings.TrimSpace(r.Header.Get("Authorization"))
			if authHeader == "" {
				httpapi.WriteError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required", nil)
				return
			}

			// Robust parsing: exactly 2 fields, scheme case-insensitively 'Bearer', non-empty token
			fields := strings.Fields(authHeader)
			if len(fields) != 2 || !strings.EqualFold(fields[0], "Bearer") || fields[1] == "" {
				httpapi.WriteError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required", nil)
				return
			}

			tokenStr := fields[1]

			// 1. Validate JWT cryptographic signature and claims
			payload, err := jwtService.ValidateAccessToken(tokenStr)
			if err != nil {
				httpapi.WriteError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required", nil)
				return
			}

			// 2. Validate current state in database (DB is authoritative for session validity and current user privileges)
			sessResult, err := authService.ValidateAuthenticatedSession(r.Context(), payload.UserID, payload.SessionID)
			if err != nil {
				if errors.Is(err, identityDomain.ErrInvalidSession) {
					httpapi.WriteError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required", nil)
					return
				}
				reqLogger := logger.FromContext(r.Context(), log)
				reqLogger.Error("authentication middleware service failure")
				httpapi.WriteError(w, r, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "authentication service temporarily unavailable", nil)
				return
			}

			// 3. Inject authoritative DB state into request context (do not blindly trust JWT claims for privileges)
			authCtx := &AuthContext{
				UserID:        sessResult.User.ID,
				SessionID:     sessResult.SessionID,
				Email:         sessResult.User.Email,
				FullName:      sessResult.User.FullName,
				IsSystemAdmin: sessResult.User.IsSystemAdmin,
				Status:        sessResult.User.Status,
			}

			ctx := WithAuthContext(r.Context(), authCtx)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
