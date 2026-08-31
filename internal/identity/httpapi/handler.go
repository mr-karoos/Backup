package httpapi

import (
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	identityDomain "backup-platform/internal/identity/domain"
	identityService "backup-platform/internal/identity/service"
	"backup-platform/internal/organization/authz"
	orgRepo "backup-platform/internal/organization/repository"
	"backup-platform/internal/platform/database"
	"backup-platform/internal/platform/httpapi"
	"backup-platform/internal/platform/logger"
	"backup-platform/pkg/uuid"
)

const (
	RefreshTokenCookieName  = "refresh_token"
	RefreshTokenCookiePath  = "/api/v1/auth"
	RefreshTokenMaxAge      = 7 * 24 * 3600 // 7 days in seconds
	DefaultTokenLifetimeSec = 900           // 15 minutes
	MaxEmailLength          = 255
)

// LoginRequest defines the expected JSON payload for login.
type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// TokenBundle represents the standard issued access token structure matching API_DESIGN.
type TokenBundle struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int64  `json:"expires_in"`
}

// LoginUserResponse represents the safe public representation of a user upon login matching API_DESIGN.
type LoginUserResponse struct {
	ID            uuid.UUID `json:"id"`
	Email         string    `json:"email"`
	FullName      string    `json:"full_name"`
	IsSystemAdmin bool      `json:"is_system_admin"`
	CreatedAt     time.Time `json:"created_at"`
}

// LoginResponse defines the canonical data payload returned upon successful login.
type LoginResponse struct {
	User         LoginUserResponse `json:"user"`
	Tokens       TokenBundle       `json:"tokens"`
	DefaultOrgID *uuid.UUID        `json:"default_organization_id"`
}

// RefreshResponse defines the payload returned upon successful token refresh matching API_DESIGN.
type RefreshResponse struct {
	Tokens TokenBundle `json:"tokens"`
}

// MeMembershipResponse describes an organization membership and associated static permissions matching API_DESIGN.
type MeMembershipResponse struct {
	OrganizationID    uuid.UUID `json:"organization_id"`
	OrganizationName  string    `json:"organization_name"`
	OrganizationSlug  string    `json:"organization_slug"`
	IsDefaultInternal bool      `json:"is_default_internal"`
	Role              string    `json:"role"`
	Status            string    `json:"status"`
	Permissions       []string  `json:"permissions"`
}

// MeUserResponse represents the user profile in /auth/me matching API_DESIGN.
type MeUserResponse struct {
	ID            uuid.UUID                 `json:"id"`
	Email         string                    `json:"email"`
	FullName      string                    `json:"full_name"`
	IsSystemAdmin bool                      `json:"is_system_admin"`
	Status        identityDomain.UserStatus `json:"status"`
}

// MeResponse describes the current authenticated user profile and their active memberships.
type MeResponse struct {
	User        MeUserResponse         `json:"user"`
	Memberships []MeMembershipResponse `json:"memberships"`
}

// Handler provides HTTP endpoints for identity and authentication workflows.
type Handler struct {
	authService  *identityService.AuthService
	memberRepo   orgRepo.MemberRepository
	txManager    database.TxManager
	rateLimiter  *RateLimiter
	cookieSecure bool
	logger       *slog.Logger
}

// NewHandler constructs a new authentication HTTP Handler.
func NewHandler(
	authService *identityService.AuthService,
	memberRepo orgRepo.MemberRepository,
	txManager database.TxManager,
	rateLimiter *RateLimiter,
	cookieSecure bool,
	logger *slog.Logger,
) *Handler {
	return &Handler{
		authService:  authService,
		memberRepo:   memberRepo,
		txManager:    txManager,
		rateLimiter:  rateLimiter,
		cookieSecure: cookieSecure,
		logger:       logger,
	}
}

// Login handles POST /api/v1/auth/login.
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	// Set security caching headers on all responses
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")

	if r.Method != http.MethodPost {
		httpapi.WriteError(w, r, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "method not allowed", nil)
		return
	}

	// Strict Content-Type enforcement (must be application/json)
	ct := r.Header.Get("Content-Type")
	mediaType, _, err := mime.ParseMediaType(ct)
	if err != nil || mediaType != "application/json" {
		httpapi.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "Content-Type must be application/json", nil)
		return
	}

	clientIP := extractClientIP(r)

	var req LoginRequest
	if err := httpapi.DecodeJSON(w, r, &req); err != nil {
		if errors.Is(err, httpapi.ErrBodyTooLarge) {
			httpapi.WriteError(w, r, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE", "request payload exceeds 64 KiB limit", nil)
			return
		}
		httpapi.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "malformed or invalid JSON payload", nil)
		return
	}

	email := strings.ToLower(strings.TrimSpace(req.Email))

	// Structural email and password length validation (prevents high-length inputs hitting DB/RateLimiter)
	if email == "" || len(email) > MaxEmailLength || req.Password == "" {
		httpapi.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "invalid login request", nil)
		return
	}

	// Rate limiting check
	if h.rateLimiter != nil {
		allowed, retryAfter := h.rateLimiter.AllowAndRecord(clientIP, email)
		if !allowed {
			w.Header().Set("Retry-After", fmt.Sprintf("%d", int(retryAfter.Seconds())+1))
			httpapi.WriteError(w, r, http.StatusTooManyRequests, "RATE_LIMIT_EXCEEDED", "too many login attempts, please try again later", nil)
			return
		}
	}

	ua := sanitizeUserAgent(r.UserAgent())

	meta := identityService.ClientMetadata{
		IPAddress: &clientIP,
		UserAgent: &ua,
	}

	authResult, err := h.authService.Login(r.Context(), email, req.Password, meta)
	if err != nil {
		if errors.Is(err, identityDomain.ErrInvalidCredentials) {
			httpapi.WriteError(w, r, http.StatusUnauthorized, "INVALID_CREDENTIALS", "invalid email or password", nil)
			return
		}
		reqLogger := logger.FromContext(r.Context(), h.logger)
		reqLogger.Error("login internal service failure")
		httpapi.WriteError(w, r, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "authentication service temporarily unavailable", nil)
		return
	}

	// Set HttpOnly refresh token cookie matching session expiry
	setRefreshCookie(w, authResult.RawRefreshToken, authResult.RefreshTokenExpires, h.cookieSecure, time.Now().UTC())

	resp := LoginResponse{
		User: LoginUserResponse{
			ID:            authResult.User.ID,
			Email:         authResult.User.Email,
			FullName:      authResult.User.FullName,
			IsSystemAdmin: authResult.User.IsSystemAdmin,
			CreatedAt:     authResult.User.CreatedAt,
		},
		Tokens: TokenBundle{
			AccessToken: authResult.AccessToken,
			TokenType:   "Bearer",
			ExpiresIn:   DefaultTokenLifetimeSec,
		},
		DefaultOrgID: authResult.DefaultOrgID,
	}

	httpapi.WriteJSON(w, r, http.StatusOK, resp, "login successful")
}

// Refresh handles POST /api/v1/auth/refresh.
func (h *Handler) Refresh(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")

	if r.Method != http.MethodPost {
		httpapi.WriteError(w, r, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "method not allowed", nil)
		return
	}

	cookie, err := r.Cookie(RefreshTokenCookieName)
	if err != nil || cookie == nil || strings.TrimSpace(cookie.Value) == "" {
		clearRefreshCookie(w, h.cookieSecure)
		httpapi.WriteError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required", nil)
		return
	}

	rawRefreshToken := strings.TrimSpace(cookie.Value)

	refreshResult, err := h.authService.Refresh(r.Context(), rawRefreshToken)
	if err != nil {
		if errors.Is(err, identityDomain.ErrInvalidSession) || errors.Is(err, identityDomain.ErrInvalidRefreshToken) {
			clearRefreshCookie(w, h.cookieSecure)
			httpapi.WriteError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required", nil)
			return
		}
		reqLogger := logger.FromContext(r.Context(), h.logger)
		reqLogger.Error("refresh internal service failure")
		httpapi.WriteError(w, r, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "authentication service temporarily unavailable", nil)
		return
	}

	// Set newly rotated HttpOnly refresh cookie matching the unchanged session expiry
	setRefreshCookie(w, refreshResult.RawRefreshToken, refreshResult.RefreshTokenExpires, h.cookieSecure, time.Now().UTC())

	resp := RefreshResponse{
		Tokens: TokenBundle{
			AccessToken: refreshResult.AccessToken,
			TokenType:   "Bearer",
			ExpiresIn:   DefaultTokenLifetimeSec,
		},
	}

	httpapi.WriteJSON(w, r, http.StatusOK, resp, "token refreshed successfully")
}

// Logout handles POST /api/v1/auth/logout.
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")

	if r.Method != http.MethodPost {
		httpapi.WriteError(w, r, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "method not allowed", nil)
		return
	}

	authCtx, ok := AuthContextFromRequest(r)
	if !ok || authCtx == nil {
		httpapi.WriteError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required", nil)
		return
	}

	// Revoke current session in database
	if err := h.authService.RevokeSession(r.Context(), authCtx.SessionID); err != nil {
		// Best-effort clear cookie on failure, but report 503 error
		clearRefreshCookie(w, h.cookieSecure)
		reqLogger := logger.FromContext(r.Context(), h.logger)
		reqLogger.Error("logout revocation failure")
		httpapi.WriteError(w, r, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "failed to revoke session", nil)
		return
	}

	// Clear HttpOnly refresh cookie on success
	clearRefreshCookie(w, h.cookieSecure)

	httpapi.WriteJSON(w, r, http.StatusOK, nil, "logout successful")
}

// Me handles GET /api/v1/auth/me.
func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")

	if r.Method != http.MethodGet {
		httpapi.WriteError(w, r, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "method not allowed", nil)
		return
	}

	authCtx, ok := AuthContextFromRequest(r)
	if !ok || authCtx == nil {
		httpapi.WriteError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required", nil)
		return
	}

	q := h.txManager.Querier()
	memberships, err := h.memberRepo.ListUserMembershipsWithOrg(r.Context(), q, authCtx.UserID)
	if err != nil {
		reqLogger := logger.FromContext(r.Context(), h.logger)
		reqLogger.Error("failed to query user memberships for /me endpoint")
		httpapi.WriteError(w, r, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "service temporarily unavailable", nil)
		return
	}

	membershipsResp := make([]MeMembershipResponse, 0, len(memberships))
	for _, m := range memberships {
		perms := authz.PermissionStringsForRole(m.Role)
		membershipsResp = append(membershipsResp, MeMembershipResponse{
			OrganizationID:    m.OrganizationID,
			OrganizationName:  m.OrganizationName,
			OrganizationSlug:  m.Slug,
			IsDefaultInternal: m.IsDefaultInternal,
			Role:              string(m.Role),
			Status:            string(m.Status),
			Permissions:       perms,
		})
	}

	resp := MeResponse{
		User: MeUserResponse{
			ID:            authCtx.UserID,
			Email:         authCtx.Email,
			FullName:      authCtx.FullName,
			IsSystemAdmin: authCtx.IsSystemAdmin,
			Status:        authCtx.Status,
		},
		Memberships: membershipsResp,
	}

	httpapi.WriteJSON(w, r, http.StatusOK, resp, "current user profile")
}

func extractClientIP(r *http.Request) string {
	remoteAddr := strings.TrimSpace(r.RemoteAddr)
	if host, _, err := net.SplitHostPort(remoteAddr); err == nil {
		return host
	}
	return remoteAddr
}

func sanitizeUserAgent(ua string) string {
	if !utf8.ValidString(ua) {
		ua = strings.ToValidUTF8(ua, "")
	}
	runes := []rune(ua)
	if len(runes) > 512 {
		return string(runes[:512])
	}
	return ua
}

// CalculateRefreshCookieMaxAge computes the remaining cookie lifetime in seconds without extending session expiration.
func CalculateRefreshCookieMaxAge(now, expiresAt time.Time) int {
	if expiresAt.Before(now) || expiresAt.Equal(now) {
		return -1
	}
	remaining := int(expiresAt.Sub(now).Seconds())
	if remaining > RefreshTokenMaxAge {
		remaining = RefreshTokenMaxAge
	}
	if remaining <= 0 {
		return -1
	}
	return remaining
}

func setRefreshCookie(w http.ResponseWriter, rawToken string, expiresAt time.Time, secure bool, now time.Time) {
	maxAge := CalculateRefreshCookieMaxAge(now, expiresAt)
	if maxAge <= 0 {
		clearRefreshCookie(w, secure)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     RefreshTokenCookieName,
		Value:    rawToken,
		Path:     RefreshTokenCookiePath,
		MaxAge:   maxAge,
		Expires:  expiresAt,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
	})
}

func clearRefreshCookie(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     RefreshTokenCookieName,
		Value:    "",
		Path:     RefreshTokenCookiePath,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
	})
}
