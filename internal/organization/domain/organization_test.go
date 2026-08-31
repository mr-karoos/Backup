package domain

import (
	"strings"
	"testing"
)

func TestOrganizationValidation(t *testing.T) {
	t.Run("valid names and slugs create active organization", func(t *testing.T) {
		validCases := []struct {
			name string
			slug string
		}{
			{"Acme Corp", "acme-corp"},
			{"Internal Organization", "internal"},
			{"سازمان فناوری و پشتیبان", "tech-backup"},
			{"A", "a"},
			{"Organization One", "org-one"},
			{"Test Enterprise System", "test-enterprise-system"},
		}

		for _, tc := range validCases {
			org, err := NewOrganization(tc.name, tc.slug, false)
			if err != nil {
				t.Errorf("expected success for name %q and slug %q, got error: %v", tc.name, tc.slug, err)
				continue
			}
			if org.Status != OrgStatusActive {
				t.Errorf("expected OrgStatusActive, got %s", org.Status)
			}
			if org.IsDefaultInternal {
				t.Errorf("expected IsDefaultInternal=false")
			}
			if string(org.Metadata) != "{}" {
				t.Errorf("expected default metadata {}, got: %s", org.Metadata)
			}
		}
	})

	t.Run("name validation", func(t *testing.T) {
		// Empty / whitespace
		_, err := NewOrganization("", "valid-slug", false)
		if err != ErrInvalidOrgName {
			t.Errorf("expected ErrInvalidOrgName for empty name, got: %v", err)
		}
		_, err = NewOrganization("   ", "valid-slug", false)
		if err != ErrInvalidOrgName {
			t.Errorf("expected ErrInvalidOrgName for whitespace name, got: %v", err)
		}

		// Unicode safe length bounds (> 100 runes)
		longName := strings.Repeat("ش", 101)
		_, err = NewOrganization(longName, "valid-slug", false)
		if err != ErrInvalidOrgName {
			t.Errorf("expected ErrInvalidOrgName for name > 100 runes, got: %v", err)
		}

		// 100 runes is valid
		exact100Name := strings.Repeat("ش", 100)
		org, err := NewOrganization(exact100Name, "valid-slug", false)
		if err != nil {
			t.Errorf("expected 100 runes name to be valid, got: %v", err)
		}
		if org == nil {
			t.Errorf("expected non-nil org")
		}
	})

	t.Run("slug validation", func(t *testing.T) {
		invalidSlugs := []string{
			"",
			"   ",
			"backup1",
			"123-backup",
			"acme-2",
			"a1",
			"1",
			"Acme Corp",
			"acme_corp",
			"-acme",
			"acme-",
			"acme--corp",
			"شرکت",
			"acme/corp",
			"acme.corp",
			"acme@corp",
			strings.Repeat("a", 101),
		}

		for _, s := range invalidSlugs {
			_, err := NewOrganization("Valid Name", s, false)
			if err != ErrInvalidOrgSlug {
				t.Errorf("expected ErrInvalidOrgSlug for slug %q, got: %v", s, err)
			}
		}

		validSlugs := []string{
			"a",
			"acme",
			"acme-corp",
			"internal",
			"internal-org",
			"a-b-c-d-e",
			strings.Repeat("a", 100),
		}

		for _, s := range validSlugs {
			org, err := NewOrganization("Valid Name", s, false)
			if err != nil {
				t.Errorf("expected slug %q to be valid, got: %v", s, err)
			}
			if org == nil {
				t.Errorf("expected non-nil org")
			}
		}

		// Uppercase normalization
		org, err := NewOrganization("Valid Name", "ACME-CORP", false)
		if err != nil {
			t.Fatalf("expected uppercase slug to normalize, got: %v", err)
		}
		if org.Slug != "acme-corp" {
			t.Errorf("expected normalized slug 'acme-corp', got: %s", org.Slug)
		}
	})

	t.Run("metadata validation", func(t *testing.T) {
		validMetadata := [][]byte{
			nil,
			[]byte(""),
			[]byte("   "),
			[]byte("{}"),
			[]byte(`{"plan": "standard", "max_resources": 10}`),
			[]byte(`{"tier": "enterprise"}`),
		}

		for _, m := range validMetadata {
			org, err := NewOrganizationWithMetadata("Valid Name", "valid-slug", m, false)
			if err != nil {
				t.Errorf("expected valid metadata %q to succeed, got error: %v", string(m), err)
			}
			if org == nil {
				t.Errorf("expected non-nil org")
			}
		}

		invalidMetadata := [][]byte{
			[]byte("null"),
			[]byte("123"),
			[]byte(`"string"`),
			[]byte(`["array"]`),
			[]byte(`{invalid-json}`),
			[]byte(`{"unclosed": `),
		}

		for _, m := range invalidMetadata {
			_, err := NewOrganizationWithMetadata("Valid Name", "valid-slug", m, false)
			if err != ErrInvalidMetadata {
				t.Errorf("expected ErrInvalidMetadata for metadata %q, got: %v", string(m), err)
			}
		}
	})
}
