package httpapi

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"

	"backup-platform/internal/connector"
	orgHttpapi "backup-platform/internal/organization/httpapi"
	"backup-platform/internal/platform/httpapi"
	"backup-platform/internal/platform/logger"
	"backup-platform/internal/resource/domain"
	"backup-platform/internal/resource/service"
	"backup-platform/pkg/uuid"
)

// ResourceService specifies the application service interface for resource operations.
type ResourceService interface {
	CreateResource(ctx context.Context, orgID uuid.UUID, input service.CreateResourceInput) (*domain.ResourceWithConnector, error)
	GetResource(ctx context.Context, orgID, resID uuid.UUID) (*domain.ResourceWithConnector, error)
	ListResources(ctx context.Context, orgID uuid.UUID) ([]*domain.ResourceWithConnector, error)
	UpdateResource(ctx context.Context, orgID, resID uuid.UUID, input service.UpdateResourceInput) (*domain.ResourceWithConnector, error)
	ArchiveResource(ctx context.Context, orgID, resID uuid.UUID) error
}

// ConnectionTestExecutor specifies the operational connection testing capability.
type ConnectionTestExecutor interface {
	TestConnection(ctx context.Context, orgID, resID uuid.UUID) (*service.ConnectionTestResponseData, error)
}

// DatabaseDiscoveryExecutor specifies the operational MySQL database discovery capability.
type DatabaseDiscoveryExecutor interface {
	DiscoverDatabases(ctx context.Context, orgID, resID uuid.UUID) ([]connector.DatabaseInfo, error)
}

// Handler handles HTTP requests for resource and connector management endpoints.
type Handler struct {
	service          ResourceService
	testService      ConnectionTestExecutor
	discoveryService DatabaseDiscoveryExecutor
	logger           *slog.Logger
}

// NewHandler constructs a new Resource HTTP Handler.
func NewHandler(
	service ResourceService,
	testService ConnectionTestExecutor,
	discoveryService DatabaseDiscoveryExecutor,
	log *slog.Logger,
) *Handler {
	if log == nil {
		log = slog.Default()
	}
	return &Handler{
		service:          service,
		testService:      testService,
		discoveryService: discoveryService,
		logger:           log,
	}
}

// List handles GET /api/v1/resources - returns all active resources formatted according to caller's role.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")

	tenantCtx, ok := orgHttpapi.TenantContextFromRequest(r)
	if !ok || tenantCtx == nil || tenantCtx.OrganizationID == uuid.Nil {
		httpapi.WriteError(w, r, http.StatusForbidden, "FORBIDDEN", "access denied", nil)
		return
	}

	items, err := h.service.ListResources(r.Context(), tenantCtx.OrganizationID)
	if err != nil {
		reqLogger := logger.FromContext(r.Context(), h.logger)
		reqLogger.Error("failed to list resources")
		httpapi.WriteError(w, r, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "service temporarily unavailable", nil)
		return
	}

	respData := make([]ResourceResponse, len(items))
	for i, item := range items {
		respData[i] = MapResourceToResponse(item, tenantCtx.Role)
	}

	httpapi.WriteJSON(w, r, http.StatusOK, respData, "فهرست منابع با موفقیت دریافت شد.")
}

// GetByID handles GET /api/v1/resources/{id} - returns a single active resource formatted according to caller's role.
func (h *Handler) GetByID(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")

	tenantCtx, ok := orgHttpapi.TenantContextFromRequest(r)
	if !ok || tenantCtx == nil || tenantCtx.OrganizationID == uuid.Nil {
		httpapi.WriteError(w, r, http.StatusForbidden, "FORBIDDEN", "access denied", nil)
		return
	}

	idStr := r.PathValue("id")
	resID, err := uuid.Parse(idStr)
	if err != nil || resID == uuid.Nil {
		httpapi.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "invalid resource identifier", nil)
		return
	}

	item, err := h.service.GetResource(r.Context(), tenantCtx.OrganizationID, resID)
	if err != nil {
		if errors.Is(err, domain.ErrResourceNotFound) {
			httpapi.WriteError(w, r, http.StatusNotFound, "RESOURCE_NOT_FOUND", "resource not found", nil)
			return
		}
		reqLogger := logger.FromContext(r.Context(), h.logger)
		reqLogger.Error("failed to get resource")
		httpapi.WriteError(w, r, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "service temporarily unavailable", nil)
		return
	}

	respData := MapResourceToResponse(item, tenantCtx.Role)
	httpapi.WriteJSON(w, r, http.StatusOK, respData, "اطلاعات منبع با موفقیت دریافت شد.")
}

// Create handles POST /api/v1/resources - creates a resource and its connector atomically.
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")

	tenantCtx, ok := orgHttpapi.TenantContextFromRequest(r)
	if !ok || tenantCtx == nil || tenantCtx.OrganizationID == uuid.Nil {
		httpapi.WriteError(w, r, http.StatusForbidden, "FORBIDDEN", "access denied", nil)
		return
	}

	contentType := r.Header.Get("Content-Type")
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil || mediaType != "application/json" {
		httpapi.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "Content-Type must be application/json", nil)
		return
	}

	var req CreateResourceRequest
	if err := httpapi.DecodeJSON(w, r, &req); err != nil {
		httpapi.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "invalid or malformed JSON payload", nil)
		return
	}

	if req.Name == nil || req.Type == nil || req.Connector == nil {
		httpapi.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "name, type, and connector are required", nil)
		return
	}

	credUUID, err := uuid.Parse(req.Connector.CredentialID)
	if err != nil || credUUID == uuid.Nil {
		httpapi.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "invalid credential identifier", nil)
		return
	}

	input := service.CreateResourceInput{
		Name: *req.Name,
		Type: domain.Type(*req.Type),
		Connector: service.CreateConnectorInput{
			Host:               req.Connector.Host,
			Port:               req.Connector.Port,
			AuthType:           domain.AuthType(req.Connector.AuthType),
			Username:           req.Connector.Username,
			CredentialID:       credUUID,
			HostKeyFingerprint: req.Connector.HostKeyFingerprint,
		},
	}

	if req.Connector.Config != nil {
		input.Connector.ConnectionTimeout = req.Connector.Config.ConnectionTimeoutSeconds
		input.Connector.UseHTTPS = req.Connector.Config.UseHTTPS
	}

	created, err := h.service.CreateResource(r.Context(), tenantCtx.OrganizationID, input)
	if err != nil {
		if errors.Is(err, domain.ErrInvalidResourceName) ||
			errors.Is(err, domain.ErrInvalidResourceType) ||
			errors.Is(err, domain.ErrInvalidConnectorHost) ||
			errors.Is(err, domain.ErrInvalidConnectorPort) ||
			errors.Is(err, domain.ErrInvalidConnectorUsername) ||
			errors.Is(err, domain.ErrInvalidAuthType) ||
			errors.Is(err, domain.ErrInvalidHostKeyFingerprint) ||
			errors.Is(err, domain.ErrInvalidConnectionTimeout) ||
			errors.Is(err, domain.ErrInvalidConnectorConfig) ||
			errors.Is(err, domain.ErrInvalidCredentialReference) {
			httpapi.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "validation failed", nil)
			return
		}

		if errors.Is(err, domain.ErrResourceConflict) {
			httpapi.WriteError(w, r, http.StatusConflict, "CONFLICT", "resource configuration conflict", nil)
			return
		}

		if errors.Is(err, domain.ErrResourceServiceUnavailable) {
			reqLogger := logger.FromContext(r.Context(), h.logger)
			reqLogger.Error("resource creation service unavailable")
			httpapi.WriteError(w, r, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "service temporarily unavailable", nil)
			return
		}

		reqLogger := logger.FromContext(r.Context(), h.logger)
		reqLogger.Error("unexpected resource creation error")
		httpapi.WriteError(w, r, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "internal server error", nil)
		return
	}

	resp := ResourceCreateResponse{
		ID:        created.Resource.ID.String(),
		Name:      created.Resource.Name,
		Type:      string(created.Resource.Type),
		Status:    string(created.Resource.Status),
		CreatedAt: created.Resource.CreatedAt,
	}

	httpapi.WriteJSON(w, r, http.StatusCreated, resp, "منبع با موفقیت ثبت گردید.")
}

// Update handles PUT /api/v1/resources/{id} - replaces editable resource fields and connector configuration.
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")

	tenantCtx, ok := orgHttpapi.TenantContextFromRequest(r)
	if !ok || tenantCtx == nil || tenantCtx.OrganizationID == uuid.Nil {
		httpapi.WriteError(w, r, http.StatusForbidden, "FORBIDDEN", "access denied", nil)
		return
	}

	idStr := r.PathValue("id")
	resID, err := uuid.Parse(idStr)
	if err != nil || resID == uuid.Nil {
		httpapi.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "invalid resource identifier", nil)
		return
	}

	contentType := r.Header.Get("Content-Type")
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil || mediaType != "application/json" {
		httpapi.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "Content-Type must be application/json", nil)
		return
	}

	var req UpdateResourceRequest
	if err := httpapi.DecodeJSON(w, r, &req); err != nil {
		httpapi.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "invalid or malformed JSON payload", nil)
		return
	}

	if req.Name == nil || req.Connector == nil {
		httpapi.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "name and connector are required for resource update", nil)
		return
	}

	credUUID, err := uuid.Parse(req.Connector.CredentialID)
	if err != nil || credUUID == uuid.Nil {
		httpapi.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "invalid credential identifier", nil)
		return
	}

	input := service.UpdateResourceInput{
		Name: *req.Name,
		Connector: service.CreateConnectorInput{
			Host:               req.Connector.Host,
			Port:               req.Connector.Port,
			AuthType:           domain.AuthType(req.Connector.AuthType),
			Username:           req.Connector.Username,
			CredentialID:       credUUID,
			HostKeyFingerprint: req.Connector.HostKeyFingerprint,
		},
	}

	if req.Connector.Config != nil {
		input.Connector.ConnectionTimeout = req.Connector.Config.ConnectionTimeoutSeconds
		input.Connector.UseHTTPS = req.Connector.Config.UseHTTPS
	}

	updated, err := h.service.UpdateResource(r.Context(), tenantCtx.OrganizationID, resID, input)
	if err != nil {
		if errors.Is(err, domain.ErrResourceNotFound) {
			httpapi.WriteError(w, r, http.StatusNotFound, "RESOURCE_NOT_FOUND", "resource not found", nil)
			return
		}

		if errors.Is(err, domain.ErrInvalidResourceName) ||
			errors.Is(err, domain.ErrInvalidConnectorHost) ||
			errors.Is(err, domain.ErrInvalidConnectorPort) ||
			errors.Is(err, domain.ErrInvalidConnectorUsername) ||
			errors.Is(err, domain.ErrInvalidAuthType) ||
			errors.Is(err, domain.ErrInvalidHostKeyFingerprint) ||
			errors.Is(err, domain.ErrInvalidConnectionTimeout) ||
			errors.Is(err, domain.ErrInvalidConnectorConfig) ||
			errors.Is(err, domain.ErrInvalidCredentialReference) {
			httpapi.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "validation failed", nil)
			return
		}

		if errors.Is(err, domain.ErrResourceServiceUnavailable) {
			reqLogger := logger.FromContext(r.Context(), h.logger)
			reqLogger.Error("resource update service unavailable")
			httpapi.WriteError(w, r, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "service temporarily unavailable", nil)
			return
		}

		reqLogger := logger.FromContext(r.Context(), h.logger)
		reqLogger.Error("unexpected resource update error")
		httpapi.WriteError(w, r, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "internal server error", nil)
		return
	}

	resp := ResourceUpdateResponse{
		ID:        updated.Resource.ID.String(),
		Name:      updated.Resource.Name,
		Type:      string(updated.Resource.Type),
		Status:    string(updated.Resource.Status),
		CreatedAt: updated.Resource.CreatedAt,
		UpdatedAt: updated.Resource.UpdatedAt,
	}

	httpapi.WriteJSON(w, r, http.StatusOK, resp, "منبع با موفقیت به‌روزرسانی شد.")
}

// Delete handles DELETE /api/v1/resources/{id} - soft-archives the resource.
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")

	tenantCtx, ok := orgHttpapi.TenantContextFromRequest(r)
	if !ok || tenantCtx == nil || tenantCtx.OrganizationID == uuid.Nil {
		httpapi.WriteError(w, r, http.StatusForbidden, "FORBIDDEN", "access denied", nil)
		return
	}

	idStr := r.PathValue("id")
	resID, err := uuid.Parse(idStr)
	if err != nil || resID == uuid.Nil {
		httpapi.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "invalid resource identifier", nil)
		return
	}

	err = h.service.ArchiveResource(r.Context(), tenantCtx.OrganizationID, resID)
	if err != nil {
		if errors.Is(err, domain.ErrResourceNotFound) {
			httpapi.WriteError(w, r, http.StatusNotFound, "RESOURCE_NOT_FOUND", "resource not found", nil)
			return
		}

		if errors.Is(err, domain.ErrResourceServiceUnavailable) {
			reqLogger := logger.FromContext(r.Context(), h.logger)
			reqLogger.Error("resource deletion failed")
			httpapi.WriteError(w, r, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "service temporarily unavailable", nil)
			return
		}

		reqLogger := logger.FromContext(r.Context(), h.logger)
		reqLogger.Error("unexpected resource deletion error")
		httpapi.WriteError(w, r, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "internal server error", nil)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// TestConnection handles POST /api/v1/resources/{id}/test-connection - executes live operational connection probe.
func (h *Handler) TestConnection(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")

	tenantCtx, ok := orgHttpapi.TenantContextFromRequest(r)
	if !ok || tenantCtx == nil || tenantCtx.OrganizationID == uuid.Nil {
		httpapi.WriteError(w, r, http.StatusForbidden, "FORBIDDEN", "access denied", nil)
		return
	}

	idStr := r.PathValue("id")
	resID, err := uuid.Parse(idStr)
	if err != nil || resID == uuid.Nil {
		httpapi.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "invalid resource identifier", nil)
		return
	}

	// Request body must be empty. Reject any non-empty body using bounded MaxBytesReader.
	if r.Body != nil {
		limitedReader := http.MaxBytesReader(w, r.Body, httpapi.MaxRequestBodyBytes)
		bodyBytes, err := io.ReadAll(limitedReader)
		defer clear(bodyBytes)
		if err != nil {
			httpapi.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "test connection endpoint does not accept a request body", nil)
			return
		}
		if len(bytes.TrimSpace(bodyBytes)) > 0 {
			httpapi.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "test connection endpoint does not accept a request body", nil)
			return
		}
	}

	if h.testService == nil {
		httpapi.WriteError(w, r, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "connection test service not available", nil)
		return
	}

	result, err := h.testService.TestConnection(r.Context(), tenantCtx.OrganizationID, resID)
	if err != nil {
		if errors.Is(err, domain.ErrResourceNotFound) {
			httpapi.WriteError(w, r, http.StatusNotFound, "RESOURCE_NOT_FOUND", "resource not found", nil)
			return
		}
		if errors.Is(err, domain.ErrInvalidHostKeyFingerprint) ||
			errors.Is(err, domain.ErrInvalidConnectorConfig) ||
			errors.Is(err, domain.ErrInvalidAuthType) {
			httpapi.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "resource configuration is invalid for connection testing", nil)
			return
		}

		reqLogger := logger.FromContext(r.Context(), h.logger)
		reqLogger.Error("connection test failed internally")
		httpapi.WriteError(w, r, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "service temporarily unavailable", nil)
		return
	}

	msg := "connection test succeeded"
	if result.Status != "success" {
		msg = "connection test failed"
	}

	resp := ConnectionTestResponse{
		Status:    result.Status,
		LatencyMS: result.LatencyMS,
		CheckedAt: result.CheckedAt,
		Details:   result.Details,
	}

	httpapi.WriteJSON(w, r, http.StatusOK, resp, msg)
}

// DiscoverDatabases handles GET /api/v1/resources/{id}/databases - discovers MySQL databases on target resource.
func (h *Handler) DiscoverDatabases(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")

	tenantCtx, ok := orgHttpapi.TenantContextFromRequest(r)
	if !ok || tenantCtx == nil || tenantCtx.OrganizationID == uuid.Nil {
		httpapi.WriteError(w, r, http.StatusForbidden, "FORBIDDEN", "access denied", nil)
		return
	}

	idStr := r.PathValue("id")
	resID, err := uuid.Parse(idStr)
	if err != nil || resID == uuid.Nil {
		httpapi.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "invalid resource identifier", nil)
		return
	}

	if h.discoveryService == nil {
		reqLogger := logger.FromContext(r.Context(), h.logger)
		reqLogger.Error("database discovery service not configured")
		httpapi.WriteError(w, r, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "service temporarily unavailable", nil)
		return
	}

	dbs, err := h.discoveryService.DiscoverDatabases(r.Context(), tenantCtx.OrganizationID, resID)
	if err != nil {
		if errors.Is(err, domain.ErrResourceNotFound) {
			httpapi.WriteError(w, r, http.StatusNotFound, "RESOURCE_NOT_FOUND", "resource not found or archived", nil)
			return
		}
		if errors.Is(err, domain.ErrInvalidHostKeyFingerprint) ||
			errors.Is(err, domain.ErrInvalidConnectorConfig) ||
			errors.Is(err, domain.ErrInvalidAuthType) ||
			errors.Is(err, domain.ErrInvalidResourceType) {
			httpapi.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "resource configuration is invalid for database discovery", nil)
			return
		}
		reqLogger := logger.FromContext(r.Context(), h.logger)
		reqLogger.Error("database discovery failed")
		httpapi.WriteError(w, r, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "service temporarily unavailable", nil)
		return
	}

	respData := make([]DiscoveredDatabaseResponse, len(dbs))
	for i, db := range dbs {
		respData[i] = DiscoveredDatabaseResponse{
			Name:        db.Name,
			SizeBytes:   db.SizeBytes,
			TablesCount: db.TablesCount,
			Status:      string(db.Status),
		}
	}

	httpapi.WriteJSON(w, r, http.StatusOK, respData, "فهرست پایگاه‌های داده با موفقیت شناسایی شد.")
}
