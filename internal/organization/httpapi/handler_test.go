package httpapi

import (
	"bytes"
	"context"
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
	identityHttpapi "backup-platform/internal/identity/httpapi"
	"backup-platform/internal/organization/domain"
	orgRepo "backup-platform/internal/organization/repository"
	orgService "backup-platform/internal/organization/service"
	"backup-platform/pkg/uuid"
)

type mockOrgService struct {
	createOrgFn func(ctx context.Context, actorUserID uuid.UUID, input orgService.CreateOrganizationInput) (*domain.Organization, error)
	listOrgFn   func(ctx context.Context, userID uuid.UUID) ([]*orgRepo.UserMembershipWithOrg, error)
	getOrgFn    func(ctx context.Context, id uuid.UUID) (*domain.Organization, error)
	updateOrgFn func(ctx context.Context, id uuid.UUID, input orgService.UpdateOrganizationInput) (*domain.Organization, error)
}

func (m *mockOrgService) CreateOrganization(ctx context.Context, actorUserID uuid.UUID, input orgService.CreateOrganizationInput) (*domain.Organization, error) {
	if m.createOrgFn != nil {
		return m.createOrgFn(ctx, actorUserID, input)
	}
	return nil, nil
}

func (m *mockOrgService) ListUserOrganizations(ctx context.Context, userID uuid.UUID) ([]*orgRepo.UserMembershipWithOrg, error) {
	if m.listOrgFn != nil {
		return m.listOrgFn(ctx, userID)
	}
	return nil, nil
}

func (m *mockOrgService) GetActiveOrganization(ctx context.Context, id uuid.UUID) (*domain.Organization, error) {
	if m.getOrgFn != nil {
		return m.getOrgFn(ctx, id)
	}
	return nil, nil
}

func (m *mockOrgService) UpdateOrganization(ctx context.Context, id uuid.UUID, input orgService.UpdateOrganizationInput) (*domain.Organization, error) {
	if m.updateOrgFn != nil {
		return m.updateOrgFn(ctx, id, input)
	}
	return nil, nil
}

func setupHandlerTest() (*Handler, *mockOrgService) {
	svc := &mockOrgService{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := NewHandler(svc, logger)
	return h, svc
}

func TestHandler_List(t *testing.T) {
	h, svc := setupHandlerTest()
	userID := uuid.New()
	authCtx := &identityHttpapi.AuthContext{
		UserID:        userID,
		SessionID:     uuid.New(),
		Email:         "user@example.com",
		FullName:      "Test User",
		IsSystemAdmin: false,
		Status:        identityDomain.UserStatusActive,
	}

	t.Run("returns list of organizations for authenticated user", func(t *testing.T) {
		orgID1 := uuid.New()
		orgID2 := uuid.New()
		now := time.Now().UTC()

		svc.listOrgFn = func(ctx context.Context, uID uuid.UUID) ([]*orgRepo.UserMembershipWithOrg, error) {
			if uID != userID {
				t.Errorf("expected userID %s, got: %s", userID, uID)
			}
			return []*orgRepo.UserMembershipWithOrg{
				{
					OrganizationID:     orgID1,
					OrganizationName:   "Internal Organization",
					Slug:               "internal",
					IsDefaultInternal:  true,
					OrganizationStatus: domain.OrgStatusActive,
					Role:               domain.RoleAdmin,
					Status:             domain.MemberStatusActive,
					CreatedAt:          now,
				},
				{
					OrganizationID:     orgID2,
					OrganizationName:   "Acme Corp",
					Slug:               "acme-corp",
					IsDefaultInternal:  false,
					OrganizationStatus: domain.OrgStatusActive,
					Role:               domain.RoleMember,
					Status:             domain.MemberStatusActive,
					CreatedAt:          now,
				},
			}, nil
		}

		req := httptest.NewRequest("GET", "/api/v1/organizations", nil)
		req = req.WithContext(identityHttpapi.WithAuthContext(req.Context(), authCtx))
		rec := httptest.NewRecorder()

		h.List(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got: %d", rec.Code)
		}
		if rec.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("expected Cache-Control: no-store")
		}

		type listResponseEnvelope struct {
			Data []OrganizationListItemResponse `json:"data"`
		}

		var env listResponseEnvelope
		if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
			t.Fatalf("failed to decode JSON response: %v", err)
		}

		if len(env.Data) != 2 {
			t.Fatalf("expected 2 organizations in response, got: %d", len(env.Data))
		}
		if env.Data[0].Name != "Internal Organization" || env.Data[0].Slug != "internal" || !env.Data[0].IsDefaultInternal || env.Data[0].UserRole != "admin" {
			t.Errorf("unexpected first item: %+v", env.Data[0])
		}
		if env.Data[1].Name != "Acme Corp" || env.Data[1].Slug != "acme-corp" || env.Data[1].IsDefaultInternal || env.Data[1].UserRole != "member" {
			t.Errorf("unexpected second item: %+v", env.Data[1])
		}
	})

	t.Run("returns empty array 200 OK when user has no organizations", func(t *testing.T) {
		svc.listOrgFn = func(ctx context.Context, uID uuid.UUID) ([]*orgRepo.UserMembershipWithOrg, error) {
			return []*orgRepo.UserMembershipWithOrg{}, nil
		}

		req := httptest.NewRequest("GET", "/api/v1/organizations", nil)
		req = req.WithContext(identityHttpapi.WithAuthContext(req.Context(), authCtx))
		rec := httptest.NewRecorder()

		h.List(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got: %d", rec.Code)
		}

		var raw map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &raw)
		dataArr, ok := raw["data"].([]any)
		if !ok || len(dataArr) != 0 {
			t.Errorf("expected empty data array in JSON, got: %v", raw["data"])
		}
	})

	t.Run("fails with 401 when AuthContext is missing", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/organizations", nil)
		rec := httptest.NewRecorder()

		h.List(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("expected 401 Unauthorized, got: %d", rec.Code)
		}
	})

	t.Run("service failure returns generic 503 SERVICE_UNAVAILABLE", func(t *testing.T) {
		svc.listOrgFn = func(ctx context.Context, uID uuid.UUID) ([]*orgRepo.UserMembershipWithOrg, error) {
			return nil, orgService.ErrOrganizationServiceUnavailable
		}

		req := httptest.NewRequest("GET", "/api/v1/organizations", nil)
		req = req.WithContext(identityHttpapi.WithAuthContext(req.Context(), authCtx))
		rec := httptest.NewRecorder()

		h.List(rec, req)

		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("expected 503 Service Unavailable, got: %d", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "SERVICE_UNAVAILABLE") {
			t.Errorf("expected code SERVICE_UNAVAILABLE, got: %s", rec.Body.String())
		}
	})
}

func TestHandler_Create(t *testing.T) {
	h, svc := setupHandlerTest()
	sysAdminID := uuid.New()
	authCtxSysAdmin := &identityHttpapi.AuthContext{
		UserID:        sysAdminID,
		SessionID:     uuid.New(),
		Email:         "admin@example.com",
		FullName:      "System Admin",
		IsSystemAdmin: true,
		Status:        identityDomain.UserStatusActive,
	}

	t.Run("successful organization creation returns 201 Created", func(t *testing.T) {
		createdOrgID := uuid.New()
		now := time.Now().UTC()

		svc.createOrgFn = func(ctx context.Context, actorID uuid.UUID, input orgService.CreateOrganizationInput) (*domain.Organization, error) {
			if actorID != sysAdminID {
				t.Errorf("expected actorID %s, got: %s", sysAdminID, actorID)
			}
			if input.Name != "Acme Corporation" || input.Slug != "acme-corp" {
				t.Errorf("unexpected input: %+v", input)
			}
			return &domain.Organization{
				ID:                createdOrgID,
				Name:              input.Name,
				Slug:              input.Slug,
				IsDefaultInternal: false,
				Status:            domain.OrgStatusActive,
				CreatedAt:         now,
				UpdatedAt:         now,
			}, nil
		}

		body := []byte(`{"name": "Acme Corporation", "slug": "acme-corp", "metadata": {"plan": "standard"}}`)
		req := httptest.NewRequest("POST", "/api/v1/organizations", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(identityHttpapi.WithAuthContext(req.Context(), authCtxSysAdmin))
		rec := httptest.NewRecorder()

		h.Create(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created, got: %d (%s)", rec.Code, rec.Body.String())
		}
		if rec.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("expected Cache-Control: no-store")
		}

		type createResponseEnvelope struct {
			Data CreateOrganizationResponse `json:"data"`
		}
		var env createResponseEnvelope
		if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
			t.Fatalf("failed to decode JSON: %v", err)
		}

		if env.Data.ID != createdOrgID || env.Data.Name != "Acme Corporation" || env.Data.Slug != "acme-corp" || env.Data.IsDefaultInternal {
			t.Errorf("unexpected create response: %+v", env.Data)
		}
	})

	t.Run("rejects unknown fields in JSON payload with 400 Bad Request", func(t *testing.T) {
		unknownFieldPayloads := [][]byte{
			[]byte(`{"name": "Acme", "slug": "acme", "is_default_internal": true}`),
			[]byte(`{"name": "Acme", "slug": "acme", "status": "active"}`),
			[]byte(`{"name": "Acme", "slug": "acme", "user_id": "123"}`),
			[]byte(`{"name": "Acme", "slug": "acme", "extra_field": "hacked"}`),
		}

		for _, p := range unknownFieldPayloads {
			req := httptest.NewRequest("POST", "/api/v1/organizations", bytes.NewReader(p))
			req.Header.Set("Content-Type", "application/json")
			req = req.WithContext(identityHttpapi.WithAuthContext(req.Context(), authCtxSysAdmin))
			rec := httptest.NewRecorder()

			h.Create(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Errorf("expected 400 Bad Request for payload %s, got: %d", string(p), rec.Code)
			}
		}
	})

	t.Run("rejects malformed JSON and wrong content-type with 400", func(t *testing.T) {
		// Wrong content type
		req := httptest.NewRequest("POST", "/api/v1/organizations", bytes.NewReader([]byte(`{"name": "Acme", "slug": "acme"}`)))
		req.Header.Set("Content-Type", "text/plain")
		req = req.WithContext(identityHttpapi.WithAuthContext(req.Context(), authCtxSysAdmin))
		rec := httptest.NewRecorder()
		h.Create(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400 for wrong content-type, got: %d", rec.Code)
		}

		// Malformed JSON
		req = httptest.NewRequest("POST", "/api/v1/organizations", bytes.NewReader([]byte(`{unclosed json`)))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(identityHttpapi.WithAuthContext(req.Context(), authCtxSysAdmin))
		rec = httptest.NewRecorder()
		h.Create(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400 for malformed json, got: %d", rec.Code)
		}
	})

	t.Run("validation failure returns 422 VALIDATION_FAILED with field details", func(t *testing.T) {
		svc.createOrgFn = func(ctx context.Context, actorID uuid.UUID, input orgService.CreateOrganizationInput) (*domain.Organization, error) {
			return nil, domain.ErrInvalidOrgSlug
		}

		body := []byte(`{"name": "Valid Name", "slug": "Invalid_Slug"}`)
		req := httptest.NewRequest("POST", "/api/v1/organizations", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(identityHttpapi.WithAuthContext(req.Context(), authCtxSysAdmin))
		rec := httptest.NewRecorder()

		h.Create(rec, req)

		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("expected 422 Unprocessable Entity, got: %d", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "VALIDATION_FAILED") {
			t.Errorf("expected VALIDATION_FAILED error code, got: %s", rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), `"field":"slug"`) {
			t.Errorf("expected field 'slug' in error details, got: %s", rec.Body.String())
		}
	})

	t.Run("duplicate slug returns 409 ALREADY_EXISTS", func(t *testing.T) {
		svc.createOrgFn = func(ctx context.Context, actorID uuid.UUID, input orgService.CreateOrganizationInput) (*domain.Organization, error) {
			return nil, domain.ErrDuplicateOrgSlug
		}

		body := []byte(`{"name": "Acme Corp", "slug": "acme-corp"}`)
		req := httptest.NewRequest("POST", "/api/v1/organizations", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(identityHttpapi.WithAuthContext(req.Context(), authCtxSysAdmin))
		rec := httptest.NewRecorder()

		h.Create(rec, req)

		if rec.Code != http.StatusConflict {
			t.Fatalf("expected 409 Conflict, got: %d", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "ALREADY_EXISTS") {
			t.Errorf("expected ALREADY_EXISTS error code, got: %s", rec.Body.String())
		}
	})

	t.Run("internal database error returns generic 503 SERVICE_UNAVAILABLE", func(t *testing.T) {
		svc.createOrgFn = func(ctx context.Context, actorID uuid.UUID, input orgService.CreateOrganizationInput) (*domain.Organization, error) {
			return nil, errors.New("pq: disk full connection lost sensitive-data=secret")
		}

		body := []byte(`{"name": "Acme Corp", "slug": "acme-corp"}`)
		req := httptest.NewRequest("POST", "/api/v1/organizations", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(identityHttpapi.WithAuthContext(req.Context(), authCtxSysAdmin))
		rec := httptest.NewRecorder()

		h.Create(rec, req)

		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("expected 503 Service Unavailable, got: %d", rec.Code)
		}
		if strings.Contains(rec.Body.String(), "sensitive-data") || strings.Contains(rec.Body.String(), "pq") {
			t.Errorf("SECURITY FLAW: database details leaked in response: %s", rec.Body.String())
		}
	})

	t.Run("integration with RequireSystemAdmin guard rejects regular user", func(t *testing.T) {
		logger := slog.New(slog.NewTextHandler(io.Discard, nil))
		protectedChain := RequireSystemAdmin(logger)(http.HandlerFunc(h.Create))

		regularUserAuthCtx := &identityHttpapi.AuthContext{
			UserID:        uuid.New(),
			SessionID:     uuid.New(),
			Email:         "regular@example.com",
			FullName:      "Regular User",
			IsSystemAdmin: false, // Not system admin
			Status:        identityDomain.UserStatusActive,
		}

		body := []byte(`{"name": "Acme Corp", "slug": "acme-corp"}`)
		req := httptest.NewRequest("POST", "/api/v1/organizations", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(identityHttpapi.WithAuthContext(req.Context(), regularUserAuthCtx))
		rec := httptest.NewRecorder()

		protectedChain.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 Forbidden for non-system admin, got: %d", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "FORBIDDEN") {
			t.Errorf("expected FORBIDDEN code, got: %s", rec.Body.String())
		}
	})
}

func TestHandler_GetByID(t *testing.T) {
	h, svc := setupHandlerTest()
	orgID := uuid.New()
	userID := uuid.New()
	now := time.Now().UTC()

	authCtx := &identityHttpapi.AuthContext{
		UserID:        userID,
		SessionID:     uuid.New(),
		Email:         "user@example.com",
		FullName:      "Test User",
		IsSystemAdmin: false,
		Status:        identityDomain.UserStatusActive,
	}

	tenantCtx := &TenantContext{
		UserID:            userID,
		OrganizationID:    orgID,
		OrganizationName:  "Acme Corp",
		OrganizationSlug:  "acme-corp",
		IsDefaultInternal: false,
		Role:              domain.RoleAdmin,
		MembershipStatus:  domain.MemberStatusActive,
	}

	t.Run("successfully retrieves active organization detail with JSON object metadata", func(t *testing.T) {
		svc.getOrgFn = func(ctx context.Context, id uuid.UUID) (*domain.Organization, error) {
			if id != orgID {
				t.Errorf("expected id %s, got: %s", orgID, id)
			}
			return &domain.Organization{
				ID:                orgID,
				Name:              "Acme Corporation",
				Slug:              "acme-corp",
				IsDefaultInternal: false,
				Status:            domain.OrgStatusActive,
				Metadata:          []byte(`{"plan":"standard","max_resources":10}`),
				CreatedAt:         now,
				UpdatedAt:         now,
			}, nil
		}

		req := httptest.NewRequest("GET", "/api/v1/organizations/"+orgID.String(), nil)
		req.SetPathValue("id", orgID.String())
		req = req.WithContext(identityHttpapi.WithAuthContext(req.Context(), authCtx))
		req = req.WithContext(WithTenantContext(req.Context(), tenantCtx))
		rec := httptest.NewRecorder()

		h.GetByID(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got: %d (%s)", rec.Code, rec.Body.String())
		}
		if rec.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("expected Cache-Control: no-store")
		}

		type detailEnvelope struct {
			Data OrganizationDetailResponse `json:"data"`
		}
		var env detailEnvelope
		if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
			t.Fatalf("failed to unmarshal detail response: %v", err)
		}

		if env.Data.ID != orgID || env.Data.Name != "Acme Corporation" || env.Data.Slug != "acme-corp" || env.Data.IsDefaultInternal {
			t.Errorf("unexpected detail data: %+v", env.Data)
		}

		var metaObj map[string]any
		if err := json.Unmarshal(env.Data.Metadata, &metaObj); err != nil {
			t.Fatalf("expected metadata to be unmarshaled into JSON object, got error: %v", err)
		}
		if metaObj["plan"] != "standard" || metaObj["max_resources"] != float64(10) {
			t.Errorf("unexpected metadata content: %+v", metaObj)
		}
	})

	t.Run("returns 400 Bad Request on invalid path UUID", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/organizations/not-valid-uuid", nil)
		req.SetPathValue("id", "not-valid-uuid")
		rec := httptest.NewRecorder()

		h.GetByID(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400 Bad Request for malformed UUID, got: %d", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "BAD_REQUEST") {
			t.Errorf("expected BAD_REQUEST error code, got: %s", rec.Body.String())
		}
	})

	t.Run("returns 404 ORGANIZATION_NOT_FOUND when organization does not exist", func(t *testing.T) {
		svc.getOrgFn = func(ctx context.Context, id uuid.UUID) (*domain.Organization, error) {
			return nil, domain.ErrOrgNotFound
		}

		req := httptest.NewRequest("GET", "/api/v1/organizations/"+orgID.String(), nil)
		req.SetPathValue("id", orgID.String())
		rec := httptest.NewRecorder()

		h.GetByID(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Errorf("expected 404 Not Found, got: %d", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "ORGANIZATION_NOT_FOUND") {
			t.Errorf("expected ORGANIZATION_NOT_FOUND code, got: %s", rec.Body.String())
		}
	})

	t.Run("returns 503 SERVICE_UNAVAILABLE on service failure without leaking details", func(t *testing.T) {
		svc.getOrgFn = func(ctx context.Context, id uuid.UUID) (*domain.Organization, error) {
			return nil, errors.New("pq: connection pool exhausted with secret=credential")
		}

		req := httptest.NewRequest("GET", "/api/v1/organizations/"+orgID.String(), nil)
		req.SetPathValue("id", orgID.String())
		rec := httptest.NewRecorder()

		h.GetByID(rec, req)

		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("expected 503 Service Unavailable, got: %d", rec.Code)
		}
		if strings.Contains(rec.Body.String(), "secret=credential") || strings.Contains(rec.Body.String(), "pq") {
			t.Errorf("SECURITY FLAW: database details leaked in response: %s", rec.Body.String())
		}
	})
}

func TestHandler_Update(t *testing.T) {
	h, svc := setupHandlerTest()
	orgID := uuid.New()
	userID := uuid.New()
	createdAt := time.Now().UTC().Add(-2 * time.Hour)
	updatedAt := time.Now().UTC()

	authCtx := &identityHttpapi.AuthContext{
		UserID:        userID,
		SessionID:     uuid.New(),
		Email:         "admin@example.com",
		FullName:      "Admin User",
		IsSystemAdmin: false,
		Status:        identityDomain.UserStatusActive,
	}

	t.Run("successfully updates active organization and returns updated detail", func(t *testing.T) {
		svc.updateOrgFn = func(ctx context.Context, id uuid.UUID, input orgService.UpdateOrganizationInput) (*domain.Organization, error) {
			if id != orgID {
				t.Errorf("expected org ID %s, got: %s", orgID, id)
			}
			if input.Name != "Acme Corporation International" {
				t.Errorf("expected updated name, got: %s", input.Name)
			}
			return &domain.Organization{
				ID:                orgID,
				Name:              "Acme Corporation International",
				Slug:              "acme-corp",
				IsDefaultInternal: false,
				Status:            domain.OrgStatusActive,
				Metadata:          []byte(`{"plan":"enterprise","max_resources":50}`),
				CreatedAt:         createdAt,
				UpdatedAt:         updatedAt,
			}, nil
		}

		body := []byte(`{"name": "Acme Corporation International", "metadata": {"plan": "enterprise", "max_resources": 50}}`)
		req := httptest.NewRequest("PUT", "/api/v1/organizations/"+orgID.String(), bytes.NewReader(body))
		req.SetPathValue("id", orgID.String())
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(identityHttpapi.WithAuthContext(req.Context(), authCtx))
		rec := httptest.NewRecorder()

		h.Update(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got: %d (%s)", rec.Code, rec.Body.String())
		}
		if rec.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("expected Cache-Control: no-store")
		}

		type detailEnvelope struct {
			Data OrganizationDetailResponse `json:"data"`
		}
		var env detailEnvelope
		if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
			t.Fatalf("failed to unmarshal update response: %v", err)
		}

		if env.Data.ID != orgID || env.Data.Name != "Acme Corporation International" || env.Data.Slug != "acme-corp" {
			t.Errorf("unexpected updated data: %+v", env.Data)
		}
		if env.Data.IsDefaultInternal {
			t.Errorf("expected is_default_internal = false")
		}
		if env.Data.Status != "active" {
			t.Errorf("expected status = active")
		}

		var metaObj map[string]any
		if err := json.Unmarshal(env.Data.Metadata, &metaObj); err != nil {
			t.Fatalf("expected metadata to be a JSON object, got error: %v", err)
		}
		if metaObj["plan"] != "enterprise" || metaObj["max_resources"] != float64(50) {
			t.Errorf("unexpected metadata: %+v", metaObj)
		}
	})

	t.Run("returns 400 Bad Request on wrong Content-Type", func(t *testing.T) {
		body := []byte(`{"name": "Acme", "metadata": {}}`)
		req := httptest.NewRequest("PUT", "/api/v1/organizations/"+orgID.String(), bytes.NewReader(body))
		req.SetPathValue("id", orgID.String())
		req.Header.Set("Content-Type", "text/plain")
		rec := httptest.NewRecorder()

		h.Update(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400 Bad Request, got: %d", rec.Code)
		}
	})

	t.Run("returns 400 Bad Request on malformed JSON", func(t *testing.T) {
		body := []byte(`{invalid-json}`)
		req := httptest.NewRequest("PUT", "/api/v1/organizations/"+orgID.String(), bytes.NewReader(body))
		req.SetPathValue("id", orgID.String())
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		h.Update(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400 Bad Request, got: %d", rec.Code)
		}
	})

	t.Run("returns 400 Bad Request when name is missing", func(t *testing.T) {
		body := []byte(`{"metadata": {}}`)
		req := httptest.NewRequest("PUT", "/api/v1/organizations/"+orgID.String(), bytes.NewReader(body))
		req.SetPathValue("id", orgID.String())
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		h.Update(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400 Bad Request for missing name, got: %d", rec.Code)
		}
	})

	t.Run("returns 400 Bad Request when metadata is missing", func(t *testing.T) {
		body := []byte(`{"name": "Acme"}`)
		req := httptest.NewRequest("PUT", "/api/v1/organizations/"+orgID.String(), bytes.NewReader(body))
		req.SetPathValue("id", orgID.String())
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		h.Update(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400 Bad Request for missing metadata, got: %d", rec.Code)
		}
	})

	t.Run("returns 400 Bad Request when client attempts to send immutable fields", func(t *testing.T) {
		forbiddenPayloads := [][]byte{
			[]byte(`{"name": "Acme", "metadata": {}, "slug": "new-slug"}`),
			[]byte(`{"name": "Acme", "metadata": {}, "status": "suspended"}`),
			[]byte(`{"name": "Acme", "metadata": {}, "is_default_internal": true}`),
			[]byte(`{"name": "Acme", "metadata": {}, "id": "12345678-1234-1234-1234-123456789012"}`),
			[]byte(`{"name": "Acme", "metadata": {}, "created_at": "2026-01-01T00:00:00Z"}`),
			[]byte(`{"name": "Acme", "metadata": {}, "role": "admin"}`),
		}

		for _, payload := range forbiddenPayloads {
			req := httptest.NewRequest("PUT", "/api/v1/organizations/"+orgID.String(), bytes.NewReader(payload))
			req.SetPathValue("id", orgID.String())
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()

			h.Update(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Errorf("expected 400 Bad Request for payload %s, got: %d", string(payload), rec.Code)
			}
		}
	})

	t.Run("returns 422 VALIDATION_FAILED on invalid name", func(t *testing.T) {
		svc.updateOrgFn = func(ctx context.Context, id uuid.UUID, input orgService.UpdateOrganizationInput) (*domain.Organization, error) {
			return nil, domain.ErrInvalidOrgName
		}

		body := []byte(`{"name": "", "metadata": {}}`)
		req := httptest.NewRequest("PUT", "/api/v1/organizations/"+orgID.String(), bytes.NewReader(body))
		req.SetPathValue("id", orgID.String())
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		h.Update(rec, req)

		if rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("expected 422 Unprocessable Entity, got: %d", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "VALIDATION_FAILED") {
			t.Errorf("expected VALIDATION_FAILED code, got: %s", rec.Body.String())
		}
	})

	t.Run("returns 422 VALIDATION_FAILED on invalid metadata", func(t *testing.T) {
		svc.updateOrgFn = func(ctx context.Context, id uuid.UUID, input orgService.UpdateOrganizationInput) (*domain.Organization, error) {
			return nil, domain.ErrInvalidMetadata
		}

		body := []byte(`{"name": "Acme", "metadata": "not-an-object"}`)
		req := httptest.NewRequest("PUT", "/api/v1/organizations/"+orgID.String(), bytes.NewReader(body))
		req.SetPathValue("id", orgID.String())
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		h.Update(rec, req)

		if rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("expected 422 Unprocessable Entity, got: %d", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "VALIDATION_FAILED") {
			t.Errorf("expected VALIDATION_FAILED code, got: %s", rec.Body.String())
		}
	})

	t.Run("returns 404 ORGANIZATION_NOT_FOUND when organization does not exist", func(t *testing.T) {
		svc.updateOrgFn = func(ctx context.Context, id uuid.UUID, input orgService.UpdateOrganizationInput) (*domain.Organization, error) {
			return nil, domain.ErrOrgNotFound
		}

		body := []byte(`{"name": "Acme", "metadata": {}}`)
		req := httptest.NewRequest("PUT", "/api/v1/organizations/"+orgID.String(), bytes.NewReader(body))
		req.SetPathValue("id", orgID.String())
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		h.Update(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Errorf("expected 404 Not Found, got: %d", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "ORGANIZATION_NOT_FOUND") {
			t.Errorf("expected ORGANIZATION_NOT_FOUND code, got: %s", rec.Body.String())
		}
	})

	t.Run("returns 503 SERVICE_UNAVAILABLE on service failure without leaking details", func(t *testing.T) {
		svc.updateOrgFn = func(ctx context.Context, id uuid.UUID, input orgService.UpdateOrganizationInput) (*domain.Organization, error) {
			return nil, errors.New("pq: disk full with secret_token=123")
		}

		body := []byte(`{"name": "Acme", "metadata": {}}`)
		req := httptest.NewRequest("PUT", "/api/v1/organizations/"+orgID.String(), bytes.NewReader(body))
		req.SetPathValue("id", orgID.String())
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		h.Update(rec, req)

		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("expected 503 Service Unavailable, got: %d", rec.Code)
		}
		if strings.Contains(rec.Body.String(), "secret_token") || strings.Contains(rec.Body.String(), "pq") {
			t.Errorf("SECURITY FLAW: database details leaked in response: %s", rec.Body.String())
		}
	})
}
