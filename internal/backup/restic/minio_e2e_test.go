package restic

import (
	"context"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"backup-platform/internal/backup/domain"
	"backup-platform/pkg/uuid"
)

func TestResticRunner_MinIO_Live_E2E(t *testing.T) {
	resticBin := os.Getenv("RESTIC_BINARY_PATH")
	minioEndpoint := os.Getenv("TEST_MINIO_ENDPOINT")
	minioBucket := os.Getenv("TEST_MINIO_BUCKET")
	minioAccessKey := os.Getenv("TEST_MINIO_ACCESS_KEY")
	minioSecretKey := os.Getenv("TEST_MINIO_SECRET_KEY")

	if resticBin == "" || minioEndpoint == "" || minioBucket == "" || minioAccessKey == "" || minioSecretKey == "" {
		t.Skip("skipping MinIO live integration test: TEST_MINIO_* or RESTIC_BINARY_PATH not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	orgID := uuid.New()
	resourceID := uuid.New()
	password := []byte("super-secure-restic-pw-9988!")

	u, err := url.Parse(minioEndpoint)
	if err != nil {
		t.Fatalf("failed parsing minio endpoint URL: %v", err)
	}

	allowInsecure := (u.Scheme == "http")
	hostOnly := u.Hostname()

	s3Cfg := domain.S3TargetConfig{
		Bucket:   minioBucket,
		Region:   "us-east-1",
		Endpoint: minioEndpoint,
	}

	newValidTarget := func() *S3RepositoryTarget {
		t.Helper()
		tgt, err := NewS3RepositoryTarget(
			"s3_compatible",
			s3Cfg,
			orgID,
			resourceID,
			minioAccessKey,
			minioSecretKey,
			nil,
			allowInsecure,
			[]string{hostOnly},
		)
		if err != nil {
			t.Fatalf("failed creating s3 repository target: %v", err)
		}
		return tgt
	}

	// 1. Create S3RepositoryTarget through platform and verify namespace & proxy env
	initialTarget := newValidTarget()
	expectedLocator := "organizations/" + orgID.String() + "/resources/" + resourceID.String() + "/restic"
	if initialTarget.Locator() != expectedLocator {
		t.Errorf("expected locator %q, got %q", expectedLocator, initialTarget.Locator())
	}

	env := initialTarget.Env()
	hasProxy := false
	for _, e := range env {
		if strings.HasPrefix(e, "HTTP_PROXY=http://127.0.0.1:") {
			hasProxy = true
			break
		}
	}
	if !hasProxy {
		t.Fatalf("SECURITY FLAW: S3RepositoryTarget missing SecureResticProxy env")
	}
	initialTarget.Cleanup()

	runner := NewResticRunner(resticBin, nil)

	// 2. Verify exact version
	t.Log("Step 2: Validating restic version...")
	if err := runner.ValidateVersion(ctx); err != nil {
		t.Fatalf("failed validating restic version: %v", err)
	}

	// 3. Test real init succeeds
	t.Log("Step 3: Initializing repository...")
	initTarget := newValidTarget()
	if err := runner.Init(ctx, initTarget, password); err != nil {
		t.Fatalf("real restic init failed against MinIO: %v", err)
	}

	// 4. Test real probe succeeds
	t.Log("Step 4: Probing repository...")
	probeTarget := newValidTarget()
	if err := runner.Probe(ctx, probeTarget, password); err != nil {
		t.Fatalf("real restic probe failed against MinIO: %v", err)
	}

	// 5. Test wrong Restic password fails and does NOT leak password
	t.Log("Step 5: Testing wrong password failure...")
	wrongPWTarget := newValidTarget()
	wrongPWErr := runner.Probe(ctx, wrongPWTarget, []byte("wrong-restic-password"))
	if wrongPWErr == nil {
		t.Errorf("expected probe to fail with wrong password, but succeeded")
	} else {
		errStr := wrongPWErr.Error()
		if strings.Contains(errStr, string(password)) {
			t.Errorf("SECURITY LEAK: error contains plaintext password: %s", errStr)
		}
		if strings.Contains(errStr, minioSecretKey) {
			t.Errorf("SECURITY LEAK: error contains plaintext S3 secret key: %s", errStr)
		}
	}

	// 6. Test wrong S3 credential fails and does NOT leak secrets
	t.Log("Step 6: Testing wrong S3 credential failure...")
	badTarget, err := NewS3RepositoryTarget(
		"s3_compatible",
		s3Cfg,
		orgID,
		resourceID,
		minioAccessKey,
		"InvalidSecretKey456",
		nil,
		allowInsecure,
		[]string{hostOnly},
	)
	if err != nil {
		t.Fatalf("failed creating bad target: %v", err)
	}

	badCtx, badCancel := context.WithTimeout(ctx, 4*time.Second)
	badCredErr := runner.Probe(badCtx, badTarget, password)
	badCancel()
	if badCredErr == nil {
		t.Errorf("expected probe to fail with bad S3 credentials, but succeeded")
	} else {
		errStr := badCredErr.Error()
		if strings.Contains(errStr, string(password)) {
			t.Errorf("SECURITY LEAK: error contains plaintext password: %s", errStr)
		}
		if strings.Contains(errStr, minioSecretKey) {
			t.Errorf("SECURITY LEAK: error contains plaintext S3 secret key: %s", errStr)
		}
	}

	// 7. Test ambient malicious proxy in host environment cannot bypass SecureResticProxy
	t.Log("Step 7: Testing ambient proxy isolation...")
	origHTTPProxy := os.Getenv("HTTP_PROXY")
	origHTTPSProxy := os.Getenv("HTTPS_PROXY")
	os.Setenv("HTTP_PROXY", "http://192.0.2.1:54321")
	os.Setenv("HTTPS_PROXY", "http://192.0.2.1:54321")
	defer func() {
		os.Setenv("HTTP_PROXY", origHTTPProxy)
		os.Setenv("HTTPS_PROXY", origHTTPSProxy)
	}()

	ambientTestTarget := newValidTarget()
	t.Logf("ambientTestTarget proxy env: %+v", ambientTestTarget.Env())
	if err := runner.Probe(ctx, ambientTestTarget, password); err != nil {
		t.Errorf("probe failed when ambient proxy set (ambient proxy bypassed or interfered): %v", err)
	}
	t.Log("Step 7 passed successfully!")
}
