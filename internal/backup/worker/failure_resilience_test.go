package worker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"backup-platform/internal/backup/domain"
	"backup-platform/internal/backup/engine"
	"backup-platform/internal/backup/verification"
	"backup-platform/internal/connector"
	"backup-platform/internal/credential/payload"
	resDomain "backup-platform/internal/resource/domain"
	"backup-platform/internal/storage"
	"backup-platform/internal/storage/local"
	"backup-platform/pkg/uuid"
)

type midStreamNetworkFailingCapability struct {
	bytesWritten int
}

func (m *midStreamNetworkFailingCapability) BackupDatabase(ctx context.Context, target connector.Target, credPayload *payload.PayloadV1, databaseName string, dest io.Writer) error {
	chunk := []byte("-- MySQL dump 10.13\nCREATE DATABASE `ecommerce_prod`;\nINSERT INTO products VALUES (1, 'item1');\n")
	n, _ := dest.Write(chunk)
	m.bytesWritten = n
	return connector.ErrSSHNetwork
}

func (m *midStreamNetworkFailingCapability) DiscoverDatabases(ctx context.Context, target connector.Target, credPayload *payload.PayloadV1) ([]string, error) {
	return []string{"ecommerce_prod"}, nil
}

type partialThenENOSPCReader struct {
	written int
}

func (r *partialThenENOSPCReader) Read(p []byte) (int, error) {
	if r.written == 0 {
		n := copy(p, []byte("some initial database dump content before disk fills up"))
		r.written = n
		return n, nil
	}
	return 0, syscall.ENOSPC
}

type failingSaveStorageProvider struct {
	storage.StorageProvider
	saveErr error
}

func (f *failingSaveStorageProvider) SaveArtifact(ctx context.Context, orgID, resID, runID, artifactID uuid.UUID, extension string, src io.Reader) (*storage.SaveResult, error) {
	if f.saveErr != nil {
		return nil, f.saveErr
	}
	if f.StorageProvider != nil {
		return f.StorageProvider.SaveArtifact(ctx, orgID, resID, runID, artifactID, extension, src)
	}
	return nil, nil
}

func setupTestWorker(t *testing.T, cap connector.DatabaseBackupCapability, store storage.StorageProvider) (*WorkerPool, *fakeWorkerRepo, uuid.UUID, uuid.UUID) {
	orgID := uuid.New()
	resID := uuid.New()
	credID := uuid.New()
	jobID := uuid.New()

	validPassJSON, _ := payload.EncodeV1("secret-pass-12345", nil)
	fingerprint := "SHA256:testfingerprint123456789"
	timeout := 10
	port := 22

	resWithConn := &resDomain.ResourceWithConnector{
		Resource: &resDomain.Resource{
			ID:             resID,
			OrganizationID: orgID,
			Type:           resDomain.TypeUbuntuSSH,
			Status:         resDomain.StatusActive,
		},
		Connector: &resDomain.ResourceConnector{
			ID:                 uuid.New(),
			ResourceID:         resID,
			CredentialID:       credID,
			Host:               "127.0.0.1",
			Port:               port,
			AuthType:           resDomain.AuthTypeSSHPassword,
			HostKeyFingerprint: &fingerprint,
			Config: resDomain.ConnectorConfig{
				Username:                 "testuser",
				ConnectionTimeoutSeconds: &timeout,
			},
		},
	}

	repo := newFakeWorkerRepo(orgID)
	job := &domain.BackupJob{
		ID:             jobID,
		OrganizationID: orgID,
		ResourceID:     resID,
		TriggerType:    domain.TriggerTypeManual,
		BackupType:     domain.BackupTypeMySQLDatabase,
		TargetSpec:     domain.TargetSpec{Databases: []string{"ecommerce_prod"}},
		Status:         domain.JobStatusPending,
		CreatedAt:      time.Now(),
	}
	repo.jobs[job.ID] = job

	reg := connector.NewBackupCapabilityRegistry()
	if cap != nil {
		reg.Register(resDomain.TypeUbuntuSSH, cap)
	}

	workerPool := NewWorkerPool(
		WorkerPoolConfig{NumWorkers: 1, PollInterval: 10 * time.Millisecond},
		repo,
		&fakeResourceFinder{resWithConn: resWithConn},
		&fakeCredentialVault{payloadBytes: validPassJSON},
		reg,
		nil,
		engine.NewDirectStreamBackupEngine(),
		store,
		verification.NewVerificationEngine(),
		NewPerResourceMutexManager(),
		nil,
	)

	return workerPool, repo, orgID, jobID
}

func TestFailureResilience_Scenarios(t *testing.T) {
	t.Run("Scenario 1: Real mid-stream network interruption cleans partial artifact, sets run failed and retries job", func(t *testing.T) {
		tempDir := t.TempDir()
		store, _ := local.NewLocalStorageProvider(tempDir)
		_ = store.EnsureStorageRoot(context.Background())

		cap := &midStreamNetworkFailingCapability{}
		workerPool, repo, _, jobID := setupTestWorker(t, cap, store)

		ctx, cancel := context.WithCancel(context.Background())
		workerPool.Start(ctx)
		time.Sleep(100 * time.Millisecond)
		_ = workerPool.Stop(context.Background())
		cancel()

		repo.mu.Lock()
		defer repo.mu.Unlock()

		if cap.bytesWritten <= 0 {
			t.Fatalf("expected bytes to have been written mid-stream before error, got %d", cap.bytesWritten)
		}

		if repo.finalizedRun == nil || repo.finalizedRun.Status != domain.RunStatusFailed {
			t.Fatalf("expected run failed on network error, got: %+v", repo.finalizedRun)
		}
		// Attempt 1 < 3 and network error is retryable -> Job must be pending
		if repo.jobs[jobID].Status != domain.JobStatusPending {
			t.Errorf("expected job reset to pending for retry, got: %s", repo.jobs[jobID].Status)
		}
		// Safe error message check
		if repo.finalizedRun.ErrorMessage == nil || !strings.Contains(*repo.finalizedRun.ErrorMessage, "network connection error") {
			t.Errorf("expected safe network error message, got: %v", repo.finalizedRun.ErrorMessage)
		}
		// No secret leaked
		if strings.Contains(*repo.finalizedRun.ErrorMessage, "secret-pass") {
			t.Errorf("SECURITY FLAW: credential leaked in error message: %s", *repo.finalizedRun.ErrorMessage)
		}
		// No artifact in DB
		if len(repo.artifacts) != 0 {
			t.Errorf("expected 0 artifacts persisted on failure, got %d", len(repo.artifacts))
		}

		// Verify that no incomplete .partial file remains on disk
		runTmpDir := filepath.Join(tempDir, "tmp", fmt.Sprintf("run-%s", repo.finalizedRun.ID.String()))
		if entries, err := os.ReadDir(runTmpDir); err == nil && len(entries) > 0 {
			t.Errorf("expected temp run dir to be empty or removed, but found %d files: %+v", len(entries), entries)
		}
	})

	t.Run("Scenario 2: Authentication failure is non-retryable and marks job failed", func(t *testing.T) {
		tempDir := t.TempDir()
		store, _ := local.NewLocalStorageProvider(tempDir)
		_ = store.EnsureStorageRoot(context.Background())

		workerPool, repo, _, jobID := setupTestWorker(t, &fakeCapability{errToReturn: connector.ErrSSHAuthentication}, store)

		ctx, cancel := context.WithCancel(context.Background())
		workerPool.Start(ctx)
		time.Sleep(100 * time.Millisecond)
		_ = workerPool.Stop(context.Background())
		cancel()

		repo.mu.Lock()
		defer repo.mu.Unlock()

		if repo.finalizedRun == nil || repo.finalizedRun.Status != domain.RunStatusFailed {
			t.Fatalf("expected run failed on auth error, got: %+v", repo.finalizedRun)
		}
		// Auth error is non-retryable -> Job must be failed immediately
		if repo.jobs[jobID].Status != domain.JobStatusFailed {
			t.Errorf("expected job failed on auth failure, got: %s", repo.jobs[jobID].Status)
		}
		if repo.finalizedRun.ErrorMessage == nil || *repo.finalizedRun.ErrorMessage != "authentication failed with remote host" {
			t.Errorf("expected safe auth error message, got: %v", repo.finalizedRun.ErrorMessage)
		}
	})

	t.Run("Scenario 3: SSH Host Key mismatch is non-retryable and marks job failed without leaking fingerprint", func(t *testing.T) {
		tempDir := t.TempDir()
		store, _ := local.NewLocalStorageProvider(tempDir)
		_ = store.EnsureStorageRoot(context.Background())

		workerPool, repo, _, jobID := setupTestWorker(t, &fakeCapability{errToReturn: connector.ErrSSHHostKeyMismatch}, store)

		ctx, cancel := context.WithCancel(context.Background())
		workerPool.Start(ctx)
		time.Sleep(100 * time.Millisecond)
		_ = workerPool.Stop(context.Background())
		cancel()

		repo.mu.Lock()
		defer repo.mu.Unlock()

		if repo.finalizedRun == nil || repo.finalizedRun.Status != domain.RunStatusFailed {
			t.Fatalf("expected run failed on host key mismatch, got: %+v", repo.finalizedRun)
		}
		if repo.jobs[jobID].Status != domain.JobStatusFailed {
			t.Errorf("expected job failed on host key mismatch, got: %s", repo.jobs[jobID].Status)
		}
		if repo.finalizedRun.ErrorMessage == nil || *repo.finalizedRun.ErrorMessage != "remote host key mismatch detected" {
			t.Errorf("expected safe host key mismatch message, got: %v", repo.finalizedRun.ErrorMessage)
		}
		if strings.Contains(*repo.finalizedRun.ErrorMessage, "testfingerprint") {
			t.Errorf("fingerprint details leaked in error message")
		}
	})

	t.Run("Scenario 4: Database dump command failure is non-retryable and marks job failed", func(t *testing.T) {
		tempDir := t.TempDir()
		store, _ := local.NewLocalStorageProvider(tempDir)
		_ = store.EnsureStorageRoot(context.Background())

		workerPool, repo, _, jobID := setupTestWorker(t, &fakeCapability{errToReturn: connector.ErrDumpCommandFailed}, store)

		ctx, cancel := context.WithCancel(context.Background())
		workerPool.Start(ctx)
		time.Sleep(100 * time.Millisecond)
		_ = workerPool.Stop(context.Background())
		cancel()

		repo.mu.Lock()
		defer repo.mu.Unlock()

		if repo.finalizedRun == nil || repo.finalizedRun.Status != domain.RunStatusFailed {
			t.Fatalf("expected run failed on dump command error, got: %+v", repo.finalizedRun)
		}
		if repo.jobs[jobID].Status != domain.JobStatusFailed {
			t.Errorf("expected job failed on dump command failure, got: %s", repo.jobs[jobID].Status)
		}
		if repo.finalizedRun.ErrorMessage == nil || *repo.finalizedRun.ErrorMessage != "database dump command exited with error" {
			t.Errorf("expected safe dump command message, got: %v", repo.finalizedRun.ErrorMessage)
		}
	})

	t.Run("Scenario 5: ENOSPC storage full is non-retryable and marks job failed without path leakage", func(t *testing.T) {
		tempDir := t.TempDir()
		store, _ := local.NewLocalStorageProvider(tempDir)
		_ = store.EnsureStorageRoot(context.Background())

		mockStore := &failingSaveStorageProvider{
			StorageProvider: store,
			saveErr:         storage.ErrStorageFull,
		}

		workerPool, repo, _, jobID := setupTestWorker(t, &fakeCapability{
			sqlDump: "-- MySQL dump 10.13\nCREATE DATABASE `ecommerce_prod`;\n",
		}, mockStore)

		ctx, cancel := context.WithCancel(context.Background())
		workerPool.Start(ctx)
		time.Sleep(100 * time.Millisecond)
		_ = workerPool.Stop(context.Background())
		cancel()

		repo.mu.Lock()
		defer repo.mu.Unlock()

		if repo.finalizedRun == nil || repo.finalizedRun.Status != domain.RunStatusFailed {
			t.Fatalf("expected run failed on storage full, got: %+v", repo.finalizedRun)
		}
		if repo.jobs[jobID].Status != domain.JobStatusFailed {
			t.Errorf("expected job failed on storage full, got: %s", repo.jobs[jobID].Status)
		}
		if repo.finalizedRun.ErrorMessage == nil || *repo.finalizedRun.ErrorMessage != "storage target out of disk space" {
			t.Errorf("expected storage full safe error message, got: %v", repo.finalizedRun.ErrorMessage)
		}
		if strings.Contains(*repo.finalizedRun.ErrorMessage, tempDir) || strings.Contains(*repo.finalizedRun.ErrorMessage, "tmp") {
			t.Errorf("SECURITY FLAW: storage path leaked in error message")
		}
	})

	t.Run("Scenario 6: Real partial-write ENOSPC in LocalStorageProvider cleans partial file cleanly", func(t *testing.T) {
		tempDir := t.TempDir()
		store, err := local.NewLocalStorageProvider(tempDir)
		if err != nil {
			t.Fatalf("failed creating local storage: %v", err)
		}
		_ = store.EnsureStorageRoot(context.Background())

		orgID := uuid.New()
		resID := uuid.New()
		runID := uuid.New()
		artID := uuid.New()

		reader := &partialThenENOSPCReader{}
		_, saveErr := store.SaveArtifact(context.Background(), orgID, resID, runID, artID, ".sql.gz", reader)
		if saveErr == nil {
			t.Fatalf("expected error from ENOSPC, got nil")
		}

		if !errors.Is(saveErr, storage.ErrStorageFull) {
			t.Errorf("expected error compatible with storage.ErrStorageFull, got: %v", saveErr)
		}

		// Ensure no path leaked in error message
		if strings.Contains(saveErr.Error(), tempDir) {
			t.Errorf("path leaked in error message: %s", saveErr.Error())
		}

		// Ensure no partial file remains
		runTempDir := filepath.Join(tempDir, "tmp", fmt.Sprintf("run-%s", runID.String()))
		partialFile := filepath.Join(runTempDir, fmt.Sprintf("artifact-%s.sql.gz.partial", artID.String()))
		if _, statErr := os.Stat(partialFile); !os.IsNotExist(statErr) {
			t.Errorf("partial file was not cleaned up on ENOSPC: %s", partialFile)
		}

		// Ensure empty run temp directory was removed
		if _, statErr := os.Stat(runTempDir); !os.IsNotExist(statErr) {
			t.Errorf("empty run temp directory was not removed on ENOSPC: %s", runTempDir)
		}

		// Ensure no final artifact was created in storage artifacts directory
		artifactsDir := filepath.Join(tempDir, "artifacts")
		if entries, err := os.ReadDir(artifactsDir); err == nil && len(entries) > 0 {
			t.Errorf("expected no final artifacts created, found %d entries", len(entries))
		}
	})
}
