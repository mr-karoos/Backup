package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"backup-platform/internal/audit/domain"
	identityHttpapi "backup-platform/internal/identity/httpapi"
	orgAuthz "backup-platform/internal/organization/authz"
	orgDomain "backup-platform/internal/organization/domain"
	orgHttpapi "backup-platform/internal/organization/httpapi"
	orgRepo "backup-platform/internal/organization/repository"
	"backup-platform/internal/platform/database"
	platformHttpapi "backup-platform/internal/platform/httpapi"
	"backup-platform/pkg/uuid"
)

type mockAuditReaderForHandler struct {
	listAuditLogsFunc func(ctx context.Context, role orgDomain.Role, orgID uuid.UUID, filter domain.AuditLogFilter) (*domain.AuditLogListResult, error)
}

func (m *mockAuditReaderForHandler) ListAuditLogs(ctx context.Context, role orgDomain.Role, orgID uuid.UUID, filter domain.AuditLogFilter) (*domain.AuditLogListResult, error) {
	if m.listAuditLogsFunc != nil {
		return m.listAuditLogsFunc(ctx, role, orgID, filter)
	}
	return &domain.AuditLogListResult{Logs: []*domain.AuditLog{}}, nil
}

func TestHandler_ListAuditLogs_Unit(t *testing.T) {
	orgID := uuid.New()
	tenantCtx := &orgHttpapi.TenantContext{
		OrganizationID: orgID,
		Role:           orgDomain.RoleAdmin,
	}

	t.Run("missing tenant context returns 403", func(t *testing.T) {
		h := NewHandler(&mockAuditReaderForHandler{}, nil)
		req := httptest.NewRequest(http.MethodGet, "/api/v1/audit-logs", nil)
		w := httptest.NewRecorder()

		h.ListAuditLogs(w, req)
		if w.Code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d", w.Code)
		}
	})

	t.Run("nil audit reader returns 503", func(t *testing.T) {
		h := NewHandler(nil, nil)
		req := httptest.NewRequest(http.MethodGet, "/api/v1/audit-logs", nil)
		req = req.WithContext(orgHttpapi.WithTenantContext(req.Context(), tenantCtx))
		w := httptest.NewRecorder()

		h.ListAuditLogs(w, req)
		if w.Code != http.StatusServiceUnavailable {
			t.Fatalf("expected 503, got %d", w.Code)
		}
	})

	t.Run("rejects unknown query parameter with 400", func(t *testing.T) {
		h := NewHandler(&mockAuditReaderForHandler{}, nil)
		req := httptest.NewRequest(http.MethodGet, "/api/v1/audit-logs?unknown_param=123", nil)
		req = req.WithContext(orgHttpapi.WithTenantContext(req.Context(), tenantCtx))
		w := httptest.NewRecorder()

		h.ListAuditLogs(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for unknown query parameter, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "unknown query parameter: unknown_param") {
			t.Fatalf("expected error message to mention unknown parameter, got %s", w.Body.String())
		}
	})

	t.Run("validates limit parameter (1..100)", func(t *testing.T) {
		h := NewHandler(&mockAuditReaderForHandler{}, nil)
		invalidLimits := []string{"0", "-1", "101", "xyz"}
		for _, l := range invalidLimits {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/audit-logs?limit="+l, nil)
			req = req.WithContext(orgHttpapi.WithTenantContext(req.Context(), tenantCtx))
			w := httptest.NewRecorder()

			h.ListAuditLogs(w, req)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected 400 for invalid limit %q, got %d", l, w.Code)
			}
		}
	})

	t.Run("validates cursor format with 400", func(t *testing.T) {
		h := NewHandler(&mockAuditReaderForHandler{}, nil)
		req := httptest.NewRequest(http.MethodGet, "/api/v1/audit-logs?cursor=not-valid-base64-cursor", nil)
		req = req.WithContext(orgHttpapi.WithTenantContext(req.Context(), tenantCtx))
		w := httptest.NewRecorder()

		h.ListAuditLogs(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for invalid cursor, got %d", w.Code)
		}
	})

	t.Run("valid canonical cursor accepted", func(t *testing.T) {
		validCursor := domain.EncodeAuditCursor(time.Now().UTC(), uuid.New())
		called := false
		mockReader := &mockAuditReaderForHandler{
			listAuditLogsFunc: func(ctx context.Context, role orgDomain.Role, oID uuid.UUID, filter domain.AuditLogFilter) (*domain.AuditLogListResult, error) {
				called = true
				if filter.CursorCreatedAt == nil || filter.CursorID == nil {
					t.Errorf("expected cursor fields to be populated")
				}
				return &domain.AuditLogListResult{Logs: []*domain.AuditLog{}}, nil
			},
		}
		h := NewHandler(mockReader, nil)
		req := httptest.NewRequest(http.MethodGet, "/api/v1/audit-logs?cursor="+validCursor, nil)
		req = req.WithContext(orgHttpapi.WithTenantContext(req.Context(), tenantCtx))
		w := httptest.NewRecorder()
		h.ListAuditLogs(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 for valid cursor, got %d: %s", w.Code, w.Body.String())
		}
		if !called {
			t.Fatalf("expected service to be called for valid cursor")
		}
	})

	t.Run("rejects cursor with leading or trailing whitespace with 400", func(t *testing.T) {
		validCursor := domain.EncodeAuditCursor(time.Now().UTC(), uuid.New())
		h := NewHandler(&mockAuditReaderForHandler{}, nil)

		// Leading whitespace (%20)
		reqLead := httptest.NewRequest(http.MethodGet, "/api/v1/audit-logs?cursor=%20"+validCursor, nil)
		reqLead = reqLead.WithContext(orgHttpapi.WithTenantContext(reqLead.Context(), tenantCtx))
		wLead := httptest.NewRecorder()
		h.ListAuditLogs(wLead, reqLead)
		if wLead.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for cursor with leading whitespace, got %d", wLead.Code)
		}

		// Trailing whitespace (%20)
		reqTrail := httptest.NewRequest(http.MethodGet, "/api/v1/audit-logs?cursor="+validCursor+"%20", nil)
		reqTrail = reqTrail.WithContext(orgHttpapi.WithTenantContext(reqTrail.Context(), tenantCtx))
		wTrail := httptest.NewRecorder()
		h.ListAuditLogs(wTrail, reqTrail)
		if wTrail.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for cursor with trailing whitespace, got %d", wTrail.Code)
		}
	})

	t.Run("rejects query parameters with surrounding whitespace with 400", func(t *testing.T) {
		h := NewHandler(&mockAuditReaderForHandler{}, nil)
		validID := uuid.New().String()

		badQueries := []string{
			"?limit=%2050%20",
			"?limit=50%20",
			"?limit=%2050",
			"?entity_id=%20" + validID,
			"?entity_id=" + validID + "%20",
			"?user_id=%20" + validID,
			"?user_id=" + validID + "%20",
			"?from=%202026-09-08T10:00:00Z",
			"?from=2026-09-08T10:00:00Z%20",
			"?to=%202026-09-08T10:00:00Z",
			"?to=2026-09-08T10:00:00Z%20",
		}

		for _, bq := range badQueries {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/audit-logs"+bq, nil)
			req = req.WithContext(orgHttpapi.WithTenantContext(req.Context(), tenantCtx))
			w := httptest.NewRecorder()
			h.ListAuditLogs(w, req)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected 400 for whitespace-padded parameter query %q, got %d: %s", bq, w.Code, w.Body.String())
			}
		}
	})

	t.Run("preserves raw exact value for action and entity_type without silent trimming", func(t *testing.T) {
		rawAction := "  backup.run.verified  "
		rawEntityType := "  backup_run  "
		capturedFilter := domain.AuditLogFilter{}

		mockReader := &mockAuditReaderForHandler{
			listAuditLogsFunc: func(ctx context.Context, role orgDomain.Role, oID uuid.UUID, filter domain.AuditLogFilter) (*domain.AuditLogListResult, error) {
				capturedFilter = filter
				return &domain.AuditLogListResult{Logs: []*domain.AuditLog{}}, nil
			},
		}

		h := NewHandler(mockReader, nil)
		req := httptest.NewRequest(http.MethodGet, "/api/v1/audit-logs?action=%20%20backup.run.verified%20%20&entity_type=%20%20backup_run%20%20", nil)
		req = req.WithContext(orgHttpapi.WithTenantContext(req.Context(), tenantCtx))
		w := httptest.NewRecorder()
		h.ListAuditLogs(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
		}
		if capturedFilter.Action == nil || *capturedFilter.Action != rawAction {
			t.Fatalf("expected raw action %q to be preserved without trimming, got %v", rawAction, capturedFilter.Action)
		}
		if capturedFilter.EntityType == nil || *capturedFilter.EntityType != rawEntityType {
			t.Fatalf("expected raw entity_type %q to be preserved without trimming, got %v", rawEntityType, capturedFilter.EntityType)
		}
	})

	t.Run("accepts valid RFC3339 year 0001 zero-time without error", func(t *testing.T) {
		mockReader := &mockAuditReaderForHandler{
			listAuditLogsFunc: func(ctx context.Context, role orgDomain.Role, oID uuid.UUID, filter domain.AuditLogFilter) (*domain.AuditLogListResult, error) {
				if filter.From == nil || !filter.From.IsZero() {
					t.Errorf("expected filter.From to be zero-time")
				}
				if filter.To == nil || !filter.To.IsZero() {
					t.Errorf("expected filter.To to be zero-time")
				}
				return &domain.AuditLogListResult{Logs: []*domain.AuditLog{}}, nil
			},
		}
		h := NewHandler(mockReader, nil)
		req := httptest.NewRequest(http.MethodGet, "/api/v1/audit-logs?from=0001-01-01T00:00:00Z&to=0001-01-01T00:00:00Z", nil)
		req = req.WithContext(orgHttpapi.WithTenantContext(req.Context(), tenantCtx))
		w := httptest.NewRecorder()
		h.ListAuditLogs(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK for valid RFC3339 year 0001 timestamps, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("rejects empty values for singleton query parameters with 400", func(t *testing.T) {
		h := NewHandler(&mockAuditReaderForHandler{}, nil)
		emptyParams := []string{
			"?limit=",
			"?cursor=",
			"?action=",
			"?action=%20%20",
			"?entity_type=",
			"?entity_type=%20%20",
			"?entity_id=",
			"?user_id=",
			"?from=",
			"?to=",
		}
		for _, ep := range emptyParams {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/audit-logs"+ep, nil)
			req = req.WithContext(orgHttpapi.WithTenantContext(req.Context(), tenantCtx))
			w := httptest.NewRecorder()
			h.ListAuditLogs(w, req)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected 400 for empty parameter query %q, got %d: %s", ep, w.Code, w.Body.String())
			}
		}
	})

	t.Run("rejects duplicate singleton query parameters with 400", func(t *testing.T) {
		h := NewHandler(&mockAuditReaderForHandler{}, nil)
		id1 := uuid.New().String()
		id2 := uuid.New().String()
		dupParams := []string{
			"?limit=10&limit=20",
			"?cursor=abc&cursor=def",
			"?action=backup.download&action=backup.delete",
			"?entity_type=resource&entity_type=backup_artifact",
			"?entity_id=" + id1 + "&entity_id=" + id2,
			"?user_id=" + id1 + "&user_id=" + id2,
			"?from=2026-09-08T00:00:00Z&from=2026-09-08T01:00:00Z",
			"?to=2026-09-08T00:00:00Z&to=2026-09-08T01:00:00Z",
		}
		for _, dp := range dupParams {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/audit-logs"+dp, nil)
			req = req.WithContext(orgHttpapi.WithTenantContext(req.Context(), tenantCtx))
			w := httptest.NewRecorder()
			h.ListAuditLogs(w, req)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected 400 for duplicate parameter query %q, got %d: %s", dp, w.Code, w.Body.String())
			}
			if !strings.Contains(w.Body.String(), "duplicate query parameter") {
				t.Fatalf("expected error message to mention duplicate query parameter for %q, got: %s", dp, w.Body.String())
			}
		}
	})

	t.Run("validates action parameter (> 100) with 400", func(t *testing.T) {
		h := NewHandler(&mockAuditReaderForHandler{}, nil)
		reqLong := httptest.NewRequest(http.MethodGet, "/api/v1/audit-logs?action="+strings.Repeat("a", 101), nil)
		reqLong = reqLong.WithContext(orgHttpapi.WithTenantContext(reqLong.Context(), tenantCtx))
		wLong := httptest.NewRecorder()
		h.ListAuditLogs(wLong, reqLong)
		if wLong.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for action > 100, got %d", wLong.Code)
		}
	})

	t.Run("validates entity_type parameter (> 50) with 400", func(t *testing.T) {
		h := NewHandler(&mockAuditReaderForHandler{}, nil)
		reqLong := httptest.NewRequest(http.MethodGet, "/api/v1/audit-logs?entity_type="+strings.Repeat("e", 51), nil)
		reqLong = reqLong.WithContext(orgHttpapi.WithTenantContext(reqLong.Context(), tenantCtx))
		wLong := httptest.NewRecorder()
		h.ListAuditLogs(wLong, reqLong)
		if wLong.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for entity_type > 50, got %d", wLong.Code)
		}
	})

	t.Run("validates entity_id UUID format with 400", func(t *testing.T) {
		h := NewHandler(&mockAuditReaderForHandler{}, nil)
		req := httptest.NewRequest(http.MethodGet, "/api/v1/audit-logs?entity_id=bad-uuid", nil)
		req = req.WithContext(orgHttpapi.WithTenantContext(req.Context(), tenantCtx))
		w := httptest.NewRecorder()

		h.ListAuditLogs(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for invalid entity_id, got %d", w.Code)
		}
	})

	t.Run("validates user_id UUID format with 400", func(t *testing.T) {
		h := NewHandler(&mockAuditReaderForHandler{}, nil)
		req := httptest.NewRequest(http.MethodGet, "/api/v1/audit-logs?user_id=bad-uuid", nil)
		req = req.WithContext(orgHttpapi.WithTenantContext(req.Context(), tenantCtx))
		w := httptest.NewRecorder()

		h.ListAuditLogs(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for invalid user_id, got %d", w.Code)
		}
	})

	t.Run("validates from/to RFC3339 timestamps and from <= to with 400", func(t *testing.T) {
		h := NewHandler(&mockAuditReaderForHandler{}, nil)

		// Invalid from format
		req := httptest.NewRequest(http.MethodGet, "/api/v1/audit-logs?from=not-rfc3339", nil)
		req = req.WithContext(orgHttpapi.WithTenantContext(req.Context(), tenantCtx))
		w := httptest.NewRecorder()
		h.ListAuditLogs(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for invalid from timestamp, got %d", w.Code)
		}

		// Invalid to format
		req = httptest.NewRequest(http.MethodGet, "/api/v1/audit-logs?to=not-rfc3339", nil)
		req = req.WithContext(orgHttpapi.WithTenantContext(req.Context(), tenantCtx))
		w = httptest.NewRecorder()
		h.ListAuditLogs(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for invalid to timestamp, got %d", w.Code)
		}

		// from > to
		now := time.Now().UTC()
		fromStr := now.Add(time.Hour).Format(time.RFC3339)
		toStr := now.Format(time.RFC3339)
		req = httptest.NewRequest(http.MethodGet, "/api/v1/audit-logs?from="+fromStr+"&to="+toStr, nil)
		req = req.WithContext(orgHttpapi.WithTenantContext(req.Context(), tenantCtx))
		w = httptest.NewRecorder()
		h.ListAuditLogs(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for from > to, got %d", w.Code)
		}
	})

	t.Run("successful listing returns 200 with anti-caching headers and envelope", func(t *testing.T) {
		logID := uuid.New()
		userID := uuid.New()
		entID := uuid.New()
		ip := "192.168.1.1"
		ua := "test-browser"
		now := time.Now().UTC()
		nextCur := domain.EncodeAuditCursor(now, logID)

		mockReader := &mockAuditReaderForHandler{
			listAuditLogsFunc: func(ctx context.Context, role orgDomain.Role, oID uuid.UUID, filter domain.AuditLogFilter) (*domain.AuditLogListResult, error) {
				return &domain.AuditLogListResult{
					Logs: []*domain.AuditLog{
						{
							ID:             logID,
							OrganizationID: &oID,
							UserID:         &userID,
							Action:         domain.ActionBackupDownload,
							EntityType:     domain.EntityTypeBackupArtifact,
							EntityID:       &entID,
							IPAddress:      &ip,
							UserAgent:      &ua,
							Metadata:       []byte(`{"token":"DO_NOT_EXPOSE"}`),
							CreatedAt:      now,
						},
					},
					NextCursor: &nextCur,
					HasMore:    true,
				}, nil
			},
		}

		h := NewHandler(mockReader, nil)
		req := httptest.NewRequest(http.MethodGet, "/api/v1/audit-logs?limit=50", nil)
		req = req.WithContext(orgHttpapi.WithTenantContext(req.Context(), tenantCtx))
		w := httptest.NewRecorder()

		h.ListAuditLogs(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", w.Code)
		}

		// Anti-caching headers check
		if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
			t.Fatalf("expected Cache-Control: no-store, got %q", cc)
		}
		if pragma := w.Header().Get("Pragma"); pragma != "no-cache" {
			t.Fatalf("expected Pragma: no-cache, got %q", pragma)
		}

		var resp AuditLogListResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed unmarshaling response envelope: %v", err)
		}

		if len(resp.Data) != 1 {
			t.Fatalf("expected 1 item in data, got %d", len(resp.Data))
		}
		item := resp.Data[0]
		if item.ID != logID || item.Action != domain.ActionBackupDownload {
			t.Fatalf("unexpected data item: %+v", item)
		}
		if resp.Page.NextCursor == nil || *resp.Page.NextCursor != nextCur {
			t.Fatalf("expected next_cursor %q, got %v", nextCur, resp.Page.NextCursor)
		}
		if !resp.Page.HasMore {
			t.Fatalf("expected has_more = true")
		}
	})
}

func TestAuditLogDTO_SecurityAndPrivacy(t *testing.T) {
	logID := uuid.New()
	orgID := uuid.New()
	userID := uuid.New()
	entID := uuid.New()
	ip := "10.0.0.1"
	ua := "sec-agent"
	now := time.Now().UTC()

	// Domain entry with secret-laden metadata and organization_id
	entry := &domain.AuditLog{
		ID:             logID,
		OrganizationID: &orgID,
		UserID:         &userID,
		Action:         domain.ActionCredentialCreate,
		EntityType:     domain.EntityTypeCredential,
		EntityID:       &entID,
		IPAddress:      &ip,
		UserAgent:      &ua,
		Metadata: []byte(`{
			"password":"DO_NOT_EXPOSE_PASSWORD",
			"token":"DO_NOT_EXPOSE_TOKEN",
			"secret":"DO_NOT_EXPOSE_SECRET",
			"restic_password":"DO_NOT_EXPOSE_RESTIC",
			"s3_secret_key":"DO_NOT_EXPOSE_S3",
			"jwt":"DO_NOT_EXPOSE_JWT"
		}`),
		CreatedAt: now,
	}

	dto := ToAuditLogDTO(entry)

	// JSON serialization
	jsonBytes, err := json.Marshal(dto)
	if err != nil {
		t.Fatalf("failed marshaling dto: %v", err)
	}
	jsonStr := string(jsonBytes)

	// 1. Verify permitted fields are in JSON
	allowedFields := []string{"id", "user_id", "action", "entity_type", "entity_id", "ip_address", "user_agent", "created_at"}
	for _, f := range allowedFields {
		if !strings.Contains(jsonStr, `"`+f+`":`) {
			t.Errorf("missing allowed field %q in JSON: %s", f, jsonStr)
		}
	}

	// 2. Verify forbidden fields are strictly ABSENT
	forbiddenStrings := []string{
		"organization_id",
		orgID.String(),
		"metadata",
		"DO_NOT_EXPOSE_PASSWORD",
		"DO_NOT_EXPOSE_TOKEN",
		"DO_NOT_EXPOSE_SECRET",
		"DO_NOT_EXPOSE_RESTIC",
		"DO_NOT_EXPOSE_S3",
		"DO_NOT_EXPOSE_JWT",
	}
	for _, s := range forbiddenStrings {
		if strings.Contains(jsonStr, s) {
			t.Fatalf("SECURITY VIOLATION: sensitive string %q leaked in DTO JSON: %s", s, jsonStr)
		}
	}

	// 3. Verify nullable fields serialize as null when nil
	emptyEntry := &domain.AuditLog{
		ID:         logID,
		Action:     domain.ActionAuthLogout,
		EntityType: domain.EntityTypeUser,
		CreatedAt:  now,
	}
	emptyDTO := ToAuditLogDTO(emptyEntry)
	emptyJSONBytes, err := json.Marshal(emptyDTO)
	if err != nil {
		t.Fatalf("failed marshaling empty dto: %v", err)
	}
	emptyJSONStr := string(emptyJSONBytes)

	for _, field := range []string{"user_id", "entity_id", "ip_address", "user_agent"} {
		expectedNull := `"` + field + `":null`
		if !strings.Contains(emptyJSONStr, expectedNull) {
			t.Errorf("expected %q to be null in JSON, got: %s", field, emptyJSONStr)
		}
	}
}

func TestHandler_ListAuditLogs_ErrorMapping(t *testing.T) {
	orgID := uuid.New()
	tenantCtx := &orgHttpapi.TenantContext{
		OrganizationID: orgID,
		Role:           orgDomain.RoleAdmin,
	}

	tests := []struct {
		name           string
		serviceErr     error
		expectedStatus int
		expectedCode   string
	}{
		{
			name:           "ErrUnauthorizedRole -> 403 FORBIDDEN",
			serviceErr:     domain.ErrUnauthorizedRole,
			expectedStatus: http.StatusForbidden,
			expectedCode:   "FORBIDDEN",
		},
		{
			name:           "ErrInvalidAuditFilter -> 400 BAD_REQUEST",
			serviceErr:     domain.ErrInvalidAuditFilter,
			expectedStatus: http.StatusBadRequest,
			expectedCode:   "BAD_REQUEST",
		},
		{
			name:           "ErrAuditServiceUnavailable -> 503 SERVICE_UNAVAILABLE",
			serviceErr:     domain.ErrAuditServiceUnavailable,
			expectedStatus: http.StatusServiceUnavailable,
			expectedCode:   "SERVICE_UNAVAILABLE",
		},
		{
			name:           "unexpected error -> 500 INTERNAL_SERVER_ERROR",
			serviceErr:     errors.New("unexpected database driver fault"),
			expectedStatus: http.StatusInternalServerError,
			expectedCode:   "INTERNAL_SERVER_ERROR",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mockReader := &mockAuditReaderForHandler{
				listAuditLogsFunc: func(ctx context.Context, role orgDomain.Role, oID uuid.UUID, filter domain.AuditLogFilter) (*domain.AuditLogListResult, error) {
					return nil, tc.serviceErr
				},
			}

			h := NewHandler(mockReader, nil)
			req := httptest.NewRequest(http.MethodGet, "/api/v1/audit-logs", nil)
			req = req.WithContext(orgHttpapi.WithTenantContext(req.Context(), tenantCtx))
			w := httptest.NewRecorder()

			h.ListAuditLogs(w, req)
			if w.Code != tc.expectedStatus {
				t.Fatalf("expected status %d, got %d. Body: %s", tc.expectedStatus, w.Code, w.Body.String())
			}
			if !strings.Contains(w.Body.String(), tc.expectedCode) {
				t.Fatalf("expected error code %q in body, got: %s", tc.expectedCode, w.Body.String())
			}
			// Verify raw internal error message is NOT leaked
			if strings.Contains(w.Body.String(), "unexpected database driver fault") {
				t.Fatalf("SECURITY LEAK: internal error message leaked in response: %s", w.Body.String())
			}
		})
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

func TestAuditLogs_RouteChain_Semantics(t *testing.T) {
	orgID := uuid.New()
	userID := uuid.New()
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
				UserID:    userID,
				SessionID: uuid.New(),
				Email:     "user@test.local",
			})
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}

	mockMembers := make(map[string]*orgRepo.UserMembershipWithOrg)
	memberRepository := &routeTestMemberRepo{members: mockMembers}
	orgContextMiddleware := orgHttpapi.NewOrganizationContextMiddleware(memberRepository, &routeTestTxManager{}, nil)

	mockReader := &mockAuditReaderForHandler{
		listAuditLogsFunc: func(ctx context.Context, role orgDomain.Role, oID uuid.UUID, filter domain.AuditLogFilter) (*domain.AuditLogListResult, error) {
			return &domain.AuditLogListResult{Logs: []*domain.AuditLog{}}, nil
		},
	}
	h := NewHandler(mockReader, nil)

	// Canonical route pipeline matching main.go with NoStoreMiddleware
	routePipeline := NoStoreMiddleware(authMiddleware(orgContextMiddleware(orgHttpapi.RequirePermission(orgAuthz.PermissionAuditLogRead, nil)(http.HandlerFunc(h.ListAuditLogs)))))

	assertNoCache := func(t *testing.T, w *httptest.ResponseRecorder) {
		t.Helper()
		if got := w.Header().Get("Cache-Control"); got != "no-store" {
			t.Errorf("expected Cache-Control: no-store, got %q", got)
		}
		if got := w.Header().Get("Pragma"); got != "no-cache" {
			t.Errorf("expected Pragma: no-cache, got %q", got)
		}
	}

	t.Run("no Authorization / unauthenticated => 401", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/audit-logs", nil)
		w := httptest.NewRecorder()
		routePipeline.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 Unauthorized, got %d", w.Code)
		}
		assertNoCache(t, w)
	})

	t.Run("authenticated but missing X-Organization-ID => 400", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/audit-logs", nil)
		req.Header.Set("Authorization", "Bearer "+validToken)
		w := httptest.NewRecorder()
		routePipeline.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request, got %d", w.Code)
		}
		assertNoCache(t, w)
	})

	t.Run("authenticated but invalid X-Organization-ID format => 400", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/audit-logs", nil)
		req.Header.Set("Authorization", "Bearer "+validToken)
		req.Header.Set("X-Organization-ID", "not-a-valid-uuid")
		w := httptest.NewRecorder()
		routePipeline.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request, got %d", w.Code)
		}
		assertNoCache(t, w)
	})

	t.Run("authenticated but user not member of org => 404", func(t *testing.T) {
		foreignOrg := uuid.New()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/audit-logs", nil)
		req.Header.Set("Authorization", "Bearer "+validToken)
		req.Header.Set("X-Organization-ID", foreignOrg.String())
		w := httptest.NewRecorder()
		routePipeline.ServeHTTP(w, req)
		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404 Organization Not Found, got %d", w.Code)
		}
		assertNoCache(t, w)
	})

	t.Run("authenticated + valid tenant + Admin => 200", func(t *testing.T) {
		k := orgID.String() + ":" + userID.String()
		mockMembers[k] = &orgRepo.UserMembershipWithOrg{
			OrganizationID:   orgID,
			OrganizationName: "Test Org",
			Slug:             "test-org",
			Role:             orgDomain.RoleAdmin,
			Status:           orgDomain.MemberStatusActive,
		}
		defer delete(mockMembers, k)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/audit-logs", nil)
		req.Header.Set("Authorization", "Bearer "+validToken)
		req.Header.Set("X-Organization-ID", orgID.String())
		w := httptest.NewRecorder()
		routePipeline.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK for Admin, got %d: %s", w.Code, w.Body.String())
		}
		assertNoCache(t, w)
	})

	t.Run("authenticated + valid tenant + Member => 403", func(t *testing.T) {
		k := orgID.String() + ":" + userID.String()
		mockMembers[k] = &orgRepo.UserMembershipWithOrg{
			OrganizationID:   orgID,
			OrganizationName: "Test Org",
			Slug:             "test-org",
			Role:             orgDomain.RoleMember,
			Status:           orgDomain.MemberStatusActive,
		}
		defer delete(mockMembers, k)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/audit-logs", nil)
		req.Header.Set("Authorization", "Bearer "+validToken)
		req.Header.Set("X-Organization-ID", orgID.String())
		w := httptest.NewRecorder()
		routePipeline.ServeHTTP(w, req)
		if w.Code != http.StatusForbidden {
			t.Fatalf("expected 403 Forbidden for Member, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "INSUFFICIENT_PERMISSIONS") {
			t.Fatalf("expected INSUFFICIENT_PERMISSIONS error code, got %s", w.Body.String())
		}
		assertNoCache(t, w)
	})

	t.Run("authenticated + valid tenant + Viewer => 403", func(t *testing.T) {
		k := orgID.String() + ":" + userID.String()
		mockMembers[k] = &orgRepo.UserMembershipWithOrg{
			OrganizationID:   orgID,
			OrganizationName: "Test Org",
			Slug:             "test-org",
			Role:             orgDomain.RoleViewer,
			Status:           orgDomain.MemberStatusActive,
		}
		defer delete(mockMembers, k)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/audit-logs", nil)
		req.Header.Set("Authorization", "Bearer "+validToken)
		req.Header.Set("X-Organization-ID", orgID.String())
		w := httptest.NewRecorder()
		routePipeline.ServeHTTP(w, req)
		if w.Code != http.StatusForbidden {
			t.Fatalf("expected 403 Forbidden for Viewer, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "INSUFFICIENT_PERMISSIONS") {
			t.Fatalf("expected INSUFFICIENT_PERMISSIONS error code, got %s", w.Body.String())
		}
		assertNoCache(t, w)
	})

	t.Run("authenticated + valid tenant + Admin + service failure => 500", func(t *testing.T) {
		errReader := &mockAuditReaderForHandler{
			listAuditLogsFunc: func(ctx context.Context, role orgDomain.Role, oID uuid.UUID, filter domain.AuditLogFilter) (*domain.AuditLogListResult, error) {
				return nil, errors.New("database connection failed")
			},
		}
		errHandler := NewHandler(errReader, nil)
		errPipeline := NoStoreMiddleware(authMiddleware(orgContextMiddleware(orgHttpapi.RequirePermission(orgAuthz.PermissionAuditLogRead, nil)(http.HandlerFunc(errHandler.ListAuditLogs)))))

		k := orgID.String() + ":" + userID.String()
		mockMembers[k] = &orgRepo.UserMembershipWithOrg{
			OrganizationID:   orgID,
			OrganizationName: "Test Org",
			Slug:             "test-org",
			Role:             orgDomain.RoleAdmin,
			Status:           orgDomain.MemberStatusActive,
		}
		defer delete(mockMembers, k)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/audit-logs", nil)
		req.Header.Set("Authorization", "Bearer "+validToken)
		req.Header.Set("X-Organization-ID", orgID.String())
		w := httptest.NewRecorder()
		errPipeline.ServeHTTP(w, req)
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500 Internal Server Error, got %d: %s", w.Code, w.Body.String())
		}
		assertNoCache(t, w)
	})
}
