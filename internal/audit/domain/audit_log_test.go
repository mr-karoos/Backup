package domain

import (
	"testing"
	"time"

	"backup-platform/pkg/uuid"
)

func TestAuditCursor_Roundtrip(t *testing.T) {
	id := uuid.New()
	ts := time.Date(2026, 9, 8, 14, 0, 0, 123456789, time.UTC)

	cursor := EncodeAuditCursor(ts, id)
	if cursor == "" {
		t.Fatalf("expected non-empty cursor string")
	}

	decodedTime, decodedID, err := DecodeAuditCursor(cursor)
	if err != nil {
		t.Fatalf("failed decoding valid cursor: %v", err)
	}

	if decodedID != id {
		t.Fatalf("expected id %s, got %s", id, decodedID)
	}
	if !decodedTime.Equal(ts) {
		t.Fatalf("expected time %v, got %v", ts, decodedTime)
	}
}

func TestDecodeAuditCursor_Rejections(t *testing.T) {
	id := uuid.New()
	ts := time.Date(2026, 9, 8, 14, 0, 0, 123456789, time.UTC)
	validCanonical := EncodeAuditCursor(ts, id)

	testCases := []struct {
		name   string
		cursor string
	}{
		{"empty cursor", ""},
		{"garbage non-base64", "not-base64-!@#$%^&*()"},
		{"padded base64 with equals", validCanonical + "="},
		{"standard base64 characters with plus", "MjAyNi0wOS0wOFQxNDowMDowMFosNTUwZTg0MDAtZTI5Yi00MWQ0LWE3MTYtNDQ2NjU1NDQwMDAw+abc"},
		{"standard base64 characters with slash", "MjAyNi0wOS0wOFQxNDowMDowMFosNTUwZTg0MDAtZTI5Yi00MWQ0LWE3MTYtNDQ2NjU1NDQwMDAw/abc"},
		{"whitespace included", validCanonical + " "},
		{"extra component", EncodeAuditCursor(ts, id) + "extra"},
		{"bad timestamp", "bm90LWEtdGltZXN0YW1wLDU1MGU4NDAwLWUyOWItNDFkNC1hNzE2LTQ0NjY1NTQ0MDAwMA"},
		{"bad UUID", "MjAyNi0wOS0wOFQxNDowMDowMFosbm90LWEtdXVpZA"},
		{"zero UUID", "MjAyNi0wOS0wOFQxNDowMDowMFosMDAwMDAwMDAtMDAwMC0wMDAwLTAwMDAtMDAwMDAwMDAwMDAw"},
		{"canonical cursor with trailing characters", validCanonical + "trailing"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := DecodeAuditCursor(tc.cursor)
			if err == nil {
				t.Fatalf("expected error for %s (%q), got nil", tc.name, tc.cursor)
			}
		})
	}
}
