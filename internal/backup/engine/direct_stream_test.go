package engine

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

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

	engine := NewDirectStreamBackupEngine()
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

	if saveRes.SizeBytes <= 0 {
		t.Errorf("expected positive artifact size, got %d", saveRes.SizeBytes)
	}

	// Verify the stored file is valid gzip and decompresses to original SQL
	rc, err := storageProvider.OpenArtifact(ctx, saveRes.StorageReference)
	if err != nil {
		t.Fatalf("failed opening stored artifact: %v", err)
	}
	defer rc.Close()

	gzReader, err := gzip.NewReader(rc)
	if err != nil {
		t.Fatalf("failed creating gzip reader: %v", err)
	}
	defer gzReader.Close()

	decompressed, err := io.ReadAll(gzReader)
	if err != nil {
		t.Fatalf("failed reading decompressed stream: %v", err)
	}

	if string(decompressed) != originalSQL {
		t.Errorf("decompressed SQL does not match original SQL")
	}
}

func TestDirectStreamBackupEngine_ProducerFailure(t *testing.T) {
	tempDir := t.TempDir()
	storageProvider, _ := local.NewLocalStorageProvider(tempDir)
	ctx := context.Background()
	_ = storageProvider.EnsureStorageRoot(ctx)

	cap := &fakeBackupCapability{errToReturn: errors.New("mysqldump remote crash")}
	engine := NewDirectStreamBackupEngine()

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
	// Read a few bytes then fail to simulate write failure
	buf := make([]byte, 10)
	_, _ = src.Read(buf)
	return nil, errors.New("storage disk full")
}

func TestDirectStreamBackupEngine_StorageFailure(t *testing.T) {
	ctx := context.Background()
	cap := &fakeBackupCapability{dataToSend: []byte(strings.Repeat("A", 10000))}
	engine := NewDirectStreamBackupEngine()

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

	rawTarBytes := []byte("tar_header_block" + strings.Repeat("file_contents_data", 500))
	cap := &fakeFileBackupCapability{dataToSend: rawTarBytes}

	engine := NewDirectStreamBackupEngine()
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

	// Verify compressed output can be read with gzip.Reader and matches original raw tar bytes
	rc, err := storageProvider.OpenArtifact(ctx, saveRes.StorageReference)
	if err != nil {
		t.Fatalf("failed opening stored artifact: %v", err)
	}
	defer rc.Close()

	gzReader, err := gzip.NewReader(rc)
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

	cap := &fakeFileBackupCapability{errToReturn: errors.New("tar command failed")}
	engine := NewDirectStreamBackupEngine()

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
