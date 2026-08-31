package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	identityDomain "backup-platform/internal/identity/domain"
	identityService "backup-platform/internal/identity/service"
	orgDomain "backup-platform/internal/organization/domain"
	orgRepo "backup-platform/internal/organization/repository"
	"backup-platform/internal/platform/database"
	"backup-platform/internal/platform/httpapi"
	"backup-platform/pkg/uuid"
)

type mockOrgMemberRepo struct {
	memberships []*orgRepo.UserMembershipWithOrg
	overrideErr error
}

func (r *mockOrgMemberRepo) Create(ctx context.Context, q database.Querier, member *orgDomain.Member) error {
	return nil
}
func (r *mockOrgMemberRepo) FindMembership(ctx context.Context, q database.Querier, orgID, userID uuid.UUID) (*orgDomain.Member, error) {
	return nil, nil
}
func (r *mockOrgMemberRepo) ListUserOrganizations(ctx context.Context, q database.Querier, userID uuid.UUID) ([]*orgDomain.Organization, error) {
	return nil, nil
}
func (r *mockOrgMemberRepo) ListUserMembershipsWithOrg(ctx context.Context, q database.Querier, userID uuid.UUID) ([]*orgRepo.UserMembershipWithOrg, error) {
	if r.overrideErr != nil {
		return nil, r.overrideErr
	}
	return r.memberships, nil
}
func (r *mockOrgMemberRepo) FindActiveMembershipWithOrg(ctx context.Context, q database.Querier, orgID, userID uuid.UUID) (*orgRepo.UserMembershipWithOrg, error) {
	if r.overrideErr != nil {
		return nil, r.overrideErr
	}
	for _, m := range r.memberships {
		if m.OrganizationID == orgID {
			return m, nil
		}
	}
	return nil, nil
}

func setupHandlerTest(t *testing.T, cookieSecure bool) (*Handler, *identityService.AuthService, *mockUserRepo, *mockSessionRepo, *mockOrgMemberRepo, identityService.TokenService) {
	jwtKey := []byte("test-secret-key-at-least-32-bytes-long!")
	jwtService, err := identityService.NewJWTService(jwtKey)
	if err != nil {
		t.Fatalf("failed to init JWT service: %v", err)
	}

	userRepo := &mockUserRepo{users: make(map[uuid.UUID]*identityDomain.User)}
	sessionRepo := &mockSessionRepo{sessions: make(map[uuid.UUID]*identityDomain.Session)}
	memberRepo := &mockOrgMemberRepo{}
	hasher := identityService.NewDefaultArgon2idHasher()
	tokenGen := identityService.NewSecureTokenGenerator()
	txManager := &mockAuthTxManager{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	authService := identityService.NewAuthService(
		userRepo,
		sessionRepo,
		memberRepo,
		hasher,
		tokenGen,
		jwtService,
		txManager,
		logger,
	)

	rateLimiter := NewRateLimiter(nil)
	handler := NewHandler(
		authService,
		memberRepo,
		txManager,
		rateLimiter,
		cookieSecure,
		logger,
	)

	return handler, authService, userRepo, sessionRepo, memberRepo, jwtService
}

func TestCalculateRefreshCookieMaxAge(t *testing.T) {
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)

	t.Run("session expiry exactly 7 days", func(t *testing.T) {
		expiresAt := now.Add(7 * 24 * time.Hour)
		maxAge := CalculateRefreshCookieMaxAge(now, expiresAt)
		expected := 7 * 24 * 3600
		if maxAge != expected {
			t.Errorf("expected maxAge %d, got %d", expected, maxAge)
		}
	})

	t.Run("session expiry 2 days remaining", func(t *testing.T) {
		expiresAt := now.Add(2 * 24 * time.Hour)
		maxAge := CalculateRefreshCookieMaxAge(now, expiresAt)
		expected := 2 * 24 * 3600
		if maxAge != expected {
			t.Errorf("expected maxAge %d, got %d", expected, maxAge)
		}
	})

	t.Run("session expiry 30 minutes remaining", func(t *testing.T) {
		expiresAt := now.Add(30 * time.Minute)
		maxAge := CalculateRefreshCookieMaxAge(now, expiresAt)
		expected := 30 * 60
		if maxAge != expected {
			t.Errorf("expected maxAge %d, got %d", expected, maxAge)
		}
	})

	t.Run("session expired in past", func(t *testing.T) {
		expiresAt := now.Add(-1 * time.Minute)
		maxAge := CalculateRefreshCookieMaxAge(now, expiresAt)
		if maxAge != -1 {
			t.Errorf("expected maxAge -1 for expired session, got %d", maxAge)
		}
	})

	t.Run("session expiry equal to now", func(t *testing.T) {
		maxAge := CalculateRefreshCookieMaxAge(now, now)
		if maxAge != -1 {
			t.Errorf("expected maxAge -1 for session expiring right now, got %d", maxAge)
		}
	})

	t.Run("session expiry capped at 7 days", func(t *testing.T) {
		expiresAt := now.Add(10 * 24 * time.Hour)
		maxAge := CalculateRefreshCookieMaxAge(now, expiresAt)
		expected := 7 * 24 * 3600
		if maxAge != expected {
			t.Errorf("expected maxAge capped at %d, got %d", expected, maxAge)
		}
	})
}

func TestHandler_Login(t *testing.T) {
	handler, _, userRepo, _, _, _ := setupHandlerTest(t, false)
	hasher := identityService.NewDefaultArgon2idHasher()

	password := "SecretPassword123!"
	pwdHash, _ := hasher.Hash(password)
	user, _ := identityDomain.NewUser("admin@example.com", pwdHash, "Admin User", true)
	_ = userRepo.Create(context.Background(), nil, user)

	t.Run("successful login with standard Content-Type", func(t *testing.T) {
		body := `{"email":"admin@example.com","password":"SecretPassword123!"}`
		req := httptest.NewRequest("POST", "/api/v1/auth/login", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
		rec := httptest.NewRecorder()

		handler.Login(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got: %d (%s)", rec.Code, rec.Body.String())
		}

		if rec.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("expected Cache-Control: no-store")
		}
		if rec.Header().Get("Pragma") != "no-cache" {
			t.Errorf("expected Pragma: no-cache")
		}

		// Verify cookie
		cookies := rec.Result().Cookies()
		var refreshCookie *http.Cookie
		for _, c := range cookies {
			if c.Name == RefreshTokenCookieName {
				refreshCookie = c
				break
			}
		}
		if refreshCookie == nil {
			t.Fatal("expected refresh_token cookie to be set")
		}
		if !refreshCookie.HttpOnly {
			t.Errorf("expected HttpOnly = true")
		}
		if refreshCookie.SameSite != http.SameSiteStrictMode {
			t.Errorf("expected SameSite = Strict")
		}
		if refreshCookie.Path != RefreshTokenCookiePath {
			t.Errorf("expected Path = /api/v1/auth, got: %s", refreshCookie.Path)
		}
		if refreshCookie.MaxAge <= 0 || refreshCookie.MaxAge > RefreshTokenMaxAge {
			t.Errorf("expected valid MaxAge <= 7 days, got: %d", refreshCookie.MaxAge)
		}

		// Verify canonical response body structure
		var env httpapi.ResponseEnvelope
		_ = json.Unmarshal(rec.Body.Bytes(), &env)
		dataBytes, _ := json.Marshal(env.Data)
		var loginResp LoginResponse
		_ = json.Unmarshal(dataBytes, &loginResp)

		if loginResp.Tokens.AccessToken == "" {
			t.Errorf("expected non-empty tokens.access_token")
		}
		if loginResp.Tokens.TokenType != "Bearer" {
			t.Errorf("expected tokens.token_type Bearer")
		}
		if loginResp.Tokens.ExpiresIn != 900 {
			t.Errorf("expected tokens.expires_in 900")
		}
		if loginResp.User.Email != "admin@example.com" {
			t.Errorf("expected user email match")
		}
		if loginResp.User.CreatedAt.IsZero() {
			t.Errorf("expected non-zero created_at in user response")
		}

		// Raw Refresh Token must NOT be in JSON
		if strings.Contains(rec.Body.String(), refreshCookie.Value) {
			t.Errorf("SECURITY FLAW: raw refresh token leaked in JSON response body!")
		}
	})

	t.Run("missing Content-Type returns 400", func(t *testing.T) {
		body := `{"email":"admin@example.com","password":"SecretPassword123!"}`
		req := httptest.NewRequest("POST", "/api/v1/auth/login", strings.NewReader(body))
		rec := httptest.NewRecorder()

		handler.Login(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400 for missing Content-Type, got: %d", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "BAD_REQUEST") {
			t.Errorf("expected BAD_REQUEST error code")
		}
	})

	t.Run("wrong Content-Type text/plain returns 400", func(t *testing.T) {
		body := `{"email":"admin@example.com","password":"SecretPassword123!"}`
		req := httptest.NewRequest("POST", "/api/v1/auth/login", strings.NewReader(body))
		req.Header.Set("Content-Type", "text/plain")
		rec := httptest.NewRecorder()

		handler.Login(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400 for text/plain Content-Type, got: %d", rec.Code)
		}
	})

	t.Run("oversized email returns 400 without rate limit entry", func(t *testing.T) {
		oversizedEmail := strings.Repeat("a", 250) + "@example.com" // > 255 chars
		body := `{"email":"` + oversizedEmail + `","password":"SecretPassword123!"}`
		req := httptest.NewRequest("POST", "/api/v1/auth/login", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		handler.Login(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400 for oversized email, got: %d", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "invalid login request") {
			t.Errorf("expected generic message 'invalid login request'")
		}
		// Must not echo raw oversized email in response
		if strings.Contains(rec.Body.String(), oversizedEmail) {
			t.Errorf("response must not echo raw oversized email")
		}
		// Rate limiter stores must not track this oversized email
		oversizedHash := sha256.Sum256([]byte(oversizedEmail))
		if _, exists := handler.rateLimiter.emailStore[oversizedHash]; exists {
			t.Errorf("rate limiter should not track invalid oversized email")
		}
	})

	t.Run("empty email returns 400", func(t *testing.T) {
		body := `{"email":"","password":"SecretPassword123!"}`
		req := httptest.NewRequest("POST", "/api/v1/auth/login", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		handler.Login(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400 for empty email, got: %d", rec.Code)
		}
	})

	t.Run("empty password returns 400", func(t *testing.T) {
		body := `{"email":"admin@example.com","password":""}`
		req := httptest.NewRequest("POST", "/api/v1/auth/login", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		handler.Login(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400 for empty password, got: %d", rec.Code)
		}
	})

	t.Run("invalid credentials returns 401", func(t *testing.T) {
		body := `{"email":"admin@example.com","password":"WrongPassword!"}`
		req := httptest.NewRequest("POST", "/api/v1/auth/login", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		handler.Login(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("expected 401, got: %d", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "INVALID_CREDENTIALS") {
			t.Errorf("expected error code INVALID_CREDENTIALS, got: %s", rec.Body.String())
		}
	})

	t.Run("unknown fields in JSON returns 400", func(t *testing.T) {
		body := `{"email":"admin@example.com","password":"pass","unexpected_field":true}`
		req := httptest.NewRequest("POST", "/api/v1/auth/login", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		handler.Login(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400 for unknown fields, got: %d", rec.Code)
		}
	})

	t.Run("oversized body returns 413", func(t *testing.T) {
		largeVal := strings.Repeat("x", 65*1024)
		body := `{"email":"admin@example.com","password":"` + largeVal + `"}`
		req := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader([]byte(body)))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		handler.Login(rec, req)

		if rec.Code != http.StatusRequestEntityTooLarge {
			t.Errorf("expected 413 for oversized body, got: %d", rec.Code)
		}
	})
}

func TestHandler_Refresh(t *testing.T) {
	handler, authService, userRepo, _, _, _ := setupHandlerTest(t, true)
	hasher := identityService.NewDefaultArgon2idHasher()

	password := "SecretPassword123!"
	pwdHash, _ := hasher.Hash(password)
	user, _ := identityDomain.NewUser("admin@example.com", pwdHash, "Admin User", true)
	_ = userRepo.Create(context.Background(), nil, user)

	loginRes, err := authService.Login(context.Background(), "admin@example.com", password, identityService.ClientMetadata{})
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}

	t.Run("successful refresh preserves session expiry", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/v1/auth/refresh", nil)
		req.AddCookie(&http.Cookie{
			Name:  RefreshTokenCookieName,
			Value: loginRes.RawRefreshToken,
		})
		rec := httptest.NewRecorder()

		handler.Refresh(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 on refresh, got: %d (%s)", rec.Code, rec.Body.String())
		}

		cookies := rec.Result().Cookies()
		var rotatedCookie *http.Cookie
		for _, c := range cookies {
			if c.Name == RefreshTokenCookieName {
				rotatedCookie = c
				break
			}
		}
		if rotatedCookie == nil {
			t.Fatal("expected rotated cookie in response")
		}
		if rotatedCookie.Value == loginRes.RawRefreshToken {
			t.Errorf("expected rotated cookie value to differ from old token")
		}
		if !rotatedCookie.Secure {
			t.Errorf("expected Secure=true based on config")
		}
		// Cookie MaxAge must not exceed server session expiry
		if rotatedCookie.MaxAge > RefreshTokenMaxAge || rotatedCookie.MaxAge <= 0 {
			t.Errorf("expected valid MaxAge bounded by session expiry, got: %d", rotatedCookie.MaxAge)
		}
		if rotatedCookie.Expires.Unix() != loginRes.RefreshTokenExpires.Unix() {
			t.Errorf("expected rotated cookie Expires to equal session expires_at (%v), got %v", loginRes.RefreshTokenExpires.Unix(), rotatedCookie.Expires.Unix())
		}

		var env httpapi.ResponseEnvelope
		_ = json.Unmarshal(rec.Body.Bytes(), &env)
		dataBytes, _ := json.Marshal(env.Data)
		var refreshResp RefreshResponse
		_ = json.Unmarshal(dataBytes, &refreshResp)

		if refreshResp.Tokens.AccessToken == "" {
			t.Errorf("expected access_token in response")
		}
		if refreshResp.Tokens.TokenType != "Bearer" {
			t.Errorf("expected token_type Bearer")
		}
		if refreshResp.Tokens.ExpiresIn != 900 {
			t.Errorf("expected expires_in 900")
		}
	})

	t.Run("missing cookie returns generic 401 and clears cookie", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/v1/auth/refresh", nil)
		rec := httptest.NewRecorder()

		handler.Refresh(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("expected 401 for missing cookie, got: %d", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), `"code":"UNAUTHORIZED"`) || !strings.Contains(rec.Body.String(), `"message":"authentication required"`) {
			t.Errorf("expected generic UNAUTHORIZED message, got: %s", rec.Body.String())
		}

		cookies := rec.Result().Cookies()
		var clearedCookie *http.Cookie
		for _, c := range cookies {
			if c.Name == RefreshTokenCookieName {
				clearedCookie = c
				break
			}
		}
		if clearedCookie == nil || clearedCookie.MaxAge != -1 {
			t.Errorf("expected cookie to be cleared with MaxAge = -1 on missing cookie")
		}
	})

	t.Run("replayed old token returns generic 401 and clears cookie", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/v1/auth/refresh", nil)
		req.AddCookie(&http.Cookie{
			Name:  RefreshTokenCookieName,
			Value: loginRes.RawRefreshToken, // already rotated in previous subtest
		})
		rec := httptest.NewRecorder()

		handler.Refresh(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("expected 401 for replayed token, got: %d", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), `"code":"UNAUTHORIZED"`) || !strings.Contains(rec.Body.String(), `"message":"authentication required"`) {
			t.Errorf("expected generic UNAUTHORIZED message, got: %s", rec.Body.String())
		}

		// Check cookie was cleared
		cookies := rec.Result().Cookies()
		var clearedCookie *http.Cookie
		for _, c := range cookies {
			if c.Name == RefreshTokenCookieName {
				clearedCookie = c
				break
			}
		}
		if clearedCookie == nil || clearedCookie.MaxAge != -1 {
			t.Errorf("expected cookie to be cleared with MaxAge = -1")
		}
	})
}

func TestHandler_Logout(t *testing.T) {
	t.Run("successful logout", func(t *testing.T) {
		handler, _, _, sessionRepo, _, _ := setupHandlerTest(t, false)

		userID := uuid.New()
		session, _ := identityDomain.NewSession(userID, strings.Repeat("c", 64), nil, nil, 7*24*time.Hour)
		_ = sessionRepo.Create(context.Background(), nil, session)

		authCtx := &AuthContext{
			UserID:        userID,
			SessionID:     session.ID,
			Email:         "user@example.com",
			FullName:      "User",
			IsSystemAdmin: false,
			Status:        identityDomain.UserStatusActive,
		}

		req := httptest.NewRequest("POST", "/api/v1/auth/logout", nil)
		req = req.WithContext(WithAuthContext(req.Context(), authCtx))
		rec := httptest.NewRecorder()

		handler.Logout(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 on logout, got: %d", rec.Code)
		}

		// Verify session is revoked
		if !session.IsRevoked() {
			t.Errorf("expected session to be marked as revoked")
		}

		// Verify cookie is cleared
		cookies := rec.Result().Cookies()
		var clearedCookie *http.Cookie
		for _, c := range cookies {
			if c.Name == RefreshTokenCookieName {
				clearedCookie = c
				break
			}
		}
		if clearedCookie == nil || clearedCookie.MaxAge != -1 {
			t.Errorf("expected refresh_token cookie cleared on logout")
		}

		// Verify data is null
		var env httpapi.ResponseEnvelope
		_ = json.Unmarshal(rec.Body.Bytes(), &env)
		if env.Data != nil {
			t.Errorf("expected data to be null in logout response, got: %v", env.Data)
		}
		if env.Message != "logout successful" {
			t.Errorf("expected message 'logout successful', got: %s", env.Message)
		}
	})

	t.Run("logout database failure returns 503 and clears cookie best-effort", func(t *testing.T) {
		handler, _, _, sessionRepo, _, _ := setupHandlerTest(t, false)

		userID := uuid.New()
		sessionID := uuid.New()
		sessionRepo.overrideErr = errors.New("database connection down")

		authCtx := &AuthContext{
			UserID:        userID,
			SessionID:     sessionID,
			Email:         "user@example.com",
			FullName:      "User",
			IsSystemAdmin: false,
			Status:        identityDomain.UserStatusActive,
		}

		req := httptest.NewRequest("POST", "/api/v1/auth/logout", nil)
		req = req.WithContext(WithAuthContext(req.Context(), authCtx))
		rec := httptest.NewRecorder()

		handler.Logout(rec, req)

		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("expected 503 on database failure during logout, got: %d (%s)", rec.Code, rec.Body.String())
		}

		// Must NOT claim logout success
		if strings.Contains(rec.Body.String(), "logout successful") {
			t.Errorf("must not return success message on failure")
		}

		// Cookie must still be cleared best-effort
		cookies := rec.Result().Cookies()
		var clearedCookie *http.Cookie
		for _, c := range cookies {
			if c.Name == RefreshTokenCookieName {
				clearedCookie = c
				break
			}
		}
		if clearedCookie == nil || clearedCookie.MaxAge != -1 {
			t.Errorf("expected refresh_token cookie cleared best-effort on error")
		}
	})
}

func TestHandler_Me(t *testing.T) {
	handler, _, _, _, memberRepo, _ := setupHandlerTest(t, false)

	userID := uuid.New()
	orgID := uuid.New()

	memberRepo.memberships = []*orgRepo.UserMembershipWithOrg{
		{
			OrganizationID:    orgID,
			OrganizationName:  "Test Org",
			Slug:              "test-org",
			IsDefaultInternal: true,
			Role:              orgDomain.RoleAdmin,
			Status:            orgDomain.MemberStatusActive,
		},
	}

	authCtx := &AuthContext{
		UserID:        userID,
		SessionID:     uuid.New(),
		Email:         "admin@example.com",
		FullName:      "Admin User",
		IsSystemAdmin: true,
		Status:        identityDomain.UserStatusActive,
	}

	req := httptest.NewRequest("GET", "/api/v1/auth/me", nil)
	req = req.WithContext(WithAuthContext(req.Context(), authCtx))
	rec := httptest.NewRecorder()

	handler.Me(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on /me, got: %d", rec.Code)
	}

	var env httpapi.ResponseEnvelope
	_ = json.Unmarshal(rec.Body.Bytes(), &env)
	dataBytes, _ := json.Marshal(env.Data)
	var meResp MeResponse
	_ = json.Unmarshal(dataBytes, &meResp)

	if meResp.User.Email != "admin@example.com" {
		t.Errorf("expected user email match")
	}
	if len(meResp.Memberships) != 1 {
		t.Fatalf("expected 1 membership, got: %d", len(meResp.Memberships))
	}

	m := meResp.Memberships[0]
	if m.OrganizationSlug != "test-org" || m.Role != "admin" {
		t.Errorf("unexpected membership details: %+v", m)
	}
	if len(m.Permissions) != 9 {
		t.Errorf("expected 9 canonical permissions attached for admin role, got %d", len(m.Permissions))
	}
}

func TestHandler_InternalErrors_Safety(t *testing.T) {
	t.Run("database failure on login returns safe 503 without leaking details", func(t *testing.T) {
		handler, _, userRepo, _, _, _ := setupHandlerTest(t, false)
		userRepo.overrideErr = errors.New("pq: fatal connection error with sensitive credentials user=db password=secret")

		body := `{"email":"admin@example.com","password":"SecretPassword123!"}`
		req := httptest.NewRequest("POST", "/api/v1/auth/login", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		handler.Login(rec, req)

		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("expected 503, got: %d", rec.Code)
		}
		respBody := rec.Body.String()
		if strings.Contains(respBody, "sensitive") || strings.Contains(respBody, "fatal connection error") {
			t.Errorf("response leaked raw database error: %s", respBody)
		}
		if !strings.Contains(respBody, "SERVICE_UNAVAILABLE") {
			t.Errorf("expected code SERVICE_UNAVAILABLE, got: %s", respBody)
		}
	})

	t.Run("database failure on refresh returns safe 503 without leaking details and preserves cookie", func(t *testing.T) {
		handler, _, _, sessionRepo, _, _ := setupHandlerTest(t, false)
		sessionRepo.overrideErr = errors.New("pq: disk full internal postgres error")

		req := httptest.NewRequest("POST", "/api/v1/auth/refresh", nil)
		req.AddCookie(&http.Cookie{
			Name:  RefreshTokenCookieName,
			Value: strings.Repeat("x", 64),
		})
		rec := httptest.NewRecorder()

		handler.Refresh(rec, req)

		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("expected 503, got: %d", rec.Code)
		}
		respBody := rec.Body.String()
		if strings.Contains(respBody, "disk full") || strings.Contains(respBody, "postgres") {
			t.Errorf("response leaked raw database error: %s", respBody)
		}
		if !strings.Contains(respBody, "SERVICE_UNAVAILABLE") {
			t.Errorf("expected code SERVICE_UNAVAILABLE, got: %s", respBody)
		}

		// Refresh cookie MUST NOT be deleted or modified on transient infrastructure outage
		cookies := rec.Result().Cookies()
		for _, c := range cookies {
			if c.Name == RefreshTokenCookieName {
				t.Fatalf("SECURITY/USABILITY FLAW: refresh cookie must not be cleared or modified on infrastructure failure, got: %+v", c)
			}
		}
	})
}
