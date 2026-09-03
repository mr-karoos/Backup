package restic

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"backup-platform/pkg/uuid"
)

func TestResticRunner_LocalExecution_E2E(t *testing.T) {
	binaryPath := os.Getenv("RESTIC_BINARY_PATH")
	if binaryPath == "" {
		// Check common locations
		candidates := []string{
			filepath.Join(os.TempDir(), "restic-bin", "restic.exe"),
			"/usr/local/bin/restic",
		}
		for _, c := range candidates {
			if _, err := os.Stat(c); err == nil {
				binaryPath = c
				break
			}
		}
	}

	if binaryPath == "" {
		t.Skip("skipping restic local execution test: RESTIC_BINARY_PATH not set and restic not found")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	runner := NewResticRunner(binaryPath, nil)

	t.Run("Version returns valid 0.19.1 version string", func(t *testing.T) {
		v, err := runner.Version(ctx)
		if err != nil {
			t.Fatalf("expected Version to succeed, got: %v", err)
		}
		if !strings.Contains(v, "restic 0.19.1") {
			t.Errorf("expected version to contain 'restic 0.19.1', got: %s", v)
		}
	})

	t.Run("Init and Probe lifecycle on local repository", func(t *testing.T) {
		tempStorageRoot := t.TempDir()
		orgID := uuid.New()
		resID := uuid.New()

		target, err := NewLocalRepositoryTarget(tempStorageRoot, orgID, resID)
		if err != nil {
			t.Fatalf("failed creating local repository target: %v", err)
		}

		password := []byte("a-very-secure-32-byte-password-for-restic!!")

		// 1. Probe before init should fail
		err = runner.Probe(ctx, target, password)
		if err == nil {
			t.Fatalf("expected probe to fail on uninitialized repository")
		}

		// 2. Init should succeed
		err = runner.Init(ctx, target, password)
		if err != nil {
			t.Fatalf("expected restic init to succeed, got: %v", err)
		}

		// Verify repository files exist (e.g. config file, data dir, index dir)
		configPath := filepath.Join(target.ResticRepositoryURL(), "config")
		if _, err := os.Stat(configPath); err != nil {
			t.Fatalf("expected restic config file to exist at %s: %v", configPath, err)
		}

		// 3. Probe with correct password should succeed
		err = runner.Probe(ctx, target, password)
		if err != nil {
			t.Fatalf("expected probe to succeed with correct password, got: %v", err)
		}

		// 4. Probe with wrong password should fail and sanitize password from error
		wrongPassword := []byte("wrong-password-1234")
		err = runner.Probe(ctx, target, wrongPassword)
		if err == nil {
			t.Fatalf("expected probe to fail with wrong password")
		}
		if strings.Contains(err.Error(), string(wrongPassword)) {
			t.Errorf("SECURITY LEAK: wrong password leaked in error message: %v", err)
		}

		// 5. Corrupted repository (damage config file) should fail probe
		_ = os.WriteFile(configPath, []byte("garbage-corrupted-data"), 0600)
		err = runner.Probe(ctx, target, password)
		if err == nil {
			t.Fatalf("expected probe to fail on corrupted repository config")
		}
	})
}
