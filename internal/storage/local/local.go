package local

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"syscall"

	"backup-platform/internal/storage"
	"backup-platform/pkg/uuid"
)

const (
	dirPermission  = 0700
	filePermission = 0600
)

var drivePrefixRegex = regexp.MustCompile(`^[a-zA-Z]:`)

// SafeStorageError encapsulates storage errors without leaking internal filesystem paths in Error().
type SafeStorageError struct {
	Op      string
	Cause   error
	Message string
}

func (e *SafeStorageError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return fmt.Sprintf("artifact storage %s failed", e.Op)
}

func (e *SafeStorageError) Unwrap() error {
	return e.Cause
}

func mapStorageError(err error, op string) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, syscall.ENOSPC) || errors.Is(err, storage.ErrStorageFull) {
		return &SafeStorageError{Op: op, Cause: storage.ErrStorageFull, Message: "storage target out of disk space"}
	}
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, storage.ErrArtifactNotFound) {
		return &SafeStorageError{Op: op, Cause: storage.ErrArtifactNotFound, Message: "artifact not found in storage"}
	}
	if errors.Is(err, os.ErrExist) || errors.Is(err, storage.ErrArtifactCollision) {
		return &SafeStorageError{Op: op, Cause: storage.ErrArtifactCollision, Message: "artifact destination collision"}
	}
	if errors.Is(err, storage.ErrInvalidStorageReference) {
		return &SafeStorageError{Op: op, Cause: storage.ErrInvalidStorageReference, Message: "invalid storage reference"}
	}
	return &SafeStorageError{Op: op, Cause: storage.ErrStorageIO, Message: fmt.Sprintf("artifact storage %s failed", op)}
}

// LocalStorageProvider implements storage.StorageProvider for local filesystem storage.
type LocalStorageProvider struct {
	storageRoot string
}

// NewLocalStorageProvider initializes a new LocalStorageProvider rooted at the specified directory.
func NewLocalStorageProvider(storageRoot string) (*LocalStorageProvider, error) {
	cleaned := filepath.Clean(strings.TrimSpace(storageRoot))
	if cleaned == "" || cleaned == "." {
		return nil, errors.New("storage root cannot be empty")
	}
	return &LocalStorageProvider{storageRoot: cleaned}, nil
}

func ensurePrivateDir(path string) error {
	if err := os.MkdirAll(path, dirPermission); err != nil {
		return mapStorageError(err, "create_directory")
	}

	fi, err := os.Lstat(path)
	if err != nil {
		return mapStorageError(err, "stat_directory")
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return mapStorageError(storage.ErrInvalidStorageReference, "symlink_directory_rejected")
	}
	if runtime.GOOS != "windows" {
		if fi.Mode().Perm() != dirPermission {
			if err := os.Chmod(path, dirPermission); err != nil {
				return mapStorageError(err, "chmod_directory")
			}
			fiAfter, err := os.Lstat(path)
			if err != nil {
				return mapStorageError(err, "stat_directory_after_chmod")
			}
			if fiAfter.Mode().Perm() != dirPermission {
				return mapStorageError(storage.ErrStorageIO, "verify_directory_permission")
			}
		}
	}
	return nil
}

// EnsureStorageRoot validates that the storage root directory and required platform subdirectories exist
// with 0700 permissions, enforces 0700 on existing directories, and verifies write access via a transient probe file.
func (p *LocalStorageProvider) EnsureStorageRoot(ctx context.Context) error {
	// 1. Explicitly ensure platform root
	if err := ensurePrivateDir(p.storageRoot); err != nil {
		return mapStorageError(err, "ensure_storage_root")
	}

	// 2. Explicitly ensure platform tmp directory
	tmpDir := filepath.Join(p.storageRoot, "tmp")
	if err := ensurePrivateDir(tmpDir); err != nil {
		return mapStorageError(err, "ensure_tmp_directory")
	}

	// 3. Explicitly ensure platform organizations directory
	orgsDir := filepath.Join(p.storageRoot, "organizations")
	if err := ensurePrivateDir(orgsDir); err != nil {
		return mapStorageError(err, "ensure_organizations_directory")
	}

	// 4. Probe writeability and verify clean removal
	probeFile := filepath.Join(tmpDir, fmt.Sprintf(".probe-%s", uuid.New().String()))
	f, err := os.OpenFile(probeFile, os.O_CREATE|os.O_EXCL|os.O_WRONLY, filePermission)
	if err != nil {
		return mapStorageError(err, "probe_storage_root")
	}
	if closeErr := f.Close(); closeErr != nil {
		_ = os.Remove(probeFile)
		return mapStorageError(closeErr, "close_probe_file")
	}
	if removeErr := os.Remove(probeFile); removeErr != nil {
		return mapStorageError(removeErr, "remove_probe_file")
	}

	return nil
}

// SaveArtifact streams data from src into a temporary file, calculates its SHA-256 hash,
// verifies non-zero byte size, checks for collision, and atomically moves it to its final physical destination.
func (p *LocalStorageProvider) SaveArtifact(
	ctx context.Context,
	orgID, resID, runID, artifactID uuid.UUID,
	extension string,
	src io.Reader,
) (*storage.SaveResult, error) {
	if src == nil {
		return nil, errors.New("source reader cannot be nil")
	}

	ext := strings.TrimSpace(extension)
	if ext == "" {
		ext = ".sql.gz"
	}
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}

	if ext != ".sql.gz" && ext != ".tar.gz" {
		return nil, mapStorageError(storage.ErrInvalidStorageReference, "validate_extension")
	}

	// 1. Ensure platform root and tmp directory levels
	if err := ensurePrivateDir(p.storageRoot); err != nil {
		return nil, mapStorageError(err, "ensure_storage_root")
	}
	rootTmpDir := filepath.Join(p.storageRoot, "tmp")
	if err := ensurePrivateDir(rootTmpDir); err != nil {
		return nil, mapStorageError(err, "ensure_tmp_directory")
	}

	// 2. Prepare temporary run directory and file
	tempDir := filepath.Join(rootTmpDir, fmt.Sprintf("run-%s", runID.String()))
	if err := ensurePrivateDir(tempDir); err != nil {
		return nil, mapStorageError(err, "create_temp_run_directory")
	}

	defer func() {
		_ = os.Remove(tempDir)
	}()

	tempFilePath := filepath.Join(tempDir, fmt.Sprintf("artifact-%s%s.partial", artifactID.String(), ext))
	tempFile, err := os.OpenFile(tempFilePath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, filePermission)
	if err != nil {
		return nil, mapStorageError(err, "create_temporary_file")
	}

	// Defensive cleanup on any early failure
	cleanupTemp := true
	defer func() {
		if cleanupTemp {
			_ = tempFile.Close()
			_ = os.Remove(tempFilePath)
		}
	}()

	// 3. Stream data, compute SHA-256 simultaneously, and count written bytes
	hasher := sha256.New()
	mw := io.MultiWriter(tempFile, hasher)

	written, err := io.Copy(mw, src)
	if err != nil {
		return nil, mapStorageError(err, "stream_artifact_data")
	}
	if err := tempFile.Sync(); err != nil {
		return nil, mapStorageError(err, "sync_temporary_file")
	}
	if err := tempFile.Close(); err != nil {
		return nil, mapStorageError(err, "close_temporary_file")
	}

	// 4. Reject 0-byte empty artifacts
	if written <= 0 {
		return nil, errors.New("artifact size must be greater than zero")
	}

	// 5. Explicitly ensure every platform-owned directory level:
	// <root>/organizations
	// <root>/organizations/{org_id}
	// <root>/organizations/{org_id}/resources
	// <root>/organizations/{org_id}/resources/{res_id}
	// <root>/organizations/{org_id}/resources/{res_id}/artifacts
	orgsDir := filepath.Join(p.storageRoot, "organizations")
	if err := ensurePrivateDir(orgsDir); err != nil {
		return nil, mapStorageError(err, "ensure_organizations_directory")
	}

	orgDir := filepath.Join(orgsDir, orgID.String())
	if err := ensurePrivateDir(orgDir); err != nil {
		return nil, mapStorageError(err, "create_org_directory")
	}

	resourcesDir := filepath.Join(orgDir, "resources")
	if err := ensurePrivateDir(resourcesDir); err != nil {
		return nil, mapStorageError(err, "ensure_resources_directory")
	}

	resDir := filepath.Join(resourcesDir, resID.String())
	if err := ensurePrivateDir(resDir); err != nil {
		return nil, mapStorageError(err, "create_res_directory")
	}

	finalDir := filepath.Join(resDir, "artifacts")
	if err := ensurePrivateDir(finalDir); err != nil {
		return nil, mapStorageError(err, "create_artifacts_directory")
	}

	finalFilePath := filepath.Join(finalDir, fmt.Sprintf("%s%s", artifactID.String(), ext))

	// 6. Atomic no-overwrite finalization using os.Link (hard link in the same storage filesystem)
	linkErr := os.Link(tempFilePath, finalFilePath)
	if linkErr != nil {
		if os.IsExist(linkErr) {
			return nil, storage.ErrArtifactCollision
		}
		return nil, mapStorageError(linkErr, "finalize_artifact_link")
	}

	// 7. Enforce and verify final file permissions (0600)
	if chmodErr := os.Chmod(finalFilePath, filePermission); chmodErr != nil {
		_ = os.Remove(finalFilePath)
		return nil, mapStorageError(chmodErr, "chmod_final_artifact")
	}
	if runtime.GOOS != "windows" {
		finalStat, statErr := os.Lstat(finalFilePath)
		if statErr != nil {
			_ = os.Remove(finalFilePath)
			return nil, mapStorageError(statErr, "stat_final_artifact")
		}
		if finalStat.Mode().Perm() != filePermission {
			_ = os.Remove(finalFilePath)
			return nil, mapStorageError(storage.ErrStorageIO, "verify_final_artifact_permission")
		}
	}

	// 8. Remove temp file and ensure no duplicate hard link remains
	if unlinkErr := os.Remove(tempFilePath); unlinkErr != nil {
		_ = os.Remove(finalFilePath)
		return nil, mapStorageError(unlinkErr, "unlink_temporary_file")
	}
	cleanupTemp = false // Successfully finalized and temp unlinked

	checksumHex := hex.EncodeToString(hasher.Sum(nil))
	storageRef := fmt.Sprintf("organizations/%s/resources/%s/artifacts/%s%s", orgID.String(), resID.String(), artifactID.String(), ext)

	return &storage.SaveResult{
		StorageReference: storageRef,
		SizeBytes:        written,
		ChecksumSHA256:   checksumHex,
	}, nil
}

// OpenArtifact opens the physical file associated with storageReference for reading.
func (p *LocalStorageProvider) OpenArtifact(ctx context.Context, storageReference string) (io.ReadCloser, error) {
	physicalPath, err := p.resolveSecurePath(storageReference)
	if err != nil {
		return nil, err
	}

	file, err := os.Open(physicalPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, storage.ErrArtifactNotFound
		}
		return nil, mapStorageError(err, "open_artifact")
	}

	return file, nil
}

// DeleteArtifact deletes the physical file associated with storageReference.
func (p *LocalStorageProvider) DeleteArtifact(ctx context.Context, storageReference string) error {
	physicalPath, err := p.resolveSecurePath(storageReference)
	if err != nil {
		return err
	}

	if err := os.Remove(physicalPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil // Already deleted (idempotent)
		}
		return mapStorageError(err, "delete_artifact")
	}

	return nil
}

// resolveSecurePath strictly validates that storageReference is a canonical relative reference
// matching: organizations/{org_id}/resources/{res_id}/artifacts/{art_id}.{ext}
func (p *LocalStorageProvider) resolveSecurePath(storageReference string) (string, error) {
	// Rejects leading or trailing whitespace without silent trimming
	if storageReference != strings.TrimSpace(storageReference) || storageReference == "" {
		return "", mapStorageError(storage.ErrInvalidStorageReference, "validate_storage_reference")
	}

	// Reject NUL character
	if strings.ContainsRune(storageReference, '\x00') {
		return "", mapStorageError(storage.ErrInvalidStorageReference, "nul_byte_rejected")
	}

	// Reject any backslash character (storage reference must be canonical forward slash)
	if strings.Contains(storageReference, "\\") {
		return "", mapStorageError(storage.ErrInvalidStorageReference, "backslashes_rejected")
	}

	// Cross-platform drive letter prefix rejection (e.g. C:, D:)
	if drivePrefixRegex.MatchString(storageReference) {
		return "", mapStorageError(storage.ErrInvalidStorageReference, "drive_prefix_rejected")
	}

	// Reject explicit leading slashes or root-relative paths
	if strings.HasPrefix(storageReference, "/") {
		return "", mapStorageError(storage.ErrInvalidStorageReference, "absolute_path_rejected")
	}

	segments := strings.Split(storageReference, "/")
	if len(segments) != 6 {
		return "", mapStorageError(storage.ErrInvalidStorageReference, "segment_count_mismatch")
	}

	if segments[0] != "organizations" || segments[2] != "resources" || segments[4] != "artifacts" {
		return "", mapStorageError(storage.ErrInvalidStorageReference, "canonical_structure_mismatch")
	}

	if _, err := uuid.Parse(segments[1]); err != nil {
		return "", mapStorageError(storage.ErrInvalidStorageReference, "invalid_org_uuid")
	}

	if _, err := uuid.Parse(segments[3]); err != nil {
		return "", mapStorageError(storage.ErrInvalidStorageReference, "invalid_res_uuid")
	}

	artFile := segments[5]
	var artIDStr string
	if strings.HasSuffix(artFile, ".sql.gz") {
		artIDStr = strings.TrimSuffix(artFile, ".sql.gz")
	} else if strings.HasSuffix(artFile, ".tar.gz") {
		artIDStr = strings.TrimSuffix(artFile, ".tar.gz")
	} else {
		return "", mapStorageError(storage.ErrInvalidStorageReference, "invalid_extension")
	}

	if _, err := uuid.Parse(artIDStr); err != nil {
		return "", mapStorageError(storage.ErrInvalidStorageReference, "invalid_art_uuid")
	}

	// Normalize to local OS filepath
	cleanedRef := filepath.Clean(filepath.FromSlash(storageReference))
	if strings.HasPrefix(cleanedRef, "..") || filepath.IsAbs(cleanedRef) || filepath.VolumeName(cleanedRef) != "" {
		return "", mapStorageError(storage.ErrInvalidStorageReference, "path_traversal_detected")
	}

	resolvedPath := filepath.Join(p.storageRoot, cleanedRef)
	rootCleaned := filepath.Clean(p.storageRoot)

	// Ensure resolved physical path is strictly within storageRoot
	if !strings.HasPrefix(resolvedPath, rootCleaned+string(filepath.Separator)) && resolvedPath != rootCleaned {
		return "", mapStorageError(storage.ErrInvalidStorageReference, "storage_root_escape_detected")
	}

	return resolvedPath, nil
}

var (
	canonicalRunDirRegex  = regexp.MustCompile(`^run-([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})$`)
	canonicalPartialRegex = regexp.MustCompile(`^artifact-([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})\.(sql|tar)\.gz\.partial$`)
)

// CleanOrphanTemporaryArtifacts scans <storageRoot>/tmp for canonical platform-generated
// temporary run directories and .partial files left behind after ungraceful crashes, safely
// removing recognized temporary files and deleting empty run directories.
func (p *LocalStorageProvider) CleanOrphanTemporaryArtifacts(ctx context.Context) (int, error) {
	tmpDir := filepath.Join(p.storageRoot, "tmp")

	// If tmp directory does not exist, nothing to clean
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}
		return 0, mapStorageError(err, "read_tmp_directory")
	}

	cleanedCount := 0

	for _, entry := range entries {
		if ctx.Err() != nil {
			return cleanedCount, ctx.Err()
		}

		// Reject symlinks and non-directories
		if entry.Type()&os.ModeSymlink != 0 || !entry.IsDir() {
			continue
		}

		// Validate directory name matches canonical format run-<uuid>
		matches := canonicalRunDirRegex.FindStringSubmatch(entry.Name())
		if len(matches) < 2 {
			continue
		}
		runUUID, err := uuid.Parse(matches[1])
		if err != nil || runUUID == uuid.Nil {
			continue
		}

		runDirPath := filepath.Join(tmpDir, entry.Name())
		fileEntries, err := os.ReadDir(runDirPath)
		if err != nil {
			continue
		}

		for _, fileEntry := range fileEntries {
			if ctx.Err() != nil {
				return cleanedCount, ctx.Err()
			}

			// Reject symlinks and subdirectories
			if fileEntry.Type()&os.ModeSymlink != 0 || fileEntry.IsDir() {
				continue
			}

			// Validate file name matches canonical format artifact-<uuid>.(sql|tar).gz.partial
			fileMatches := canonicalPartialRegex.FindStringSubmatch(fileEntry.Name())
			if len(fileMatches) < 2 {
				continue
			}
			artUUID, err := uuid.Parse(fileMatches[1])
			if err != nil || artUUID == uuid.Nil {
				continue
			}

			filePath := filepath.Join(runDirPath, fileEntry.Name())
			if remErr := os.Remove(filePath); remErr == nil {
				cleanedCount++
			}
		}

		// Only removes directory if it is now completely empty
		_ = os.Remove(runDirPath)
	}

	return cleanedCount, nil
}
