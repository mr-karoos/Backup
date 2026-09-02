package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"mime"
	"net/http"

	"backup-platform/internal/credential/domain"
	"backup-platform/internal/credential/payload"
	"backup-platform/internal/credential/secretcrypto"
	orgHttpapi "backup-platform/internal/organization/httpapi"
	"backup-platform/internal/platform/httpapi"
	"backup-platform/internal/platform/logger"
	"backup-platform/pkg/uuid"
)

// CredentialService specifies the application-level operations required by the HTTP handler.
type CredentialService interface {
	CreateCredential(
		ctx context.Context,
		orgID uuid.UUID,
		name string,
		credType domain.Type,
		plaintextPayload []byte,
		fingerprint *string,
	) (*domain.CredentialMetadata, error)

	ListCredentialMetadata(
		ctx context.Context,
		orgID uuid.UUID,
	) ([]*domain.CredentialMetadata, error)

	GetCredentialMetadata(
		ctx context.Context,
		orgID uuid.UUID,
		credID uuid.UUID,
	) (*domain.CredentialMetadata, error)

	UpdateCredentialName(
		ctx context.Context,
		orgID uuid.UUID,
		credID uuid.UUID,
		name string,
	) (*domain.CredentialMetadata, error)

	ReplaceCredentialSecret(
		ctx context.Context,
		orgID uuid.UUID,
		credID uuid.UUID,
		name *string,
		plaintextPayload []byte,
		fingerprint *string,
	) (*domain.CredentialMetadata, error)

	DeleteCredential(
		ctx context.Context,
		orgID uuid.UUID,
		credID uuid.UUID,
	) error
}

// Handler handles HTTP requests for credential management endpoints.
type Handler struct {
	service CredentialService
	logger  *slog.Logger
}

// NewHandler constructs a new credential HTTP Handler.
func NewHandler(service CredentialService, log *slog.Logger) *Handler {
	if log == nil {
		log = slog.Default()
	}
	return &Handler{
		service: service,
		logger:  log,
	}
}

// clearCreateCredentialRequest clears sensitive plaintext string references from the request DTO with best effort.
func clearCreateCredentialRequest(req *CreateCredentialRequest) {
	if req == nil {
		return
	}
	req.Secret = ""
	if req.Passphrase != nil {
		*req.Passphrase = ""
		req.Passphrase = nil
	}
	if req.AccessKeyID != nil {
		*req.AccessKeyID = ""
		req.AccessKeyID = nil
	}
	if req.SecretAccessKey != nil {
		*req.SecretAccessKey = ""
		req.SecretAccessKey = nil
	}
	if req.SessionToken != nil {
		*req.SessionToken = ""
		req.SessionToken = nil
	}
}

// clearUpdateCredentialRequest clears sensitive plaintext string references from the update request DTO with best effort.
func clearUpdateCredentialRequest(req *UpdateCredentialRequest) {
	if req == nil {
		return
	}
	if req.Secret != nil {
		*req.Secret = ""
		req.Secret = nil
	}
	if req.Passphrase != nil {
		*req.Passphrase = ""
		req.Passphrase = nil
	}
	if req.AccessKeyID != nil {
		*req.AccessKeyID = ""
		req.AccessKeyID = nil
	}
	if req.SecretAccessKey != nil {
		*req.SecretAccessKey = ""
		req.SecretAccessKey = nil
	}
	if req.SessionToken != nil {
		*req.SessionToken = ""
		req.SessionToken = nil
	}
}

// List handles GET /api/v1/credentials - returns safe metadata of credentials for the active organization.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")

	tenantCtx, ok := orgHttpapi.TenantContextFromRequest(r)
	if !ok || tenantCtx == nil || tenantCtx.OrganizationID == uuid.Nil {
		httpapi.WriteError(w, r, http.StatusForbidden, "FORBIDDEN", "access denied", nil)
		return
	}

	items, err := h.service.ListCredentialMetadata(r.Context(), tenantCtx.OrganizationID)
	if err != nil {
		reqLogger := logger.FromContext(r.Context(), h.logger)
		reqLogger.Error("failed to list credentials")
		httpapi.WriteError(w, r, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "service temporarily unavailable", nil)
		return
	}

	respData := make([]CredentialListItemResponse, len(items))
	for i, item := range items {
		respData[i] = CredentialListItemResponse{
			ID:          item.ID.String(),
			Name:        item.Name,
			Type:        string(item.Type),
			Fingerprint: item.Fingerprint,
			KeyVersion:  item.KeyVersion,
			CreatedAt:   item.CreatedAt,
		}
	}

	httpapi.WriteJSON(w, r, http.StatusOK, respData, "اعتبارنامه‌ها با موفقیت دریافت شدند.")
}

// Create handles POST /api/v1/credentials - creates a new encrypted credential in the active organization.
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

	var req CreateCredentialRequest
	if err := httpapi.DecodeJSON(w, r, &req); err != nil {
		httpapi.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "invalid or malformed JSON payload", nil)
		return
	}
	defer clearCreateCredentialRequest(&req)

	// 1. Validate name
	validName, err := domain.ValidateName(req.Name)
	if err != nil {
		httpapi.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "credential name must be between 1 and 100 characters", nil)
		return
	}

	// 2. Validate type
	credType := domain.Type(req.Type)
	if err := domain.ValidateType(credType); err != nil {
		httpapi.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "unsupported or invalid credential type", nil)
		return
	}

	var payloadBytes []byte
	var fingerprint *string

	if credType == domain.TypeS3Credentials {
		// S3 credentials validation
		if req.AccessKeyID == nil || len(*req.AccessKeyID) == 0 ||
			req.SecretAccessKey == nil || len(*req.SecretAccessKey) == 0 {
			httpapi.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "access_key_id and secret_access_key are required for s3_credentials", nil)
			return
		}
		if req.Passphrase != nil && len(*req.Passphrase) > 0 {
			httpapi.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "passphrase is only supported for ssh_private_key", nil)
			return
		}
		pb, err := payload.EncodeS3V1(*req.AccessKeyID, *req.SecretAccessKey, req.SessionToken)
		clearCreateCredentialRequest(&req)
		if err != nil {
			httpapi.WriteError(w, r, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "internal error processing credential", nil)
			return
		}
		payloadBytes = pb
	} else {
		// Standard single-secret credentials
		if len(req.Secret) == 0 {
			httpapi.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "secret payload cannot be empty", nil)
			return
		}
		if credType != domain.TypeSSHPrivateKey && req.Passphrase != nil && len(*req.Passphrase) > 0 {
			httpapi.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "passphrase is only supported for ssh_private_key", nil)
			return
		}
		if credType == domain.TypeSSHPrivateKey {
			fp, err := processSSHKey(req.Secret, req.Passphrase)
			if err != nil {
				httpapi.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "invalid SSH private key or passphrase", nil)
				return
			}
			fingerprint = fp
		}
		pb, err := payload.EncodeV1(req.Secret, req.Passphrase)
		clearCreateCredentialRequest(&req)
		if err != nil {
			httpapi.WriteError(w, r, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "internal error processing credential", nil)
			return
		}
		payloadBytes = pb
	}
	defer secretcrypto.ZeroBytes(payloadBytes)

	// 7. Persist encrypted credential via VaultService
	meta, err := h.service.CreateCredential(
		r.Context(),
		tenantCtx.OrganizationID,
		validName,
		credType,
		payloadBytes,
		fingerprint,
	)
	secretcrypto.ZeroBytes(payloadBytes)
	if err != nil {
		if errors.Is(err, domain.ErrInvalidCredentialName) ||
			errors.Is(err, domain.ErrInvalidCredentialType) ||
			errors.Is(err, domain.ErrEmptyPlaintextSecret) ||
			errors.Is(err, domain.ErrInvalidSSHKey) {
			httpapi.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "validation failed", nil)
			return
		}
		if errors.Is(err, domain.ErrCredentialEncryptionFailed) || errors.Is(err, domain.ErrCredentialServiceUnavailable) {
			reqLogger := logger.FromContext(r.Context(), h.logger)
			reqLogger.Error("credential creation failed")
			httpapi.WriteError(w, r, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "service temporarily unavailable", nil)
			return
		}

		reqLogger := logger.FromContext(r.Context(), h.logger)
		reqLogger.Error("unexpected credential creation error")
		httpapi.WriteError(w, r, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "internal server error", nil)
		return
	}

	res := CredentialCreateResponse{
		ID:          meta.ID.String(),
		Name:        meta.Name,
		Type:        string(meta.Type),
		Fingerprint: meta.Fingerprint,
		CreatedAt:   meta.CreatedAt,
	}

	httpapi.WriteJSON(w, r, http.StatusCreated, res, "اعتبارنامه با موفقیت ایجاد شد.")
}

// Update handles PUT /api/v1/credentials/{id} - updates name and/or secret material of an existing credential.
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")

	tenantCtx, ok := orgHttpapi.TenantContextFromRequest(r)
	if !ok || tenantCtx == nil || tenantCtx.OrganizationID == uuid.Nil {
		httpapi.WriteError(w, r, http.StatusForbidden, "FORBIDDEN", "access denied", nil)
		return
	}

	idStr := r.PathValue("id")
	credID, err := uuid.Parse(idStr)
	if err != nil || credID == uuid.Nil {
		httpapi.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "invalid credential identifier", nil)
		return
	}

	contentType := r.Header.Get("Content-Type")
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil || mediaType != "application/json" {
		httpapi.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "Content-Type must be application/json", nil)
		return
	}

	var req UpdateCredentialRequest
	if err := httpapi.DecodeJSON(w, r, &req); err != nil {
		httpapi.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "invalid or malformed JSON payload", nil)
		return
	}
	defer clearUpdateCredentialRequest(&req)

	// Require at least one editable field to be present
	hasSecretUpdate := req.Secret != nil || req.AccessKeyID != nil || req.SecretAccessKey != nil
	if req.Name == nil && !hasSecretUpdate {
		httpapi.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "at least one of name, secret, or access credentials must be provided", nil)
		return
	}

	var validName *string
	if req.Name != nil {
		vn, err := domain.ValidateName(*req.Name)
		if err != nil {
			httpapi.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "credential name must be between 1 and 100 characters", nil)
			return
		}
		validName = &vn
	}

	// Case 1: Name-only update (no secret replacement)
	if !hasSecretUpdate {
		if req.Passphrase != nil {
			httpapi.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "passphrase can only be updated when a new secret is provided", nil)
			return
		}

		meta, err := h.service.UpdateCredentialName(r.Context(), tenantCtx.OrganizationID, credID, *validName)
		if err != nil {
			if errors.Is(err, domain.ErrCredentialNotFound) {
				httpapi.WriteError(w, r, http.StatusNotFound, "CREDENTIAL_NOT_FOUND", "credential not found", nil)
				return
			}
			if errors.Is(err, domain.ErrInvalidCredentialName) {
				httpapi.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "credential name must be between 1 and 100 characters", nil)
				return
			}
			reqLogger := logger.FromContext(r.Context(), h.logger)
			reqLogger.Error("failed to update credential name")
			httpapi.WriteError(w, r, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "service temporarily unavailable", nil)
			return
		}

		res := CredentialUpdateResponse{
			ID:          meta.ID.String(),
			Name:        meta.Name,
			Type:        string(meta.Type),
			Fingerprint: meta.Fingerprint,
			KeyVersion:  meta.KeyVersion,
			CreatedAt:   meta.CreatedAt,
			UpdatedAt:   meta.UpdatedAt,
		}
		httpapi.WriteJSON(w, r, http.StatusOK, res, "اعتبارنامه با موفقیت به‌روزرسانی شد.")
		return
	}

	// Case 2: Secret update (with or without name)
	// Load current metadata to verify existence and inspect credential type
	currentMeta, err := h.service.GetCredentialMetadata(r.Context(), tenantCtx.OrganizationID, credID)
	if err != nil {
		if errors.Is(err, domain.ErrCredentialNotFound) {
			httpapi.WriteError(w, r, http.StatusNotFound, "CREDENTIAL_NOT_FOUND", "credential not found", nil)
			return
		}
		reqLogger := logger.FromContext(r.Context(), h.logger)
		reqLogger.Error("failed to load credential metadata for update")
		httpapi.WriteError(w, r, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "service temporarily unavailable", nil)
		return
	}

	var payloadBytes []byte
	var fingerprint *string

	if currentMeta.Type == domain.TypeS3Credentials {
		if req.AccessKeyID == nil || len(*req.AccessKeyID) == 0 ||
			req.SecretAccessKey == nil || len(*req.SecretAccessKey) == 0 {
			httpapi.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "both access_key_id and secret_access_key are required when updating s3_credentials", nil)
			return
		}
		if req.Passphrase != nil && len(*req.Passphrase) > 0 {
			httpapi.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "passphrase is only supported for ssh_private_key", nil)
			return
		}
		pb, err := payload.EncodeS3V1(*req.AccessKeyID, *req.SecretAccessKey, req.SessionToken)
		clearUpdateCredentialRequest(&req)
		if err != nil {
			httpapi.WriteError(w, r, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "internal error processing credential", nil)
			return
		}
		payloadBytes = pb
	} else {
		if req.Secret == nil || len(*req.Secret) == 0 {
			httpapi.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "secret payload cannot be empty", nil)
			return
		}
		if currentMeta.Type != domain.TypeSSHPrivateKey && req.Passphrase != nil && len(*req.Passphrase) > 0 {
			httpapi.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "passphrase is only supported for ssh_private_key", nil)
			return
		}
		if currentMeta.Type == domain.TypeSSHPrivateKey {
			fp, err := processSSHKey(*req.Secret, req.Passphrase)
			if err != nil {
				httpapi.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "invalid SSH private key or passphrase", nil)
				return
			}
			fingerprint = fp
		}
		pb, err := payload.EncodeV1(*req.Secret, req.Passphrase)
		clearUpdateCredentialRequest(&req)
		if err != nil {
			httpapi.WriteError(w, r, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "internal error processing credential", nil)
			return
		}
		payloadBytes = pb
	}
	defer secretcrypto.ZeroBytes(payloadBytes)

	meta, err := h.service.ReplaceCredentialSecret(
		r.Context(),
		tenantCtx.OrganizationID,
		credID,
		validName,
		payloadBytes,
		fingerprint,
	)
	secretcrypto.ZeroBytes(payloadBytes)
	if err != nil {
		if errors.Is(err, domain.ErrCredentialNotFound) {
			httpapi.WriteError(w, r, http.StatusNotFound, "CREDENTIAL_NOT_FOUND", "credential not found", nil)
			return
		}
		if errors.Is(err, domain.ErrInvalidCredentialName) ||
			errors.Is(err, domain.ErrEmptyPlaintextSecret) ||
			errors.Is(err, domain.ErrInvalidSSHKey) {
			httpapi.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "validation failed", nil)
			return
		}
		if errors.Is(err, domain.ErrCredentialEncryptionFailed) || errors.Is(err, domain.ErrCredentialServiceUnavailable) {
			reqLogger := logger.FromContext(r.Context(), h.logger)
			reqLogger.Error("credential secret update failed")
			httpapi.WriteError(w, r, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "service temporarily unavailable", nil)
			return
		}

		reqLogger := logger.FromContext(r.Context(), h.logger)
		reqLogger.Error("unexpected credential update error")
		httpapi.WriteError(w, r, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "internal server error", nil)
		return
	}

	res := CredentialUpdateResponse{
		ID:          meta.ID.String(),
		Name:        meta.Name,
		Type:        string(meta.Type),
		Fingerprint: meta.Fingerprint,
		KeyVersion:  meta.KeyVersion,
		CreatedAt:   meta.CreatedAt,
		UpdatedAt:   meta.UpdatedAt,
	}

	httpapi.WriteJSON(w, r, http.StatusOK, res, "اعتبارنامه با موفقیت به‌روزرسانی شد.")
}

// Delete handles DELETE /api/v1/credentials/{id} - permanently deletes an unreferenced credential.
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")

	tenantCtx, ok := orgHttpapi.TenantContextFromRequest(r)
	if !ok || tenantCtx == nil || tenantCtx.OrganizationID == uuid.Nil {
		httpapi.WriteError(w, r, http.StatusForbidden, "FORBIDDEN", "access denied", nil)
		return
	}

	idStr := r.PathValue("id")
	credID, err := uuid.Parse(idStr)
	if err != nil || credID == uuid.Nil {
		httpapi.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "invalid credential identifier", nil)
		return
	}

	err = h.service.DeleteCredential(r.Context(), tenantCtx.OrganizationID, credID)
	if err != nil {
		if errors.Is(err, domain.ErrCredentialNotFound) {
			httpapi.WriteError(w, r, http.StatusNotFound, "CREDENTIAL_NOT_FOUND", "credential not found", nil)
			return
		}
		if errors.Is(err, domain.ErrCredentialInUse) {
			httpapi.WriteError(w, r, http.StatusConflict, "CREDENTIAL_IN_USE", "credential is currently in use", nil)
			return
		}
		if errors.Is(err, domain.ErrCredentialServiceUnavailable) {
			reqLogger := logger.FromContext(r.Context(), h.logger)
			reqLogger.Error("credential deletion failed")
			httpapi.WriteError(w, r, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "service temporarily unavailable", nil)
			return
		}

		reqLogger := logger.FromContext(r.Context(), h.logger)
		reqLogger.Error("unexpected credential deletion error")
		httpapi.WriteError(w, r, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "internal server error", nil)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
