package restic

import (
	"strings"
	"testing"
)

func TestFilterCleanEnv(t *testing.T) {
	env := []string{
		"PATH=/usr/bin",
		"RESTIC_PASSWORD=leaked_pw",
		"RESTIC_REPOSITORY=/tmp/repo",
		"AWS_ACCESS_KEY_ID=AKIA123",
		"AWS_SECRET_ACCESS_KEY=secret456",
		"HOME=/home/user",
		"aws_default_region=us-east-1",
	}

	filtered := filterCleanEnv(env)

	for _, e := range filtered {
		if strings.HasPrefix(strings.ToUpper(e), "RESTIC_") {
			t.Errorf("filtered env still contains restic variable: %s", e)
		}
		if strings.HasPrefix(strings.ToUpper(e), "AWS_") {
			t.Errorf("filtered env still contains aws variable: %s", e)
		}
	}

	foundPath := false
	foundHome := false
	for _, e := range filtered {
		if strings.HasPrefix(e, "PATH=") {
			foundPath = true
		}
		if strings.HasPrefix(e, "HOME=") {
			foundHome = true
		}
	}

	if !foundPath || !foundHome {
		t.Errorf("expected PATH and HOME to be retained in filtered env")
	}
}

func TestSanitizeSecrets(t *testing.T) {
	password := "superSecretResticPassword123!"
	awsKey := "AKIAIOSFODNN7EXAMPLE"
	awsSecret := "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"

	targetEnv := []string{
		"AWS_ACCESS_KEY_ID=" + awsKey,
		"AWS_SECRET_ACCESS_KEY=" + awsSecret,
	}

	rawOutput := "Fatal: unable to open config: invalid password " + password + " for key " + awsSecret

	sanitized := sanitizeSecrets(rawOutput, password, targetEnv)

	if strings.Contains(sanitized, password) {
		t.Errorf("SECURITY LEAK: password present in sanitized output: %s", sanitized)
	}
	if strings.Contains(sanitized, awsSecret) {
		t.Errorf("SECURITY LEAK: awsSecret present in sanitized output: %s", sanitized)
	}
	if !strings.Contains(sanitized, "[REDACTED_PASSWORD]") {
		t.Errorf("expected [REDACTED_PASSWORD] placeholder, got: %s", sanitized)
	}
	if !strings.Contains(sanitized, "[REDACTED_SECRET]") {
		t.Errorf("expected [REDACTED_SECRET] placeholder, got: %s", sanitized)
	}
}

func TestBoundedBuffer(t *testing.T) {
	buf := newBoundedBuffer(50)

	chunk1 := []byte("12345678901234567890") // 20 bytes
	chunk2 := []byte("12345678901234567890") // 20 bytes
	chunk3 := []byte("12345678901234567890") // 20 bytes -> total 60 bytes, should be capped at 50

	n, err := buf.Write(chunk1)
	if err != nil || n != 20 {
		t.Fatalf("unexpected write 1 result: n=%d err=%v", n, err)
	}
	n, err = buf.Write(chunk2)
	if err != nil || n != 20 {
		t.Fatalf("unexpected write 2 result: n=%d err=%v", n, err)
	}
	n, err = buf.Write(chunk3)
	if err != nil || n != 20 {
		t.Fatalf("unexpected write 3 result: n=%d err=%v", n, err)
	}

	if buf.buf.Len() != 50 {
		t.Errorf("expected buffer length capped at 50, got %d", buf.buf.Len())
	}
}
