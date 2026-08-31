package httpapi

import (
	"encoding/json"
	"errors"
	"log/slog"
	"mime"
	"net/http"
	"time"

	identityHttpapi "backup-platform/internal/identity/httpapi"
	"backup-platform/internal/organization/domain"
	orgService "backup-platform/internal/organization/service"
	"backup-platform/internal/platform/httpapi"
	"backup-platform/internal/platform/logger"
	"backup-platform/pkg/uuid"
)

// FieldValidationError describes a field-specific validation failure.
type FieldValidationError struct {
	Field  string `json:"field"`
	Reason string `json:"reason"`
}

// OrganizationListItemResponse represents a safe tenant organization entry in the GET /organizations response.
type OrganizationListItemResponse struct {
	ID                uuid.UUID `json:"id"`
	Name              string    `json:"name"`
	Slug              string    `json:"slug"`
	IsDefaultInternal bool      `json:"is_default_internal"`
	Status            string    `json:"status"`
	UserRole          string    `json:"user_role"`
	CreatedAt         time.Time `json:"created_at"`
}

// OrganizationDetailResponse represents the safe response payload for GET /organizations/{id} and PUT /organizations/{id}.
type OrganizationDetailResponse struct {
	ID                uuid.UUID       `json:"id"`
	Name              string          `json:"name"`
	Slug              string          `json:"slug"`
	IsDefaultInternal bool            `json:"is_default_internal"`
	Status            string          `json:"status"`
	Metadata          json.RawMessage `json:"metadata"`
	CreatedAt         time.Time       `json:"created_at"`
	UpdatedAt         time.Time       `json:"updated_at"`
}

// CreateOrganizationRequest represents the incoming JSON request payload for POST /organizations.
type CreateOrganizationRequest struct {
	Name     string          `json:"name"`
	Slug     string          `json:"slug"`
	Metadata json.RawMessage `json:"metadata,omitempty"`
}

// CreateOrganizationResponse represents the safe response payload for POST /organizations.
type CreateOrganizationResponse struct {
	ID                uuid.UUID `json:"id"`
	Name              string    `json:"name"`
	Slug              string    `json:"slug"`
	IsDefaultInternal bool      `json:"is_default_internal"`
	Status            string    `json:"status"`
	CreatedAt         time.Time `json:"created_at"`
}

// UpdateOrganizationRequest represents the incoming JSON request payload for PUT /organizations/{id}.
type UpdateOrganizationRequest struct {
	Name     *string          `json:"name"`
	Metadata *json.RawMessage `json:"metadata"`
}

// Handler handles HTTP requests for organization endpoints.
type Handler struct {
	service orgService.OrganizationService
	logger  *slog.Logger
}

// NewHandler constructs a new Organization HTTP Handler.
func NewHandler(service orgService.OrganizationService, log *slog.Logger) *Handler {
	return &Handler{
		service: service,
		logger:  log,
	}
}

// List handles GET /api/v1/organizations - returns all organizations the authenticated user is an active member of.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")

	authCtx, ok := identityHttpapi.AuthContextFromRequest(r)
	if !ok || authCtx == nil {
		httpapi.WriteError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required", nil)
		return
	}

	memberships, err := h.service.ListUserOrganizations(r.Context(), authCtx.UserID)
	if err != nil {
		reqLogger := logger.FromContext(r.Context(), h.logger)
		reqLogger.Error("failed to retrieve user organizations")
		httpapi.WriteError(w, r, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "service temporarily unavailable", nil)
		return
	}

	res := make([]OrganizationListItemResponse, 0, len(memberships))
	for _, m := range memberships {
		res = append(res, OrganizationListItemResponse{
			ID:                m.OrganizationID,
			Name:              m.OrganizationName,
			Slug:              m.Slug,
			IsDefaultInternal: m.IsDefaultInternal,
			Status:            string(m.OrganizationStatus),
			UserRole:          string(m.Role),
			CreatedAt:         m.CreatedAt,
		})
	}

	httpapi.WriteJSON(w, r, http.StatusOK, res, "فهرست سازمان‌ها با موفقیت دریافت شد.")
}

// GetByID handles GET /api/v1/organizations/{id} - returns detailed configuration of an active organization.
func (h *Handler) GetByID(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")

	pathIDStr := r.PathValue("id")
	orgID, err := uuid.Parse(pathIDStr)
	if err != nil || orgID == uuid.Nil {
		httpapi.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "invalid organization identifier", nil)
		return
	}

	org, err := h.service.GetActiveOrganization(r.Context(), orgID)
	if err != nil {
		if errors.Is(err, domain.ErrOrgNotFound) {
			httpapi.WriteError(w, r, http.StatusNotFound, "ORGANIZATION_NOT_FOUND", "organization not found", nil)
			return
		}

		reqLogger := logger.FromContext(r.Context(), h.logger)
		reqLogger.Error("failed to retrieve organization details")
		httpapi.WriteError(w, r, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "service temporarily unavailable", nil)
		return
	}

	metadata := json.RawMessage(org.Metadata)
	if len(metadata) == 0 {
		metadata = json.RawMessage("{}")
	}

	res := OrganizationDetailResponse{
		ID:                org.ID,
		Name:              org.Name,
		Slug:              org.Slug,
		IsDefaultInternal: org.IsDefaultInternal,
		Status:            string(org.Status),
		Metadata:          metadata,
		CreatedAt:         org.CreatedAt,
		UpdatedAt:         org.UpdatedAt,
	}

	httpapi.WriteJSON(w, r, http.StatusOK, res, "اطلاعات سازمان با موفقیت دریافت شد.")
}

// Update handles PUT /api/v1/organizations/{id} - updates name and metadata of an active organization.
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")

	pathIDStr := r.PathValue("id")
	orgID, err := uuid.Parse(pathIDStr)
	if err != nil || orgID == uuid.Nil {
		httpapi.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "invalid organization identifier", nil)
		return
	}

	contentType := r.Header.Get("Content-Type")
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil || mediaType != "application/json" {
		httpapi.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "Content-Type must be application/json", nil)
		return
	}

	var reqPayload UpdateOrganizationRequest
	if err := httpapi.DecodeJSON(w, r, &reqPayload); err != nil {
		httpapi.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "invalid or malformed JSON payload", nil)
		return
	}

	if reqPayload.Name == nil || reqPayload.Metadata == nil {
		httpapi.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "name and metadata are required fields", nil)
		return
	}

	input := orgService.UpdateOrganizationInput{
		Name:     *reqPayload.Name,
		Metadata: *reqPayload.Metadata,
	}

	org, err := h.service.UpdateOrganization(r.Context(), orgID, input)
	if err != nil {
		if errors.Is(err, domain.ErrInvalidOrgName) {
			httpapi.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "organization name is invalid", []FieldValidationError{
				{Field: "name", Reason: "name must be between 1 and 100 characters"},
			})
			return
		}
		if errors.Is(err, domain.ErrInvalidMetadata) {
			httpapi.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "organization metadata must be a JSON object", []FieldValidationError{
				{Field: "metadata", Reason: "metadata must be a valid JSON object"},
			})
			return
		}
		if errors.Is(err, domain.ErrOrgNotFound) {
			httpapi.WriteError(w, r, http.StatusNotFound, "ORGANIZATION_NOT_FOUND", "organization not found", nil)
			return
		}

		reqLogger := logger.FromContext(r.Context(), h.logger)
		reqLogger.Error("organization update failure")
		httpapi.WriteError(w, r, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "service temporarily unavailable", nil)
		return
	}

	metadata := json.RawMessage(org.Metadata)
	if len(metadata) == 0 {
		metadata = json.RawMessage("{}")
	}

	res := OrganizationDetailResponse{
		ID:                org.ID,
		Name:              org.Name,
		Slug:              org.Slug,
		IsDefaultInternal: org.IsDefaultInternal,
		Status:            string(org.Status),
		Metadata:          metadata,
		CreatedAt:         org.CreatedAt,
		UpdatedAt:         org.UpdatedAt,
	}

	httpapi.WriteJSON(w, r, http.StatusOK, res, "organization updated successfully")
}

// Create handles POST /api/v1/organizations - provisions a new tenant organization (Platform admin only).
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")

	authCtx, ok := identityHttpapi.AuthContextFromRequest(r)
	if !ok || authCtx == nil {
		httpapi.WriteError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required", nil)
		return
	}

	contentType := r.Header.Get("Content-Type")
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil || mediaType != "application/json" {
		httpapi.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "Content-Type must be application/json", nil)
		return
	}

	var reqPayload CreateOrganizationRequest
	if err := httpapi.DecodeJSON(w, r, &reqPayload); err != nil {
		httpapi.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "invalid or malformed JSON payload", nil)
		return
	}

	input := orgService.CreateOrganizationInput{
		Name:     reqPayload.Name,
		Slug:     reqPayload.Slug,
		Metadata: reqPayload.Metadata,
	}

	org, err := h.service.CreateOrganization(r.Context(), authCtx.UserID, input)
	if err != nil {
		if errors.Is(err, domain.ErrInvalidOrgName) {
			httpapi.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "organization name is invalid", []FieldValidationError{
				{Field: "name", Reason: "name must be between 1 and 100 characters"},
			})
			return
		}
		if errors.Is(err, domain.ErrInvalidOrgSlug) {
			httpapi.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "organization slug is invalid", []FieldValidationError{
				{Field: "slug", Reason: "slug must contain only lowercase English letters and single hyphens"},
			})
			return
		}
		if errors.Is(err, domain.ErrInvalidMetadata) {
			httpapi.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "organization metadata must be a JSON object", []FieldValidationError{
				{Field: "metadata", Reason: "metadata must be a valid JSON object"},
			})
			return
		}
		if errors.Is(err, domain.ErrDuplicateOrgSlug) {
			httpapi.WriteError(w, r, http.StatusConflict, "ALREADY_EXISTS", "organization slug already exists", nil)
			return
		}

		reqLogger := logger.FromContext(r.Context(), h.logger)
		reqLogger.Error("organization creation failure")
		httpapi.WriteError(w, r, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "service temporarily unavailable", nil)
		return
	}

	res := CreateOrganizationResponse{
		ID:                org.ID,
		Name:              org.Name,
		Slug:              org.Slug,
		IsDefaultInternal: org.IsDefaultInternal,
		Status:            string(org.Status),
		CreatedAt:         org.CreatedAt,
	}

	httpapi.WriteJSON(w, r, http.StatusCreated, res, "سازمان با موفقیت ایجاد گردید.")
}
