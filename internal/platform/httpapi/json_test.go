package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"backup-platform/internal/platform/logger"
)

type sampleRequest struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

func TestWriteJSON_Success(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	req = req.WithContext(logger.WithRequestID(req.Context(), "req-12345"))

	data := map[string]string{"foo": "bar"}
	WriteJSON(rec, req, http.StatusOK, data, "operation successful")

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got: %d", rec.Code)
	}
	if !strings.Contains(rec.Header().Get("Content-Type"), "application/json") {
		t.Errorf("expected json content-type, got: %s", rec.Header().Get("Content-Type"))
	}

	var env ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if env.Message != "operation successful" {
		t.Errorf("expected message match, got: %s", env.Message)
	}
	if env.RequestID != "req-12345" {
		t.Errorf("expected request_id 'req-12345', got: %s", env.RequestID)
	}
}

func TestWriteError_StandardEnvelope(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/test", nil)
	req = req.WithContext(logger.WithRequestID(req.Context(), "err-req-999"))

	WriteError(rec, req, http.StatusUnauthorized, "INVALID_CREDENTIALS", "invalid email or password", nil)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got: %d", rec.Code)
	}

	var env ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}

	if env.Error.Code != "INVALID_CREDENTIALS" {
		t.Errorf("expected code INVALID_CREDENTIALS, got: %s", env.Error.Code)
	}
	if env.Error.Message != "invalid email or password" {
		t.Errorf("expected message match, got: %s", env.Error.Message)
	}
	if env.RequestID != "err-req-999" {
		t.Errorf("expected request_id 'err-req-999', got: %s", env.RequestID)
	}

	// Details must be explicitly serialized as null
	rawJSON := rec.Body.String()
	if !strings.Contains(rawJSON, `"details":null`) && !strings.Contains(rawJSON, `"details": null`) {
		t.Errorf("expected JSON to contain '\"details\":null', got: %s", rawJSON)
	}
}

func TestDecodeJSON(t *testing.T) {
	t.Run("valid json body", func(t *testing.T) {
		body := `{"name":"Alice","email":"alice@example.com"}`
		req := httptest.NewRequest("POST", "/", strings.NewReader(body))
		rec := httptest.NewRecorder()

		var dst sampleRequest
		err := DecodeJSON(rec, req, &dst)
		if err != nil {
			t.Fatalf("expected success, got: %v", err)
		}
		if dst.Name != "Alice" || dst.Email != "alice@example.com" {
			t.Errorf("decoded struct mismatch: %+v", dst)
		}
	})

	t.Run("unknown fields rejected", func(t *testing.T) {
		body := `{"name":"Alice","email":"alice@example.com","extra_field":"bad"}`
		req := httptest.NewRequest("POST", "/", strings.NewReader(body))
		rec := httptest.NewRecorder()

		var dst sampleRequest
		err := DecodeJSON(rec, req, &dst)
		if !errors.Is(err, ErrInvalidJSONBody) {
			t.Errorf("expected ErrInvalidJSONBody for unknown fields, got: %v", err)
		}
	})

	t.Run("trailing garbage rejected", func(t *testing.T) {
		body := `{"name":"Alice","email":"alice@example.com"} trailing garbage`
		req := httptest.NewRequest("POST", "/", strings.NewReader(body))
		rec := httptest.NewRecorder()

		var dst sampleRequest
		err := DecodeJSON(rec, req, &dst)
		if !errors.Is(err, ErrInvalidJSONBody) {
			t.Errorf("expected ErrInvalidJSONBody for trailing data, got: %v", err)
		}
	})

	t.Run("body exceeds 64 KiB rejected (MaxBytesError with partial body read)", func(t *testing.T) {
		largeVal := strings.Repeat("A", 65*1024)
		body := `{"name":"` + largeVal + `","email":"a@b.com"}`
		req := httptest.NewRequest("POST", "/", bytes.NewReader([]byte(body)))
		rec := httptest.NewRecorder()

		var dst sampleRequest
		err := DecodeJSON(rec, req, &dst)
		if !errors.Is(err, ErrBodyTooLarge) {
			t.Errorf("expected ErrBodyTooLarge, got: %v", err)
		}
	})

	t.Run("empty body rejected", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/", strings.NewReader(""))
		rec := httptest.NewRecorder()

		var dst sampleRequest
		err := DecodeJSON(rec, req, &dst)
		if !errors.Is(err, ErrEmptyBody) {
			t.Errorf("expected ErrEmptyBody, got: %v", err)
		}
	})

	t.Run("null literal rejected", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/", strings.NewReader("null"))
		rec := httptest.NewRecorder()

		var dst sampleRequest
		err := DecodeJSON(rec, req, &dst)
		if !errors.Is(err, ErrInvalidJSONBody) {
			t.Errorf("expected ErrInvalidJSONBody for null literal, got: %v", err)
		}
	})

	t.Run("array rejected", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/", strings.NewReader(`[{"name":"Alice"}]`))
		rec := httptest.NewRecorder()

		var dst sampleRequest
		err := DecodeJSON(rec, req, &dst)
		if !errors.Is(err, ErrInvalidJSONBody) {
			t.Errorf("expected ErrInvalidJSONBody for array, got: %v", err)
		}
	})

	t.Run("scalar string rejected", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/", strings.NewReader(`"string"`))
		rec := httptest.NewRecorder()

		var dst sampleRequest
		err := DecodeJSON(rec, req, &dst)
		if !errors.Is(err, ErrInvalidJSONBody) {
			t.Errorf("expected ErrInvalidJSONBody for string literal, got: %v", err)
		}
	})

	t.Run("scalar number rejected", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/", strings.NewReader(`12345`))
		rec := httptest.NewRecorder()

		var dst sampleRequest
		err := DecodeJSON(rec, req, &dst)
		if !errors.Is(err, ErrInvalidJSONBody) {
			t.Errorf("expected ErrInvalidJSONBody for numeric literal, got: %v", err)
		}
	})

	t.Run("multiple json objects rejected", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/", strings.NewReader(`{"name":"A","email":"a@b.com"}{"name":"B","email":"b@b.com"}`))
		rec := httptest.NewRecorder()

		var dst sampleRequest
		err := DecodeJSON(rec, req, &dst)
		if !errors.Is(err, ErrInvalidJSONBody) {
			t.Errorf("expected ErrInvalidJSONBody for multiple JSON objects, got: %v", err)
		}
	})
}
