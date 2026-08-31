package payload

import (
	"errors"
	"testing"
)

func TestEncodeDecodeV1(t *testing.T) {
	t.Run("Valid encode and decode with secret and passphrase", func(t *testing.T) {
		pass := "my-passphrase"
		bytes, err := EncodeV1("my-secret-key", &pass)
		if err != nil {
			t.Fatalf("encode failed: %v", err)
		}

		p, err := Decode(bytes)
		if err != nil {
			t.Fatalf("decode failed: %v", err)
		}
		if p.Version != 1 || p.Secret != "my-secret-key" || p.Passphrase == nil || *p.Passphrase != "my-passphrase" {
			t.Errorf("unexpected decoded payload: %+v", p)
		}
	})

	t.Run("Valid encode and decode without passphrase", func(t *testing.T) {
		bytes, err := EncodeV1("my-api-token", nil)
		if err != nil {
			t.Fatalf("encode failed: %v", err)
		}

		p, err := Decode(bytes)
		if err != nil {
			t.Fatalf("decode failed: %v", err)
		}
		if p.Version != 1 || p.Secret != "my-api-token" || p.Passphrase != nil {
			t.Errorf("unexpected decoded payload: %+v", p)
		}
	})

	t.Run("Reject empty string secret on encode", func(t *testing.T) {
		_, err := EncodeV1("", nil)
		if !errors.Is(err, ErrInvalidPayload) {
			t.Errorf("expected ErrInvalidPayload for empty string secret, got: %v", err)
		}
	})

	t.Run("Whitespace-only secret is preserved on encode and decode without trimming", func(t *testing.T) {
		bytes, err := EncodeV1("   ", nil)
		if err != nil {
			t.Fatalf("expected success for whitespace secret, got: %v", err)
		}

		p, err := Decode(bytes)
		if err != nil {
			t.Fatalf("decode failed: %v", err)
		}
		if p.Secret != "   " {
			t.Errorf("expected secret '   ', got: %q", p.Secret)
		}
	})

	t.Run("Leading and trailing spaces in secret are preserved verbatim", func(t *testing.T) {
		bytes, err := EncodeV1("  password  ", nil)
		if err != nil {
			t.Fatalf("expected success for secret with spaces, got: %v", err)
		}

		p, err := Decode(bytes)
		if err != nil {
			t.Fatalf("decode failed: %v", err)
		}
		if p.Secret != "  password  " {
			t.Errorf("expected secret '  password  ', got: %q", p.Secret)
		}
	})

	t.Run("Clear releases secret and passphrase references", func(t *testing.T) {
		pass := "pass"
		p := &PayloadV1{
			Version:    1,
			Secret:     "secret",
			Passphrase: &pass,
		}
		Clear(p)
		if p.Secret != "" {
			t.Errorf("expected empty secret after Clear, got: %q", p.Secret)
		}
		if p.Passphrase != nil {
			t.Errorf("expected nil passphrase after Clear, got: %v", p.Passphrase)
		}

		// Clear(nil) should not panic
		Clear(nil)
	})

	t.Run("Decode rejects empty payload bytes", func(t *testing.T) {
		_, err := Decode(nil)
		if !errors.Is(err, ErrInvalidPayload) {
			t.Errorf("expected ErrInvalidPayload for nil bytes, got: %v", err)
		}

		_, err = Decode([]byte(""))
		if !errors.Is(err, ErrInvalidPayload) {
			t.Errorf("expected ErrInvalidPayload for empty bytes, got: %v", err)
		}
	})

	t.Run("Decode rejects unsupported version", func(t *testing.T) {
		raw := []byte(`{"version":2,"secret":"some-secret"}`)
		_, err := Decode(raw)
		if !errors.Is(err, ErrUnsupportedVersion) {
			t.Errorf("expected ErrUnsupportedVersion, got: %v", err)
		}
	})

	t.Run("Decode rejects unknown fields", func(t *testing.T) {
		raw := []byte(`{"version":1,"secret":"some-secret","extra_field":"unexpected"}`)
		_, err := Decode(raw)
		if !errors.Is(err, ErrInvalidPayload) {
			t.Errorf("expected ErrInvalidPayload for unknown fields, got: %v", err)
		}
	})

	t.Run("Decode rejects trailing second JSON value", func(t *testing.T) {
		raw := []byte(`{"version":1,"secret":"first"} {"version":1,"secret":"second"}`)
		_, err := Decode(raw)
		if !errors.Is(err, ErrInvalidPayload) {
			t.Errorf("expected ErrInvalidPayload for trailing second object, got: %v", err)
		}
	})

	t.Run("Decode rejects trailing scalar value", func(t *testing.T) {
		raw := []byte(`{"version":1,"secret":"first"} 123`)
		_, err := Decode(raw)
		if !errors.Is(err, ErrInvalidPayload) {
			t.Errorf("expected ErrInvalidPayload for trailing scalar, got: %v", err)
		}
	})

	t.Run("Decode accepts trailing whitespace only", func(t *testing.T) {
		raw := []byte("{\"version\":1,\"secret\":\"first\"}   \n\t  ")
		p, err := Decode(raw)
		if err != nil {
			t.Fatalf("unexpected error for trailing whitespace: %v", err)
		}
		if p.Secret != "first" {
			t.Errorf("unexpected secret: %s", p.Secret)
		}
	})

	t.Run("Decode rejects malformed JSON", func(t *testing.T) {
		raw := []byte(`{not-json}`)
		_, err := Decode(raw)
		if !errors.Is(err, ErrInvalidPayload) {
			t.Errorf("expected ErrInvalidPayload for malformed JSON, got: %v", err)
		}
	})

	t.Run("Decode rejects empty secret in valid JSON", func(t *testing.T) {
		raw := []byte(`{"version":1,"secret":""}`)
		_, err := Decode(raw)
		if !errors.Is(err, ErrInvalidPayload) {
			t.Errorf("expected ErrInvalidPayload for empty secret string, got: %v", err)
		}
	})
}
