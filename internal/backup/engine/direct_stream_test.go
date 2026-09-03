package engine

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"testing"

	"backup-platform/internal/artifactcrypto"
	"backup-platform/internal/connector"
	"backup-platform/internal/credential/payload"
	"backup-platform/internal/storage"
	"backup-platform/internal/storage/local"
	"backup-platform/pkg/uuid"
)

type fakeBackupCapability struct {
	dataToSend  []byte
	errToReturn error
}

func (f *fakeBackupCapability) BackupDatabase(
	ctx context.Context,
	target connector.Target,
	credPayload *payload.PayloadV1,
	databaseName string,
	dest io.Writer,
) error {
	if f.errToReturn != nil {
		return f.errToReturn
	}
	_, err := dest.Write(f.dataToSend)
	return err
}

func testKeyProvider(t *testing.T) artifactcrypto.KeyProvider {
	t.Helper()
	kp, err := artifactcrypto.NewStaticKeyProvider(bytes.Repeat([]byte{0x42}, 32), 1)
	if err != nil {
		t.Fatalf("failed creating test key provider: %v", err)
	}
	return kp
}

func TestDirectStreamBackupEngine_Success(t *testing.T) {
	tempDir := t.TempDir()
	storageProvider, err := local.NewLocalStorageProvider(tempDir)
	if err != nil {
		t.Fatalf("failed initializing storage provider: %v", err)
	}

	ctx := context.Background()
	_ = storageProvider.EnsureStorageRoot(ctx)

	originalSQL := "-- MySQL dump 10.13\nCREATE DATABASE `ecommerce_prod`;\n" + strings.Repeat("INSERT INTO orders VALUES (1, 'item');\n", 1000)
	cap := &fakeBackupCapability{dataToSend: []byte(originalSQL)}

	kp := testKeyProvider(t)
	engine := NewDirectStreamBackupEngineWithKeyProvider(kp)
	orgID := uuid.New()
	resID := uuid.New()
	runID := uuid.New()
	artID := uuid.New()

	saveRes, err := engine.ExecuteDatabaseBackup(
		ctx,
		cap,
		connector.Target{},
		&payload.PayloadV1{},
		"ecommerce_prod",
		storageProvider,
		orgID, resID, runID, artID,
	)
	if err != nil {
		t.Fatalf("unexpected engine execution error: %v", err)
	}

	if saveRes.PlaintextSizeBytes <= 0 {
		t.Errorf("expected positive plaintext size, got %d", saveRes.PlaintextSizeBytes)
	}
	if saveRes.StoredSizeBytes <= 0 {
		t.Errorf("expected positive stored size, got %d", saveRes.StoredSizeBytes)
	}
	if len(saveRes.PlaintextChecksumSHA256) != 64 {
		t.Errorf("expected 64-character plaintext SHA-256, got %s", saveRes.PlaintextChecksumSHA256)
	}
	if len(saveRes.CiphertextSHA256) != 64 {
		t.Errorf("expected 64-character ciphertext SHA-256, got %s", saveRes.CiphertextSHA256)
	}

	// 1. Verify stored physical bytes are BPAE ciphertext and cannot be read directly as gzip
	rcRaw, err := storageProvider.OpenArtifact(ctx, saveRes.StorageReference)
	if err != nil {
		t.Fatalf("failed opening stored artifact: %v", err)
	}
	_, rawGzErr := gzip.NewReader(rcRaw)
	_ = rcRaw.Close()
	if rawGzErr == nil {
		t.Fatal("expected raw physical BPAE bytes to fail raw gzip parsing, got nil")
	}

	// 2. Verify stored file decrypts through BPAE DecryptReader into the exact original gzip stream
	rc, err := storageProvider.OpenArtifact(ctx, saveRes.StorageReference)
	if err != nil {
		t.Fatalf("failed opening stored artifact: %v", err)
	}
	defer rc.Close()

	decReader, err := artifactcrypto.NewDecryptReader(rc, kp, orgID, artID)
	if err != nil {
		t.Fatalf("failed creating decrypt reader: %v", err)
	}
	defer decReader.Close()

	// Read decrypted gzip stream while verifying plaintext size and checksum
	decPlainHasher := sha256.New()
	teeDecReader := io.TeeReader(decReader, decPlainHasher)

	gzReader, err := gzip.NewReader(teeDecReader)
	if err != nil {
		t.Fatalf("failed creating gzip reader on decrypted stream: %v", err)
	}
	defer gzReader.Close()

	decompressed, err := io.ReadAll(gzReader)
	if err != nil {
		t.Fatalf("failed reading decompressed stream: %v", err)
	}

	if string(decompressed) != originalSQL {
		t.Errorf("decompressed SQL does not match original SQL")
	}

	// Drain any remaining bytes in decReader to complete SHA-256
	_, _ = io.Copy(io.Discard, teeDecReader)

	calculatedPlainChecksum := hex.EncodeToString(decPlainHasher.Sum(nil))
	if calculatedPlainChecksum != saveRes.PlaintextChecksumSHA256 {
		t.Errorf("plaintext checksum mismatch: expected %s, got %s", saveRes.PlaintextChecksumSHA256, calculatedPlainChecksum)
	}
}

func TestDirectStreamBackupEngine_NilKeyProvider_FailsClosed(t *testing.T) {
	tempDir := t.TempDir()
	storageProvider, _ := local.NewLocalStorageProvider(tempDir)
	ctx := context.Background()

	cap := &fakeBackupCapability{dataToSend: []byte("test")}
	engine := NewDirectStreamBackupEngine() // nil key provider

	_, err := engine.ExecuteDatabaseBackup(
		ctx,
		cap,
		connector.Target{},
		&payload.PayloadV1{},
		"ecommerce_prod",
		storageProvider,
		uuid.New(), uuid.New(), uuid.New(), uuid.New(),
	)
	if err == nil {
		t.Fatal("expected fail-closed error with nil key provider, got nil")
	}
	if !strings.Contains(err.Error(), "artifact key provider cannot be nil") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestDirectStreamBackupEngine_ProducerFailure(t *testing.T) {
	tempDir := t.TempDir()
	storageProvider, _ := local.NewLocalStorageProvider(tempDir)
	ctx := context.Background()
	_ = storageProvider.EnsureStorageRoot(ctx)

	kp := testKeyProvider(t)
	cap := &fakeBackupCapability{errToReturn: errors.New("mysqldump remote crash")}
	engine := NewDirectStreamBackupEngineWithKeyProvider(kp)

	_, err := engine.ExecuteDatabaseBackup(
		ctx,
		cap,
		connector.Target{},
		&payload.PayloadV1{},
		"ecommerce_prod",
		storageProvider,
		uuid.New(), uuid.New(), uuid.New(), uuid.New(),
	)
	if err == nil {
		t.Fatalf("expected error on producer failure")
	}
}

type failingStorageProvider struct {
	storage.StorageProvider
}

func (f *failingStorageProvider) SaveArtifact(ctx context.Context, orgID, resID, runID, artifactID uuid.UUID, extension string, src io.Reader) (*storage.SaveResult, error) {
	buf := make([]byte, 10)
	_, _ = src.Read(buf)
	return nil, errors.New("storage disk full")
}

func TestDirectStreamBackupEngine_StorageFailure(t *testing.T) {
	ctx := context.Background()
	kp := testKeyProvider(t)
	cap := &fakeBackupCapability{dataToSend: []byte(strings.Repeat("A", 10000))}
	engine := NewDirectStreamBackupEngineWithKeyProvider(kp)

	_, err := engine.ExecuteDatabaseBackup(
		ctx,
		cap,
		connector.Target{},
		&payload.PayloadV1{},
		"ecommerce_prod",
		&failingStorageProvider{},
		uuid.New(), uuid.New(), uuid.New(), uuid.New(),
	)
	if err == nil {
		t.Fatalf("expected error on storage failure")
	}
}

type fakeFileBackupCapability struct {
	dataToSend  []byte
	errToReturn error
}

func (f *fakeFileBackupCapability) BackupFiles(
	ctx context.Context,
	target connector.Target,
	credPayload *payload.PayloadV1,
	config connector.FileBackupConfig,
	dest io.Writer,
) error {
	if f.errToReturn != nil {
		return f.errToReturn
	}
	_, err := dest.Write(f.dataToSend)
	return err
}

func TestDirectStreamBackupEngine_ExecuteFilesBackup_Success(t *testing.T) {
	tempDir := t.TempDir()
	storageProvider, err := local.NewLocalStorageProvider(tempDir)
	if err != nil {
		t.Fatalf("failed initializing storage provider: %v", err)
	}

	ctx := context.Background()
	_ = storageProvider.EnsureStorageRoot(ctx)

	kp := testKeyProvider(t)
	rawTarBytes := []byte("tar_header_block" + strings.Repeat("file_contents_data", 500))
	cap := &fakeFileBackupCapability{dataToSend: rawTarBytes}

	engine := NewDirectStreamBackupEngineWithKeyProvider(kp)
	orgID := uuid.New()
	resID := uuid.New()
	runID := uuid.New()
	artID := uuid.New()

	saveRes, err := engine.ExecuteFilesBackup(
		ctx,
		cap,
		connector.Target{},
		&payload.PayloadV1{},
		connector.FileBackupConfig{SourcePath: "/var/www/site", ExcludePatterns: []string{"*.log"}},
		storageProvider,
		orgID, resID, runID, artID,
	)
	if err != nil {
		t.Fatalf("unexpected engine execution error: %v", err)
	}

	if !strings.HasSuffix(saveRes.StorageReference, ".tar.gz") {
		t.Errorf("expected .tar.gz storage reference suffix, got: %s", saveRes.StorageReference)
	}
	if saveRes.PlaintextSizeBytes <= 0 {
		t.Errorf("expected positive plaintext size, got %d", saveRes.PlaintextSizeBytes)
	}
	if saveRes.StoredSizeBytes <= 0 {
		t.Errorf("expected positive stored size, got %d", saveRes.StoredSizeBytes)
	}

	// Verify compressed output can be read with DecryptReader + gzip.Reader and matches original raw tar bytes
	rc, err := storageProvider.OpenArtifact(ctx, saveRes.StorageReference)
	if err != nil {
		t.Fatalf("failed opening stored artifact: %v", err)
	}
	defer rc.Close()

	decReader, err := artifactcrypto.NewDecryptReader(rc, kp, orgID, artID)
	if err != nil {
		t.Fatalf("failed creating decrypt reader: %v", err)
	}
	defer decReader.Close()

	gzReader, err := gzip.NewReader(decReader)
	if err != nil {
		t.Fatalf("failed creating gzip reader: %v", err)
	}
	defer gzReader.Close()

	decompressed, err := io.ReadAll(gzReader)
	if err != nil {
		t.Fatalf("failed reading decompressed stream: %v", err)
	}

	if !bytes.Equal(decompressed, rawTarBytes) {
		t.Errorf("decompressed bytes do not match original raw tar stream")
	}
}

func TestDirectStreamBackupEngine_ExecuteFilesBackup_ProducerFailure(t *testing.T) {
	tempDir := t.TempDir()
	storageProvider, _ := local.NewLocalStorageProvider(tempDir)
	ctx := context.Background()
	_ = storageProvider.EnsureStorageRoot(ctx)

	kp := testKeyProvider(t)
	cap := &fakeFileBackupCapability{errToReturn: errors.New("tar command failed")}
	engine := NewDirectStreamBackupEngineWithKeyProvider(kp)

	_, err := engine.ExecuteFilesBackup(
		ctx,
		cap,
		connector.Target{},
		&payload.PayloadV1{},
		connector.FileBackupConfig{SourcePath: "/var/www/site"},
		storageProvider,
		uuid.New(), uuid.New(), uuid.New(), uuid.New(),
	)
	if err == nil {
		t.Fatalf("expected error on producer failure")
	}
}
