package restic

import (
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"backup-platform/internal/backup/domain"
	"backup-platform/pkg/uuid"
)

func TestLocalRepositoryTarget(t *testing.T) {
	tempDir := t.TempDir()
	orgID := uuid.New()
	resourceID := uuid.New()

	t.Run("successfully creates target with correct locator and permissions", func(t *testing.T) {
		target, err := NewLocalRepositoryTarget(tempDir, orgID, resourceID)
		if err != nil {
			t.Fatalf("expected success, got: %v", err)
		}

		expectedLocator := filepath.ToSlash(filepath.Join("repositories", "organizations", orgID.String(), "resources", resourceID.String(), "restic"))
		if target.Locator() != expectedLocator {
			t.Errorf("expected locator %s, got: %s", expectedLocator, target.Locator())
		}

		expectedPath := filepath.Join(tempDir, "repositories", "organizations", orgID.String(), "resources", resourceID.String(), "restic")
		if target.ResticRepositoryURL() != expectedPath {
			t.Errorf("expected url %s, got: %s", expectedPath, target.ResticRepositoryURL())
		}

		fi, err := os.Stat(expectedPath)
		if err != nil {
			t.Fatalf("expected directory to exist: %v", err)
		}
		if !fi.IsDir() {
			t.Errorf("expected directory")
		}
	})

	t.Run("rejects nil organization ID or resource ID", func(t *testing.T) {
		_, err := NewLocalRepositoryTarget(tempDir, uuid.Nil, resourceID)
		if err == nil {
			t.Errorf("expected error for nil org ID")
		}
		_, err = NewLocalRepositoryTarget(tempDir, orgID, uuid.Nil)
		if err == nil {
			t.Errorf("expected error for nil resource ID")
		}
	})

	t.Run("enforces 0700 permissions on pre-existing permissive directory", func(t *testing.T) {
		preDir := filepath.Join(tempDir, "repositories", "organizations", orgID.String(), "resources", resourceID.String(), "restic")
		if err := os.MkdirAll(preDir, 0777); err != nil {
			t.Fatalf("failed creating preDir: %v", err)
		}
		_ = os.Chmod(preDir, 0777)

		target, err := NewLocalRepositoryTarget(tempDir, orgID, resourceID)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		fi, err := os.Stat(target.ResticRepositoryURL())
		if err != nil {
			t.Fatalf("failed stat: %v", err)
		}
		// On platforms that support POSIX permissions (e.g. Linux production), check mode is 0700
		if runtime.GOOS != "windows" {
			if fi.Mode().Perm()&0077 != 0 {
				t.Errorf("expected 0700 permissions, got %o", fi.Mode().Perm())
			}
		}
	})
}

func TestS3RepositoryTarget(t *testing.T) {
	orgID := uuid.New()
	resourceID := uuid.New()

	t.Run("creates standard AWS S3 target with scoped namespace", func(t *testing.T) {
		cfg := domain.S3TargetConfig{
			Bucket: "company-backups",
			Region: "us-west-2",
		}

		target, err := NewS3RepositoryTarget(
			"s3",
			cfg,
			orgID,
			resourceID,
			"AKIAIOSFODNN7EXAMPLE",
			"wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
			nil,
			false,
			nil,
		)
		if err != nil {
			t.Fatalf("expected success, got: %v", err)
		}

		expectedLocator := "organizations/" + orgID.String() + "/resources/" + resourceID.String() + "/restic"
		if target.Locator() != expectedLocator {
			t.Errorf("expected locator %s, got: %s", expectedLocator, target.Locator())
		}

		expectedURL := "s3:https://s3.us-west-2.amazonaws.com/company-backups/" + expectedLocator
		if target.ResticRepositoryURL() != expectedURL {
			t.Errorf("expected url %s, got: %s", expectedURL, target.ResticRepositoryURL())
		}

		env := target.Env()
		foundKey := false
		foundSecret := false
		for _, e := range env {
			if strings.HasPrefix(e, "AWS_ACCESS_KEY_ID=") {
				foundKey = true
			}
			if strings.HasPrefix(e, "AWS_SECRET_ACCESS_KEY=") {
				foundSecret = true
			}
		}
		if !foundKey || !foundSecret {
			t.Errorf("missing AWS env credentials in %+v", env)
		}

		// Verify that standard AWS target also receives SecureResticProxy environment
		hasProxy := false
		for _, e := range env {
			if strings.HasPrefix(e, "HTTP_PROXY=http://127.0.0.1:") {
				hasProxy = true
			}
		}
		if !hasProxy {
			t.Errorf("SECURITY DEFECT: standard AWS target missing SecureResticProxy environment: %+v", env)
		}

		// Verify cleanup zeroes secret and closes proxy
		target.Cleanup()
		if len(target.secretAccessKey) != 0 {
			t.Errorf("expected secretAccessKey to be cleared after cleanup")
		}
		if target.proxy != nil {
			t.Errorf("expected proxy to be cleared after cleanup")
		}
	})

	t.Run("creates S3-compatible target with custom endpoint and base prefix", func(t *testing.T) {
		cfg := domain.S3TargetConfig{
			Bucket:   "minio-bucket",
			Region:   "us-east-1",
			Endpoint: "https://127.0.0.1:9000",
			Prefix:   "cluster-prod",
		}

		target, err := NewS3RepositoryTarget(
			"s3_compatible",
			cfg,
			orgID,
			resourceID,
			"MYACCESSKEY",
			"MYSECRETKEY",
			nil,
			true,
			[]string{"127.0.0.1"},
		)
		if err != nil {
			t.Fatalf("expected success, got: %v", err)
		}
		defer target.Cleanup()

		expectedLocator := "cluster-prod/organizations/" + orgID.String() + "/resources/" + resourceID.String() + "/restic"
		if target.Locator() != expectedLocator {
			t.Errorf("expected locator %s, got: %s", expectedLocator, target.Locator())
		}

		expectedURL := "s3:https://127.0.0.1:9000/minio-bucket/" + expectedLocator
		if target.ResticRepositoryURL() != expectedURL {
			t.Errorf("expected url %s, got: %s", expectedURL, target.ResticRepositoryURL())
		}

		// Verify proxy env is injected
		env := target.Env()
		foundProxy := false
		for _, e := range env {
			if strings.HasPrefix(e, "HTTPS_PROXY=http://127.0.0.1:") {
				foundProxy = true
				break
			}
		}
		if !foundProxy {
			t.Errorf("expected HTTPS_PROXY in target.Env(), got %v", env)
		}
	})

	t.Run("rejects path traversal in prefix", func(t *testing.T) {
		cfg := domain.S3TargetConfig{
			Bucket: "my-bucket",
			Region: "us-east-1",
			Prefix: "../escape",
		}

		_, err := NewS3RepositoryTarget(
			"s3",
			cfg,
			orgID,
			resourceID,
			"KEY",
			"SECRET",
			nil,
			false,
			nil,
		)
		if err == nil {
			t.Errorf("expected error for path traversal in prefix")
		}
	})

	t.Run("rejects path-free link-local https", func(t *testing.T) {
		cfg := domain.S3TargetConfig{
			Bucket:   "my-bucket",
			Region:   "us-east-1",
			Endpoint: "https://169.254.169.254",
		}

		_, err := NewS3RepositoryTarget("s3", cfg, orgID, resourceID, "KEY", "SECRET", nil, false, nil)
		if err == nil {
			t.Errorf("expected error for path-free link-local https")
		}
	})

	t.Run("rejects path-free link-local http even with insecure mode enabled", func(t *testing.T) {
		cfg := domain.S3TargetConfig{
			Bucket:   "my-bucket",
			Region:   "us-east-1",
			Endpoint: "http://169.254.169.254",
		}

		_, err := NewS3RepositoryTarget("s3", cfg, orgID, resourceID, "KEY", "SECRET", nil, true, nil)
		if err == nil {
			t.Errorf("expected error for path-free link-local http with insecure enabled")
		}
	})

	t.Run("rejects path-free IPv6 link-local", func(t *testing.T) {
		cfg := domain.S3TargetConfig{
			Bucket:   "my-bucket",
			Region:   "us-east-1",
			Endpoint: "https://[fe80::1]",
		}

		_, err := NewS3RepositoryTarget("s3", cfg, orgID, resourceID, "KEY", "SECRET", nil, false, nil)
		if err == nil {
			t.Errorf("expected error for IPv6 link-local")
		}
	})

	t.Run("rejects multicast and unspecified addresses", func(t *testing.T) {
		for _, ep := range []string{"https://224.0.0.1", "https://[::]"} {
			cfg := domain.S3TargetConfig{Bucket: "my-bucket", Endpoint: ep}
			_, err := NewS3RepositoryTarget("s3", cfg, orgID, resourceID, "KEY", "SECRET", nil, false, nil)
			if err == nil {
				t.Errorf("expected error for multicast/unspecified endpoint %s", ep)
			}
		}
	})

	t.Run("rejects private IPv4 without explicit allowlist", func(t *testing.T) {
		cfg := domain.S3TargetConfig{
			Bucket:   "my-bucket",
			Region:   "us-east-1",
			Endpoint: "https://10.0.0.1",
		}

		_, err := NewS3RepositoryTarget("s3", cfg, orgID, resourceID, "KEY", "SECRET", nil, false, nil)
		if err == nil {
			t.Errorf("expected error for un-allowlisted private IPv4")
		}
	})

	t.Run("accepts explicitly allowlisted private IPv4 endpoint", func(t *testing.T) {
		cfg := domain.S3TargetConfig{
			Bucket:   "my-bucket",
			Region:   "us-east-1",
			Endpoint: "https://10.0.0.1:9000",
		}

		target, err := NewS3RepositoryTarget("s3", cfg, orgID, resourceID, "KEY", "SECRET", nil, false, []string{"10.0.0.1"})
		if err != nil {
			t.Fatalf("expected success for allowlisted private IPv4 endpoint, got: %v", err)
		}
		target.Cleanup()
	})

	t.Run("rejects malicious Region injection attempts", func(t *testing.T) {
		maliciousRegions := []string{
			"us-east-1.amazonaws.com@169.254.169.254/",
			"us-east-1/escape",
			`us-east-1\escape`,
			"us-east-1:8080",
			"us-east-1?param=1",
			"us-east-1#frag",
			"[::1]",
			"us%20east",
			"us-east-1\x00",
			"US-EAST-1",
		}

		for _, reg := range maliciousRegions {
			cfg := domain.S3TargetConfig{
				Bucket: "my-bucket",
				Region: reg,
			}
			_, err := NewS3RepositoryTarget("s3", cfg, orgID, resourceID, "KEY", "SECRET", nil, false, nil)
			if err == nil {
				t.Errorf("SECURITY DEFECT: expected region %q to be rejected, but was accepted", reg)
			}
		}
	})

	t.Run("proves no URL delimiters in Region can produce userinfo, alternate host, query, or fragment", func(t *testing.T) {
		cfg := domain.S3TargetConfig{
			Bucket: "valid-bucket",
			Region: "eu-central-1",
		}
		target, err := NewS3RepositoryTarget("s3", cfg, orgID, resourceID, "KEY", "SECRET", nil, false, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer target.Cleanup()

		rawURL := target.ResticRepositoryURL()
		if !strings.HasPrefix(rawURL, "s3:") {
			t.Fatalf("expected s3: prefix on url: %s", rawURL)
		}

		cleanURL := strings.TrimPrefix(rawURL, "s3:")
		u, err := url.Parse(cleanURL)
		if err != nil {
			t.Fatalf("failed parsing generated url: %v", err)
		}

		if u.User != nil {
			t.Errorf("SECURITY DEFECT: generated URL contains userinfo: %v", u.User)
		}
		if u.Host != "s3.eu-central-1.amazonaws.com" {
			t.Errorf("expected host s3.eu-central-1.amazonaws.com, got: %s", u.Host)
		}
		if u.RawQuery != "" {
			t.Errorf("SECURITY DEFECT: generated URL contains query: %s", u.RawQuery)
		}
		if u.Fragment != "" {
			t.Errorf("SECURITY DEFECT: generated URL contains fragment: %s", u.Fragment)
		}
	})
}
