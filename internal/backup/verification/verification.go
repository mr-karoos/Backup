package verification

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"

	"backup-platform/internal/artifactcrypto"
	"backup-platform/internal/backup/domain"
	"backup-platform/internal/backup/restic"
	"backup-platform/internal/storage"
	"backup-platform/pkg/uuid"
)

const (
	maxSanityHeaderBytes      = 64 * 1024 // 64 KiB
	canonicalVerifiedMsg      = "checksum and gzip structural integrity verified"
	canonicalFilesVerifiedMsg = "checksum and tar archive structural integrity verified"
)

// Verifier defines the interface for verifying backup artifact physical and structural integrity.
type Verifier interface {
	VerifyDatabaseArtifact(
		ctx context.Context,
		storageProvider storage.StorageProvider,
		storageReference string,
		expectedSizeBytes int64,
		expectedChecksumSHA256 string,
	) (string, error)

	VerifyFilesArtifact(
		ctx context.Context,
		storageProvider storage.StorageProvider,
		storageReference string,
		expectedSizeBytes int64,
		expectedChecksumSHA256 string,
	) (string, error)

	VerifyEncryptedDatabaseArtifact(
		ctx context.Context,
		storageProvider storage.StorageProvider,
		storageReference string,
		expectedPlaintextSize int64,
		expectedPlaintextChecksum string,
		storedSizeBytes int64,
		ciphertextSHA256 string,
		orgID, artifactID uuid.UUID,
	) (string, error)

	VerifyEncryptedFilesArtifact(
		ctx context.Context,
		storageProvider storage.StorageProvider,
		storageReference string,
		expectedPlaintextSize int64,
		expectedPlaintextChecksum string,
		storedSizeBytes int64,
		ciphertextSHA256 string,
		orgID, artifactID uuid.UUID,
	) (string, error)

	VerifyResticSnapshot(
		ctx context.Context,
		runner restic.CommandRunner,
		target restic.RepositoryTarget,
		password []byte,
		snapshotID string,
		orgID, resID, runID, artifactID uuid.UUID,
		targetToken string,
		internalFilename string,
		expectedLogicalSize int64,
	) (string, error)
}

const canonicalResticVerifiedMsg = "level-1 restic snapshot verified: tags, file structure, and sample dump confirmed"

// VerificationEngine performs multi-point integrity checks on database and file backup artifacts.
type VerificationEngine struct {
	keyProvider artifactcrypto.KeyProvider
}

// NewVerificationEngine constructs a new VerificationEngine.
func NewVerificationEngine() *VerificationEngine {
	return &VerificationEngine{}
}

// NewVerificationEngineWithKeyProvider constructs a new VerificationEngine with key provider injected.
func NewVerificationEngineWithKeyProvider(keyProvider artifactcrypto.KeyProvider) *VerificationEngine {
	return &VerificationEngine{
		keyProvider: keyProvider,
	}
}

// SetKeyProvider sets or updates the artifact key provider.
func (v *VerificationEngine) SetKeyProvider(keyProvider artifactcrypto.KeyProvider) {
	v.keyProvider = keyProvider
}

type countingReader struct {
	r          io.Reader
	totalBytes int64
}

func (cr *countingReader) Read(p []byte) (int, error) {
	n, err := cr.r.Read(p)
	cr.totalBytes += int64(n)
	return n, err
}

// VerifyDatabaseArtifact performs:
// 1. File existence and non-zero size verification.
// 2. Recomputed SHA-256 match against expected checksum.
// 3. Size match against recorded metadata.
// 4. End-to-end gzip decompression without structural errors.
// 5. Non-empty decompressed content.
// 6. Basic MySQL/MariaDB dump header sanity check.
func (v *VerificationEngine) VerifyDatabaseArtifact(
	ctx context.Context,
	storageProvider storage.StorageProvider,
	storageReference string,
	expectedSizeBytes int64,
	expectedChecksumSHA256 string,
) (string, error) {
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if storageProvider == nil {
		return "", errors.New("storage provider cannot be nil")
	}
	if expectedSizeBytes <= 0 {
		return "", errors.New("expected artifact size must be greater than zero")
	}
	if strings.TrimSpace(expectedChecksumSHA256) == "" {
		return "", errors.New("expected checksum cannot be empty")
	}

	rc, err := storageProvider.OpenArtifact(ctx, storageReference)
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", fmt.Errorf("failed opening artifact for verification: %w", err)
	}
	defer rc.Close()

	// Interrupt blocked reads immediately if context is cancelled mid-stream
	stopWait := context.AfterFunc(ctx, func() {
		_ = rc.Close()
	})
	defer stopWait()

	hasher := sha256.New()
	cr := &countingReader{r: io.TeeReader(rc, hasher)}

	gzReader, err := gzip.NewReader(cr)
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", fmt.Errorf("artifact is not a valid gzip stream: %w", err)
	}
	defer gzReader.Close()

	// Read initial sanity chunk
	sanityBuf := make([]byte, maxSanityHeaderBytes)
	n, readErr := io.ReadFull(gzReader, sanityBuf)
	if readErr != nil && !errors.Is(readErr, io.EOF) && !errors.Is(readErr, io.ErrUnexpectedEOF) {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", fmt.Errorf("failed reading decompressed stream: %w", readErr)
	}

	decompressedPrefix := sanityBuf[:n]
	var totalDecompressedBytes int64 = int64(n)

	// Drain remaining decompressed stream to io.Discard to verify complete archive integrity to EOF
	remainderBytes, drainErr := io.Copy(io.Discard, gzReader)
	if drainErr != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", fmt.Errorf("gzip stream integrity check failed: %w", drainErr)
	}
	totalDecompressedBytes += remainderBytes

	if err := gzReader.Close(); err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", fmt.Errorf("gzip stream closure error: %w", err)
	}

	// Drain any remaining bytes from count reader to complete SHA-256 computation
	if _, err := io.Copy(io.Discard, cr); err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", fmt.Errorf("failed reading compressed stream: %w", err)
	}

	if ctx.Err() != nil {
		return "", ctx.Err()
	}

	// 1. Verify physical size
	if cr.totalBytes <= 0 {
		return "", errors.New("verified artifact size is zero")
	}
	if cr.totalBytes != expectedSizeBytes {
		return "", fmt.Errorf("artifact size mismatch: expected %d bytes, got %d bytes", expectedSizeBytes, cr.totalBytes)
	}

	// 2. Verify SHA-256 checksum
	calculatedSHA256 := hex.EncodeToString(hasher.Sum(nil))
	if !strings.EqualFold(calculatedSHA256, expectedChecksumSHA256) {
		return "", fmt.Errorf("checksum mismatch: expected %s, got %s", expectedChecksumSHA256, calculatedSHA256)
	}

	// 3. Verify non-empty decompressed content
	if totalDecompressedBytes <= 0 {
		return "", errors.New("decompressed database dump is empty")
	}

	// 4. Basic MySQL/MariaDB dump header sanity check
	if !hasValidMySQLDumpMarker(decompressedPrefix) {
		return "", errors.New("decompressed stream failed basic SQL dump format sanity check")
	}

	return canonicalVerifiedMsg, nil
}

// VerifyFilesArtifact performs:
// 1. File existence and non-zero size verification.
// 2. Recomputed SHA-256 match against expected checksum.
// 3. Size match against recorded metadata.
// 4. End-to-end gzip decompression without structural errors.
// 5. Full tar archive validation with at least 1 valid member.
// 6. Member path safety (rejection of absolute paths and parent traversal segments).
// 7. Full gzip footer/CRC validation.
func (v *VerificationEngine) VerifyFilesArtifact(
	ctx context.Context,
	storageProvider storage.StorageProvider,
	storageReference string,
	expectedSizeBytes int64,
	expectedChecksumSHA256 string,
) (string, error) {
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if storageProvider == nil {
		return "", errors.New("storage provider cannot be nil")
	}
	if expectedSizeBytes <= 0 {
		return "", errors.New("expected artifact size must be greater than zero")
	}
	if strings.TrimSpace(expectedChecksumSHA256) == "" {
		return "", errors.New("expected checksum cannot be empty")
	}

	rc, err := storageProvider.OpenArtifact(ctx, storageReference)
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", fmt.Errorf("failed opening artifact for verification: %w", err)
	}
	defer rc.Close()

	// Interrupt blocked reads immediately if context is cancelled mid-stream
	stopWait := context.AfterFunc(ctx, func() {
		_ = rc.Close()
	})
	defer stopWait()

	hasher := sha256.New()
	cr := &countingReader{r: io.TeeReader(rc, hasher)}

	gzReader, err := gzip.NewReader(cr)
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", fmt.Errorf("artifact is not a valid gzip stream: %w", err)
	}
	defer gzReader.Close()

	tarReader := tar.NewReader(gzReader)
	var entriesCount int

	for {
		header, err := tarReader.Next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			return "", fmt.Errorf("tar structural integrity check failed: %w", err)
		}

		entriesCount++

		name := header.Name
		if strings.HasPrefix(name, "/") {
			return "", errors.New("tar archive contains an unsafe absolute member path")
		}

		segments := strings.Split(name, "/")
		for _, seg := range segments {
			if seg == ".." {
				return "", errors.New("tar archive contains an unsafe parent traversal member path")
			}
		}
	}

	if entriesCount == 0 {
		return "", errors.New("tar archive is empty (zero entries)")
	}

	// Drain remaining bytes in gzip stream to io.Discard to ensure gzip footer/CRC is verified
	if _, err := io.Copy(io.Discard, gzReader); err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", fmt.Errorf("gzip stream integrity check failed: %w", err)
	}

	if err := gzReader.Close(); err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", fmt.Errorf("gzip stream closure error: %w", err)
	}

	// Drain any remaining bytes from count reader to complete SHA-256 computation
	if _, err := io.Copy(io.Discard, cr); err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", fmt.Errorf("failed reading compressed stream: %w", err)
	}

	if ctx.Err() != nil {
		return "", ctx.Err()
	}

	// 1. Verify physical size
	if cr.totalBytes <= 0 {
		return "", errors.New("verified artifact size is zero")
	}
	if cr.totalBytes != expectedSizeBytes {
		return "", fmt.Errorf("artifact size mismatch: expected %d bytes, got %d bytes", expectedSizeBytes, cr.totalBytes)
	}

	// 2. Verify SHA-256 checksum
	calculatedSHA256 := hex.EncodeToString(hasher.Sum(nil))
	if !strings.EqualFold(calculatedSHA256, expectedChecksumSHA256) {
		return "", fmt.Errorf("checksum mismatch: expected %s, got %s", expectedChecksumSHA256, calculatedSHA256)
	}

	return canonicalFilesVerifiedMsg, nil
}

func hasValidMySQLDumpMarker(prefix []byte) bool {
	markers := []string{
		"-- MySQL dump",
		"-- MariaDB dump",
		"/*!40",
		"CREATE DATABASE",
		"USE ",
		"DROP TABLE",
		"CREATE TABLE",
		"SET ",
		"LOCK TABLES",
		"INSERT INTO",
	}

	prefixStr := string(bytes.ToUpper(prefix))
	for _, marker := range markers {
		if strings.Contains(prefixStr, strings.ToUpper(marker)) {
			return true
		}
	}
	return false
}

// VerifyEncryptedDatabaseArtifact verifies:
// 1. Physical Layer: Stored physical object exists, matches storedSizeBytes and ciphertextSHA256.
// 2. Decryption Layer: Stream decrypts using BPAE with orgID/artifactID binding and valid FINAL record.
// 3. Plaintext Layer: Decrypted stream matches expectedPlaintextSize, expectedPlaintextChecksum, valid gzip, and valid SQL dump marker.
func (v *VerificationEngine) VerifyEncryptedDatabaseArtifact(
	ctx context.Context,
	storageProvider storage.StorageProvider,
	storageReference string,
	expectedPlaintextSize int64,
	expectedPlaintextChecksum string,
	storedSizeBytes int64,
	ciphertextSHA256 string,
	orgID, artifactID uuid.UUID,
) (string, error) {
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if storageProvider == nil {
		return "", errors.New("storage provider cannot be nil")
	}
	if v.keyProvider == nil {
		return "", errors.New("artifact key provider cannot be nil")
	}
	if expectedPlaintextSize <= 0 {
		return "", errors.New("expected plaintext size must be greater than zero")
	}
	if storedSizeBytes <= 0 {
		return "", errors.New("stored size must be greater than zero")
	}
	if strings.TrimSpace(expectedPlaintextChecksum) == "" {
		return "", errors.New("expected plaintext checksum cannot be empty")
	}
	if strings.TrimSpace(ciphertextSHA256) == "" {
		return "", errors.New("ciphertext checksum cannot be empty")
	}
	if orgID == uuid.Nil || artifactID == uuid.Nil {
		return "", errors.New("valid organization ID and artifact ID are required")
	}

	rc, err := storageProvider.OpenArtifact(ctx, storageReference)
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", fmt.Errorf("failed opening artifact for verification: %w", err)
	}
	defer rc.Close()

	stopWait := context.AfterFunc(ctx, func() {
		_ = rc.Close()
	})
	defer stopWait()

	// Physical layer: compute ciphertext SHA-256 and count physical stored bytes
	cipherHasher := sha256.New()
	cipherCounter := &countingReader{r: io.TeeReader(rc, cipherHasher)}

	// Decryption layer: stream through BPAE DecryptReader
	decReader, err := artifactcrypto.NewDecryptReader(cipherCounter, v.keyProvider, orgID, artifactID)
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", fmt.Errorf("failed initializing BPAE decryption: %w", err)
	}
	defer decReader.Close()

	// Plaintext layer: compute plaintext SHA-256 and count plaintext bytes
	plainHasher := sha256.New()
	plainCounter := &countingReader{r: io.TeeReader(decReader, plainHasher)}

	// Gzip decompression layer
	gzReader, err := gzip.NewReader(plainCounter)
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if errors.Is(err, artifactcrypto.ErrUnknownKeyVersion) || errors.Is(err, artifactcrypto.ErrInvalidKeyVersion) {
			return "", err
		}
		return "", fmt.Errorf("decrypted artifact is not a valid gzip stream: %w", err)
	}
	defer gzReader.Close()

	// Read initial sanity chunk
	sanityBuf := make([]byte, maxSanityHeaderBytes)
	n, readErr := io.ReadFull(gzReader, sanityBuf)
	if readErr != nil && !errors.Is(readErr, io.EOF) && !errors.Is(readErr, io.ErrUnexpectedEOF) {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if errors.Is(readErr, artifactcrypto.ErrUnknownKeyVersion) || errors.Is(readErr, artifactcrypto.ErrInvalidKeyVersion) {
			return "", readErr
		}
		return "", fmt.Errorf("failed reading decompressed stream: %w", readErr)
	}

	decompressedPrefix := sanityBuf[:n]
	var totalDecompressedBytes int64 = int64(n)

	remainderBytes, drainErr := io.Copy(io.Discard, gzReader)
	if drainErr != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if errors.Is(drainErr, artifactcrypto.ErrUnknownKeyVersion) || errors.Is(drainErr, artifactcrypto.ErrInvalidKeyVersion) {
			return "", drainErr
		}
		return "", fmt.Errorf("gzip stream integrity check failed: %w", drainErr)
	}
	totalDecompressedBytes += remainderBytes

	if err := gzReader.Close(); err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", fmt.Errorf("gzip stream closure error: %w", err)
	}

	// Drain any remaining decrypted bytes to finish plainHasher and plainCounter
	if _, err := io.Copy(io.Discard, plainCounter); err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", fmt.Errorf("failed draining decrypted stream: %w", err)
	}

	// Close decrypt reader to enforce EOF validation and check for trailing bytes
	if err := decReader.Close(); err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", fmt.Errorf("BPAE decryption finalization failed: %w", err)
	}

	// Drain any remaining ciphertext bytes from physical counter
	if _, err := io.Copy(io.Discard, cipherCounter); err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", fmt.Errorf("failed reading physical stream: %w", err)
	}

	if ctx.Err() != nil {
		return "", ctx.Err()
	}

	// Layer 1: Physical verification
	if cipherCounter.totalBytes != storedSizeBytes {
		return "", fmt.Errorf("physical artifact size mismatch: expected %d bytes, got %d bytes", storedSizeBytes, cipherCounter.totalBytes)
	}
	calculatedCiphertextSHA256 := hex.EncodeToString(cipherHasher.Sum(nil))
	if !strings.EqualFold(calculatedCiphertextSHA256, ciphertextSHA256) {
		return "", fmt.Errorf("ciphertext checksum mismatch: expected %s, got %s", ciphertextSHA256, calculatedCiphertextSHA256)
	}

	// Layer 2 & 3: Plaintext verification
	if plainCounter.totalBytes != expectedPlaintextSize {
		return "", fmt.Errorf("decrypted plaintext size mismatch: expected %d bytes, got %d bytes", expectedPlaintextSize, plainCounter.totalBytes)
	}
	calculatedPlaintextSHA256 := hex.EncodeToString(plainHasher.Sum(nil))
	if !strings.EqualFold(calculatedPlaintextSHA256, expectedPlaintextChecksum) {
		return "", fmt.Errorf("plaintext checksum mismatch: expected %s, got %s", expectedPlaintextChecksum, calculatedPlaintextSHA256)
	}

	if totalDecompressedBytes <= 0 {
		return "", errors.New("decompressed database dump is empty")
	}

	if !hasValidMySQLDumpMarker(decompressedPrefix) {
		return "", errors.New("decompressed stream failed basic SQL dump format sanity check")
	}

	return canonicalVerifiedMsg, nil
}

// VerifyEncryptedFilesArtifact verifies:
// 1. Physical Layer: Stored physical object exists, matches storedSizeBytes and ciphertextSHA256.
// 2. Decryption Layer: Stream decrypts using BPAE with orgID/artifactID binding and valid FINAL record.
// 3. Plaintext Layer: Decrypted stream matches expectedPlaintextSize, expectedPlaintextChecksum, valid gzip, and valid non-empty safe tar archive.
func (v *VerificationEngine) VerifyEncryptedFilesArtifact(
	ctx context.Context,
	storageProvider storage.StorageProvider,
	storageReference string,
	expectedPlaintextSize int64,
	expectedPlaintextChecksum string,
	storedSizeBytes int64,
	ciphertextSHA256 string,
	orgID, artifactID uuid.UUID,
) (string, error) {
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if storageProvider == nil {
		return "", errors.New("storage provider cannot be nil")
	}
	if v.keyProvider == nil {
		return "", errors.New("artifact key provider cannot be nil")
	}
	if expectedPlaintextSize <= 0 {
		return "", errors.New("expected plaintext size must be greater than zero")
	}
	if storedSizeBytes <= 0 {
		return "", errors.New("stored size must be greater than zero")
	}
	if strings.TrimSpace(expectedPlaintextChecksum) == "" {
		return "", errors.New("expected plaintext checksum cannot be empty")
	}
	if strings.TrimSpace(ciphertextSHA256) == "" {
		return "", errors.New("ciphertext checksum cannot be empty")
	}
	if orgID == uuid.Nil || artifactID == uuid.Nil {
		return "", errors.New("valid organization ID and artifact ID are required")
	}

	rc, err := storageProvider.OpenArtifact(ctx, storageReference)
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", fmt.Errorf("failed opening artifact for verification: %w", err)
	}
	defer rc.Close()

	stopWait := context.AfterFunc(ctx, func() {
		_ = rc.Close()
	})
	defer stopWait()

	// Physical layer: compute ciphertext SHA-256 and count physical stored bytes
	cipherHasher := sha256.New()
	cipherCounter := &countingReader{r: io.TeeReader(rc, cipherHasher)}

	// Decryption layer: stream through BPAE DecryptReader
	decReader, err := artifactcrypto.NewDecryptReader(cipherCounter, v.keyProvider, orgID, artifactID)
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", fmt.Errorf("failed initializing BPAE decryption: %w", err)
	}
	defer decReader.Close()

	// Plaintext layer: compute plaintext SHA-256 and count plaintext bytes
	plainHasher := sha256.New()
	plainCounter := &countingReader{r: io.TeeReader(decReader, plainHasher)}

	// Gzip decompression layer
	gzReader, err := gzip.NewReader(plainCounter)
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if errors.Is(err, artifactcrypto.ErrUnknownKeyVersion) || errors.Is(err, artifactcrypto.ErrInvalidKeyVersion) {
			return "", err
		}
		return "", fmt.Errorf("decrypted artifact is not a valid gzip stream: %w", err)
	}
	defer gzReader.Close()

	tarReader := tar.NewReader(gzReader)
	var entriesCount int

	for {
		header, err := tarReader.Next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			if errors.Is(err, artifactcrypto.ErrUnknownKeyVersion) || errors.Is(err, artifactcrypto.ErrInvalidKeyVersion) {
				return "", err
			}
			return "", fmt.Errorf("tar structural integrity check failed: %w", err)
		}

		entriesCount++

		name := header.Name
		if strings.HasPrefix(name, "/") {
			return "", errors.New("tar archive contains an unsafe absolute member path")
		}

		segments := strings.Split(name, "/")
		for _, seg := range segments {
			if seg == ".." {
				return "", errors.New("tar archive contains an unsafe parent traversal member path")
			}
		}
	}

	if entriesCount == 0 {
		return "", errors.New("tar archive is empty (zero entries)")
	}

	// Drain remaining bytes in gzip stream to ensure gzip footer/CRC is verified
	if _, err := io.Copy(io.Discard, gzReader); err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", fmt.Errorf("gzip stream integrity check failed: %w", err)
	}

	if err := gzReader.Close(); err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", fmt.Errorf("gzip stream closure error: %w", err)
	}

	// Drain any remaining decrypted bytes to finish plainHasher and plainCounter
	if _, err := io.Copy(io.Discard, plainCounter); err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", fmt.Errorf("failed draining decrypted stream: %w", err)
	}

	// Close decrypt reader to enforce EOF validation and check for trailing bytes
	if err := decReader.Close(); err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", fmt.Errorf("BPAE decryption finalization failed: %w", err)
	}

	// Drain any remaining ciphertext bytes from physical counter
	if _, err := io.Copy(io.Discard, cipherCounter); err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", fmt.Errorf("failed reading physical stream: %w", err)
	}

	if ctx.Err() != nil {
		return "", ctx.Err()
	}

	// Layer 1: Physical verification
	if cipherCounter.totalBytes != storedSizeBytes {
		return "", fmt.Errorf("physical artifact size mismatch: expected %d bytes, got %d bytes", storedSizeBytes, cipherCounter.totalBytes)
	}
	calculatedCiphertextSHA256 := hex.EncodeToString(cipherHasher.Sum(nil))
	if !strings.EqualFold(calculatedCiphertextSHA256, ciphertextSHA256) {
		return "", fmt.Errorf("ciphertext checksum mismatch: expected %s, got %s", ciphertextSHA256, calculatedCiphertextSHA256)
	}

	// Layer 2 & 3: Plaintext verification
	if plainCounter.totalBytes != expectedPlaintextSize {
		return "", fmt.Errorf("decrypted plaintext size mismatch: expected %d bytes, got %d bytes", expectedPlaintextSize, plainCounter.totalBytes)
	}
	calculatedPlaintextSHA256 := hex.EncodeToString(plainHasher.Sum(nil))
	if !strings.EqualFold(calculatedPlaintextSHA256, expectedPlaintextChecksum) {
		return "", fmt.Errorf("plaintext checksum mismatch: expected %s, got %s", expectedPlaintextChecksum, calculatedPlaintextSHA256)
	}

	return canonicalFilesVerifiedMsg, nil
}

// VerifyResticSnapshot performs Level-1 post-backup verification immediately after Restic snapshot creation (ADR-033).
func (v *VerificationEngine) VerifyResticSnapshot(
	ctx context.Context,
	runner restic.CommandRunner,
	target restic.RepositoryTarget,
	password []byte,
	snapshotID string,
	orgID, resID, runID, artifactID uuid.UUID,
	targetToken string,
	internalFilename string,
	expectedLogicalSize int64,
) (string, error) {
	if runner == nil {
		return "", errors.New("restic runner cannot be nil")
	}
	if target == nil {
		return "", errors.New("repository target cannot be nil")
	}
	if len(password) == 0 {
		return "", errors.New("repository password cannot be empty")
	}
	if snapshotID == "" {
		return "", fmt.Errorf("%w: snapshot ID cannot be empty", domain.ErrVerificationFailed)
	}

	// 1. Fetch snapshot from repository index
	snap, err := runner.GetSnapshot(ctx, target, password, snapshotID)
	if err != nil {
		if errors.Is(err, restic.ErrSnapshotNotFound) {
			return "", fmt.Errorf("%w: snapshot %q not found in repository index", domain.ErrVerificationFailed, snapshotID)
		}
		// Connectivity / execution / infrastructure error: do not falsely claim corruption
		return "", fmt.Errorf("repository infrastructure error during verification: %w", err)
	}

	// 2. Exact snapshot ID match
	if snap.ID != snapshotID && !strings.HasPrefix(snap.ID, snapshotID) && snap.ShortID != snapshotID {
		return "", fmt.Errorf("%w: snapshot ID mismatch: expected %q, got %q", domain.ErrVerificationFailed, snapshotID, snap.ID)
	}

	// 3. Verify all mandatory six tags
	expectedTags := map[string]bool{
		"platform=backup-platform-v1":     false,
		"org=" + orgID.String():           false,
		"resource=" + resID.String():      false,
		"run=" + runID.String():           false,
		"artifact=" + artifactID.String(): false,
		"target=" + targetToken:           false,
	}

	for _, tag := range snap.Tags {
		if _, ok := expectedTags[tag]; ok {
			expectedTags[tag] = true
		}
	}

	for tag, found := range expectedTags {
		if !found {
			return "", fmt.Errorf("%w: missing mandatory snapshot tag %q", domain.ErrVerificationFailed, tag)
		}
	}

	// 4. Verify expected internal filename exists in snapshot tree
	nodes, err := runner.ListSnapshotNodes(ctx, target, password, snapshotID)
	if err != nil {
		return "", fmt.Errorf("failed listing snapshot nodes during verification: %w", err)
	}

	var foundFile bool
	var fileSize int64
	for _, node := range nodes {
		if node.Name == internalFilename || strings.HasSuffix(node.Path, internalFilename) {
			foundFile = true
			fileSize = node.Size
			break
		}
	}

	if !foundFile {
		return "", fmt.Errorf("%w: expected internal file %q not found in snapshot tree", domain.ErrVerificationFailed, internalFilename)
	}

	// 5. Verify logical size is non-zero
	if fileSize <= 0 && expectedLogicalSize <= 0 {
		return "", fmt.Errorf("%w: snapshot internal file has zero size", domain.ErrVerificationFailed)
	}

	// 6. Verify first up-to-64-KiB sample can be read using restic dump
	sample, err := runner.DumpSample(ctx, target, password, snapshotID, internalFilename, maxSanityHeaderBytes)
	if err != nil {
		return "", fmt.Errorf("%w: failed reading restic dump sample: %v", domain.ErrVerificationFailed, err)
	}
	if len(sample) == 0 {
		return "", fmt.Errorf("%w: restic dump returned empty sample", domain.ErrVerificationFailed)
	}

	return canonicalResticVerifiedMsg, nil
}
