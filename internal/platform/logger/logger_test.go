package logger

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

func TestLogger_RedactionOfSensitiveKeys(t *testing.T) {
	tests := []struct {
		key         string
		value       string
		shouldHide  bool
		description string
	}{
		{key: "password", value: "super-secret-password-123", shouldHide: true, description: "exact password key"},
		{key: "Password", value: "CapitalPasswordValue", shouldHide: true, description: "case-insensitive Password"},
		{key: "db_password", value: "db-secret-value", shouldHide: true, description: "compound password key"},
		{key: "authorization", value: "Bearer secret-jwt-token", shouldHide: true, description: "authorization header"},
		{key: "refresh_token", value: "opaque-refresh-token-xyz", shouldHide: true, description: "refresh token"},
		{key: "database_url", value: "postgres://user:pass@host:5432/db", shouldHide: true, description: "database url"},
		{key: "dsn", value: "postgres://secret-dsn", shouldHide: true, description: "dsn parameter"},
		{key: "secret", value: "my-master-secret", shouldHide: true, description: "secret key"},
		{key: "api_key", value: "cpanel-api-token-999", shouldHide: true, description: "api key"},
		{key: "cookie", value: "session=secretcookieval", shouldHide: true, description: "cookie header"},
		{key: "request_id", value: "req-123456", shouldHide: false, description: "standard request_id"},
		{key: "status", value: "ok", shouldHide: false, description: "status field"},
		{key: "path", value: "/api/v1/health", shouldHide: false, description: "path field"},
		{key: "user_id", value: "usr_abcd1234", shouldHide: false, description: "user_id field"},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			var buf bytes.Buffer
			log := New("info", &buf)

			log.Info("test log message", slog.String(tt.key, tt.value))

			output := buf.String()

			var parsed map[string]interface{}
			if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
				t.Fatalf("failed to parse JSON log output: %v, raw: %s", err, output)
			}

			val, exists := parsed[tt.key]
			if !exists {
				t.Fatalf("expected key %s in log output, but it was missing: %s", tt.key, output)
			}

			valStr, ok := val.(string)
			if !ok {
				t.Fatalf("expected string value for key %s, got: %v", tt.key, val)
			}

			if tt.shouldHide {
				if valStr != "[REDACTED]" {
					t.Errorf("expected key %s to be '[REDACTED]', got: '%s'", tt.key, valStr)
				}
				if strings.Contains(output, tt.value) {
					t.Errorf("sensitive value '%s' leaked in raw log output: %s", tt.value, output)
				}
			} else {
				if valStr != tt.value {
					t.Errorf("expected non-sensitive key %s to retain value '%s', got: '%s'", tt.key, tt.value, valStr)
				}
			}
		})
	}
}

func TestLogger_FromContextEnrichment(t *testing.T) {
	var buf bytes.Buffer
	baseLog := New("info", &buf)

	ctx := WithRequestID(context.Background(), "req-abc-789")
	reqLog := FromContext(ctx, baseLog)

	reqLog.Info("request event")

	var parsed map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("failed to parse JSON log output: %v", err)
	}

	if parsed["request_id"] != "req-abc-789" {
		t.Errorf("expected request_id 'req-abc-789', got: %v", parsed["request_id"])
	}
}
