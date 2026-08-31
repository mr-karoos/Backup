package httpserver

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"backup-platform/internal/platform/logger"
)

const HeaderXRequestID = "X-Request-ID"

// RequestIDMiddleware ensures every HTTP request carries a sanitized or generated request ID.
func RequestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		incomingID := strings.TrimSpace(r.Header.Get(HeaderXRequestID))
		reqID := sanitizeRequestID(incomingID)
		if reqID == "" {
			reqID = generateRandomID()
		}

		ctx := logger.WithRequestID(r.Context(), reqID)
		r = r.WithContext(ctx)

		w.Header().Set(HeaderXRequestID, reqID)
		next.ServeHTTP(w, r)
	})
}

// LoggingMiddleware logs all completed HTTP requests using structured slog logging.
func LoggingMiddleware(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			wrapped := &statusResponseWriter{ResponseWriter: w, statusCode: http.StatusOK}

			next.ServeHTTP(wrapped, r)

			duration := time.Since(start)
			reqLogger := logger.FromContext(r.Context(), log)

			ua := sanitizeUserAgent(r.UserAgent())

			reqLogger.Info("http request completed",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", wrapped.statusCode),
				slog.Int64("duration_ms", duration.Milliseconds()),
				slog.String("remote_addr", r.RemoteAddr),
				slog.String("user_agent", ua),
			)
		})
	}
}

func sanitizeUserAgent(ua string) string {
	if !utf8.ValidString(ua) {
		ua = strings.ToValidUTF8(ua, "")
	}
	runes := []rune(ua)
	if len(runes) > 512 {
		return string(runes[:512])
	}
	return ua
}

type statusResponseWriter struct {
	http.ResponseWriter
	statusCode  int
	wroteHeader bool
}

func (rw *statusResponseWriter) WriteHeader(code int) {
	if rw.wroteHeader {
		return
	}
	rw.statusCode = code
	rw.wroteHeader = true
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *statusResponseWriter) Write(b []byte) (int, error) {
	if !rw.wroteHeader {
		rw.WriteHeader(http.StatusOK)
	}
	return rw.ResponseWriter.Write(b)
}

func sanitizeRequestID(id string) string {
	if len(id) < 1 || len(id) > 64 {
		return ""
	}
	for i := 0; i < len(id); i++ {
		b := id[i]
		if !((b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') || b == '-' || b == '_') {
			return ""
		}
	}
	return id
}

func generateRandomID() string {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		// Fallback deterministic timestamp format if crypto rand fails
		return hex.EncodeToString([]byte(time.Now().Format("20060102150405.000000")))
	}
	return hex.EncodeToString(bytes)
}
