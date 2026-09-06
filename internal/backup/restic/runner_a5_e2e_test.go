package restic

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"backup-platform/internal/credential/secretcrypto"
	"backup-platform/pkg/uuid"
)

type localTestTarget struct {
	repoDir string
}

func (t *localTestTarget) Type() string                { return "local" }
func (t *localTestTarget) Locator() string             { return t.repoDir }
func (t *localTestTarget) ResticRepositoryURL() string { return t.repoDir }
func (t *localTestTarget) Env() []string               { return nil }
func (t *localTestTarget) Cleanup()                    {}

func TestResticRunner_StepA5_RealResticE2E(t *testing.T) {
	resticBin := os.Getenv("TEST_RESTIC_BINARY")
	if resticBin == "" {
		if _, err := os.Stat(`C:\Users\Kroos\AppData\Local\Temp\restic-bin\restic.exe`); err == nil {
			resticBin = `C:\Users\Kroos\AppData\Local\Temp\restic-bin\restic.exe`
		} else if _, err := exec.LookPath("restic"); err == nil {
			resticBin = "restic"
		} else {
			t.Skip("skipping real restic test: restic binary not found")
		}
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	runner := NewResticRunner(resticBin, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if err := runner.ValidateVersion(ctx); err != nil {
		t.Fatalf("failed validating restic version: %v", err)
	}

	// Create temp directory for repository
	tempDir, err := os.MkdirTemp("", "restic-a5-test-*")
	if err != nil {
		t.Fatalf("failed creating temp dir: %v", err)
	}
	defer func() {
		_ = os.RemoveAll(tempDir)
	}()

	repoPath := filepath.Join(tempDir, "repo")
	target := &localTestTarget{repoDir: repoPath}
	password := []byte("strong-test-password-a5-12345")
	defer secretcrypto.ZeroBytes(password)

	// 1. Initialize repository
	if err := runner.Init(ctx, target, password); err != nil {
		t.Fatalf("failed initializing repository: %v", err)
	}

	// 2. Create a test file and back it up using restic CLI directly
	sourceFile := filepath.Join(tempDir, "testdata.txt")
	testContent := "hello real restic maintenance 0.19.1 step a5"
	if err := os.WriteFile(sourceFile, []byte(testContent), 0600); err != nil {
		t.Fatalf("failed creating source file: %v", err)
	}

	cmd := exec.CommandContext(ctx, resticBin, "-r", repoPath, "backup", sourceFile)
	cmd.Env = append(os.Environ(), "RESTIC_PASSWORD="+string(password))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed running restic backup: %v (output: %s)", err, string(out))
	}

	// 3. Find the snapshot ID using restic snapshots --json
	snapCmd := exec.CommandContext(ctx, resticBin, "-r", repoPath, "snapshots", "--json")
	snapCmd.Env = append(os.Environ(), "RESTIC_PASSWORD="+string(password))
	out, err := snapCmd.Output()
	if err != nil {
		t.Fatalf("failed listing snapshots: %v", err)
	}

	var items []SnapshotItem
	if err := json.Unmarshal(out, &items); err != nil {
		t.Fatalf("failed unmarshaling snapshot items: %v", err)
	}
	if len(items) == 0 {
		t.Fatalf("no snapshot items found in restic output: %s", string(out))
	}
	snapID := items[0].ID
	t.Logf("Created snapshot with canonical ID: %s", snapID)

	t.Run("Validation: Forget rejects invalid snapshot IDs", func(t *testing.T) {
		if err := runner.ForgetSnapshot(ctx, target, password, "latest"); err == nil {
			t.Fatalf("expected error for non-64-hex snapshot id 'latest'")
		}
		if err := runner.ForgetSnapshot(ctx, target, password, "12345"); err == nil {
			t.Fatalf("expected error for short snapshot id")
		}
		if err := runner.ForgetSnapshot(ctx, target, password, "--prune"); err == nil {
			t.Fatalf("expected error for flag injection")
		}
	})

	t.Run("Validation: CheckSubset rejects invalid parameters", func(t *testing.T) {
		if err := runner.CheckSubset(ctx, target, password, 0, 4); err == nil {
			t.Fatalf("expected error for subsetIndex < 1")
		}
		if err := runner.CheckSubset(ctx, target, password, 5, 4); err == nil {
			t.Fatalf("expected error for subsetIndex > subsetTotal")
		}
	})

	t.Run("CheckSubset succeeds on valid repository", func(t *testing.T) {
		if err := runner.CheckSubset(ctx, target, password, 1, 4); err != nil {
			t.Fatalf("check subset 1/4 failed: %v", err)
		}
		t.Log("CheckSubset 1/4 passed successfully")
	})

	t.Run("ForgetSnapshot and Prune lifecycle", func(t *testing.T) {
		// Forget snapshot
		if err := runner.ForgetSnapshot(ctx, target, password, snapID); err != nil {
			t.Fatalf("failed forgetting snapshot: %v", err)
		}
		t.Logf("Successfully executed restic forget %s", snapID)

		// Verify snapshot is gone
		checkSnap := exec.CommandContext(ctx, resticBin, "-r", repoPath, "snapshots", "--json")
		checkSnap.Env = append(os.Environ(), "RESTIC_PASSWORD="+string(password))
		checkOut, _ := checkSnap.Output()
		if strings.Contains(string(checkOut), snapID) {
			t.Fatalf("forgotten snapshot still present in repository: %s", string(checkOut))
		}

		// Prune repository
		if err := runner.Prune(ctx, target, password); err != nil {
			t.Fatalf("failed pruning repository: %v", err)
		}
		t.Log("Successfully executed restic prune")

		// Re-check repository integrity after prune
		if err := runner.CheckSubset(ctx, target, password, 1, 1); err != nil {
			t.Fatalf("check failed after prune: %v", err)
		}
		t.Log("Repository check 1/1 clean after prune")
	})

	t.Run("Zeroize password verification", func(t *testing.T) {
		pwdCopy := []byte("secret-to-zeroize-1234567890")
		secretcrypto.ZeroBytes(pwdCopy)
		for i, b := range pwdCopy {
			if b != 0 {
				t.Fatalf("byte %d was not zeroized, got %v", i, b)
			}
		}
	})

	_ = uuid.Nil
}
