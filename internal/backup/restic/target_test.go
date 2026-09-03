package restic

import (
	"os"
	"path/filepath"
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

		// Verify cleanup zeroes secret
		target.Cleanup()
		if len(target.secretAccessKey) != 0 {
			t.Errorf("expected secretAccessKey to be cleared after cleanup")
		}
	})

	t.Run("creates S3-compatible target with custom endpoint and base prefix", func(t *testing.T) {
		cfg := domain.S3TargetConfig{
			Bucket:   "minio-bucket",
			Region:   "us-east-1",
			Endpoint: "https://minio.internal.net:9000",
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
			[]string{"minio.internal.net"},
		)
		if err != nil {
			t.Fatalf("expected success, got: %v", err)
		}

		expectedLocator := "cluster-prod/organizations/" + orgID.String() + "/resources/" + resourceID.String() + "/restic"
		if target.Locator() != expectedLocator {
			t.Errorf("expected locator %s, got: %s", expectedLocator, target.Locator())
		}

		expectedURL := "s3:https://minio.internal.net:9000/minio-bucket/" + expectedLocator
		if target.ResticRepositoryURL() != expectedURL {
			t.Errorf("expected url %s, got: %s", expectedURL, target.ResticRepositoryURL())
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

	t.Run("rejects link-local SSRF endpoint", func(t *testing.T) {
		cfg := domain.S3TargetConfig{
			Bucket:   "my-bucket",
			Region:   "us-east-1",
			Endpoint: "http://169.254.169.254/latest/meta-data",
		}

		_, err := NewS3RepositoryTarget(
			"s3",
			cfg,
			orgID,
			resourceID,
			"KEY",
			"SECRET",
			nil,
			true,
			nil,
		)
		if err == nil {
			t.Errorf("expected SSRF error for link-local AWS metadata IP")
		}
	})
}
