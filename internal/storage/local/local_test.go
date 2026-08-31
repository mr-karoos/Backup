package local

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"backup-platform/internal/storage"
	"backup-platform/pkg/uuid"
)

func TestLocalStorageProvider_SaveArtifact_Success(t *testing.T) {
	tempDir := t.TempDir()
	provider, err := NewLocalStorageProvider(tempDir)
	if err != nil {
		t.Fatalf("failed to initialize provider: %v", err)
	}

	ctx := context.Background()
	if err := provider.EnsureStorageRoot(ctx); err != nil {
		t.Fatalf("failed to ensure storage root: %v", err)
	}

	orgID := uuid.New()
	resID := uuid.New()
	runID := uuid.New()
	artID := uuid.New()

	data := []byte("-- MySQL dump header\nINSERT INTO users VALUES (1, 'alice');\n")
	hasher := sha256.New()
	hasher.Write(data)
	expectedHash := hex.EncodeToString(hasher.Sum(nil))

	res, err := provider.SaveArtifact(ctx, orgID, resID, runID, artID, ".sql.gz", bytes.NewReader(data))
	if err != nil {
		t.Fatalf("unexpected save error: %v", err)
	}

	expectedRef := "organizations/" + orgID.String() + "/resources/" + resID.String() + "/artifacts/" + artID.String() + ".sql.gz"
	if res.StorageReference != expectedRef {
		t.Errorf("expected storage ref %q, got %q", expectedRef, res.StorageReference)
	}
	if res.SizeBytes != int64(len(data)) {
		t.Errorf("expected size %d, got %d", len(data), res.SizeBytes)
	}
	if res.ChecksumSHA256 != expectedHash {
		t.Errorf("expected checksum %q, got %q", expectedHash, res.ChecksumSHA256)
	}

	// Verify physical file exists and content matches
	physicalPath := filepath.Join(tempDir, filepath.FromSlash(expectedRef))
	fi, err := os.Stat(physicalPath)
	if err != nil {
		t.Fatalf("physical file not found: %v", err)
	}

	if runtime.GOOS != "windows" {
		if fi.Mode().Perm() != 0600 {
			t.Errorf("expected file mode 0600, got %v", fi.Mode().Perm())
		}
		dirInfo, err := os.Stat(filepath.Dir(physicalPath))
		if err != nil {
			t.Fatalf("failed stat parent dir: %v", err)
		}
		if dirInfo.Mode().Perm() != 0700 {
			t.Errorf("expected dir mode 0700, got %v", dirInfo.Mode().Perm())
		}
	}

	// Verify OpenArtifact
	rc, err := provider.OpenArtifact(ctx, res.StorageReference)
	if err != nil {
		t.Fatalf("failed opening artifact: %v", err)
	}
	readBytes, err := io.ReadAll(rc)
	_ = rc.Close()
	if err != nil {
		t.Fatalf("failed reading artifact: %v", err)
	}
	if !bytes.Equal(readBytes, data) {
		t.Errorf("read bytes mismatch: got %q, expected %q", string(readBytes), string(data))
	}

	// Verify DeleteArtifact
	if err := provider.DeleteArtifact(ctx, res.StorageReference); err != nil {
		t.Fatalf("failed deleting artifact: %v", err)
	}
	if _, err := os.Stat(physicalPath); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expected physical file to be removed after delete, got err: %v", err)
	}
}

func TestLocalStorageProvider_RejectsEmptyStream(t *testing.T) {
	tempDir := t.TempDir()
	provider, err := NewLocalStorageProvider(tempDir)
	if err != nil {
		t.Fatalf("failed to initialize provider: %v", err)
	}

	ctx := context.Background()
	orgID := uuid.New()
	resID := uuid.New()
	runID := uuid.New()
	artID := uuid.New()

	emptyReader := bytes.NewReader([]byte{})
	_, err = provider.SaveArtifact(ctx, orgID, resID, runID, artID, ".sql.gz", emptyReader)
	if err == nil {
		t.Fatalf("expected error when saving 0-byte stream")
	}

	// Verify no partial file or artifact file exists
	expectedRef := "organizations/" + orgID.String() + "/resources/" + resID.String() + "/artifacts/" + artID.String() + ".sql.gz"
	physicalPath := filepath.Join(tempDir, filepath.FromSlash(expectedRef))
	if _, err := os.Stat(physicalPath); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("0-byte file should not have been finalized on disk")
	}
}

func TestLocalStorageProvider_CollisionProtection(t *testing.T) {
	tempDir := t.TempDir()
	provider, err := NewLocalStorageProvider(tempDir)
	if err != nil {
		t.Fatalf("failed to initialize provider: %v", err)
	}

	ctx := context.Background()
	_ = provider.EnsureStorageRoot(ctx)

	orgID := uuid.New()
	resID := uuid.New()
	runID := uuid.New()
	artID := uuid.New()

	finalDir := filepath.Join(tempDir, "organizations", orgID.String(), "resources", resID.String(), "artifacts")
	_ = os.MkdirAll(finalDir, 0700)
	finalPath := filepath.Join(finalDir, artID.String()+".sql.gz")
	originalData := []byte("original data that must not be overwritten")
	_ = os.WriteFile(finalPath, originalData, 0600)

	newData := []byte("new conflicting data")
	_, err = provider.SaveArtifact(ctx, orgID, resID, runID, artID, ".sql.gz", bytes.NewReader(newData))
	if !errors.Is(err, storage.ErrArtifactCollision) {
		t.Fatalf("expected ErrArtifactCollision, got: %v", err)
	}

	// Verify original file unchanged
	readData, _ := os.ReadFile(finalPath)
	if !bytes.Equal(readData, originalData) {
		t.Errorf("expected original data preserved, got %q", string(readData))
	}
}

func TestLocalStorageProvider_PathTraversalRejection(t *testing.T) {
	tempDir := t.TempDir()
	provider, err := NewLocalStorageProvider(tempDir)
	if err != nil {
		t.Fatalf("failed to initialize provider: %v", err)
	}

	ctx := context.Background()
	maliciousRefs := []string{
		"../../etc/passwd",
		"/etc/passwd",
		"organizations/../../../sensitive",
		"..\\..\\windows\\system32",
		"C:\\Windows\\system32\\cmd.exe",
		"C:/Windows/system32/cmd.exe",
		"D:\\file.txt",
		"organizations/..\x00../etc",
		"organizations/valid/../../etc",
		"organizations\\resources\\test.sql.gz",
		" organizations/" + uuid.New().String() + "/resources/" + uuid.New().String() + "/artifacts/" + uuid.New().String() + ".sql.gz",
		"organizations/" + uuid.New().String() + "/resources/" + uuid.New().String() + "/artifacts/" + uuid.New().String() + ".sql.gz ",
		"tmp/run/test.sql.gz",
		"organizations/not-a-uuid/resources/" + uuid.New().String() + "/artifacts/" + uuid.New().String() + ".sql.gz",
		"organizations/" + uuid.New().String() + "/resources/not-a-uuid/artifacts/" + uuid.New().String() + ".sql.gz",
		"organizations/" + uuid.New().String() + "/resources/" + uuid.New().String() + "/artifacts/not-a-uuid.sql.gz",
		"organizations/" + uuid.New().String() + "/resources/" + uuid.New().String() + "/artifacts/" + uuid.New().String() + ".exe",
		"organizations/" + uuid.New().String() + "/resources/" + uuid.New().String() + "/artifacts/" + uuid.New().String() + "/extra.sql.gz",
	}

	for _, ref := range maliciousRefs {
		t.Run("Open_"+ref, func(t *testing.T) {
			_, err := provider.OpenArtifact(ctx, ref)
			if !errors.Is(err, storage.ErrInvalidStorageReference) {
				t.Errorf("expected ErrInvalidStorageReference for invalid ref attempt %q, got: %v", ref, err)
			}
		})
		t.Run("Delete_"+ref, func(t *testing.T) {
			err := provider.DeleteArtifact(ctx, ref)
			if !errors.Is(err, storage.ErrInvalidStorageReference) {
				t.Errorf("expected ErrInvalidStorageReference for invalid ref attempt %q, got: %v", ref, err)
			}
		})
	}
}

func TestLocalStorageProvider_AbsolutePathRedaction(t *testing.T) {
	tempDir := t.TempDir()
	provider, err := NewLocalStorageProvider(tempDir)
	if err != nil {
		t.Fatalf("failed to initialize provider: %v", err)
	}

	ctx := context.Background()
	validRef := "organizations/" + uuid.New().String() + "/resources/" + uuid.New().String() + "/artifacts/" + uuid.New().String() + ".sql.gz"

	// Trigger open error on non-existent file
	_, err = provider.OpenArtifact(ctx, validRef)
	if err == nil {
		t.Fatalf("expected error on non-existent file")
	}

	errMsg := err.Error()
	if strings.Contains(errMsg, tempDir) || strings.Contains(errMsg, "/organizations") || strings.Contains(errMsg, "\\organizations") {
		t.Errorf("SECURITY FLAW: storage Error() leaked physical file path: %s", errMsg)
	}
}

func TestLocalStorageProvider_PermissionEnforcement(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping POSIX permission tests on Windows")
	}

	tempDir := t.TempDir()
	_ = os.Chmod(tempDir, 0755)

	provider, err := NewLocalStorageProvider(tempDir)
	if err != nil {
		t.Fatalf("failed to initialize provider: %v", err)
	}

	if err := provider.EnsureStorageRoot(context.Background()); err != nil {
		t.Fatalf("failed to ensure storage root: %v", err)
	}

	fi, err := os.Stat(tempDir)
	if err != nil {
		t.Fatalf("failed to stat storage root: %v", err)
	}
	if fi.Mode().Perm() != 0700 {
		t.Errorf("expected storage root upgraded to 0700, got: %v", fi.Mode().Perm())
	}
}

func TestLocalStorageProvider_PermissionHierarchyEnforcement(t *testing.T) {
	tempDir := t.TempDir()

	// Pre-create organizations and subdirs with permissive 0755 permissions
	orgsPre := filepath.Join(tempDir, "organizations")
	_ = os.MkdirAll(orgsPre, 0755)
	_ = os.Chmod(orgsPre, 0755)

	provider, err := NewLocalStorageProvider(tempDir)
	if err != nil {
		t.Fatalf("failed to initialize provider: %v", err)
	}

	ctx := context.Background()
	orgID := uuid.New()
	resID := uuid.New()
	runID := uuid.New()
	artID := uuid.New()

	data := []byte("-- MySQL dump header\nINSERT INTO users VALUES (1, 'alice');\n")
	res, err := provider.SaveArtifact(ctx, orgID, resID, runID, artID, ".sql.gz", bytes.NewReader(data))
	if err != nil {
		t.Fatalf("unexpected save error: %v", err)
	}

	if runtime.GOOS != "windows" {
		// Check all platform-owned levels are 0700
		levels := []string{
			tempDir,
			filepath.Join(tempDir, "tmp"),
			filepath.Join(tempDir, "organizations"),
			filepath.Join(tempDir, "organizations", orgID.String()),
			filepath.Join(tempDir, "organizations", orgID.String(), "resources"),
			filepath.Join(tempDir, "organizations", orgID.String(), "resources", resID.String()),
			filepath.Join(tempDir, "organizations", orgID.String(), "resources", resID.String(), "artifacts"),
		}

		for _, lvl := range levels {
			fi, err := os.Stat(lvl)
			if err != nil {
				t.Fatalf("failed to stat directory level %s: %v", lvl, err)
			}
			if fi.Mode().Perm() != 0700 {
				t.Errorf("expected directory %s to have 0700, got %v", lvl, fi.Mode().Perm())
			}
		}

		// Check final artifact file is 0600
		finalPath := filepath.Join(tempDir, filepath.FromSlash(res.StorageReference))
		fileInfo, err := os.Stat(finalPath)
		if err != nil {
			t.Fatalf("failed to stat final artifact: %v", err)
		}
		if fileInfo.Mode().Perm() != 0600 {
			t.Errorf("expected final artifact to have 0600, got %v", fileInfo.Mode().Perm())
		}
	}
}

func TestLocalStorageProvider_TempCleanupRegression(t *testing.T) {
	tempDir := t.TempDir()
	provider, err := NewLocalStorageProvider(tempDir)
	if err != nil {
		t.Fatalf("failed to initialize provider: %v", err)
	}

	ctx := context.Background()
	orgID := uuid.New()
	resID := uuid.New()
	runID := uuid.New()
	artID := uuid.New()

	data := []byte("-- MySQL dump header\nINSERT INTO users VALUES (1, 'alice');\n")
	_, err = provider.SaveArtifact(ctx, orgID, resID, runID, artID, ".sql.gz", bytes.NewReader(data))
	if err != nil {
		t.Fatalf("unexpected save error: %v", err)
	}

	// Verify temp run directory has no lingering partial files
	runTempDir := filepath.Join(tempDir, "tmp", fmt.Sprintf("run-%s", runID.String()))
	partialFile := filepath.Join(runTempDir, fmt.Sprintf("artifact-%s.sql.gz.partial", artID.String()))
	if _, err := os.Stat(partialFile); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expected partial file to be removed after successful save, got err: %v", err)
	}
}
