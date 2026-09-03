package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"backup-platform/internal/artifactcrypto"
	"backup-platform/internal/backup/domain"
	"backup-platform/internal/backup/repository"
	orgDomain "backup-platform/internal/organization/domain"
	"backup-platform/internal/storage"
	"backup-platform/pkg/uuid"
)

type mockVerificationRepo struct {
	mu          sync.Mutex
	runs        map[uuid.UUID]*domain.BackupRun
	artifacts   map[uuid.UUID][]*domain.BackupArtifact
	updatedArts map[uuid.UUID]domain.VerificationStatus
	updateErr   error
	getRunErr   error
	getArtsErr  error
}

func newMockVerificationRepo() *mockVerificationRepo {
	return &mockVerificationRepo{
		runs:        make(map[uuid.UUID]*domain.BackupRun),
		artifacts:   make(map[uuid.UUID][]*domain.BackupArtifact),
		updatedArts: make(map[uuid.UUID]domain.VerificationStatus),
	}
}

func (m *mockVerificationRepo) EnsureDefaultLocalStorageTarget(ctx context.Context, orgID uuid.UUID) (*domain.StorageTarget, error) {
	return nil, nil
}
func (m *mockVerificationRepo) GetStorageTargetByID(ctx context.Context, orgID, targetID uuid.UUID) (*domain.StorageTarget, error) {
	return nil, nil
}
func (m *mockVerificationRepo) GetPlanByID(ctx context.Context, orgID, planID uuid.UUID) (*domain.BackupPlan, error) {
	return nil, nil
}
func (m *mockVerificationRepo) CreateJob(ctx context.Context, job *domain.BackupJob) (*domain.BackupJob, error) {
	return nil, nil
}
func (m *mockVerificationRepo) GetJobByID(ctx context.Context, orgID, jobID uuid.UUID) (*domain.BackupJob, error) {
	return nil, nil
}
func (m *mockVerificationRepo) GetActiveManualJobForResource(ctx context.Context, orgID, resourceID uuid.UUID) (*domain.BackupJob, error) {
	return nil, nil
}
func (m *mockVerificationRepo) GetActiveJobConflictForResource(ctx context.Context, orgID, resourceID uuid.UUID) (*domain.BackupJob, error) {
	return nil, nil
}
func (m *mockVerificationRepo) FindPendingJobs(ctx context.Context, limit int, afterCreatedAt *time.Time, afterID *uuid.UUID) ([]*domain.BackupJob, error) {
	return nil, nil
}
func (m *mockVerificationRepo) TransactionalClaimJob(ctx context.Context, orgID, jobID uuid.UUID) (*domain.BackupRun, *domain.BackupJob, error) {
	return nil, nil, nil
}
func (m *mockVerificationRepo) GetRunByID(ctx context.Context, orgID, runID uuid.UUID) (*domain.BackupRun, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.getRunErr != nil {
		return nil, m.getRunErr
	}
	r, exists := m.runs[runID]
	if !exists || r.OrganizationID != orgID {
		return nil, domain.ErrRunNotFound
	}
	return r, nil
}
func (m *mockVerificationRepo) GetRunDetail(ctx context.Context, orgID, runID uuid.UUID) (*domain.BackupRunWithStats, error) {
	return nil, nil
}
func (m *mockVerificationRepo) ListRuns(ctx context.Context, orgID uuid.UUID, filter domain.RunFilter) ([]*domain.BackupRunWithStats, error) {
	return nil, nil
}
func (m *mockVerificationRepo) ListSuccessfulRunsForPlan(ctx context.Context, orgID, planID uuid.UUID) ([]*domain.BackupRun, error) {
	return nil, nil
}
func (m *mockVerificationRepo) GetLatestRunForJob(ctx context.Context, orgID, jobID uuid.UUID) (*domain.BackupRun, error) {
	return nil, nil
}
func (m *mockVerificationRepo) UpdateHeartbeat(ctx context.Context, orgID, runID uuid.UUID) error {
	return nil
}
func (m *mockVerificationRepo) FinalizeRunAndJob(ctx context.Context, orgID, runID, jobID uuid.UUID, runStatus domain.RunStatus, jobStatus domain.JobStatus, errMsg *string, logsSummary []byte) error {
	return nil
}
func (m *mockVerificationRepo) CreateArtifact(ctx context.Context, artifact *domain.BackupArtifact) (*domain.BackupArtifact, error) {
	return nil, nil
}
func (m *mockVerificationRepo) GetArtifactByID(ctx context.Context, orgID, artifactID uuid.UUID) (*domain.BackupArtifact, error) {
	return nil, nil
}
func (m *mockVerificationRepo) ListArtifacts(ctx context.Context, orgID uuid.UUID) ([]*domain.BackupArtifact, error) {
	return nil, nil
}
func (m *mockVerificationRepo) UpdateArtifactVerification(ctx context.Context, orgID, artifactID uuid.UUID, status domain.VerificationStatus, details *string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.updateErr != nil {
		return m.updateErr
	}
	m.updatedArts[artifactID] = status
	return nil
}
func (m *mockVerificationRepo) TombstoneArtifact(ctx context.Context, orgID, artifactID uuid.UUID) error {
	return nil
}
func (m *mockVerificationRepo) GetRunArtifacts(ctx context.Context, orgID, runID uuid.UUID) ([]*domain.BackupArtifact, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.getArtsErr != nil {
		return nil, m.getArtsErr
	}
	arts, exists := m.artifacts[runID]
	if !exists {
		return []*domain.BackupArtifact{}, nil
	}
	var res []*domain.BackupArtifact
	for _, a := range arts {
		if a.OrganizationID == orgID {
			res = append(res, a)
		}
	}
	return res, nil
}
func (m *mockVerificationRepo) RecoverInterruptedRuns(ctx context.Context) ([]domain.RecoveredRunInfo, error) {
	return nil, nil
}
func (m *mockVerificationRepo) ReapStaleRuns(ctx context.Context) ([]domain.RecoveredRunInfo, error) {
	return nil, nil
}
func (m *mockVerificationRepo) CreateStorageTarget(ctx context.Context, target *domain.StorageTarget) (*domain.StorageTarget, error) {
	return nil, nil
}
func (m *mockVerificationRepo) ListStorageTargets(ctx context.Context, orgID uuid.UUID) ([]*domain.StorageTarget, error) {
	return nil, nil
}
func (m *mockVerificationRepo) UpdateStorageTarget(ctx context.Context, target *domain.StorageTarget) (*domain.StorageTarget, error) {
	return nil, nil
}
func (m *mockVerificationRepo) DeleteStorageTarget(ctx context.Context, orgID, targetID uuid.UUID) error {
	return nil
}
func (m *mockVerificationRepo) CountArtifactsByStorageTarget(ctx context.Context, orgID, targetID uuid.UUID) (int64, error) {
	return 0, nil
}
func (m *mockVerificationRepo) CountPlansByStorageTarget(ctx context.Context, orgID, targetID uuid.UUID) (int64, error) {
	return 0, nil
}
func (m *mockVerificationRepo) CountActiveJobsByStorageTarget(ctx context.Context, orgID, targetID uuid.UUID) (int64, error) {
	return 0, nil
}
func (m *mockVerificationRepo) CreateRepository(ctx context.Context, repo *domain.BackupRepository) (*domain.BackupRepository, error) {
	return nil, nil
}
func (m *mockVerificationRepo) GetRepositoryByResourceID(ctx context.Context, orgID, resourceID uuid.UUID) (*domain.BackupRepository, error) {
	return nil, nil
}
func (m *mockVerificationRepo) GetRepositoryByID(ctx context.Context, orgID, repoID uuid.UUID) (*domain.BackupRepository, error) {
	return nil, nil
}

var _ repository.BackupRepository = (*mockVerificationRepo)(nil)

type mockVerifyStorageProvider struct {
	openedRefs []string
	openErr    error
}

func (m *mockVerifyStorageProvider) SaveArtifact(ctx context.Context, orgID, resID, runID, artifactID uuid.UUID, extension string, src io.Reader) (*storage.SaveResult, error) {
	return nil, nil
}
func (m *mockVerifyStorageProvider) OpenArtifact(ctx context.Context, storageReference string) (io.ReadCloser, error) {
	if m.openErr != nil {
		return nil, m.openErr
	}
	m.openedRefs = append(m.openedRefs, storageReference)
	return io.NopCloser(strings.NewReader("dummy-content")), nil
}
func (m *mockVerifyStorageProvider) DeleteArtifact(ctx context.Context, storageReference string) error {
	return nil
}
func (m *mockVerifyStorageProvider) EnsureStorageRoot(ctx context.Context) error {
	return nil
}

type mockVerifier struct {
	dbVerifyMsg   string
	dbVerifyErr   error
	fileVerifyMsg string
	fileVerifyErr error
	callsCount    int
}

func (m *mockVerifier) VerifyDatabaseArtifact(
	ctx context.Context,
	storageProvider storage.StorageProvider,
	storageReference string,
	expectedSizeBytes int64,
	expectedChecksumSHA256 string,
) (string, error) {
	m.callsCount++
	if m.dbVerifyErr != nil {
		return "", m.dbVerifyErr
	}
	return m.dbVerifyMsg, nil
}

func (m *mockVerifier) VerifyFilesArtifact(
	ctx context.Context,
	storageProvider storage.StorageProvider,
	storageReference string,
	expectedSizeBytes int64,
	expectedChecksumSHA256 string,
) (string, error) {
	m.callsCount++
	if m.fileVerifyErr != nil {
		return "", m.fileVerifyErr
	}
	return m.fileVerifyMsg, nil
}

func (m *mockVerifier) VerifyEncryptedDatabaseArtifact(
	ctx context.Context,
	storageProvider storage.StorageProvider,
	storageReference string,
	expectedPlaintextSize int64,
	expectedPlaintextChecksum string,
	storedSizeBytes int64,
	ciphertextSHA256 string,
	orgID, artifactID uuid.UUID,
) (string, error) {
	m.callsCount++
	if m.dbVerifyErr != nil {
		return "", m.dbVerifyErr
	}
	return m.dbVerifyMsg, nil
}

func (m *mockVerifier) VerifyEncryptedFilesArtifact(
	ctx context.Context,
	storageProvider storage.StorageProvider,
	storageReference string,
	expectedPlaintextSize int64,
	expectedPlaintextChecksum string,
	storedSizeBytes int64,
	ciphertextSHA256 string,
	orgID, artifactID uuid.UUID,
) (string, error) {
	m.callsCount++
	if m.fileVerifyErr != nil {
		return "", m.fileVerifyErr
	}
	return m.fileVerifyMsg, nil
}

func TestVerificationService_RBAC(t *testing.T) {
	orgID := uuid.New()
	runID := uuid.New()

	repo := newMockVerificationRepo()
	repo.runs[runID] = &domain.BackupRun{ID: runID, OrganizationID: orgID, Status: domain.RunStatusSuccess}
	artID := uuid.New()
	repo.artifacts[runID] = []*domain.BackupArtifact{
		{
			ID:               artID,
			OrganizationID:   orgID,
			RunID:            runID,
			ArtifactType:     domain.ArtifactTypeDatabaseDump,
			Format:           domain.ArtifactFormatSQLGzip,
			StorageReference: "local://test.sql.gz",
			SizeBytes:        1024,
			ChecksumHash:     "abc",
			IsDeleted:        false,
		},
	}

	store := &mockVerifyStorageProvider{}
	verifier := &mockVerifier{dbVerifyMsg: "checksum and gzip structural integrity verified"}
	svc := NewVerificationService(repo, store, verifier, slog.New(slog.NewTextHandler(io.Discard, nil)))

	t.Run("admin role is permitted", func(t *testing.T) {
		res, err := svc.VerifyRun(context.Background(), orgDomain.RoleAdmin, orgID, runID)
		if err != nil {
			t.Fatalf("expected admin to succeed, got: %v", err)
		}
		if res.VerificationStatus != domain.VerificationStatusVerified {
			t.Errorf("expected status verified, got: %s", res.VerificationStatus)
		}
	})

	t.Run("member role is permitted", func(t *testing.T) {
		res, err := svc.VerifyRun(context.Background(), orgDomain.RoleMember, orgID, runID)
		if err != nil {
			t.Fatalf("expected member to succeed, got: %v", err)
		}
		if res.VerificationStatus != domain.VerificationStatusVerified {
			t.Errorf("expected status verified, got: %s", res.VerificationStatus)
		}
	})

	t.Run("viewer role is forbidden", func(t *testing.T) {
		_, err := svc.VerifyRun(context.Background(), orgDomain.RoleViewer, orgID, runID)
		if !errors.Is(err, domain.ErrUnauthorizedRole) {
			t.Fatalf("expected ErrUnauthorizedRole for viewer, got: %v", err)
		}
	})

	t.Run("invalid or unknown role is forbidden", func(t *testing.T) {
		_, err := svc.VerifyRun(context.Background(), "guest", orgID, runID)
		if !errors.Is(err, domain.ErrUnauthorizedRole) {
			t.Fatalf("expected ErrUnauthorizedRole for guest, got: %v", err)
		}
	})
}

func TestVerificationService_TenantIsolation(t *testing.T) {
	orgA := uuid.New()
	orgB := uuid.New()
	runID := uuid.New()

	repo := newMockVerificationRepo()
	repo.runs[runID] = &domain.BackupRun{ID: runID, OrganizationID: orgA, Status: domain.RunStatusSuccess}
	artID := uuid.New()
	repo.artifacts[runID] = []*domain.BackupArtifact{
		{
			ID:               artID,
			OrganizationID:   orgA,
			RunID:            runID,
			ArtifactType:     domain.ArtifactTypeDatabaseDump,
			Format:           domain.ArtifactFormatSQLGzip,
			StorageReference: "local://art.sql.gz",
			SizeBytes:        1024,
			ChecksumHash:     "abc",
			IsDeleted:        false,
		},
	}

	store := &mockVerifyStorageProvider{}
	verifier := &mockVerifier{dbVerifyMsg: "ok"}
	svc := NewVerificationService(repo, store, verifier, slog.New(slog.NewTextHandler(io.Discard, nil)))

	t.Run("cross-organization verification returns safe not-found", func(t *testing.T) {
		_, err := svc.VerifyRun(context.Background(), orgDomain.RoleAdmin, orgB, runID)
		if !errors.Is(err, domain.ErrRunNotFound) {
			t.Fatalf("expected ErrRunNotFound for cross-org lookup, got: %v", err)
		}
	})
}

func TestVerificationService_ArtifactFiltering(t *testing.T) {
	orgID := uuid.New()
	runID := uuid.New()

	t.Run("deleted artifacts are never opened or verified", func(t *testing.T) {
		repo := newMockVerificationRepo()
		repo.runs[runID] = &domain.BackupRun{ID: runID, OrganizationID: orgID, Status: domain.RunStatusSuccess}

		artActive := &domain.BackupArtifact{
			ID:               uuid.New(),
			OrganizationID:   orgID,
			RunID:            runID,
			ArtifactType:     domain.ArtifactTypeDatabaseDump,
			Format:           domain.ArtifactFormatSQLGzip,
			StorageReference: "local://active.sql.gz",
			SizeBytes:        1024,
			ChecksumHash:     "abc",
			IsDeleted:        false,
		}
		artDeleted := &domain.BackupArtifact{
			ID:               uuid.New(),
			OrganizationID:   orgID,
			RunID:            runID,
			ArtifactType:     domain.ArtifactTypeDatabaseDump,
			Format:           domain.ArtifactFormatSQLGzip,
			StorageReference: "local://deleted.sql.gz",
			SizeBytes:        1024,
			ChecksumHash:     "def",
			IsDeleted:        true,
		}
		repo.artifacts[runID] = []*domain.BackupArtifact{artActive, artDeleted}

		store := &mockVerifyStorageProvider{}
		verifier := &mockVerifier{dbVerifyMsg: "verified"}
		svc := NewVerificationService(repo, store, verifier, slog.New(slog.NewTextHandler(io.Discard, nil)))

		res, err := svc.VerifyRun(context.Background(), orgDomain.RoleAdmin, orgID, runID)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if res.VerificationStatus != domain.VerificationStatusVerified {
			t.Errorf("expected verified, got: %s", res.VerificationStatus)
		}
		if verifier.callsCount != 1 {
			t.Errorf("expected exactly 1 verification call, got: %d", verifier.callsCount)
		}
		if _, deletedUpdated := repo.updatedArts[artDeleted.ID]; deletedUpdated {
			t.Errorf("deleted artifact must NOT be updated or verified")
		}
	})

	t.Run("all artifacts deleted returns ErrNoVerifiableArtifacts", func(t *testing.T) {
		repo := newMockVerificationRepo()
		repo.runs[runID] = &domain.BackupRun{ID: runID, OrganizationID: orgID, Status: domain.RunStatusSuccess}
		artDeleted := &domain.BackupArtifact{
			ID:             uuid.New(),
			OrganizationID: orgID,
			RunID:          runID,
			IsDeleted:      true,
		}
		repo.artifacts[runID] = []*domain.BackupArtifact{artDeleted}

		store := &mockVerifyStorageProvider{}
		verifier := &mockVerifier{dbVerifyMsg: "verified"}
		svc := NewVerificationService(repo, store, verifier, slog.New(slog.NewTextHandler(io.Discard, nil)))

		_, err := svc.VerifyRun(context.Background(), orgDomain.RoleAdmin, orgID, runID)
		if !errors.Is(err, domain.ErrNoVerifiableArtifacts) {
			t.Fatalf("expected ErrNoVerifiableArtifacts, got: %v", err)
		}
	})

	t.Run("zero artifacts returns ErrNoVerifiableArtifacts", func(t *testing.T) {
		repo := newMockVerificationRepo()
		repo.runs[runID] = &domain.BackupRun{ID: runID, OrganizationID: orgID, Status: domain.RunStatusSuccess}
		repo.artifacts[runID] = []*domain.BackupArtifact{}

		store := &mockVerifyStorageProvider{}
		verifier := &mockVerifier{dbVerifyMsg: "verified"}
		svc := NewVerificationService(repo, store, verifier, slog.New(slog.NewTextHandler(io.Discard, nil)))

		_, err := svc.VerifyRun(context.Background(), orgDomain.RoleAdmin, orgID, runID)
		if !errors.Is(err, domain.ErrNoVerifiableArtifacts) {
			t.Fatalf("expected ErrNoVerifiableArtifacts, got: %v", err)
		}
	})
}

func TestVerificationService_IntegrityChecks(t *testing.T) {
	orgID := uuid.New()
	runID := uuid.New()

	t.Run("valid database dump verified with exact 4 frozen fields", func(t *testing.T) {
		repo := newMockVerificationRepo()
		repo.runs[runID] = &domain.BackupRun{ID: runID, OrganizationID: orgID, Status: domain.RunStatusSuccess}
		artID := uuid.New()
		repo.artifacts[runID] = []*domain.BackupArtifact{
			{
				ID:               artID,
				OrganizationID:   orgID,
				RunID:            runID,
				ArtifactType:     domain.ArtifactTypeDatabaseDump,
				Format:           domain.ArtifactFormatSQLGzip,
				StorageReference: "local://test.sql.gz",
				SizeBytes:        1024,
				ChecksumHash:     "abc",
			},
		}

		store := &mockVerifyStorageProvider{}
		verifier := &mockVerifier{dbVerifyMsg: "checksum and gzip structural integrity verified"}
		svc := NewVerificationService(repo, store, verifier, slog.New(slog.NewTextHandler(io.Discard, nil)))

		res, err := svc.VerifyRun(context.Background(), orgDomain.RoleAdmin, orgID, runID)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if res.VerificationStatus != domain.VerificationStatusVerified {
			t.Errorf("expected verified, got %s", res.VerificationStatus)
		}
		if !res.Details.ChecksumMatched || res.Details.ArchiveIntegrity != "passed" || !res.Details.CompressionValid || res.Details.ExtractedSampleCheck != "valid_sql_dump" {
			t.Errorf("unexpected details: %+v", res.Details)
		}
		if repo.updatedArts[artID] != domain.VerificationStatusVerified {
			t.Errorf("expected artifact to be updated to verified in repo")
		}

		// Verify serialized JSON contains exact 4 keys
		bytes, err := json.Marshal(res.Details)
		if err != nil {
			t.Fatalf("failed marshaling details: %v", err)
		}
		var rawMap map[string]any
		if err := json.Unmarshal(bytes, &rawMap); err != nil {
			t.Fatalf("failed unmarshaling details: %v", err)
		}
		if len(rawMap) != 4 {
			t.Errorf("expected exactly 4 keys in details, got %d: %+v", len(rawMap), rawMap)
		}
	})

	t.Run("checksum mismatch marks failed and reports false for unproven checks", func(t *testing.T) {
		repo := newMockVerificationRepo()
		repo.runs[runID] = &domain.BackupRun{ID: runID, OrganizationID: orgID, Status: domain.RunStatusSuccess}
		artID := uuid.New()
		repo.artifacts[runID] = []*domain.BackupArtifact{
			{
				ID:               artID,
				OrganizationID:   orgID,
				RunID:            runID,
				ArtifactType:     domain.ArtifactTypeDatabaseDump,
				Format:           domain.ArtifactFormatSQLGzip,
				StorageReference: "/var/backups/internal/secret/path/test.sql.gz",
				SizeBytes:        1024,
				ChecksumHash:     "expected-sha",
			},
		}

		store := &mockVerifyStorageProvider{}
		verifier := &mockVerifier{dbVerifyErr: errors.New("checksum mismatch: expected expected-sha, got actual-sha")}
		svc := NewVerificationService(repo, store, verifier, slog.New(slog.NewTextHandler(io.Discard, nil)))

		res, err := svc.VerifyRun(context.Background(), orgDomain.RoleAdmin, orgID, runID)
		if err != nil {
			t.Fatalf("unexpected service error: %v", err)
		}

		if res.VerificationStatus != domain.VerificationStatusFailed {
			t.Errorf("expected status failed, got: %s", res.VerificationStatus)
		}
		if res.Details.ChecksumMatched != false || res.Details.ArchiveIntegrity != "failed" || res.Details.CompressionValid != false || res.Details.ExtractedSampleCheck != "failed" {
			t.Errorf("expected conservative failure details, got: %+v", res.Details)
		}
		if repo.updatedArts[artID] != domain.VerificationStatusFailed {
			t.Errorf("expected artifact to be updated to failed in repo")
		}

		// Verify serialized JSON contains exact 4 keys with NO extra error field
		bytes, err := json.Marshal(res.Details)
		if err != nil {
			t.Fatalf("failed marshaling details: %v", err)
		}
		var rawMap map[string]any
		if err := json.Unmarshal(bytes, &rawMap); err != nil {
			t.Fatalf("failed unmarshaling details: %v", err)
		}
		if len(rawMap) != 4 {
			t.Errorf("expected exactly 4 keys in details, got %d: %+v", len(rawMap), rawMap)
		}
		if _, exists := rawMap["error"]; exists {
			t.Errorf("error key must NOT exist in public details")
		}
	})

	t.Run("corrupt gzip database dump reports conservative failure details", func(t *testing.T) {
		repo := newMockVerificationRepo()
		repo.runs[runID] = &domain.BackupRun{ID: runID, OrganizationID: orgID, Status: domain.RunStatusSuccess}
		artID := uuid.New()
		repo.artifacts[runID] = []*domain.BackupArtifact{
			{
				ID:               artID,
				OrganizationID:   orgID,
				RunID:            runID,
				ArtifactType:     domain.ArtifactTypeDatabaseDump,
				Format:           domain.ArtifactFormatSQLGzip,
				StorageReference: "local://corrupt.sql.gz",
				SizeBytes:        1024,
				ChecksumHash:     "abc",
			},
		}

		store := &mockVerifyStorageProvider{}
		verifier := &mockVerifier{dbVerifyErr: errors.New("artifact is not a valid gzip stream: unexpected EOF")}
		svc := NewVerificationService(repo, store, verifier, slog.New(slog.NewTextHandler(io.Discard, nil)))

		res, err := svc.VerifyRun(context.Background(), orgDomain.RoleAdmin, orgID, runID)
		if err != nil {
			t.Fatalf("unexpected service error: %v", err)
		}

		if res.VerificationStatus != domain.VerificationStatusFailed {
			t.Errorf("expected failed, got: %s", res.VerificationStatus)
		}
		if res.Details.ChecksumMatched != false {
			t.Errorf("unproven checksum check must not be reported as true on early gzip failure")
		}
	})

	t.Run("valid website tar.gz verified successfully with not_applicable sample check", func(t *testing.T) {
		repo := newMockVerificationRepo()
		repo.runs[runID] = &domain.BackupRun{ID: runID, OrganizationID: orgID, Status: domain.RunStatusSuccess}
		artID := uuid.New()
		repo.artifacts[runID] = []*domain.BackupArtifact{
			{
				ID:               artID,
				OrganizationID:   orgID,
				RunID:            runID,
				ArtifactType:     domain.ArtifactTypeFilesArchive,
				Format:           domain.ArtifactFormatTarGzip,
				StorageReference: "local://site.tar.gz",
				SizeBytes:        2048,
				ChecksumHash:     "def",
			},
		}

		store := &mockVerifyStorageProvider{}
		verifier := &mockVerifier{fileVerifyMsg: "checksum and tar archive structural integrity verified"}
		svc := NewVerificationService(repo, store, verifier, slog.New(slog.NewTextHandler(io.Discard, nil)))

		res, err := svc.VerifyRun(context.Background(), orgDomain.RoleAdmin, orgID, runID)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if res.VerificationStatus != domain.VerificationStatusVerified {
			t.Errorf("expected verified, got %s", res.VerificationStatus)
		}
		if res.Details.ExtractedSampleCheck != "not_applicable" {
			t.Errorf("expected not_applicable for website files, got %s", res.Details.ExtractedSampleCheck)
		}

		bytes, err := json.Marshal(res.Details)
		if err != nil {
			t.Fatalf("failed marshaling details: %v", err)
		}
		var rawMap map[string]any
		if err := json.Unmarshal(bytes, &rawMap); err != nil {
			t.Fatalf("failed unmarshaling details: %v", err)
		}
		if _, exists := rawMap["tar_archive_valid"]; exists {
			t.Errorf("tar_archive_valid must NOT exist in public details")
		}
		if len(rawMap) != 4 {
			t.Errorf("expected exactly 4 keys in details, got %d", len(rawMap))
		}
	})

	t.Run("corrupt tar archive marks failed without false positive checksum", func(t *testing.T) {
		repo := newMockVerificationRepo()
		repo.runs[runID] = &domain.BackupRun{ID: runID, OrganizationID: orgID, Status: domain.RunStatusSuccess}
		artID := uuid.New()
		repo.artifacts[runID] = []*domain.BackupArtifact{
			{
				ID:               artID,
				OrganizationID:   orgID,
				RunID:            runID,
				ArtifactType:     domain.ArtifactTypeFilesArchive,
				Format:           domain.ArtifactFormatTarGzip,
				StorageReference: "local://bad.tar.gz",
				SizeBytes:        2048,
				ChecksumHash:     "def",
			},
		}

		store := &mockVerifyStorageProvider{}
		verifier := &mockVerifier{fileVerifyErr: errors.New("tar structural integrity check failed: unexpected EOF")}
		svc := NewVerificationService(repo, store, verifier, slog.New(slog.NewTextHandler(io.Discard, nil)))

		res, err := svc.VerifyRun(context.Background(), orgDomain.RoleAdmin, orgID, runID)
		if err != nil {
			t.Fatalf("unexpected service error: %v", err)
		}

		if res.VerificationStatus != domain.VerificationStatusFailed {
			t.Errorf("expected failed, got: %s", res.VerificationStatus)
		}
		if res.Details.ChecksumMatched != false {
			t.Errorf("checksum_matched must be false on failed tar verification")
		}
	})
}

func TestVerificationService_MultiArtifact(t *testing.T) {
	orgID := uuid.New()
	runID := uuid.New()

	t.Run("all artifacts pass produces overall verified status and aggregate 4-field details", func(t *testing.T) {
		repo := newMockVerificationRepo()
		repo.runs[runID] = &domain.BackupRun{ID: runID, OrganizationID: orgID, Status: domain.RunStatusSuccess}
		art1 := &domain.BackupArtifact{
			ID:               uuid.New(),
			OrganizationID:   orgID,
			RunID:            runID,
			ArtifactType:     domain.ArtifactTypeDatabaseDump,
			Format:           domain.ArtifactFormatSQLGzip,
			StorageReference: "local://db1.sql.gz",
			SizeBytes:        1000,
			ChecksumHash:     "hash1",
		}
		art2 := &domain.BackupArtifact{
			ID:               uuid.New(),
			OrganizationID:   orgID,
			RunID:            runID,
			ArtifactType:     domain.ArtifactTypeDatabaseDump,
			Format:           domain.ArtifactFormatSQLGzip,
			StorageReference: "local://db2.sql.gz",
			SizeBytes:        2000,
			ChecksumHash:     "hash2",
		}
		repo.artifacts[runID] = []*domain.BackupArtifact{art1, art2}

		store := &mockVerifyStorageProvider{}
		verifier := &mockVerifier{dbVerifyMsg: "verified"}
		svc := NewVerificationService(repo, store, verifier, slog.New(slog.NewTextHandler(io.Discard, nil)))

		res, err := svc.VerifyRun(context.Background(), orgDomain.RoleAdmin, orgID, runID)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if res.VerificationStatus != domain.VerificationStatusVerified {
			t.Errorf("expected overall verified, got %s", res.VerificationStatus)
		}
		if !res.Details.ChecksumMatched || res.Details.ArchiveIntegrity != "passed" || !res.Details.CompressionValid || res.Details.ExtractedSampleCheck != "valid_sql_dump" {
			t.Errorf("unexpected multi-artifact details: %+v", res.Details)
		}

		bytes, err := json.Marshal(res.Details)
		if err != nil {
			t.Fatalf("failed marshaling: %v", err)
		}
		var rawMap map[string]any
		if err := json.Unmarshal(bytes, &rawMap); err != nil {
			t.Fatalf("failed unmarshaling: %v", err)
		}
		for _, forbiddenKey := range []string{"artifacts", "artifacts_total", "artifacts_verified", "artifact_id"} {
			if _, exists := rawMap[forbiddenKey]; exists {
				t.Errorf("forbidden key %q found in multi-artifact details", forbiddenKey)
			}
		}
	})

	t.Run("one artifact fails marks overall failed and continues processing remaining artifacts", func(t *testing.T) {
		repo := newMockVerificationRepo()
		repo.runs[runID] = &domain.BackupRun{ID: runID, OrganizationID: orgID, Status: domain.RunStatusSuccess}
		art1 := &domain.BackupArtifact{
			ID:               uuid.New(),
			OrganizationID:   orgID,
			RunID:            runID,
			ArtifactType:     domain.ArtifactTypeDatabaseDump,
			Format:           domain.ArtifactFormatSQLGzip,
			StorageReference: "local://db1.sql.gz",
			SizeBytes:        1000,
			ChecksumHash:     "hash1",
		}
		art2 := &domain.BackupArtifact{
			ID:               uuid.New(),
			OrganizationID:   orgID,
			RunID:            runID,
			ArtifactType:     domain.ArtifactTypeFilesArchive,
			Format:           domain.ArtifactFormatTarGzip,
			StorageReference: "local://files2.tar.gz",
			SizeBytes:        2000,
			ChecksumHash:     "hash2",
		}
		repo.artifacts[runID] = []*domain.BackupArtifact{art1, art2}

		store := &mockVerifyStorageProvider{}
		verifier := &mockVerifier{
			dbVerifyErr:   errors.New("checksum mismatch: expected hash1, got other"),
			fileVerifyMsg: "tar archive verified",
		}
		svc := NewVerificationService(repo, store, verifier, slog.New(slog.NewTextHandler(io.Discard, nil)))

		res, err := svc.VerifyRun(context.Background(), orgDomain.RoleAdmin, orgID, runID)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if res.VerificationStatus != domain.VerificationStatusFailed {
			t.Errorf("expected overall failed, got %s", res.VerificationStatus)
		}
		if verifier.callsCount != 2 {
			t.Errorf("expected both artifacts to be verified, got %d calls", verifier.callsCount)
		}
		if repo.updatedArts[art1.ID] != domain.VerificationStatusFailed {
			t.Errorf("art1 must be marked failed in DB")
		}
		if repo.updatedArts[art2.ID] != domain.VerificationStatusVerified {
			t.Errorf("art2 must be marked verified in DB")
		}
		if res.Details.ChecksumMatched != false || res.Details.ArchiveIntegrity != "failed" || res.Details.CompressionValid != false || res.Details.ExtractedSampleCheck != "failed" {
			t.Errorf("expected conservative failure details on multi-artifact corruption: %+v", res.Details)
		}
	})
}

func TestVerificationService_InfrastructureErrors(t *testing.T) {
	orgID := uuid.New()
	runID := uuid.New()

	t.Run("storage open error returns safe service error without corruption tombstone", func(t *testing.T) {
		repo := newMockVerificationRepo()
		repo.runs[runID] = &domain.BackupRun{ID: runID, OrganizationID: orgID, Status: domain.RunStatusSuccess}
		artID := uuid.New()
		repo.artifacts[runID] = []*domain.BackupArtifact{
			{
				ID:               artID,
				OrganizationID:   orgID,
				RunID:            runID,
				ArtifactType:     domain.ArtifactTypeDatabaseDump,
				Format:           domain.ArtifactFormatSQLGzip,
				StorageReference: "local://test.sql.gz",
				SizeBytes:        1024,
				ChecksumHash:     "abc",
			},
		}

		store := &mockVerifyStorageProvider{}
		verifier := &mockVerifier{dbVerifyErr: errors.New("failed opening artifact for verification: disk I/O failure")}
		svc := NewVerificationService(repo, store, verifier, slog.New(slog.NewTextHandler(io.Discard, nil)))

		_, err := svc.VerifyRun(context.Background(), orgDomain.RoleAdmin, orgID, runID)
		if !errors.Is(err, domain.ErrBackupServiceUnavailable) {
			t.Fatalf("expected ErrBackupServiceUnavailable on storage open failure, got: %v", err)
		}
		if _, updated := repo.updatedArts[artID]; updated {
			t.Errorf("artifact must NOT be marked corrupt on storage infrastructure failure")
		}
	})

	t.Run("repository update error returns safe service error", func(t *testing.T) {
		repo := newMockVerificationRepo()
		repo.runs[runID] = &domain.BackupRun{ID: runID, OrganizationID: orgID, Status: domain.RunStatusSuccess}
		artID := uuid.New()
		repo.artifacts[runID] = []*domain.BackupArtifact{
			{
				ID:               artID,
				OrganizationID:   orgID,
				RunID:            runID,
				ArtifactType:     domain.ArtifactTypeDatabaseDump,
				Format:           domain.ArtifactFormatSQLGzip,
				StorageReference: "local://test.sql.gz",
				SizeBytes:        1024,
				ChecksumHash:     "abc",
			},
		}
		repo.updateErr = errors.New("db connection lost")

		store := &mockVerifyStorageProvider{}
		verifier := &mockVerifier{dbVerifyMsg: "verified"}
		svc := NewVerificationService(repo, store, verifier, slog.New(slog.NewTextHandler(io.Discard, nil)))

		_, err := svc.VerifyRun(context.Background(), orgDomain.RoleAdmin, orgID, runID)
		if !errors.Is(err, domain.ErrBackupServiceUnavailable) {
			t.Fatalf("expected ErrBackupServiceUnavailable on DB update error, got: %v", err)
		}
	})

	t.Run("context cancellation returns context error immediately", func(t *testing.T) {
		repo := newMockVerificationRepo()
		repo.runs[runID] = &domain.BackupRun{ID: runID, OrganizationID: orgID, Status: domain.RunStatusSuccess}
		repo.artifacts[runID] = []*domain.BackupArtifact{
			{
				ID:               uuid.New(),
				OrganizationID:   orgID,
				RunID:            runID,
				ArtifactType:     domain.ArtifactTypeDatabaseDump,
				Format:           domain.ArtifactFormatSQLGzip,
				StorageReference: "local://test.sql.gz",
				SizeBytes:        1024,
				ChecksumHash:     "abc",
			},
		}

		store := &mockVerifyStorageProvider{}
		verifier := &mockVerifier{dbVerifyErr: context.Canceled}
		svc := NewVerificationService(repo, store, verifier, slog.New(slog.NewTextHandler(io.Discard, nil)))

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := svc.VerifyRun(ctx, orgDomain.RoleAdmin, orgID, runID)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got: %v", err)
		}
	})

	t.Run("verifier returns DeadlineExceeded with active context is returned deterministically", func(t *testing.T) {
		repo := newMockVerificationRepo()
		repo.runs[runID] = &domain.BackupRun{ID: runID, OrganizationID: orgID, Status: domain.RunStatusSuccess}
		repo.artifacts[runID] = []*domain.BackupArtifact{
			{
				ID:               uuid.New(),
				OrganizationID:   orgID,
				RunID:            runID,
				ArtifactType:     domain.ArtifactTypeDatabaseDump,
				Format:           domain.ArtifactFormatSQLGzip,
				StorageReference: "local://test.sql.gz",
				SizeBytes:        1024,
				ChecksumHash:     "abc",
			},
		}

		store := &mockVerifyStorageProvider{}
		verifier := &mockVerifier{dbVerifyErr: context.DeadlineExceeded}
		svc := NewVerificationService(repo, store, verifier, slog.New(slog.NewTextHandler(io.Discard, nil)))

		res, err := svc.VerifyRun(context.Background(), orgDomain.RoleAdmin, orgID, runID)
		if res != nil {
			t.Errorf("expected result to be nil on context error, got: %+v", res)
		}
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("expected DeadlineExceeded error, got: %v", err)
		}
	})

	t.Run("missing dependencies fail closed", func(t *testing.T) {
		svc := NewVerificationService(nil, nil, nil, nil)
		_, err := svc.VerifyRun(context.Background(), orgDomain.RoleAdmin, orgID, runID)
		if !errors.Is(err, domain.ErrBackupServiceUnavailable) {
			t.Fatalf("expected ErrBackupServiceUnavailable on missing dependencies, got: %v", err)
		}
	})

	t.Run("storage provider resolution failure returns ErrBackupServiceUnavailable without updating artifact to failed", func(t *testing.T) {
		repo := newMockVerificationRepo()
		repo.runs[runID] = &domain.BackupRun{ID: runID, OrganizationID: orgID, Status: domain.RunStatusSuccess}
		artID := uuid.New()
		targetID := uuid.New()
		repo.artifacts[runID] = []*domain.BackupArtifact{
			{
				ID:               artID,
				OrganizationID:   orgID,
				RunID:            runID,
				StorageTargetID:  targetID,
				ArtifactType:     domain.ArtifactTypeDatabaseDump,
				Format:           domain.ArtifactFormatSQLGzip,
				StorageReference: "organizations/" + orgID.String() + "/resources/" + uuid.New().String() + "/artifacts/" + artID.String() + ".sql.gz",
				SizeBytes:        1024,
				ChecksumHash:     "abc",
			},
		}

		verifier := &mockVerifier{}
		svc := NewVerificationService(repo, nil, verifier, slog.New(slog.NewTextHandler(io.Discard, nil)))
		failingResolver := &mockFailingVerifyResolver{err: errors.New("s3 connection timeout")}
		svc.SetStorageResolver(failingResolver)

		res, err := svc.VerifyRun(context.Background(), orgDomain.RoleAdmin, orgID, runID)
		if res != nil {
			t.Errorf("expected result to be nil on provider resolution error, got: %+v", res)
		}
		if !errors.Is(err, domain.ErrBackupServiceUnavailable) {
			t.Fatalf("expected ErrBackupServiceUnavailable, got: %v", err)
		}

		// Verify that artifact verification status was NOT marked failed in the database!
		if status, updated := repo.updatedArts[artID]; updated {
			t.Fatalf("artifact must NOT have verification status updated when provider resolution fails, got: %v", status)
		}
	})

	t.Run("unknown key version returns infrastructure error and preserves prior verification status", func(t *testing.T) {
		repo := newMockVerificationRepo()
		repo.runs[runID] = &domain.BackupRun{ID: runID, OrganizationID: orgID, Status: domain.RunStatusSuccess}
		artID := uuid.New()
		targetID := uuid.New()
		storedSize := int64(2048)
		engineMeta := []byte(`{"ciphertext_sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`)
		repo.artifacts[runID] = []*domain.BackupArtifact{
			{
				ID:               artID,
				OrganizationID:   orgID,
				RunID:            runID,
				StorageTargetID:  targetID,
				ArtifactType:     domain.ArtifactTypeDatabaseDump,
				Format:           domain.ArtifactFormatSQLGzip,
				StorageReference: "organizations/" + orgID.String() + "/resources/" + uuid.New().String() + "/artifacts/" + artID.String() + ".sql.gz",
				SizeBytes:        1024,
				ChecksumHash:     "abc",
				StoredSizeBytes:  &storedSize,
				EngineMetadata:   engineMeta,
			},
		}

		verifier := &mockVerifier{
			dbVerifyErr: artifactcrypto.ErrUnknownKeyVersion,
		}
		storeProvider := &mockVerifyStorageProvider{}
		svc := NewVerificationService(repo, storeProvider, verifier, slog.New(slog.NewTextHandler(io.Discard, nil)))

		res, err := svc.VerifyRun(context.Background(), orgDomain.RoleAdmin, orgID, runID)
		if res != nil {
			t.Errorf("expected result to be nil on key infrastructure error, got: %+v", res)
		}
		if !errors.Is(err, domain.ErrBackupServiceUnavailable) {
			t.Fatalf("expected ErrBackupServiceUnavailable, got: %v", err)
		}

		// Verify that UpdateArtifactVerification was NOT called! Status remains preserved.
		if status, updated := repo.updatedArts[artID]; updated {
			t.Fatalf("CRITICAL: UpdateArtifactVerification must NOT be called on ErrUnknownKeyVersion! got updated status: %v", status)
		}
	})

	t.Run("corrupted DATA or FINAL tag marks artifact as verification failed", func(t *testing.T) {
		repo := newMockVerificationRepo()
		repo.runs[runID] = &domain.BackupRun{ID: runID, OrganizationID: orgID, Status: domain.RunStatusSuccess}
		artID := uuid.New()
		targetID := uuid.New()
		storedSize := int64(2048)
		engineMeta := []byte(`{"ciphertext_sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`)
		repo.artifacts[runID] = []*domain.BackupArtifact{
			{
				ID:               artID,
				OrganizationID:   orgID,
				RunID:            runID,
				StorageTargetID:  targetID,
				ArtifactType:     domain.ArtifactTypeDatabaseDump,
				Format:           domain.ArtifactFormatSQLGzip,
				StorageReference: "organizations/" + orgID.String() + "/resources/" + uuid.New().String() + "/artifacts/" + artID.String() + ".sql.gz",
				SizeBytes:        1024,
				ChecksumHash:     "abc",
				StoredSizeBytes:  &storedSize,
				EngineMetadata:   engineMeta,
			},
		}

		verifier := &mockVerifier{
			dbVerifyErr: artifactcrypto.ErrAuthFailed,
		}
		storeProvider := &mockVerifyStorageProvider{}
		svc := NewVerificationService(repo, storeProvider, verifier, slog.New(slog.NewTextHandler(io.Discard, nil)))

		res, err := svc.VerifyRun(context.Background(), orgDomain.RoleAdmin, orgID, runID)
		if err != nil {
			t.Fatalf("expected nil error (verification report returned), got: %v", err)
		}
		if res.VerificationStatus != domain.VerificationStatusFailed {
			t.Errorf("expected overallStatus Failed on integrity failure, got: %v", res.VerificationStatus)
		}

		// Verify that UpdateArtifactVerification WAS called with Failed!
		if status, updated := repo.updatedArts[artID]; !updated || status != domain.VerificationStatusFailed {
			t.Fatalf("expected artifact verification status to be updated to Failed, updated=%v, status=%v", updated, status)
		}
	})
}

type mockFailingVerifyResolver struct {
	err error
}

func (m *mockFailingVerifyResolver) Resolve(ctx context.Context, orgID, targetID uuid.UUID) (storage.StorageProvider, error) {
	return nil, m.err
}
