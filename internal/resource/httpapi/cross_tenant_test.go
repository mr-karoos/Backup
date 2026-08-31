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

	"backup-platform/internal/connector"
	identityDomain "backup-platform/internal/identity/domain"
	identityHttpapi "backup-platform/internal/identity/httpapi"
	"backup-platform/internal/organization/authz"
	orgDomain "backup-platform/internal/organization/domain"
	orgHttpapi "backup-platform/internal/organization/httpapi"
	orgRepo "backup-platform/internal/organization/repository"
	"backup-platform/internal/platform/database"
	"backup-platform/internal/resource/domain"
	"backup-platform/pkg/uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type mockTxManager struct{}

func (m *mockTxManager) Querier() database.Querier {
	return &mockTxQuerier{}
}

func (m *mockTxManager) WithinTx(ctx context.Context, fn func(tx database.Querier) error) error {
	return fn(&mockTxQuerier{})
}

type mockTxQuerier struct{}

func (q *mockTxQuerier) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}
func (q *mockTxQuerier) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return nil, nil
}
func (q *mockTxQuerier) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return nil
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

func TestResource_CrossTenantAndPipelineIntegration(t *testing.T) {
	userAdminA := uuid.New()
	userMemberA := uuid.New()
	userViewerA := uuid.New()
	orgA := uuid.New()
	orgB := uuid.New()

	memberRepo := newMockOrgMemberRepo()
	memberRepo.memberships[orgA.String()+":"+userAdminA.String()] = &orgRepo.UserMembershipWithOrg{
		OrganizationID:    orgA,
		OrganizationName:  "Org A",
		Slug:              "org-a",
		IsDefaultInternal: false,
		Role:              orgDomain.RoleAdmin,
		Status:            orgDomain.MemberStatusActive,
	}
	memberRepo.memberships[orgA.String()+":"+userMemberA.String()] = &orgRepo.UserMembershipWithOrg{
		OrganizationID:    orgA,
		OrganizationName:  "Org A",
		Slug:              "org-a",
		IsDefaultInternal: false,
		Role:              orgDomain.RoleMember,
		Status:            orgDomain.MemberStatusActive,
	}
	memberRepo.memberships[orgA.String()+":"+userViewerA.String()] = &orgRepo.UserMembershipWithOrg{
		OrganizationID:    orgA,
		OrganizationName:  "Org A",
		Slug:              "org-a",
		IsDefaultInternal: false,
		Role:              orgDomain.RoleViewer,
		Status:            orgDomain.MemberStatusActive,
	}

	testLogger := slog.New(slog.NewTextHandler(io.Discard, nil))
	orgContextMW := orgHttpapi.NewOrganizationContextMiddleware(memberRepo, &mockTxManager{}, testLogger)
	readGuard := orgHttpapi.RequirePermission(authz.PermissionResourceRead, testLogger)
	writeGuard := orgHttpapi.RequirePermission(authz.PermissionResourceWrite, testLogger)

	resID := uuid.New()
	credID := uuid.New()
	itemOrgA := sampleResourceWithConnector(orgA, resID, credID)

	resService := &fakeResourceService{
		listItems:  []*domain.ResourceWithConnector{itemOrgA},
		getRes:     itemOrgA,
		createdRes: itemOrgA,
		updatedRes: itemOrgA,
	}
	discService := &fakeDatabaseDiscoveryExecutor{
		result: []connector.DatabaseInfo{
			{Name: "ecommerce_prod", SizeBytes: 104857600, Status: connector.DatabaseStatusAccessible},
		},
	}
	resHandler := NewHandler(resService, &fakeConnectionTestExecutor{}, discService, testLogger)

	listPipeline := orgContextMW(readGuard(http.HandlerFunc(resHandler.List)))
	getPipeline := orgContextMW(readGuard(http.HandlerFunc(resHandler.GetByID)))
	createPipeline := orgContextMW(writeGuard(http.HandlerFunc(resHandler.Create)))
	updatePipeline := orgContextMW(writeGuard(http.HandlerFunc(resHandler.Update)))
	deletePipeline := orgContextMW(writeGuard(http.HandlerFunc(resHandler.Delete)))
	testConnPipeline := orgContextMW(writeGuard(http.HandlerFunc(resHandler.TestConnection)))
	discPipeline := orgContextMW(writeGuard(http.HandlerFunc(resHandler.DiscoverDatabases)))

	t.Run("Admin Org A can read, write, test, and discover", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/resources", nil)
		req.Header.Set("X-Organization-ID", orgA.String())
		authCtx := &identityHttpapi.AuthContext{
			UserID:        userAdminA,
			Status:        identityDomain.UserStatusActive,
			IsSystemAdmin: false,
		}
		req = req.WithContext(identityHttpapi.WithAuthContext(req.Context(), authCtx))

		rec := httptest.NewRecorder()
		listPipeline.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 for Admin read, got %d", rec.Code)
		}

		// Discover Databases -> 200 OK
		reqDisc := httptest.NewRequest(http.MethodGet, "/api/v1/resources/"+resID.String()+"/databases", nil)
		reqDisc.Header.Set("X-Organization-ID", orgA.String())
		reqDisc.SetPathValue("id", resID.String())
		reqDisc = reqDisc.WithContext(identityHttpapi.WithAuthContext(reqDisc.Context(), authCtx))
		recDisc := httptest.NewRecorder()
		discPipeline.ServeHTTP(recDisc, reqDisc)
		if recDisc.Code != http.StatusOK {
			t.Fatalf("expected 200 for Admin database discovery, got %d", recDisc.Code)
		}
	})

	t.Run("Member Org A receives 403 on write and discovery endpoints", func(t *testing.T) {
		authCtx := &identityHttpapi.AuthContext{
			UserID:        userMemberA,
			Status:        identityDomain.UserStatusActive,
			IsSystemAdmin: false,
		}

		// Read List -> 200 OK
		reqList := httptest.NewRequest(http.MethodGet, "/api/v1/resources", nil)
		reqList.Header.Set("X-Organization-ID", orgA.String())
		reqList = reqList.WithContext(identityHttpapi.WithAuthContext(reqList.Context(), authCtx))
		recList := httptest.NewRecorder()
		listPipeline.ServeHTTP(recList, reqList)
		if recList.Code != http.StatusOK {
			t.Errorf("expected 200 for Member read list, got %d", recList.Code)
		}

		// Read Detail -> 200 OK
		reqGet := httptest.NewRequest(http.MethodGet, "/api/v1/resources/"+resID.String(), nil)
		reqGet.Header.Set("X-Organization-ID", orgA.String())
		reqGet.SetPathValue("id", resID.String())
		reqGet = reqGet.WithContext(identityHttpapi.WithAuthContext(reqGet.Context(), authCtx))
		recGet := httptest.NewRecorder()
		getPipeline.ServeHTTP(recGet, reqGet)
		if recGet.Code != http.StatusOK {
			t.Errorf("expected 200 for Member read detail, got %d", recGet.Code)
		}

		// Create -> 403 Forbidden
		body := map[string]any{
			"name": "Member Created Server",
			"type": "ubuntu_ssh",
			"connector": map[string]any{
				"host":          "10.0.0.1",
				"port":          22,
				"auth_type":     "ssh_key",
				"username":      "root",
				"credential_id": credID.String(),
			},
		}
		jsonBytes, _ := json.Marshal(body)
		reqCreate := httptest.NewRequest(http.MethodPost, "/api/v1/resources", bytes.NewReader(jsonBytes))
		reqCreate.Header.Set("Content-Type", "application/json")
		reqCreate.Header.Set("X-Organization-ID", orgA.String())
		reqCreate = reqCreate.WithContext(identityHttpapi.WithAuthContext(reqCreate.Context(), authCtx))
		recCreate := httptest.NewRecorder()
		createPipeline.ServeHTTP(recCreate, reqCreate)
		if recCreate.Code != http.StatusForbidden {
			t.Errorf("expected 403 on Create for Member, got %d", recCreate.Code)
		}

		// Update -> 403 Forbidden
		reqUpdate := httptest.NewRequest(http.MethodPut, "/api/v1/resources/"+resID.String(), bytes.NewReader(jsonBytes))
		reqUpdate.Header.Set("Content-Type", "application/json")
		reqUpdate.Header.Set("X-Organization-ID", orgA.String())
		reqUpdate.SetPathValue("id", resID.String())
		reqUpdate = reqUpdate.WithContext(identityHttpapi.WithAuthContext(reqUpdate.Context(), authCtx))
		recUpdate := httptest.NewRecorder()
		updatePipeline.ServeHTTP(recUpdate, reqUpdate)
		if recUpdate.Code != http.StatusForbidden {
			t.Errorf("expected 403 on Update for Member, got %d", recUpdate.Code)
		}

		// Delete -> 403 Forbidden
		reqDelete := httptest.NewRequest(http.MethodDelete, "/api/v1/resources/"+resID.String(), nil)
		reqDelete.Header.Set("X-Organization-ID", orgA.String())
		reqDelete.SetPathValue("id", resID.String())
		reqDelete = reqDelete.WithContext(identityHttpapi.WithAuthContext(reqDelete.Context(), authCtx))
		recDelete := httptest.NewRecorder()
		deletePipeline.ServeHTTP(recDelete, reqDelete)
		if recDelete.Code != http.StatusForbidden {
			t.Errorf("expected 403 on Delete for Member, got %d", recDelete.Code)
		}

		// Test Connection -> 403 Forbidden
		reqTest := httptest.NewRequest(http.MethodPost, "/api/v1/resources/"+resID.String()+"/test-connection", nil)
		reqTest.Header.Set("X-Organization-ID", orgA.String())
		reqTest.SetPathValue("id", resID.String())
		reqTest = reqTest.WithContext(identityHttpapi.WithAuthContext(reqTest.Context(), authCtx))
		recTest := httptest.NewRecorder()
		testConnPipeline.ServeHTTP(recTest, reqTest)
		if recTest.Code != http.StatusForbidden {
			t.Errorf("expected 403 on TestConnection for Member, got %d", recTest.Code)
		}

		// Database Discovery -> 403 Forbidden
		reqDisc := httptest.NewRequest(http.MethodGet, "/api/v1/resources/"+resID.String()+"/databases", nil)
		reqDisc.Header.Set("X-Organization-ID", orgA.String())
		reqDisc.SetPathValue("id", resID.String())
		reqDisc = reqDisc.WithContext(identityHttpapi.WithAuthContext(reqDisc.Context(), authCtx))
		recDisc := httptest.NewRecorder()
		discPipeline.ServeHTTP(recDisc, reqDisc)
		if recDisc.Code != http.StatusForbidden {
			t.Errorf("expected 403 on DatabaseDiscovery for Member, got %d", recDisc.Code)
		}
	})

	t.Run("Viewer Org A can read but receives 403 on write and discovery endpoints", func(t *testing.T) {
		authCtx := &identityHttpapi.AuthContext{
			UserID:        userViewerA,
			Status:        identityDomain.UserStatusActive,
			IsSystemAdmin: false,
		}

		// Read List -> 200 OK
		reqList := httptest.NewRequest(http.MethodGet, "/api/v1/resources", nil)
		reqList.Header.Set("X-Organization-ID", orgA.String())
		reqList = reqList.WithContext(identityHttpapi.WithAuthContext(reqList.Context(), authCtx))
		recList := httptest.NewRecorder()
		listPipeline.ServeHTTP(recList, reqList)
		if recList.Code != http.StatusOK {
			t.Errorf("expected 200 for Viewer read list, got %d", recList.Code)
		}

		// Delete -> 403 Forbidden
		reqDelete := httptest.NewRequest(http.MethodDelete, "/api/v1/resources/"+resID.String(), nil)
		reqDelete.Header.Set("X-Organization-ID", orgA.String())
		reqDelete.SetPathValue("id", resID.String())
		reqDelete = reqDelete.WithContext(identityHttpapi.WithAuthContext(reqDelete.Context(), authCtx))
		recDelete := httptest.NewRecorder()
		deletePipeline.ServeHTTP(recDelete, reqDelete)
		if recDelete.Code != http.StatusForbidden {
			t.Errorf("expected 403 on Delete for Viewer, got %d", recDelete.Code)
		}

		// Test Connection -> 403 Forbidden
		reqTest := httptest.NewRequest(http.MethodPost, "/api/v1/resources/"+resID.String()+"/test-connection", nil)
		reqTest.Header.Set("X-Organization-ID", orgA.String())
		reqTest.SetPathValue("id", resID.String())
		reqTest = reqTest.WithContext(identityHttpapi.WithAuthContext(reqTest.Context(), authCtx))
		recTest := httptest.NewRecorder()
		testConnPipeline.ServeHTTP(recTest, reqTest)
		if recTest.Code != http.StatusForbidden {
			t.Errorf("expected 403 on TestConnection for Viewer, got %d", recTest.Code)
		}

		// Database Discovery -> 403 Forbidden
		reqDisc := httptest.NewRequest(http.MethodGet, "/api/v1/resources/"+resID.String()+"/databases", nil)
		reqDisc.Header.Set("X-Organization-ID", orgA.String())
		reqDisc.SetPathValue("id", resID.String())
		reqDisc = reqDisc.WithContext(identityHttpapi.WithAuthContext(reqDisc.Context(), authCtx))
		recDisc := httptest.NewRecorder()
		discPipeline.ServeHTTP(recDisc, reqDisc)
		if recDisc.Code != http.StatusForbidden {
			t.Errorf("expected 403 on DatabaseDiscovery for Viewer, got %d", recDisc.Code)
		}
	})

	t.Run("User attempting to access Org B receives 404 ORGANIZATION_NOT_FOUND", func(t *testing.T) {
		authCtx := &identityHttpapi.AuthContext{
			UserID:        userAdminA,
			Status:        identityDomain.UserStatusActive,
			IsSystemAdmin: false,
		}

		req := httptest.NewRequest(http.MethodGet, "/api/v1/resources", nil)
		req.Header.Set("X-Organization-ID", orgB.String())
		req = req.WithContext(identityHttpapi.WithAuthContext(req.Context(), authCtx))

		rec := httptest.NewRecorder()
		listPipeline.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 for non-member org, got %d", rec.Code)
		}
	})

	t.Run("System Admin without membership in Org A receives 404 (no generic bypass)", func(t *testing.T) {
		sysAdmin := uuid.New()
		authCtx := &identityHttpapi.AuthContext{
			UserID:        sysAdmin,
			Status:        identityDomain.UserStatusActive,
			IsSystemAdmin: true,
		}

		req := httptest.NewRequest(http.MethodGet, "/api/v1/resources", nil)
		req.Header.Set("X-Organization-ID", orgA.String())
		req = req.WithContext(identityHttpapi.WithAuthContext(req.Context(), authCtx))

		rec := httptest.NewRecorder()
		listPipeline.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 for System Admin without membership, got %d", rec.Code)
		}
	})
}
