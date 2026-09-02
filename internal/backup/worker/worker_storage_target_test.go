package worker

import (
	"context"
	"errors"
	"io"
	"testing"

	"backup-platform/internal/backup/domain"
	"backup-platform/internal/backup/engine"
	"backup-platform/internal/backup/verification"
	connector "backup-platform/internal/connector"
	"backup-platform/internal/credential/payload"
	resDomain "backup-platform/internal/resource/domain"
	"backup-platform/internal/storage"
	"backup-platform/internal/storage/local"
	"backup-platform/pkg/uuid"
)

type mockStorageResolver struct {
	providers map[uuid.UUID]storage.StorageProvider
}

func (m *mockStorageResolver) Resolve(ctx context.Context, orgID, targetID uuid.UUID) (storage.StorageProvider, error) {
	if p, ok := m.providers[targetID]; ok {
		return p, nil
	}
	return nil, domain.ErrStorageTargetNotFound
}

func TestWorkerPool_StorageTargetResolutionAndEngineValidation(t *testing.T) {
	orgID := uuid.New()
	resID := uuid.New()
	credID := uuid.New()
	s3TargetID := uuid.New()

	fingerprint := "SHA256:test"
	timeout := 10
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
			Port:               22,
			AuthType:           resDomain.AuthTypeSSHPassword,
			HostKeyFingerprint: &fingerprint,
			Config: resDomain.ConnectorConfig{
				Username:                 "testuser",
				ConnectionTimeoutSeconds: &timeout,
			},
		},
	}

	tempDir := t.TempDir()
	s3MockStore, _ := local.NewLocalStorageProvider(tempDir)

	validPassJSON, _ := payload.EncodeV1("testpass", nil)

	resolver := &mockStorageResolver{
		providers: map[uuid.UUID]storage.StorageProvider{
			s3TargetID: s3MockStore,
		},
	}

	t.Run("Unsupported engine_type fails closed with ErrUnsupportedEngineType", func(t *testing.T) {
		repo := newFakeWorkerRepo(orgID)
		pool := NewWorkerPool(
			WorkerPoolConfig{NumWorkers: 1},
			repo,
			&fakeResourceFinder{resWithConn: resWithConn},
			&fakeCredentialVault{payloadBytes: validPassJSON},
			nil, nil,
			engine.NewDirectStreamBackupEngine(),
			s3MockStore,
			verification.NewVerificationEngine(),
			NewPerResourceMutexManager(),
			nil,
		)
		pool.SetStorageResolver(resolver)

		run := &domain.BackupRun{ID: uuid.New(), OrganizationID: orgID, JobID: uuid.New()}
		job := &domain.BackupJob{
			ID:              run.JobID,
			OrganizationID:  orgID,
			ResourceID:      resID,
			BackupType:      domain.BackupTypeMySQLDatabase,
			EngineType:      domain.EngineType("restic_snapshot"),
			StorageTargetID: s3TargetID,
			TargetSpec:      domain.TargetSpec{Databases: []string{"db1"}},
		}

		err := pool.executeBackupPipeline(context.Background(), run, job)
		if !errors.Is(err, domain.ErrUnsupportedEngineType) {
			t.Fatalf("expected ErrUnsupportedEngineType, got %v", err)
		}
	})

	t.Run("Blank engine_type fails closed with ErrInvalidEngineType", func(t *testing.T) {
		repo := newFakeWorkerRepo(orgID)
		pool := NewWorkerPool(
			WorkerPoolConfig{NumWorkers: 1},
			repo,
			&fakeResourceFinder{resWithConn: resWithConn},
			&fakeCredentialVault{payloadBytes: validPassJSON},
			nil, nil,
			engine.NewDirectStreamBackupEngine(),
			s3MockStore,
			verification.NewVerificationEngine(),
			NewPerResourceMutexManager(),
			nil,
		)
		pool.SetStorageResolver(resolver)

		run := &domain.BackupRun{ID: uuid.New(), OrganizationID: orgID, JobID: uuid.New()}
		job := &domain.BackupJob{
			ID:              run.JobID,
			OrganizationID:  orgID,
			ResourceID:      resID,
			BackupType:      domain.BackupTypeMySQLDatabase,
			EngineType:      "", // Blank engine type
			StorageTargetID: s3TargetID,
			TargetSpec:      domain.TargetSpec{Databases: []string{"db1"}},
		}

		err := pool.executeBackupPipeline(context.Background(), run, job)
		if !errors.Is(err, domain.ErrInvalidEngineType) {
			t.Fatalf("expected ErrInvalidEngineType, got %v", err)
		}
	})

	t.Run("Missing StorageTargetID fails closed with ErrStorageTargetNotFound", func(t *testing.T) {
		repo := newFakeWorkerRepo(orgID)
		pool := NewWorkerPool(
			WorkerPoolConfig{NumWorkers: 1},
			repo,
			&fakeResourceFinder{resWithConn: resWithConn},
			&fakeCredentialVault{payloadBytes: validPassJSON},
			nil, nil,
			engine.NewDirectStreamBackupEngine(),
			s3MockStore,
			verification.NewVerificationEngine(),
			NewPerResourceMutexManager(),
			nil,
		)
		pool.SetStorageResolver(resolver)

		run := &domain.BackupRun{ID: uuid.New(), OrganizationID: orgID, JobID: uuid.New()}
		job := &domain.BackupJob{
			ID:              run.JobID,
			OrganizationID:  orgID,
			ResourceID:      resID,
			BackupType:      domain.BackupTypeMySQLDatabase,
			EngineType:      domain.EngineTypeDirectStream,
			StorageTargetID: uuid.Nil, // Missing storage target ID
			TargetSpec:      domain.TargetSpec{Databases: []string{"db1"}},
		}

		err := pool.executeBackupPipeline(context.Background(), run, job)
		if !errors.Is(err, domain.ErrStorageTargetNotFound) {
			t.Fatalf("expected ErrStorageTargetNotFound, got %v", err)
		}
	})

	t.Run("S3 resolver failure fails closed without falling back to Local", func(t *testing.T) {
		repo := newFakeWorkerRepo(orgID)
		repo.targets[s3TargetID] = &domain.StorageTarget{
			ID:             s3TargetID,
			OrganizationID: orgID,
			Name:           "S3 Target",
			Type:           domain.StorageTargetTypeS3,
			Status:         domain.StorageTargetStatusActive,
		}

		failingResolver := &mockStorageResolver{
			providers: map[uuid.UUID]storage.StorageProvider{}, // Empty -> fails
		}

		localStore, _ := local.NewLocalStorageProvider(t.TempDir())
		pool := NewWorkerPool(
			WorkerPoolConfig{NumWorkers: 1},
			repo,
			&fakeResourceFinder{resWithConn: resWithConn},
			&fakeCredentialVault{payloadBytes: validPassJSON},
			nil, nil,
			engine.NewDirectStreamBackupEngine(),
			localStore, // Local store configured on pool
			verification.NewVerificationEngine(),
			NewPerResourceMutexManager(),
			nil,
		)
		pool.SetStorageResolver(failingResolver)

		run := &domain.BackupRun{ID: uuid.New(), OrganizationID: orgID, JobID: uuid.New()}
		job := &domain.BackupJob{
			ID:              run.JobID,
			OrganizationID:  orgID,
			ResourceID:      resID,
			BackupType:      domain.BackupTypeMySQLDatabase,
			EngineType:      domain.EngineTypeDirectStream,
			StorageTargetID: s3TargetID,
			TargetSpec:      domain.TargetSpec{Databases: []string{"db1"}},
		}

		err := pool.executeBackupPipeline(context.Background(), run, job)
		if !errors.Is(err, domain.ErrStorageTargetNotFound) {
			t.Fatalf("expected ErrStorageTargetNotFound from failing resolver, got %v", err)
		}
		if len(repo.artifacts) != 0 {
			t.Fatalf("expected no artifacts created via local fallback, got %d", len(repo.artifacts))
		}
	})

	t.Run("Inactive storage target fails closed with ErrStorageTargetNotActive", func(t *testing.T) {
		repo := newFakeWorkerRepo(orgID)
		inactiveTargetID := uuid.New()
		repo.targets[inactiveTargetID] = &domain.StorageTarget{
			ID:             inactiveTargetID,
			OrganizationID: orgID,
			Name:           "Inactive Target",
			Type:           domain.StorageTargetTypeS3,
			Status:         domain.StorageTargetStatusDisabled,
		}

		pool := NewWorkerPool(
			WorkerPoolConfig{NumWorkers: 1},
			repo,
			&fakeResourceFinder{resWithConn: resWithConn},
			&fakeCredentialVault{payloadBytes: validPassJSON},
			nil, nil,
			engine.NewDirectStreamBackupEngine(),
			s3MockStore,
			verification.NewVerificationEngine(),
			NewPerResourceMutexManager(),
			nil,
		)
		pool.SetStorageResolver(resolver)

		run := &domain.BackupRun{ID: uuid.New(), OrganizationID: orgID, JobID: uuid.New()}
		job := &domain.BackupJob{
			ID:              run.JobID,
			OrganizationID:  orgID,
			ResourceID:      resID,
			BackupType:      domain.BackupTypeMySQLDatabase,
			EngineType:      domain.EngineTypeDirectStream,
			StorageTargetID: inactiveTargetID,
			TargetSpec:      domain.TargetSpec{Databases: []string{"db1"}},
		}

		err := pool.executeBackupPipeline(context.Background(), run, job)
		if !errors.Is(err, domain.ErrStorageTargetNotActive) {
			t.Fatalf("expected ErrStorageTargetNotActive, got %v", err)
		}
	})

	t.Run("Successful direct stream to S3 target persists artifact with S3 target ID", func(t *testing.T) {
		repo := newFakeWorkerRepo(orgID)
		repo.targets[s3TargetID] = &domain.StorageTarget{
			ID:             s3TargetID,
			OrganizationID: orgID,
			Name:           "MinIO S3 Target",
			Type:           domain.StorageTargetTypeS3,
			Status:         domain.StorageTargetStatusActive,
		}

		reg := connector.NewBackupCapabilityRegistry()
		mockCap := &mockMySQLBackupCapability{
			streamData: "CREATE TABLE test; INSERT INTO test VALUES (1);",
		}
		reg.Register(resDomain.TypeUbuntuSSH, mockCap)

		pool := NewWorkerPool(
			WorkerPoolConfig{NumWorkers: 1},
			repo,
			&fakeResourceFinder{resWithConn: resWithConn},
			&fakeCredentialVault{payloadBytes: validPassJSON},
			reg, nil,
			engine.NewDirectStreamBackupEngine(),
			nil, // default storage is nil; must use resolver!
			verification.NewVerificationEngine(),
			NewPerResourceMutexManager(),
			nil,
		)
		pool.SetStorageResolver(resolver)

		run := &domain.BackupRun{ID: uuid.New(), OrganizationID: orgID, JobID: uuid.New()}
		job := &domain.BackupJob{
			ID:              run.JobID,
			OrganizationID:  orgID,
			ResourceID:      resID,
			BackupType:      domain.BackupTypeMySQLDatabase,
			EngineType:      domain.EngineTypeDirectStream,
			StorageTargetID: s3TargetID,
			TargetSpec:      domain.TargetSpec{Databases: []string{"testdb"}},
		}

		err := pool.executeBackupPipeline(context.Background(), run, job)
		if err != nil {
			t.Fatalf("unexpected error executing backup to S3 target: %v", err)
		}

		if len(repo.artifacts) != 1 {
			t.Fatalf("expected 1 artifact created, got %d", len(repo.artifacts))
		}

		for _, art := range repo.artifacts {
			if art.StorageTargetID != s3TargetID {
				t.Fatalf("expected artifact StorageTargetID to be %s, got %s", s3TargetID, art.StorageTargetID)
			}
			if art.VerificationStatus != domain.VerificationStatusVerified {
				t.Fatalf("expected artifact to be verified, got %s", art.VerificationStatus)
			}
		}
	})
}

type mockMySQLBackupCapability struct {
	streamData string
}

func (m *mockMySQLBackupCapability) BackupDatabase(
	ctx context.Context,
	target connector.Target,
	credPayload *payload.PayloadV1,
	databaseName string,
	dest io.Writer,
) error {
	_, err := io.WriteString(dest, m.streamData)
	return err
}
