package service

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"backup-platform/internal/artifactcrypto"
	auditDomain "backup-platform/internal/audit/domain"
	"backup-platform/internal/backup/domain"
	"backup-platform/internal/backup/restic"
	credDomain "backup-platform/internal/credential/domain"
	orgDomain "backup-platform/internal/organization/domain"
	"backup-platform/internal/storage"
	"backup-platform/pkg/uuid"
)

type mockArtifactRepo struct {
	getArtifactByIDFunc   func(ctx context.Context, orgID, artifactID uuid.UUID) (*domain.BackupArtifact, error)
	listArtifactsFunc     func(ctx context.Context, orgID uuid.UUID) ([]*domain.BackupArtifact, error)
	tombstoneArtifactFunc func(ctx context.Context, orgID, artifactID uuid.UUID) error
}

func (m *mockArtifactRepo) GetArtifactByID(ctx context.Context, orgID, artifactID uuid.UUID) (*domain.BackupArtifact, error) {
	if m.getArtifactByIDFunc != nil {
		return m.getArtifactByIDFunc(ctx, orgID, artifactID)
	}
	return nil, domain.ErrArtifactNotFound
}

func (m *mockArtifactRepo) ListArtifacts(ctx context.Context, orgID uuid.UUID) ([]*domain.BackupArtifact, error) {
	if m.listArtifactsFunc != nil {
		return m.listArtifactsFunc(ctx, orgID)
	}
	return nil, nil
}

func (m *mockArtifactRepo) TombstoneArtifact(ctx context.Context, orgID, artifactID uuid.UUID) error {
	if m.tombstoneArtifactFunc != nil {
		return m.tombstoneArtifactFunc(ctx, orgID, artifactID)
	}
	return nil
}

func (m *mockArtifactRepo) EnsureDefaultLocalStorageTarget(ctx context.Context, orgID uuid.UUID) (*domain.StorageTarget, error) {
	return nil, nil
}
func (m *mockArtifactRepo) GetStorageTargetByID(ctx context.Context, orgID, targetID uuid.UUID) (*domain.StorageTarget, error) {
	return nil, nil
}
func (m *mockArtifactRepo) GetPlanByID(ctx context.Context, orgID, planID uuid.UUID) (*domain.BackupPlan, error) {
	return nil, nil
}
func (m *mockArtifactRepo) CreateJob(ctx context.Context, job *domain.BackupJob) (*domain.BackupJob, error) {
	return nil, nil
}
func (m *mockArtifactRepo) GetJobByID(ctx context.Context, orgID, jobID uuid.UUID) (*domain.BackupJob, error) {
	return nil, nil
}
func (m *mockArtifactRepo) GetActiveManualJobForResource(ctx context.Context, orgID, resourceID uuid.UUID) (*domain.BackupJob, error) {
	return nil, nil
}
func (m *mockArtifactRepo) GetActiveJobConflictForResource(ctx context.Context, orgID, resourceID uuid.UUID) (*domain.BackupJob, error) {
	return nil, nil
}
func (m *mockArtifactRepo) FindPendingJobs(ctx context.Context, limit int, afterCreatedAt *time.Time, afterID *uuid.UUID) ([]*domain.BackupJob, error) {
	return nil, nil
}
func (m *mockArtifactRepo) TransactionalClaimJob(ctx context.Context, orgID, jobID uuid.UUID) (*domain.BackupRun, *domain.BackupJob, error) {
	return nil, nil, nil
}
func (m *mockArtifactRepo) GetRunByID(ctx context.Context, orgID, runID uuid.UUID) (*domain.BackupRun, error) {
	return nil, nil
}
func (m *mockArtifactRepo) GetRunDetail(ctx context.Context, orgID, runID uuid.UUID) (*domain.BackupRunWithStats, error) {
	return nil, nil
}
func (m *mockArtifactRepo) ListRuns(ctx context.Context, orgID uuid.UUID, filter domain.RunFilter) ([]*domain.BackupRunWithStats, error) {
	return nil, nil
}
func (m *mockArtifactRepo) ListSuccessfulRunsForPlan(ctx context.Context, orgID, planID uuid.UUID) ([]*domain.BackupRun, error) {
	return nil, nil
}
func (m *mockArtifactRepo) GetLatestRunForJob(ctx context.Context, orgID, jobID uuid.UUID) (*domain.BackupRun, error) {
	return nil, nil
}
func (m *mockArtifactRepo) UpdateHeartbeat(ctx context.Context, orgID, runID uuid.UUID) error {
	return nil
}
func (m *mockArtifactRepo) FinalizeRunAndJob(ctx context.Context, orgID, runID, jobID uuid.UUID, runStatus domain.RunStatus, jobStatus domain.JobStatus, errMsg *string, logsSummary []byte) error {
	return nil
}
func (m *mockArtifactRepo) CreateArtifact(ctx context.Context, artifact *domain.BackupArtifact) (*domain.BackupArtifact, error) {
	return nil, nil
}
func (m *mockArtifactRepo) UpdateArtifactVerification(ctx context.Context, orgID, artifactID uuid.UUID, status domain.VerificationStatus, details *string) error {
	return nil
}
func (m *mockArtifactRepo) GetRunArtifacts(ctx context.Context, orgID, runID uuid.UUID) ([]*domain.BackupArtifact, error) {
	return nil, nil
}
func (m *mockArtifactRepo) RecoverInterruptedRuns(ctx context.Context) ([]domain.RecoveredRunInfo, error) {
	return nil, nil
}
func (m *mockArtifactRepo) ReapStaleRuns(ctx context.Context) ([]domain.RecoveredRunInfo, error) {
	return nil, nil
}
func (m *mockArtifactRepo) CreateStorageTarget(ctx context.Context, target *domain.StorageTarget) (*domain.StorageTarget, error) {
	return nil, nil
}
func (m *mockArtifactRepo) ListStorageTargets(ctx context.Context, orgID uuid.UUID) ([]*domain.StorageTarget, error) {
	return nil, nil
}
func (m *mockArtifactRepo) UpdateStorageTarget(ctx context.Context, target *domain.StorageTarget) (*domain.StorageTarget, error) {
	return nil, nil
}
func (m *mockArtifactRepo) DeleteStorageTarget(ctx context.Context, orgID, targetID uuid.UUID) error {
	return nil
}
func (m *mockArtifactRepo) CountArtifactsByStorageTarget(ctx context.Context, orgID, targetID uuid.UUID) (int64, error) {
	return 0, nil
}
func (m *mockArtifactRepo) CountPlansByStorageTarget(ctx context.Context, orgID, targetID uuid.UUID) (int64, error) {
	return 0, nil
}
func (m *mockArtifactRepo) CountActiveJobsByStorageTarget(ctx context.Context, orgID, targetID uuid.UUID) (int64, error) {
	return 0, nil
}
func (m *mockArtifactRepo) CountRepositoriesByStorageTarget(ctx context.Context, orgID, targetID uuid.UUID) (int64, error) {
	return 0, nil
}
func (m *mockArtifactRepo) CreateRepository(ctx context.Context, repo *domain.BackupRepository) (*domain.BackupRepository, error) {
	return nil, nil
}
func (m *mockArtifactRepo) GetRepositoryByResourceID(ctx context.Context, orgID, resourceID uuid.UUID) (*domain.BackupRepository, error) {
	return nil, nil
}
func (m *mockArtifactRepo) GetRepositoryByID(ctx context.Context, orgID, repoID uuid.UUID) (*domain.BackupRepository, error) {
	return nil, nil
}

type mockStorageProvider struct {
	openArtifactFunc   func(ctx context.Context, storageRef string) (io.ReadCloser, error)
	deleteArtifactFunc func(ctx context.Context, storageRef string) error
}

func (m *mockStorageProvider) OpenArtifact(ctx context.Context, storageRef string) (io.ReadCloser, error) {
	if m.openArtifactFunc != nil {
		return m.openArtifactFunc(ctx, storageRef)
	}
	return nil, storage.ErrArtifactNotFound
}

func (m *mockStorageProvider) DeleteArtifact(ctx context.Context, storageRef string) error {
	if m.deleteArtifactFunc != nil {
		return m.deleteArtifactFunc(ctx, storageRef)
	}
	return nil
}

func (m *mockStorageProvider) SaveArtifact(ctx context.Context, orgID, resID, runID, artifactID uuid.UUID, extension string, src io.Reader) (*storage.SaveResult, error) {
	return &storage.SaveResult{}, nil
}

func (m *mockStorageProvider) EnsureStorageRoot(ctx context.Context) error {
	return nil
}

type mockAuditService struct {
	recordFunc func(ctx context.Context, entry *auditDomain.AuditLog) error
}

func (m *mockAuditService) Record(ctx context.Context, entry *auditDomain.AuditLog) error {
	if m.recordFunc != nil {
		return m.recordFunc(ctx, entry)
	}
	return nil
}

func TestArtifactService_ListAndGet(t *testing.T) {
	orgID := uuid.New()
	artID := uuid.New()

	t.Run("ListArtifacts returns active artifacts", func(t *testing.T) {
		repo := &mockArtifactRepo{
			listArtifactsFunc: func(ctx context.Context, oID uuid.UUID) ([]*domain.BackupArtifact, error) {
				return []*domain.BackupArtifact{
					{ID: artID, OrganizationID: oID, TargetName: "db1"},
				}, nil
			},
		}

		svc := NewArtifactService(repo, &mockStorageProvider{}, &mockAuditService{}, nil)
		arts, err := svc.ListArtifacts(context.Background(), orgDomain.RoleViewer, orgID)
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if len(arts) != 1 || arts[0].ID != artID {
			t.Fatalf("unexpected artifacts list")
		}
	})

	t.Run("GetArtifact returns active artifact", func(t *testing.T) {
		repo := &mockArtifactRepo{
			getArtifactByIDFunc: func(ctx context.Context, oID, aID uuid.UUID) (*domain.BackupArtifact, error) {
				return &domain.BackupArtifact{ID: aID, OrganizationID: oID, IsDeleted: false}, nil
			},
		}

		svc := NewArtifactService(repo, &mockStorageProvider{}, &mockAuditService{}, nil)
		art, err := svc.GetArtifact(context.Background(), orgDomain.RoleMember, orgID, artID)
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if art.ID != artID {
			t.Fatalf("unexpected artifact ID")
		}
	})

	t.Run("GetArtifact returns ErrArtifactNotFound if tombstoned", func(t *testing.T) {
		repo := &mockArtifactRepo{
			getArtifactByIDFunc: func(ctx context.Context, oID, aID uuid.UUID) (*domain.BackupArtifact, error) {
				return &domain.BackupArtifact{ID: aID, OrganizationID: oID, IsDeleted: true}, nil
			},
		}

		svc := NewArtifactService(repo, &mockStorageProvider{}, &mockAuditService{}, nil)
		_, err := svc.GetArtifact(context.Background(), orgDomain.RoleAdmin, orgID, artID)
		if !errors.Is(err, domain.ErrArtifactNotFound) {
			t.Fatalf("expected ErrArtifactNotFound for deleted artifact, got: %v", err)
		}
	})
}

func TestArtifactService_OpenArtifactDownload(t *testing.T) {
	orgID := uuid.New()
	artID := uuid.New()
	content := []byte("gzipped-tar-data")

	t.Run("allows admin and member to open download stream", func(t *testing.T) {
		repo := &mockArtifactRepo{
			getArtifactByIDFunc: func(ctx context.Context, oID, aID uuid.UUID) (*domain.BackupArtifact, error) {
				return &domain.BackupArtifact{
					ID:               aID,
					OrganizationID:   oID,
					StorageReference: "local://org/db.sql.gz",
					IsDeleted:        false,
				}, nil
			},
		}

		stor := &mockStorageProvider{
			openArtifactFunc: func(ctx context.Context, storageRef string) (io.ReadCloser, error) {
				return io.NopCloser(bytes.NewReader(content)), nil
			},
		}

		svc := NewArtifactService(repo, stor, &mockAuditService{}, nil)

		desc, err := svc.OpenArtifactDownload(context.Background(), orgDomain.RoleMember, orgID, artID)
		if err != nil {
			t.Fatalf("expected no error for member, got: %v", err)
		}
		defer desc.Reader.Close()

		if desc.Artifact.ID != artID {
			t.Fatalf("unexpected artifact")
		}
	})

	t.Run("rejects viewer with ErrUnauthorizedRole", func(t *testing.T) {
		svc := NewArtifactService(&mockArtifactRepo{}, &mockStorageProvider{}, &mockAuditService{}, nil)
		_, err := svc.OpenArtifactDownload(context.Background(), orgDomain.RoleViewer, orgID, artID)
		if !errors.Is(err, domain.ErrUnauthorizedRole) {
			t.Fatalf("expected ErrUnauthorizedRole for viewer, got: %v", err)
		}
	})

	t.Run("rejects deleted artifact without calling storage provider", func(t *testing.T) {
		repo := &mockArtifactRepo{
			getArtifactByIDFunc: func(ctx context.Context, oID, aID uuid.UUID) (*domain.BackupArtifact, error) {
				return &domain.BackupArtifact{ID: aID, OrganizationID: oID, IsDeleted: true}, nil
			},
		}

		var storageCalled bool
		stor := &mockStorageProvider{
			openArtifactFunc: func(ctx context.Context, storageRef string) (io.ReadCloser, error) {
				storageCalled = true
				return nil, nil
			},
		}

		svc := NewArtifactService(repo, stor, &mockAuditService{}, nil)
		_, err := svc.OpenArtifactDownload(context.Background(), orgDomain.RoleAdmin, orgID, artID)
		if !errors.Is(err, domain.ErrArtifactNotFound) {
			t.Fatalf("expected ErrArtifactNotFound, got: %v", err)
		}
		if storageCalled {
			t.Fatalf("storage provider should not be called on deleted artifact")
		}
	})

	t.Run("encrypted artifact download streams decrypted plaintext", func(t *testing.T) {
		kp, err := artifactcrypto.NewStaticKeyProvider(bytes.Repeat([]byte{0x88}, 32), 1)
		if err != nil {
			t.Fatalf("failed creating key provider: %v", err)
		}

		plainContent := []byte("plain SQL database content for download verification")
		var bpaeBuf bytes.Buffer
		encWriter, err := artifactcrypto.NewEncryptWriter(&bpaeBuf, kp, orgID, artID)
		if err != nil {
			t.Fatalf("failed creating encrypt writer: %v", err)
		}
		if _, err := encWriter.Write(plainContent); err != nil {
			t.Fatalf("failed writing plaintext: %v", err)
		}
		if err := encWriter.Close(); err != nil {
			t.Fatalf("failed closing encWriter: %v", err)
		}

		ciphertext := bpaeBuf.Bytes()
		storedSize := int64(len(ciphertext))

		repo := &mockArtifactRepo{
			getArtifactByIDFunc: func(ctx context.Context, oID, aID uuid.UUID) (*domain.BackupArtifact, error) {
				return &domain.BackupArtifact{
					ID:               aID,
					OrganizationID:   oID,
					StorageReference: "local://org/db.sql.gz",
					TargetName:       "ecommerce",
					SizeBytes:        int64(len(plainContent)),
					StoredSizeBytes:  &storedSize,
					IsDeleted:        false,
				}, nil
			},
		}

		stor := &mockStorageProvider{
			openArtifactFunc: func(ctx context.Context, storageRef string) (io.ReadCloser, error) {
				return io.NopCloser(bytes.NewReader(ciphertext)), nil
			},
		}

		svc := NewArtifactService(repo, stor, &mockAuditService{}, nil)
		svc.SetKeyProvider(kp)

		desc, err := svc.OpenArtifactDownload(context.Background(), orgDomain.RoleAdmin, orgID, artID)
		if err != nil {
			t.Fatalf("expected successful download open, got: %v", err)
		}
		defer desc.Reader.Close()

		if desc.Artifact.ID != artID {
			t.Errorf("expected artifact ID %s, got %s", artID, desc.Artifact.ID)
		}

		decrypted, err := io.ReadAll(desc.Reader)
		if err != nil {
			t.Fatalf("failed reading decrypted stream: %v", err)
		}

		if !bytes.Equal(decrypted, plainContent) {
			t.Errorf("decrypted content mismatch: expected %q, got %q", string(plainContent), string(decrypted))
		}
	})

	t.Run("encrypted artifact download fails if key provider missing", func(t *testing.T) {
		storedSize := int64(100)
		repo := &mockArtifactRepo{
			getArtifactByIDFunc: func(ctx context.Context, oID, aID uuid.UUID) (*domain.BackupArtifact, error) {
				return &domain.BackupArtifact{
					ID:               aID,
					OrganizationID:   oID,
					StorageReference: "local://org/db.sql.gz",
					StoredSizeBytes:  &storedSize,
					IsDeleted:        false,
				}, nil
			},
		}

		stor := &mockStorageProvider{
			openArtifactFunc: func(ctx context.Context, storageRef string) (io.ReadCloser, error) {
				return io.NopCloser(bytes.NewReader([]byte("ciphertext"))), nil
			},
		}

		svc := NewArtifactService(repo, stor, &mockAuditService{}, nil) // no key provider

		_, err := svc.OpenArtifactDownload(context.Background(), orgDomain.RoleAdmin, orgID, artID)
		if err == nil {
			t.Fatal("expected error on missing key provider, got nil")
		}
	})

	t.Run("encrypted artifact download fails with safe error on unknown key version", func(t *testing.T) {
		kpV1, _ := artifactcrypto.NewStaticKeyProvider(bytes.Repeat([]byte{0x11}, 32), 1)
		plainContent := []byte("plain SQL database content for download verification")
		var bpaeBuf bytes.Buffer
		encWriter, err := artifactcrypto.NewEncryptWriter(&bpaeBuf, kpV1, orgID, artID)
		if err != nil {
			t.Fatalf("failed creating encrypt writer: %v", err)
		}
		_, _ = encWriter.Write(plainContent)
		_ = encWriter.Close()

		ciphertext := bpaeBuf.Bytes()
		storedSize := int64(len(ciphertext))

		repo := &mockArtifactRepo{
			getArtifactByIDFunc: func(ctx context.Context, oID, aID uuid.UUID) (*domain.BackupArtifact, error) {
				return &domain.BackupArtifact{
					ID:               aID,
					OrganizationID:   oID,
					StorageReference: "local://org/db.sql.gz",
					TargetName:       "ecommerce",
					SizeBytes:        int64(len(plainContent)),
					StoredSizeBytes:  &storedSize,
					IsDeleted:        false,
				}, nil
			},
		}

		stor := &mockStorageProvider{
			openArtifactFunc: func(ctx context.Context, storageRef string) (io.ReadCloser, error) {
				return io.NopCloser(bytes.NewReader(ciphertext)), nil
			},
		}

		// Service has key provider configured for version 2 only (version 1 is unknown)
		kpV2, _ := artifactcrypto.NewStaticKeyProvider(bytes.Repeat([]byte{0x22}, 32), 2)
		svc := NewArtifactService(repo, stor, &mockAuditService{}, nil)
		svc.SetKeyProvider(kpV2)

		_, err = svc.OpenArtifactDownload(context.Background(), orgDomain.RoleAdmin, orgID, artID)
		if err == nil {
			t.Fatal("expected error on unknown key version, got nil")
		}
		if !errors.Is(err, artifactcrypto.ErrUnknownKeyVersion) {
			t.Errorf("expected ErrUnknownKeyVersion, got: %v", err)
		}
	})

	t.Run("missing or corrupted FINAL produces error and does not return complete plaintext", func(t *testing.T) {
		kp, _ := artifactcrypto.NewStaticKeyProvider(bytes.Repeat([]byte{0x33}, 32), 1)
		plainContent := bytes.Repeat([]byte("large-database-content-chunk-"), 3000) // ~90KB (multiple chunks)
		var bpaeBuf bytes.Buffer
		encWriter, err := artifactcrypto.NewEncryptWriter(&bpaeBuf, kp, orgID, artID)
		if err != nil {
			t.Fatalf("failed creating encrypt writer: %v", err)
		}
		_, _ = encWriter.Write(plainContent)
		_ = encWriter.Close()

		// Truncate the buffer to drop the FINAL chunk entirely
		truncatedBytes := bpaeBuf.Bytes()[:bpaeBuf.Len()-60]
		storedSize := int64(len(truncatedBytes))

		repo := &mockArtifactRepo{
			getArtifactByIDFunc: func(ctx context.Context, oID, aID uuid.UUID) (*domain.BackupArtifact, error) {
				return &domain.BackupArtifact{
					ID:               aID,
					OrganizationID:   oID,
					StorageReference: "local://org/db.sql.gz",
					TargetName:       "ecommerce",
					SizeBytes:        int64(len(plainContent)),
					StoredSizeBytes:  &storedSize,
					IsDeleted:        false,
				}, nil
			},
		}

		stor := &mockStorageProvider{
			openArtifactFunc: func(ctx context.Context, storageRef string) (io.ReadCloser, error) {
				return io.NopCloser(bytes.NewReader(truncatedBytes)), nil
			},
		}

		svc := NewArtifactService(repo, stor, &mockAuditService{}, nil)
		svc.SetKeyProvider(kp)

		desc, err := svc.OpenArtifactDownload(context.Background(), orgDomain.RoleAdmin, orgID, artID)
		if err != nil {
			t.Fatalf("unexpected error on open: %v", err)
		}
		defer desc.Reader.Close()

		// Reading until the end must produce an error and NOT succeed with all plaintext
		readBuf := make([]byte, len(plainContent)+1000)
		totalRead := 0
		var streamErr error
		for {
			n, rErr := desc.Reader.Read(readBuf[totalRead:])
			totalRead += n
			if rErr != nil {
				streamErr = rErr
				break
			}
		}
		if streamErr == nil || errors.Is(streamErr, io.EOF) {
			t.Fatalf("expected stream error on missing FINAL chunk, got: %v", streamErr)
		}
		if totalRead >= len(plainContent) {
			t.Fatalf("stream must not return full plaintext when FINAL is missing, got %d of %d", totalRead, len(plainContent))
		}
	})
}

func TestArtifactService_DeleteArtifact(t *testing.T) {
	orgID := uuid.New()
	userID := uuid.New()
	artID := uuid.New()

	t.Run("performs physical delete first, then tombstones metadata, then audits", func(t *testing.T) {
		var stepOrder []string
		repo := &mockArtifactRepo{
			getArtifactByIDFunc: func(ctx context.Context, oID, aID uuid.UUID) (*domain.BackupArtifact, error) {
				stepOrder = append(stepOrder, "get_metadata")
				return &domain.BackupArtifact{
					ID:               aID,
					OrganizationID:   oID,
					StorageReference: "local://org/db.sql.gz",
					TargetName:       "db1",
					SizeBytes:        5000,
					IsDeleted:        false,
				}, nil
			},
			tombstoneArtifactFunc: func(ctx context.Context, oID, aID uuid.UUID) error {
				stepOrder = append(stepOrder, "db_tombstone")
				return nil
			},
		}

		stor := &mockStorageProvider{
			deleteArtifactFunc: func(ctx context.Context, storageRef string) error {
				stepOrder = append(stepOrder, "physical_delete")
				return nil
			},
		}

		var auditRecorded bool
		auditSvc := &mockAuditService{
			recordFunc: func(ctx context.Context, entry *auditDomain.AuditLog) error {
				if entry.Action == auditDomain.ActionBackupDelete {
					auditRecorded = true
				}
				stepOrder = append(stepOrder, "audit_record")
				return nil
			},
		}

		svc := NewArtifactService(repo, stor, auditSvc, nil)

		err := svc.DeleteArtifact(context.Background(), orgDomain.RoleAdmin, orgID, userID, artID, "127.0.0.1", "curl/8.0")
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}

		if len(stepOrder) != 4 ||
			stepOrder[0] != "get_metadata" ||
			stepOrder[1] != "physical_delete" ||
			stepOrder[2] != "db_tombstone" ||
			stepOrder[3] != "audit_record" {
			t.Fatalf("unexpected execution sequence: %v", stepOrder)
		}

		if !auditRecorded {
			t.Fatalf("expected delete audit log to be recorded")
		}
	})

	t.Run("aborts and DOES NOT tombstone when physical delete fails", func(t *testing.T) {
		repo := &mockArtifactRepo{
			getArtifactByIDFunc: func(ctx context.Context, oID, aID uuid.UUID) (*domain.BackupArtifact, error) {
				return &domain.BackupArtifact{
					ID:               aID,
					OrganizationID:   oID,
					StorageReference: "local://org/db.sql.gz",
					IsDeleted:        false,
				}, nil
			},
			tombstoneArtifactFunc: func(ctx context.Context, oID, aID uuid.UUID) error {
				t.Fatalf("database tombstone MUST NOT be called when physical delete fails!")
				return nil
			},
		}

		stor := &mockStorageProvider{
			deleteArtifactFunc: func(ctx context.Context, storageRef string) error {
				return errors.New("disk I/O error or permission denied")
			},
		}

		svc := NewArtifactService(repo, stor, &mockAuditService{}, nil)

		err := svc.DeleteArtifact(context.Background(), orgDomain.RoleAdmin, orgID, userID, artID, "127.0.0.1", "test")
		if !errors.Is(err, domain.ErrArtifactDeleteFailed) {
			t.Fatalf("expected ErrArtifactDeleteFailed, got: %v", err)
		}
	})

	t.Run("aborts and DOES NOT tombstone when storage provider returns ErrInvalidStorageReference", func(t *testing.T) {
		repo := &mockArtifactRepo{
			getArtifactByIDFunc: func(ctx context.Context, oID, aID uuid.UUID) (*domain.BackupArtifact, error) {
				return &domain.BackupArtifact{
					ID:               aID,
					OrganizationID:   oID,
					StorageReference: "invalid/malformed/ref",
					IsDeleted:        false,
				}, nil
			},
			tombstoneArtifactFunc: func(ctx context.Context, oID, aID uuid.UUID) error {
				t.Fatalf("database tombstone MUST NOT be called on ErrInvalidStorageReference!")
				return nil
			},
		}

		stor := &mockStorageProvider{
			deleteArtifactFunc: func(ctx context.Context, storageRef string) error {
				return storage.ErrInvalidStorageReference
			},
		}

		svc := NewArtifactService(repo, stor, &mockAuditService{}, nil)

		err := svc.DeleteArtifact(context.Background(), orgDomain.RoleAdmin, orgID, userID, artID, "127.0.0.1", "test")
		if !errors.Is(err, domain.ErrArtifactDeleteFailed) {
			t.Fatalf("expected ErrArtifactDeleteFailed, got: %v", err)
		}
	})

	t.Run("rejects non-admin roles with ErrUnauthorizedRole", func(t *testing.T) {
		svc := NewArtifactService(&mockArtifactRepo{}, &mockStorageProvider{}, &mockAuditService{}, nil)
		err := svc.DeleteArtifact(context.Background(), orgDomain.RoleMember, orgID, userID, artID, "", "")
		if !errors.Is(err, domain.ErrUnauthorizedRole) {
			t.Fatalf("expected ErrUnauthorizedRole for member, got: %v", err)
		}
	})

	t.Run("completes delete successfully even if audit recording fails (no fake rollback)", func(t *testing.T) {
		repo := &mockArtifactRepo{
			getArtifactByIDFunc: func(ctx context.Context, oID, aID uuid.UUID) (*domain.BackupArtifact, error) {
				return &domain.BackupArtifact{
					ID:               aID,
					OrganizationID:   oID,
					StorageReference: "local://org/db.sql.gz",
					TargetName:       "db1",
					SizeBytes:        5000,
					IsDeleted:        false,
				}, nil
			},
			tombstoneArtifactFunc: func(ctx context.Context, oID, aID uuid.UUID) error {
				return nil
			},
		}

		stor := &mockStorageProvider{
			deleteArtifactFunc: func(ctx context.Context, storageRef string) error {
				return nil
			},
		}

		auditSvc := &mockAuditService{
			recordFunc: func(ctx context.Context, entry *auditDomain.AuditLog) error {
				return errors.New("audit database unavailable")
			},
		}

		svc := NewArtifactService(repo, stor, auditSvc, nil)
		err := svc.DeleteArtifact(context.Background(), orgDomain.RoleAdmin, orgID, userID, artID, "127.0.0.1", "test")
		if err != nil {
			t.Fatalf("expected delete to succeed without fake rollback when audit fails, got: %v", err)
		}
	})
}

func TestArtifactService_RecordDownloadAudit(t *testing.T) {
	orgID := uuid.New()
	userID := uuid.New()
	artID := uuid.New()

	t.Run("records download audit successfully", func(t *testing.T) {
		var recorded *auditDomain.AuditLog
		auditSvc := &mockAuditService{
			recordFunc: func(ctx context.Context, entry *auditDomain.AuditLog) error {
				recorded = entry
				return nil
			},
		}

		svc := NewArtifactService(&mockArtifactRepo{}, &mockStorageProvider{}, auditSvc, nil)
		err := svc.RecordDownloadAudit(context.Background(), orgID, userID, artID, 4096, "192.168.1.1", "curl/8.0")
		if err != nil {
			t.Fatalf("expected nil error, got: %v", err)
		}
		if recorded == nil || recorded.Action != auditDomain.ActionBackupDownload {
			t.Fatalf("expected download audit record, got: %+v", recorded)
		}
	})

	t.Run("returns error and logs when audit recording fails", func(t *testing.T) {
		auditSvc := &mockAuditService{
			recordFunc: func(ctx context.Context, entry *auditDomain.AuditLog) error {
				return errors.New("audit db failure")
			},
		}

		svc := NewArtifactService(&mockArtifactRepo{}, &mockStorageProvider{}, auditSvc, nil)
		err := svc.RecordDownloadAudit(context.Background(), orgID, userID, artID, 4096, "192.168.1.1", "curl/8.0")
		if err == nil {
			t.Fatalf("expected error when audit recorder fails")
		}
	})
}

type mockResticRunnerForDownload struct {
	restic.CommandRunner
	dumpStreamFunc func(ctx context.Context, target restic.RepositoryTarget, password []byte, snapshotID, internalFilename string) (io.ReadCloser, error)
	listNodesFunc  func(ctx context.Context, target restic.RepositoryTarget, password []byte, snapshotID string) ([]restic.SnapshotNode, error)
}

func (m *mockResticRunnerForDownload) DumpStream(ctx context.Context, target restic.RepositoryTarget, password []byte, snapshotID, internalFilename string) (io.ReadCloser, error) {
	if m.dumpStreamFunc != nil {
		return m.dumpStreamFunc(ctx, target, password, snapshotID, internalFilename)
	}
	return nil, errors.New("not implemented")
}

func (m *mockResticRunnerForDownload) ListSnapshotNodes(ctx context.Context, target restic.RepositoryTarget, password []byte, snapshotID string) ([]restic.SnapshotNode, error) {
	if m.listNodesFunc != nil {
		return m.listNodesFunc(ctx, target, password, snapshotID)
	}
	return []restic.SnapshotNode{{Name: "ecommerce.sql", Type: "file"}}, nil
}

type mockTargetResolverForDownload struct {
	resolveTargetFunc func(ctx context.Context, orgID, resourceID uuid.UUID, target *domain.StorageTarget) (restic.RepositoryTarget, error)
}

func (m *mockTargetResolverForDownload) ResolveTarget(ctx context.Context, orgID, resourceID uuid.UUID, target *domain.StorageTarget) (restic.RepositoryTarget, error) {
	if m.resolveTargetFunc != nil {
		return m.resolveTargetFunc(ctx, orgID, resourceID, target)
	}
	return restic.NewLocalRepositoryTarget("/tmp/repo", orgID, resourceID)
}

type mockVaultForDownload struct {
	loadCredFunc func(ctx context.Context, orgID, credID uuid.UUID) (credDomain.Type, []byte, error)
}

func (m *mockVaultForDownload) CreateSystemCredential(ctx context.Context, orgID uuid.UUID, name string, credType credDomain.Type, plaintextPayload []byte) (*credDomain.CredentialMetadata, error) {
	return nil, nil
}
func (m *mockVaultForDownload) LoadCredentialForUse(ctx context.Context, orgID, credID uuid.UUID) (credDomain.Type, []byte, error) {
	if m.loadCredFunc != nil {
		return m.loadCredFunc(ctx, orgID, credID)
	}
	return credDomain.TypeResticRepositoryKey, []byte("password123"), nil
}
func (m *mockVaultForDownload) DeleteSystemCredential(ctx context.Context, orgID, credID uuid.UUID) error {
	return nil
}

func TestArtifactService_ResticDownload(t *testing.T) {
	orgID := uuid.New()
	artID := uuid.New()
	repoID := uuid.New()
	targetID := uuid.New()
	resID := uuid.New()
	credID := uuid.New()

	rawDumpContent := []byte("CREATE TABLE customers (id INT PRIMARY KEY); INSERT INTO customers VALUES (1);")

	resticArt := &domain.BackupArtifact{
		ID:               artID,
		OrganizationID:   orgID,
		Format:           domain.ArtifactFormatResticSnapshot,
		ArtifactType:     domain.ArtifactTypeDatabaseDump,
		TargetName:       "ecommerce",
		RepositoryID:     &repoID,
		SnapshotID:       "snap123456",
		StorageTargetID:  targetID,
		LogicalSizeBytes: int64Ptr(int64(len(rawDumpContent))),
	}

	backupRepo := &mockArtifactRepo{
		getArtifactByIDFunc: func(ctx context.Context, oID, aID uuid.UUID) (*domain.BackupArtifact, error) {
			if aID == artID {
				return resticArt, nil
			}
			return nil, domain.ErrArtifactNotFound
		},
	}

	coordinator := restic.NewRepositoryOperationCoordinator()
	runner := &mockResticRunnerForDownload{
		dumpStreamFunc: func(ctx context.Context, target restic.RepositoryTarget, password []byte, snapshotID, internalFilename string) (io.ReadCloser, error) {
			if snapshotID != "snap123456" {
				return nil, errors.New("wrong snapshot ID")
			}
			return io.NopCloser(bytes.NewReader(rawDumpContent)), nil
		},
	}
	vault := &mockVaultForDownload{}
	targetResolver := &mockTargetResolverForDownload{}

	t.Run("successful restic streaming download with on-the-fly gzip", func(t *testing.T) {
		mockRepoFull := &mockRepoWithResticGet{
			mockArtifactRepo: backupRepo,
			repoRecord: &domain.BackupRepository{
				ID:              repoID,
				OrganizationID:  orgID,
				ResourceID:      resID,
				StorageTargetID: targetID,
				CredentialID:    credID,
			},
			storageTargetRecord: &domain.StorageTarget{
				ID:             targetID,
				OrganizationID: orgID,
				Type:           domain.StorageTargetTypeLocal,
				Status:         domain.StorageTargetStatusActive,
			},
		}

		svc := NewArtifactService(mockRepoFull, &mockStorageProvider{}, &mockAuditService{}, nil)
		svc.SetResticDependencies(runner, coordinator, vault, targetResolver)

		desc, err := svc.OpenArtifactDownload(context.Background(), orgDomain.RoleAdmin, orgID, artID)
		if err != nil {
			t.Fatalf("expected successful restic download open, got: %v", err)
		}
		defer desc.Reader.Close()

		if desc.ContentType != "application/gzip" {
			t.Errorf("expected application/gzip content type, got: %s", desc.ContentType)
		}
		if desc.OptionalContentLength != nil {
			t.Errorf("expected nil OptionalContentLength for chunked restic stream, got: %v", desc.OptionalContentLength)
		}
		if !strings.HasSuffix(desc.Filename, ".sql.gz") {
			t.Errorf("expected filename ending in .sql.gz, got: %s", desc.Filename)
		}

		// Verify coordinator lock is held (exclusive acquire should time out)
		lockCtx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		_, errEx := coordinator.AcquireExclusive(lockCtx, repoID)
		cancel()
		if errEx == nil {
			t.Errorf("expected shared lock to prevent exclusive lock while stream is open")
		}

		// Read and decompress
		gzReader, err := gzip.NewReader(desc.Reader)
		if err != nil {
			t.Fatalf("failed creating gzip reader: %v", err)
		}
		decompressed, err := io.ReadAll(gzReader)
		if err != nil {
			t.Fatalf("failed reading decompressed stream: %v", err)
		}
		_ = gzReader.Close()

		if !bytes.Equal(decompressed, rawDumpContent) {
			t.Errorf("decompressed content mismatch: expected %q, got %q", string(rawDumpContent), string(decompressed))
		}

		// Close reader and verify lock is released
		if err := desc.Reader.Close(); err != nil {
			t.Errorf("failed closing reader: %v", err)
		}

		lockCtx2, cancel2 := context.WithTimeout(context.Background(), 100*time.Millisecond)
		relEx, errEx2 := coordinator.AcquireExclusive(lockCtx2, repoID)
		cancel2()
		if errEx2 != nil {
			t.Errorf("expected shared lock to be released after reader close, got: %v", errEx2)
		} else {
			relEx()
		}
	})

	t.Run("fails when restic dependencies are nil", func(t *testing.T) {
		svc := NewArtifactService(backupRepo, &mockStorageProvider{}, &mockAuditService{}, nil)
		_, err := svc.OpenArtifactDownload(context.Background(), orgDomain.RoleAdmin, orgID, artID)
		if !errors.Is(err, domain.ErrBackupServiceUnavailable) {
			t.Fatalf("expected ErrBackupServiceUnavailable on missing restic dependencies, got: %v", err)
		}
	})

	t.Run("fails when repository_id is nil", func(t *testing.T) {
		nilRepoArt := &domain.BackupArtifact{
			ID:             artID,
			OrganizationID: orgID,
			Format:         domain.ArtifactFormatResticSnapshot,
			SnapshotID:     "snap123",
		}
		repoNil := &mockArtifactRepo{
			getArtifactByIDFunc: func(ctx context.Context, oID, aID uuid.UUID) (*domain.BackupArtifact, error) {
				return nilRepoArt, nil
			},
		}
		svc := NewArtifactService(repoNil, &mockStorageProvider{}, &mockAuditService{}, nil)
		svc.SetResticDependencies(runner, coordinator, vault, targetResolver)

		_, err := svc.OpenArtifactDownload(context.Background(), orgDomain.RoleAdmin, orgID, artID)
		if !errors.Is(err, domain.ErrBackupServiceUnavailable) {
			t.Fatalf("expected ErrBackupServiceUnavailable on nil repository_id, got: %v", err)
		}
	})
}

func TestArtifactService_DeleteArtifact_ResticPrevention(t *testing.T) {
	orgID := uuid.New()
	userID := uuid.New()
	artID := uuid.New()
	repoID := uuid.New()

	resticArt := &domain.BackupArtifact{
		ID:             artID,
		OrganizationID: orgID,
		Format:         domain.ArtifactFormatResticSnapshot,
		RepositoryID:   &repoID,
		SnapshotID:     "snap123",
	}

	backupRepo := &mockArtifactRepo{
		getArtifactByIDFunc: func(ctx context.Context, oID, aID uuid.UUID) (*domain.BackupArtifact, error) {
			return resticArt, nil
		},
	}

	svc := NewArtifactService(backupRepo, &mockStorageProvider{}, &mockAuditService{}, nil)
	err := svc.DeleteArtifact(context.Background(), orgDomain.RoleAdmin, orgID, userID, artID, "127.0.0.1", "agent")
	if err == nil {
		t.Fatalf("expected error when deleting restic snapshot artifact")
	}
	if !errors.Is(err, domain.ErrBackupServiceUnavailable) {
		t.Fatalf("expected ErrBackupServiceUnavailable, got: %v", err)
	}
}

type mockRepoWithResticGet struct {
	*mockArtifactRepo
	repoRecord          *domain.BackupRepository
	storageTargetRecord *domain.StorageTarget
}

func (m *mockRepoWithResticGet) GetRepositoryByID(ctx context.Context, orgID, repoID uuid.UUID) (*domain.BackupRepository, error) {
	if m.repoRecord != nil {
		return m.repoRecord, nil
	}
	return nil, domain.ErrRepositoryNotFound
}

func (m *mockRepoWithResticGet) GetStorageTargetByID(ctx context.Context, orgID, targetID uuid.UUID) (*domain.StorageTarget, error) {
	if m.storageTargetRecord != nil {
		return m.storageTargetRecord, nil
	}
	return nil, domain.ErrStorageTargetNotFound
}

func int64Ptr(v int64) *int64 { return &v }
