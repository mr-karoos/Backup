package engine

import (
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	"backup-platform/internal/connector"
	"backup-platform/internal/credential/payload"
	"backup-platform/internal/storage"
	"backup-platform/pkg/uuid"
)

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
	) (*storage.SaveResult, error)

	ExecuteFilesBackup(
		ctx context.Context,
		capability connector.FileBackupCapability,
		target connector.Target,
		credPayload *payload.PayloadV1,
		config connector.FileBackupConfig,
		storageProvider storage.StorageProvider,
		orgID, resID, runID, artifactID uuid.UUID,
	) (*storage.SaveResult, error)
}

// DirectStreamBackupEngine streams raw extraction output directly through a gzip compressor
// into the storage provider without buffering full dump contents in process memory.
type DirectStreamBackupEngine struct{}

// NewDirectStreamBackupEngine constructs a new DirectStreamBackupEngine.
func NewDirectStreamBackupEngine() *DirectStreamBackupEngine {
	return &DirectStreamBackupEngine{}
}

// ExecuteDatabaseBackup executes the concurrent producer-consumer pipeline:
// DatabaseBackupCapability -> gzip.Writer -> io.Pipe -> StorageProvider.SaveArtifact (.sql.gz).
func (e *DirectStreamBackupEngine) ExecuteDatabaseBackup(
	ctx context.Context,
	capability connector.DatabaseBackupCapability,
	target connector.Target,
	credPayload *payload.PayloadV1,
	databaseName string,
	storageProvider storage.StorageProvider,
	orgID, resID, runID, artifactID uuid.UUID,
) (*storage.SaveResult, error) {
	if capability == nil {
		return nil, errors.New("backup capability cannot be nil")
	}
	if storageProvider == nil {
		return nil, errors.New("storage provider cannot be nil")
	}

	pipeReader, pipeWriter := io.Pipe()

	var wg sync.WaitGroup
	var producerErr error
	var producerPanicked bool
	var panicVal any

	// Producer Goroutine: Streams raw SQL from capability into gzip writer -> pipeWriter
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

		gw := gzip.NewWriter(pipeWriter)

		err := capability.BackupDatabase(ctx, target, credPayload, databaseName, gw)
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

		_ = pipeWriter.Close()
	}()

	// Consumer (Main Goroutine): Streams compressed bytes from pipeReader into storage
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

	return saveResult, nil
}

// ExecuteFilesBackup executes the concurrent producer-consumer pipeline:
// FileBackupCapability -> gzip.Writer -> io.Pipe -> StorageProvider.SaveArtifact (.tar.gz).
func (e *DirectStreamBackupEngine) ExecuteFilesBackup(
	ctx context.Context,
	capability connector.FileBackupCapability,
	target connector.Target,
	credPayload *payload.PayloadV1,
	config connector.FileBackupConfig,
	storageProvider storage.StorageProvider,
	orgID, resID, runID, artifactID uuid.UUID,
) (*storage.SaveResult, error) {
	if capability == nil {
		return nil, errors.New("file backup capability cannot be nil")
	}
	if storageProvider == nil {
		return nil, errors.New("storage provider cannot be nil")
	}

	pipeReader, pipeWriter := io.Pipe()

	var wg sync.WaitGroup
	var producerErr error
	var producerPanicked bool
	var panicVal any

	// Producer Goroutine: Streams raw uncompressed tar from capability into gzip writer -> pipeWriter
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

		gw := gzip.NewWriter(pipeWriter)

		err := capability.BackupFiles(ctx, target, credPayload, config, gw)
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

		_ = pipeWriter.Close()
	}()

	// Consumer (Main Goroutine): Streams compressed bytes from pipeReader into storage
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

	return saveResult, nil
}
