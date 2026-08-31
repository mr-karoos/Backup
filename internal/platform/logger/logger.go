package logger

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
)

type contextKey string

const requestIDContextKey contextKey = "request_id"

// New creates a new structured JSON logger writing to the specified writer with the given minimum log level.
func New(levelStr string, w io.Writer) *slog.Logger {
	if w == nil {
		w = os.Stdout
	}

	var level slog.Level
	switch strings.ToLower(strings.TrimSpace(levelStr)) {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	case "info":
		fallthrough
	default:
		level = slog.LevelInfo
	}

	var sensitiveKeywords = []string{
		"password",
		"passwd",
		"token",
		"access_token",
		"refresh_token",
		"authorization",
		"cookie",
		"secret",
		"private_key",
		"api_key",
		"database_url",
		"dsn",
	}

	opts := &slog.HandlerOptions{
		Level: level,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			lowerKey := strings.ToLower(a.Key)
			for _, kw := range sensitiveKeywords {
				if strings.Contains(lowerKey, kw) {
					return slog.String(a.Key, "[REDACTED]")
				}
			}
			return a
		},
	}

	handler := slog.NewJSONHandler(w, opts)
	return slog.New(handler)
}

// WithRequestID stores the request ID into the given context.
func WithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, requestIDContextKey, requestID)
}

// RequestIDFromContext extracts the request ID from the context if present.
func RequestIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if val, ok := ctx.Value(requestIDContextKey).(string); ok {
		return val
	}
	return ""
}

// FromContext returns a logger enriched with context attributes such as request_id if available.
func FromContext(ctx context.Context, base *slog.Logger) *slog.Logger {
	if base == nil {
		base = slog.Default()
	}
	reqID := RequestIDFromContext(ctx)
	if reqID != "" {
		return base.With(slog.String("request_id", reqID))
	}
	return base
}
