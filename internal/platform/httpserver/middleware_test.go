package httpserver

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"backup-platform/internal/platform/logger"
)

func TestRequestIDMiddleware_GeneratedWhenMissing(t *testing.T) {
	handler := RequestIDMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	reqID := rr.Header().Get(HeaderXRequestID)
	if reqID == "" {
		t.Fatal("expected X-Request-ID response header to be set, got empty")
	}
	if len(reqID) != 32 { // 16 bytes hex = 32 chars
		t.Errorf("expected 32-char hex request ID, got: %s (len: %d)", reqID, len(reqID))
	}
}

func TestRequestIDMiddleware_PreservesValidIncomingHeader(t *testing.T) {
	handler := RequestIDMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set(HeaderXRequestID, "custom-request-id-123")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	reqID := rr.Header().Get(HeaderXRequestID)
	if reqID != "custom-request-id-123" {
		t.Errorf("expected X-Request-ID to be preserved as 'custom-request-id-123', got: %s", reqID)
	}
}

func TestRequestIDMiddleware_ReplacesInvalidIncomingHeader(t *testing.T) {
	handler := RequestIDMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set(HeaderXRequestID, "invalid id with spaces & symbols @#$")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	reqID := rr.Header().Get(HeaderXRequestID)
	if reqID == "invalid id with spaces & symbols @#$" || reqID == "" {
		t.Errorf("expected invalid X-Request-ID to be replaced with new valid ID, got: %s", reqID)
	}
}

func TestStatusResponseWriter_FirstStatusPreserved(t *testing.T) {
	var buf bytes.Buffer
	log := logger.New("info", &buf)

	handler := LoggingMiddleware(log)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)            // 202
		w.WriteHeader(http.StatusInternalServerError) // 500 should be ignored
		_, _ = w.Write([]byte("response body"))
	}))

	req := httptest.NewRequest(http.MethodPost, "/test", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Errorf("expected status code 202 Accepted, got: %d", rr.Code)
	}
}

func TestStatusResponseWriter_Implicit200OnWrite(t *testing.T) {
	var buf bytes.Buffer
	log := logger.New("info", &buf)

	handler := LoggingMiddleware(log)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("hello world"))
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status code 200 OK on implicit write, got: %d", rr.Code)
	}
}

func TestLoggingMiddleware_UserAgentBounded(t *testing.T) {
	var buf bytes.Buffer
	log := logger.New("info", &buf)

	handler := LoggingMiddleware(log)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	longUA := strings.Repeat("A", 1000)
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("User-Agent", longUA)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	logOutput := buf.String()
	if strings.Contains(logOutput, longUA) {
		t.Errorf("expected log output to NOT contain full 1000-char User-Agent")
	}
	expectedTruncated := strings.Repeat("A", 512)
	if !strings.Contains(logOutput, expectedTruncated) {
		t.Errorf("expected log output to contain truncated 512-char User-Agent")
	}
}

func TestLoggingMiddleware_UnicodeUserAgentBounded(t *testing.T) {
	var buf bytes.Buffer
	log := logger.New("info", &buf)

	handler := LoggingMiddleware(log)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Multi-byte Unicode characters (e.g. Persian/Arabic/Emojis)
	longUnicodeUA := strings.Repeat("گزارش", 200) // 1000 runes
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("User-Agent", longUnicodeUA)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	logOutput := buf.String()
	if strings.Contains(logOutput, longUnicodeUA) {
		t.Errorf("expected log output to NOT contain full 1000-rune Unicode User-Agent")
	}
	// Truncated to 512 runes
	expectedTruncated := string([]rune(longUnicodeUA)[:512])
	if !strings.Contains(logOutput, expectedTruncated) {
		t.Errorf("expected log output to contain 512-rune truncated Unicode User-Agent")
	}
}
