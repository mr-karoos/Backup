package domain

import (
	"errors"
	"strings"
	"testing"
)

func TestType_Validation(t *testing.T) {
	validTypes := []Type{
		TypeSSHPrivateKey,
		TypeSSHPassword,
		TypeCPanelAPIToken,
		TypeCPanelPassword,
	}

	for _, vt := range validTypes {
		if !vt.IsValid() {
			t.Errorf("expected type %s to be valid", vt)
		}
		if err := ValidateType(vt); err != nil {
			t.Errorf("expected ValidateType(%s) to be nil, got: %v", vt, err)
		}
	}

	invalidTypes := []Type{
		"",
		"aws_iam",
		"ssh_key",
		"invalid_type",
		"CPANEL_PASSWORD",
	}

	for _, it := range invalidTypes {
		if it.IsValid() {
			t.Errorf("expected type %s to be invalid", it)
		}
		if err := ValidateType(it); !errors.Is(err, ErrInvalidCredentialType) {
			t.Errorf("expected ErrInvalidCredentialType for %s, got: %v", it, err)
		}
	}
}

func TestValidateName(t *testing.T) {
	t.Run("valid names return trimmed string", func(t *testing.T) {
		validCases := []struct {
			input    string
			expected string
		}{
			{"Production SSH Key", "Production SSH Key"},
			{"  Staging Key  ", "Staging Key"},
			{"کلید اختصاصی سرور اصلی", "کلید اختصاصی سرور اصلی"},
			{"cPanel-API-Token-2026", "cPanel-API-Token-2026"},
			{"a", "a"},
			{strings.Repeat("ک", 100), strings.Repeat("ک", 100)},
		}

		for _, tc := range validCases {
			res, err := ValidateName(tc.input)
			if err != nil {
				t.Errorf("expected valid for %q, got error: %v", tc.input, err)
			}
			if res != tc.expected {
				t.Errorf("expected %q, got %q", tc.expected, res)
			}
		}
	})

	t.Run("invalid names return ErrInvalidCredentialName", func(t *testing.T) {
		invalidCases := []string{
			"",
			"   ",
			"\t\n",
			strings.Repeat("x", 101),
			strings.Repeat("گ", 101),
		}

		for _, ic := range invalidCases {
			_, err := ValidateName(ic)
			if !errors.Is(err, ErrInvalidCredentialName) {
				t.Errorf("expected ErrInvalidCredentialName for %q, got: %v", ic, err)
			}
		}
	})
}
