package domain

import (
	"strings"
	"testing"
)

func TestValidatePlanName(t *testing.T) {
	validCases := []struct {
		name     string
		input    string
		expected string
	}{
		{name: "Simple ASCII", input: "Daily Production Backup", expected: "Daily Production Backup"},
		{name: "With leading and trailing whitespace", input: "  Nightly Backup  ", expected: "Nightly Backup"},
		{name: "Unicode Persian", input: "پشتیبان‌گیری روزانه دیتابیس", expected: "پشتیبان‌گیری روزانه دیتابیس"},
		{name: "Exact 100 runes", input: strings.Repeat("a", 100), expected: strings.Repeat("a", 100)},
	}

	for _, tc := range validCases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := ValidatePlanName(tc.input)
			if err != nil {
				t.Fatalf("expected valid plan name for %q, got error: %v", tc.input, err)
			}
			if res != tc.expected {
				t.Fatalf("expected %q, got %q", tc.expected, res)
			}
		})
	}

	invalidCases := []struct {
		name  string
		input string
	}{
		{name: "Empty string", input: ""},
		{name: "Whitespace only", input: "    \t\n  "},
		{name: "Over 100 runes", input: strings.Repeat("a", 101)},
		{name: "Contains NUL byte", input: "Plan\x00Name"},
		{name: "Contains control character", input: "Plan\x07Name"},
		{name: "Invalid UTF-8", input: "Plan\xff\xfeName"},
	}

	for _, tc := range invalidCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ValidatePlanName(tc.input)
			if err == nil {
				t.Fatalf("expected error for invalid plan name %q, got nil", tc.input)
			}
		})
	}
}

func TestValidateRetentionPolicy(t *testing.T) {
	t.Run("nil counts and days are valid", func(t *testing.T) {
		if err := ValidateRetentionPolicy(nil, nil); err != nil {
			t.Fatalf("expected nil error for nil retention, got: %v", err)
		}
	})

	t.Run("positive count and days are valid", func(t *testing.T) {
		count := 7
		days := 30
		if err := ValidateRetentionPolicy(&count, &days); err != nil {
			t.Fatalf("expected nil error for positive retention, got: %v", err)
		}
	})

	t.Run("zero count is invalid", func(t *testing.T) {
		count := 0
		if err := ValidateRetentionPolicy(&count, nil); err == nil {
			t.Fatalf("expected error for zero count, got nil")
		}
	})

	t.Run("negative count is invalid", func(t *testing.T) {
		count := -1
		if err := ValidateRetentionPolicy(&count, nil); err == nil {
			t.Fatalf("expected error for negative count, got nil")
		}
	})

	t.Run("zero days is invalid", func(t *testing.T) {
		days := 0
		if err := ValidateRetentionPolicy(nil, &days); err == nil {
			t.Fatalf("expected error for zero days, got nil")
		}
	})

	t.Run("negative days is invalid", func(t *testing.T) {
		days := -5
		if err := ValidateRetentionPolicy(nil, &days); err == nil {
			t.Fatalf("expected error for negative days, got nil")
		}
	})
}
