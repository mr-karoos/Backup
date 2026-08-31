package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"backup-platform/internal/platform/logger"
)

// MaxRequestBodyBytes bounds the maximum size of accepted JSON request payloads (64 KiB).
const MaxRequestBodyBytes = 64 * 1024

var (
	ErrInvalidJSONBody = errors.New("request body contains invalid or malformed JSON")
	ErrBodyTooLarge    = errors.New("request body exceeds maximum allowed size of 64 KiB")
	ErrEmptyBody       = errors.New("request body cannot be empty")
)

// ResponseEnvelope is the canonical envelope for successful API responses.
type ResponseEnvelope struct {
	Data      any    `json:"data"`
	Message   string `json:"message,omitempty"`
	RequestID string `json:"request_id,omitempty"`
}

// ErrorDetail describes the canonical error object structure.
type ErrorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details any    `json:"details"`
}

// ErrorEnvelope is the canonical envelope for failed API responses.
type ErrorEnvelope struct {
	Error     ErrorDetail `json:"error"`
	RequestID string      `json:"request_id,omitempty"`
}

// WriteJSON writes a successful JSON response matching the standard envelope.
func WriteJSON(w http.ResponseWriter, r *http.Request, status int, data any, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)

	reqID := logger.RequestIDFromContext(r.Context())
	envelope := ResponseEnvelope{
		Data:      data,
		Message:   message,
		RequestID: reqID,
	}

	_ = json.NewEncoder(w).Encode(envelope)
}

// WriteError writes an error JSON response matching the standard error envelope.
func WriteError(w http.ResponseWriter, r *http.Request, status int, code, message string, details any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)

	reqID := logger.RequestIDFromContext(r.Context())
	envelope := ErrorEnvelope{
		Error: ErrorDetail{
			Code:    code,
			Message: message,
			Details: details,
		},
		RequestID: reqID,
	}

	_ = json.NewEncoder(w).Encode(envelope)
}

// DecodeJSON strictly decodes a single JSON object from the request body up to MaxRequestBodyBytes.
func DecodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	if r.Body == nil {
		return ErrEmptyBody
	}

	limitedReader := http.MaxBytesReader(w, r.Body, MaxRequestBodyBytes)
	bodyBytes, err := io.ReadAll(limitedReader)

	// Best-effort reduction of sensitive request-body lifetime in process memory.
	// This reduces the retention window of sensitive request bytes in the heap, including
	// partial body bytes returned alongside a read error (e.g. MaxBytesError), but does
	// not guarantee complete wiping of underlying Go runtime/HTTP transport copies.
	defer func() {
		clear(bodyBytes)
	}()

	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			return ErrBodyTooLarge
		}
		return ErrInvalidJSONBody
	}

	trimmed := bytes.TrimSpace(bodyBytes)
	if len(trimmed) == 0 {
		return ErrEmptyBody
	}

	// Request contract strictly requires a single JSON Object
	if trimmed[0] != '{' || trimmed[len(trimmed)-1] != '}' {
		return ErrInvalidJSONBody
	}

	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.DisallowUnknownFields()

	if err := dec.Decode(dst); err != nil {
		return ErrInvalidJSONBody
	}

	// Reject trailing data or multiple JSON values
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return ErrInvalidJSONBody
	}

	return nil
}
