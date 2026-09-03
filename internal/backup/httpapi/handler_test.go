package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"backup-platform/internal/artifactcrypto"
	"backup-platform/internal/backup/domain"
	"backup-platform/internal/backup/service"
	orgDomain "backup-platform/internal/organization/domain"
	orgHttpapi "backup-platform/internal/organization/httpapi"
	"backup-platform/pkg/uuid"
)

type mockJobCreator struct {
	createJobFunc func(ctx context.Context, role orgDomain.Role, orgID, userID uuid.UUID, input service.CreateManualJobInput) (*domain.BackupJob, error)
}

func (m *mockJobCreator) CreateManualJob(ctx context.Context, role orgDomain.Role, orgID, userID uuid.UUID, input service.CreateManualJobInput) (*domain.BackupJob, error) {
	if m.createJobFunc != nil {
		return m.createJobFunc(ctx, role, orgID, userID, input)
	}
	return nil, errors.New("not implemented")
}

type mockPlanManager struct {
	createPlanFunc  func(ctx context.Context, role orgDomain.Role, orgID uuid.UUID, input service.CreatePlanInput) (*domain.BackupPlan, error)
	getPlanFunc     func(ctx context.Context, role orgDomain.Role, orgID, planID uuid.UUID) (*domain.BackupPlanWithResource, error)
	listPlansFunc   func(ctx context.Context, role orgDomain.Role, orgID uuid.UUID, filter domain.PlanFilter) ([]*domain.BackupPlanWithResource, error)
	updatePlanFunc  func(ctx context.Context, role orgDomain.Role, orgID, planID uuid.UUID, input service.UpdatePlanInput) (*domain.BackupPlan, error)
	archivePlanFunc func(ctx context.Context, role orgDomain.Role, orgID, planID uuid.UUID) error
}

func (m *mockPlanManager) CreatePlan(ctx context.Context, role orgDomain.Role, orgID uuid.UUID, input service.CreatePlanInput) (*domain.BackupPlan, error) {
	if m.createPlanFunc != nil {
		return m.createPlanFunc(ctx, role, orgID, input)
	}
	return nil, errors.New("not implemented")
}

func (m *mockPlanManager) GetPlan(ctx context.Context, role orgDomain.Role, orgID, planID uuid.UUID) (*domain.BackupPlanWithResource, error) {
	if m.getPlanFunc != nil {
		return m.getPlanFunc(ctx, role, orgID, planID)
	}
	return nil, errors.New("not implemented")
}

func (m *mockPlanManager) ListPlans(ctx context.Context, role orgDomain.Role, orgID uuid.UUID, filter domain.PlanFilter) ([]*domain.BackupPlanWithResource, error) {
	if m.listPlansFunc != nil {
		return m.listPlansFunc(ctx, role, orgID, filter)
	}
	return nil, errors.New("not implemented")
}

func (m *mockPlanManager) UpdatePlan(ctx context.Context, role orgDomain.Role, orgID, planID uuid.UUID, input service.UpdatePlanInput) (*domain.BackupPlan, error) {
	if m.updatePlanFunc != nil {
		return m.updatePlanFunc(ctx, role, orgID, planID, input)
	}
	return nil, errors.New("not implemented")
}

func (m *mockPlanManager) ArchivePlan(ctx context.Context, role orgDomain.Role, orgID, planID uuid.UUID) error {
	if m.archivePlanFunc != nil {
		return m.archivePlanFunc(ctx, role, orgID, planID)
	}
	return errors.New("not implemented")
}

type mockHistoryManager struct {
	listRunsFunc func(ctx context.Context, role orgDomain.Role, orgID uuid.UUID, filter domain.RunFilter) ([]*domain.BackupRunWithStats, error)
	getRunFunc   func(ctx context.Context, role orgDomain.Role, orgID, runID uuid.UUID) (*domain.BackupRunWithStats, error)
}

func (m *mockHistoryManager) ListRuns(ctx context.Context, role orgDomain.Role, orgID uuid.UUID, filter domain.RunFilter) ([]*domain.BackupRunWithStats, error) {
	if m.listRunsFunc != nil {
		return m.listRunsFunc(ctx, role, orgID, filter)
	}
	return nil, errors.New("not implemented")
}

func (m *mockHistoryManager) GetRun(ctx context.Context, role orgDomain.Role, orgID, runID uuid.UUID) (*domain.BackupRunWithStats, error) {
	if m.getRunFunc != nil {
		return m.getRunFunc(ctx, role, orgID, runID)
	}
	return nil, errors.New("not implemented")
}

type mockArtifactManager struct {
	listArtifactsFunc        func(ctx context.Context, role orgDomain.Role, orgID uuid.UUID) ([]*domain.BackupArtifact, error)
	getArtifactFunc          func(ctx context.Context, role orgDomain.Role, orgID, artifactID uuid.UUID) (*domain.BackupArtifact, error)
	openArtifactDownloadFunc func(ctx context.Context, role orgDomain.Role, orgID, artifactID uuid.UUID) (*domain.BackupArtifact, io.ReadCloser, error)
	recordDownloadAuditFunc  func(ctx context.Context, orgID, userID, artifactID uuid.UUID, sizeBytes int64, clientIP, userAgent string) error
	deleteArtifactFunc       func(ctx context.Context, role orgDomain.Role, orgID, userID, artifactID uuid.UUID, clientIP, userAgent string) error
}

func (m *mockArtifactManager) ListArtifacts(ctx context.Context, role orgDomain.Role, orgID uuid.UUID) ([]*domain.BackupArtifact, error) {
	if m.listArtifactsFunc != nil {
		return m.listArtifactsFunc(ctx, role, orgID)
	}
	return nil, errors.New("not implemented")
}

func (m *mockArtifactManager) GetArtifact(ctx context.Context, role orgDomain.Role, orgID, artifactID uuid.UUID) (*domain.BackupArtifact, error) {
	if m.getArtifactFunc != nil {
		return m.getArtifactFunc(ctx, role, orgID, artifactID)
	}
	return nil, errors.New("not implemented")
}

func (m *mockArtifactManager) OpenArtifactDownload(ctx context.Context, role orgDomain.Role, orgID, artifactID uuid.UUID) (*domain.BackupArtifact, io.ReadCloser, error) {
	if m.openArtifactDownloadFunc != nil {
		return m.openArtifactDownloadFunc(ctx, role, orgID, artifactID)
	}
	return nil, nil, errors.New("not implemented")
}

func (m *mockArtifactManager) RecordDownloadAudit(ctx context.Context, orgID, userID, artifactID uuid.UUID, sizeBytes int64, clientIP, userAgent string) error {
	if m.recordDownloadAuditFunc != nil {
		return m.recordDownloadAuditFunc(ctx, orgID, userID, artifactID, sizeBytes, clientIP, userAgent)
	}
	return nil
}

func (m *mockArtifactManager) DeleteArtifact(ctx context.Context, role orgDomain.Role, orgID, userID, artifactID uuid.UUID, clientIP, userAgent string) error {
	if m.deleteArtifactFunc != nil {
		return m.deleteArtifactFunc(ctx, role, orgID, userID, artifactID, clientIP, userAgent)
	}
	return errors.New("not implemented")
}

type mockVerifier struct {
	verifyRunFunc func(ctx context.Context, role orgDomain.Role, orgID, runID uuid.UUID) (*service.RunVerificationResult, error)
}

func (m *mockVerifier) VerifyRun(ctx context.Context, role orgDomain.Role, orgID, runID uuid.UUID) (*service.RunVerificationResult, error) {
	if m.verifyRunFunc != nil {
		return m.verifyRunFunc(ctx, role, orgID, runID)
	}
	return nil, errors.New("not implemented")
}

func TestHandler_CreateBackupJob(t *testing.T) {
	orgID := uuid.New()
	userID := uuid.New()
	resID := uuid.New()

	t.Run("202 Accepted on valid manual job creation with correct DTO response", func(t *testing.T) {
		mockCreator := &mockJobCreator{
			createJobFunc: func(ctx context.Context, role orgDomain.Role, oID, uID uuid.UUID, input service.CreateManualJobInput) (*domain.BackupJob, error) {
				return &domain.BackupJob{
					ID:             uuid.New(),
					OrganizationID: oID,
					ResourceID:     *input.ResourceID,
					TriggerType:    domain.TriggerTypeManual,
					BackupType:     domain.BackupTypeMySQLDatabase,
					TargetSpec:     *input.TargetSpec,
					Status:         domain.JobStatusPending,
					CreatedAt:      time.Now(),
					UpdatedAt:      time.Now(),
				}, nil
			},
		}

		handler := NewHandler(mockCreator, nil, nil, nil, nil, nil)

		reqBody := `{"resource_id":"` + resID.String() + `","backup_type":"mysql_database","target_spec":{"databases":["prod_db"]}}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/backup-jobs", bytes.NewBufferString(reqBody))
		req.Header.Set("Content-Type", "application/json")

		tenantCtx := &orgHttpapi.TenantContext{
			UserID:           userID,
			OrganizationID:   orgID,
			Role:             orgDomain.RoleAdmin,
			MembershipStatus: orgDomain.MemberStatusActive,
		}
		req = req.WithContext(orgHttpapi.WithTenantContext(req.Context(), tenantCtx))
		rec := httptest.NewRecorder()

		handler.CreateBackupJob(rec, req)

		if rec.Code != http.StatusAccepted {
			t.Fatalf("expected 202 Accepted, got %d: %s", rec.Code, rec.Body.String())
		}

		var rawEnvelope map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &rawEnvelope); err != nil {
			t.Fatalf("failed unmarshaling raw response: %v", err)
		}

		dataMap, ok := rawEnvelope["data"].(map[string]any)
		if !ok {
			t.Fatalf("expected data map in response")
		}

		// Verify organization_id and updated_at are NOT in the public response
		if _, exists := dataMap["organization_id"]; exists {
			t.Errorf("contract violation: organization_id should not be in public response")
		}
		if _, exists := dataMap["updated_at"]; exists {
			t.Errorf("contract violation: updated_at should not be in public response")
		}
	})

	t.Run("403 Forbidden when member attempts ad-hoc backup", func(t *testing.T) {
		mockCreator := &mockJobCreator{
			createJobFunc: func(ctx context.Context, role orgDomain.Role, oID, uID uuid.UUID, input service.CreateManualJobInput) (*domain.BackupJob, error) {
				return nil, domain.ErrUnauthorizedRole
			},
		}

		handler := NewHandler(mockCreator, nil, nil, nil, nil, nil)

		reqBody := `{"resource_id":"` + resID.String() + `","backup_type":"mysql_database","target_spec":{"databases":["prod_db"]}}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/backup-jobs", bytes.NewBufferString(reqBody))
		req.Header.Set("Content-Type", "application/json")

		tenantCtx := &orgHttpapi.TenantContext{
			UserID:           userID,
			OrganizationID:   orgID,
			Role:             orgDomain.RoleMember,
			MembershipStatus: orgDomain.MemberStatusActive,
		}
		req = req.WithContext(orgHttpapi.WithTenantContext(req.Context(), tenantCtx))
		rec := httptest.NewRecorder()

		handler.CreateBackupJob(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 Forbidden, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("422 Unprocessable Entity when ad-hoc manual MySQL job sends empty databases array", func(t *testing.T) {
		mockCreator := &mockJobCreator{
			createJobFunc: func(ctx context.Context, role orgDomain.Role, oID, uID uuid.UUID, input service.CreateManualJobInput) (*domain.BackupJob, error) {
				return nil, domain.ErrInvalidTargetSpec
			},
		}

		handler := NewHandler(mockCreator, nil, nil, nil, nil, nil)

		reqBody := `{"resource_id":"` + resID.String() + `","backup_type":"mysql_database","target_spec":{"databases":[]}}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/backup-jobs", bytes.NewBufferString(reqBody))
		req.Header.Set("Content-Type", "application/json")

		tenantCtx := &orgHttpapi.TenantContext{
			UserID:           userID,
			OrganizationID:   orgID,
			Role:             orgDomain.RoleAdmin,
			MembershipStatus: orgDomain.MemberStatusActive,
		}
		req = req.WithContext(orgHttpapi.WithTenantContext(req.Context(), tenantCtx))
		rec := httptest.NewRecorder()

		handler.CreateBackupJob(rec, req)

		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("expected 422 Unprocessable Entity for ad-hoc empty databases, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("422 Unprocessable Entity when ad-hoc manual MySQL job sends empty target_spec", func(t *testing.T) {
		mockCreator := &mockJobCreator{
			createJobFunc: func(ctx context.Context, role orgDomain.Role, oID, uID uuid.UUID, input service.CreateManualJobInput) (*domain.BackupJob, error) {
				return nil, domain.ErrInvalidTargetSpec
			},
		}

		handler := NewHandler(mockCreator, nil, nil, nil, nil, nil)

		reqBody := `{"resource_id":"` + resID.String() + `","backup_type":"mysql_database","target_spec":{}}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/backup-jobs", bytes.NewBufferString(reqBody))
		req.Header.Set("Content-Type", "application/json")

		tenantCtx := &orgHttpapi.TenantContext{
			UserID:           userID,
			OrganizationID:   orgID,
			Role:             orgDomain.RoleAdmin,
			MembershipStatus: orgDomain.MemberStatusActive,
		}
		req = req.WithContext(orgHttpapi.WithTenantContext(req.Context(), tenantCtx))
		rec := httptest.NewRecorder()

		handler.CreateBackupJob(rec, req)

		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("expected 422 Unprocessable Entity for ad-hoc empty target spec, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("202 Accepted when member creates plan-triggered job with mode all plan", func(t *testing.T) {
		planUUID := uuid.New()
		mockCreator := &mockJobCreator{
			createJobFunc: func(ctx context.Context, role orgDomain.Role, oID, uID uuid.UUID, input service.CreateManualJobInput) (*domain.BackupJob, error) {
				return &domain.BackupJob{
					ID:           uuid.New(),
					BackupPlanID: &planUUID,
					ResourceID:   resID,
					TriggerType:  domain.TriggerTypeManual,
					BackupType:   domain.BackupTypeMySQLDatabase,
					TargetSpec:   domain.TargetSpec{Databases: []string{}},
					Status:       domain.JobStatusPending,
					CreatedAt:    time.Now(),
				}, nil
			},
		}

		handler := NewHandler(mockCreator, nil, nil, nil, nil, nil)

		reqBody := `{"backup_plan_id":"` + planUUID.String() + `"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/backup-jobs", bytes.NewBufferString(reqBody))
		req.Header.Set("Content-Type", "application/json")

		tenantCtx := &orgHttpapi.TenantContext{
			UserID:           userID,
			OrganizationID:   orgID,
			Role:             orgDomain.RoleMember,
			MembershipStatus: orgDomain.MemberStatusActive,
		}
		req = req.WithContext(orgHttpapi.WithTenantContext(req.Context(), tenantCtx))
		rec := httptest.NewRecorder()

		handler.CreateBackupJob(rec, req)

		if rec.Code != http.StatusAccepted {
			t.Fatalf("expected 202 Accepted, got %d: %s", rec.Code, rec.Body.String())
		}
	})
}

func TestHandler_BackupPlans(t *testing.T) {
	orgID := uuid.New()
	userID := uuid.New()
	resID := uuid.New()
	planID := uuid.New()
	cron := "0 2 * * *"

	t.Run("POST /api/v1/backup-plans returns 201 Created", func(t *testing.T) {
		mockPlan := &mockPlanManager{
			createPlanFunc: func(ctx context.Context, role orgDomain.Role, oID uuid.UUID, input service.CreatePlanInput) (*domain.BackupPlan, error) {
				return &domain.BackupPlan{
					ID:             planID,
					OrganizationID: oID,
					ResourceID:     input.ResourceID,
					Name:           input.Name,
					Status:         domain.PlanStatusActive,
					CreatedAt:      time.Now(),
				}, nil
			},
		}

		handler := NewHandler(nil, mockPlan, nil, nil, nil, nil)

		reqBody := `{
			"name": "Daily MySQL Backup",
			"resource_id": "` + resID.String() + `",
			"backup_type": "mysql_database",
			"database_selection": {
				"mode": "selected",
				"databases": ["db1"]
			},
			"schedule": {
				"is_enabled": true,
				"cron_expression": "0 2 * * *",
				"timezone": "Asia/Tehran"
			}
		}`

		req := httptest.NewRequest(http.MethodPost, "/api/v1/backup-plans", bytes.NewBufferString(reqBody))
		req.Header.Set("Content-Type", "application/json")

		tenantCtx := &orgHttpapi.TenantContext{
			UserID:           userID,
			OrganizationID:   orgID,
			Role:             orgDomain.RoleAdmin,
			MembershipStatus: orgDomain.MemberStatusActive,
		}
		req = req.WithContext(orgHttpapi.WithTenantContext(req.Context(), tenantCtx))
		rec := httptest.NewRecorder()

		handler.CreateBackupPlan(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("POST /api/v1/backup-plans returns 201 Created for mode all", func(t *testing.T) {
		mockPlan := &mockPlanManager{
			createPlanFunc: func(ctx context.Context, role orgDomain.Role, oID uuid.UUID, input service.CreatePlanInput) (*domain.BackupPlan, error) {
				return &domain.BackupPlan{
					ID:             planID,
					OrganizationID: oID,
					ResourceID:     input.ResourceID,
					Name:           input.Name,
					BackupType:     domain.BackupTypeMySQLDatabase,
					TargetSpec:     domain.TargetSpec{Databases: []string{}},
					Status:         domain.PlanStatusActive,
					CreatedAt:      time.Now(),
				}, nil
			},
		}

		handler := NewHandler(nil, mockPlan, nil, nil, nil, nil)

		reqBody := `{
			"name": "Daily MySQL Backup",
			"resource_id": "` + resID.String() + `",
			"backup_type": "mysql_database",
			"database_selection": {
				"mode": "all"
			},
			"schedule": {
				"is_enabled": true,
				"cron_expression": "0 2 * * *",
				"timezone": "UTC"
			}
		}`

		req := httptest.NewRequest(http.MethodPost, "/api/v1/backup-plans", bytes.NewBufferString(reqBody))
		req.Header.Set("Content-Type", "application/json")

		tenantCtx := &orgHttpapi.TenantContext{
			UserID:           userID,
			OrganizationID:   orgID,
			Role:             orgDomain.RoleAdmin,
			MembershipStatus: orgDomain.MemberStatusActive,
		}
		req = req.WithContext(orgHttpapi.WithTenantContext(req.Context(), tenantCtx))
		rec := httptest.NewRecorder()

		handler.CreateBackupPlan(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("POST /api/v1/backup-plans returns 422 for invalid mode", func(t *testing.T) {
		mockPlan := &mockPlanManager{
			createPlanFunc: func(ctx context.Context, role orgDomain.Role, oID uuid.UUID, input service.CreatePlanInput) (*domain.BackupPlan, error) {
				return nil, domain.ErrInvalidTargetSpec
			},
		}

		handler := NewHandler(nil, mockPlan, nil, nil, nil, nil)

		reqBody := `{
			"name": "Daily MySQL Backup",
			"resource_id": "` + resID.String() + `",
			"backup_type": "mysql_database",
			"database_selection": {
				"mode": "invalid"
			},
			"schedule": {
				"is_enabled": true,
				"cron_expression": "0 2 * * *",
				"timezone": "UTC"
			}
		}`

		req := httptest.NewRequest(http.MethodPost, "/api/v1/backup-plans", bytes.NewBufferString(reqBody))
		req.Header.Set("Content-Type", "application/json")

		tenantCtx := &orgHttpapi.TenantContext{
			UserID:           userID,
			OrganizationID:   orgID,
			Role:             orgDomain.RoleAdmin,
			MembershipStatus: orgDomain.MemberStatusActive,
		}
		req = req.WithContext(orgHttpapi.WithTenantContext(req.Context(), tenantCtx))
		rec := httptest.NewRecorder()

		handler.CreateBackupPlan(rec, req)

		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("expected 422 Unprocessable Entity, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("POST /api/v1/backup-plans returns 422 for mode all with non-empty databases", func(t *testing.T) {
		mockPlan := &mockPlanManager{
			createPlanFunc: func(ctx context.Context, role orgDomain.Role, oID uuid.UUID, input service.CreatePlanInput) (*domain.BackupPlan, error) {
				return nil, domain.ErrInvalidTargetSpec
			},
		}

		handler := NewHandler(nil, mockPlan, nil, nil, nil, nil)

		reqBody := `{
			"name": "Conflicting MySQL Backup",
			"resource_id": "` + resID.String() + `",
			"backup_type": "mysql_database",
			"database_selection": {
				"mode": "all",
				"databases": ["db1"]
			},
			"schedule": {
				"is_enabled": true,
				"cron_expression": "0 2 * * *",
				"timezone": "UTC"
			}
		}`

		req := httptest.NewRequest(http.MethodPost, "/api/v1/backup-plans", bytes.NewBufferString(reqBody))
		req.Header.Set("Content-Type", "application/json")

		tenantCtx := &orgHttpapi.TenantContext{
			UserID:           userID,
			OrganizationID:   orgID,
			Role:             orgDomain.RoleAdmin,
			MembershipStatus: orgDomain.MemberStatusActive,
		}
		req = req.WithContext(orgHttpapi.WithTenantContext(req.Context(), tenantCtx))
		rec := httptest.NewRecorder()

		handler.CreateBackupPlan(rec, req)

		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("expected 422 Unprocessable Entity for mode all with databases, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("GET /api/v1/backup-plans returns 200 OK with list of plans", func(t *testing.T) {
		nextRun := time.Date(2026, 8, 21, 2, 0, 0, 0, time.UTC)
		mockPlan := &mockPlanManager{
			listPlansFunc: func(ctx context.Context, role orgDomain.Role, oID uuid.UUID, filter domain.PlanFilter) ([]*domain.BackupPlanWithResource, error) {
				return []*domain.BackupPlanWithResource{
					{
						Plan: domain.BackupPlan{
							ID:                planID,
							OrganizationID:    oID,
							ResourceID:        resID,
							Name:              "Daily MySQL Backup",
							BackupType:        domain.BackupTypeMySQLDatabase,
							TargetSpec:        domain.TargetSpec{Databases: []string{"db1"}},
							ScheduleCron:      &cron,
							ScheduleTimezone:  "Asia/Tehran",
							IsScheduleEnabled: true,
							Status:            domain.PlanStatusActive,
							NextRunAt:         &nextRun,
							CreatedAt:         time.Now(),
						},
						ResourceName: "Ubuntu Main DB",
					},
				}, nil
			},
		}

		handler := NewHandler(nil, mockPlan, nil, nil, nil, nil)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/backup-plans", nil)
		tenantCtx := &orgHttpapi.TenantContext{
			UserID:           userID,
			OrganizationID:   orgID,
			Role:             orgDomain.RoleViewer,
			MembershipStatus: orgDomain.MemberStatusActive,
		}
		req = req.WithContext(orgHttpapi.WithTenantContext(req.Context(), tenantCtx))
		rec := httptest.NewRecorder()

		handler.ListBackupPlans(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
		}

		if rec.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("expected Cache-Control: no-store, got %s", rec.Header().Get("Cache-Control"))
		}
	})

	t.Run("GET /api/v1/backup-plans returns 400 on unknown query parameter", func(t *testing.T) {
		handler := NewHandler(nil, &mockPlanManager{}, nil, nil, nil, nil)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/backup-plans?unknown=param", nil)
		tenantCtx := &orgHttpapi.TenantContext{
			UserID:           userID,
			OrganizationID:   orgID,
			Role:             orgDomain.RoleMember,
			MembershipStatus: orgDomain.MemberStatusActive,
		}
		req = req.WithContext(orgHttpapi.WithTenantContext(req.Context(), tenantCtx))
		rec := httptest.NewRecorder()

		handler.ListBackupPlans(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("GET /api/v1/backup-plans returns 200 OK with database_selection mode all", func(t *testing.T) {
		nextRun := time.Date(2026, 8, 21, 2, 0, 0, 0, time.UTC)
		mockPlan := &mockPlanManager{
			listPlansFunc: func(ctx context.Context, role orgDomain.Role, oID uuid.UUID, filter domain.PlanFilter) ([]*domain.BackupPlanWithResource, error) {
				return []*domain.BackupPlanWithResource{
					{
						Plan: domain.BackupPlan{
							ID:                planID,
							OrganizationID:    oID,
							ResourceID:        resID,
							Name:              "All MySQL Plan",
							BackupType:        domain.BackupTypeMySQLDatabase,
							TargetSpec:        domain.TargetSpec{Databases: []string{}}, // mode = "all"
							ScheduleCron:      &cron,
							ScheduleTimezone:  "Asia/Tehran",
							IsScheduleEnabled: true,
							Status:            domain.PlanStatusActive,
							NextRunAt:         &nextRun,
							CreatedAt:         time.Now(),
						},
						ResourceName: "Ubuntu Main DB",
					},
				}, nil
			},
		}

		handler := NewHandler(nil, mockPlan, nil, nil, nil, nil)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/backup-plans", nil)
		tenantCtx := &orgHttpapi.TenantContext{
			UserID:           userID,
			OrganizationID:   orgID,
			Role:             orgDomain.RoleViewer,
			MembershipStatus: orgDomain.MemberStatusActive,
		}
		req = req.WithContext(orgHttpapi.WithTenantContext(req.Context(), tenantCtx))
		rec := httptest.NewRecorder()

		handler.ListBackupPlans(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
		}

		var rawEnvelope map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &rawEnvelope); err != nil {
			t.Fatalf("failed unmarshaling list response: %v", err)
		}
		dataList, ok := rawEnvelope["data"].([]any)
		if !ok || len(dataList) != 1 {
			t.Fatalf("expected data list of length 1")
		}
		planMap := dataList[0].(map[string]any)
		dbSelMap, ok := planMap["database_selection"].(map[string]any)
		if !ok {
			t.Fatalf("expected database_selection map in plan response, got: %+v", planMap)
		}
		if dbSelMap["mode"] != "all" {
			t.Fatalf("expected mode 'all', got: %v", dbSelMap["mode"])
		}
		if _, exists := dbSelMap["databases"]; exists {
			t.Fatalf("expected databases field to be omitted for mode all, got: %v", dbSelMap["databases"])
		}
	})

	t.Run("GET /api/v1/backup-plans/{id} returns 200 OK with database_selection mode all", func(t *testing.T) {
		mockPlan := &mockPlanManager{
			getPlanFunc: func(ctx context.Context, role orgDomain.Role, oID, pID uuid.UUID) (*domain.BackupPlanWithResource, error) {
				return &domain.BackupPlanWithResource{
					Plan: domain.BackupPlan{
						ID:                planID,
						OrganizationID:    oID,
						ResourceID:        resID,
						Name:              "All MySQL Plan",
						BackupType:        domain.BackupTypeMySQLDatabase,
						TargetSpec:        domain.TargetSpec{Databases: []string{}}, // mode = "all"
						ScheduleCron:      &cron,
						ScheduleTimezone:  "UTC",
						IsScheduleEnabled: true,
						Status:            domain.PlanStatusActive,
						CreatedAt:         time.Now(),
						UpdatedAt:         time.Now(),
					},
					ResourceName: "Ubuntu Main DB",
				}, nil
			},
		}

		handler := NewHandler(nil, mockPlan, nil, nil, nil, nil)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/backup-plans/"+planID.String(), nil)
		req.SetPathValue("id", planID.String())
		tenantCtx := &orgHttpapi.TenantContext{
			UserID:           userID,
			OrganizationID:   orgID,
			Role:             orgDomain.RoleMember,
			MembershipStatus: orgDomain.MemberStatusActive,
		}
		req = req.WithContext(orgHttpapi.WithTenantContext(req.Context(), tenantCtx))
		rec := httptest.NewRecorder()

		handler.GetBackupPlan(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
		}

		var rawEnvelope map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &rawEnvelope); err != nil {
			t.Fatalf("failed unmarshaling response: %v", err)
		}
		dataMap := rawEnvelope["data"].(map[string]any)
		dbSelMap := dataMap["database_selection"].(map[string]any)
		if dbSelMap["mode"] != "all" {
			t.Fatalf("expected mode 'all', got: %v", dbSelMap["mode"])
		}
		if _, exists := dbSelMap["databases"]; exists {
			t.Fatalf("expected databases field omitted for mode all, got: %v", dbSelMap["databases"])
		}
	})

	t.Run("GET /api/v1/backup-plans/{id} returns 200 OK with database_selection mode selected", func(t *testing.T) {
		mockPlan := &mockPlanManager{
			getPlanFunc: func(ctx context.Context, role orgDomain.Role, oID, pID uuid.UUID) (*domain.BackupPlanWithResource, error) {
				return &domain.BackupPlanWithResource{
					Plan: domain.BackupPlan{
						ID:                planID,
						OrganizationID:    oID,
						ResourceID:        resID,
						Name:              "Selected MySQL Plan",
						BackupType:        domain.BackupTypeMySQLDatabase,
						TargetSpec:        domain.TargetSpec{Databases: []string{"db1", "db2"}},
						ScheduleCron:      &cron,
						ScheduleTimezone:  "UTC",
						IsScheduleEnabled: true,
						Status:            domain.PlanStatusActive,
						CreatedAt:         time.Now(),
						UpdatedAt:         time.Now(),
					},
					ResourceName: "Ubuntu Main DB",
				}, nil
			},
		}

		handler := NewHandler(nil, mockPlan, nil, nil, nil, nil)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/backup-plans/"+planID.String(), nil)
		req.SetPathValue("id", planID.String())
		tenantCtx := &orgHttpapi.TenantContext{
			UserID:           userID,
			OrganizationID:   orgID,
			Role:             orgDomain.RoleMember,
			MembershipStatus: orgDomain.MemberStatusActive,
		}
		req = req.WithContext(orgHttpapi.WithTenantContext(req.Context(), tenantCtx))
		rec := httptest.NewRecorder()

		handler.GetBackupPlan(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
		}

		var rawEnvelope map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &rawEnvelope); err != nil {
			t.Fatalf("failed unmarshaling response: %v", err)
		}
		dataMap := rawEnvelope["data"].(map[string]any)
		dbSelMap := dataMap["database_selection"].(map[string]any)
		if dbSelMap["mode"] != "selected" {
			t.Fatalf("expected mode 'selected', got: %v", dbSelMap["mode"])
		}
		dbsList, ok := dbSelMap["databases"].([]any)
		if !ok || len(dbsList) != 2 || dbsList[0] != "db1" || dbsList[1] != "db2" {
			t.Fatalf("expected databases ['db1', 'db2'], got: %v", dbSelMap["databases"])
		}
	})

	t.Run("DELETE /api/v1/backup-plans/{id} returns 204 No Content", func(t *testing.T) {
		mockPlan := &mockPlanManager{
			archivePlanFunc: func(ctx context.Context, role orgDomain.Role, oID, pID uuid.UUID) error {
				return nil
			},
		}

		handler := NewHandler(nil, mockPlan, nil, nil, nil, nil)

		req := httptest.NewRequest(http.MethodDelete, "/api/v1/backup-plans/"+planID.String(), nil)
		req.SetPathValue("id", planID.String())
		tenantCtx := &orgHttpapi.TenantContext{
			UserID:           userID,
			OrganizationID:   orgID,
			Role:             orgDomain.RoleAdmin,
			MembershipStatus: orgDomain.MemberStatusActive,
		}
		req = req.WithContext(orgHttpapi.WithTenantContext(req.Context(), tenantCtx))
		rec := httptest.NewRecorder()

		handler.ArchiveBackupPlan(rec, req)

		if rec.Code != http.StatusNoContent {
			t.Fatalf("expected 204 No Content, got %d: %s", rec.Code, rec.Body.String())
		}
	})
}

func TestHandler_BackupRuns(t *testing.T) {
	orgID := uuid.New()
	userID := uuid.New()
	resID := uuid.New()
	jobID := uuid.New()
	runID := uuid.New()
	now := time.Now()
	ended := now.Add(2 * time.Minute)

	t.Run("GET /api/v1/backup-runs returns 200 OK with runs list", func(t *testing.T) {
		mockHist := &mockHistoryManager{
			listRunsFunc: func(ctx context.Context, role orgDomain.Role, oID uuid.UUID, filter domain.RunFilter) ([]*domain.BackupRunWithStats, error) {
				return []*domain.BackupRunWithStats{
					{
						Run: domain.BackupRun{
							ID:             runID,
							OrganizationID: oID,
							JobID:          jobID,
							AttemptNumber:  1,
							Status:         domain.RunStatusSuccess,
							StartedAt:      now,
							EndedAt:        &ended,
							CreatedAt:      now,
						},
						ResourceID:             resID,
						TotalArtifactSizeBytes: 1024,
						ArtifactsCount:         1,
					},
				}, nil
			},
		}

		handler := NewHandler(nil, nil, mockHist, nil, nil, nil)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/backup-runs?resource_id="+resID.String()+"&status=success", nil)
		tenantCtx := &orgHttpapi.TenantContext{
			UserID:           userID,
			OrganizationID:   orgID,
			Role:             orgDomain.RoleViewer,
			MembershipStatus: orgDomain.MemberStatusActive,
		}
		req = req.WithContext(orgHttpapi.WithTenantContext(req.Context(), tenantCtx))
		rec := httptest.NewRecorder()

		handler.ListBackupRuns(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
		}

		var rawEnvelope map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &rawEnvelope); err != nil {
			t.Fatalf("failed unmarshaling response: %v", err)
		}
		dataList, ok := rawEnvelope["data"].([]any)
		if !ok || len(dataList) != 1 {
			t.Fatalf("expected 1 run in data list")
		}
		first := dataList[0].(map[string]any)
		if first["id"] != runID.String() {
			t.Errorf("expected run ID %s, got %v", runID.String(), first["id"])
		}
		if first["total_artifact_size_bytes"] != float64(1024) {
			t.Errorf("expected total size 1024, got %v", first["total_artifact_size_bytes"])
		}
		if first["duration_seconds"] != float64(120) {
			t.Errorf("expected duration_seconds 120, got %v", first["duration_seconds"])
		}
	})

	t.Run("GET /api/v1/backup-runs returns 400 on unknown query parameter", func(t *testing.T) {
		handler := NewHandler(nil, nil, &mockHistoryManager{}, nil, nil, nil)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/backup-runs?foo=bar", nil)
		tenantCtx := &orgHttpapi.TenantContext{
			UserID:           userID,
			OrganizationID:   orgID,
			Role:             orgDomain.RoleMember,
			MembershipStatus: orgDomain.MemberStatusActive,
		}
		req = req.WithContext(orgHttpapi.WithTenantContext(req.Context(), tenantCtx))
		rec := httptest.NewRecorder()

		handler.ListBackupRuns(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request, got %d", rec.Code)
		}
	})

	t.Run("GET /api/v1/backup-runs/{id} returns 200 OK with run detail", func(t *testing.T) {
		mockHist := &mockHistoryManager{
			getRunFunc: func(ctx context.Context, role orgDomain.Role, oID, rID uuid.UUID) (*domain.BackupRunWithStats, error) {
				return &domain.BackupRunWithStats{
					Run: domain.BackupRun{
						ID:             rID,
						OrganizationID: oID,
						JobID:          jobID,
						AttemptNumber:  1,
						Status:         domain.RunStatusSuccess,
						StartedAt:      now,
						EndedAt:        &ended,
						CreatedAt:      now,
					},
					ResourceID:             resID,
					TotalArtifactSizeBytes: 2048,
					ArtifactsCount:         2,
				}, nil
			},
		}

		handler := NewHandler(nil, nil, mockHist, nil, nil, nil)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/backup-runs/"+runID.String(), nil)
		req.SetPathValue("id", runID.String())
		tenantCtx := &orgHttpapi.TenantContext{
			UserID:           userID,
			OrganizationID:   orgID,
			Role:             orgDomain.RoleViewer,
			MembershipStatus: orgDomain.MemberStatusActive,
		}
		req = req.WithContext(orgHttpapi.WithTenantContext(req.Context(), tenantCtx))
		rec := httptest.NewRecorder()

		handler.GetBackupRun(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("GET /api/v1/backup-runs/{id} returns 404 when not found", func(t *testing.T) {
		mockHist := &mockHistoryManager{
			getRunFunc: func(ctx context.Context, role orgDomain.Role, oID, rID uuid.UUID) (*domain.BackupRunWithStats, error) {
				return nil, domain.ErrRunNotFound
			},
		}

		handler := NewHandler(nil, nil, mockHist, nil, nil, nil)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/backup-runs/"+runID.String(), nil)
		req.SetPathValue("id", runID.String())
		tenantCtx := &orgHttpapi.TenantContext{
			UserID:           userID,
			OrganizationID:   orgID,
			Role:             orgDomain.RoleMember,
			MembershipStatus: orgDomain.MemberStatusActive,
		}
		req = req.WithContext(orgHttpapi.WithTenantContext(req.Context(), tenantCtx))
		rec := httptest.NewRecorder()

		handler.GetBackupRun(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 Not Found, got %d", rec.Code)
		}
	})
}

func TestHandler_BackupArtifacts(t *testing.T) {
	orgID := uuid.New()
	userID := uuid.New()
	runID := uuid.New()
	resID := uuid.New()
	artID := uuid.New()
	now := time.Now()

	t.Run("GET /api/v1/backup-artifacts returns 200 OK with active artifacts", func(t *testing.T) {
		mockArt := &mockArtifactManager{
			listArtifactsFunc: func(ctx context.Context, role orgDomain.Role, oID uuid.UUID) ([]*domain.BackupArtifact, error) {
				return []*domain.BackupArtifact{
					{
						ID:                 artID,
						OrganizationID:     oID,
						RunID:              runID,
						ResourceID:         resID,
						ArtifactType:       domain.ArtifactTypeDatabaseDump,
						Format:             domain.ArtifactFormatSQLGzip,
						TargetName:         "mysql_prod_db",
						StorageReference:   "org/art.sql.gz",
						SizeBytes:          1024,
						ChecksumAlgorithm:  "sha256",
						ChecksumHash:       "hash123",
						VerificationStatus: domain.VerificationStatusVerified,
						CreatedAt:          now,
					},
				}, nil
			},
		}

		handler := NewHandler(nil, nil, nil, mockArt, nil, nil)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/backup-artifacts", nil)
		tenantCtx := &orgHttpapi.TenantContext{
			UserID:           userID,
			OrganizationID:   orgID,
			Role:             orgDomain.RoleViewer,
			MembershipStatus: orgDomain.MemberStatusActive,
		}
		req = req.WithContext(orgHttpapi.WithTenantContext(req.Context(), tenantCtx))
		rec := httptest.NewRecorder()

		handler.ListBackupArtifacts(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
		}

		var rawEnvelope map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &rawEnvelope); err != nil {
			t.Fatalf("failed unmarshaling response: %v", err)
		}
		dataList := rawEnvelope["data"].([]any)
		if len(dataList) != 1 {
			t.Fatalf("expected 1 artifact in response")
		}
		item := dataList[0].(map[string]any)

		// Assert strictly NO storage_reference or physical path in public response
		if _, exists := item["storage_reference"]; exists {
			t.Errorf("security violation: storage_reference found in response: %v", item["storage_reference"])
		}
	})

	t.Run("GET /api/v1/backup-artifacts/{id} returns 200 OK with logical safe filename", func(t *testing.T) {
		mockArt := &mockArtifactManager{
			getArtifactFunc: func(ctx context.Context, role orgDomain.Role, oID, aID uuid.UUID) (*domain.BackupArtifact, error) {
				return &domain.BackupArtifact{
					ID:                 aID,
					OrganizationID:     oID,
					RunID:              runID,
					ResourceID:         resID,
					ArtifactType:       domain.ArtifactTypeDatabaseDump,
					Format:             domain.ArtifactFormatSQLGzip,
					TargetName:         "mysql_prod_db",
					StorageReference:   "org/art.sql.gz",
					SizeBytes:          1024,
					ChecksumAlgorithm:  "sha256",
					ChecksumHash:       "hash123",
					VerificationStatus: domain.VerificationStatusVerified,
					CreatedAt:          now,
				}, nil
			},
		}

		handler := NewHandler(nil, nil, nil, mockArt, nil, nil)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/backup-artifacts/"+artID.String(), nil)
		req.SetPathValue("id", artID.String())
		tenantCtx := &orgHttpapi.TenantContext{
			UserID:           userID,
			OrganizationID:   orgID,
			Role:             orgDomain.RoleMember,
			MembershipStatus: orgDomain.MemberStatusActive,
		}
		req = req.WithContext(orgHttpapi.WithTenantContext(req.Context(), tenantCtx))
		rec := httptest.NewRecorder()

		handler.GetBackupArtifact(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
		}

		var rawEnvelope map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &rawEnvelope)
		item := rawEnvelope["data"].(map[string]any)
		if _, exists := item["storage_reference"]; exists {
			t.Errorf("security violation: storage_reference exposed in artifact detail")
		}
		if item["artifact_name"] != "mysql_prod_db.sql.gz" {
			t.Errorf("expected logical artifact_name mysql_prod_db.sql.gz, got %v", item["artifact_name"])
		}
	})

	t.Run("GET /api/v1/backup-artifacts formats website target paths to safe filenames", func(t *testing.T) {
		mockArt := &mockArtifactManager{
			listArtifactsFunc: func(ctx context.Context, role orgDomain.Role, oID uuid.UUID) ([]*domain.BackupArtifact, error) {
				return []*domain.BackupArtifact{
					{
						ID:                 artID,
						OrganizationID:     oID,
						RunID:              runID,
						ResourceID:         resID,
						ArtifactType:       domain.ArtifactTypeFilesArchive,
						Format:             domain.ArtifactFormatTarGzip,
						TargetName:         "/var/www/example/public_html",
						StorageReference:   "/srv/backup-platform/artifacts/secret_uuid.tar.gz",
						SizeBytes:          2048,
						ChecksumAlgorithm:  "sha256",
						ChecksumHash:       "hash456",
						VerificationStatus: domain.VerificationStatusVerified,
						CreatedAt:          now,
					},
				}, nil
			},
		}

		handler := NewHandler(nil, nil, nil, mockArt, nil, nil)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/backup-artifacts", nil)
		tenantCtx := &orgHttpapi.TenantContext{
			UserID:           userID,
			OrganizationID:   orgID,
			Role:             orgDomain.RoleMember,
			MembershipStatus: orgDomain.MemberStatusActive,
		}
		req = req.WithContext(orgHttpapi.WithTenantContext(req.Context(), tenantCtx))
		rec := httptest.NewRecorder()

		handler.ListBackupArtifacts(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
		}

		var rawEnvelope map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &rawEnvelope)
		dataList := rawEnvelope["data"].([]any)
		item := dataList[0].(map[string]any)
		if item["artifact_name"] != "public_html.tar.gz" {
			t.Errorf("expected logical artifact_name public_html.tar.gz, got %v", item["artifact_name"])
		}
		if strings.Contains(item["artifact_name"].(string), "/var/www") || strings.Contains(item["artifact_name"].(string), "secret_uuid") {
			t.Errorf("security violation: raw path or storage reference leaked in artifact_name: %v", item["artifact_name"])
		}
	})
}

func TestHandler_DownloadBackupArtifact(t *testing.T) {
	orgID := uuid.New()
	userID := uuid.New()
	artID := uuid.New()
	payload := []byte("dummy-backup-gzipped-stream-bytes")

	t.Run("GET /api/v1/backup-artifacts/{id}/download streams content and audits", func(t *testing.T) {
		var auditRecorded bool
		mockArt := &mockArtifactManager{
			openArtifactDownloadFunc: func(ctx context.Context, role orgDomain.Role, oID, aID uuid.UUID) (*domain.BackupArtifact, io.ReadCloser, error) {
				art := &domain.BackupArtifact{
					ID:         aID,
					Format:     domain.ArtifactFormatSQLGzip,
					TargetName: "prod_db",
					SizeBytes:  int64(len(payload)),
				}
				return art, io.NopCloser(bytes.NewReader(payload)), nil
			},
			recordDownloadAuditFunc: func(ctx context.Context, oID, uID, aID uuid.UUID, sizeBytes int64, clientIP, userAgent string) error {
				auditRecorded = true
				return nil
			},
		}

		handler := NewHandler(nil, nil, nil, mockArt, nil, nil)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/backup-artifacts/"+artID.String()+"/download", nil)
		req.SetPathValue("id", artID.String())
		tenantCtx := &orgHttpapi.TenantContext{
			UserID:           userID,
			OrganizationID:   orgID,
			Role:             orgDomain.RoleMember,
			MembershipStatus: orgDomain.MemberStatusActive,
		}
		req = req.WithContext(orgHttpapi.WithTenantContext(req.Context(), tenantCtx))
		rec := httptest.NewRecorder()

		handler.DownloadBackupArtifact(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
		}

		if rec.Header().Get("Content-Type") != "application/gzip" {
			t.Errorf("expected Content-Type: application/gzip, got %s", rec.Header().Get("Content-Type"))
		}

		disp := rec.Header().Get("Content-Disposition")
		if !strings.Contains(disp, "attachment;") || !strings.Contains(disp, "prod_db.sql.gz") {
			t.Errorf("expected Content-Disposition attachment with prod_db.sql.gz, got %s", disp)
		}

		if !bytes.Equal(rec.Body.Bytes(), payload) {
			t.Errorf("downloaded payload mismatch")
		}

		if !auditRecorded {
			t.Errorf("expected audit event to be recorded upon successful download")
		}
	})

	t.Run("GET /api/v1/backup-artifacts/{id}/download website file artifact has matching Content-Disposition", func(t *testing.T) {
		mockArt := &mockArtifactManager{
			openArtifactDownloadFunc: func(ctx context.Context, role orgDomain.Role, oID, aID uuid.UUID) (*domain.BackupArtifact, io.ReadCloser, error) {
				art := &domain.BackupArtifact{
					ID:         aID,
					Format:     domain.ArtifactFormatTarGzip,
					TargetName: "/var/www/example/public_html",
					SizeBytes:  int64(len(payload)),
				}
				return art, io.NopCloser(bytes.NewReader(payload)), nil
			},
			recordDownloadAuditFunc: func(ctx context.Context, oID, uID, aID uuid.UUID, sizeBytes int64, clientIP, userAgent string) error {
				return nil
			},
		}

		handler := NewHandler(nil, nil, nil, mockArt, nil, nil)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/backup-artifacts/"+artID.String()+"/download", nil)
		req.SetPathValue("id", artID.String())
		tenantCtx := &orgHttpapi.TenantContext{
			UserID:           userID,
			OrganizationID:   orgID,
			Role:             orgDomain.RoleMember,
			MembershipStatus: orgDomain.MemberStatusActive,
		}
		req = req.WithContext(orgHttpapi.WithTenantContext(req.Context(), tenantCtx))
		rec := httptest.NewRecorder()

		handler.DownloadBackupArtifact(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", rec.Code)
		}

		disp := rec.Header().Get("Content-Disposition")
		if !strings.Contains(disp, `filename="public_html.tar.gz"`) {
			t.Errorf("expected Content-Disposition filename public_html.tar.gz, got %s", disp)
		}
	})

	t.Run("interrupted stream DOES NOT record download audit", func(t *testing.T) {
		var auditAttempted bool
		mockArt := &mockArtifactManager{
			openArtifactDownloadFunc: func(ctx context.Context, role orgDomain.Role, oID, aID uuid.UUID) (*domain.BackupArtifact, io.ReadCloser, error) {
				art := &domain.BackupArtifact{
					ID:         aID,
					Format:     domain.ArtifactFormatSQLGzip,
					TargetName: "prod_db",
					SizeBytes:  10000, // Declared size is larger than actual reader payload
				}
				// Reader provides only 5 bytes, simulating an interrupted / short stream
				return art, io.NopCloser(bytes.NewReader([]byte("short"))), nil
			},
			recordDownloadAuditFunc: func(ctx context.Context, oID, uID, aID uuid.UUID, sizeBytes int64, clientIP, userAgent string) error {
				auditAttempted = true
				return nil
			},
		}

		handler := NewHandler(nil, nil, nil, mockArt, nil, nil)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/backup-artifacts/"+artID.String()+"/download", nil)
		req.SetPathValue("id", artID.String())
		tenantCtx := &orgHttpapi.TenantContext{
			UserID:           userID,
			OrganizationID:   orgID,
			Role:             orgDomain.RoleMember,
			MembershipStatus: orgDomain.MemberStatusActive,
		}
		req = req.WithContext(orgHttpapi.WithTenantContext(req.Context(), tenantCtx))
		rec := httptest.NewRecorder()

		handler.DownloadBackupArtifact(rec, req)

		if auditAttempted {
			t.Errorf("interrupted or incomplete download MUST NOT trigger download audit")
		}
	})

	t.Run("audit failure during download is handled safely without crashing", func(t *testing.T) {
		mockArt := &mockArtifactManager{
			openArtifactDownloadFunc: func(ctx context.Context, role orgDomain.Role, oID, aID uuid.UUID) (*domain.BackupArtifact, io.ReadCloser, error) {
				art := &domain.BackupArtifact{
					ID:         aID,
					Format:     domain.ArtifactFormatSQLGzip,
					TargetName: "prod_db",
					SizeBytes:  int64(len(payload)),
				}
				return art, io.NopCloser(bytes.NewReader(payload)), nil
			},
			recordDownloadAuditFunc: func(ctx context.Context, oID, uID, aID uuid.UUID, sizeBytes int64, clientIP, userAgent string) error {
				return errors.New("audit log database write failed")
			},
		}

		handler := NewHandler(nil, nil, nil, mockArt, nil, nil)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/backup-artifacts/"+artID.String()+"/download", nil)
		req.SetPathValue("id", artID.String())
		tenantCtx := &orgHttpapi.TenantContext{
			UserID:           userID,
			OrganizationID:   orgID,
			Role:             orgDomain.RoleMember,
			MembershipStatus: orgDomain.MemberStatusActive,
		}
		req = req.WithContext(orgHttpapi.WithTenantContext(req.Context(), tenantCtx))
		rec := httptest.NewRecorder()

		handler.DownloadBackupArtifact(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK since stream succeeded before audit recording, got %d", rec.Code)
		}
	})

	t.Run("GET /api/v1/backup-artifacts/{id}/download returns 404 when artifact not found or deleted", func(t *testing.T) {
		mockArt := &mockArtifactManager{
			openArtifactDownloadFunc: func(ctx context.Context, role orgDomain.Role, oID, aID uuid.UUID) (*domain.BackupArtifact, io.ReadCloser, error) {
				return nil, nil, domain.ErrArtifactNotFound
			},
		}

		handler := NewHandler(nil, nil, nil, mockArt, nil, nil)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/backup-artifacts/"+artID.String()+"/download", nil)
		req.SetPathValue("id", artID.String())
		tenantCtx := &orgHttpapi.TenantContext{
			UserID:           userID,
			OrganizationID:   orgID,
			Role:             orgDomain.RoleAdmin,
			MembershipStatus: orgDomain.MemberStatusActive,
		}
		req = req.WithContext(orgHttpapi.WithTenantContext(req.Context(), tenantCtx))
		rec := httptest.NewRecorder()

		handler.DownloadBackupArtifact(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 Not Found, got %d", rec.Code)
		}
	})

	t.Run("GET /api/v1/backup-artifacts/{id}/download sets Content-Length to SizeBytes not StoredSizeBytes", func(t *testing.T) {
		plainSize := int64(12345)
		storedSize := int64(67890)
		mockArt := &mockArtifactManager{
			openArtifactDownloadFunc: func(ctx context.Context, role orgDomain.Role, oID, aID uuid.UUID) (*domain.BackupArtifact, io.ReadCloser, error) {
				art := &domain.BackupArtifact{
					ID:              aID,
					Format:          domain.ArtifactFormatSQLGzip,
					TargetName:      "prod_db",
					SizeBytes:       plainSize,
					StoredSizeBytes: &storedSize,
				}
				dummyContent := bytes.Repeat([]byte("x"), int(plainSize))
				return art, io.NopCloser(bytes.NewReader(dummyContent)), nil
			},
			recordDownloadAuditFunc: func(ctx context.Context, oID, uID, aID uuid.UUID, sizeBytes int64, clientIP, userAgent string) error {
				return nil
			},
		}

		handler := NewHandler(nil, nil, nil, mockArt, nil, nil)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/backup-artifacts/"+artID.String()+"/download", nil)
		req.SetPathValue("id", artID.String())
		tenantCtx := &orgHttpapi.TenantContext{
			UserID:           userID,
			OrganizationID:   orgID,
			Role:             orgDomain.RoleMember,
			MembershipStatus: orgDomain.MemberStatusActive,
		}
		req = req.WithContext(orgHttpapi.WithTenantContext(req.Context(), tenantCtx))
		rec := httptest.NewRecorder()

		handler.DownloadBackupArtifact(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
		}

		// Verify Content-Length is SizeBytes (plainSize), NOT StoredSizeBytes
		contentLen := rec.Header().Get("Content-Length")
		if contentLen != "12345" {
			t.Fatalf("Content-Length mismatch: expected SizeBytes (12345), got %q (StoredSizeBytes is %d)", contentLen, storedSize)
		}
	})

	t.Run("GET /api/v1/backup-artifacts/{id}/download returns 503 and does not audit when key version unknown", func(t *testing.T) {
		var auditAttempted bool
		mockArt := &mockArtifactManager{
			openArtifactDownloadFunc: func(ctx context.Context, role orgDomain.Role, oID, aID uuid.UUID) (*domain.BackupArtifact, io.ReadCloser, error) {
				return nil, nil, artifactcrypto.ErrUnknownKeyVersion
			},
			recordDownloadAuditFunc: func(ctx context.Context, oID, uID, aID uuid.UUID, sizeBytes int64, clientIP, userAgent string) error {
				auditAttempted = true
				return nil
			},
		}

		handler := NewHandler(nil, nil, nil, mockArt, nil, nil)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/backup-artifacts/"+artID.String()+"/download", nil)
		req.SetPathValue("id", artID.String())
		tenantCtx := &orgHttpapi.TenantContext{
			UserID:           userID,
			OrganizationID:   orgID,
			Role:             orgDomain.RoleMember,
			MembershipStatus: orgDomain.MemberStatusActive,
		}
		req = req.WithContext(orgHttpapi.WithTenantContext(req.Context(), tenantCtx))
		rec := httptest.NewRecorder()

		handler.DownloadBackupArtifact(rec, req)

		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("expected 503 Service Unavailable, got %d", rec.Code)
		}

		if auditAttempted {
			t.Fatalf("download audit must NOT be recorded on key infrastructure failure")
		}
	})
}

func TestHandler_DeleteBackupArtifact(t *testing.T) {
	orgID := uuid.New()
	userID := uuid.New()
	artID := uuid.New()

	t.Run("DELETE /api/v1/backup-artifacts/{id} returns 204 No Content for admin", func(t *testing.T) {
		var deleted bool
		mockArt := &mockArtifactManager{
			deleteArtifactFunc: func(ctx context.Context, role orgDomain.Role, oID, uID, aID uuid.UUID, clientIP, userAgent string) error {
				deleted = true
				return nil
			},
		}

		handler := NewHandler(nil, nil, nil, mockArt, nil, nil)

		req := httptest.NewRequest(http.MethodDelete, "/api/v1/backup-artifacts/"+artID.String(), nil)
		req.SetPathValue("id", artID.String())
		tenantCtx := &orgHttpapi.TenantContext{
			UserID:           userID,
			OrganizationID:   orgID,
			Role:             orgDomain.RoleAdmin,
			MembershipStatus: orgDomain.MemberStatusActive,
		}
		req = req.WithContext(orgHttpapi.WithTenantContext(req.Context(), tenantCtx))
		rec := httptest.NewRecorder()

		handler.DeleteBackupArtifact(rec, req)

		if rec.Code != http.StatusNoContent {
			t.Fatalf("expected 204 No Content, got %d: %s", rec.Code, rec.Body.String())
		}
		if !deleted {
			t.Errorf("expected DeleteArtifact to be called on service")
		}
	})

	t.Run("DELETE /api/v1/backup-artifacts/{id} returns 403 Forbidden for unauthorized role", func(t *testing.T) {
		mockArt := &mockArtifactManager{
			deleteArtifactFunc: func(ctx context.Context, role orgDomain.Role, oID, uID, aID uuid.UUID, clientIP, userAgent string) error {
				return domain.ErrUnauthorizedRole
			},
		}

		handler := NewHandler(nil, nil, nil, mockArt, nil, nil)

		req := httptest.NewRequest(http.MethodDelete, "/api/v1/backup-artifacts/"+artID.String(), nil)
		req.SetPathValue("id", artID.String())
		tenantCtx := &orgHttpapi.TenantContext{
			UserID:           userID,
			OrganizationID:   orgID,
			Role:             orgDomain.RoleMember,
			MembershipStatus: orgDomain.MemberStatusActive,
		}
		req = req.WithContext(orgHttpapi.WithTenantContext(req.Context(), tenantCtx))
		rec := httptest.NewRecorder()

		handler.DeleteBackupArtifact(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 Forbidden, got %d", rec.Code)
		}
	})
}

func TestHandler_VerifyBackupRun(t *testing.T) {
	orgID := uuid.New()
	userID := uuid.New()
	runID := uuid.New()
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

	t.Run("POST /api/v1/backup-runs/{id}/verify returns 200 OK for admin with verified message and exact 4 details keys", func(t *testing.T) {
		mockVer := &mockVerifier{
			verifyRunFunc: func(ctx context.Context, role orgDomain.Role, oID, rID uuid.UUID) (*service.RunVerificationResult, error) {
				if role != orgDomain.RoleAdmin || oID != orgID || rID != runID {
					return nil, errors.New("mismatch")
				}
				return &service.RunVerificationResult{
					RunID:              runID,
					VerificationStatus: domain.VerificationStatusVerified,
					VerifiedAt:         now,
					Details: service.VerificationDetails{
						ChecksumMatched:      true,
						ArchiveIntegrity:     "passed",
						CompressionValid:     true,
						ExtractedSampleCheck: "valid_sql_dump",
					},
				}, nil
			},
		}

		handler := NewHandler(nil, nil, nil, nil, mockVer, nil)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/backup-runs/"+runID.String()+"/verify", nil)
		req.SetPathValue("id", runID.String())
		tenantCtx := &orgHttpapi.TenantContext{
			UserID:           userID,
			OrganizationID:   orgID,
			Role:             orgDomain.RoleAdmin,
			MembershipStatus: orgDomain.MemberStatusActive,
		}
		req = req.WithContext(orgHttpapi.WithTenantContext(req.Context(), tenantCtx))
		rec := httptest.NewRecorder()

		handler.VerifyBackupRun(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
		}
		if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
			t.Errorf("expected Cache-Control: no-store, got: %s", cc)
		}

		var resp struct {
			Data    VerifyBackupRunResponse `json:"data"`
			Message string                  `json:"message"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed decoding response: %v", err)
		}
		if resp.Data.RunID != runID {
			t.Errorf("expected run_id %s, got: %s", runID, resp.Data.RunID)
		}
		if resp.Data.VerificationStatus != domain.VerificationStatusVerified {
			t.Errorf("expected status verified, got: %s", resp.Data.VerificationStatus)
		}
		if !resp.Data.Details.ChecksumMatched || resp.Data.Details.ArchiveIntegrity != "passed" || !resp.Data.Details.CompressionValid || resp.Data.Details.ExtractedSampleCheck != "valid_sql_dump" {
			t.Errorf("unexpected details: %+v", resp.Data.Details)
		}
		if resp.Message != "صحت و یکپارچگی ساختاری فایل پشتیبان تأیید گردید." {
			t.Errorf("expected verified success message, got: %s", resp.Message)
		}

		// Ensure exact 4 fields in details JSON and no leaked paths
		var rawMap map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &rawMap); err != nil {
			t.Fatalf("failed unmarshaling raw response: %v", err)
		}
		detailsMap, ok := rawMap["data"].(map[string]any)["details"].(map[string]any)
		if !ok {
			t.Fatalf("expected details object in data")
		}
		if len(detailsMap) != 4 {
			t.Errorf("expected exactly 4 keys in details, got %d: %+v", len(detailsMap), detailsMap)
		}

		bodyStr := rec.Body.String()
		if strings.Contains(bodyStr, "storage_reference") || strings.Contains(bodyStr, "physical_path") {
			t.Errorf("SECURITY FLAW: leaked internal path or storage reference in response body: %s", bodyStr)
		}
	})

	t.Run("POST /api/v1/backup-runs/{id}/verify returns 200 OK for member with website backup", func(t *testing.T) {
		mockVer := &mockVerifier{
			verifyRunFunc: func(ctx context.Context, role orgDomain.Role, oID, rID uuid.UUID) (*service.RunVerificationResult, error) {
				return &service.RunVerificationResult{
					RunID:              runID,
					VerificationStatus: domain.VerificationStatusVerified,
					VerifiedAt:         now,
					Details: service.VerificationDetails{
						ChecksumMatched:      true,
						ArchiveIntegrity:     "passed",
						CompressionValid:     true,
						ExtractedSampleCheck: "not_applicable",
					},
				}, nil
			},
		}

		handler := NewHandler(nil, nil, nil, nil, mockVer, nil)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/backup-runs/"+runID.String()+"/verify", nil)
		req.SetPathValue("id", runID.String())
		tenantCtx := &orgHttpapi.TenantContext{
			UserID:           userID,
			OrganizationID:   orgID,
			Role:             orgDomain.RoleMember,
			MembershipStatus: orgDomain.MemberStatusActive,
		}
		req = req.WithContext(orgHttpapi.WithTenantContext(req.Context(), tenantCtx))
		rec := httptest.NewRecorder()

		handler.VerifyBackupRun(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp struct {
			Data    VerifyBackupRunResponse `json:"data"`
			Message string                  `json:"message"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed decoding response: %v", err)
		}
		if resp.Data.Details.ExtractedSampleCheck != "not_applicable" {
			t.Errorf("expected not_applicable, got %s", resp.Data.Details.ExtractedSampleCheck)
		}
	})

	t.Run("POST /api/v1/backup-runs/{id}/verify returns 403 Forbidden for viewer", func(t *testing.T) {
		mockVer := &mockVerifier{
			verifyRunFunc: func(ctx context.Context, role orgDomain.Role, oID, rID uuid.UUID) (*service.RunVerificationResult, error) {
				return nil, domain.ErrUnauthorizedRole
			},
		}

		handler := NewHandler(nil, nil, nil, nil, mockVer, nil)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/backup-runs/"+runID.String()+"/verify", nil)
		req.SetPathValue("id", runID.String())
		tenantCtx := &orgHttpapi.TenantContext{
			UserID:           userID,
			OrganizationID:   orgID,
			Role:             orgDomain.RoleViewer,
			MembershipStatus: orgDomain.MemberStatusActive,
		}
		req = req.WithContext(orgHttpapi.WithTenantContext(req.Context(), tenantCtx))
		rec := httptest.NewRecorder()

		handler.VerifyBackupRun(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 Forbidden, got %d", rec.Code)
		}
	})

	t.Run("POST /api/v1/backup-runs/{id}/verify returns 400 Bad Request on invalid UUID", func(t *testing.T) {
		handler := NewHandler(nil, nil, nil, nil, nil, nil)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/backup-runs/invalid-uuid/verify", nil)
		req.SetPathValue("id", "invalid-uuid")
		tenantCtx := &orgHttpapi.TenantContext{
			UserID:           userID,
			OrganizationID:   orgID,
			Role:             orgDomain.RoleAdmin,
			MembershipStatus: orgDomain.MemberStatusActive,
		}
		req = req.WithContext(orgHttpapi.WithTenantContext(req.Context(), tenantCtx))
		rec := httptest.NewRecorder()

		handler.VerifyBackupRun(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request, got %d", rec.Code)
		}
	})

	t.Run("POST /api/v1/backup-runs/{id}/verify returns 404 Not Found on missing run", func(t *testing.T) {
		mockVer := &mockVerifier{
			verifyRunFunc: func(ctx context.Context, role orgDomain.Role, oID, rID uuid.UUID) (*service.RunVerificationResult, error) {
				return nil, domain.ErrRunNotFound
			},
		}

		handler := NewHandler(nil, nil, nil, nil, mockVer, nil)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/backup-runs/"+runID.String()+"/verify", nil)
		req.SetPathValue("id", runID.String())
		tenantCtx := &orgHttpapi.TenantContext{
			UserID:           userID,
			OrganizationID:   orgID,
			Role:             orgDomain.RoleAdmin,
			MembershipStatus: orgDomain.MemberStatusActive,
		}
		req = req.WithContext(orgHttpapi.WithTenantContext(req.Context(), tenantCtx))
		rec := httptest.NewRecorder()

		handler.VerifyBackupRun(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 Not Found, got %d", rec.Code)
		}
	})

	t.Run("POST /api/v1/backup-runs/{id}/verify returns 422 Unprocessable Entity when no verifiable artifacts remain", func(t *testing.T) {
		mockVer := &mockVerifier{
			verifyRunFunc: func(ctx context.Context, role orgDomain.Role, oID, rID uuid.UUID) (*service.RunVerificationResult, error) {
				return nil, domain.ErrNoVerifiableArtifacts
			},
		}

		handler := NewHandler(nil, nil, nil, nil, mockVer, nil)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/backup-runs/"+runID.String()+"/verify", nil)
		req.SetPathValue("id", runID.String())
		tenantCtx := &orgHttpapi.TenantContext{
			UserID:           userID,
			OrganizationID:   orgID,
			Role:             orgDomain.RoleAdmin,
			MembershipStatus: orgDomain.MemberStatusActive,
		}
		req = req.WithContext(orgHttpapi.WithTenantContext(req.Context(), tenantCtx))
		rec := httptest.NewRecorder()

		handler.VerifyBackupRun(rec, req)

		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("expected 422 Unprocessable Entity, got %d", rec.Code)
		}
	})

	t.Run("POST /api/v1/backup-runs/{id}/verify returns 503 Service Unavailable on infrastructure error", func(t *testing.T) {
		mockVer := &mockVerifier{
			verifyRunFunc: func(ctx context.Context, role orgDomain.Role, oID, rID uuid.UUID) (*service.RunVerificationResult, error) {
				return nil, domain.ErrBackupServiceUnavailable
			},
		}

		handler := NewHandler(nil, nil, nil, nil, mockVer, nil)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/backup-runs/"+runID.String()+"/verify", nil)
		req.SetPathValue("id", runID.String())
		tenantCtx := &orgHttpapi.TenantContext{
			UserID:           userID,
			OrganizationID:   orgID,
			Role:             orgDomain.RoleAdmin,
			MembershipStatus: orgDomain.MemberStatusActive,
		}
		req = req.WithContext(orgHttpapi.WithTenantContext(req.Context(), tenantCtx))
		rec := httptest.NewRecorder()

		handler.VerifyBackupRun(rec, req)

		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("expected 503 Service Unavailable, got %d", rec.Code)
		}
	})

	t.Run("POST /api/v1/backup-runs/{id}/verify returns 200 OK with failed message and conservative details on corruption", func(t *testing.T) {
		mockVer := &mockVerifier{
			verifyRunFunc: func(ctx context.Context, role orgDomain.Role, oID, rID uuid.UUID) (*service.RunVerificationResult, error) {
				return &service.RunVerificationResult{
					RunID:              runID,
					VerificationStatus: domain.VerificationStatusFailed,
					VerifiedAt:         now,
					Details: service.VerificationDetails{
						ChecksumMatched:      false,
						ArchiveIntegrity:     "failed",
						CompressionValid:     false,
						ExtractedSampleCheck: "failed",
					},
				}, nil
			},
		}

		handler := NewHandler(nil, nil, nil, nil, mockVer, nil)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/backup-runs/"+runID.String()+"/verify", nil)
		req.SetPathValue("id", runID.String())
		tenantCtx := &orgHttpapi.TenantContext{
			UserID:           userID,
			OrganizationID:   orgID,
			Role:             orgDomain.RoleAdmin,
			MembershipStatus: orgDomain.MemberStatusActive,
		}
		req = req.WithContext(orgHttpapi.WithTenantContext(req.Context(), tenantCtx))
		rec := httptest.NewRecorder()

		handler.VerifyBackupRun(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp struct {
			Data    VerifyBackupRunResponse `json:"data"`
			Message string                  `json:"message"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed decoding response: %v", err)
		}
		if resp.Data.VerificationStatus != domain.VerificationStatusFailed {
			t.Errorf("expected status failed, got: %s", resp.Data.VerificationStatus)
		}
		if resp.Message != "اعتبارسنجی انجام شد اما یکپارچگی یک یا چند آرتیفکت تأیید نشد." {
			t.Errorf("expected safe failed message, got: %s", resp.Message)
		}
		if strings.Contains(resp.Message, "تأیید گردید") {
			t.Errorf("failed response message must NOT claim verification succeeded: %s", resp.Message)
		}
		if resp.Data.Details.ChecksumMatched != false || resp.Data.Details.ArchiveIntegrity != "failed" || resp.Data.Details.CompressionValid != false || resp.Data.Details.ExtractedSampleCheck != "failed" {
			t.Errorf("unexpected details on failure: %+v", resp.Data.Details)
		}
	})
}
