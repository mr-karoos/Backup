package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"backup-platform/internal/connector"
	orgDomain "backup-platform/internal/organization/domain"
	orgHttpapi "backup-platform/internal/organization/httpapi"
	"backup-platform/internal/platform/httpapi"
	"backup-platform/internal/resource/domain"
	"backup-platform/internal/resource/service"
	"backup-platform/pkg/uuid"
)

type fakeDatabaseDiscoveryExecutor struct {
	result      []connector.DatabaseInfo
	err         error
	callCount   int
	capturedOrg uuid.UUID
	capturedID  uuid.UUID
}

func (f *fakeDatabaseDiscoveryExecutor) DiscoverDatabases(ctx context.Context, orgID, resID uuid.UUID) ([]connector.DatabaseInfo, error) {
	f.callCount++
	f.capturedOrg = orgID
	f.capturedID = resID
	if f.err != nil {
		return nil, f.err
	}
	return f.result, nil
}

type fakeResourceService struct {
	createErr   error
	getErr      error
	listErr     error
	updateErr   error
	archiveErr  error
	createdRes  *domain.ResourceWithConnector
	getRes      *domain.ResourceWithConnector
	listItems   []*domain.ResourceWithConnector
	updatedRes  *domain.ResourceWithConnector
	capturedOrg uuid.UUID
	capturedID  uuid.UUID
	createCalls int
	getCalls    int
	listCalls   int
	updateCalls int
	deleteCalls int
}

type fakeConnectionTestExecutor struct {
	result      *service.ConnectionTestResponseData
	err         error
	callCount   int
	capturedOrg uuid.UUID
	capturedID  uuid.UUID
}

func (f *fakeConnectionTestExecutor) TestConnection(ctx context.Context, orgID, resID uuid.UUID) (*service.ConnectionTestResponseData, error) {
	f.callCount++
	f.capturedOrg = orgID
	f.capturedID = resID
	if f.err != nil {
		return nil, f.err
	}
	if f.result != nil {
		return f.result, nil
	}
	return &service.ConnectionTestResponseData{
		Status:    "success",
		LatencyMS: 25,
		CheckedAt: time.Now().UTC(),
		Details:   map[string]any{"auth_method": "password", "server_banner": "OpenSSH"},
	}, nil
}

func (f *fakeResourceService) CreateResource(ctx context.Context, orgID uuid.UUID, input service.CreateResourceInput) (*domain.ResourceWithConnector, error) {
	f.createCalls++
	f.capturedOrg = orgID
	if f.createErr != nil {
		return nil, f.createErr
	}
	if f.createdRes != nil {
		return f.createdRes, nil
	}
	now := time.Now().UTC()
	return &domain.ResourceWithConnector{
		Resource: &domain.Resource{
			ID:             uuid.New(),
			OrganizationID: orgID,
			Name:           input.Name,
			Type:           input.Type,
			Status:         domain.StatusActive,
			CreatedAt:      now,
			UpdatedAt:      now,
		},
		Connector: &domain.ResourceConnector{
			ID:                 uuid.New(),
			OrganizationID:     orgID,
			ResourceID:         uuid.New(),
			ConnectorType:      domain.ConnectorType(input.Type),
			CredentialID:       input.Connector.CredentialID,
			Host:               input.Connector.Host,
			Port:               input.Connector.Port,
			AuthType:           input.Connector.AuthType,
			HostKeyFingerprint: input.Connector.HostKeyFingerprint,
			Config: domain.ConnectorConfig{
				Username:                 input.Connector.Username,
				ConnectionTimeoutSeconds: input.Connector.ConnectionTimeout,
				UseHTTPS:                 input.Connector.UseHTTPS,
			},
			CreatedAt: now,
			UpdatedAt: now,
		},
		CredentialName: "Test Credential",
	}, nil
}

func (f *fakeResourceService) GetResource(ctx context.Context, orgID, resID uuid.UUID) (*domain.ResourceWithConnector, error) {
	f.getCalls++
	f.capturedOrg = orgID
	f.capturedID = resID
	if f.getErr != nil {
		return nil, f.getErr
	}
	if f.getRes != nil {
		return f.getRes, nil
	}
	return nil, domain.ErrResourceNotFound
}

func (f *fakeResourceService) ListResources(ctx context.Context, orgID uuid.UUID) ([]*domain.ResourceWithConnector, error) {
	f.listCalls++
	f.capturedOrg = orgID
	if f.listErr != nil {
		return nil, f.listErr
	}
	if f.listItems != nil {
		return f.listItems, nil
	}
	return []*domain.ResourceWithConnector{}, nil
}

func (f *fakeResourceService) UpdateResource(ctx context.Context, orgID, resID uuid.UUID, input service.UpdateResourceInput) (*domain.ResourceWithConnector, error) {
	f.updateCalls++
	f.capturedOrg = orgID
	f.capturedID = resID
	if f.updateErr != nil {
		return nil, f.updateErr
	}
	if f.updatedRes != nil {
		return f.updatedRes, nil
	}
	now := time.Now().UTC()
	return &domain.ResourceWithConnector{
		Resource: &domain.Resource{
			ID:             resID,
			OrganizationID: orgID,
			Name:           input.Name,
			Type:           domain.TypeUbuntuSSH,
			Status:         domain.StatusActive,
			CreatedAt:      now,
			UpdatedAt:      now,
		},
		Connector: &domain.ResourceConnector{
			ID:                 uuid.New(),
			OrganizationID:     orgID,
			ResourceID:         resID,
			ConnectorType:      domain.ConnectorTypeUbuntuSSH,
			CredentialID:       input.Connector.CredentialID,
			Host:               input.Connector.Host,
			Port:               input.Connector.Port,
			AuthType:           input.Connector.AuthType,
			HostKeyFingerprint: input.Connector.HostKeyFingerprint,
			Config: domain.ConnectorConfig{
				Username:                 input.Connector.Username,
				ConnectionTimeoutSeconds: input.Connector.ConnectionTimeout,
				UseHTTPS:                 input.Connector.UseHTTPS,
			},
			CreatedAt: now,
			UpdatedAt: now,
		},
		CredentialName: "Updated Credential",
	}, nil
}

func (f *fakeResourceService) ArchiveResource(ctx context.Context, orgID, resID uuid.UUID) error {
	f.deleteCalls++
	f.capturedOrg = orgID
	f.capturedID = resID
	if f.archiveErr != nil {
		return f.archiveErr
	}
	return nil
}

func attachTenantContext(r *http.Request, orgID uuid.UUID, role orgDomain.Role) *http.Request {
	tenantCtx := &orgHttpapi.TenantContext{
		OrganizationID:    orgID,
		UserID:            uuid.New(),
		OrganizationName:  "Acme Corp",
		OrganizationSlug:  "acme-corp",
		IsDefaultInternal: false,
		Role:              role,
		MembershipStatus:  orgDomain.MemberStatusActive,
	}
	return r.WithContext(orgHttpapi.WithTenantContext(r.Context(), tenantCtx))
}

func sampleResourceWithConnector(orgID, resID, credID uuid.UUID) *domain.ResourceWithConnector {
	now := time.Now().UTC()
	fp := "SHA256:abcd1234efgh"
	timeout := 15
	return &domain.ResourceWithConnector{
		Resource: &domain.Resource{
			ID:                   resID,
			OrganizationID:       orgID,
			Name:                 "Production Server",
			Type:                 domain.TypeUbuntuSSH,
			Status:               domain.StatusActive,
			LastConnectionTestAt: &now,
			CreatedAt:            now,
			UpdatedAt:            now,
		},
		Connector: &domain.ResourceConnector{
			ID:                 uuid.New(),
			OrganizationID:     orgID,
			ResourceID:         resID,
			ConnectorType:      domain.ConnectorTypeUbuntuSSH,
			CredentialID:       credID,
			Host:               "198.51.100.10",
			Port:               22,
			AuthType:           domain.AuthTypeSSHKey,
			HostKeyFingerprint: &fp,
			Config: domain.ConnectorConfig{
				Username:                 "root",
				ConnectionTimeoutSeconds: &timeout,
			},
			CreatedAt: now,
			UpdatedAt: now,
		},
		CredentialName: "Prod SSH Key",
	}
}

func TestRoleBasedVisibility(t *testing.T) {
	orgID := uuid.New()
	resID := uuid.New()
	credID := uuid.New()
	item := sampleResourceWithConnector(orgID, resID, credID)

	t.Run("Admin sees full allowed connector metadata without secrets", func(t *testing.T) {
		resp := MapResourceToResponse(item, orgDomain.RoleAdmin)
		if resp.Connector == nil {
			t.Fatalf("expected connector for admin")
		}
		if resp.Connector.Host != "198.51.100.10" || resp.Connector.Port != 22 {
			t.Errorf("unexpected host/port: %s:%d", resp.Connector.Host, resp.Connector.Port)
		}
		if resp.Connector.Username != "root" || resp.Connector.AuthType != "ssh_key" {
			t.Errorf("unexpected username/auth_type: %s/%s", resp.Connector.Username, resp.Connector.AuthType)
		}
		if resp.Connector.CredentialID != credID.String() || resp.Connector.CredentialName != "Prod SSH Key" {
			t.Errorf("unexpected credential ref: %s/%s", resp.Connector.CredentialID, resp.Connector.CredentialName)
		}
		if resp.Connector.HostKeyFingerprint == nil || *resp.Connector.HostKeyFingerprint != "SHA256:abcd1234efgh" {
			t.Errorf("unexpected host key fingerprint")
		}
	})

	t.Run("Member sees only host and port", func(t *testing.T) {
		resp := MapResourceToResponse(item, orgDomain.RoleMember)
		if resp.Connector == nil {
			t.Fatalf("expected connector for member")
		}
		if resp.Connector.Host != "198.51.100.10" || resp.Connector.Port != 22 {
			t.Errorf("unexpected host/port: %s:%d", resp.Connector.Host, resp.Connector.Port)
		}
		if resp.Connector.Username != "" {
			t.Errorf("SECURITY FLAW: member sees username %s", resp.Connector.Username)
		}
		if resp.Connector.AuthType != "" {
			t.Errorf("SECURITY FLAW: member sees auth_type %s", resp.Connector.AuthType)
		}
		if resp.Connector.CredentialID != "" || resp.Connector.CredentialName != "" {
			t.Errorf("SECURITY FLAW: member sees credential info")
		}
		if resp.Connector.HostKeyFingerprint != nil {
			t.Errorf("SECURITY FLAW: member sees host key fingerprint")
		}
	})

	t.Run("Viewer sees no connector network object", func(t *testing.T) {
		resp := MapResourceToResponse(item, orgDomain.RoleViewer)
		if resp.Connector != nil {
			t.Errorf("SECURITY FLAW: viewer received connector object: %+v", resp.Connector)
		}
		if resp.Name != "Production Server" || resp.Type != "ubuntu_ssh" {
			t.Errorf("unexpected public metadata: %+v", resp)
		}
	})
}

func TestSecretKeywordRegression(t *testing.T) {
	orgID := uuid.New()
	resID := uuid.New()
	credID := uuid.New()
	item := sampleResourceWithConnector(orgID, resID, credID)

	roles := []orgDomain.Role{orgDomain.RoleAdmin, orgDomain.RoleMember, orgDomain.RoleViewer}
	forbiddenKeywords := []string{
		`"secret"`,
		`"password"`,
		`"token"`,
		`"private_key"`,
		`"encrypted_secret"`,
		`"nonce"`,
		`"auth_tag"`,
	}

	for _, role := range roles {
		t.Run("Role "+string(role), func(t *testing.T) {
			resp := MapResourceToResponse(item, role)
			jsonBytes, err := json.Marshal(resp)
			if err != nil {
				t.Fatalf("marshal failed: %v", err)
			}
			jsonStr := strings.ToLower(string(jsonBytes))

			for _, kw := range forbiddenKeywords {
				if strings.Contains(jsonStr, kw) {
					t.Errorf("SECURITY FLAW: forbidden keyword %s found in JSON for role %s: %s", kw, role, jsonStr)
				}
			}
		})
	}
}

func TestResourceHandler_List(t *testing.T) {
	orgID := uuid.New()
	resID := uuid.New()
	credID := uuid.New()
	item := sampleResourceWithConnector(orgID, resID, credID)

	t.Run("Admin receives 200 OK with full visibility", func(t *testing.T) {
		svc := &fakeResourceService{
			listItems: []*domain.ResourceWithConnector{item},
		}
		h := NewHandler(svc, nil, nil, nil)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/resources", nil)
		req = attachTenantContext(req, orgID, orgDomain.RoleAdmin)

		rec := httptest.NewRecorder()
		h.List(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", rec.Code)
		}
		var resp struct {
			Data []ResourceResponse `json:"data"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if len(resp.Data) != 1 {
			t.Fatalf("expected 1 item, got %d", len(resp.Data))
		}
		if resp.Data[0].Connector.Username != "root" {
			t.Errorf("expected admin to see username 'root'")
		}
	})

	t.Run("Member receives 200 OK with restricted connector", func(t *testing.T) {
		svc := &fakeResourceService{
			listItems: []*domain.ResourceWithConnector{item},
		}
		h := NewHandler(svc, nil, nil, nil)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/resources", nil)
		req = attachTenantContext(req, orgID, orgDomain.RoleMember)

		rec := httptest.NewRecorder()
		h.List(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", rec.Code)
		}
		var resp struct {
			Data []ResourceResponse `json:"data"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.Data[0].Connector.Username != "" {
			t.Errorf("member should not see username")
		}
		if resp.Data[0].Connector.Host != "198.51.100.10" {
			t.Errorf("member should see host")
		}
	})

	t.Run("Viewer receives 200 OK without connector", func(t *testing.T) {
		svc := &fakeResourceService{
			listItems: []*domain.ResourceWithConnector{item},
		}
		h := NewHandler(svc, nil, nil, nil)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/resources", nil)
		req = attachTenantContext(req, orgID, orgDomain.RoleViewer)

		rec := httptest.NewRecorder()
		h.List(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", rec.Code)
		}
		var resp struct {
			Data []ResourceResponse `json:"data"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.Data[0].Connector != nil {
			t.Errorf("viewer should not see connector")
		}
	})

	t.Run("Empty list returns 200 OK with []", func(t *testing.T) {
		svc := &fakeResourceService{
			listItems: []*domain.ResourceWithConnector{},
		}
		h := NewHandler(svc, nil, nil, nil)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/resources", nil)
		req = attachTenantContext(req, orgID, orgDomain.RoleAdmin)

		rec := httptest.NewRecorder()
		h.List(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", rec.Code)
		}
	})

	t.Run("Service error returns 503 SERVICE_UNAVAILABLE", func(t *testing.T) {
		svc := &fakeResourceService{
			listErr: domain.ErrResourceServiceUnavailable,
		}
		h := NewHandler(svc, nil, nil, nil)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/resources", nil)
		req = attachTenantContext(req, orgID, orgDomain.RoleAdmin)

		rec := httptest.NewRecorder()
		h.List(rec, req)

		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("expected 503, got %d", rec.Code)
		}
	})
}

func TestResourceHandler_GetByID(t *testing.T) {
	orgID := uuid.New()
	resID := uuid.New()
	credID := uuid.New()
	item := sampleResourceWithConnector(orgID, resID, credID)

	t.Run("Valid request returns 200 OK", func(t *testing.T) {
		svc := &fakeResourceService{
			getRes: item,
		}
		h := NewHandler(svc, nil, nil, nil)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/resources/"+resID.String(), nil)
		req.SetPathValue("id", resID.String())
		req = attachTenantContext(req, orgID, orgDomain.RoleAdmin)

		rec := httptest.NewRecorder()
		h.GetByID(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", rec.Code)
		}
	})

	t.Run("Invalid path UUID returns 400 BAD_REQUEST", func(t *testing.T) {
		svc := &fakeResourceService{}
		h := NewHandler(svc, nil, nil, nil)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/resources/bad-id", nil)
		req.SetPathValue("id", "bad-id")
		req = attachTenantContext(req, orgID, orgDomain.RoleAdmin)

		rec := httptest.NewRecorder()
		h.GetByID(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", rec.Code)
		}
	})

	t.Run("Not found or archived returns 404 RESOURCE_NOT_FOUND", func(t *testing.T) {
		svc := &fakeResourceService{
			getErr: domain.ErrResourceNotFound,
		}
		h := NewHandler(svc, nil, nil, nil)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/resources/"+resID.String(), nil)
		req.SetPathValue("id", resID.String())
		req = attachTenantContext(req, orgID, orgDomain.RoleAdmin)

		rec := httptest.NewRecorder()
		h.GetByID(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", rec.Code)
		}
		var errEnv httpapi.ErrorEnvelope
		_ = json.Unmarshal(rec.Body.Bytes(), &errEnv)
		if errEnv.Error.Code != "RESOURCE_NOT_FOUND" {
			t.Errorf("expected code RESOURCE_NOT_FOUND, got %s", errEnv.Error.Code)
		}
	})

	t.Run("Internal storage corruption returns 503 SERVICE_UNAVAILABLE", func(t *testing.T) {
		svc := &fakeResourceService{
			getErr: domain.ErrResourceServiceUnavailable,
		}
		h := NewHandler(svc, nil, nil, nil)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/resources/"+resID.String(), nil)
		req.SetPathValue("id", resID.String())
		req = attachTenantContext(req, orgID, orgDomain.RoleAdmin)

		rec := httptest.NewRecorder()
		h.GetByID(rec, req)

		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("expected 503, got %d", rec.Code)
		}
	})
}

func TestResourceHandler_Create(t *testing.T) {
	orgID := uuid.New()
	credID := uuid.New()

	validUbuntuBody := func() map[string]any {
		return map[string]any{
			"name": "Ubuntu App Server",
			"type": "ubuntu_ssh",
			"connector": map[string]any{
				"host":                 "198.51.100.15",
				"port":                 22,
				"auth_type":            "ssh_key",
				"username":             "root",
				"credential_id":        credID.String(),
				"host_key_fingerprint": "SHA256:abc1234",
				"config": map[string]any{
					"connection_timeout_seconds": 15,
				},
			},
		}
	}

	validCPanelBody := func() map[string]any {
		return map[string]any{
			"name": "cPanel Shared Host",
			"type": "cpanel",
			"connector": map[string]any{
				"host":          "cpanel.example.com",
				"port":          2083,
				"auth_type":     "cpanel_api_token",
				"username":      "mycpanel",
				"credential_id": credID.String(),
				"config": map[string]any{
					"use_https":                  true,
					"connection_timeout_seconds": 20,
				},
			},
		}
	}

	t.Run("Valid Ubuntu SSH request creates resource with 201 Created", func(t *testing.T) {
		svc := &fakeResourceService{}
		h := NewHandler(svc, nil, nil, nil)

		jsonBytes, _ := json.Marshal(validUbuntuBody())
		req := httptest.NewRequest(http.MethodPost, "/api/v1/resources", bytes.NewReader(jsonBytes))
		req.Header.Set("Content-Type", "application/json")
		req = attachTenantContext(req, orgID, orgDomain.RoleAdmin)

		rec := httptest.NewRecorder()
		h.Create(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created, got %d. Body: %s", rec.Code, rec.Body.String())
		}
		var resp struct {
			Data ResourceCreateResponse `json:"data"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.Data.Name != "Ubuntu App Server" || resp.Data.Type != "ubuntu_ssh" {
			t.Errorf("unexpected create response: %+v", resp.Data)
		}
	})

	t.Run("Valid cPanel request creates resource with 201 Created", func(t *testing.T) {
		svc := &fakeResourceService{}
		h := NewHandler(svc, nil, nil, nil)

		jsonBytes, _ := json.Marshal(validCPanelBody())
		req := httptest.NewRequest(http.MethodPost, "/api/v1/resources", bytes.NewReader(jsonBytes))
		req.Header.Set("Content-Type", "application/json")
		req = attachTenantContext(req, orgID, orgDomain.RoleAdmin)

		rec := httptest.NewRecorder()
		h.Create(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created, got %d. Body: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("Rejects missing connector with 400 BAD_REQUEST", func(t *testing.T) {
		svc := &fakeResourceService{}
		h := NewHandler(svc, nil, nil, nil)

		body := map[string]any{
			"name": "Missing Connector",
			"type": "ubuntu_ssh",
		}
		jsonBytes, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/resources", bytes.NewReader(jsonBytes))
		req.Header.Set("Content-Type", "application/json")
		req = attachTenantContext(req, orgID, orgDomain.RoleAdmin)

		rec := httptest.NewRecorder()
		h.Create(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for missing connector, got %d", rec.Code)
		}
	})

	t.Run("Rejects missing name with 400 BAD_REQUEST", func(t *testing.T) {
		svc := &fakeResourceService{}
		h := NewHandler(svc, nil, nil, nil)

		body := validUbuntuBody()
		delete(body, "name")

		jsonBytes, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/resources", bytes.NewReader(jsonBytes))
		req.Header.Set("Content-Type", "application/json")
		req = attachTenantContext(req, orgID, orgDomain.RoleAdmin)

		rec := httptest.NewRecorder()
		h.Create(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for missing name, got %d", rec.Code)
		}
	})

	t.Run("Rejects missing type with 400 BAD_REQUEST", func(t *testing.T) {
		svc := &fakeResourceService{}
		h := NewHandler(svc, nil, nil, nil)

		body := validUbuntuBody()
		delete(body, "type")

		jsonBytes, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/resources", bytes.NewReader(jsonBytes))
		req.Header.Set("Content-Type", "application/json")
		req = attachTenantContext(req, orgID, orgDomain.RoleAdmin)

		rec := httptest.NewRecorder()
		h.Create(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for missing type, got %d", rec.Code)
		}
	})

	t.Run("Rejects invalid credential UUID with 422 VALIDATION_FAILED", func(t *testing.T) {
		svc := &fakeResourceService{}
		h := NewHandler(svc, nil, nil, nil)

		body := validUbuntuBody()
		body["connector"].(map[string]any)["credential_id"] = "not-a-uuid"

		jsonBytes, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/resources", bytes.NewReader(jsonBytes))
		req.Header.Set("Content-Type", "application/json")
		req = attachTenantContext(req, orgID, orgDomain.RoleAdmin)

		rec := httptest.NewRecorder()
		h.Create(rec, req)

		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("expected 422 for invalid credential UUID, got %d", rec.Code)
		}
	})

	t.Run("Rejects unknown top-level field with 400 BAD_REQUEST", func(t *testing.T) {
		svc := &fakeResourceService{}
		h := NewHandler(svc, nil, nil, nil)

		body := validUbuntuBody()
		body["status"] = "active" // Server-managed field

		jsonBytes, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/resources", bytes.NewReader(jsonBytes))
		req.Header.Set("Content-Type", "application/json")
		req = attachTenantContext(req, orgID, orgDomain.RoleAdmin)

		rec := httptest.NewRecorder()
		h.Create(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for unknown field 'status', got %d", rec.Code)
		}
	})

	t.Run("Rejects unknown connector field with 400 BAD_REQUEST", func(t *testing.T) {
		svc := &fakeResourceService{}
		h := NewHandler(svc, nil, nil, nil)

		body := validUbuntuBody()
		body["connector"].(map[string]any)["connector_type"] = "ubuntu_ssh" // Must be derived, not passed

		jsonBytes, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/resources", bytes.NewReader(jsonBytes))
		req.Header.Set("Content-Type", "application/json")
		req = attachTenantContext(req, orgID, orgDomain.RoleAdmin)

		rec := httptest.NewRecorder()
		h.Create(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for client-supplied 'connector_type', got %d", rec.Code)
		}
	})

	t.Run("Rejects unknown config field with 400 BAD_REQUEST (prevents secret injection in config)", func(t *testing.T) {
		svc := &fakeResourceService{}
		h := NewHandler(svc, nil, nil, nil)

		body := validUbuntuBody()
		body["connector"].(map[string]any)["config"].(map[string]any)["password"] = "leak"

		jsonBytes, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/resources", bytes.NewReader(jsonBytes))
		req.Header.Set("Content-Type", "application/json")
		req = attachTenantContext(req, orgID, orgDomain.RoleAdmin)

		rec := httptest.NewRecorder()
		h.Create(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for unknown config field 'password', got %d", rec.Code)
		}
	})

	t.Run("Maps ErrInvalidConnectorConfig to 422 VALIDATION_FAILED", func(t *testing.T) {
		svc := &fakeResourceService{
			createErr: domain.ErrInvalidConnectorConfig,
		}
		h := NewHandler(svc, nil, nil, nil)

		jsonBytes, _ := json.Marshal(validUbuntuBody())
		req := httptest.NewRequest(http.MethodPost, "/api/v1/resources", bytes.NewReader(jsonBytes))
		req.Header.Set("Content-Type", "application/json")
		req = attachTenantContext(req, orgID, orgDomain.RoleAdmin)

		rec := httptest.NewRecorder()
		h.Create(rec, req)

		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("expected 422 for ErrInvalidConnectorConfig, got %d", rec.Code)
		}
	})

	t.Run("Maps validation error to 422 VALIDATION_FAILED", func(t *testing.T) {
		svc := &fakeResourceService{
			createErr: domain.ErrInvalidCredentialReference,
		}
		h := NewHandler(svc, nil, nil, nil)

		jsonBytes, _ := json.Marshal(validUbuntuBody())
		req := httptest.NewRequest(http.MethodPost, "/api/v1/resources", bytes.NewReader(jsonBytes))
		req.Header.Set("Content-Type", "application/json")
		req = attachTenantContext(req, orgID, orgDomain.RoleAdmin)

		rec := httptest.NewRecorder()
		h.Create(rec, req)

		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("expected 422 for credential mismatch, got %d", rec.Code)
		}
	})
}

func TestResourceHandler_Update(t *testing.T) {
	orgID := uuid.New()
	resID := uuid.New()
	credID := uuid.New()

	validUpdateBody := func() map[string]any {
		return map[string]any{
			"name": "Updated Server Name",
			"connector": map[string]any{
				"host":                 "198.51.100.20",
				"port":                 2222,
				"auth_type":            "ssh_key",
				"username":             "deploy",
				"credential_id":        credID.String(),
				"host_key_fingerprint": "SHA256:xyz9876",
			},
		}
	}

	t.Run("Valid update returns 200 OK", func(t *testing.T) {
		svc := &fakeResourceService{}
		h := NewHandler(svc, nil, nil, nil)

		jsonBytes, _ := json.Marshal(validUpdateBody())
		req := httptest.NewRequest(http.MethodPut, "/api/v1/resources/"+resID.String(), bytes.NewReader(jsonBytes))
		req.Header.Set("Content-Type", "application/json")
		req.SetPathValue("id", resID.String())
		req = attachTenantContext(req, orgID, orgDomain.RoleAdmin)

		rec := httptest.NewRecorder()
		h.Update(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d. Body: %s", rec.Code, rec.Body.String())
		}
		var resp struct {
			Data ResourceUpdateResponse `json:"data"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.Data.Name != "Updated Server Name" {
			t.Errorf("expected updated name, got %s", resp.Data.Name)
		}
	})

	t.Run("Missing name returns 400 BAD_REQUEST", func(t *testing.T) {
		svc := &fakeResourceService{}
		h := NewHandler(svc, nil, nil, nil)

		body := map[string]any{
			"connector": validUpdateBody()["connector"],
		}
		jsonBytes, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPut, "/api/v1/resources/"+resID.String(), bytes.NewReader(jsonBytes))
		req.Header.Set("Content-Type", "application/json")
		req.SetPathValue("id", resID.String())
		req = attachTenantContext(req, orgID, orgDomain.RoleAdmin)

		rec := httptest.NewRecorder()
		h.Update(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for missing name, got %d", rec.Code)
		}
	})

	t.Run("Missing connector returns 400 BAD_REQUEST", func(t *testing.T) {
		svc := &fakeResourceService{}
		h := NewHandler(svc, nil, nil, nil)

		body := map[string]any{
			"name": "Server Name",
		}
		jsonBytes, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPut, "/api/v1/resources/"+resID.String(), bytes.NewReader(jsonBytes))
		req.Header.Set("Content-Type", "application/json")
		req.SetPathValue("id", resID.String())
		req = attachTenantContext(req, orgID, orgDomain.RoleAdmin)

		rec := httptest.NewRecorder()
		h.Update(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for missing connector, got %d", rec.Code)
		}
	})

	t.Run("Name present but empty returns 422 VALIDATION_FAILED", func(t *testing.T) {
		svc := &fakeResourceService{
			updateErr: domain.ErrInvalidResourceName,
		}
		h := NewHandler(svc, nil, nil, nil)

		body := validUpdateBody()
		body["name"] = ""

		jsonBytes, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPut, "/api/v1/resources/"+resID.String(), bytes.NewReader(jsonBytes))
		req.Header.Set("Content-Type", "application/json")
		req.SetPathValue("id", resID.String())
		req = attachTenantContext(req, orgID, orgDomain.RoleAdmin)

		rec := httptest.NewRecorder()
		h.Update(rec, req)

		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("expected 422 for empty name, got %d", rec.Code)
		}
	})

	t.Run("Rejects immutable 'type' field in PUT with 400 BAD_REQUEST", func(t *testing.T) {
		svc := &fakeResourceService{}
		h := NewHandler(svc, nil, nil, nil)

		body := validUpdateBody()
		body["type"] = "cpanel"

		jsonBytes, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPut, "/api/v1/resources/"+resID.String(), bytes.NewReader(jsonBytes))
		req.Header.Set("Content-Type", "application/json")
		req.SetPathValue("id", resID.String())
		req = attachTenantContext(req, orgID, orgDomain.RoleAdmin)

		rec := httptest.NewRecorder()
		h.Update(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for immutable 'type' field in PUT, got %d", rec.Code)
		}
	})

	t.Run("Returns 404 RESOURCE_NOT_FOUND when updating nonexistent or archived resource", func(t *testing.T) {
		svc := &fakeResourceService{
			updateErr: domain.ErrResourceNotFound,
		}
		h := NewHandler(svc, nil, nil, nil)

		jsonBytes, _ := json.Marshal(validUpdateBody())
		req := httptest.NewRequest(http.MethodPut, "/api/v1/resources/"+resID.String(), bytes.NewReader(jsonBytes))
		req.Header.Set("Content-Type", "application/json")
		req.SetPathValue("id", resID.String())
		req = attachTenantContext(req, orgID, orgDomain.RoleAdmin)

		rec := httptest.NewRecorder()
		h.Update(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", rec.Code)
		}
	})
}

func TestResourceHandler_Delete(t *testing.T) {
	orgID := uuid.New()
	resID := uuid.New()

	t.Run("Successfully archives resource with 204 No Content and empty body", func(t *testing.T) {
		svc := &fakeResourceService{}
		h := NewHandler(svc, nil, nil, nil)

		req := httptest.NewRequest(http.MethodDelete, "/api/v1/resources/"+resID.String(), nil)
		req.SetPathValue("id", resID.String())
		req = attachTenantContext(req, orgID, orgDomain.RoleAdmin)

		rec := httptest.NewRecorder()
		h.Delete(rec, req)

		if rec.Code != http.StatusNoContent {
			t.Fatalf("expected 204 No Content, got %d", rec.Code)
		}
		if rec.Body.Len() != 0 {
			t.Errorf("expected empty body for 204, got: %s", rec.Body.String())
		}
		if rec.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("expected Cache-Control: no-store header")
		}
	})

	t.Run("Returns 404 RESOURCE_NOT_FOUND when deleting nonexistent resource", func(t *testing.T) {
		svc := &fakeResourceService{
			archiveErr: domain.ErrResourceNotFound,
		}
		h := NewHandler(svc, nil, nil, nil)

		req := httptest.NewRequest(http.MethodDelete, "/api/v1/resources/"+resID.String(), nil)
		req.SetPathValue("id", resID.String())
		req = attachTenantContext(req, orgID, orgDomain.RoleAdmin)

		rec := httptest.NewRecorder()
		h.Delete(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", rec.Code)
		}
	})
}

func TestResourceHandler_TestConnection(t *testing.T) {
	orgID := uuid.New()
	resID := uuid.New()

	t.Run("Admin receives 200 OK with success result", func(t *testing.T) {
		testSvc := &fakeConnectionTestExecutor{
			result: &service.ConnectionTestResponseData{
				Status:    "success",
				LatencyMS: 38,
				CheckedAt: time.Now().UTC(),
				Details: map[string]any{
					"server_banner": "SSH-2.0-OpenSSH_8.9",
					"auth_method":   "password",
				},
			},
		}
		h := NewHandler(&fakeResourceService{}, testSvc, nil, nil)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/resources/"+resID.String()+"/test-connection", nil)
		req.SetPathValue("id", resID.String())
		req = attachTenantContext(req, orgID, orgDomain.RoleAdmin)

		rec := httptest.NewRecorder()
		h.TestConnection(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got: %d", rec.Code)
		}
		if rec.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("expected Cache-Control: no-store header")
		}

		var envelope httpapi.ResponseEnvelope
		if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
			t.Fatalf("failed to decode response envelope: %v", err)
		}
		if envelope.Message != "connection test succeeded" {
			t.Errorf("expected message 'connection test succeeded', got: %s", envelope.Message)
		}

		dataMap, ok := envelope.Data.(map[string]any)
		if !ok {
			t.Fatalf("expected data map, got: %T", envelope.Data)
		}
		if dataMap["status"] != "success" {
			t.Errorf("expected data.status 'success', got: %v", dataMap["status"])
		}
		if dataMap["latency_ms"] != float64(38) {
			t.Errorf("expected latency_ms 38, got: %v", dataMap["latency_ms"])
		}
	})

	t.Run("Admin receives 200 OK with failed probe result", func(t *testing.T) {
		testSvc := &fakeConnectionTestExecutor{
			result: &service.ConnectionTestResponseData{
				Status:    "failed",
				LatencyMS: 150,
				CheckedAt: time.Now().UTC(),
				Details: map[string]any{
					"reason": "authentication failed",
				},
			},
		}
		h := NewHandler(&fakeResourceService{}, testSvc, nil, nil)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/resources/"+resID.String()+"/test-connection", nil)
		req.SetPathValue("id", resID.String())
		req = attachTenantContext(req, orgID, orgDomain.RoleAdmin)

		rec := httptest.NewRecorder()
		h.TestConnection(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got: %d", rec.Code)
		}

		var envelope httpapi.ResponseEnvelope
		if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
			t.Fatalf("failed to decode response envelope: %v", err)
		}
		if envelope.Message != "connection test failed" {
			t.Errorf("expected message 'connection test failed', got: %s", envelope.Message)
		}

		dataMap, ok := envelope.Data.(map[string]any)
		if !ok {
			t.Fatalf("expected data map, got: %T", envelope.Data)
		}
		if dataMap["status"] != "failed" {
			t.Errorf("expected data.status 'failed', got: %v", dataMap["status"])
		}
		details, _ := dataMap["details"].(map[string]any)
		if details["reason"] != "authentication failed" {
			t.Errorf("expected details.reason 'authentication failed', got: %v", details["reason"])
		}
	})

	t.Run("Rejects non-empty request body with 400 BAD_REQUEST", func(t *testing.T) {
		testSvc := &fakeConnectionTestExecutor{}
		h := NewHandler(&fakeResourceService{}, testSvc, nil, nil)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/resources/"+resID.String()+"/test-connection", strings.NewReader(`{"override_host":"10.0.0.1"}`))
		req.SetPathValue("id", resID.String())
		req = attachTenantContext(req, orgID, orgDomain.RoleAdmin)

		rec := httptest.NewRecorder()
		h.TestConnection(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 BAD_REQUEST for non-empty body, got %d", rec.Code)
		}
		if testSvc.callCount != 0 {
			t.Errorf("service should not be called when request body is non-empty")
		}
	})

	t.Run("Rejects bypass attempt with 1024+ whitespace bytes followed by payload with 400 BAD_REQUEST", func(t *testing.T) {
		testSvc := &fakeConnectionTestExecutor{}
		h := NewHandler(&fakeResourceService{}, testSvc, nil, nil)

		bypassBody := strings.Repeat(" ", 2048) + `{"override_host":"10.0.0.1"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/resources/"+resID.String()+"/test-connection", strings.NewReader(bypassBody))
		req.SetPathValue("id", resID.String())
		req = attachTenantContext(req, orgID, orgDomain.RoleAdmin)

		rec := httptest.NewRecorder()
		h.TestConnection(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 BAD_REQUEST for bypass payload, got %d", rec.Code)
		}
		if testSvc.callCount != 0 {
			t.Errorf("service should not be called on bypass attempt")
		}
	})

	t.Run("Rejects oversized body (>64 KiB) with 400 BAD_REQUEST", func(t *testing.T) {
		testSvc := &fakeConnectionTestExecutor{}
		h := NewHandler(&fakeResourceService{}, testSvc, nil, nil)

		oversizedBody := strings.Repeat(" ", 70*1024)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/resources/"+resID.String()+"/test-connection", strings.NewReader(oversizedBody))
		req.SetPathValue("id", resID.String())
		req = attachTenantContext(req, orgID, orgDomain.RoleAdmin)

		rec := httptest.NewRecorder()
		h.TestConnection(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 BAD_REQUEST for oversized body, got %d", rec.Code)
		}
		if testSvc.callCount != 0 {
			t.Errorf("service should not be called on oversized body")
		}
	})

	t.Run("Accepts whitespace-only body within limit with 200 OK", func(t *testing.T) {
		testSvc := &fakeConnectionTestExecutor{}
		h := NewHandler(&fakeResourceService{}, testSvc, nil, nil)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/resources/"+resID.String()+"/test-connection", strings.NewReader("   \n\t  "))
		req.SetPathValue("id", resID.String())
		req = attachTenantContext(req, orgID, orgDomain.RoleAdmin)

		rec := httptest.NewRecorder()
		h.TestConnection(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK for whitespace-only body, got %d", rec.Code)
		}
		if testSvc.callCount != 1 {
			t.Errorf("service should be called for valid whitespace-only body")
		}
	})

	t.Run("Invalid resource ID returns 400 BAD_REQUEST", func(t *testing.T) {
		testSvc := &fakeConnectionTestExecutor{}
		h := NewHandler(&fakeResourceService{}, testSvc, nil, nil)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/resources/not-a-uuid/test-connection", nil)
		req.SetPathValue("id", "not-a-uuid")
		req = attachTenantContext(req, orgID, orgDomain.RoleAdmin)

		rec := httptest.NewRecorder()
		h.TestConnection(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 BAD_REQUEST, got %d", rec.Code)
		}
	})

	t.Run("Resource not found returns 404 RESOURCE_NOT_FOUND", func(t *testing.T) {
		testSvc := &fakeConnectionTestExecutor{
			err: domain.ErrResourceNotFound,
		}
		h := NewHandler(&fakeResourceService{}, testSvc, nil, nil)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/resources/"+resID.String()+"/test-connection", nil)
		req.SetPathValue("id", resID.String())
		req = attachTenantContext(req, orgID, orgDomain.RoleAdmin)

		rec := httptest.NewRecorder()
		h.TestConnection(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 RESOURCE_NOT_FOUND, got %d", rec.Code)
		}
	})

	t.Run("Preflight validation failure returns 422 VALIDATION_FAILED", func(t *testing.T) {
		testSvc := &fakeConnectionTestExecutor{
			err: domain.ErrInvalidHostKeyFingerprint,
		}
		h := NewHandler(&fakeResourceService{}, testSvc, nil, nil)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/resources/"+resID.String()+"/test-connection", nil)
		req.SetPathValue("id", resID.String())
		req = attachTenantContext(req, orgID, orgDomain.RoleAdmin)

		rec := httptest.NewRecorder()
		h.TestConnection(rec, req)

		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("expected 422 VALIDATION_FAILED, got %d", rec.Code)
		}
	})

	t.Run("Internal service error returns 503 SERVICE_UNAVAILABLE", func(t *testing.T) {
		testSvc := &fakeConnectionTestExecutor{
			err: domain.ErrResourceServiceUnavailable,
		}
		h := NewHandler(&fakeResourceService{}, testSvc, nil, nil)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/resources/"+resID.String()+"/test-connection", nil)
		req.SetPathValue("id", resID.String())
		req = attachTenantContext(req, orgID, orgDomain.RoleAdmin)

		rec := httptest.NewRecorder()
		h.TestConnection(rec, req)

		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("expected 503 SERVICE_UNAVAILABLE, got %d", rec.Code)
		}
	})
}

func TestResourceHandler_DiscoverDatabases(t *testing.T) {
	orgID := uuid.New()
	resID := uuid.New()

	t.Run("Admin receives 200 OK with discovered databases (Ubuntu: integer tables_count)", func(t *testing.T) {
		tables48 := int64(48)
		tables12 := int64(12)
		discSvc := &fakeDatabaseDiscoveryExecutor{
			result: []connector.DatabaseInfo{
				{
					Name:        "alpha_prod",
					SizeBytes:   104857600,
					TablesCount: &tables48,
					Status:      connector.DatabaseStatusAccessible,
				},
				{
					Name:        "zeta_dw",
					SizeBytes:   524288000,
					TablesCount: &tables12,
					Status:      connector.DatabaseStatusAccessible,
				},
			},
		}
		h := NewHandler(&fakeResourceService{}, nil, discSvc, nil)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/resources/"+resID.String()+"/databases", nil)
		req.SetPathValue("id", resID.String())
		req = attachTenantContext(req, orgID, orgDomain.RoleAdmin)

		rec := httptest.NewRecorder()
		h.DiscoverDatabases(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got: %d", rec.Code)
		}

		if rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get("Pragma") != "no-cache" {
			t.Errorf("expected no-store / no-cache headers")
		}

		var resp struct {
			Data []struct {
				Name        string `json:"name"`
				SizeBytes   int64  `json:"size_bytes"`
				TablesCount *int64 `json:"tables_count"`
				Status      string `json:"status"`
			} `json:"data"`
			Message string `json:"message"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to decode response JSON: %v", err)
		}

		if len(resp.Data) != 2 {
			t.Fatalf("expected 2 database items, got %d", len(resp.Data))
		}
		if resp.Data[0].Name != "alpha_prod" || resp.Data[0].SizeBytes != 104857600 || *resp.Data[0].TablesCount != 48 || resp.Data[0].Status != "accessible" {
			t.Errorf("unexpected database 0: %+v", resp.Data[0])
		}
		if resp.Data[1].Name != "zeta_dw" || resp.Data[1].SizeBytes != 524288000 || *resp.Data[1].TablesCount != 12 || resp.Data[1].Status != "accessible" {
			t.Errorf("unexpected database 1: %+v", resp.Data[1])
		}
		if resp.Message != "فهرست پایگاه‌های داده با موفقیت شناسایی شد." {
			t.Errorf("unexpected message: %s", resp.Message)
		}
	})

	t.Run("Admin receives 200 OK with discovered databases (cPanel: explicit null tables_count)", func(t *testing.T) {
		discSvc := &fakeDatabaseDiscoveryExecutor{
			result: []connector.DatabaseInfo{
				{
					Name:        "cpanel_shop",
					SizeBytes:   4161,
					TablesCount: nil, // null
					Status:      connector.DatabaseStatusAccessible,
				},
			},
		}
		h := NewHandler(&fakeResourceService{}, nil, discSvc, nil)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/resources/"+resID.String()+"/databases", nil)
		req.SetPathValue("id", resID.String())
		req = attachTenantContext(req, orgID, orgDomain.RoleAdmin)

		rec := httptest.NewRecorder()
		h.DiscoverDatabases(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got: %d", rec.Code)
		}

		rawJSON := rec.Body.String()
		if !strings.Contains(rawJSON, `"tables_count":null`) {
			t.Errorf("expected explicit '\"tables_count\":null' in JSON for cPanel, got: %s", rawJSON)
		}
	})

	t.Run("Empty database list returns 200 OK with 'data': [] (never null)", func(t *testing.T) {
		discSvc := &fakeDatabaseDiscoveryExecutor{
			result: []connector.DatabaseInfo{},
		}
		h := NewHandler(&fakeResourceService{}, nil, discSvc, nil)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/resources/"+resID.String()+"/databases", nil)
		req.SetPathValue("id", resID.String())
		req = attachTenantContext(req, orgID, orgDomain.RoleAdmin)

		rec := httptest.NewRecorder()
		h.DiscoverDatabases(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got: %d", rec.Code)
		}

		rawJSON := rec.Body.String()
		if !strings.Contains(rawJSON, `"data":[]`) {
			t.Errorf("expected '\"data\":[]' in JSON for empty list, got: %s", rawJSON)
		}
	})

	t.Run("Secret and unallowlisted fields regression", func(t *testing.T) {
		discSvc := &fakeDatabaseDiscoveryExecutor{
			result: []connector.DatabaseInfo{
				{
					Name:        "test_app",
					SizeBytes:   100,
					TablesCount: nil,
					Status:      connector.DatabaseStatusAccessible,
				},
			},
		}
		h := NewHandler(&fakeResourceService{}, nil, discSvc, nil)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/resources/"+resID.String()+"/databases", nil)
		req.SetPathValue("id", resID.String())
		req = attachTenantContext(req, orgID, orgDomain.RoleAdmin)

		rec := httptest.NewRecorder()
		h.DiscoverDatabases(rec, req)

		rawJSON := rec.Body.String()
		forbiddenKeywords := []string{
			"credential_id",
			"password",
			"secret",
			"token",
			"username",
			"users",
			"host",
			"port",
			"command",
			"stderr",
		}
		for _, kw := range forbiddenKeywords {
			if strings.Contains(rawJSON, `"`+kw+`"`) {
				t.Errorf("SECURITY LEAK: forbidden field %q found in response: %s", kw, rawJSON)
			}
		}
	})

	t.Run("Missing tenant context returns 403 FORBIDDEN", func(t *testing.T) {
		discSvc := &fakeDatabaseDiscoveryExecutor{}
		h := NewHandler(&fakeResourceService{}, nil, discSvc, nil)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/resources/"+resID.String()+"/databases", nil)
		req.SetPathValue("id", resID.String())
		// No tenant context

		rec := httptest.NewRecorder()
		h.DiscoverDatabases(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 FORBIDDEN, got: %d", rec.Code)
		}
	})

	t.Run("Invalid resource UUID returns 400 BAD_REQUEST", func(t *testing.T) {
		discSvc := &fakeDatabaseDiscoveryExecutor{}
		h := NewHandler(&fakeResourceService{}, nil, discSvc, nil)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/resources/not-a-uuid/databases", nil)
		req.SetPathValue("id", "not-a-uuid")
		req = attachTenantContext(req, orgID, orgDomain.RoleAdmin)

		rec := httptest.NewRecorder()
		h.DiscoverDatabases(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 BAD_REQUEST, got: %d", rec.Code)
		}
	})

	t.Run("Resource not found returns 404 RESOURCE_NOT_FOUND", func(t *testing.T) {
		discSvc := &fakeDatabaseDiscoveryExecutor{
			err: domain.ErrResourceNotFound,
		}
		h := NewHandler(&fakeResourceService{}, nil, discSvc, nil)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/resources/"+resID.String()+"/databases", nil)
		req.SetPathValue("id", resID.String())
		req = attachTenantContext(req, orgID, orgDomain.RoleAdmin)

		rec := httptest.NewRecorder()
		h.DiscoverDatabases(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 RESOURCE_NOT_FOUND, got: %d", rec.Code)
		}
	})

	t.Run("Preflight validation failure returns 422 VALIDATION_FAILED", func(t *testing.T) {
		discSvc := &fakeDatabaseDiscoveryExecutor{
			err: domain.ErrInvalidHostKeyFingerprint,
		}
		h := NewHandler(&fakeResourceService{}, nil, discSvc, nil)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/resources/"+resID.String()+"/databases", nil)
		req.SetPathValue("id", resID.String())
		req = attachTenantContext(req, orgID, orgDomain.RoleAdmin)

		rec := httptest.NewRecorder()
		h.DiscoverDatabases(rec, req)

		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("expected 422 VALIDATION_FAILED, got: %d", rec.Code)
		}
	})

	t.Run("Discovery service failure returns 503 SERVICE_UNAVAILABLE", func(t *testing.T) {
		discSvc := &fakeDatabaseDiscoveryExecutor{
			err: domain.ErrResourceServiceUnavailable,
		}
		h := NewHandler(&fakeResourceService{}, nil, discSvc, nil)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/resources/"+resID.String()+"/databases", nil)
		req.SetPathValue("id", resID.String())
		req = attachTenantContext(req, orgID, orgDomain.RoleAdmin)

		rec := httptest.NewRecorder()
		h.DiscoverDatabases(rec, req)

		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("expected 503 SERVICE_UNAVAILABLE, got: %d", rec.Code)
		}
	})

	t.Run("Discovery service nil returns 503 SERVICE_UNAVAILABLE", func(t *testing.T) {
		h := NewHandler(&fakeResourceService{}, nil, nil, nil) // Nil discovery service

		req := httptest.NewRequest(http.MethodGet, "/api/v1/resources/"+resID.String()+"/databases", nil)
		req.SetPathValue("id", resID.String())
		req = attachTenantContext(req, orgID, orgDomain.RoleAdmin)

		rec := httptest.NewRecorder()
		h.DiscoverDatabases(rec, req)

		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("expected 503 SERVICE_UNAVAILABLE, got: %d", rec.Code)
		}
	})
}
