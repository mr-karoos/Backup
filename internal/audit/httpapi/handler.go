package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"backup-platform/internal/audit/domain"
	"backup-platform/internal/audit/service"
	orgHttpapi "backup-platform/internal/organization/httpapi"
	"backup-platform/internal/platform/httpapi"
	"backup-platform/internal/platform/logger"
	"backup-platform/pkg/uuid"
)

// NoStoreMiddleware ensures Cache-Control: no-store and Pragma: no-cache
// are set on all responses for the wrapped route, including middleware rejections.
func NoStoreMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Pragma", "no-cache")
		next.ServeHTTP(w, r)
	})
}

// parseSingletonQueryParam validates that if the key is present in q:
// 1. It must have exactly one value (no duplicate keys, e.g. ?action=a&action=b).
// 2. Its trimmed value cannot be empty (e.g. ?action= or ?action=%20).
// If absent, returns ("", false, nil).
// If invalid, returns ("", true, error).
// If valid and present, returns (rawValue, true, nil) preserving original raw input.
func parseSingletonQueryParam(q url.Values, key string) (string, bool, error) {
	values, ok := q[key]
	if !ok {
		return "", false, nil
	}
	if len(values) != 1 {
		return "", true, fmt.Errorf("duplicate query parameter: %s", key)
	}
	raw := values[0]
	if strings.TrimSpace(raw) == "" {
		return "", true, fmt.Errorf("%s query parameter cannot be empty", key)
	}
	return raw, true, nil
}

// Handler exposes HTTP REST endpoints for the audit subsystem.
type Handler struct {
	auditReader service.AuditLogReader
	logger      *slog.Logger
}

// NewHandler constructs a new Handler.
func NewHandler(auditReader service.AuditLogReader, log *slog.Logger) *Handler {
	if log == nil {
		log = slog.Default()
	}
	return &Handler{
		auditReader: auditReader,
		logger:      log,
	}
}

// ListAuditLogs handles GET /api/v1/audit-logs.
func (h *Handler) ListAuditLogs(w http.ResponseWriter, r *http.Request) {
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

	if h.auditReader == nil {
		httpapi.WriteError(w, r, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "audit service is not available", nil)
		return
	}

	// 2. Strict Query Parameter Validation
	q := r.URL.Query()
	for key := range q {
		if key != "limit" && key != "cursor" && key != "action" && key != "entity_type" &&
			key != "entity_id" && key != "user_id" && key != "from" && key != "to" {
			httpapi.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", fmt.Sprintf("unknown query parameter: %s", key), nil)
			return
		}
	}

	limit := 50
	if limitVal, present, err := parseSingletonQueryParam(q, "limit"); err != nil {
		httpapi.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", err.Error(), nil)
		return
	} else if present {
		parsedLimit, err := strconv.Atoi(limitVal)
		if err != nil || parsedLimit < 1 || parsedLimit > 100 {
			httpapi.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "limit must be an integer between 1 and 100", nil)
			return
		}
		limit = parsedLimit
	}

	var filter domain.AuditLogFilter
	filter.Limit = limit

	if cursorVal, present, err := parseSingletonQueryParam(q, "cursor"); err != nil {
		httpapi.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", err.Error(), nil)
		return
	} else if present {
		curTime, curID, err := domain.DecodeAuditCursor(cursorVal)
		if err != nil {
			httpapi.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "invalid cursor format", nil)
			return
		}
		filter.CursorCreatedAt = &curTime
		filter.CursorID = &curID
	}

	if actionVal, present, err := parseSingletonQueryParam(q, "action"); err != nil {
		httpapi.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", err.Error(), nil)
		return
	} else if present {
		if len(actionVal) > 100 {
			httpapi.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "action parameter exceeds maximum length of 100", nil)
			return
		}
		filter.Action = &actionVal
	}

	if entityTypeVal, present, err := parseSingletonQueryParam(q, "entity_type"); err != nil {
		httpapi.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", err.Error(), nil)
		return
	} else if present {
		if len(entityTypeVal) > 50 {
			httpapi.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "entity_type parameter exceeds maximum length of 50", nil)
			return
		}
		filter.EntityType = &entityTypeVal
	}

	if entityIDVal, present, err := parseSingletonQueryParam(q, "entity_id"); err != nil {
		httpapi.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", err.Error(), nil)
		return
	} else if present {
		if strings.TrimSpace(entityIDVal) != entityIDVal {
			httpapi.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "invalid entity_id query parameter format", nil)
			return
		}
		entID, err := uuid.Parse(entityIDVal)
		if err != nil || entID == uuid.Nil {
			httpapi.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "invalid entity_id query parameter format", nil)
			return
		}
		filter.EntityID = &entID
	}

	if userIDVal, present, err := parseSingletonQueryParam(q, "user_id"); err != nil {
		httpapi.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", err.Error(), nil)
		return
	} else if present {
		if strings.TrimSpace(userIDVal) != userIDVal {
			httpapi.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "invalid user_id query parameter format", nil)
			return
		}
		uID, err := uuid.Parse(userIDVal)
		if err != nil || uID == uuid.Nil {
			httpapi.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "invalid user_id query parameter format", nil)
			return
		}
		filter.UserID = &uID
	}

	var fromTime, toTime *time.Time
	if fromVal, present, err := parseSingletonQueryParam(q, "from"); err != nil {
		httpapi.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", err.Error(), nil)
		return
	} else if present {
		parsedFrom, err := time.Parse(time.RFC3339, fromVal)
		if err != nil {
			httpapi.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "invalid from timestamp format (RFC3339 required)", nil)
			return
		}
		fromTime = &parsedFrom
		filter.From = fromTime
	}

	if toVal, present, err := parseSingletonQueryParam(q, "to"); err != nil {
		httpapi.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", err.Error(), nil)
		return
	} else if present {
		parsedTo, err := time.Parse(time.RFC3339, toVal)
		if err != nil {
			httpapi.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "invalid to timestamp format (RFC3339 required)", nil)
			return
		}
		toTime = &parsedTo
		filter.To = toTime
	}

	if fromTime != nil && toTime != nil && fromTime.After(*toTime) {
		httpapi.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "from timestamp cannot be after to timestamp", nil)
		return
	}

	// 3. Execute Service Query
	result, err := h.auditReader.ListAuditLogs(r.Context(), tenantCtx.Role, tenantCtx.OrganizationID, filter)
	if err != nil {
		if errors.Is(err, domain.ErrUnauthorizedRole) {
			httpapi.WriteError(w, r, http.StatusForbidden, "FORBIDDEN", "unauthorized role", nil)

			return
		}
		if errors.Is(err, domain.ErrInvalidAuditFilter) {
			httpapi.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "invalid audit filter", nil)
			return
		}
		if errors.Is(err, domain.ErrAuditServiceUnavailable) {
			httpapi.WriteError(w, r, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "service temporarily unavailable", nil)
			return
		}
		reqLogger.Error("failed listing audit logs", "error", err.Error())
		httpapi.WriteError(w, r, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "internal server error", nil)
		return
	}

	// 4. Transform to DTO
	data := make([]AuditLogDTO, len(result.Logs))
	for i, entry := range result.Logs {
		data[i] = ToAuditLogDTO(entry)
	}

	page := PaginationMeta{
		NextCursor: result.NextCursor,
		HasMore:    result.HasMore,
	}

	writePaginatedJSON(w, r, http.StatusOK, data, page, "audit logs retrieved successfully")
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
