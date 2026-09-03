package verification

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"backup-platform/internal/artifactcrypto"
	"backup-platform/internal/storage"
	"backup-platform/internal/storage/local"
	"backup-platform/pkg/uuid"
)

func createGzipData(t *testing.T, rawContent []byte) ([]byte, string) {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	if _, err := gw.Write(rawContent); err != nil {
		t.Fatalf("failed writing gzip: %v", err)
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("failed closing gzip: %v", err)
	}

	compressed := buf.Bytes()
	hasher := sha256.New()
	hasher.Write(compressed)
	return compressed, hex.EncodeToString(hasher.Sum(nil))
}

func TestVerificationEngine_VerifyDatabaseArtifact_Success(t *testing.T) {
	tempDir := t.TempDir()
	storageProvider, _ := local.NewLocalStorageProvider(tempDir)
	ctx := context.Background()
	_ = storageProvider.EnsureStorageRoot(ctx)

	rawSQL := "-- MySQL dump 10.13  Distrib 8.0.32\nCREATE DATABASE `prod_db`;\nUSE `prod_db`;\n" + strings.Repeat("INSERT INTO t VALUES (1);\n", 500)
	compressed, checksum := createGzipData(t, []byte(rawSQL))

	orgID := uuid.New()
	resID := uuid.New()
	runID := uuid.New()
	artID := uuid.New()

	saveRes, err := storageProvider.SaveArtifact(ctx, orgID, resID, runID, artID, ".sql.gz", bytes.NewReader(compressed))
	if err != nil {
		t.Fatalf("failed saving artifact: %v", err)
	}

	verifier := NewVerificationEngine()
	details, err := verifier.VerifyDatabaseArtifact(ctx, storageProvider, saveRes.StorageReference, int64(len(compressed)), checksum)
	if err != nil {
		t.Fatalf("unexpected verification failure: %v", err)
	}

	if details != canonicalVerifiedMsg {
		t.Errorf("expected canonical details %q, got %q", canonicalVerifiedMsg, details)
	}
}

func TestVerificationEngine_VerifyDatabaseArtifact_SizeMismatch(t *testing.T) {
	tempDir := t.TempDir()
	storageProvider, _ := local.NewLocalStorageProvider(tempDir)
	ctx := context.Background()
	_ = storageProvider.EnsureStorageRoot(ctx)

	rawSQL := "-- MariaDB dump 10.19\nCREATE DATABASE `prod_db`;\n"
	compressed, checksum := createGzipData(t, []byte(rawSQL))

	orgID := uuid.New()
	resID := uuid.New()
	runID := uuid.New()
	artID := uuid.New()

	saveRes, _ := storageProvider.SaveArtifact(ctx, orgID, resID, runID, artID, ".sql.gz", bytes.NewReader(compressed))

	verifier := NewVerificationEngine()
	// Pass wrong size
	_, err := verifier.VerifyDatabaseArtifact(ctx, storageProvider, saveRes.StorageReference, int64(len(compressed))+100, checksum)
	if err == nil {
		t.Fatalf("expected error on size mismatch")
	}
	if !strings.Contains(err.Error(), "artifact size mismatch") {
		t.Errorf("expected size mismatch error message, got %v", err)
	}
}

func TestVerificationEngine_VerifyDatabaseArtifact_ChecksumMismatch(t *testing.T) {
	tempDir := t.TempDir()
	storageProvider, _ := local.NewLocalStorageProvider(tempDir)
	ctx := context.Background()
	_ = storageProvider.EnsureStorageRoot(ctx)

	rawSQL := "-- MySQL dump 10.13\n"
	compressed, _ := createGzipData(t, []byte(rawSQL))

	orgID := uuid.New()
	resID := uuid.New()
	runID := uuid.New()
	artID := uuid.New()

	saveRes, _ := storageProvider.SaveArtifact(ctx, orgID, resID, runID, artID, ".sql.gz", bytes.NewReader(compressed))

	verifier := NewVerificationEngine()
	// Pass wrong checksum
	wrongChecksum := strings.Repeat("0", 64)
	_, err := verifier.VerifyDatabaseArtifact(ctx, storageProvider, saveRes.StorageReference, int64(len(compressed)), wrongChecksum)
	if err == nil {
		t.Fatalf("expected error on checksum mismatch")
	}
	if !strings.Contains(err.Error(), "checksum mismatch") {
		t.Errorf("expected checksum mismatch error, got %v", err)
	}
}

func TestVerificationEngine_VerifyDatabaseArtifact_CorruptGzip(t *testing.T) {
	tempDir := t.TempDir()
	storageProvider, _ := local.NewLocalStorageProvider(tempDir)
	ctx := context.Background()
	_ = storageProvider.EnsureStorageRoot(ctx)

	// Corrupt bytes (not valid gzip)
	corruptBytes := []byte("this is not a valid gzip stream at all!")
	hasher := sha256.New()
	hasher.Write(corruptBytes)
	checksum := hex.EncodeToString(hasher.Sum(nil))

	orgID := uuid.New()
	resID := uuid.New()
	runID := uuid.New()
	artID := uuid.New()

	saveRes, _ := storageProvider.SaveArtifact(ctx, orgID, resID, runID, artID, ".sql.gz", bytes.NewReader(corruptBytes))

	verifier := NewVerificationEngine()
	_, err := verifier.VerifyDatabaseArtifact(ctx, storageProvider, saveRes.StorageReference, int64(len(corruptBytes)), checksum)
	if err == nil {
		t.Fatalf("expected error on corrupt gzip stream")
	}
}

func TestVerificationEngine_VerifyDatabaseArtifact_SanityHeaderCheckFail(t *testing.T) {
	tempDir := t.TempDir()
	storageProvider, _ := local.NewLocalStorageProvider(tempDir)
	ctx := context.Background()
	_ = storageProvider.EnsureStorageRoot(ctx)

	// Valid gzip, but content is random non-SQL text
	nonSQLContent := []byte("hello world this is plain text without any sql keywords or headers")
	compressed, checksum := createGzipData(t, nonSQLContent)

	orgID := uuid.New()
	resID := uuid.New()
	runID := uuid.New()
	artID := uuid.New()

	saveRes, _ := storageProvider.SaveArtifact(ctx, orgID, resID, runID, artID, ".sql.gz", bytes.NewReader(compressed))

	verifier := NewVerificationEngine()
	_, err := verifier.VerifyDatabaseArtifact(ctx, storageProvider, saveRes.StorageReference, int64(len(compressed)), checksum)
	if err == nil {
		t.Fatalf("expected sanity header check error for non-SQL content")
	}
	if !strings.Contains(err.Error(), "sanity check") {
		t.Errorf("expected sanity check error message, got %v", err)
	}
}

func TestVerificationEngine_VerifyDatabaseArtifact_ContextCancellation(t *testing.T) {
	tempDir := t.TempDir()
	storageProvider, _ := local.NewLocalStorageProvider(tempDir)
	ctx := context.Background()
	_ = storageProvider.EnsureStorageRoot(ctx)

	rawSQL := "-- MySQL dump 10.13\n" + strings.Repeat("SELECT 1;\n", 1000)
	compressed, checksum := createGzipData(t, []byte(rawSQL))

	orgID := uuid.New()
	resID := uuid.New()
	runID := uuid.New()
	artID := uuid.New()

	saveRes, _ := storageProvider.SaveArtifact(ctx, orgID, resID, runID, artID, ".sql.gz", bytes.NewReader(compressed))

	verifier := NewVerificationEngine()
	cancelCtx, cancel := context.WithCancel(ctx)
	cancel() // Cancel immediately

	_, err := verifier.VerifyDatabaseArtifact(cancelCtx, storageProvider, saveRes.StorageReference, int64(len(compressed)), checksum)
	if err == nil {
		t.Fatalf("expected error on cancelled context")
	}
}

type blockingStorageProvider struct {
	data        []byte
	readStarted chan struct{}
}

func (b *blockingStorageProvider) SaveArtifact(ctx context.Context, orgID, resID, runID, artifactID uuid.UUID, extension string, src io.Reader) (*storage.SaveResult, error) {
	return nil, nil
}
func (b *blockingStorageProvider) DeleteArtifact(ctx context.Context, storageReference string) error {
	return nil
}
func (b *blockingStorageProvider) EnsureStorageRoot(ctx context.Context) error {
	return nil
}
func (b *blockingStorageProvider) OpenArtifact(ctx context.Context, storageReference string) (io.ReadCloser, error) {
	return &blockingReadCloser{
		data:        b.data,
		readStarted: b.readStarted,
		closedCh:    make(chan struct{}),
	}, nil
}

type blockingReadCloser struct {
	data        []byte
	readStarted chan struct{}

	mu         sync.Mutex
	offset     int
	closed     bool
	closedCh   chan struct{}
	closeOnce  sync.Once
	signalOnce sync.Once
}

func (br *blockingReadCloser) Read(p []byte) (int, error) {
	br.mu.Lock()
	if br.closed {
		br.mu.Unlock()
		return 0, errors.New("reader closed")
	}

	if br.offset == 0 && len(br.data) > 0 {
		n := copy(p, br.data[:10]) // read small initial chunk
		br.offset += n
		br.mu.Unlock()
		return n, nil
	}
	br.mu.Unlock()

	// Signal that the blocking read phase has started
	if br.readStarted != nil {
		br.signalOnce.Do(func() {
			close(br.readStarted)
		})
	}

	// Block until Close() is called concurrently
	<-br.closedCh
	return 0, errors.New("reader closed")
}

func (br *blockingReadCloser) Close() error {
	br.mu.Lock()
	br.closed = true
	br.mu.Unlock()

	br.closeOnce.Do(func() {
		close(br.closedCh)
	})
	return nil
}

func TestVerificationEngine_VerifyDatabaseArtifact_MidStreamCancellation(t *testing.T) {
	rawSQL := "-- MySQL dump 10.13\n" + strings.Repeat("SELECT 1;\n", 1000)
	compressed, checksum := createGzipData(t, []byte(rawSQL))

	readStarted := make(chan struct{})
	fakeStorage := &blockingStorageProvider{
		data:        compressed,
		readStarted: readStarted,
	}
	verifier := NewVerificationEngine()

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)

	go func() {
		_, err := verifier.VerifyDatabaseArtifact(ctx, fakeStorage, "valid_ref", int64(len(compressed)), checksum)
		errCh <- err
	}()

	// Wait deterministically for the blocking read to start (no fixed sleep)
	select {
	case <-readStarted:
	case <-time.After(1 * time.Second):
		t.Fatalf("timed out waiting for blocking read to start")
	}

	cancel()

	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("expected context.Canceled, got: %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatalf("verification did not cancel promptly mid-stream")
	}
}

func createTarGzipData(t *testing.T, entries map[string][]byte) ([]byte, string) {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	for name, content := range entries {
		hdr := &tar.Header{
			Name: name,
			Mode: 0644,
			Size: int64(len(content)),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("failed writing tar header: %v", err)
		}
		if len(content) > 0 {
			if _, err := tw.Write(content); err != nil {
				t.Fatalf("failed writing tar content: %v", err)
			}
		}
	}

	if err := tw.Close(); err != nil {
		t.Fatalf("failed closing tar writer: %v", err)
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("failed closing gzip writer: %v", err)
	}

	compressed := buf.Bytes()
	hasher := sha256.New()
	hasher.Write(compressed)
	return compressed, hex.EncodeToString(hasher.Sum(nil))
}

func TestVerificationEngine_VerifyFilesArtifact_Success(t *testing.T) {
	tempDir := t.TempDir()
	storageProvider, _ := local.NewLocalStorageProvider(tempDir)
	ctx := context.Background()
	_ = storageProvider.EnsureStorageRoot(ctx)

	entries := map[string][]byte{
		"index.php":     []byte("<?php echo 'hello'; ?>"),
		"wp-config.php": []byte("<?php define('DB_NAME', 'wp'); ?>"),
		"assets/app.js": []byte("console.log('test');"),
	}
	compressed, checksum := createTarGzipData(t, entries)

	orgID := uuid.New()
	resID := uuid.New()
	runID := uuid.New()
	artID := uuid.New()

	saveRes, err := storageProvider.SaveArtifact(ctx, orgID, resID, runID, artID, ".tar.gz", bytes.NewReader(compressed))
	if err != nil {
		t.Fatalf("failed saving artifact: %v", err)
	}

	verifier := NewVerificationEngine()
	details, err := verifier.VerifyFilesArtifact(ctx, storageProvider, saveRes.StorageReference, int64(len(compressed)), checksum)
	if err != nil {
		t.Fatalf("unexpected verification failure: %v", err)
	}

	if details != canonicalFilesVerifiedMsg {
		t.Errorf("expected canonical details %q, got %q", canonicalFilesVerifiedMsg, details)
	}
}

func TestVerificationEngine_VerifyFilesArtifact_EmptyTarArchive(t *testing.T) {
	tempDir := t.TempDir()
	storageProvider, _ := local.NewLocalStorageProvider(tempDir)
	ctx := context.Background()
	_ = storageProvider.EnsureStorageRoot(ctx)

	// Zero entries
	compressed, checksum := createTarGzipData(t, map[string][]byte{})

	orgID := uuid.New()
	resID := uuid.New()
	runID := uuid.New()
	artID := uuid.New()

	saveRes, _ := storageProvider.SaveArtifact(ctx, orgID, resID, runID, artID, ".tar.gz", bytes.NewReader(compressed))

	verifier := NewVerificationEngine()
	_, err := verifier.VerifyFilesArtifact(ctx, storageProvider, saveRes.StorageReference, int64(len(compressed)), checksum)
	if err == nil {
		t.Fatalf("expected error for empty tar archive")
	}
	if !strings.Contains(err.Error(), "zero entries") {
		t.Errorf("expected zero entries error, got: %v", err)
	}
}

func TestVerificationEngine_VerifyFilesArtifact_AbsoluteMemberRejection(t *testing.T) {
	tempDir := t.TempDir()
	storageProvider, _ := local.NewLocalStorageProvider(tempDir)
	ctx := context.Background()
	_ = storageProvider.EnsureStorageRoot(ctx)

	entries := map[string][]byte{
		"/etc/passwd": []byte("root:x:0:0:..."),
	}
	compressed, checksum := createTarGzipData(t, entries)

	saveRes, _ := storageProvider.SaveArtifact(ctx, uuid.New(), uuid.New(), uuid.New(), uuid.New(), ".tar.gz", bytes.NewReader(compressed))

	verifier := NewVerificationEngine()
	_, err := verifier.VerifyFilesArtifact(ctx, storageProvider, saveRes.StorageReference, int64(len(compressed)), checksum)
	if err == nil {
		t.Fatalf("expected error for absolute member path")
	}
	if !strings.Contains(err.Error(), "unsafe absolute member path") {
		t.Errorf("expected unsafe absolute member path error, got: %v", err)
	}
}

func TestVerificationEngine_VerifyFilesArtifact_ParentTraversalMemberRejection(t *testing.T) {
	tempDir := t.TempDir()
	storageProvider, _ := local.NewLocalStorageProvider(tempDir)
	ctx := context.Background()
	_ = storageProvider.EnsureStorageRoot(ctx)

	entries := map[string][]byte{
		"var/www/../../etc/shadow": []byte("secret"),
	}
	compressed, checksum := createTarGzipData(t, entries)

	saveRes, _ := storageProvider.SaveArtifact(ctx, uuid.New(), uuid.New(), uuid.New(), uuid.New(), ".tar.gz", bytes.NewReader(compressed))

	verifier := NewVerificationEngine()
	_, err := verifier.VerifyFilesArtifact(ctx, storageProvider, saveRes.StorageReference, int64(len(compressed)), checksum)
	if err == nil {
		t.Fatalf("expected error for parent traversal member path")
	}
	if !strings.Contains(err.Error(), "unsafe parent traversal member path") {
		t.Errorf("expected unsafe parent traversal member path error, got: %v", err)
	}
}

func TestVerificationEngine_VerifyFilesArtifact_MidStreamCancellation(t *testing.T) {
	entries := map[string][]byte{
		"index.php": []byte(strings.Repeat("echo 'test';\n", 1000)),
	}
	compressed, checksum := createTarGzipData(t, entries)

	readStarted := make(chan struct{})
	fakeStorage := &blockingStorageProvider{
		data:        compressed,
		readStarted: readStarted,
	}
	verifier := NewVerificationEngine()

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)

	go func() {
		_, err := verifier.VerifyFilesArtifact(ctx, fakeStorage, "valid_ref", int64(len(compressed)), checksum)
		errCh <- err
	}()

	select {
	case <-readStarted:
	case <-time.After(1 * time.Second):
		t.Fatalf("timed out waiting for blocking read to start")
	}

	cancel()

	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("expected context.Canceled, got: %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatalf("verification did not cancel promptly mid-stream")
	}
}

type memoryArtifactStorage struct {
	data []byte
}

func (m *memoryArtifactStorage) SaveArtifact(ctx context.Context, orgID, resID, runID, artifactID uuid.UUID, ext string, r io.Reader) (*storage.SaveResult, error) {
	return nil, errors.New("not implemented")
}

func (m *memoryArtifactStorage) OpenArtifact(ctx context.Context, storageRef string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(m.data)), nil
}

func (m *memoryArtifactStorage) DeleteArtifact(ctx context.Context, storageRef string) error {
	return nil
}

func (m *memoryArtifactStorage) EnsureStorageRoot(ctx context.Context) error {
	return nil
}

type countingWriterTarget struct {
	w io.Writer
	h io.Writer
}

func (c *countingWriterTarget) Write(p []byte) (int, error) {
	_, _ = c.h.Write(p)
	return c.w.Write(p)
}

func createEncryptedBPAEData(t *testing.T, plaintext []byte, kp artifactcrypto.KeyProvider, orgID, artID uuid.UUID) ([]byte, string, int64, string) {
	t.Helper()
	var bpaeBuf bytes.Buffer
	encWriter, err := artifactcrypto.NewEncryptWriter(&bpaeBuf, kp, orgID, artID)
	if err != nil {
		t.Fatalf("failed creating encrypt writer: %v", err)
	}

	plainHasher := sha256.New()
	plainCounter := &countingWriterTarget{w: encWriter, h: plainHasher}

	_, err = plainCounter.Write(plaintext)
	if err != nil {
		t.Fatalf("failed writing plaintext: %v", err)
	}
	if err := encWriter.Close(); err != nil {
		t.Fatalf("failed closing encWriter: %v", err)
	}

	ciphertext := bpaeBuf.Bytes()
	cipherHasher := sha256.New()
	cipherHasher.Write(ciphertext)

	return ciphertext, hex.EncodeToString(cipherHasher.Sum(nil)), int64(len(plaintext)), hex.EncodeToString(plainHasher.Sum(nil))
}

func testArtifactKeyProvider(t *testing.T) artifactcrypto.KeyProvider {
	t.Helper()
	kp, err := artifactcrypto.NewStaticKeyProvider(bytes.Repeat([]byte{0x77}, 32), 1)
	if err != nil {
		t.Fatalf("failed creating test key provider: %v", err)
	}
	return kp
}

func TestVerificationEngine_VerifyEncryptedDatabaseArtifact_Success(t *testing.T) {
	kp := testArtifactKeyProvider(t)
	rawSQL := "-- MySQL dump 10.13\n" + strings.Repeat("INSERT INTO users VALUES (1, 'alice');\n", 500)
	gzipData, _ := createGzipData(t, []byte(rawSQL))

	orgID := uuid.New()
	artID := uuid.New()
	cipherBytes, cipherHash, plainSize, plainHash := createEncryptedBPAEData(t, gzipData, kp, orgID, artID)

	fakeStorage := &memoryArtifactStorage{data: cipherBytes}
	verifier := NewVerificationEngineWithKeyProvider(kp)

	msg, err := verifier.VerifyEncryptedDatabaseArtifact(
		context.Background(),
		fakeStorage,
		"ref",
		plainSize,
		plainHash,
		int64(len(cipherBytes)),
		cipherHash,
		orgID,
		artID,
	)
	if err != nil {
		t.Fatalf("expected verification success, got error: %v", err)
	}
	if msg != canonicalVerifiedMsg {
		t.Errorf("expected '%s', got '%s'", canonicalVerifiedMsg, msg)
	}
}

func TestVerificationEngine_VerifyEncryptedDatabaseArtifact_PhysicalSizeMismatch(t *testing.T) {
	kp := testArtifactKeyProvider(t)
	rawSQL := "-- MySQL dump 10.13\n" + strings.Repeat("INSERT INTO users VALUES (1, 'alice');\n", 500)
	gzipData, _ := createGzipData(t, []byte(rawSQL))

	orgID := uuid.New()
	artID := uuid.New()
	cipherBytes, cipherHash, plainSize, plainHash := createEncryptedBPAEData(t, gzipData, kp, orgID, artID)

	fakeStorage := &memoryArtifactStorage{data: cipherBytes}
	verifier := NewVerificationEngineWithKeyProvider(kp)

	_, err := verifier.VerifyEncryptedDatabaseArtifact(
		context.Background(),
		fakeStorage,
		"ref",
		plainSize,
		plainHash,
		int64(len(cipherBytes))+10, // wrong stored size
		cipherHash,
		orgID,
		artID,
	)
	if err == nil {
		t.Fatal("expected failure on physical size mismatch, got nil")
	}
	if !strings.Contains(err.Error(), "physical artifact size mismatch") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestVerificationEngine_VerifyEncryptedDatabaseArtifact_CiphertextChecksumMismatch(t *testing.T) {
	kp := testArtifactKeyProvider(t)
	rawSQL := "-- MySQL dump 10.13\n" + strings.Repeat("INSERT INTO users VALUES (1, 'alice');\n", 500)
	gzipData, _ := createGzipData(t, []byte(rawSQL))

	orgID := uuid.New()
	artID := uuid.New()
	cipherBytes, _, plainSize, plainHash := createEncryptedBPAEData(t, gzipData, kp, orgID, artID)

	wrongCipherHash := strings.Repeat("0", 64)
	fakeStorage := &memoryArtifactStorage{data: cipherBytes}
	verifier := NewVerificationEngineWithKeyProvider(kp)

	_, err := verifier.VerifyEncryptedDatabaseArtifact(
		context.Background(),
		fakeStorage,
		"ref",
		plainSize,
		plainHash,
		int64(len(cipherBytes)),
		wrongCipherHash,
		orgID,
		artID,
	)
	if err == nil {
		t.Fatal("expected failure on ciphertext hash mismatch, got nil")
	}
	if !strings.Contains(err.Error(), "ciphertext checksum mismatch") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestVerificationEngine_VerifyEncryptedDatabaseArtifact_PlaintextChecksumMismatch(t *testing.T) {
	kp := testArtifactKeyProvider(t)
	rawSQL := "-- MySQL dump 10.13\n" + strings.Repeat("INSERT INTO users VALUES (1, 'alice');\n", 500)
	gzipData, _ := createGzipData(t, []byte(rawSQL))

	orgID := uuid.New()
	artID := uuid.New()
	cipherBytes, cipherHash, plainSize, _ := createEncryptedBPAEData(t, gzipData, kp, orgID, artID)

	wrongPlainHash := strings.Repeat("f", 64)
	fakeStorage := &memoryArtifactStorage{data: cipherBytes}
	verifier := NewVerificationEngineWithKeyProvider(kp)

	_, err := verifier.VerifyEncryptedDatabaseArtifact(
		context.Background(),
		fakeStorage,
		"ref",
		plainSize,
		wrongPlainHash,
		int64(len(cipherBytes)),
		cipherHash,
		orgID,
		artID,
	)
	if err == nil {
		t.Fatal("expected failure on plaintext hash mismatch, got nil")
	}
	if !strings.Contains(err.Error(), "plaintext checksum mismatch") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestVerificationEngine_VerifyEncryptedDatabaseArtifact_CorruptedBPAEData(t *testing.T) {
	kp := testArtifactKeyProvider(t)
	rawSQL := "-- MySQL dump 10.13\n" + strings.Repeat("INSERT INTO users VALUES (1, 'alice');\n", 500)
	gzipData, _ := createGzipData(t, []byte(rawSQL))

	orgID := uuid.New()
	artID := uuid.New()
	cipherBytes, _, plainSize, plainHash := createEncryptedBPAEData(t, gzipData, kp, orgID, artID)

	// Tamper with ciphertext in the DATA chunk (byte 115)
	corruptedCipher := make([]byte, len(cipherBytes))
	copy(corruptedCipher, cipherBytes)
	corruptedCipher[115] ^= 0xff

	corruptHash := sha256.Sum256(corruptedCipher)
	fakeStorage := &memoryArtifactStorage{data: corruptedCipher}
	verifier := NewVerificationEngineWithKeyProvider(kp)

	_, err := verifier.VerifyEncryptedDatabaseArtifact(
		context.Background(),
		fakeStorage,
		"ref",
		plainSize,
		plainHash,
		int64(len(corruptedCipher)),
		hex.EncodeToString(corruptHash[:]),
		orgID,
		artID,
	)
	if err == nil {
		t.Fatal("expected failure on tampered BPAE ciphertext, got nil")
	}
}

func TestVerificationEngine_VerifyEncryptedDatabaseArtifact_MissingFINAL(t *testing.T) {
	kp := testArtifactKeyProvider(t)
	rawSQL := "-- MySQL dump 10.13\n" + strings.Repeat("INSERT INTO users VALUES (1, 'alice');\n", 500)
	gzipData, _ := createGzipData(t, []byte(rawSQL))

	orgID := uuid.New()
	artID := uuid.New()
	cipherBytes, _, plainSize, plainHash := createEncryptedBPAEData(t, gzipData, kp, orgID, artID)

	// Truncate the 41-byte FINAL record
	truncatedCipher := cipherBytes[:len(cipherBytes)-41]
	truncHash := sha256.Sum256(truncatedCipher)
	fakeStorage := &memoryArtifactStorage{data: truncatedCipher}
	verifier := NewVerificationEngineWithKeyProvider(kp)

	_, err := verifier.VerifyEncryptedDatabaseArtifact(
		context.Background(),
		fakeStorage,
		"ref",
		plainSize,
		plainHash,
		int64(len(truncatedCipher)),
		hex.EncodeToString(truncHash[:]),
		orgID,
		artID,
	)
	if err == nil {
		t.Fatal("expected failure on missing FINAL record, got nil")
	}
}

func TestVerificationEngine_VerifyEncryptedDatabaseArtifact_IdentityMismatch(t *testing.T) {
	kp := testArtifactKeyProvider(t)
	rawSQL := "-- MySQL dump 10.13\n" + strings.Repeat("INSERT INTO users VALUES (1, 'alice');\n", 500)
	gzipData, _ := createGzipData(t, []byte(rawSQL))

	orgID := uuid.New()
	artID := uuid.New()
	cipherBytes, cipherHash, plainSize, plainHash := createEncryptedBPAEData(t, gzipData, kp, orgID, artID)

	fakeStorage := &memoryArtifactStorage{data: cipherBytes}
	verifier := NewVerificationEngineWithKeyProvider(kp)

	// Pass a different orgID to simulate identity binding mismatch
	wrongOrgID := uuid.New()
	_, err := verifier.VerifyEncryptedDatabaseArtifact(
		context.Background(),
		fakeStorage,
		"ref",
		plainSize,
		plainHash,
		int64(len(cipherBytes)),
		cipherHash,
		wrongOrgID,
		artID,
	)
	if err == nil {
		t.Fatal("expected failure on orgID identity mismatch, got nil")
	}
}

func TestVerificationEngine_VerifyEncryptedFilesArtifact_Success(t *testing.T) {
	kp := testArtifactKeyProvider(t)
	tarGzData, _ := createTarGzipData(t, map[string][]byte{
		"app/index.html": []byte("<h1>Welcome</h1>"),
		"app/style.css":  []byte("body { color: red; }"),
	})

	orgID := uuid.New()
	artID := uuid.New()
	cipherBytes, cipherHash, plainSize, plainHash := createEncryptedBPAEData(t, tarGzData, kp, orgID, artID)

	fakeStorage := &memoryArtifactStorage{data: cipherBytes}
	verifier := NewVerificationEngineWithKeyProvider(kp)

	msg, err := verifier.VerifyEncryptedFilesArtifact(
		context.Background(),
		fakeStorage,
		"ref",
		plainSize,
		plainHash,
		int64(len(cipherBytes)),
		cipherHash,
		orgID,
		artID,
	)
	if err != nil {
		t.Fatalf("expected verification success, got error: %v", err)
	}
	if msg != canonicalFilesVerifiedMsg {
		t.Errorf("expected '%s', got '%s'", canonicalFilesVerifiedMsg, msg)
	}
}
