package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	identityDomain "backup-platform/internal/identity/domain"
	identityHttpapi "backup-platform/internal/identity/httpapi"
	orgDomain "backup-platform/internal/organization/domain"
	orgRepo "backup-platform/internal/organization/repository"
	"backup-platform/internal/platform/database"
	"backup-platform/internal/platform/logger"
	"backup-platform/pkg/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type mockQuerier struct{}

func (m *mockQuerier) Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}
func (m *mockQuerier) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return nil, nil
}
func (m *mockQuerier) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return nil
}

type mockTxManager struct{}

func (m *mockTxManager) Querier() database.Querier { return &mockQuerier{} }
func (m *mockTxManager) WithinTx(ctx context.Context, fn func(q database.Querier) error) error {
	return fn(&mockQuerier{})
}

type mockOrgMemberRepo struct {
	memberships map[string]*orgRepo.UserMembershipWithOrg
	overrideErr error
}

func newMockOrgMemberRepo() *mockOrgMemberRepo {
	return &mockOrgMemberRepo{
		memberships: make(map[string]*orgRepo.UserMembershipWithOrg),
	}
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
	return nil, nil
}
func (r *mockOrgMemberRepo) FindActiveMembershipWithOrg(ctx context.Context, q database.Querier, orgID, userID uuid.UUID) (*orgRepo.UserMembershipWithOrg, error) {
	if r.overrideErr != nil {
		return nil, r.overrideErr
	}
	key := orgID.String() + ":" + userID.String()
	m, ok := r.memberships[key]
	if !ok || m.Status != orgDomain.MemberStatusActive {
		return nil, nil
	}
	return m, nil
}

func setupMiddlewareTest() (*mockOrgMemberRepo, func(http.Handler) http.Handler) {
	repo := newMockOrgMemberRepo()
	txManager := &mockTxManager{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	mw := NewOrganizationContextMiddleware(repo, txManager, logger)
	return repo, mw
}

func TestOrganizationContextMiddleware_HeaderParsing(t *testing.T) {
	repo, mw := setupMiddlewareTest()

	userID := uuid.New()
	orgID := uuid.New()

	// Seed active membership
	repo.memberships[orgID.String()+":"+userID.String()] = &orgRepo.UserMembershipWithOrg{
		OrganizationID:    orgID,
		OrganizationName:  "Test Organization",
		Slug:              "test-org",
		IsDefaultInternal: false,
		Role:              orgDomain.RoleAdmin,
		Status:            orgDomain.MemberStatusActive,
	}

	authCtx := &identityHttpapi.AuthContext{
		UserID:        userID,
		SessionID:     uuid.New(),
		Email:         "user@example.com",
		FullName:      "Test User",
		IsSystemAdmin: false,
		Status:        identityDomain.UserStatusActive,
	}

	dummyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tCtx, ok := TenantContextFromRequest(r)
		if !ok || tCtx == nil {
			http.Error(w, "missing tenant context", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK:" + tCtx.OrganizationSlug))
	})

	wrapped := mw(dummyHandler)

	t.Run("valid single UUID header succeeds and injects tenant context", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/resources", nil)
		req = req.WithContext(identityHttpapi.WithAuthContext(req.Context(), authCtx))
		req.Header.Set(HeaderXOrganizationID, orgID.String())
		rec := httptest.NewRecorder()

		wrapped.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got: %d (%s)", rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "OK:test-org") {
			t.Errorf("unexpected body: %s", rec.Body.String())
		}
		if rec.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("expected Cache-Control: no-store")
		}
	})

	t.Run("missing X-Organization-ID header returns 400", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/resources", nil)
		req = req.WithContext(identityHttpapi.WithAuthContext(req.Context(), authCtx))
		rec := httptest.NewRecorder()

		wrapped.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400 Bad Request, got: %d", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "INVALID_ORGANIZATION_CONTEXT") {
			t.Errorf("expected code INVALID_ORGANIZATION_CONTEXT, got: %s", rec.Body.String())
		}
	})

	t.Run("empty X-Organization-ID header returns 400", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/resources", nil)
		req = req.WithContext(identityHttpapi.WithAuthContext(req.Context(), authCtx))
		req.Header.Set(HeaderXOrganizationID, "")
		rec := httptest.NewRecorder()

		wrapped.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400 Bad Request, got: %d", rec.Code)
		}
	})

	t.Run("whitespace-only X-Organization-ID header returns 400", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/resources", nil)
		req = req.WithContext(identityHttpapi.WithAuthContext(req.Context(), authCtx))
		req.Header.Set(HeaderXOrganizationID, "   ")
		rec := httptest.NewRecorder()

		wrapped.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400 Bad Request, got: %d", rec.Code)
		}
	})

	t.Run("malformed UUID in X-Organization-ID returns 400 without leaking raw header", func(t *testing.T) {
		rawInvalid := "malicious-header-value<script>"
		req := httptest.NewRequest("GET", "/api/v1/resources", nil)
		req = req.WithContext(identityHttpapi.WithAuthContext(req.Context(), authCtx))
		req.Header.Set(HeaderXOrganizationID, rawInvalid)
		rec := httptest.NewRecorder()

		wrapped.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400 Bad Request, got: %d", rec.Code)
		}
		if strings.Contains(rec.Body.String(), rawInvalid) {
			t.Errorf("SECURITY FLAW: raw invalid header echoed in response body")
		}
	})

	t.Run("multiple X-Organization-ID headers return 400", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/resources", nil)
		req = req.WithContext(identityHttpapi.WithAuthContext(req.Context(), authCtx))
		req.Header.Add(HeaderXOrganizationID, orgID.String())
		req.Header.Add(HeaderXOrganizationID, uuid.New().String())
		rec := httptest.NewRecorder()

		wrapped.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400 Bad Request for multiple headers, got: %d", rec.Code)
		}
	})

	t.Run("comma-separated X-Organization-ID header returns 400", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/resources", nil)
		req = req.WithContext(identityHttpapi.WithAuthContext(req.Context(), authCtx))
		req.Header.Set(HeaderXOrganizationID, orgID.String()+","+uuid.New().String())
		rec := httptest.NewRecorder()

		wrapped.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400 Bad Request for comma-separated IDs, got: %d", rec.Code)
		}
	})

	t.Run("missing AuthContext fails closed with 401", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/resources", nil)
		// No AuthContext
		req.Header.Set(HeaderXOrganizationID, orgID.String())
		rec := httptest.NewRecorder()

		wrapped.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("expected 401 Unauthorized when AuthContext is missing, got: %d", rec.Code)
		}
	})
}

func TestOrganizationContextMiddleware_CrossTenantAndStatusIsolation(t *testing.T) {
	repo, mw := setupMiddlewareTest()

	user1ID := uuid.New()
	orgAID := uuid.New()
	orgBID := uuid.New()

	// user1 is active admin in Org A
	repo.memberships[orgAID.String()+":"+user1ID.String()] = &orgRepo.UserMembershipWithOrg{
		OrganizationID:    orgAID,
		OrganizationName:  "Org A",
		Slug:              "org-a",
		IsDefaultInternal: false,
		Role:              orgDomain.RoleAdmin,
		Status:            orgDomain.MemberStatusActive,
	}

	authCtxUser1 := &identityHttpapi.AuthContext{
		UserID:        user1ID,
		SessionID:     uuid.New(),
		Email:         "user1@example.com",
		FullName:      "User One",
		IsSystemAdmin: false,
		Status:        identityDomain.UserStatusActive,
	}

	dummyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	wrapped := mw(dummyHandler)

	assertExact404NotFound := func(t *testing.T, rec *httptest.ResponseRecorder, scenarioName string, secretKeywords []string) {
		t.Helper()
		if rec.Code != http.StatusNotFound {
			t.Fatalf("[%s] expected HTTP 404 Not Found, got: %d", scenarioName, rec.Code)
		}

		type errorEnvelope struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
				Details any    `json:"details"`
			} `json:"error"`
			RequestID string `json:"request_id"`
		}

		var env errorEnvelope
		if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
			t.Fatalf("[%s] failed to decode error envelope JSON: %v", scenarioName, err)
		}

		if env.Error.Code != "ORGANIZATION_NOT_FOUND" {
			t.Errorf("[%s] expected error code 'ORGANIZATION_NOT_FOUND', got: %q", scenarioName, env.Error.Code)
		}
		if env.Error.Message != "organization not found" {
			t.Errorf("[%s] expected error message 'organization not found', got: %q", scenarioName, env.Error.Message)
		}
		if env.Error.Details != nil {
			t.Errorf("[%s] expected details to be nil, got: %v", scenarioName, env.Error.Details)
		}
		if env.RequestID == "" {
			t.Errorf("[%s] expected non-empty request_id", scenarioName)
		}

		bodyStr := rec.Body.String()
		for _, kw := range secretKeywords {
			if strings.Contains(strings.ToLower(bodyStr), strings.ToLower(kw)) {
				t.Errorf("[%s] SECURITY FLAW: internal authorization detail %q leaked in body: %s", scenarioName, kw, bodyStr)
			}
		}
	}

	t.Run("user accessing their active organization succeeds", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/resources", nil)
		req = req.WithContext(identityHttpapi.WithAuthContext(req.Context(), authCtxUser1))
		req.Header.Set(HeaderXOrganizationID, orgAID.String())
		rec := httptest.NewRecorder()

		wrapped.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected 200 OK, got: %d", rec.Code)
		}
	})

	t.Run("user accessing non-member organization returns exact generic 404", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/resources", nil)
		req = req.WithContext(logger.WithRequestID(identityHttpapi.WithAuthContext(req.Context(), authCtxUser1), "req-id-cross-1"))
		req.Header.Set(HeaderXOrganizationID, orgBID.String()) // not a member of Org B
		rec := httptest.NewRecorder()

		wrapped.ServeHTTP(rec, req)

		assertExact404NotFound(t, rec, "non-member", []string{"member", "membership", "suspended", "invited", orgBID.String()})
	})

	t.Run("user accessing completely non-existent organization returns exact identical generic 404", func(t *testing.T) {
		nonExistentID := uuid.New()
		req := httptest.NewRequest("GET", "/api/v1/resources", nil)
		req = req.WithContext(logger.WithRequestID(identityHttpapi.WithAuthContext(req.Context(), authCtxUser1), "req-id-cross-2"))
		req.Header.Set(HeaderXOrganizationID, nonExistentID.String())
		rec := httptest.NewRecorder()

		wrapped.ServeHTTP(rec, req)

		assertExact404NotFound(t, rec, "non-existent", []string{"exist", "membership", nonExistentID.String()})
	})

	t.Run("invited membership returns exact generic 404 to prevent enumeration", func(t *testing.T) {
		invitedOrgID := uuid.New()
		repo.memberships[invitedOrgID.String()+":"+user1ID.String()] = &orgRepo.UserMembershipWithOrg{
			OrganizationID:    invitedOrgID,
			OrganizationName:  "Invited Org",
			Slug:              "invited-org",
			IsDefaultInternal: false,
			Role:              orgDomain.RoleMember,
			Status:            orgDomain.MemberStatusInvited, // not active
		}

		req := httptest.NewRequest("GET", "/api/v1/resources", nil)
		req = req.WithContext(logger.WithRequestID(identityHttpapi.WithAuthContext(req.Context(), authCtxUser1), "req-id-cross-3"))
		req.Header.Set(HeaderXOrganizationID, invitedOrgID.String())
		rec := httptest.NewRecorder()

		wrapped.ServeHTTP(rec, req)

		assertExact404NotFound(t, rec, "invited-member", []string{"invited", "membership", invitedOrgID.String(), "invited-org"})
	})

	t.Run("suspended membership returns exact generic 404 to prevent enumeration", func(t *testing.T) {
		suspendedOrgID := uuid.New()
		repo.memberships[suspendedOrgID.String()+":"+user1ID.String()] = &orgRepo.UserMembershipWithOrg{
			OrganizationID:    suspendedOrgID,
			OrganizationName:  "Suspended Org",
			Slug:              "suspended-org",
			IsDefaultInternal: false,
			Role:              orgDomain.RoleMember,
			Status:            orgDomain.MemberStatusSuspended, // suspended
		}

		req := httptest.NewRequest("GET", "/api/v1/resources", nil)
		req = req.WithContext(logger.WithRequestID(identityHttpapi.WithAuthContext(req.Context(), authCtxUser1), "req-id-cross-4"))
		req.Header.Set(HeaderXOrganizationID, suspendedOrgID.String())
		rec := httptest.NewRecorder()

		wrapped.ServeHTTP(rec, req)

		assertExact404NotFound(t, rec, "suspended-member", []string{"suspended", "membership", suspendedOrgID.String(), "suspended-org"})
	})
}

func TestOrganizationContextMiddleware_SystemAdminIsolation(t *testing.T) {
	_, mw := setupMiddlewareTest()

	sysAdminID := uuid.New()
	targetOrgID := uuid.New()

	// sysAdmin is NOT a member of targetOrgID
	authCtxSysAdmin := &identityHttpapi.AuthContext{
		UserID:        sysAdminID,
		SessionID:     uuid.New(),
		Email:         "sysadmin@example.com",
		FullName:      "System Administrator",
		IsSystemAdmin: true, // System admin flag
		Status:        identityDomain.UserStatusActive,
	}

	dummyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	wrapped := mw(dummyHandler)

	req := httptest.NewRequest("GET", "/api/v1/resources", nil)
	req = req.WithContext(identityHttpapi.WithAuthContext(req.Context(), authCtxSysAdmin))
	req.Header.Set(HeaderXOrganizationID, targetOrgID.String())
	rec := httptest.NewRecorder()

	wrapped.ServeHTTP(rec, req)

	// System Admin must NOT bypass tenant membership on tenant-scoped routes
	if rec.Code != http.StatusNotFound {
		t.Fatalf("SECURITY FLAW: System Admin bypassed tenant membership isolation, expected 404, got %d", rec.Code)
	}
}

func TestOrganizationContextMiddleware_InfrastructureFailure(t *testing.T) {
	repo, mw := setupMiddlewareTest()
	repo.overrideErr = errors.New("pq: connection reset by peer with sensitive credentials user=db")

	userID := uuid.New()
	orgID := uuid.New()

	authCtx := &identityHttpapi.AuthContext{
		UserID:        userID,
		SessionID:     uuid.New(),
		Email:         "user@example.com",
		FullName:      "Test User",
		IsSystemAdmin: false,
		Status:        identityDomain.UserStatusActive,
	}

	dummyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	wrapped := mw(dummyHandler)

	req := httptest.NewRequest("GET", "/api/v1/resources", nil)
	req = req.WithContext(identityHttpapi.WithAuthContext(req.Context(), authCtx))
	req.Header.Set(HeaderXOrganizationID, orgID.String())
	rec := httptest.NewRecorder()

	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 Service Unavailable on DB failure, got: %d", rec.Code)
	}
	respBody := rec.Body.String()
	if strings.Contains(respBody, "pq") || strings.Contains(respBody, "connection reset") || strings.Contains(respBody, "sensitive") {
		t.Errorf("SECURITY FLAW: raw database error leaked in body: %s", respBody)
	}
	if !strings.Contains(respBody, "SERVICE_UNAVAILABLE") {
		t.Errorf("expected code SERVICE_UNAVAILABLE, got: %s", respBody)
	}
}
