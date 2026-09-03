package engine

import (
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"sync"

	"backup-platform/internal/artifactcrypto"
	"backup-platform/internal/connector"
	"backup-platform/internal/credential/payload"
	"backup-platform/internal/storage"
	"backup-platform/pkg/uuid"
)

// ExecutionResult represents the output of a backup engine execution,
// containing both plaintext gzip metrics and stored physical BPAE metrics.
type ExecutionResult struct {
	StorageReference        string
	PlaintextSizeBytes      int64
	PlaintextChecksumSHA256 string
	StoredSizeBytes         int64
	CiphertextSHA256        string
}

// BackupEngine defines the interface for executing backup extraction pipelines.
type BackupEngine interface {
	ExecuteDatabaseBackup(
		ctx context.Context,
		capability connector.DatabaseBackupCapability,
		target connector.Target,
		credPayload *payload.PayloadV1,
		databaseName string,
		storageProvider storage.StorageProvider,
		orgID, resID, runID, artifactID uuid.UUID,
	) (*ExecutionResult, error)

	ExecuteFilesBackup(
		ctx context.Context,
		capability connector.FileBackupCapability,
		target connector.Target,
		credPayload *payload.PayloadV1,
		config connector.FileBackupConfig,
		storageProvider storage.StorageProvider,
		orgID, resID, runID, artifactID uuid.UUID,
	) (*ExecutionResult, error)
}

// DirectStreamBackupEngine streams raw extraction output directly through a gzip compressor,
// through BPAE authenticated encryption, into the storage provider without buffering full dump contents in process memory.
type DirectStreamBackupEngine struct {
	keyProvider artifactcrypto.KeyProvider
}

// NewDirectStreamBackupEngine constructs a new DirectStreamBackupEngine.
func NewDirectStreamBackupEngine() *DirectStreamBackupEngine {
	return &DirectStreamBackupEngine{}
}

// NewDirectStreamBackupEngineWithKeyProvider constructs a new DirectStreamBackupEngine with an injected key provider.
func NewDirectStreamBackupEngineWithKeyProvider(keyProvider artifactcrypto.KeyProvider) *DirectStreamBackupEngine {
	return &DirectStreamBackupEngine{
		keyProvider: keyProvider,
	}
}

// SetKeyProvider sets or updates the artifact crypto key provider.
func (e *DirectStreamBackupEngine) SetKeyProvider(keyProvider artifactcrypto.KeyProvider) {
	e.keyProvider = keyProvider
}

type hashingCountingWriter struct {
	w       io.Writer
	hasher  hash.Hash
	written int64
}

func (h *hashingCountingWriter) Write(p []byte) (int, error) {
	n, err := h.w.Write(p)
	if n > 0 {
		h.hasher.Write(p[:n])
		h.written += int64(n)
	}
	return n, err
}

// ExecuteDatabaseBackup executes the concurrent producer-consumer pipeline:
// DatabaseBackupCapability -> gzip.Writer -> plaintext gzip metrics -> BPAE EncryptWriter -> io.Pipe -> StorageProvider.SaveArtifact (.sql.gz).
func (e *DirectStreamBackupEngine) ExecuteDatabaseBackup(
	ctx context.Context,
	capability connector.DatabaseBackupCapability,
	target connector.Target,
	credPayload *payload.PayloadV1,
	databaseName string,
	storageProvider storage.StorageProvider,
	orgID, resID, runID, artifactID uuid.UUID,
) (*ExecutionResult, error) {
	if capability == nil {
		return nil, errors.New("backup capability cannot be nil")
	}
	if storageProvider == nil {
		return nil, errors.New("storage provider cannot be nil")
	}
	if e.keyProvider == nil {
		return nil, errors.New("artifact key provider cannot be nil: direct stream requires BPAE encryption at rest")
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	pipeReader, pipeWriter := io.Pipe()

	var wg sync.WaitGroup
	var producerErr error
	var producerPanicked bool
	var panicVal any

	var plainBytesCount int64
	var plainChecksumHex string

	// Producer Goroutine: Streams raw SQL from capability into gzip writer -> plainCounter -> EncryptWriter -> pipeWriter
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() {
			if r := recover(); r != nil {
				producerPanicked = true
				panicVal = r
				_ = pipeWriter.CloseWithError(errors.New("panic in backup producer"))
			}
		}()

		encWriter, err := artifactcrypto.NewEncryptWriter(pipeWriter, e.keyProvider, orgID, artifactID)
		if err != nil {
			producerErr = fmt.Errorf("failed initializing BPAE encrypt writer: %w", err)
			_ = pipeWriter.CloseWithError(producerErr)
			return
		}
		defer encWriter.Close()

		plainHasher := sha256.New()
		plainWriter := &hashingCountingWriter{
			w:      encWriter,
			hasher: plainHasher,
		}

		gw := gzip.NewWriter(plainWriter)

		err = capability.BackupDatabase(ctx, target, credPayload, databaseName, gw)
		if err != nil {
			producerErr = err
			_ = gw.Close()
			_ = pipeWriter.CloseWithError(err)
			return
		}

		if err := gw.Close(); err != nil {
			producerErr = fmt.Errorf("failed closing gzip stream: %w", err)
			_ = pipeWriter.CloseWithError(producerErr)
			return
		}

		plainBytesCount = plainWriter.written
		plainChecksumHex = hex.EncodeToString(plainHasher.Sum(nil))

		if err := encWriter.Close(); err != nil {
			producerErr = fmt.Errorf("failed finalizing BPAE stream: %w", err)
			_ = pipeWriter.CloseWithError(producerErr)
			return
		}

		_ = pipeWriter.Close()
	}()

	// Consumer (Main Goroutine): Streams encrypted bytes from pipeReader into storage
	saveResult, consumerErr := storageProvider.SaveArtifact(ctx, orgID, resID, runID, artifactID, ".sql.gz", pipeReader)
	if consumerErr != nil {
		// Break the pipe immediately to cancel the producer
		_ = pipeReader.CloseWithError(consumerErr)
	}

	// Guarantee producer completes before returning to prevent goroutine leaks
	wg.Wait()

	if producerPanicked {
		panic(panicVal)
	}

	if producerErr != nil {
		return nil, producerErr
	}
	if consumerErr != nil {
		return nil, consumerErr
	}

	return &ExecutionResult{
		StorageReference:        saveResult.StorageReference,
		PlaintextSizeBytes:      plainBytesCount,
		PlaintextChecksumSHA256: plainChecksumHex,
		StoredSizeBytes:         saveResult.SizeBytes,
		CiphertextSHA256:        saveResult.ChecksumSHA256,
	}, nil
}

// ExecuteFilesBackup executes the concurrent producer-consumer pipeline:
// FileBackupCapability -> gzip.Writer -> plaintext gzip metrics -> BPAE EncryptWriter -> io.Pipe -> StorageProvider.SaveArtifact (.tar.gz).
func (e *DirectStreamBackupEngine) ExecuteFilesBackup(
	ctx context.Context,
	capability connector.FileBackupCapability,
	target connector.Target,
	credPayload *payload.PayloadV1,
	config connector.FileBackupConfig,
	storageProvider storage.StorageProvider,
	orgID, resID, runID, artifactID uuid.UUID,
) (*ExecutionResult, error) {
	if capability == nil {
		return nil, errors.New("file backup capability cannot be nil")
	}
	if storageProvider == nil {
		return nil, errors.New("storage provider cannot be nil")
	}
	if e.keyProvider == nil {
		return nil, errors.New("artifact key provider cannot be nil: direct stream requires BPAE encryption at rest")
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	pipeReader, pipeWriter := io.Pipe()

	var wg sync.WaitGroup
	var producerErr error
	var producerPanicked bool
	var panicVal any

	var plainBytesCount int64
	var plainChecksumHex string

	// Producer Goroutine: Streams raw uncompressed tar from capability into gzip writer -> plainCounter -> EncryptWriter -> pipeWriter
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() {
			if r := recover(); r != nil {
				producerPanicked = true
				panicVal = r
				_ = pipeWriter.CloseWithError(errors.New("panic in file backup producer"))
			}
		}()

		encWriter, err := artifactcrypto.NewEncryptWriter(pipeWriter, e.keyProvider, orgID, artifactID)
		if err != nil {
			producerErr = fmt.Errorf("failed initializing BPAE encrypt writer: %w", err)
			_ = pipeWriter.CloseWithError(producerErr)
			return
		}
		defer encWriter.Close()

		plainHasher := sha256.New()
		plainWriter := &hashingCountingWriter{
			w:      encWriter,
			hasher: plainHasher,
		}

		gw := gzip.NewWriter(plainWriter)

		err = capability.BackupFiles(ctx, target, credPayload, config, gw)
		if err != nil {
			producerErr = err
			_ = gw.Close()
			_ = pipeWriter.CloseWithError(err)
			return
		}

		if err := gw.Close(); err != nil {
			producerErr = fmt.Errorf("failed closing gzip stream: %w", err)
			_ = pipeWriter.CloseWithError(producerErr)
			return
		}

		plainBytesCount = plainWriter.written
		plainChecksumHex = hex.EncodeToString(plainHasher.Sum(nil))

		if err := encWriter.Close(); err != nil {
			producerErr = fmt.Errorf("failed finalizing BPAE stream: %w", err)
			_ = pipeWriter.CloseWithError(producerErr)
			return
		}

		_ = pipeWriter.Close()
	}()

	// Consumer (Main Goroutine): Streams encrypted bytes from pipeReader into storage
	saveResult, consumerErr := storageProvider.SaveArtifact(ctx, orgID, resID, runID, artifactID, ".tar.gz", pipeReader)
	if consumerErr != nil {
		// Break the pipe immediately to cancel the producer
		_ = pipeReader.CloseWithError(consumerErr)
	}

	// Guarantee producer completes before returning to prevent goroutine leaks
	wg.Wait()

	if producerPanicked {
		panic(panicVal)
	}

	if producerErr != nil {
		return nil, producerErr
	}
	if consumerErr != nil {
		return nil, consumerErr
	}

	return &ExecutionResult{
		StorageReference:        saveResult.StorageReference,
		PlaintextSizeBytes:      plainBytesCount,
		PlaintextChecksumSHA256: plainChecksumHex,
		StoredSizeBytes:         saveResult.SizeBytes,
		CiphertextSHA256:        saveResult.ChecksumSHA256,
	}, nil
}
