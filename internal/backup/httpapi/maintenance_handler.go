package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"backup-platform/internal/backup/domain"
	orgHttpapi "backup-platform/internal/organization/httpapi"
	"backup-platform/internal/platform/httpapi"
	"backup-platform/internal/platform/logger"
	"backup-platform/pkg/uuid"
)

// ListMaintenanceJobs handles GET /api/v1/maintenance-jobs.
func (h *Handler) ListMaintenanceJobs(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.FromContext(r.Context(), h.logger)

	// Set required anti-caching response headers
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")

	// 1. Resolve Tenant Context
	tenantCtx, ok := orgHttpapi.TenantContextFromRequest(r)
	if !ok || tenantCtx == nil || tenantCtx.OrganizationID == uuid.Nil {
		httpapi.WriteError(w, r, http.StatusForbidden, "FORBIDDEN", "tenant context required", nil)
		return
	}

	if h.maintenanceService == nil {
		httpapi.WriteError(w, r, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "maintenance service is not available", nil)
		return
	}

	// 2. Validate Query Parameters strictly (reject unknown parameters)
	q := r.URL.Query()
	for key := range q {
		if key != "limit" && key != "cursor" && key != "repository_id" && key != "status" && key != "operation_type" {
			httpapi.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", fmt.Sprintf("unknown query parameter: %s", key), nil)
			return
		}
	}

	limit := 50
	if limitStr := q.Get("limit"); limitStr != "" {
		parsedLimit, err := strconv.Atoi(limitStr)
		if err != nil || parsedLimit < 1 || parsedLimit > 100 {
			httpapi.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "limit must be an integer between 1 and 100", nil)
			return
		}
		limit = parsedLimit
	}

	var filter domain.MaintenanceJobFilter
	filter.Limit = limit

	if repoIDStr := q.Get("repository_id"); repoIDStr != "" {
		repoID, err := uuid.Parse(repoIDStr)
		if err != nil || repoID == uuid.Nil {
			httpapi.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "invalid repository_id query parameter format", nil)
			return
		}
		filter.RepositoryID = &repoID
	}

	if statusStr := q.Get("status"); statusStr != "" {
		switch domain.MaintenanceJobStatus(statusStr) {
		case domain.MaintenanceJobPending, domain.MaintenanceJobRunning, domain.MaintenanceJobCompleted, domain.MaintenanceJobFailed, domain.MaintenanceJobCancelled:
			st := domain.MaintenanceJobStatus(statusStr)
			filter.Status = &st
		default:
			httpapi.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "invalid status query parameter", nil)
			return
		}
	}

	if opTypeStr := q.Get("operation_type"); opTypeStr != "" {
		switch domain.MaintenanceOperationType(opTypeStr) {
		case domain.MaintenanceOpResticForget, domain.MaintenanceOpResticPrune, domain.MaintenanceOpResticDeepCheck:
			op := domain.MaintenanceOperationType(opTypeStr)
			filter.OperationType = &op
		default:
			httpapi.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "invalid operation_type query parameter", nil)
			return
		}
	}

	if cursorStr := q.Get("cursor"); cursorStr != "" {
		curTime, curID, err := domain.DecodeMaintenanceCursor(cursorStr)
		if err != nil {
			httpapi.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "invalid cursor format", nil)
			return
		}
		filter.CursorCreatedAt = &curTime
		filter.CursorID = &curID
	}

	// 3. Execute Service Query
	result, err := h.maintenanceService.ListMaintenanceJobs(r.Context(), tenantCtx.Role, tenantCtx.OrganizationID, filter)
	if err != nil {
		if errors.Is(err, domain.ErrUnauthorizedRole) {
			httpapi.WriteError(w, r, http.StatusForbidden, "FORBIDDEN", "unauthorized role", nil)
			return
		}
		if errors.Is(err, domain.ErrBackupServiceUnavailable) {
			httpapi.WriteError(w, r, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "service temporarily unavailable", nil)
			return
		}
		reqLogger.Error("failed listing maintenance jobs", "error", err.Error())
		httpapi.WriteError(w, r, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "internal server error", nil)
		return
	}

	// 4. Transform to DTO
	data := make([]MaintenanceJobDTO, len(result.Jobs))
	for i, job := range result.Jobs {
		data[i] = ToMaintenanceJobDTO(job)
	}

	page := PaginationMeta{
		NextCursor: result.NextCursor,
		HasMore:    result.HasMore,
	}

	writePaginatedJSON(w, r, http.StatusOK, data, page, "maintenance jobs retrieved successfully")
}

func writePaginatedJSON(w http.ResponseWriter, r *http.Request, status int, data any, page PaginationMeta, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)

	reqID := logger.RequestIDFromContext(r.Context())
	envelope := struct {
		Data      any            `json:"data"`
		Page      PaginationMeta `json:"page"`
		Message   string         `json:"message,omitempty"`
		RequestID string         `json:"request_id,omitempty"`
	}{
		Data:      data,
		Page:      page,
		Message:   message,
		RequestID: reqID,
	}

	_ = json.NewEncoder(w).Encode(envelope)
}

// GetMaintenanceJob handles GET /api/v1/maintenance-jobs/{id}.
func (h *Handler) GetMaintenanceJob(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.FromContext(r.Context(), h.logger)

	// Set required anti-caching response headers
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")

	// 1. Resolve Tenant Context
	tenantCtx, ok := orgHttpapi.TenantContextFromRequest(r)
	if !ok || tenantCtx == nil || tenantCtx.OrganizationID == uuid.Nil {
		httpapi.WriteError(w, r, http.StatusForbidden, "FORBIDDEN", "tenant context required", nil)
		return
	}

	if h.maintenanceService == nil {
		httpapi.WriteError(w, r, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "maintenance service is not available", nil)
		return
	}

	// 2. Parse and Validate Job ID
	jobIDStr := r.PathValue("id")
	jobID, err := uuid.Parse(jobIDStr)
	if err != nil || jobID == uuid.Nil {
		httpapi.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "invalid maintenance job ID format", nil)
		return
	}

	// 3. Query Service
	detail, err := h.maintenanceService.GetMaintenanceJobDetail(r.Context(), tenantCtx.Role, tenantCtx.OrganizationID, jobID)
	if err != nil {
		if errors.Is(err, domain.ErrUnauthorizedRole) {
			httpapi.WriteError(w, r, http.StatusForbidden, "FORBIDDEN", "unauthorized role", nil)
			return
		}
		if errors.Is(err, domain.ErrJobNotFound) {
			httpapi.WriteError(w, r, http.StatusNotFound, "NOT_FOUND", "maintenance job not found", nil)
			return
		}
		if errors.Is(err, domain.ErrBackupServiceUnavailable) {
			httpapi.WriteError(w, r, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "service temporarily unavailable", nil)
			return
		}
		reqLogger.Error("failed retrieving maintenance job detail", "error", err.Error())
		httpapi.WriteError(w, r, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "internal server error", nil)
		return
	}

	// 4. Transform to DTO
	runs := make([]MaintenanceRunSummaryDTO, len(detail.Runs))
	for i, run := range detail.Runs {
		runs[i] = ToMaintenanceRunSummaryDTO(run)
	}

	resp := MaintenanceJobDetailDTO{
		MaintenanceJobDTO: ToMaintenanceJobDTO(detail.Job),
		Runs:              runs,
	}

	httpapi.WriteJSON(w, r, http.StatusOK, resp, "maintenance job retrieved successfully")
}
