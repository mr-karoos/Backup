package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"backup-platform/internal/backup/domain"
	"backup-platform/internal/backup/service"
	orgDomain "backup-platform/internal/organization/domain"
	orgHttpapi "backup-platform/internal/organization/httpapi"
	"backup-platform/internal/platform/httpapi"
	"backup-platform/internal/platform/logger"
	"backup-platform/pkg/uuid"
)

// BackupJobCreator specifies the manual backup job creation interface.
type BackupJobCreator interface {
	CreateManualJob(
		ctx context.Context,
		userRole orgDomain.Role,
		orgID, userID uuid.UUID,
		input service.CreateManualJobInput,
	) (*domain.BackupJob, error)
}

// BackupPlanManager specifies the backup plan management interface.
type BackupPlanManager interface {
	CreatePlan(ctx context.Context, role orgDomain.Role, orgID uuid.UUID, input service.CreatePlanInput) (*domain.BackupPlan, error)
	GetPlan(ctx context.Context, role orgDomain.Role, orgID, planID uuid.UUID) (*domain.BackupPlanWithResource, error)
	ListPlans(ctx context.Context, role orgDomain.Role, orgID uuid.UUID, filter domain.PlanFilter) ([]*domain.BackupPlanWithResource, error)
	UpdatePlan(ctx context.Context, role orgDomain.Role, orgID, planID uuid.UUID, input service.UpdatePlanInput) (*domain.BackupPlan, error)
	ArchivePlan(ctx context.Context, role orgDomain.Role, orgID, planID uuid.UUID) error
}

// BackupHistoryManager specifies the backup run history querying interface.
type BackupHistoryManager interface {
	ListRuns(ctx context.Context, role orgDomain.Role, orgID uuid.UUID, filter domain.RunFilter) ([]*domain.BackupRunWithStats, error)
	GetRun(ctx context.Context, role orgDomain.Role, orgID, runID uuid.UUID) (*domain.BackupRunWithStats, error)
}

// BackupArtifactManager specifies the backup artifact management and download interface.
type BackupArtifactManager interface {
	ListArtifacts(ctx context.Context, role orgDomain.Role, orgID uuid.UUID) ([]*domain.BackupArtifact, error)
	GetArtifact(ctx context.Context, role orgDomain.Role, orgID, artifactID uuid.UUID) (*domain.BackupArtifact, error)
	OpenArtifactDownload(ctx context.Context, role orgDomain.Role, orgID, artifactID uuid.UUID) (*domain.BackupArtifact, io.ReadCloser, error)
	RecordDownloadAudit(ctx context.Context, orgID, userID, artifactID uuid.UUID, sizeBytes int64, clientIP, userAgent string) error
	DeleteArtifact(ctx context.Context, role orgDomain.Role, orgID, userID, artifactID uuid.UUID, clientIP, userAgent string) error
}

// BackupVerifier specifies the backup run verification interface.
type BackupVerifier interface {
	VerifyRun(ctx context.Context, role orgDomain.Role, orgID, runID uuid.UUID) (*service.RunVerificationResult, error)
}

// StorageTargetManager specifies the storage target management interface.
type StorageTargetManager interface {
	CreateStorageTarget(ctx context.Context, role orgDomain.Role, orgID uuid.UUID, input service.CreateStorageTargetInput) (*domain.StorageTarget, error)
	GetStorageTarget(ctx context.Context, role orgDomain.Role, orgID, targetID uuid.UUID) (*domain.StorageTarget, error)
	ListStorageTargets(ctx context.Context, role orgDomain.Role, orgID uuid.UUID) ([]*domain.StorageTarget, error)
	UpdateStorageTarget(ctx context.Context, role orgDomain.Role, orgID, targetID uuid.UUID, input service.UpdateStorageTargetInput) (*domain.StorageTarget, error)
	DeleteStorageTarget(ctx context.Context, role orgDomain.Role, orgID, targetID uuid.UUID) error
}

// Handler handles HTTP endpoints for backup operations, plans, history, artifacts, verification, and storage targets.
type Handler struct {
	jobService           BackupJobCreator
	planService          BackupPlanManager
	historyService       BackupHistoryManager
	artifactService      BackupArtifactManager
	verifyService        BackupVerifier
	storageTargetService StorageTargetManager
	logger               *slog.Logger
}

// NewHandler constructs a new Backup HTTP Handler.
func NewHandler(
	jobService BackupJobCreator,
	planService BackupPlanManager,
	historyService BackupHistoryManager,
	artifactService BackupArtifactManager,
	verifyService BackupVerifier,
	log *slog.Logger,
) *Handler {
	if log == nil {
		log = slog.Default()
	}
	return &Handler{
		jobService:      jobService,
		planService:     planService,
		historyService:  historyService,
		artifactService: artifactService,
		verifyService:   verifyService,
		logger:          log,
	}
}

// SetStorageTargetService injects the storage target service into the handler.
func (h *Handler) SetStorageTargetService(svc StorageTargetManager) {
	h.storageTargetService = svc
}

// CreateBackupJob handles POST /api/v1/backup-jobs.
func (h *Handler) CreateBackupJob(w http.ResponseWriter, r *http.Request) {
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

	// 2. Validate Content-Type: application/json
	ct := r.Header.Get("Content-Type")
	mediaType, _, err := mime.ParseMediaType(ct)
	if err != nil || mediaType != "application/json" {
		httpapi.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "Content-Type must be application/json", nil)
		return
	}

	// 3. Decode Request Body using shared platform decoder
	var req CreateBackupJobRequest
	if err := httpapi.DecodeJSON(w, r, &req); err != nil {
		switch {
		case errors.Is(err, httpapi.ErrBodyTooLarge):
			httpapi.WriteError(w, r, http.StatusRequestEntityTooLarge, "BODY_TOO_LARGE", "request body exceeds maximum allowed size of 64 KiB", nil)
		case errors.Is(err, httpapi.ErrEmptyBody):
			httpapi.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "request body cannot be empty", nil)
		default:
			httpapi.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "malformed JSON request payload", nil)
		}
		return
	}

	input := service.CreateManualJobInput{
		BackupPlanID:    req.BackupPlanID,
		ResourceID:      req.ResourceID,
		BackupType:      req.BackupType,
		EngineType:      req.EngineType,
		StorageTargetID: req.StorageTargetID,
		TargetSpec:      req.TargetSpec,
	}

	job, err := h.jobService.CreateManualJob(r.Context(), tenantCtx.Role, tenantCtx.OrganizationID, tenantCtx.UserID, input)
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrUnauthorizedRole):
			httpapi.WriteError(w, r, http.StatusForbidden, "FORBIDDEN", "unauthorized role for backup operation", nil)
		case errors.Is(err, domain.ErrManualBackupConflict):
			httpapi.WriteError(w, r, http.StatusConflict, "CONFLICT", "a manual backup is already pending or running for this resource", nil)
		case errors.Is(err, domain.ErrPlanNotFound):
			httpapi.WriteError(w, r, http.StatusNotFound, "PLAN_NOT_FOUND", "backup plan not found", nil)
		case errors.Is(err, domain.ErrResourceNotFound):
			httpapi.WriteError(w, r, http.StatusNotFound, "RESOURCE_NOT_FOUND", "resource not found", nil)
		case errors.Is(err, domain.ErrStorageTargetNotFound):
			httpapi.WriteError(w, r, http.StatusNotFound, "STORAGE_TARGET_NOT_FOUND", "storage target not found", nil)
		case errors.Is(err, domain.ErrPlanOverrideForbidden):
			httpapi.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "engine_type or storage_target_id override forbidden for plan-backed job", nil)
		case errors.Is(err, domain.ErrPlanNotActive):
			httpapi.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "backup plan is not active", nil)
		case errors.Is(err, domain.ErrResourceDisabled), errors.Is(err, domain.ErrResourceNotActive):
			httpapi.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "resource is not active", nil)
		case errors.Is(err, domain.ErrUnsupportedResourceType):
			httpapi.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "unsupported resource type for backup execution", nil)
		case errors.Is(err, domain.ErrUnsupportedBackupType):
			httpapi.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "unsupported backup type", nil)
		case errors.Is(err, domain.ErrUnsupportedEngineType):
			httpapi.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "unsupported engine type", nil)
		case errors.Is(err, domain.ErrStorageTargetNotActive):
			httpapi.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "storage target is not active", nil)
		case errors.Is(err, domain.ErrIncompatibleEngineStorage):
			httpapi.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "incompatible engine and storage target", nil)
		case errors.Is(err, domain.ErrInvalidTargetSpec):
			httpapi.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "invalid backup target specification", nil)
		case errors.Is(err, domain.ErrBackupServiceUnavailable):
			httpapi.WriteError(w, r, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "backup service is temporarily unavailable", nil)
		default:
			reqLogger.Error("failed creating manual backup job")
			httpapi.WriteError(w, r, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "internal server error", nil)
		}
		return
	}

	httpapi.WriteJSON(w, r, http.StatusAccepted, toBackupJobResponse(job), "backup job enqueued successfully")
}

// CreateBackupPlan handles POST /api/v1/backup-plans.
func (h *Handler) CreateBackupPlan(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.FromContext(r.Context(), h.logger)

	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")

	tenantCtx, ok := orgHttpapi.TenantContextFromRequest(r)
	if !ok || tenantCtx == nil || tenantCtx.OrganizationID == uuid.Nil {
		httpapi.WriteError(w, r, http.StatusForbidden, "FORBIDDEN", "tenant context required", nil)
		return
	}

	ct := r.Header.Get("Content-Type")
	mediaType, _, err := mime.ParseMediaType(ct)
	if err != nil || mediaType != "application/json" {
		httpapi.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "Content-Type must be application/json", nil)
		return
	}

	var req CreateBackupPlanRequest
	if err := httpapi.DecodeJSON(w, r, &req); err != nil {
		switch {
		case errors.Is(err, httpapi.ErrBodyTooLarge):
			httpapi.WriteError(w, r, http.StatusRequestEntityTooLarge, "BODY_TOO_LARGE", "request body exceeds maximum allowed size of 64 KiB", nil)
		case errors.Is(err, httpapi.ErrEmptyBody):
			httpapi.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "request body cannot be empty", nil)
		default:
			httpapi.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "malformed JSON request payload", nil)
		}
		return
	}

	var dbSel *service.DatabaseSelectionInput
	if req.DatabaseSelection != nil {
		dbSel = &service.DatabaseSelectionInput{
			Mode:      req.DatabaseSelection.Mode,
			Databases: req.DatabaseSelection.Databases,
		}
	}

	var fileSel *service.FileSelectionInput
	if req.FileSelection != nil {
		fileSel = &service.FileSelectionInput{
			Paths:           req.FileSelection.Paths,
			ExcludePatterns: req.FileSelection.ExcludePatterns,
		}
	}

	var retPolicy *service.RetentionPolicyInput
	if req.RetentionPolicy != nil {
		retPolicy = &service.RetentionPolicyInput{
			KeepLastN: req.RetentionPolicy.KeepLastN,
			KeepDays:  req.RetentionPolicy.KeepDays,
		}
	}

	input := service.CreatePlanInput{
		Name:              req.Name,
		ResourceID:        req.ResourceID,
		BackupType:        req.BackupType,
		EngineType:        req.EngineType,
		StorageTargetID:   req.StorageTargetID,
		DatabaseSelection: dbSel,
		FileSelection:     fileSel,
		Schedule: service.ScheduleInput{
			IsEnabled:      req.Schedule.IsEnabled,
			CronExpression: req.Schedule.CronExpression,
			Timezone:       req.Schedule.Timezone,
		},
		RetentionPolicy: retPolicy,
	}

	plan, err := h.planService.CreatePlan(r.Context(), tenantCtx.Role, tenantCtx.OrganizationID, input)
	if err != nil {
		h.handlePlanError(w, r, reqLogger, err, "failed creating backup plan")
		return
	}

	httpapi.WriteJSON(w, r, http.StatusCreated, toCreateBackupPlanResponse(plan), "backup plan created successfully")
}

// GetBackupPlan handles GET /api/v1/backup-plans/{id}.
func (h *Handler) GetBackupPlan(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.FromContext(r.Context(), h.logger)

	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")

	tenantCtx, ok := orgHttpapi.TenantContextFromRequest(r)
	if !ok || tenantCtx == nil || tenantCtx.OrganizationID == uuid.Nil {
		httpapi.WriteError(w, r, http.StatusForbidden, "FORBIDDEN", "tenant context required", nil)
		return
	}

	idStr := r.PathValue("id")
	planID, err := uuid.Parse(idStr)
	if err != nil || planID == uuid.Nil {
		httpapi.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "invalid backup plan ID format", nil)
		return
	}

	planWithRes, err := h.planService.GetPlan(r.Context(), tenantCtx.Role, tenantCtx.OrganizationID, planID)
	if err != nil {
		h.handlePlanError(w, r, reqLogger, err, "failed retrieving backup plan")
		return
	}

	httpapi.WriteJSON(w, r, http.StatusOK, toBackupPlanResponse(&planWithRes.Plan, planWithRes.ResourceName, true), "backup plan retrieved successfully")
}

// ListBackupPlans handles GET /api/v1/backup-plans.
func (h *Handler) ListBackupPlans(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.FromContext(r.Context(), h.logger)

	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")

	tenantCtx, ok := orgHttpapi.TenantContextFromRequest(r)
	if !ok || tenantCtx == nil || tenantCtx.OrganizationID == uuid.Nil {
		httpapi.WriteError(w, r, http.StatusForbidden, "FORBIDDEN", "tenant context required", nil)
		return
	}

	// Validate query parameters
	q := r.URL.Query()
	for key := range q {
		if key != "resource_id" && key != "status" {
			httpapi.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "unknown query parameter: "+key, nil)
			return
		}
	}

	var filter domain.PlanFilter
	if resIDStr := q.Get("resource_id"); resIDStr != "" {
		resID, err := uuid.Parse(resIDStr)
		if err != nil || resID == uuid.Nil {
			httpapi.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "invalid resource_id query parameter format", nil)
			return
		}
		filter.ResourceID = &resID
	}

	if statusStr := q.Get("status"); statusStr != "" {
		switch domain.PlanStatus(statusStr) {
		case domain.PlanStatusActive, domain.PlanStatusPaused, domain.PlanStatusArchived:
			st := domain.PlanStatus(statusStr)
			filter.Status = &st
		default:
			httpapi.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "invalid status query parameter", nil)
			return
		}
	}

	plans, err := h.planService.ListPlans(r.Context(), tenantCtx.Role, tenantCtx.OrganizationID, filter)
	if err != nil {
		h.handlePlanError(w, r, reqLogger, err, "failed listing backup plans")
		return
	}

	dtos := make([]*BackupPlanResponse, 0, len(plans))
	for _, p := range plans {
		dtos = append(dtos, toBackupPlanResponse(&p.Plan, p.ResourceName, false))
	}

	httpapi.WriteJSON(w, r, http.StatusOK, dtos, "backup plans retrieved successfully")
}

// UpdateBackupPlan handles PUT /api/v1/backup-plans/{id}.
func (h *Handler) UpdateBackupPlan(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.FromContext(r.Context(), h.logger)

	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")

	tenantCtx, ok := orgHttpapi.TenantContextFromRequest(r)
	if !ok || tenantCtx == nil || tenantCtx.OrganizationID == uuid.Nil {
		httpapi.WriteError(w, r, http.StatusForbidden, "FORBIDDEN", "tenant context required", nil)
		return
	}

	idStr := r.PathValue("id")
	planID, err := uuid.Parse(idStr)
	if err != nil || planID == uuid.Nil {
		httpapi.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "invalid backup plan ID format", nil)
		return
	}

	ct := r.Header.Get("Content-Type")
	mediaType, _, err := mime.ParseMediaType(ct)
	if err != nil || mediaType != "application/json" {
		httpapi.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "Content-Type must be application/json", nil)
		return
	}

	var req UpdateBackupPlanRequest
	if err := httpapi.DecodeJSON(w, r, &req); err != nil {
		switch {
		case errors.Is(err, httpapi.ErrBodyTooLarge):
			httpapi.WriteError(w, r, http.StatusRequestEntityTooLarge, "BODY_TOO_LARGE", "request body exceeds maximum allowed size of 64 KiB", nil)
		case errors.Is(err, httpapi.ErrEmptyBody):
			httpapi.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "request body cannot be empty", nil)
		default:
			httpapi.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "malformed JSON request payload", nil)
		}
		return
	}

	var dbSel *service.DatabaseSelectionInput
	if req.DatabaseSelection != nil {
		dbSel = &service.DatabaseSelectionInput{
			Mode:      req.DatabaseSelection.Mode,
			Databases: req.DatabaseSelection.Databases,
		}
	}

	var fileSel *service.FileSelectionInput
	if req.FileSelection != nil {
		fileSel = &service.FileSelectionInput{
			Paths:           req.FileSelection.Paths,
			ExcludePatterns: req.FileSelection.ExcludePatterns,
		}
	}

	var retPolicy *service.RetentionPolicyInput
	if req.RetentionPolicy != nil {
		retPolicy = &service.RetentionPolicyInput{
			KeepLastN: req.RetentionPolicy.KeepLastN,
			KeepDays:  req.RetentionPolicy.KeepDays,
		}
	}

	input := service.UpdatePlanInput{
		Name:              req.Name,
		EngineType:        req.EngineType,
		StorageTargetID:   req.StorageTargetID,
		DatabaseSelection: dbSel,
		FileSelection:     fileSel,
		Schedule: service.ScheduleInput{
			IsEnabled:      req.Schedule.IsEnabled,
			CronExpression: req.Schedule.CronExpression,
			Timezone:       req.Schedule.Timezone,
		},
		RetentionPolicy: retPolicy,
		Status:          req.Status,
	}

	plan, err := h.planService.UpdatePlan(r.Context(), tenantCtx.Role, tenantCtx.OrganizationID, planID, input)
	if err != nil {
		h.handlePlanError(w, r, reqLogger, err, "failed updating backup plan")
		return
	}

	// Fetch joined resource name for full response
	planWithRes, getErr := h.planService.GetPlan(r.Context(), tenantCtx.Role, tenantCtx.OrganizationID, plan.ID)
	resName := ""
	if getErr == nil && planWithRes != nil {
		resName = planWithRes.ResourceName
	}

	httpapi.WriteJSON(w, r, http.StatusOK, toBackupPlanResponse(plan, resName, true), "backup plan updated successfully")
}

// ArchiveBackupPlan handles DELETE /api/v1/backup-plans/{id}.
func (h *Handler) ArchiveBackupPlan(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.FromContext(r.Context(), h.logger)

	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")

	tenantCtx, ok := orgHttpapi.TenantContextFromRequest(r)
	if !ok || tenantCtx == nil || tenantCtx.OrganizationID == uuid.Nil {
		httpapi.WriteError(w, r, http.StatusForbidden, "FORBIDDEN", "tenant context required", nil)
		return
	}

	idStr := r.PathValue("id")
	planID, err := uuid.Parse(idStr)
	if err != nil || planID == uuid.Nil {
		httpapi.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "invalid backup plan ID format", nil)
		return
	}

	if err := h.planService.ArchivePlan(r.Context(), tenantCtx.Role, tenantCtx.OrganizationID, planID); err != nil {
		h.handlePlanError(w, r, reqLogger, err, "failed archiving backup plan")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handlePlanError(w http.ResponseWriter, r *http.Request, reqLogger *slog.Logger, err error, logMsg string) {
	switch {
	case errors.Is(err, domain.ErrUnauthorizedRole):
		httpapi.WriteError(w, r, http.StatusForbidden, "FORBIDDEN", "unauthorized role for backup plan operation", nil)
	case errors.Is(err, domain.ErrPlanNotFound):
		httpapi.WriteError(w, r, http.StatusNotFound, "PLAN_NOT_FOUND", "backup plan not found", nil)
	case errors.Is(err, domain.ErrResourceNotFound):
		httpapi.WriteError(w, r, http.StatusNotFound, "RESOURCE_NOT_FOUND", "resource not found", nil)
	case errors.Is(err, domain.ErrPlanAlreadyArchived):
		httpapi.WriteError(w, r, http.StatusConflict, "CONFLICT", "backup plan is already archived and cannot be updated", nil)
	case errors.Is(err, domain.ErrResourceDisabled), errors.Is(err, domain.ErrResourceNotActive):
		httpapi.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "resource is not active", nil)
	case errors.Is(err, domain.ErrUnsupportedResourceType):
		httpapi.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "unsupported resource type for backup execution", nil)
	case errors.Is(err, domain.ErrUnsupportedBackupType):
		httpapi.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "unsupported backup type", nil)
	case errors.Is(err, domain.ErrStorageTargetNotFound):
		httpapi.WriteError(w, r, http.StatusNotFound, "STORAGE_TARGET_NOT_FOUND", "storage target not found", nil)
	case errors.Is(err, domain.ErrUnsupportedEngineType):
		httpapi.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "unsupported engine type", nil)
	case errors.Is(err, domain.ErrStorageTargetNotActive):
		httpapi.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "storage target is not active", nil)
	case errors.Is(err, domain.ErrIncompatibleEngineStorage):
		httpapi.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "incompatible engine and storage target", nil)
	case errors.Is(err, domain.ErrInvalidPlanName):
		httpapi.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "invalid backup plan name", nil)
	case errors.Is(err, domain.ErrInvalidCronExpression):
		httpapi.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "invalid 5-field cron expression", nil)
	case errors.Is(err, domain.ErrInvalidTimezone):
		httpapi.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "invalid IANA timezone", nil)
	case errors.Is(err, domain.ErrInvalidRetentionPolicy):
		httpapi.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "invalid retention policy", nil)
	case errors.Is(err, domain.ErrInvalidTargetSpec):
		httpapi.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "invalid backup target specification", nil)
	case errors.Is(err, domain.ErrBackupServiceUnavailable):
		httpapi.WriteError(w, r, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "backup service is temporarily unavailable", nil)
	default:
		reqLogger.Error(logMsg)
		httpapi.WriteError(w, r, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "internal server error", nil)
	}
}

// ListBackupRuns handles GET /api/v1/backup-runs.
func (h *Handler) ListBackupRuns(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.FromContext(r.Context(), h.logger)

	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")

	tenantCtx, ok := orgHttpapi.TenantContextFromRequest(r)
	if !ok || tenantCtx == nil || tenantCtx.OrganizationID == uuid.Nil {
		httpapi.WriteError(w, r, http.StatusForbidden, "FORBIDDEN", "tenant context required", nil)
		return
	}

	// Validate query parameters
	q := r.URL.Query()
	for key := range q {
		if key != "resource_id" && key != "job_id" && key != "status" && key != "from_date" && key != "to_date" {
			httpapi.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "unknown query parameter: "+key, nil)
			return
		}
	}

	var filter domain.RunFilter
	if resIDStr := q.Get("resource_id"); resIDStr != "" {
		resID, err := uuid.Parse(resIDStr)
		if err != nil || resID == uuid.Nil {
			httpapi.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "invalid resource_id query parameter format", nil)
			return
		}
		filter.ResourceID = &resID
	}

	if jobIDStr := q.Get("job_id"); jobIDStr != "" {
		jobID, err := uuid.Parse(jobIDStr)
		if err != nil || jobID == uuid.Nil {
			httpapi.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "invalid job_id query parameter format", nil)
			return
		}
		filter.JobID = &jobID
	}

	if statusStr := q.Get("status"); statusStr != "" {
		switch domain.RunStatus(statusStr) {
		case domain.RunStatusPending, domain.RunStatusRunning, domain.RunStatusSuccess, domain.RunStatusFailed, domain.RunStatusCancelled:
			st := domain.RunStatus(statusStr)
			filter.Status = &st
		default:
			httpapi.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "invalid status query parameter", nil)
			return
		}
	}

	if fromStr := q.Get("from_date"); fromStr != "" {
		t, err := time.Parse(time.RFC3339, fromStr)
		if err != nil {
			httpapi.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "invalid from_date query parameter format", nil)
			return
		}
		filter.FromDate = &t
	}

	if toStr := q.Get("to_date"); toStr != "" {
		t, err := time.Parse(time.RFC3339, toStr)
		if err != nil {
			httpapi.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "invalid to_date query parameter format", nil)
			return
		}
		filter.ToDate = &t
	}

	runs, err := h.historyService.ListRuns(r.Context(), tenantCtx.Role, tenantCtx.OrganizationID, filter)
	if err != nil {
		reqLogger.Error("failed listing backup runs")
		httpapi.WriteError(w, r, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "service temporarily unavailable", nil)
		return
	}

	dtos := make([]*BackupRunResponse, 0, len(runs))
	for _, run := range runs {
		dtos = append(dtos, toBackupRunResponse(run))
	}

	httpapi.WriteJSON(w, r, http.StatusOK, dtos, "backup runs retrieved successfully")
}

// GetBackupRun handles GET /api/v1/backup-runs/{id}.
func (h *Handler) GetBackupRun(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.FromContext(r.Context(), h.logger)

	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")

	tenantCtx, ok := orgHttpapi.TenantContextFromRequest(r)
	if !ok || tenantCtx == nil || tenantCtx.OrganizationID == uuid.Nil {
		httpapi.WriteError(w, r, http.StatusForbidden, "FORBIDDEN", "tenant context required", nil)
		return
	}

	idStr := r.PathValue("id")
	runID, err := uuid.Parse(idStr)
	if err != nil || runID == uuid.Nil {
		httpapi.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "invalid backup run ID format", nil)
		return
	}

	run, err := h.historyService.GetRun(r.Context(), tenantCtx.Role, tenantCtx.OrganizationID, runID)
	if err != nil {
		if errors.Is(err, domain.ErrRunNotFound) {
			httpapi.WriteError(w, r, http.StatusNotFound, "RUN_NOT_FOUND", "backup run not found", nil)
			return
		}
		reqLogger.Error("failed getting backup run")
		httpapi.WriteError(w, r, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "internal server error", nil)
		return
	}

	httpapi.WriteJSON(w, r, http.StatusOK, toBackupRunResponse(run), "backup run retrieved successfully")
}

// ListBackupArtifacts handles GET /api/v1/backup-artifacts.
func (h *Handler) ListBackupArtifacts(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.FromContext(r.Context(), h.logger)

	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")

	tenantCtx, ok := orgHttpapi.TenantContextFromRequest(r)
	if !ok || tenantCtx == nil || tenantCtx.OrganizationID == uuid.Nil {
		httpapi.WriteError(w, r, http.StatusForbidden, "FORBIDDEN", "tenant context required", nil)
		return
	}

	// Reject unexpected query parameters
	q := r.URL.Query()
	for key := range q {
		httpapi.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "unknown query parameter: "+key, nil)
		return
	}

	artifacts, err := h.artifactService.ListArtifacts(r.Context(), tenantCtx.Role, tenantCtx.OrganizationID)
	if err != nil {
		reqLogger.Error("failed listing backup artifacts")
		httpapi.WriteError(w, r, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "service temporarily unavailable", nil)
		return
	}

	dtos := make([]*BackupArtifactResponse, 0, len(artifacts))
	for _, art := range artifacts {
		dtos = append(dtos, toBackupArtifactResponse(art))
	}

	httpapi.WriteJSON(w, r, http.StatusOK, dtos, "backup artifacts retrieved successfully")
}

// GetBackupArtifact handles GET /api/v1/backup-artifacts/{id}.
func (h *Handler) GetBackupArtifact(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.FromContext(r.Context(), h.logger)

	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")

	tenantCtx, ok := orgHttpapi.TenantContextFromRequest(r)
	if !ok || tenantCtx == nil || tenantCtx.OrganizationID == uuid.Nil {
		httpapi.WriteError(w, r, http.StatusForbidden, "FORBIDDEN", "tenant context required", nil)
		return
	}

	idStr := r.PathValue("id")
	artifactID, err := uuid.Parse(idStr)
	if err != nil || artifactID == uuid.Nil {
		httpapi.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "invalid backup artifact ID format", nil)
		return
	}

	artifact, err := h.artifactService.GetArtifact(r.Context(), tenantCtx.Role, tenantCtx.OrganizationID, artifactID)
	if err != nil {
		if errors.Is(err, domain.ErrArtifactNotFound) {
			httpapi.WriteError(w, r, http.StatusNotFound, "ARTIFACT_NOT_FOUND", "backup artifact not found", nil)
			return
		}
		reqLogger.Error("failed getting backup artifact")
		httpapi.WriteError(w, r, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "internal server error", nil)
		return
	}

	httpapi.WriteJSON(w, r, http.StatusOK, toBackupArtifactResponse(artifact), "backup artifact retrieved successfully")
}

// DownloadBackupArtifact handles GET /api/v1/backup-artifacts/{id}/download.
func (h *Handler) DownloadBackupArtifact(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.FromContext(r.Context(), h.logger)

	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")

	tenantCtx, ok := orgHttpapi.TenantContextFromRequest(r)
	if !ok || tenantCtx == nil || tenantCtx.OrganizationID == uuid.Nil {
		httpapi.WriteError(w, r, http.StatusForbidden, "FORBIDDEN", "tenant context required", nil)
		return
	}

	idStr := r.PathValue("id")
	artifactID, err := uuid.Parse(idStr)
	if err != nil || artifactID == uuid.Nil {
		httpapi.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "invalid backup artifact ID format", nil)
		return
	}

	artifact, reader, err := h.artifactService.OpenArtifactDownload(r.Context(), tenantCtx.Role, tenantCtx.OrganizationID, artifactID)
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrUnauthorizedRole):
			httpapi.WriteError(w, r, http.StatusForbidden, "FORBIDDEN", "insufficient permissions to download backup artifact", nil)
		case errors.Is(err, domain.ErrArtifactNotFound):
			httpapi.WriteError(w, r, http.StatusNotFound, "ARTIFACT_NOT_FOUND", "backup artifact not found", nil)
		default:
			reqLogger.Error("failed opening artifact for download")
			httpapi.WriteError(w, r, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "service temporarily unavailable", nil)
		}
		return
	}
	defer reader.Close()

	safeName := SafeArtifactFilename(artifact.TargetName, artifact.Format, artifact.ID)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, safeName))
	w.Header().Set("Content-Type", "application/gzip")
	if artifact.SizeBytes > 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(artifact.SizeBytes, 10))
	}
	w.WriteHeader(http.StatusOK)

	written, copyErr := io.Copy(w, reader)
	if copyErr != nil || (artifact.SizeBytes > 0 && written != artifact.SizeBytes) {
		reqLogger.Error("interrupted while streaming artifact download")
		return
	}

	clientIP := extractClientIP(r)
	userAgent := sanitizeUserAgent(r.UserAgent())
	if auditErr := h.artifactService.RecordDownloadAudit(r.Context(), tenantCtx.OrganizationID, tenantCtx.UserID, artifact.ID, artifact.SizeBytes, clientIP, userAgent); auditErr != nil {
		reqLogger.Error("failed recording audit log for artifact download",
			slog.String("org_id", tenantCtx.OrganizationID.String()),
			slog.String("artifact_id", artifact.ID.String()),
		)
	}
}

// DeleteBackupArtifact handles DELETE /api/v1/backup-artifacts/{id}.
func (h *Handler) DeleteBackupArtifact(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.FromContext(r.Context(), h.logger)

	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")

	tenantCtx, ok := orgHttpapi.TenantContextFromRequest(r)
	if !ok || tenantCtx == nil || tenantCtx.OrganizationID == uuid.Nil {
		httpapi.WriteError(w, r, http.StatusForbidden, "FORBIDDEN", "tenant context required", nil)
		return
	}

	idStr := r.PathValue("id")
	artifactID, err := uuid.Parse(idStr)
	if err != nil || artifactID == uuid.Nil {
		httpapi.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "invalid backup artifact ID format", nil)
		return
	}

	clientIP := extractClientIP(r)
	userAgent := sanitizeUserAgent(r.UserAgent())

	if err := h.artifactService.DeleteArtifact(r.Context(), tenantCtx.Role, tenantCtx.OrganizationID, tenantCtx.UserID, artifactID, clientIP, userAgent); err != nil {
		switch {
		case errors.Is(err, domain.ErrUnauthorizedRole):
			httpapi.WriteError(w, r, http.StatusForbidden, "FORBIDDEN", "unauthorized role for artifact deletion", nil)
		case errors.Is(err, domain.ErrArtifactNotFound):
			httpapi.WriteError(w, r, http.StatusNotFound, "ARTIFACT_NOT_FOUND", "backup artifact not found", nil)
		case errors.Is(err, domain.ErrArtifactDeleteFailed):
			reqLogger.Error("failed deleting backup artifact")
			httpapi.WriteError(w, r, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "internal server error", nil)
		default:
			reqLogger.Error("service error deleting backup artifact")
			httpapi.WriteError(w, r, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "service temporarily unavailable", nil)
		}
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func extractClientIP(r *http.Request) string {
	remoteAddr := strings.TrimSpace(r.RemoteAddr)
	if host, _, err := net.SplitHostPort(remoteAddr); err == nil {
		return host
	}
	return remoteAddr
}

func sanitizeUserAgent(ua string) string {
	if !utf8.ValidString(ua) {
		ua = strings.ToValidUTF8(ua, "")
	}
	runes := []rune(ua)
	if len(runes) > 255 {
		return string(runes[:255])
	}
	return ua
}

// VerifyBackupRun handles POST /api/v1/backup-runs/{id}/verify.
func (h *Handler) VerifyBackupRun(w http.ResponseWriter, r *http.Request) {
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

	// 2. Validate run ID from URL path (UUID format)
	idStr := r.PathValue("id")
	runID, err := uuid.Parse(idStr)
	if err != nil || runID == uuid.Nil {
		httpapi.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "invalid backup run ID format", nil)
		return
	}

	// 3. Verify that Verification Service is initialized
	if h.verifyService == nil {
		reqLogger.Error("verification service is not initialized")
		httpapi.WriteError(w, r, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "backup verification service is temporarily unavailable", nil)
		return
	}

	// 4. Execute Verification in Service Layer
	result, err := h.verifyService.VerifyRun(r.Context(), tenantCtx.Role, tenantCtx.OrganizationID, runID)
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrUnauthorizedRole):
			httpapi.WriteError(w, r, http.StatusForbidden, "FORBIDDEN", "unauthorized role for backup verification", nil)
		case errors.Is(err, domain.ErrRunNotFound):
			httpapi.WriteError(w, r, http.StatusNotFound, "NOT_FOUND", "backup run not found", nil)
		case errors.Is(err, domain.ErrNoVerifiableArtifacts):
			httpapi.WriteError(w, r, http.StatusUnprocessableEntity, "NO_ARTIFACTS", "no verifiable backup artifacts found for run", nil)
		case errors.Is(err, domain.ErrBackupServiceUnavailable):
			httpapi.WriteError(w, r, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "backup verification service is temporarily unavailable", nil)
		default:
			reqLogger.Error("failed verifying backup run", slog.String("run_id", runID.String()))
			httpapi.WriteError(w, r, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "internal server error", nil)
		}
		return
	}

	// 5. Build DTO and write 200 OK response with status-dependent message
	resp := toVerifyBackupRunResponse(result)
	msg := "صحت و یکپارچگی ساختاری فایل پشتیبان تأیید گردید."
	if result.VerificationStatus == domain.VerificationStatusFailed {
		msg = "اعتبارسنجی انجام شد اما یکپارچگی یک یا چند آرتیفکت تأیید نشد."
	}
	httpapi.WriteJSON(w, r, http.StatusOK, resp, msg)
}

// CreateStorageTarget handles POST /api/v1/storage-targets.
func (h *Handler) CreateStorageTarget(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.FromContext(r.Context(), h.logger)

	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")

	tenantCtx, ok := orgHttpapi.TenantContextFromRequest(r)
	if !ok || tenantCtx == nil || tenantCtx.OrganizationID == uuid.Nil {
		httpapi.WriteError(w, r, http.StatusForbidden, "FORBIDDEN", "tenant context required", nil)
		return
	}

	ct := r.Header.Get("Content-Type")
	mediaType, _, err := mime.ParseMediaType(ct)
	if err != nil || mediaType != "application/json" {
		httpapi.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "Content-Type must be application/json", nil)
		return
	}

	var req CreateStorageTargetRequest
	if err := httpapi.DecodeJSON(w, r, &req); err != nil {
		switch {
		case errors.Is(err, httpapi.ErrBodyTooLarge):
			httpapi.WriteError(w, r, http.StatusRequestEntityTooLarge, "BODY_TOO_LARGE", "request body exceeds maximum allowed size of 64 KiB", nil)
		case errors.Is(err, httpapi.ErrEmptyBody):
			httpapi.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "request body cannot be empty", nil)
		default:
			httpapi.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "malformed JSON request payload", nil)
		}
		return
	}

	if h.storageTargetService == nil {
		reqLogger.Error("storage target service is not initialized")
		httpapi.WriteError(w, r, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "storage target service is temporarily unavailable", nil)
		return
	}

	var configJSON json.RawMessage
	if req.Type == domain.StorageTargetTypeS3 {
		if req.S3Config == nil {
			httpapi.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "s3_config is required for s3 storage target", nil)
			return
		}
		raw, err := json.Marshal(req.S3Config)
		if err != nil {
			httpapi.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "malformed s3_config", nil)
			return
		}
		configJSON = raw
	}

	input := service.CreateStorageTargetInput{
		Name:         req.Name,
		Type:         req.Type,
		Config:       configJSON,
		CredentialID: req.CredentialID,
	}

	target, err := h.storageTargetService.CreateStorageTarget(r.Context(), tenantCtx.Role, tenantCtx.OrganizationID, input)
	if err != nil {
		h.handleStorageTargetError(w, r, reqLogger, err, "failed creating storage target")
		return
	}

	httpapi.WriteJSON(w, r, http.StatusCreated, toStorageTargetResponse(target), "storage target created successfully")
}

// GetStorageTarget handles GET /api/v1/storage-targets/{id}.
func (h *Handler) GetStorageTarget(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.FromContext(r.Context(), h.logger)

	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")

	tenantCtx, ok := orgHttpapi.TenantContextFromRequest(r)
	if !ok || tenantCtx == nil || tenantCtx.OrganizationID == uuid.Nil {
		httpapi.WriteError(w, r, http.StatusForbidden, "FORBIDDEN", "tenant context required", nil)
		return
	}

	idStr := r.PathValue("id")
	targetID, err := uuid.Parse(idStr)
	if err != nil || targetID == uuid.Nil {
		httpapi.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "invalid storage target ID format", nil)
		return
	}

	if h.storageTargetService == nil {
		reqLogger.Error("storage target service is not initialized")
		httpapi.WriteError(w, r, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "storage target service is temporarily unavailable", nil)
		return
	}

	target, err := h.storageTargetService.GetStorageTarget(r.Context(), tenantCtx.Role, tenantCtx.OrganizationID, targetID)
	if err != nil {
		h.handleStorageTargetError(w, r, reqLogger, err, "failed retrieving storage target")
		return
	}

	httpapi.WriteJSON(w, r, http.StatusOK, toStorageTargetResponse(target), "storage target retrieved successfully")
}

// ListStorageTargets handles GET /api/v1/storage-targets.
func (h *Handler) ListStorageTargets(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.FromContext(r.Context(), h.logger)

	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")

	tenantCtx, ok := orgHttpapi.TenantContextFromRequest(r)
	if !ok || tenantCtx == nil || tenantCtx.OrganizationID == uuid.Nil {
		httpapi.WriteError(w, r, http.StatusForbidden, "FORBIDDEN", "tenant context required", nil)
		return
	}

	if h.storageTargetService == nil {
		reqLogger.Error("storage target service is not initialized")
		httpapi.WriteError(w, r, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "storage target service is temporarily unavailable", nil)
		return
	}

	targets, err := h.storageTargetService.ListStorageTargets(r.Context(), tenantCtx.Role, tenantCtx.OrganizationID)
	if err != nil {
		h.handleStorageTargetError(w, r, reqLogger, err, "failed listing storage targets")
		return
	}

	resp := make([]*StorageTargetResponse, len(targets))
	for i, t := range targets {
		resp[i] = toStorageTargetResponse(t)
	}

	httpapi.WriteJSON(w, r, http.StatusOK, resp, "storage targets retrieved successfully")
}

// UpdateStorageTarget handles PUT /api/v1/storage-targets/{id}.
func (h *Handler) UpdateStorageTarget(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.FromContext(r.Context(), h.logger)

	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")

	tenantCtx, ok := orgHttpapi.TenantContextFromRequest(r)
	if !ok || tenantCtx == nil || tenantCtx.OrganizationID == uuid.Nil {
		httpapi.WriteError(w, r, http.StatusForbidden, "FORBIDDEN", "tenant context required", nil)
		return
	}

	idStr := r.PathValue("id")
	targetID, err := uuid.Parse(idStr)
	if err != nil || targetID == uuid.Nil {
		httpapi.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "invalid storage target ID format", nil)
		return
	}

	ct := r.Header.Get("Content-Type")
	mediaType, _, err := mime.ParseMediaType(ct)
	if err != nil || mediaType != "application/json" {
		httpapi.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "Content-Type must be application/json", nil)
		return
	}

	var req UpdateStorageTargetRequest
	if err := httpapi.DecodeJSON(w, r, &req); err != nil {
		switch {
		case errors.Is(err, httpapi.ErrBodyTooLarge):
			httpapi.WriteError(w, r, http.StatusRequestEntityTooLarge, "BODY_TOO_LARGE", "request body exceeds maximum allowed size of 64 KiB", nil)
		case errors.Is(err, httpapi.ErrEmptyBody):
			httpapi.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "request body cannot be empty", nil)
		default:
			httpapi.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "malformed JSON request payload", nil)
		}
		return
	}

	if h.storageTargetService == nil {
		reqLogger.Error("storage target service is not initialized")
		httpapi.WriteError(w, r, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "storage target service is temporarily unavailable", nil)
		return
	}

	var configJSON json.RawMessage
	if req.S3Config != nil {
		raw, err := json.Marshal(req.S3Config)
		if err != nil {
			httpapi.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "malformed s3_config", nil)
			return
		}
		configJSON = raw
	}

	name := ""
	if req.Name != nil {
		name = *req.Name
	}
	var status domain.StorageTargetStatus
	if req.Status != nil {
		status = *req.Status
	}

	input := service.UpdateStorageTargetInput{
		Name:         name,
		Config:       configJSON,
		CredentialID: req.CredentialID,
		Status:       status,
	}

	target, err := h.storageTargetService.UpdateStorageTarget(r.Context(), tenantCtx.Role, tenantCtx.OrganizationID, targetID, input)
	if err != nil {
		h.handleStorageTargetError(w, r, reqLogger, err, "failed updating storage target")
		return
	}

	httpapi.WriteJSON(w, r, http.StatusOK, toStorageTargetResponse(target), "storage target updated successfully")
}

// DeleteStorageTarget handles DELETE /api/v1/storage-targets/{id}.
func (h *Handler) DeleteStorageTarget(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.FromContext(r.Context(), h.logger)

	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")

	tenantCtx, ok := orgHttpapi.TenantContextFromRequest(r)
	if !ok || tenantCtx == nil || tenantCtx.OrganizationID == uuid.Nil {
		httpapi.WriteError(w, r, http.StatusForbidden, "FORBIDDEN", "tenant context required", nil)
		return
	}

	idStr := r.PathValue("id")
	targetID, err := uuid.Parse(idStr)
	if err != nil || targetID == uuid.Nil {
		httpapi.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "invalid storage target ID format", nil)
		return
	}

	if h.storageTargetService == nil {
		reqLogger.Error("storage target service is not initialized")
		httpapi.WriteError(w, r, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "storage target service is temporarily unavailable", nil)
		return
	}

	if err := h.storageTargetService.DeleteStorageTarget(r.Context(), tenantCtx.Role, tenantCtx.OrganizationID, targetID); err != nil {
		h.handleStorageTargetError(w, r, reqLogger, err, "failed deleting storage target")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleStorageTargetError(w http.ResponseWriter, r *http.Request, reqLogger *slog.Logger, err error, logMsg string) {
	switch {
	case errors.Is(err, domain.ErrUnauthorizedRole):
		httpapi.WriteError(w, r, http.StatusForbidden, "FORBIDDEN", "unauthorized role for storage target operation", nil)
	case errors.Is(err, domain.ErrStorageTargetNotFound):
		httpapi.WriteError(w, r, http.StatusNotFound, "STORAGE_TARGET_NOT_FOUND", "storage target not found", nil)
	case errors.Is(err, domain.ErrStorageTargetLocationImmutable):
		httpapi.WriteError(w, r, http.StatusConflict, "CONFLICT", "storage target location cannot be modified after artifacts are created", nil)
	case errors.Is(err, domain.ErrCannotDeleteDefaultStorageTarget):
		httpapi.WriteError(w, r, http.StatusConflict, "CONFLICT", "cannot delete or disable default storage target", nil)
	case errors.Is(err, domain.ErrStorageTargetInUse):
		httpapi.WriteError(w, r, http.StatusConflict, "CONFLICT", "storage target is in use and cannot be deleted", nil)
	case errors.Is(err, domain.ErrInvalidStorageTargetName):
		httpapi.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "invalid storage target name", nil)
	case errors.Is(err, domain.ErrStorageTargetNotSupported):
		httpapi.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "unsupported storage target type", nil)
	case errors.Is(err, domain.ErrStorageTargetCredentialRequired):
		httpapi.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "credential is required for s3 storage target", nil)
	case errors.Is(err, domain.ErrInvalidStorageTargetConfig):
		httpapi.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", err.Error(), nil)
	case errors.Is(err, domain.ErrBackupServiceUnavailable):
		httpapi.WriteError(w, r, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "storage service is temporarily unavailable", nil)
	default:
		errMsg := err.Error()
		if strings.Contains(errMsg, "does not exist in organization") {
			httpapi.WriteError(w, r, http.StatusNotFound, "CREDENTIAL_NOT_FOUND", "referenced credential does not exist in organization", nil)
			return
		}
		if strings.Contains(errMsg, "must be of type s3_credentials") || strings.Contains(errMsg, "invalid storage target status") || strings.Contains(errMsg, "manual creation of local storage targets") {
			httpapi.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", errMsg, nil)
			return
		}
		reqLogger.Error(logMsg, slog.String("error", err.Error()))
		httpapi.WriteError(w, r, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "internal server error", nil)
	}
}
