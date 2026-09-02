package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"backup-platform/internal/backup/domain"
	"backup-platform/internal/backup/service"
	orgDomain "backup-platform/internal/organization/domain"
	orgHttpapi "backup-platform/internal/organization/httpapi"
	"backup-platform/pkg/uuid"
)

type mockStorageTargetManager struct {
	createFunc func(ctx context.Context, role orgDomain.Role, orgID uuid.UUID, input service.CreateStorageTargetInput) (*domain.StorageTarget, error)
	getFunc    func(ctx context.Context, role orgDomain.Role, orgID, targetID uuid.UUID) (*domain.StorageTarget, error)
	listFunc   func(ctx context.Context, role orgDomain.Role, orgID uuid.UUID) ([]*domain.StorageTarget, error)
	updateFunc func(ctx context.Context, role orgDomain.Role, orgID, targetID uuid.UUID, input service.UpdateStorageTargetInput) (*domain.StorageTarget, error)
	deleteFunc func(ctx context.Context, role orgDomain.Role, orgID, targetID uuid.UUID) error
}

func (m *mockStorageTargetManager) CreateStorageTarget(ctx context.Context, role orgDomain.Role, orgID uuid.UUID, input service.CreateStorageTargetInput) (*domain.StorageTarget, error) {
	if m.createFunc != nil {
		return m.createFunc(ctx, role, orgID, input)
	}
	return nil, nil
}

func (m *mockStorageTargetManager) GetStorageTarget(ctx context.Context, role orgDomain.Role, orgID, targetID uuid.UUID) (*domain.StorageTarget, error) {
	if m.getFunc != nil {
		return m.getFunc(ctx, role, orgID, targetID)
	}
	return nil, nil
}

func (m *mockStorageTargetManager) ListStorageTargets(ctx context.Context, role orgDomain.Role, orgID uuid.UUID) ([]*domain.StorageTarget, error) {
	if m.listFunc != nil {
		return m.listFunc(ctx, role, orgID)
	}
	return nil, nil
}

func (m *mockStorageTargetManager) UpdateStorageTarget(ctx context.Context, role orgDomain.Role, orgID, targetID uuid.UUID, input service.UpdateStorageTargetInput) (*domain.StorageTarget, error) {
	if m.updateFunc != nil {
		return m.updateFunc(ctx, role, orgID, targetID, input)
	}
	return nil, nil
}

func (m *mockStorageTargetManager) DeleteStorageTarget(ctx context.Context, role orgDomain.Role, orgID, targetID uuid.UUID) error {
	if m.deleteFunc != nil {
		return m.deleteFunc(ctx, role, orgID, targetID)
	}
	return nil
}

func TestHandler_StorageTargets(t *testing.T) {
	orgID := uuid.New()
	targetID := uuid.New()
	credID := uuid.New()

	t.Run("POST /api/v1/storage-targets creates S3 storage target successfully (201 Created)", func(t *testing.T) {
		mockSvc := &mockStorageTargetManager{
			createFunc: func(ctx context.Context, role orgDomain.Role, oID uuid.UUID, input service.CreateStorageTargetInput) (*domain.StorageTarget, error) {
				if role != orgDomain.RoleAdmin {
					return nil, domain.ErrUnauthorizedRole
				}
				_, err := domain.ParseS3TargetConfig(input.Config)
				if err != nil {
					t.Fatalf("failed parsing s3 config: %v", err)
				}
				return &domain.StorageTarget{
					ID:             targetID,
					OrganizationID: oID,
					Name:           input.Name,
					Type:           input.Type,
					Status:         domain.StorageTargetStatusActive,
					IsDefault:      false,
					Config:         input.Config,
					CredentialID:   input.CredentialID,
					CreatedAt:      time.Now().UTC(),
					UpdatedAt:      time.Now().UTC(),
				}, nil
			},
		}

		h := NewHandler(nil, nil, nil, nil, nil, nil)
		h.SetStorageTargetService(mockSvc)

		body := map[string]any{
			"name": "Production S3",
			"type": "s3",
			"s3_config": map[string]any{
				"bucket":           "prod-backups",
				"endpoint":         "https://s3.eu-central-1.amazonaws.com",
				"region":           "eu-central-1",
				"force_path_style": false,
			},
			"credential_id": credID.String(),
		}
		raw, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/storage-targets", bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(orgHttpapi.WithTenantContext(req.Context(), &orgHttpapi.TenantContext{
			OrganizationID: orgID,
			Role:           orgDomain.RoleAdmin,
			UserID:         uuid.New(),
		}))

		rec := httptest.NewRecorder()
		h.CreateStorageTarget(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("expected status 201 Created, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed unmarshaling response: %v", err)
		}
		data := resp["data"].(map[string]any)
		if data["id"] != targetID.String() {
			t.Errorf("expected id %s, got %v", targetID.String(), data["id"])
		}
		if data["name"] != "Production S3" {
			t.Errorf("expected name 'Production S3', got %v", data["name"])
		}
		if data["type"] != "s3" {
			t.Errorf("expected type 's3', got %v", data["type"])
		}
	})

	t.Run("POST /api/v1/storage-targets missing s3_config fails with 422 Unprocessable Entity", func(t *testing.T) {
		h := NewHandler(nil, nil, nil, nil, nil, nil)
		h.SetStorageTargetService(&mockStorageTargetManager{})

		body := map[string]any{
			"name":          "Missing S3 Config",
			"type":          "s3",
			"credential_id": credID.String(),
		}
		raw, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/storage-targets", bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(orgHttpapi.WithTenantContext(req.Context(), &orgHttpapi.TenantContext{
			OrganizationID: orgID,
			Role:           orgDomain.RoleAdmin,
			UserID:         uuid.New(),
		}))

		rec := httptest.NewRecorder()
		h.CreateStorageTarget(rec, req)

		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("expected status 422, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("POST /api/v1/storage-targets forbidden for member role (403)", func(t *testing.T) {
		mockSvc := &mockStorageTargetManager{
			createFunc: func(ctx context.Context, role orgDomain.Role, oID uuid.UUID, input service.CreateStorageTargetInput) (*domain.StorageTarget, error) {
				return nil, domain.ErrUnauthorizedRole
			},
		}

		h := NewHandler(nil, nil, nil, nil, nil, nil)
		h.SetStorageTargetService(mockSvc)

		body := map[string]any{
			"name": "Member Attempt",
			"type": "s3",
			"s3_config": map[string]any{
				"bucket":   "test",
				"endpoint": "https://s3.eu-central-1.amazonaws.com",
				"region":   "eu-central-1",
			},
			"credential_id": credID.String(),
		}
		raw, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/storage-targets", bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(orgHttpapi.WithTenantContext(req.Context(), &orgHttpapi.TenantContext{
			OrganizationID: orgID,
			Role:           orgDomain.RoleMember,
			UserID:         uuid.New(),
		}))

		rec := httptest.NewRecorder()
		h.CreateStorageTarget(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected status 403 Forbidden, got %d", rec.Code)
		}
	})

	t.Run("GET /api/v1/storage-targets lists targets successfully (200 OK)", func(t *testing.T) {
		mockSvc := &mockStorageTargetManager{
			listFunc: func(ctx context.Context, role orgDomain.Role, oID uuid.UUID) ([]*domain.StorageTarget, error) {
				return []*domain.StorageTarget{
					{
						ID:             targetID,
						OrganizationID: oID,
						Name:           "Default Local",
						Type:           domain.StorageTargetTypeLocal,
						Status:         domain.StorageTargetStatusActive,
						IsDefault:      true,
						CreatedAt:      time.Now().UTC(),
						UpdatedAt:      time.Now().UTC(),
					},
				}, nil
			},
		}

		h := NewHandler(nil, nil, nil, nil, nil, nil)
		h.SetStorageTargetService(mockSvc)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/storage-targets", nil)
		req = req.WithContext(orgHttpapi.WithTenantContext(req.Context(), &orgHttpapi.TenantContext{
			OrganizationID: orgID,
			Role:           orgDomain.RoleMember,
			UserID:         uuid.New(),
		}))

		rec := httptest.NewRecorder()
		h.ListStorageTargets(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200 OK, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed unmarshaling response: %v", err)
		}
		data := resp["data"].([]any)
		if len(data) != 1 {
			t.Fatalf("expected 1 item, got %d", len(data))
		}
	})

	t.Run("GET /api/v1/storage-targets/{id} returns target detail (200 OK)", func(t *testing.T) {
		mockSvc := &mockStorageTargetManager{
			getFunc: func(ctx context.Context, role orgDomain.Role, oID, tID uuid.UUID) (*domain.StorageTarget, error) {
				if tID != targetID {
					return nil, domain.ErrStorageTargetNotFound
				}
				cfgJSON, _ := json.Marshal(domain.S3TargetConfig{
					Bucket:   "my-bucket",
					Endpoint: "https://s3.eu-central-1.amazonaws.com",
					Region:   "eu-central-1",
				})
				return &domain.StorageTarget{
					ID:             tID,
					OrganizationID: oID,
					Name:           "S3 Target",
					Type:           domain.StorageTargetTypeS3,
					Status:         domain.StorageTargetStatusActive,
					IsDefault:      false,
					Config:         cfgJSON,
					CredentialID:   &credID,
					CreatedAt:      time.Now().UTC(),
					UpdatedAt:      time.Now().UTC(),
				}, nil
			},
		}

		h := NewHandler(nil, nil, nil, nil, nil, nil)
		h.SetStorageTargetService(mockSvc)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/storage-targets/"+targetID.String(), nil)
		req.SetPathValue("id", targetID.String())
		req = req.WithContext(orgHttpapi.WithTenantContext(req.Context(), &orgHttpapi.TenantContext{
			OrganizationID: orgID,
			Role:           orgDomain.RoleMember,
			UserID:         uuid.New(),
		}))

		rec := httptest.NewRecorder()
		h.GetStorageTarget(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200 OK, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("GET /api/v1/storage-targets/{id} succeeds for Viewer role (200 OK)", func(t *testing.T) {
		mockSvc := &mockStorageTargetManager{
			getFunc: func(ctx context.Context, role orgDomain.Role, oID, tID uuid.UUID) (*domain.StorageTarget, error) {
				if role != orgDomain.RoleViewer {
					return nil, domain.ErrUnauthorizedRole
				}
				return &domain.StorageTarget{
					ID:             tID,
					OrganizationID: oID,
					Name:           "S3 Target",
					Type:           domain.StorageTargetTypeS3,
					Status:         domain.StorageTargetStatusActive,
					IsDefault:      false,
					CreatedAt:      time.Now().UTC(),
					UpdatedAt:      time.Now().UTC(),
				}, nil
			},
		}

		h := NewHandler(nil, nil, nil, nil, nil, nil)
		h.SetStorageTargetService(mockSvc)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/storage-targets/"+targetID.String(), nil)
		req.SetPathValue("id", targetID.String())
		req = req.WithContext(orgHttpapi.WithTenantContext(req.Context(), &orgHttpapi.TenantContext{
			OrganizationID: orgID,
			Role:           orgDomain.RoleViewer,
			UserID:         uuid.New(),
		}))

		rec := httptest.NewRecorder()
		h.GetStorageTarget(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200 OK for viewer, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("GET /api/v1/storage-targets/{id} returns 404 when target not found", func(t *testing.T) {
		mockSvc := &mockStorageTargetManager{
			getFunc: func(ctx context.Context, role orgDomain.Role, oID, tID uuid.UUID) (*domain.StorageTarget, error) {
				return nil, domain.ErrStorageTargetNotFound
			},
		}

		h := NewHandler(nil, nil, nil, nil, nil, nil)
		h.SetStorageTargetService(mockSvc)

		missingID := uuid.New()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/storage-targets/"+missingID.String(), nil)
		req.SetPathValue("id", missingID.String())
		req = req.WithContext(orgHttpapi.WithTenantContext(req.Context(), &orgHttpapi.TenantContext{
			OrganizationID: orgID,
			Role:           orgDomain.RoleAdmin,
			UserID:         uuid.New(),
		}))

		rec := httptest.NewRecorder()
		h.GetStorageTarget(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected status 404 Not Found, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("PUT /api/v1/storage-targets/{id} returns 409 when location is immutable", func(t *testing.T) {
		mockSvc := &mockStorageTargetManager{
			updateFunc: func(ctx context.Context, role orgDomain.Role, oID, tID uuid.UUID, input service.UpdateStorageTargetInput) (*domain.StorageTarget, error) {
				return nil, domain.ErrStorageTargetLocationImmutable
			},
		}

		h := NewHandler(nil, nil, nil, nil, nil, nil)
		h.SetStorageTargetService(mockSvc)

		body := map[string]any{
			"name": "Renamed",
			"s3_config": map[string]any{
				"bucket":   "different-bucket",
				"endpoint": "https://s3.eu-central-1.amazonaws.com",
				"region":   "eu-central-1",
			},
		}
		raw, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPut, "/api/v1/storage-targets/"+targetID.String(), bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		req.SetPathValue("id", targetID.String())
		req = req.WithContext(orgHttpapi.WithTenantContext(req.Context(), &orgHttpapi.TenantContext{
			OrganizationID: orgID,
			Role:           orgDomain.RoleAdmin,
			UserID:         uuid.New(),
		}))

		rec := httptest.NewRecorder()
		h.UpdateStorageTarget(rec, req)

		if rec.Code != http.StatusConflict {
			t.Fatalf("expected status 409 Conflict, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("DELETE /api/v1/storage-targets/{id} deletes target successfully (204 No Content)", func(t *testing.T) {
		mockSvc := &mockStorageTargetManager{
			deleteFunc: func(ctx context.Context, role orgDomain.Role, oID, tID uuid.UUID) error {
				return nil
			},
		}

		h := NewHandler(nil, nil, nil, nil, nil, nil)
		h.SetStorageTargetService(mockSvc)

		req := httptest.NewRequest(http.MethodDelete, "/api/v1/storage-targets/"+targetID.String(), nil)
		req.SetPathValue("id", targetID.String())
		req = req.WithContext(orgHttpapi.WithTenantContext(req.Context(), &orgHttpapi.TenantContext{
			OrganizationID: orgID,
			Role:           orgDomain.RoleAdmin,
			UserID:         uuid.New(),
		}))

		rec := httptest.NewRecorder()
		h.DeleteStorageTarget(rec, req)

		if rec.Code != http.StatusNoContent {
			t.Fatalf("expected status 204 No Content, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("DELETE /api/v1/storage-targets/{id} returns 409 when target is in use", func(t *testing.T) {
		mockSvc := &mockStorageTargetManager{
			deleteFunc: func(ctx context.Context, role orgDomain.Role, oID, tID uuid.UUID) error {
				return domain.ErrStorageTargetInUse
			},
		}

		h := NewHandler(nil, nil, nil, nil, nil, nil)
		h.SetStorageTargetService(mockSvc)

		req := httptest.NewRequest(http.MethodDelete, "/api/v1/storage-targets/"+targetID.String(), nil)
		req.SetPathValue("id", targetID.String())
		req = req.WithContext(orgHttpapi.WithTenantContext(req.Context(), &orgHttpapi.TenantContext{
			OrganizationID: orgID,
			Role:           orgDomain.RoleAdmin,
			UserID:         uuid.New(),
		}))

		rec := httptest.NewRecorder()
		h.DeleteStorageTarget(rec, req)

		if rec.Code != http.StatusConflict {
			t.Fatalf("expected status 409 Conflict, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("DELETE /api/v1/storage-targets/{id} returns 409 when target is default", func(t *testing.T) {
		mockSvc := &mockStorageTargetManager{
			deleteFunc: func(ctx context.Context, role orgDomain.Role, oID, tID uuid.UUID) error {
				return domain.ErrCannotDeleteDefaultStorageTarget
			},
		}

		h := NewHandler(nil, nil, nil, nil, nil, nil)
		h.SetStorageTargetService(mockSvc)

		req := httptest.NewRequest(http.MethodDelete, "/api/v1/storage-targets/"+targetID.String(), nil)
		req.SetPathValue("id", targetID.String())
		req = req.WithContext(orgHttpapi.WithTenantContext(req.Context(), &orgHttpapi.TenantContext{
			OrganizationID: orgID,
			Role:           orgDomain.RoleAdmin,
			UserID:         uuid.New(),
		}))

		rec := httptest.NewRecorder()
		h.DeleteStorageTarget(rec, req)

		if rec.Code != http.StatusConflict {
			t.Fatalf("expected status 409 Conflict, got %d: %s", rec.Code, rec.Body.String())
		}
	})
}

func TestHandler_StorageTarget_JobAndPlanValidation(t *testing.T) {
	orgID := uuid.New()
	resID := uuid.New()
	targetID := uuid.New()

	t.Run("POST /api/v1/backup-jobs with plan override returns 422", func(t *testing.T) {
		mockJob := &mockJobCreator{
			createJobFunc: func(ctx context.Context, role orgDomain.Role, oID, uID uuid.UUID, input service.CreateManualJobInput) (*domain.BackupJob, error) {
				return nil, domain.ErrPlanOverrideForbidden
			},
		}

		h := NewHandler(mockJob, nil, nil, nil, nil, nil)

		planID := uuid.New()
		eng := domain.EngineTypeDirectStream
		body := map[string]any{
			"backup_plan_id":    planID.String(),
			"engine_type":       eng,
			"storage_target_id": targetID.String(),
		}
		raw, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/backup-jobs", bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(orgHttpapi.WithTenantContext(req.Context(), &orgHttpapi.TenantContext{
			OrganizationID: orgID,
			Role:           orgDomain.RoleMember,
			UserID:         uuid.New(),
		}))

		rec := httptest.NewRecorder()
		h.CreateBackupJob(rec, req)

		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("expected status 422, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("POST /api/v1/backup-jobs with unknown storage target returns 404", func(t *testing.T) {
		mockJob := &mockJobCreator{
			createJobFunc: func(ctx context.Context, role orgDomain.Role, oID, uID uuid.UUID, input service.CreateManualJobInput) (*domain.BackupJob, error) {
				return nil, domain.ErrStorageTargetNotFound
			},
		}

		h := NewHandler(mockJob, nil, nil, nil, nil, nil)

		body := map[string]any{
			"resource_id":       resID.String(),
			"backup_type":       "database_mysql",
			"storage_target_id": targetID.String(),
		}
		raw, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/backup-jobs", bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(orgHttpapi.WithTenantContext(req.Context(), &orgHttpapi.TenantContext{
			OrganizationID: orgID,
			Role:           orgDomain.RoleMember,
			UserID:         uuid.New(),
		}))

		rec := httptest.NewRecorder()
		h.CreateBackupJob(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected status 404, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("POST /api/v1/backup-plans with incompatible engine and storage returns 422", func(t *testing.T) {
		mockPlan := &mockPlanManager{
			createPlanFunc: func(ctx context.Context, role orgDomain.Role, oID uuid.UUID, input service.CreatePlanInput) (*domain.BackupPlan, error) {
				return nil, domain.ErrIncompatibleEngineStorage
			},
		}

		h := NewHandler(nil, mockPlan, nil, nil, nil, nil)

		eng := domain.EngineType("unsupported")
		body := map[string]any{
			"name":              "Incompatible Plan",
			"resource_id":       resID.String(),
			"backup_type":       "database_mysql",
			"engine_type":       eng,
			"storage_target_id": targetID.String(),
			"schedule": map[string]any{
				"is_enabled": false,
				"timezone":   "UTC",
			},
		}
		raw, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/backup-plans", bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(orgHttpapi.WithTenantContext(req.Context(), &orgHttpapi.TenantContext{
			OrganizationID: orgID,
			Role:           orgDomain.RoleAdmin,
			UserID:         uuid.New(),
		}))

		rec := httptest.NewRecorder()
		h.CreateBackupPlan(rec, req)

		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("expected status 422, got %d: %s", rec.Code, rec.Body.String())
		}
	})
}
