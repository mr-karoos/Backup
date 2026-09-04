package restic

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"backup-platform/internal/backup/domain"
	"backup-platform/pkg/uuid"
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

func TestFilterCleanEnv_Proxies(t *testing.T) {
	env := []string{
		"PATH=/usr/bin",
		"HTTP_PROXY=http://malicious-proxy:8080",
		"HTTPS_PROXY=http://malicious-proxy:8080",
		"ALL_PROXY=socks5://malicious-proxy:1080",
		"NO_PROXY=internal.net",
		"http_proxy=http://malicious-proxy:8080",
		"https_proxy=http://malicious-proxy:8080",
		"SAFE_VAR=val",
	}

	filtered := filterCleanEnv(env)
	for _, e := range filtered {
		upper := strings.ToUpper(e)
		if strings.HasPrefix(upper, "HTTP_PROXY=") ||
			strings.HasPrefix(upper, "HTTPS_PROXY=") ||
			strings.HasPrefix(upper, "ALL_PROXY=") ||
			strings.HasPrefix(upper, "NO_PROXY=") {
			t.Errorf("filtered env still contains ambient proxy variable: %s", e)
		}
	}
}

func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	defer os.Exit(1)
	// Echo sensitive environment variables to stderr and exit with error
	pw := os.Getenv("RESTIC_PASSWORD")
	sec := os.Getenv("AWS_SECRET_ACCESS_KEY")
	_, _ = fmt.Fprintf(os.Stderr, "FATAL: authentication failed with password %s and secret %s\n", pw, sec)
}

func TestResticRunner_RedactionOrder(t *testing.T) {
	ctx := context.Background()

	pw := []byte("super-secret-restic-password-999")
	awsSecret := "super-secret-aws-key-888"

	// Construct an S3RepositoryTarget that wipes secrets on Cleanup()
	cfg := domain.S3TargetConfig{Bucket: "test-bucket"}
	target, err := NewS3RepositoryTarget(
		"s3",
		cfg,
		uuid.New(),
		uuid.New(),
		"AKIA12345",
		awsSecret,
		nil,
		false,
		nil,
	)
	if err != nil {
		t.Fatalf("failed creating target: %v", err)
	}

	// Use test binary helper process to simulate failing restic process echoing secrets
	runner := &ResticRunner{
		binaryPath: os.Args[0],
	}

	// Set helper process environment
	origEnv := os.Getenv("GO_WANT_HELPER_PROCESS")
	os.Setenv("GO_WANT_HELPER_PROCESS", "1")
	defer os.Setenv("GO_WANT_HELPER_PROCESS", origEnv)

	_, runErr := runner.runCommand(ctx, target, pw, []string{"-test.run=TestHelperProcess", "--"})
	if runErr == nil {
		t.Fatalf("expected command failure from helper process")
	}

	errStr := runErr.Error()

	// Assert NO plaintext password or secret appears in returned error
	if strings.Contains(errStr, string(pw)) {
		t.Errorf("SECURITY LEAK: error contains plaintext password: %s", errStr)
	}
	if strings.Contains(errStr, awsSecret) {
		t.Errorf("SECURITY LEAK: error contains plaintext AWS secret: %s", errStr)
	}

	// Assert redaction placeholders are present
	if !strings.Contains(errStr, "[REDACTED_PASSWORD]") {
		t.Errorf("expected [REDACTED_PASSWORD] in error, got: %s", errStr)
	}
	if !strings.Contains(errStr, "[REDACTED_SECRET]") {
		t.Errorf("expected [REDACTED_SECRET] in error, got: %s", errStr)
	}

	// Assert target.Cleanup() was called and zeroed the in-memory secret
	if len(target.secretAccessKey) != 0 {
		t.Errorf("expected target secretAccessKey to be zeroed after runner completes")
	}
}

func TestResticRunner_EnvironmentSeparation(t *testing.T) {
	orig := os.Getenv("RESTIC_BINARY_PATH")
	os.Setenv("RESTIC_BINARY_PATH", "/malicious/override/binary")
	defer os.Setenv("RESTIC_BINARY_PATH", orig)

	// NewResticRunner with empty path MUST default to /usr/local/bin/restic and NEVER read RESTIC_BINARY_PATH
	runner := NewResticRunner("", nil)
	if runner.binaryPath != "/usr/local/bin/restic" {
		t.Fatalf("SECURITY FLAW: NewResticRunner inspected RESTIC_BINARY_PATH internally: got %q, expected /usr/local/bin/restic", runner.binaryPath)
	}

	// Caller may explicitly supply a path
	custom := NewResticRunner("/opt/custom/restic", nil)
	if custom.binaryPath != "/opt/custom/restic" {
		t.Fatalf("expected explicit path to be retained, got %q", custom.binaryPath)
	}
}

func TestResticRunner_ValidateVersion(t *testing.T) {
	ctx := context.Background()

	t.Run("rejects non-existent binary", func(t *testing.T) {
		runner := NewResticRunner("/non/existent/path/to/restic-binary", nil)
		err := runner.ValidateVersion(ctx)
		if err == nil {
			t.Errorf("expected error for non-existent binary")
		}
	})

	t.Run("parseAndValidateResticVersion exact matching", func(t *testing.T) {
		validCases := []string{
			"restic 0.19.1",
			"restic 0.19.1 compiled with go1.24.0 on linux/amd64",
			"restic 0.19.1 compiled with go1.26.4 on windows/amd64",
		}
		for _, vc := range validCases {
			if err := parseAndValidateResticVersion(vc); err != nil {
				t.Errorf("expected %q to be accepted, got error: %v", vc, err)
			}
		}

		invalidCases := []string{
			"restic 0.19.0 compiled with go1.24.0 on linux/amd64",
			"restic 0.19.10 compiled with go1.24.0 on linux/amd64",
			"restic 1.0.0 compiled with go1.24.0 on linux/amd64",
			"restic 0.18.0",
			"restic 0.19.2",
			"other 0.19.1",
			"",
			"   ",
			"restic",
		}
		for _, ic := range invalidCases {
			if err := parseAndValidateResticVersion(ic); err == nil {
				t.Errorf("expected %q to be rejected, but was accepted", ic)
			}
		}
	})
}
