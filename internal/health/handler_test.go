package health

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

type mockPinger struct {
	err error
}

func (m *mockPinger) Ping(ctx context.Context) error {
	return m.err
}

func TestHealthHandler_Healthy(t *testing.T) {
	mockDB := &mockPinger{err: nil}
	handler := NewHandler(mockDB)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status code 200 OK, got: %d", rr.Code)
	}

	contentType := rr.Header().Get("Content-Type")
	if contentType != "application/json; charset=utf-8" {
		t.Errorf("expected Content-Type application/json; charset=utf-8, got: %s", contentType)
	}

	var resp Response
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode json response: %v", err)
	}

	if resp.Status != "ok" {
		t.Errorf("expected status 'ok', got: '%s'", resp.Status)
	}
}

func TestHealthHandler_DatabaseUnavailable(t *testing.T) {
	mockDB := &mockPinger{err: errors.New("connection refused")}
	handler := NewHandler(mockDB)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status code 503 Service Unavailable, got: %d", rr.Code)
	}

	var resp Response
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode json response: %v", err)
	}

	if resp.Status != "unavailable" {
		t.Errorf("expected status 'unavailable', got: '%s'", resp.Status)
	}
}

func TestHealthHandler_NilDatabase(t *testing.T) {
	handler := NewHandler(nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status code 503 Service Unavailable, got: %d", rr.Code)
	}

	var resp Response
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode json response: %v", err)
	}

	if resp.Status != "unavailable" {
		t.Errorf("expected status 'unavailable', got: '%s'", resp.Status)
	}
}

func TestHealthHandler_MethodNotAllowed(t *testing.T) {
	mockDB := &mockPinger{err: nil}
	handler := NewHandler(mockDB)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/health", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected status code 405 Method Not Allowed, got: %d", rr.Code)
	}
}
