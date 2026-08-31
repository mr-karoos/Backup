package uuid

import (
	"encoding/json"
	"testing"
)

func TestUUID_GenerateAndParse(t *testing.T) {
	u1 := New()
	str := u1.String()

	if len(str) != 36 {
		t.Fatalf("expected 36 chars, got: %s (len: %d)", str, len(str))
	}

	// Verify RFC 4122 v4 version and variant bits
	if str[14] != '4' {
		t.Errorf("expected version 4 at character 14, got: %c", str[14])
	}
	variant := str[19]
	if variant != '8' && variant != '9' && variant != 'a' && variant != 'b' {
		t.Errorf("expected RFC 4122 variant (8, 9, a, b) at character 19, got: %c", variant)
	}

	u2, err := Parse(str)
	if err != nil {
		t.Fatalf("failed to parse generated UUID string: %v", err)
	}

	if u1 != u2 {
		t.Errorf("expected u1 == u2, got %v != %v", u1, u2)
	}
}

func TestUUID_ParseInvalid(t *testing.T) {
	invalidCases := []string{
		"",
		"not-a-uuid",
		"12345678-1234-1234-1234-12345678901",   // 35 chars
		"12345678-1234-1234-1234-1234567890123", // 37 chars
		"12345678_1234_1234_1234_123456789012",  // underscore instead of hyphen
		"zzzzzzzz-zzzz-zzzz-zzzz-zzzzzzzzzzzz",  // invalid hex
	}

	for _, tc := range invalidCases {
		t.Run("invalid:"+tc, func(t *testing.T) {
			_, err := Parse(tc)
			if err == nil {
				t.Fatalf("expected error for invalid UUID '%s', got nil", tc)
			}
		})
	}
}

func TestUUID_SQLDriverValuerAndScanner(t *testing.T) {
	u := New()
	val, err := u.Value()
	if err != nil {
		t.Fatalf("failed to get driver.Value: %v", err)
	}

	strVal, ok := val.(string)
	if !ok || strVal != u.String() {
		t.Errorf("expected driver.Value to return string matching UUID, got: %v", val)
	}

	var scanned UUID
	if err := scanned.Scan(strVal); err != nil {
		t.Fatalf("failed to scan UUID from string: %v", err)
	}
	if scanned != u {
		t.Errorf("scanned UUID %v does not match original %v", scanned, u)
	}

	// Scan from []byte
	var scannedBytes UUID
	if err := scannedBytes.Scan([]byte(strVal)); err != nil {
		t.Fatalf("failed to scan UUID from []byte: %v", err)
	}
	if scannedBytes != u {
		t.Errorf("scanned UUID %v does not match original %v", scannedBytes, u)
	}

	// Scan nil
	var nilScanned UUID
	if err := nilScanned.Scan(nil); err != nil {
		t.Fatalf("failed to scan nil: %v", err)
	}
	if nilScanned != Nil {
		t.Errorf("expected Nil UUID on scanning nil, got: %v", nilScanned)
	}
}

func TestUUID_JSONMarshaling(t *testing.T) {
	type DTO struct {
		ID UUID `json:"id"`
	}

	original := DTO{ID: New()}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("failed to marshal struct with UUID: %v", err)
	}

	var decoded DTO
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal JSON into struct with UUID: %v", err)
	}

	if decoded.ID != original.ID {
		t.Errorf("decoded ID %v does not match original %v", decoded.ID, original.ID)
	}
}
