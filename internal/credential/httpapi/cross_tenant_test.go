package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"backup-platform/internal/credential/domain"
	identityDomain "backup-platform/internal/identity/domain"
	identityHttpapi "backup-platform/internal/identity/httpapi"
	orgDomain "backup-platform/internal/organization/domain"
	orgHttpapi "backup-platform/internal/organization/httpapi"
	orgRepo "backup-platform/internal/organization/repository"
	"backup-platform/internal/platform/database"
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
	key := orgID.String() + ":" + userID.String()
	m, ok := r.memberships[key]
	if !ok || m.Status != orgDomain.MemberStatusActive {
		return nil, nil
	}
	return m, nil
}

func TestCredential_CrossTenantAndPipelineIntegration(t *testing.T) {
	userA := uuid.New()
	orgA := uuid.New()
	orgB := uuid.New()

	memberRepo := newMockOrgMemberRepo()
	// userA is active admin in Org A only
	memberRepo.memberships[orgA.String()+":"+userA.String()] = &orgRepo.UserMembershipWithOrg{
		OrganizationID:    orgA,
		OrganizationName:  "Org A",
		Slug:              "org-a",
		IsDefaultInternal: false,
		Role:              orgDomain.RoleAdmin,
		Status:            orgDomain.MemberStatusActive,
	}

	testLogger := slog.New(slog.NewTextHandler(io.Discard, nil))
	orgContextMW := orgHttpapi.NewOrganizationContextMiddleware(memberRepo, &mockTxManager{}, testLogger)
	adminGuard := orgHttpapi.RequireOrganizationAdmin(testLogger)

	credService := &fakeCredentialService{
		listItems: []*domain.CredentialMetadata{
			{
				ID:             uuid.New(),
				OrganizationID: orgA,
				Name:           "Org A Credential",
				Type:           domain.TypeSSHPassword,
				KeyVersion:     1,
				CreatedAt:      time.Now().UTC(),
				UpdatedAt:      time.Now().UTC(),
			},
		},
	}
	credHandler := NewHandler(credService, testLogger)

	listPipeline := orgContextMW(adminGuard(http.HandlerFunc(credHandler.List)))
	createPipeline := orgContextMW(adminGuard(http.HandlerFunc(credHandler.Create)))
	updatePipeline := orgContextMW(adminGuard(http.HandlerFunc(credHandler.Update)))
	deletePipeline := orgContextMW(adminGuard(http.HandlerFunc(credHandler.Delete)))

	t.Run("User A accessing Org A is granted access", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/credentials", nil)
		req.Header.Set("X-Organization-ID", orgA.String())
		authCtx := &identityHttpapi.AuthContext{
			UserID:        userA,
			Email:         "userA@example.com",
			Status:        identityDomain.UserStatusActive,
			IsSystemAdmin: false,
		}
		req = req.WithContext(identityHttpapi.WithAuthContext(req.Context(), authCtx))

		rec := httptest.NewRecorder()
		listPipeline.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK for Org A admin, got %d. Body: %s", rec.Code, rec.Body.String())
		}
		if credService.capturedOrgID != orgA {
			t.Errorf("expected service to query Org A ID %s, got %s", orgA, credService.capturedOrgID)
		}
	})

	t.Run("User A attempting to access Org B receives 404 ORGANIZATION_NOT_FOUND", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/credentials", nil)
		req.Header.Set("X-Organization-ID", orgB.String())
		authCtx := &identityHttpapi.AuthContext{
			UserID:        userA,
			Email:         "userA@example.com",
			Status:        identityDomain.UserStatusActive,
			IsSystemAdmin: false,
		}
		req = req.WithContext(identityHttpapi.WithAuthContext(req.Context(), authCtx))

		rec := httptest.NewRecorder()
		listPipeline.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 for non-member org, got %d", rec.Code)
		}
	})

	t.Run("System Admin without membership in Org A or B receives 404 (no generic bypass)", func(t *testing.T) {
		sysAdminUser := uuid.New()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/credentials", nil)
		req.Header.Set("X-Organization-ID", orgA.String())
		authCtx := &identityHttpapi.AuthContext{
			UserID:        sysAdminUser,
			Email:         "sysadmin@example.com",
			Status:        identityDomain.UserStatusActive,
			IsSystemAdmin: true, // System admin flag set
		}
		req = req.WithContext(identityHttpapi.WithAuthContext(req.Context(), authCtx))

		rec := httptest.NewRecorder()
		listPipeline.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 for system admin without membership, got %d", rec.Code)
		}
	})

	t.Run("Create credential strictly binds to X-Organization-ID tenant context", func(t *testing.T) {
		body := map[string]any{
			"name":   "Tenant A Key",
			"type":   "ssh_password",
			"secret": "password-a",
		}
		jsonBytes, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/credentials", bytes.NewReader(jsonBytes))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Organization-ID", orgA.String())
		authCtx := &identityHttpapi.AuthContext{
			UserID:        userA,
			Email:         "userA@example.com",
			Status:        identityDomain.UserStatusActive,
			IsSystemAdmin: false,
		}
		req = req.WithContext(identityHttpapi.WithAuthContext(req.Context(), authCtx))

		rec := httptest.NewRecorder()
		createPipeline.ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created, got %d", rec.Code)
		}
		if credService.capturedOrgID != orgA {
			t.Errorf("expected credential created under Org A %s, got %s", orgA, credService.capturedOrgID)
		}
	})

	t.Run("Update credential strictly binds to X-Organization-ID tenant context", func(t *testing.T) {
		credID := uuid.New()
		credService.currentMeta = &domain.CredentialMetadata{
			ID:             credID,
			OrganizationID: orgA,
			Name:           "Old Name",
			Type:           domain.TypeSSHPassword,
		}

		body := map[string]any{
			"name": "New Name",
		}
		jsonBytes, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPut, "/api/v1/credentials/"+credID.String(), bytes.NewReader(jsonBytes))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Organization-ID", orgA.String())
		req.SetPathValue("id", credID.String())
		authCtx := &identityHttpapi.AuthContext{
			UserID:        userA,
			Email:         "userA@example.com",
			Status:        identityDomain.UserStatusActive,
			IsSystemAdmin: false,
		}
		req = req.WithContext(identityHttpapi.WithAuthContext(req.Context(), authCtx))

		rec := httptest.NewRecorder()
		updatePipeline.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d. Body: %s", rec.Code, rec.Body.String())
		}
		if credService.capturedOrgID != orgA {
			t.Errorf("expected update under Org A %s, got %s", orgA, credService.capturedOrgID)
		}
	})

	t.Run("Delete credential strictly binds to X-Organization-ID tenant context", func(t *testing.T) {
		credID := uuid.New()
		credService.currentMeta = &domain.CredentialMetadata{
			ID:             credID,
			OrganizationID: orgA,
		}

		req := httptest.NewRequest(http.MethodDelete, "/api/v1/credentials/"+credID.String(), nil)
		req.Header.Set("X-Organization-ID", orgA.String())
		req.SetPathValue("id", credID.String())
		authCtx := &identityHttpapi.AuthContext{
			UserID:        userA,
			Email:         "userA@example.com",
			Status:        identityDomain.UserStatusActive,
			IsSystemAdmin: false,
		}
		req = req.WithContext(identityHttpapi.WithAuthContext(req.Context(), authCtx))

		rec := httptest.NewRecorder()
		deletePipeline.ServeHTTP(rec, req)

		if rec.Code != http.StatusNoContent {
			t.Fatalf("expected 204 No Content, got %d. Body: %s", rec.Code, rec.Body.String())
		}
		if credService.capturedOrgID != orgA {
			t.Errorf("expected delete under Org A %s, got %s", orgA, credService.capturedOrgID)
		}
	})
}
