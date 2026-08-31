package httpapi

import (
	"context"
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
	"backup-platform/pkg/uuid"
)

// In-Memory Test Setup for Middleware Tests

type mockAuthQuerier struct{}

func (m *mockAuthQuerier) Exec(ctx context.Context, sql string, arguments ...any) (any, error) {
	return nil, nil
}

type mockAuthTxManager struct {
	querier database.Querier
}

func (m *mockAuthTxManager) Querier() database.Querier { return nil }
func (m *mockAuthTxManager) WithinTx(ctx context.Context, fn func(q database.Querier) error) error {
	return fn(nil)
}

type mockUserRepo struct {
	users       map[uuid.UUID]*identityDomain.User
	overrideErr error
}

func (r *mockUserRepo) Create(ctx context.Context, q database.Querier, user *identityDomain.User) error {
	r.users[user.ID] = user
	return nil
}
func (r *mockUserRepo) FindByID(ctx context.Context, q database.Querier, id uuid.UUID) (*identityDomain.User, error) {
	if r.overrideErr != nil {
		return nil, r.overrideErr
	}
	u, ok := r.users[id]
	if !ok {
		return nil, identityDomain.ErrUserNotFound
	}
	return u, nil
}
func (r *mockUserRepo) FindByEmail(ctx context.Context, q database.Querier, email string) (*identityDomain.User, error) {
	if r.overrideErr != nil {
		return nil, r.overrideErr
	}
	for _, u := range r.users {
		if u.Email == email {
			return u, nil
		}
	}
	return nil, identityDomain.ErrUserNotFound
}
func (r *mockUserRepo) HasSystemAdmin(ctx context.Context, q database.Querier) (bool, error) {
	return true, nil
}
func (r *mockUserRepo) UpdateStatus(ctx context.Context, q database.Querier, id uuid.UUID, status identityDomain.UserStatus) error {
	return nil
}

type mockSessionRepo struct {
	sessions    map[uuid.UUID]*identityDomain.Session
	overrideErr error
}

func (r *mockSessionRepo) Create(ctx context.Context, q database.Querier, s *identityDomain.Session) error {
	r.sessions[s.ID] = s
	return nil
}
func (r *mockSessionRepo) FindByID(ctx context.Context, q database.Querier, id uuid.UUID) (*identityDomain.Session, error) {
	if r.overrideErr != nil {
		return nil, r.overrideErr
	}
	s, ok := r.sessions[id]
	if !ok {
		return nil, identityDomain.ErrSessionNotFound
	}
	return s, nil
}
func (r *mockSessionRepo) FindByRefreshTokenHash(ctx context.Context, q database.Querier, hash string) (*identityDomain.Session, error) {
	if r.overrideErr != nil {
		return nil, r.overrideErr
	}
	for _, s := range r.sessions {
		if s.RefreshTokenHash == hash {
			return s, nil
		}
	}
	return nil, identityDomain.ErrSessionNotFound
}
func (r *mockSessionRepo) RotateRefreshToken(ctx context.Context, q database.Querier, sessionID uuid.UUID, oldHash, newHash string, now time.Time) error {
	if r.overrideErr != nil {
		return r.overrideErr
	}
	s, ok := r.sessions[sessionID]
	if !ok {
		return identityDomain.ErrSessionNotFound
	}
	if s.RefreshTokenHash != oldHash || s.IsRevoked() || s.IsExpired(now) {
		return identityDomain.ErrInvalidSession
	}
	s.RefreshTokenHash = newHash
	s.LastUsedAt = now
	return nil
}
func (r *mockSessionRepo) RevokeByID(ctx context.Context, q database.Querier, id uuid.UUID, now time.Time) error {
	if r.overrideErr != nil {
		return r.overrideErr
	}
	if s, ok := r.sessions[id]; ok {
		s.Revoke(now)
	}
	return nil
}
func (r *mockSessionRepo) RevokeAllForUser(ctx context.Context, q database.Querier, userID uuid.UUID, now time.Time) error {
	if r.overrideErr != nil {
		return r.overrideErr
	}
	return nil
}

type mockMiddlewareMemberRepo struct{}

func (r *mockMiddlewareMemberRepo) Create(ctx context.Context, q database.Querier, member *orgDomain.Member) error {
	return nil
}
func (r *mockMiddlewareMemberRepo) FindMembership(ctx context.Context, q database.Querier, orgID, userID uuid.UUID) (*orgDomain.Member, error) {
	return nil, nil
}
func (r *mockMiddlewareMemberRepo) ListUserOrganizations(ctx context.Context, q database.Querier, userID uuid.UUID) ([]*orgDomain.Organization, error) {
	return nil, nil
}
func (r *mockMiddlewareMemberRepo) ListUserMembershipsWithOrg(ctx context.Context, q database.Querier, userID uuid.UUID) ([]*orgRepo.UserMembershipWithOrg, error) {
	return nil, nil
}
func (r *mockMiddlewareMemberRepo) FindActiveMembershipWithOrg(ctx context.Context, q database.Querier, orgID, userID uuid.UUID) (*orgRepo.UserMembershipWithOrg, error) {
	return nil, nil
}

func setupMiddlewareTest(t *testing.T) (identityService.TokenService, *identityService.AuthService, *mockUserRepo, *mockSessionRepo) {
	jwtKey := []byte("test-secret-key-at-least-32-bytes-long!")
	jwtService, err := identityService.NewJWTService(jwtKey)
	if err != nil {
		t.Fatalf("failed to init JWT service: %v", err)
	}

	userRepo := &mockUserRepo{users: make(map[uuid.UUID]*identityDomain.User)}
	sessionRepo := &mockSessionRepo{sessions: make(map[uuid.UUID]*identityDomain.Session)}
	memberRepo := &mockMiddlewareMemberRepo{}
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

	return jwtService, authService, userRepo, sessionRepo
}

func TestAuthMiddleware_Scenarios(t *testing.T) {
	jwtService, authService, userRepo, sessionRepo := setupMiddlewareTest(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	authMiddleware := NewAuthMiddleware(jwtService, authService, logger)

	// Create user in DB
	user, _ := identityDomain.NewUser("admin@example.com", "$argon2id$v=19$m=65536,t=3,p=4$salt$hash", "Admin", true)
	_ = userRepo.Create(context.Background(), nil, user)

	// Create session in DB
	session, _ := identityDomain.NewSession(user.ID, strings.Repeat("a", 64), nil, nil, 7*24*time.Hour)
	_ = sessionRepo.Create(context.Background(), nil, session)

	// Generate valid JWT
	validToken, _, _ := jwtService.GenerateAccessToken(user.ID, session.ID, user.IsSystemAdmin)

	dummyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authCtx, ok := AuthContextFromRequest(r)
		if !ok || authCtx == nil {
			http.Error(w, "missing context", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK:" + authCtx.Email))
	})

	wrapped := authMiddleware(dummyHandler)

	t.Run("missing authorization header", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/protected", nil)
		rec := httptest.NewRecorder()
		wrapped.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("expected 401, got: %d", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), `"code":"UNAUTHORIZED"`) || !strings.Contains(rec.Body.String(), `"message":"authentication required"`) {
			t.Errorf("expected generic UNAUTHORIZED message, got: %s", rec.Body.String())
		}
		if rec.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("expected Cache-Control: no-store")
		}
	})

	t.Run("malformed bearer header", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/protected", nil)
		req.Header.Set("Authorization", "Basic 12345")
		rec := httptest.NewRecorder()
		wrapped.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("expected 401, got: %d", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), `"message":"authentication required"`) {
			t.Errorf("expected generic message, got: %s", rec.Body.String())
		}
	})

	t.Run("invalid JWT token string", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/protected", nil)
		req.Header.Set("Authorization", "Bearer invalid.garbage.token")
		rec := httptest.NewRecorder()
		wrapped.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("expected 401, got: %d", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), `"message":"authentication required"`) {
			t.Errorf("expected generic message, got: %s", rec.Body.String())
		}
	})

	t.Run("valid JWT with active DB session", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/protected", nil)
		req.Header.Set("Authorization", "Bearer "+validToken)
		rec := httptest.NewRecorder()
		wrapped.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got: %d", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "OK:admin@example.com") {
			t.Errorf("unexpected body: %s", rec.Body.String())
		}
	})

	t.Run("valid JWT with revoked DB session", func(t *testing.T) {
		// Revoke session in DB
		session.Revoke(time.Now().UTC())

		req := httptest.NewRequest("GET", "/protected", nil)
		req.Header.Set("Authorization", "Bearer "+validToken)
		rec := httptest.NewRecorder()
		wrapped.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("expected 401 for revoked DB session, got: %d", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), `"message":"authentication required"`) {
			t.Errorf("expected generic message, got: %s", rec.Body.String())
		}
		session.RevokedAt = nil // unrevoke for other tests
	})

	t.Run("valid JWT with inactive DB user", func(t *testing.T) {
		user.Status = identityDomain.UserStatusInactive

		req := httptest.NewRequest("GET", "/protected", nil)
		req.Header.Set("Authorization", "Bearer "+validToken)
		rec := httptest.NewRecorder()
		wrapped.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("expected 401 for inactive DB user, got: %d", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), `"message":"authentication required"`) {
			t.Errorf("expected generic message, got: %s", rec.Body.String())
		}
		user.Status = identityDomain.UserStatusActive // reset
	})

	t.Run("database failure during session validation returns 503", func(t *testing.T) {
		sessionRepo.overrideErr = errors.New("pq: database down")

		req := httptest.NewRequest("GET", "/protected", nil)
		req.Header.Set("Authorization", "Bearer "+validToken)
		rec := httptest.NewRecorder()
		wrapped.ServeHTTP(rec, req)
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("expected 503 on DB outage, got: %d", rec.Code)
		}
		sessionRepo.overrideErr = nil
	})
}

func TestAuthMiddleware_StaleJWTPrivilegeOverrides(t *testing.T) {
	jwtService, authService, userRepo, sessionRepo := setupMiddlewareTest(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	authMiddleware := NewAuthMiddleware(jwtService, authService, logger)

	// User in DB has IsSystemAdmin = FALSE
	user, _ := identityDomain.NewUser("operator@example.com", "$argon2id$v=19$m=65536,t=3,p=4$salt$hash", "Operator", false)
	_ = userRepo.Create(context.Background(), nil, user)

	session, _ := identityDomain.NewSession(user.ID, strings.Repeat("b", 64), nil, nil, 7*24*time.Hour)
	_ = sessionRepo.Create(context.Background(), nil, session)

	// Stale JWT token issued previously when user was system admin (is_system_admin = TRUE in claims)
	staleTokenWithAdminClaim, _, err := jwtService.GenerateAccessToken(user.ID, session.ID, true)
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}

	var capturedAdminStatus bool
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authCtx, ok := AuthContextFromRequest(r)
		if !ok || authCtx == nil {
			http.Error(w, "missing context", http.StatusInternalServerError)
			return
		}
		capturedAdminStatus = authCtx.IsSystemAdmin
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+staleTokenWithAdminClaim)
	rec := httptest.NewRecorder()

	authMiddleware(handler).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got: %d", rec.Code)
	}

	// CRITICAL ASSERTION: The context must reflect the DB current state (false), NOT the stale JWT claim (true)
	if capturedAdminStatus != false {
		t.Errorf("SECURITY FLAW: AuthContext used stale JWT claim (is_system_admin=true) instead of authoritative DB state (false)")
	}
}
