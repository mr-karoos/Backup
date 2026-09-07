package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"backup-platform/internal/backup/domain"
	identityDomain "backup-platform/internal/identity/domain"
	identityHttpapi "backup-platform/internal/identity/httpapi"
	orgAuthz "backup-platform/internal/organization/authz"
	orgDomain "backup-platform/internal/organization/domain"
	orgHttpapi "backup-platform/internal/organization/httpapi"
	orgRepo "backup-platform/internal/organization/repository"
	"backup-platform/internal/platform/database"
	platformHttpapi "backup-platform/internal/platform/httpapi"
	"backup-platform/pkg/uuid"
)

type mockMaintenanceJobReader struct {
	listJobsFunc func(ctx context.Context, role orgDomain.Role, orgID uuid.UUID, filter domain.MaintenanceJobFilter) (*domain.MaintenanceJobListResult, error)
	getJobFunc  func(ctx context.Context, role orgDomain.Role, orgID, jobID uuid.UUID) (*domain.MaintenanceJobDetail, error)
}

func (m *mockMaintenanceJobReader) ListMaintenanceJobs(ctx context.Context, role orgDomain.Role, orgID uuid.UUID, filter domain.MaintenanceJobFilter) (*domain.MaintenanceJobListResult, error) {
	if m.listJobsFunc != nil {
		return m.listJobsFunc(ctx, role, orgID, filter)
	}
	return nil, nil
}

func (m *mockMaintenanceJobReader) GetMaintenanceJobDetail(ctx context.Context, role orgDomain.Role, orgID, jobID uuid.UUID) (*domain.MaintenanceJobDetail, error) {
	if m.getJobFunc != nil {
		return m.getJobFunc(ctx, role, orgID, jobID)
	}
	return nil, nil
}

func TestHandler_ListMaintenanceJobs(t *testing.T) {
	orgID := uuid.New()
	tenantCtx := &orgHttpapi.TenantContext{
		OrganizationID: orgID,
		Role:           orgDomain.RoleMember,
	}

	t.Run("missing tenant context returns 403", func(t *testing.T) {
		h := NewHandler(nil, nil, nil, nil, nil, nil)
		h.SetMaintenanceService(&mockMaintenanceJobReader{})

		req := httptest.NewRequest(http.MethodGet, "/api/v1/maintenance-jobs", nil)
		w := httptest.NewRecorder()

		h.ListMaintenanceJobs(w, req)
		if w.Code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d", w.Code)
		}
	})

	t.Run("rejects unknown query parameter with 400", func(t *testing.T) {
		h := NewHandler(nil, nil, nil, nil, nil, nil)
		h.SetMaintenanceService(&mockMaintenanceJobReader{})

		req := httptest.NewRequest(http.MethodGet, "/api/v1/maintenance-jobs?foo=bar", nil)
		req = req.WithContext(orgHttpapi.WithTenantContext(req.Context(), tenantCtx))
		w := httptest.NewRecorder()

		h.ListMaintenanceJobs(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for unknown query param, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "unknown query parameter: foo") {
			t.Fatalf("expected error message mentioning unknown query parameter, got %s", w.Body.String())
		}
	})

	t.Run("validates limit bounds (1..100)", func(t *testing.T) {
		h := NewHandler(nil, nil, nil, nil, nil, nil)
		h.SetMaintenanceService(&mockMaintenanceJobReader{})

		invalidLimits := []string{"0", "-5", "101", "abc"}
		for _, l := range invalidLimits {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/maintenance-jobs?limit="+l, nil)
			req = req.WithContext(orgHttpapi.WithTenantContext(req.Context(), tenantCtx))
			w := httptest.NewRecorder()

			h.ListMaintenanceJobs(w, req)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected 400 for limit=%s, got %d", l, w.Code)
			}
		}
	})

	t.Run("validates repository_id format", func(t *testing.T) {
		h := NewHandler(nil, nil, nil, nil, nil, nil)
		h.SetMaintenanceService(&mockMaintenanceJobReader{})

		req := httptest.NewRequest(http.MethodGet, "/api/v1/maintenance-jobs?repository_id=not-a-uuid", nil)
		req = req.WithContext(orgHttpapi.WithTenantContext(req.Context(), tenantCtx))
		w := httptest.NewRecorder()

		h.ListMaintenanceJobs(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for bad repository_id, got %d", w.Code)
		}
	})

	t.Run("validates status parameter", func(t *testing.T) {
		h := NewHandler(nil, nil, nil, nil, nil, nil)
		h.SetMaintenanceService(&mockMaintenanceJobReader{})

		req := httptest.NewRequest(http.MethodGet, "/api/v1/maintenance-jobs?status=invalid_status", nil)
		req = req.WithContext(orgHttpapi.WithTenantContext(req.Context(), tenantCtx))
		w := httptest.NewRecorder()

		h.ListMaintenanceJobs(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for bad status, got %d", w.Code)
		}
	})

	t.Run("validates operation_type parameter", func(t *testing.T) {
		h := NewHandler(nil, nil, nil, nil, nil, nil)
		h.SetMaintenanceService(&mockMaintenanceJobReader{})

		req := httptest.NewRequest(http.MethodGet, "/api/v1/maintenance-jobs?operation_type=unknown_op", nil)
		req = req.WithContext(orgHttpapi.WithTenantContext(req.Context(), tenantCtx))
		w := httptest.NewRecorder()

		h.ListMaintenanceJobs(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for bad operation_type, got %d", w.Code)
		}
	})

	t.Run("validates cursor format", func(t *testing.T) {
		h := NewHandler(nil, nil, nil, nil, nil, nil)
		h.SetMaintenanceService(&mockMaintenanceJobReader{})

		req := httptest.NewRequest(http.MethodGet, "/api/v1/maintenance-jobs?cursor=corrupted-base64", nil)
		req = req.WithContext(orgHttpapi.WithTenantContext(req.Context(), tenantCtx))
		w := httptest.NewRecorder()

		h.ListMaintenanceJobs(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for corrupted cursor, got %d", w.Code)
		}
	})

	t.Run("successful list response with anti-caching headers and pagination metadata", func(t *testing.T) {
		jobID := uuid.New()
		repoID := uuid.New()
		nextCursor := "next-cursor-token"

		mockReader := &mockMaintenanceJobReader{
			listJobsFunc: func(ctx context.Context, role orgDomain.Role, oID uuid.UUID, filter domain.MaintenanceJobFilter) (*domain.MaintenanceJobListResult, error) {
				return &domain.MaintenanceJobListResult{
					Jobs: []*domain.MaintenanceJob{
						{
							ID:             jobID,
							OrganizationID: oID,
							RepositoryID:   repoID,
							OperationType:  domain.MaintenanceOpResticPrune,
							Status:         domain.MaintenanceJobCompleted,
							AttemptCount:   1,
							MaxAttempts:    3,
							CreatedAt:      time.Now(),
							UpdatedAt:      time.Now(),
							Metadata:       []byte(`{"internal_secret":"do_not_leak"}`),
						},
					},
					NextCursor: &nextCursor,
					HasMore:    true,
				}, nil
			},
		}

		h := NewHandler(nil, nil, nil, nil, nil, nil)
		h.SetMaintenanceService(mockReader)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/maintenance-jobs?limit=10", nil)
		req = req.WithContext(orgHttpapi.WithTenantContext(req.Context(), tenantCtx))
		w := httptest.NewRecorder()

		h.ListMaintenanceJobs(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}

		// Verify anti-caching headers
		if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
			t.Fatalf("expected Cache-Control: no-store, got %q", cc)
		}
		if pragma := w.Header().Get("Pragma"); pragma != "no-cache" {
			t.Fatalf("expected Pragma: no-cache, got %q", pragma)
		}

		var resp MaintenanceJobListResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed unmarshaling response: %v", err)
		}

		if len(resp.Data) != 1 {
			t.Fatalf("expected 1 job, got %d", len(resp.Data))
		}
		if resp.Data[0].ID != jobID {
			t.Fatalf("expected job ID %s, got %s", jobID, resp.Data[0].ID)
		}
		if !resp.Page.HasMore {
			t.Fatalf("expected has_more: true")
		}
		if resp.Page.NextCursor == nil || *resp.Page.NextCursor != nextCursor {
			t.Fatalf("expected next_cursor %q, got %v", nextCursor, resp.Page.NextCursor)
		}

		// Verify sensitive fields not exposed
		rawJSON := w.Body.String()
		if strings.Contains(rawJSON, "internal_secret") || strings.Contains(rawJSON, "organization_id") {
			t.Fatalf("SECURITY VIOLATION: sensitive internal fields exposed in response: %s", rawJSON)
		}
	})
}

func TestHandler_GetMaintenanceJob(t *testing.T) {
	orgID := uuid.New()
	tenantCtx := &orgHttpapi.TenantContext{
		OrganizationID: orgID,
		Role:           orgDomain.RoleViewer,
	}

	t.Run("missing tenant context returns 403", func(t *testing.T) {
		h := NewHandler(nil, nil, nil, nil, nil, nil)
		h.SetMaintenanceService(&mockMaintenanceJobReader{})

		req := httptest.NewRequest(http.MethodGet, "/api/v1/maintenance-jobs/"+uuid.New().String(), nil)
		w := httptest.NewRecorder()

		h.GetMaintenanceJob(w, req)
		if w.Code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d", w.Code)
		}
	})

	t.Run("invalid UUID in path returns 400", func(t *testing.T) {
		h := NewHandler(nil, nil, nil, nil, nil, nil)
		h.SetMaintenanceService(&mockMaintenanceJobReader{})

		req := httptest.NewRequest(http.MethodGet, "/api/v1/maintenance-jobs/not-a-valid-uuid", nil)
		req.SetPathValue("id", "not-a-valid-uuid")
		req = req.WithContext(orgHttpapi.WithTenantContext(req.Context(), tenantCtx))
		w := httptest.NewRecorder()

		h.GetMaintenanceJob(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for bad path ID, got %d", w.Code)
		}
	})

	t.Run("returns 404 when job not found (anti-enumeration)", func(t *testing.T) {
		jobID := uuid.New()
		mockReader := &mockMaintenanceJobReader{
			getJobFunc: func(ctx context.Context, role orgDomain.Role, oID, jID uuid.UUID) (*domain.MaintenanceJobDetail, error) {
				return nil, domain.ErrJobNotFound
			},
		}

		h := NewHandler(nil, nil, nil, nil, nil, nil)
		h.SetMaintenanceService(mockReader)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/maintenance-jobs/"+jobID.String(), nil)
		req.SetPathValue("id", jobID.String())
		req = req.WithContext(orgHttpapi.WithTenantContext(req.Context(), tenantCtx))
		w := httptest.NewRecorder()

		h.GetMaintenanceJob(w, req)
		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", w.Code)
		}
	})

	t.Run("successful retrieval with runs and redacted error summary", func(t *testing.T) {
		jobID := uuid.New()
		repoID := uuid.New()
		runID := uuid.New()
		now := time.Now()
		startedAt := now.Add(-5 * time.Second)
		endedAt := now
		rawError := "failed restic execution: password=secret123 was invalid"

		mockReader := &mockMaintenanceJobReader{
			getJobFunc: func(ctx context.Context, role orgDomain.Role, oID, jID uuid.UUID) (*domain.MaintenanceJobDetail, error) {
				return &domain.MaintenanceJobDetail{
					Job: &domain.MaintenanceJob{
						ID:             jobID,
						OrganizationID: oID,
						RepositoryID:   repoID,
						OperationType:  domain.MaintenanceOpResticPrune,
						Status:         domain.MaintenanceJobFailed,
						AttemptCount:   1,
						MaxAttempts:    3,
						CreatedAt:      startedAt,
						UpdatedAt:      endedAt,
						Metadata:       []byte(`{"admin_key":"super_secret"}`),
					},
					Runs: []*domain.MaintenanceRun{
						{
							ID:             runID,
							OrganizationID: oID,
							JobID:          jobID,
							AttemptNumber:  1,
							Status:         domain.MaintenanceRunFailed,
							StartedAt:      startedAt,
							EndedAt:        &endedAt,
							ErrorMessage:   &rawError,
							LogsSummary:    []byte(`[{"secret_log":"do_not_leak"}]`),
							CreatedAt:      startedAt,
							UpdatedAt:      endedAt,
						},
					},
				}, nil
			},
		}

		h := NewHandler(nil, nil, nil, nil, nil, nil)
		h.SetMaintenanceService(mockReader)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/maintenance-jobs/"+jobID.String(), nil)
		req.SetPathValue("id", jobID.String())
		req = req.WithContext(orgHttpapi.WithTenantContext(req.Context(), tenantCtx))
		w := httptest.NewRecorder()

		h.GetMaintenanceJob(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}

		// Verify anti-caching headers
		if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
			t.Fatalf("expected Cache-Control: no-store, got %q", cc)
		}
		if pragma := w.Header().Get("Pragma"); pragma != "no-cache" {
			t.Fatalf("expected Pragma: no-cache, got %q", pragma)
		}

		var envelope struct {
			Data MaintenanceJobDetailDTO `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
			t.Fatalf("failed unmarshaling response: %v", err)
		}
		resp := envelope.Data

		if resp.ID != jobID {
			t.Fatalf("expected job ID %s, got %s", jobID, resp.ID)
		}
		if len(resp.Runs) != 1 {
			t.Fatalf("expected 1 run, got %d", len(resp.Runs))
		}
		if resp.Runs[0].ID != runID {
			t.Fatalf("expected run ID %s, got %s", runID, resp.Runs[0].ID)
		}
		if resp.Runs[0].DurationMS == nil || *resp.Runs[0].DurationMS < 5000 {
			t.Fatalf("expected duration >= 5000ms, got %v", resp.Runs[0].DurationMS)
		}
		if resp.Runs[0].ErrorSummary == nil {
			t.Fatalf("expected error summary to be present")
		}

		rawJSON := w.Body.String()
		if strings.Contains(rawJSON, "admin_key") || strings.Contains(rawJSON, "secret_log") || strings.Contains(rawJSON, "organization_id") {
			t.Fatalf("SECURITY VIOLATION: sensitive metadata/logs leaked in detail response: %s", rawJSON)
		}
	})
}

func TestHandler_MaintenanceJobs_RBACMatrix(t *testing.T) {
	orgID := uuid.New()
	jobID := uuid.New()

	mockReader := &mockMaintenanceJobReader{
		listJobsFunc: func(ctx context.Context, role orgDomain.Role, oID uuid.UUID, filter domain.MaintenanceJobFilter) (*domain.MaintenanceJobListResult, error) {
			return &domain.MaintenanceJobListResult{Jobs: []*domain.MaintenanceJob{}}, nil
		},
		getJobFunc: func(ctx context.Context, role orgDomain.Role, oID, jID uuid.UUID) (*domain.MaintenanceJobDetail, error) {
			return &domain.MaintenanceJobDetail{
				Job: &domain.MaintenanceJob{ID: jID, OrganizationID: oID, OperationType: domain.MaintenanceOpResticPrune},
			}, nil
		},
	}

	h := NewHandler(nil, nil, nil, nil, nil, nil)
	h.SetMaintenanceService(mockReader)

	roles := []orgDomain.Role{
		orgDomain.RoleAdmin,
		orgDomain.RoleMember,
		orgDomain.RoleViewer,
	}

	for _, r := range roles {
		t.Run("list jobs allowed for role "+string(r), func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/maintenance-jobs", nil)
			req = req.WithContext(orgHttpapi.WithTenantContext(req.Context(), &orgHttpapi.TenantContext{
				OrganizationID: orgID,
				Role:           r,
			}))
			w := httptest.NewRecorder()
			h.ListMaintenanceJobs(w, req)
			if w.Code != http.StatusOK {
				t.Fatalf("expected 200 OK for role %s, got %d", r, w.Code)
			}
		})

		t.Run("get job detail allowed for role "+string(r), func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/maintenance-jobs/"+jobID.String(), nil)
			req.SetPathValue("id", jobID.String())
			req = req.WithContext(orgHttpapi.WithTenantContext(req.Context(), &orgHttpapi.TenantContext{
				OrganizationID: orgID,
				Role:           r,
			}))
			w := httptest.NewRecorder()
			h.GetMaintenanceJob(w, req)
			if w.Code != http.StatusOK {
				t.Fatalf("expected 200 OK for role %s, got %d", r, w.Code)
			}
		})
	}

	t.Run("handler direct call without TenantContext rejected with 403 (defense-in-depth)", func(t *testing.T) {
		reqList := httptest.NewRequest(http.MethodGet, "/api/v1/maintenance-jobs", nil)
		wList := httptest.NewRecorder()
		h.ListMaintenanceJobs(wList, reqList)
		if wList.Code != http.StatusForbidden {
			t.Fatalf("expected 403 Forbidden, got %d", wList.Code)
		}

		reqGet := httptest.NewRequest(http.MethodGet, "/api/v1/maintenance-jobs/"+jobID.String(), nil)
		reqGet.SetPathValue("id", jobID.String())
		wGet := httptest.NewRecorder()
		h.GetMaintenanceJob(wGet, reqGet)
		if wGet.Code != http.StatusForbidden {
			t.Fatalf("expected 403 Forbidden, got %d", wGet.Code)
		}
	})
}

func TestHandler_MaintenanceJobs_AntiEnumeration(t *testing.T) {
	orgID := uuid.New()
	foreignRepoID := uuid.New()

	mockReader := &mockMaintenanceJobReader{
		listJobsFunc: func(ctx context.Context, role orgDomain.Role, oID uuid.UUID, filter domain.MaintenanceJobFilter) (*domain.MaintenanceJobListResult, error) {
			// Anti-enumeration: foreign or unknown repo returns empty list, no error
			return &domain.MaintenanceJobListResult{
				Jobs:       []*domain.MaintenanceJob{},
				NextCursor: nil,
				HasMore:    false,
			}, nil
		},
		getJobFunc: func(ctx context.Context, role orgDomain.Role, oID, jID uuid.UUID) (*domain.MaintenanceJobDetail, error) {
			return nil, domain.ErrJobNotFound
		},
	}

	h := NewHandler(nil, nil, nil, nil, nil, nil)
	h.SetMaintenanceService(mockReader)

	tenantCtx := &orgHttpapi.TenantContext{
		OrganizationID: orgID,
		Role:           orgDomain.RoleViewer,
	}

	t.Run("invalid repository_id format rejected with 400", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/maintenance-jobs?repository_id=malformed-uuid", nil)
		req = req.WithContext(orgHttpapi.WithTenantContext(req.Context(), tenantCtx))
		w := httptest.NewRecorder()
		h.ListMaintenanceJobs(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request, got %d", w.Code)
		}
	})

	t.Run("foreign repository_id returns 200 with empty list (safe fail-closed)", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/maintenance-jobs?repository_id="+foreignRepoID.String(), nil)
		req = req.WithContext(orgHttpapi.WithTenantContext(req.Context(), tenantCtx))
		w := httptest.NewRecorder()
		h.ListMaintenanceJobs(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", w.Code)
		}

		var envelope struct {
			Data []MaintenanceJobDTO `json:"data"`
			Page PaginationMeta      `json:"page"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
			t.Fatalf("failed unmarshaling: %v", err)
		}
		if len(envelope.Data) != 0 || envelope.Page.HasMore || envelope.Page.NextCursor != nil {
			t.Fatalf("expected empty data without pagination, got: %+v", envelope)
		}
	})

	t.Run("foreign or missing job ID returns 404", func(t *testing.T) {
		foreignJobID := uuid.New()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/maintenance-jobs/"+foreignJobID.String(), nil)
		req.SetPathValue("id", foreignJobID.String())
		req = req.WithContext(orgHttpapi.WithTenantContext(req.Context(), tenantCtx))
		w := httptest.NewRecorder()
		h.GetMaintenanceJob(w, req)
		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404 Not Found, got %d", w.Code)
		}
	})
}

func TestHandler_MaintenanceJobs_DetailSecurityAndSanitization(t *testing.T) {
	orgID := uuid.New()
	jobID := uuid.New()
	repoID := uuid.New()
	runID := uuid.New()

	// 2000-byte error message with multibyte Unicode characters at the 1024-byte boundary
	prefix := strings.Repeat("a", 1005)
	multibyteRune := "گچپژ" // 2-byte Persian characters
	rawLongError := prefix + multibyteRune + strings.Repeat("z", 1000)

	mockReader := &mockMaintenanceJobReader{
		getJobFunc: func(ctx context.Context, role orgDomain.Role, oID, jID uuid.UUID) (*domain.MaintenanceJobDetail, error) {
			now := time.Now().UTC()
			return &domain.MaintenanceJobDetail{
				Job: &domain.MaintenanceJob{
					ID:             jobID,
					OrganizationID: oID,
					RepositoryID:   repoID,
					OperationType:  domain.MaintenanceOpResticPrune,
					Status:         domain.MaintenanceJobFailed,
					AttemptCount:   2,
					MaxAttempts:    3,
					CreatedAt:      now.Add(-10 * time.Minute),
					UpdatedAt:      now,
					Metadata:       []byte(`{"internal_token":"topsecret","db_password":"leak"}`),
				},
				Runs: []*domain.MaintenanceRun{
					{
						ID:             runID,
						OrganizationID: oID,
						JobID:          jobID,
						AttemptNumber:  1,
						Status:         domain.MaintenanceRunFailed,
						StartedAt:      now.Add(-9 * time.Minute),
						EndedAt:        &now,
						ErrorMessage:   &rawLongError,
						LogsSummary:    []byte(`{"stdout":"raw stdout","stderr":"raw stderr"}`),
						CreatedAt:      now.Add(-9 * time.Minute),
					},
				},
			}, nil
		},
	}

	h := NewHandler(nil, nil, nil, nil, nil, nil)
	h.SetMaintenanceService(mockReader)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/maintenance-jobs/"+jobID.String(), nil)
	req.SetPathValue("id", jobID.String())
	req = req.WithContext(orgHttpapi.WithTenantContext(req.Context(), &orgHttpapi.TenantContext{
		OrganizationID: orgID,
		Role:           orgDomain.RoleMember,
	}))
	w := httptest.NewRecorder()
	h.GetMaintenanceJob(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", w.Code)
	}

	bodyStr := w.Body.String()

	// 1. Verify NO leak of sensitive fields
	forbiddenSubstrings := []string{
		"topsecret",
		"db_password",
		"raw stdout",
		"raw stderr",
		"internal_token",
		"organization_id",
		orgID.String(),
	}
	for _, forbidden := range forbiddenSubstrings {
		if strings.Contains(bodyStr, forbidden) {
			t.Fatalf("SECURITY LEAK: response contains sensitive string %q: %s", forbidden, bodyStr)
		}
	}

	// 2. Verify error_summary is bounded to 1024 bytes and valid UTF-8
	var envelope struct {
		Data MaintenanceJobDetailDTO `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("failed unmarshaling: %v", err)
	}

	run := envelope.Data.Runs[0]
	if run.ErrorSummary == nil {
		t.Fatalf("expected error_summary to be populated")
	}
	summary := *run.ErrorSummary
	if len(summary) > 1024 {
		t.Fatalf("expected error_summary <= 1024 bytes, got %d bytes", len(summary))
	}
	if !utf8.ValidString(summary) {
		t.Fatalf("error_summary contains invalid UTF-8 sequence!")
	}
	if !strings.HasSuffix(summary, "... [truncated]") {
		t.Fatalf("expected error_summary to have truncation suffix, got %q", summary)
	}
}

type routeTestQuerier struct{}

func (q *routeTestQuerier) Exec(ctx context.Context, sql string, arguments ...any) (any, error) {
	return nil, nil
}

type routeTestTxManager struct{}

func (m *routeTestTxManager) Querier() database.Querier { return nil }
func (m *routeTestTxManager) WithinTx(ctx context.Context, fn func(q database.Querier) error) error {
	return fn(nil)
}

type routeTestMemberRepo struct {
	members map[string]*orgRepo.UserMembershipWithOrg
}

func (r *routeTestMemberRepo) Create(ctx context.Context, q database.Querier, member *orgDomain.Member) error {
	return nil
}
func (r *routeTestMemberRepo) FindMembership(ctx context.Context, q database.Querier, orgID, userID uuid.UUID) (*orgDomain.Member, error) {
	return nil, nil
}
func (r *routeTestMemberRepo) FindActiveMembershipWithOrg(ctx context.Context, q database.Querier, orgID, userID uuid.UUID) (*orgRepo.UserMembershipWithOrg, error) {
	k := orgID.String() + ":" + userID.String()
	m, ok := r.members[k]
	if !ok {
		return nil, nil
	}
	return m, nil
}
func (r *routeTestMemberRepo) ListUserOrganizations(ctx context.Context, q database.Querier, userID uuid.UUID) ([]*orgDomain.Organization, error) {
	return nil, nil
}
func (r *routeTestMemberRepo) ListUserMembershipsWithOrg(ctx context.Context, q database.Querier, userID uuid.UUID) ([]*orgRepo.UserMembershipWithOrg, error) {
	return nil, nil
}

func TestMaintenanceJobs_RouteChain_Semantics(t *testing.T) {
	orgID := uuid.New()
	userID := uuid.New()
	jobID := uuid.New()
	validToken := "valid-secret-token"

	// Mock authMiddleware that mimics canonical identityHttpapi.NewAuthMiddleware
	authMiddleware := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := strings.TrimSpace(r.Header.Get("Authorization"))
			if authHeader == "" || authHeader != "Bearer "+validToken {
				platformHttpapi.WriteError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required", nil)
				return
			}
			ctx := identityHttpapi.WithAuthContext(r.Context(), &identityHttpapi.AuthContext{
				UserID:        userID,
				SessionID:     uuid.New(),
				IsSystemAdmin: false,
				Status:        identityDomain.UserStatusActive,
			})
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}

	mockMembers := make(map[string]*orgRepo.UserMembershipWithOrg)
	memberRepo := &routeTestMemberRepo{members: mockMembers}
	txManager := &routeTestTxManager{}
	orgContextMiddleware := orgHttpapi.NewOrganizationContextMiddleware(memberRepo, txManager, nil)

	mockReader := &mockMaintenanceJobReader{
		listJobsFunc: func(ctx context.Context, role orgDomain.Role, oID uuid.UUID, filter domain.MaintenanceJobFilter) (*domain.MaintenanceJobListResult, error) {
			return &domain.MaintenanceJobListResult{Jobs: []*domain.MaintenanceJob{}}, nil
		},
		getJobFunc: func(ctx context.Context, role orgDomain.Role, oID, jID uuid.UUID) (*domain.MaintenanceJobDetail, error) {
			return &domain.MaintenanceJobDetail{
				Job: &domain.MaintenanceJob{ID: jID, OrganizationID: oID, OperationType: domain.MaintenanceOpResticPrune},
			}, nil
		},
	}

	h := NewHandler(nil, nil, nil, nil, nil, nil)
	h.SetMaintenanceService(mockReader)

	// Build EXACT route-level chains as registered in cmd/server/main.go:
	// authMiddleware(orgContextMiddleware(orgHttpapi.RequirePermission(authz.PermissionMaintenanceRead, log)(handler)))
	listRoute := authMiddleware(orgContextMiddleware(orgHttpapi.RequirePermission(orgAuthz.PermissionMaintenanceRead, nil)(http.HandlerFunc(h.ListMaintenanceJobs))))
	getRoute := authMiddleware(orgContextMiddleware(orgHttpapi.RequirePermission(orgAuthz.PermissionMaintenanceRead, nil)(http.HandlerFunc(h.GetMaintenanceJob))))

	endpoints := []struct {
		name    string
		handler http.Handler
		path    string
	}{
		{name: "GET /api/v1/maintenance-jobs", handler: listRoute, path: "/api/v1/maintenance-jobs"},
		{name: "GET /api/v1/maintenance-jobs/{id}", handler: getRoute, path: "/api/v1/maintenance-jobs/" + jobID.String()},
	}

	for _, ep := range endpoints {
		t.Run(ep.name, func(t *testing.T) {
			t.Run("no Authorization / unauthenticated => 401", func(t *testing.T) {
				req := httptest.NewRequest(http.MethodGet, ep.path, nil)
				if ep.name == "GET /api/v1/maintenance-jobs/{id}" {
					req.SetPathValue("id", jobID.String())
				}
				w := httptest.NewRecorder()
				ep.handler.ServeHTTP(w, req)
				if w.Code != http.StatusUnauthorized {
					t.Fatalf("expected 401 Unauthorized, got %d", w.Code)
				}
				if !strings.Contains(w.Body.String(), `"UNAUTHORIZED"`) {
					t.Fatalf("expected UNAUTHORIZED error code, got %s", w.Body.String())
				}
			})

			t.Run("authenticated but missing X-Organization-ID => 400", func(t *testing.T) {
				req := httptest.NewRequest(http.MethodGet, ep.path, nil)
				req.Header.Set("Authorization", "Bearer "+validToken)
				if ep.name == "GET /api/v1/maintenance-jobs/{id}" {
					req.SetPathValue("id", jobID.String())
				}
				w := httptest.NewRecorder()
				ep.handler.ServeHTTP(w, req)
				if w.Code != http.StatusBadRequest {
					t.Fatalf("expected 400 Bad Request, got %d", w.Code)
				}
				if !strings.Contains(w.Body.String(), `"INVALID_ORGANIZATION_CONTEXT"`) {
					t.Fatalf("expected INVALID_ORGANIZATION_CONTEXT, got %s", w.Body.String())
				}
			})

			t.Run("authenticated but invalid X-Organization-ID format => 400", func(t *testing.T) {
				req := httptest.NewRequest(http.MethodGet, ep.path, nil)
				req.Header.Set("Authorization", "Bearer "+validToken)
				req.Header.Set("X-Organization-ID", "not-a-valid-uuid")
				if ep.name == "GET /api/v1/maintenance-jobs/{id}" {
					req.SetPathValue("id", jobID.String())
				}
				w := httptest.NewRecorder()
				ep.handler.ServeHTTP(w, req)
				if w.Code != http.StatusBadRequest {
					t.Fatalf("expected 400 Bad Request, got %d", w.Code)
				}
			})

			t.Run("authenticated but user not member of org => 404", func(t *testing.T) {
				foreignOrg := uuid.New()
				req := httptest.NewRequest(http.MethodGet, ep.path, nil)
				req.Header.Set("Authorization", "Bearer "+validToken)
				req.Header.Set("X-Organization-ID", foreignOrg.String())
				if ep.name == "GET /api/v1/maintenance-jobs/{id}" {
					req.SetPathValue("id", jobID.String())
				}
				w := httptest.NewRecorder()
				ep.handler.ServeHTTP(w, req)
				if w.Code != http.StatusNotFound {
					t.Fatalf("expected 404 Organization Not Found, got %d", w.Code)
				}
			})

			// Test RBAC for permitted roles: Admin, Member, Viewer => 200
			permittedRoles := []orgDomain.Role{orgDomain.RoleAdmin, orgDomain.RoleMember, orgDomain.RoleViewer}
			for _, role := range permittedRoles {
				t.Run("authenticated + valid tenant + "+string(role)+" => 200", func(t *testing.T) {
					k := orgID.String() + ":" + userID.String()
					mockMembers[k] = &orgRepo.UserMembershipWithOrg{
						OrganizationID:   orgID,
						OrganizationName: "Test Org",
						Slug:             "test-org",
						Role:             role,
						Status:           orgDomain.MemberStatusActive,
					}
					defer delete(mockMembers, k)

					req := httptest.NewRequest(http.MethodGet, ep.path, nil)
					req.Header.Set("Authorization", "Bearer "+validToken)
					req.Header.Set("X-Organization-ID", orgID.String())
					if ep.name == "GET /api/v1/maintenance-jobs/{id}" {
						req.SetPathValue("id", jobID.String())
					}
					w := httptest.NewRecorder()
					ep.handler.ServeHTTP(w, req)
					if w.Code != http.StatusOK {
						t.Fatalf("expected 200 OK for %s, got %d: %s", role, w.Code, w.Body.String())
					}
				})
			}

			t.Run("authenticated + valid tenant + role without maintenance:read => 403", func(t *testing.T) {
				k := orgID.String() + ":" + userID.String()
				mockMembers[k] = &orgRepo.UserMembershipWithOrg{
					OrganizationID:   orgID,
					OrganizationName: "Test Org",
					Slug:             "test-org",
					Role:             orgDomain.Role("unprivileged_guest"),
					Status:           orgDomain.MemberStatusActive,
				}
				defer delete(mockMembers, k)

				req := httptest.NewRequest(http.MethodGet, ep.path, nil)
				req.Header.Set("Authorization", "Bearer "+validToken)
				req.Header.Set("X-Organization-ID", orgID.String())
				if ep.name == "GET /api/v1/maintenance-jobs/{id}" {
					req.SetPathValue("id", jobID.String())
				}
				w := httptest.NewRecorder()
				ep.handler.ServeHTTP(w, req)
				if w.Code != http.StatusForbidden {
					t.Fatalf("expected 403 Forbidden, got %d", w.Code)
				}
				if !strings.Contains(w.Body.String(), `"INSUFFICIENT_PERMISSIONS"`) {
					t.Fatalf("expected INSUFFICIENT_PERMISSIONS, got %s", w.Body.String())
				}
			})
		})
	}
}
