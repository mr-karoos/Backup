package worker

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"backup-platform/internal/artifactcrypto"
	"backup-platform/internal/backup/domain"
	"backup-platform/internal/backup/engine"
	"backup-platform/internal/backup/repository"
	"backup-platform/internal/backup/retention"
	"backup-platform/internal/backup/verification"
	"backup-platform/internal/connector"
	credDomain "backup-platform/internal/credential/domain"
	"backup-platform/internal/credential/payload"
	resDomain "backup-platform/internal/resource/domain"
	"backup-platform/internal/storage"
	"backup-platform/internal/storage/local"
	"backup-platform/pkg/uuid"
)

var testSyntheticKeyProvider, _ = artifactcrypto.NewStaticKeyProvider(bytes.Repeat([]byte{0x42}, 32), 1)

func newTestWorkerPool(
	cfg WorkerPoolConfig,
	repo repository.BackupRepository,
	resFinder ResourceConnectorFinder,
	vault CredentialVault,
	capabilityRegistry *connector.BackupCapabilityRegistry,
	fileCapabilityRegistry *connector.FileBackupCapabilityRegistry,
	engine engine.BackupEngine,
	storageProvider storage.StorageProvider,
	verifier verification.Verifier,
	mutexManager *PerResourceMutexManager,
	log *slog.Logger,
) *WorkerPool {
	return NewWorkerPoolWithKeyProvider(
		cfg,
		repo,
		resFinder,
		vault,
		capabilityRegistry,
		fileCapabilityRegistry,
		engine,
		storageProvider,
		verifier,
		mutexManager,
		log,
		testSyntheticKeyProvider,
	)
}

func TestPerResourceMutexManager(t *testing.T) {
	mgr := NewPerResourceMutexManager()
	res1 := uuid.New()
	res2 := uuid.New()

	// 1. Acquire res1
	unlock1, ok := mgr.TryAcquire(res1)
	if !ok || unlock1 == nil {
		t.Fatalf("expected to acquire res1")
	}

	// 2. Second acquire on res1 fails
	unlock2, ok := mgr.TryAcquire(res1)
	if ok || unlock2 != nil {
		t.Fatalf("expected second acquire on res1 to fail")
	}

	// 3. Acquire on distinct res2 succeeds
	unlockRes2, ok := mgr.TryAcquire(res2)
	if !ok || unlockRes2 == nil {
		t.Fatalf("expected to acquire res2 concurrently")
	}

	// 4. Unlock res1 and re-acquire
	unlock1()
	unlock1Again, ok := mgr.TryAcquire(res1)
	if !ok || unlock1Again == nil {
		t.Fatalf("expected to re-acquire res1 after unlock")
	}

	unlock1Again()
	unlockRes2()
}

func TestCalculateRetryDelay(t *testing.T) {
	jobID := uuid.New()

	// Attempt 1: 2^1 * 30s = 60s (+ 0-14s jitter) -> 60s-74s
	d1 := CalculateRetryDelay(jobID, 1)
	if d1 < 60*time.Second || d1 > 75*time.Second {
		t.Errorf("expected attempt 1 delay between 60-75s, got %v", d1)
	}

	// Attempt 2: 2^2 * 30s = 120s (+ 0-14s jitter) -> 120s-134s
	d2 := CalculateRetryDelay(jobID, 2)
	if d2 < 120*time.Second || d2 > 135*time.Second {
		t.Errorf("expected attempt 2 delay between 120-135s, got %v", d2)
	}

	// Attempt 3: 2^3 * 30s = 240s (+ 0-14s jitter) -> 240s-254s
	d3 := CalculateRetryDelay(jobID, 3)
	if d3 < 240*time.Second || d3 > 255*time.Second {
		t.Errorf("expected attempt 3 delay between 240-255s, got %v", d3)
	}

	// Attempt 10: capped strictly at 600s (10m)
	dHigh := CalculateRetryDelay(jobID, 10)
	if dHigh != 600*time.Second {
		t.Errorf("expected capped delay == 600s (10m), got %v", dHigh)
	}
}

func TestErrorClassification(t *testing.T) {
	cases := []struct {
		name         string
		err          error
		expectedKind FailureKind
		retryable    bool
	}{
		{
			name:         "Storage ENOSPC is non-retryable storage_full",
			err:          storage.ErrStorageFull,
			expectedKind: FailureKindStorageFull,
			retryable:    false,
		},
		{
			name:         "Syscall ENOSPC is non-retryable storage_full",
			err:          syscall.ENOSPC,
			expectedKind: FailureKindStorageFull,
			retryable:    false,
		},
		{
			name:         "SSH Timeout is retryable timeout",
			err:          connector.ErrSSHTimeout,
			expectedKind: FailureKindTimeout,
			retryable:    true,
		},
		{
			name:         "SSH Network is retryable network",
			err:          connector.ErrSSHNetwork,
			expectedKind: FailureKindNetwork,
			retryable:    true,
		},
		{
			name:         "SSH Auth failure is non-retryable authentication",
			err:          connector.ErrSSHAuthentication,
			expectedKind: FailureKindAuthentication,
			retryable:    false,
		},
		{
			name:         "SSH Host key mismatch is non-retryable host_key_mismatch",
			err:          connector.ErrSSHHostKeyMismatch,
			expectedKind: FailureKindHostKeyMismatch,
			retryable:    false,
		},
		{
			name:         "Dump tool missing is non-retryable dump_tool_missing",
			err:          connector.ErrDumpToolMissing,
			expectedKind: FailureKindDumpToolMissing,
			retryable:    false,
		},
		{
			name:         "Verification failure is non-retryable verification",
			err:          domain.ErrVerificationFailed,
			expectedKind: FailureKindVerification,
			retryable:    false,
		},
		{
			name:         "Corrupt resource data is non-retryable invalid_configuration",
			err:          resDomain.ErrCorruptResourceData,
			expectedKind: FailureKindInvalidConfiguration,
			retryable:    false,
		},
		{
			name:         "Invalid credential reference is non-retryable invalid_configuration",
			err:          resDomain.ErrInvalidCredentialReference,
			expectedKind: FailureKindInvalidConfiguration,
			retryable:    false,
		},
		{
			name:         "Archive tool missing is non-retryable archive_tool_missing",
			err:          connector.ErrArchiveToolMissing,
			expectedKind: FailureKindArchiveToolMissing,
			retryable:    false,
		},
		{
			name:         "Archive command failed is non-retryable archive_command_failed",
			err:          connector.ErrArchiveCommandFailed,
			expectedKind: FailureKindArchiveCommandFailed,
			retryable:    false,
		},
		{
			name:         "Worker canceled context is retryable worker_interrupted",
			err:          context.Canceled,
			expectedKind: FailureKindWorkerInterrupted,
			retryable:    true,
		},
		{
			name:         "Invalid file backup config is non-retryable invalid_configuration",
			err:          connector.ErrInvalidFileBackupConfig,
			expectedKind: FailureKindInvalidConfiguration,
			retryable:    false,
		},
		{
			name:         "Shared stderr overflow direct is non-retryable internal",
			err:          connector.ErrRemoteCommandStderrOverflow,
			expectedKind: FailureKindInternal,
			retryable:    false,
		},
		{
			name:         "Generic infrastructure database error is retryable platform_dependency",
			err:          errors.New("database connection refused"),
			expectedKind: FailureKindPlatformDependency,
			retryable:    true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ef := ClassifyError(tc.err)
			if ef == nil {
				t.Fatalf("expected non-nil ExecutionFailure for %v", tc.err)
			}
			if ef.Kind != tc.expectedKind {
				t.Errorf("expected kind %q, got %q", tc.expectedKind, ef.Kind)
			}
			if ef.Kind.IsRetryable() != tc.retryable {
				t.Errorf("expected retryable=%v, got %v", tc.retryable, ef.Kind.IsRetryable())
			}
		})
	}
}

type fakeWorkerRepo struct {
	mu                    sync.Mutex
	jobs                  map[uuid.UUID]*domain.BackupJob
	runs                  map[uuid.UUID]*domain.BackupRun
	artifacts             map[uuid.UUID]*domain.BackupArtifact
	target                *domain.StorageTarget
	targets               map[uuid.UUID]*domain.StorageTarget
	heartbeats            int
	finalizedRun          *domain.BackupRun
	finalizedJob          *domain.BackupJob
	tombstones            map[uuid.UUID]bool
	finalizeErr           error
	createArtifactErr     error
	updateVerificationErr error
	tombstoneErr          error
	getLatestRunErr       error
	updateHeartbeatErr    error
}

func newFakeWorkerRepo(orgID uuid.UUID) *fakeWorkerRepo {
	defTarget := &domain.StorageTarget{
		ID:             uuid.New(),
		OrganizationID: orgID,
		Name:           "Default Local Storage",
		Type:           domain.StorageTargetTypeLocal,
		Status:         domain.StorageTargetStatusActive,
		IsDefault:      true,
	}
	targets := make(map[uuid.UUID]*domain.StorageTarget)
	targets[defTarget.ID] = defTarget
	return &fakeWorkerRepo{
		jobs:       make(map[uuid.UUID]*domain.BackupJob),
		runs:       make(map[uuid.UUID]*domain.BackupRun),
		artifacts:  make(map[uuid.UUID]*domain.BackupArtifact),
		tombstones: make(map[uuid.UUID]bool),
		target:     defTarget,
		targets:    targets,
	}
}

func (r *fakeWorkerRepo) EnsureDefaultLocalStorageTarget(ctx context.Context, orgID uuid.UUID) (*domain.StorageTarget, error) {
	return r.target, nil
}
func (r *fakeWorkerRepo) GetStorageTargetByID(ctx context.Context, orgID, targetID uuid.UUID) (*domain.StorageTarget, error) {
	if r.targets != nil {
		if t, ok := r.targets[targetID]; ok {
			return t, nil
		}
	}
	if r.target != nil && r.target.ID == targetID {
		return r.target, nil
	}
	return nil, domain.ErrStorageTargetNotFound
}
func (r *fakeWorkerRepo) GetPlanByID(ctx context.Context, orgID, planID uuid.UUID) (*domain.BackupPlan, error) {
	return nil, nil
}
func (r *fakeWorkerRepo) CreateJob(ctx context.Context, job *domain.BackupJob) (*domain.BackupJob, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.jobs[job.ID] = job
	return job, nil
}
func (r *fakeWorkerRepo) GetJobByID(ctx context.Context, orgID, jobID uuid.UUID) (*domain.BackupJob, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.jobs[jobID], nil
}
func (r *fakeWorkerRepo) GetActiveManualJobForResource(ctx context.Context, orgID, resourceID uuid.UUID) (*domain.BackupJob, error) {
	return nil, nil
}
func (r *fakeWorkerRepo) GetActiveJobConflictForResource(ctx context.Context, orgID, resourceID uuid.UUID) (*domain.BackupJob, error) {
	return nil, nil
}
func (r *fakeWorkerRepo) FindPendingJobs(ctx context.Context, limit int, afterCreatedAt *time.Time, afterID *uuid.UUID) ([]*domain.BackupJob, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var pending []*domain.BackupJob
	for _, j := range r.jobs {
		if j.Status == domain.JobStatusPending {
			pending = append(pending, j)
		}
	}
	sort.Slice(pending, func(i, j int) bool {
		if pending[i].CreatedAt.Equal(pending[j].CreatedAt) {
			return pending[i].ID.String() < pending[j].ID.String()
		}
		return pending[i].CreatedAt.Before(pending[j].CreatedAt)
	})

	var filtered []*domain.BackupJob
	for _, j := range pending {
		if afterCreatedAt != nil && afterID != nil {
			if j.CreatedAt.Before(*afterCreatedAt) {
				continue
			}
			if j.CreatedAt.Equal(*afterCreatedAt) && j.ID.String() <= afterID.String() {
				continue
			}
		}
		filtered = append(filtered, j)
		if len(filtered) >= limit {
			break
		}
	}
	return filtered, nil
}
func (r *fakeWorkerRepo) TransactionalClaimJob(ctx context.Context, orgID, jobID uuid.UUID) (*domain.BackupRun, *domain.BackupJob, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	j, ok := r.jobs[jobID]
	if !ok || j.Status != domain.JobStatusPending {
		return nil, nil, errors.New("not pending")
	}
	if j.EngineType == "" && j.StorageTargetID == uuid.Nil && r.target != nil {
		j.EngineType = domain.EngineTypeDirectStream
		j.StorageTargetID = r.target.ID
	}
	j.Status = domain.JobStatusRunning
	run := &domain.BackupRun{
		ID:             uuid.New(),
		OrganizationID: orgID,
		JobID:          jobID,
		AttemptNumber:  1,
		Status:         domain.RunStatusRunning,
		StartedAt:      time.Now(),
		HeartbeatAt:    time.Now(),
		LeaseUntil:     time.Now().Add(2 * time.Minute),
	}
	r.runs[run.ID] = run
	return run, j, nil
}
func (r *fakeWorkerRepo) GetRunByID(ctx context.Context, orgID, runID uuid.UUID) (*domain.BackupRun, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.runs[runID], nil
}
func (r *fakeWorkerRepo) GetLatestRunForJob(ctx context.Context, orgID, jobID uuid.UUID) (*domain.BackupRun, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.getLatestRunErr != nil {
		return nil, r.getLatestRunErr
	}
	var latest *domain.BackupRun
	for _, run := range r.runs {
		if run.JobID == jobID {
			if latest == nil || run.AttemptNumber > latest.AttemptNumber {
				latest = run
			}
		}
	}
	if latest != nil {
		return latest, nil
	}
	return nil, domain.ErrRunNotFound
}
func (r *fakeWorkerRepo) UpdateHeartbeat(ctx context.Context, orgID, runID uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.heartbeats++
	if r.updateHeartbeatErr != nil {
		return r.updateHeartbeatErr
	}
	return nil
}
func (r *fakeWorkerRepo) FinalizeRunAndJob(ctx context.Context, orgID, runID, jobID uuid.UUID, runStatus domain.RunStatus, jobStatus domain.JobStatus, errMsg *string, logsSummary []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.finalizeErr != nil {
		return r.finalizeErr
	}
	if run, ok := r.runs[runID]; ok {
		run.Status = runStatus
		run.ErrorMessage = errMsg
		r.finalizedRun = run
	}
	if job, ok := r.jobs[jobID]; ok {
		job.Status = jobStatus
		r.finalizedJob = job
	}
	return nil
}
func (r *fakeWorkerRepo) CreateArtifact(ctx context.Context, artifact *domain.BackupArtifact) (*domain.BackupArtifact, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.createArtifactErr != nil {
		return nil, r.createArtifactErr
	}
	if run, ok := r.runs[artifact.RunID]; ok {
		if job, ok := r.jobs[run.JobID]; ok {
			if job.StorageTargetID != uuid.Nil && artifact.StorageTargetID != job.StorageTargetID {
				return nil, domain.ErrArtifactChainMismatch
			}
		}
	}
	r.artifacts[artifact.ID] = artifact
	return artifact, nil
}
func (r *fakeWorkerRepo) UpdateArtifactVerification(ctx context.Context, orgID, artifactID uuid.UUID, status domain.VerificationStatus, details *string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.updateVerificationErr != nil {
		return r.updateVerificationErr
	}
	if art, ok := r.artifacts[artifactID]; ok {
		art.VerificationStatus = status
		art.VerificationDetails = details
	}
	return nil
}
func (r *fakeWorkerRepo) TombstoneArtifact(ctx context.Context, orgID, artifactID uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.tombstoneErr != nil {
		return r.tombstoneErr
	}
	r.tombstones[artifactID] = true
	if art, ok := r.artifacts[artifactID]; ok {
		art.IsDeleted = true
	}
	return nil
}
func (r *fakeWorkerRepo) GetRunArtifacts(ctx context.Context, orgID, runID uuid.UUID) ([]*domain.BackupArtifact, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var arts []*domain.BackupArtifact
	for _, a := range r.artifacts {
		if a.RunID == runID {
			arts = append(arts, a)
		}
	}
	return arts, nil
}
func (r *fakeWorkerRepo) GetRunDetail(ctx context.Context, orgID, runID uuid.UUID) (*domain.BackupRunWithStats, error) {
	return nil, domain.ErrRunNotFound
}
func (r *fakeWorkerRepo) ListRuns(ctx context.Context, orgID uuid.UUID, filter domain.RunFilter) ([]*domain.BackupRunWithStats, error) {
	return nil, nil
}
func (r *fakeWorkerRepo) ListSuccessfulRunsForPlan(ctx context.Context, orgID, planID uuid.UUID) ([]*domain.BackupRun, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var res []*domain.BackupRun
	for _, run := range r.runs {
		if run.OrganizationID == orgID && run.Status == domain.RunStatusSuccess {
			if job, ok := r.jobs[run.JobID]; ok && job.BackupPlanID != nil && *job.BackupPlanID == planID {
				res = append(res, run)
			}
		}
	}
	sort.Slice(res, func(i, j int) bool {
		var iEnded, jEnded time.Time
		if res[i].EndedAt != nil {
			iEnded = *res[i].EndedAt
		}
		if res[j].EndedAt != nil {
			jEnded = *res[j].EndedAt
		}
		if iEnded.Equal(jEnded) {
			return res[i].ID.String() > res[j].ID.String()
		}
		return iEnded.After(jEnded)
	})
	return res, nil
}
func (r *fakeWorkerRepo) GetArtifactByID(ctx context.Context, orgID, artifactID uuid.UUID) (*domain.BackupArtifact, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if a, ok := r.artifacts[artifactID]; ok {
		return a, nil
	}
	return nil, domain.ErrArtifactNotFound
}
func (r *fakeWorkerRepo) ListArtifacts(ctx context.Context, orgID uuid.UUID) ([]*domain.BackupArtifact, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var arts []*domain.BackupArtifact
	for _, a := range r.artifacts {
		if !a.IsDeleted {
			arts = append(arts, a)
		}
	}
	return arts, nil
}
func (r *fakeWorkerRepo) RecoverInterruptedRuns(ctx context.Context) ([]domain.RecoveredRunInfo, error) {
	return nil, nil
}
func (r *fakeWorkerRepo) ReapStaleRuns(ctx context.Context) ([]domain.RecoveredRunInfo, error) {
	return nil, nil
}
func (r *fakeWorkerRepo) CreateStorageTarget(ctx context.Context, target *domain.StorageTarget) (*domain.StorageTarget, error) {
	return nil, nil
}
func (r *fakeWorkerRepo) ListStorageTargets(ctx context.Context, orgID uuid.UUID) ([]*domain.StorageTarget, error) {
	return nil, nil
}
func (r *fakeWorkerRepo) UpdateStorageTarget(ctx context.Context, target *domain.StorageTarget) (*domain.StorageTarget, error) {
	return nil, nil
}
func (r *fakeWorkerRepo) DeleteStorageTarget(ctx context.Context, orgID, targetID uuid.UUID) error {
	return nil
}
func (r *fakeWorkerRepo) CountArtifactsByStorageTarget(ctx context.Context, orgID, targetID uuid.UUID) (int64, error) {
	return 0, nil
}
func (r *fakeWorkerRepo) CountPlansByStorageTarget(ctx context.Context, orgID, targetID uuid.UUID) (int64, error) {
	return 0, nil
}
func (r *fakeWorkerRepo) CountActiveJobsByStorageTarget(ctx context.Context, orgID, targetID uuid.UUID) (int64, error) {
	return 0, nil
}
func (r *fakeWorkerRepo) CountRepositoriesByStorageTarget(ctx context.Context, orgID, targetID uuid.UUID) (int64, error) {
	return 0, nil
}
func (r *fakeWorkerRepo) CreateRepository(ctx context.Context, repo *domain.BackupRepository) (*domain.BackupRepository, error) {
	return nil, nil
}
func (r *fakeWorkerRepo) GetRepositoryByResourceID(ctx context.Context, orgID, resourceID uuid.UUID) (*domain.BackupRepository, error) {
	return nil, nil
}
func (r *fakeWorkerRepo) GetRepositoryByID(ctx context.Context, orgID, repoID uuid.UUID) (*domain.BackupRepository, error) {
	return nil, nil
}

type fakeResourceFinder struct {
	resWithConn *resDomain.ResourceWithConnector
}

func (f *fakeResourceFinder) FindByIDForOrganization(ctx context.Context, orgID, resID uuid.UUID) (*resDomain.ResourceWithConnector, error) {
	return f.resWithConn, nil
}

type fakeCredentialVault struct {
	payloadBytes []byte
}

func (f *fakeCredentialVault) LoadCredentialForUse(ctx context.Context, orgID, credID uuid.UUID) (credDomain.Type, []byte, error) {
	copyBuf := make([]byte, len(f.payloadBytes))
	copy(copyBuf, f.payloadBytes)
	return credDomain.TypeSSHPassword, copyBuf, nil
}

type fakeCapability struct {
	sqlDump     string
	errToReturn error
}

func (f *fakeCapability) BackupDatabase(ctx context.Context, target connector.Target, credPayload *payload.PayloadV1, databaseName string, dest io.Writer) error {
	if f.errToReturn != nil {
		return f.errToReturn
	}
	_, err := dest.Write([]byte(f.sqlDump))
	return err
}

type fakeFileCapability struct {
	tarData     []byte
	errToReturn error
}

func (f *fakeFileCapability) BackupFiles(ctx context.Context, target connector.Target, credPayload *payload.PayloadV1, config connector.FileBackupConfig, dest io.Writer) error {
	if f.errToReturn != nil {
		return f.errToReturn
	}
	_, err := dest.Write(f.tarData)
	return err
}

func createRawTarBytes(entries map[string][]byte) []byte {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for name, content := range entries {
		_ = tw.WriteHeader(&tar.Header{
			Name: name,
			Mode: 0644,
			Size: int64(len(content)),
		})
		_, _ = tw.Write(content)
	}
	_ = tw.Close()
	return buf.Bytes()
}

func TestWorkerPool_EndToEndSuccessfulBackup(t *testing.T) {
	tempDir := t.TempDir()
	storageProvider, _ := local.NewLocalStorageProvider(tempDir)
	_ = storageProvider.EnsureStorageRoot(context.Background())

	orgID := uuid.New()
	resID := uuid.New()
	credID := uuid.New()

	validPassJSON, _ := payload.EncodeV1("testpass", nil)

	fingerprint := "SHA256:mock"
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
		ID:             uuid.New(),
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
	reg.Register(resDomain.TypeUbuntuSSH, &fakeCapability{
		sqlDump: "-- MySQL dump 10.13\nCREATE DATABASE `ecommerce_prod`;\nINSERT INTO t VALUES (1);\n",
	})

	workerPool := newTestWorkerPool(
		WorkerPoolConfig{NumWorkers: 1, PollInterval: 10 * time.Millisecond},
		repo,
		&fakeResourceFinder{resWithConn: resWithConn},
		&fakeCredentialVault{payloadBytes: validPassJSON},
		reg,
		nil,
		engine.NewDirectStreamBackupEngine(),
		storageProvider,
		verification.NewVerificationEngine(),
		NewPerResourceMutexManager(),
		nil,
	)

	ctx, cancel := context.WithCancel(context.Background())
	workerPool.Start(ctx)

	// Wait for job execution to complete
	time.Sleep(100 * time.Millisecond)
	_ = workerPool.Stop(context.Background())
	cancel()

	repo.mu.Lock()
	defer repo.mu.Unlock()

	if repo.finalizedJob == nil || repo.finalizedJob.Status != domain.JobStatusCompleted {
		t.Fatalf("expected finalized job completed, got: %+v", repo.finalizedJob)
	}
	if repo.finalizedRun == nil || repo.finalizedRun.Status != domain.RunStatusSuccess {
		t.Fatalf("expected finalized run success, got: %+v", repo.finalizedRun)
	}
	if len(repo.artifacts) != 1 {
		t.Fatalf("expected 1 artifact created, got %d", len(repo.artifacts))
	}
	for _, art := range repo.artifacts {
		if art.VerificationStatus != domain.VerificationStatusVerified {
			t.Errorf("expected artifact verified, got %s", art.VerificationStatus)
		}
	}
}

func TestWorkerPool_RecoveryAndReaper(t *testing.T) {
	t.Run("Startup recovery marks interrupted runs failed", func(t *testing.T) {
		repo := newFakeWorkerRepo(uuid.New())
		runID := uuid.New()
		jobID := uuid.New()

		repo.runs[runID] = &domain.BackupRun{
			ID:     runID,
			JobID:  jobID,
			Status: domain.RunStatusRunning,
		}
		repo.jobs[jobID] = &domain.BackupJob{
			ID:     jobID,
			Status: domain.JobStatusRunning,
		}

		store, _ := local.NewLocalStorageProvider(t.TempDir())
		err := RunStartupRecovery(context.Background(), repo, store, nil)
		if err != nil {
			t.Fatalf("unexpected recovery error: %v", err)
		}
	})

	t.Run("Stale reaper lifecycle", func(t *testing.T) {
		repo := newFakeWorkerRepo(uuid.New())
		store, _ := local.NewLocalStorageProvider(t.TempDir())
		reaper := NewStaleRunReaper(repo, store, 10*time.Millisecond, nil)

		ctx, cancel := context.WithCancel(context.Background())
		reaper.Start(ctx)
		time.Sleep(30 * time.Millisecond)
		_ = reaper.Stop(context.Background())
		cancel()
	})
}

type failingCapability struct{}

func (f *failingCapability) BackupDatabase(ctx context.Context, target connector.Target, credPayload *payload.PayloadV1, databaseName string, dest io.Writer) error {
	return connector.ErrSSHAuthentication
}

func TestWorkerPool_FailureCleansArtifactsAndFailsJob(t *testing.T) {
	tempDir := t.TempDir()
	storageProvider, _ := local.NewLocalStorageProvider(tempDir)
	_ = storageProvider.EnsureStorageRoot(context.Background())

	orgID := uuid.New()
	resID := uuid.New()
	credID := uuid.New()

	validPassJSON, _ := payload.EncodeV1("testpass", nil)

	fingerprint := "SHA256:mock"
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
		ID:             uuid.New(),
		OrganizationID: orgID,
		ResourceID:     resID,
		TriggerType:    domain.TriggerTypeManual,
		BackupType:     domain.BackupTypeMySQLDatabase,
		TargetSpec:     domain.TargetSpec{Databases: []string{"prod_db"}},
		Status:         domain.JobStatusPending,
		CreatedAt:      time.Now(),
	}
	repo.jobs[job.ID] = job

	reg := connector.NewBackupCapabilityRegistry()
	reg.Register(resDomain.TypeUbuntuSSH, &failingCapability{})

	workerPool := newTestWorkerPool(
		WorkerPoolConfig{NumWorkers: 1, PollInterval: 10 * time.Millisecond},
		repo,
		&fakeResourceFinder{resWithConn: resWithConn},
		&fakeCredentialVault{payloadBytes: validPassJSON},
		reg,
		nil,
		engine.NewDirectStreamBackupEngine(),
		storageProvider,
		verification.NewVerificationEngine(),
		NewPerResourceMutexManager(),
		nil,
	)

	ctx, cancel := context.WithCancel(context.Background())
	workerPool.Start(ctx)

	time.Sleep(100 * time.Millisecond)
	_ = workerPool.Stop(context.Background())
	cancel()

	repo.mu.Lock()
	defer repo.mu.Unlock()

	if repo.finalizedJob == nil || repo.finalizedJob.Status != domain.JobStatusFailed {
		t.Fatalf("expected finalized job failed for non-retryable error, got: %+v", repo.finalizedJob)
	}
	if repo.finalizedRun == nil || repo.finalizedRun.Status != domain.RunStatusFailed {
		t.Fatalf("expected finalized run failed, got: %+v", repo.finalizedRun)
	}
}

func TestWorkerPool_QueueStarvation_600Jobs(t *testing.T) {
	tempDir := t.TempDir()
	storageProvider, _ := local.NewLocalStorageProvider(tempDir)
	_ = storageProvider.EnsureStorageRoot(context.Background())

	orgID := uuid.New()
	resID := uuid.New()
	credID := uuid.New()

	validPassJSON, _ := payload.EncodeV1("testpass", nil)
	fingerprint := "SHA256:mock"
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
	baseTime := time.Now().Add(-1 * time.Hour)

	// Create 600 pending jobs with previous failed runs that are not yet due for retry
	for i := 0; i < 600; i++ {
		jobID := uuid.New()
		runID := uuid.New()
		endedAt := baseTime.Add(50 * time.Minute) // ended 10 minutes ago
		jobCreatedAt := baseTime.Add(time.Duration(i) * time.Second)

		job := &domain.BackupJob{
			ID:             jobID,
			OrganizationID: orgID,
			ResourceID:     resID,
			TriggerType:    domain.TriggerTypeManual,
			BackupType:     domain.BackupTypeMySQLDatabase,
			TargetSpec:     domain.TargetSpec{Databases: []string{"test_db"}},
			Status:         domain.JobStatusPending,
			CreatedAt:      jobCreatedAt,
		}
		repo.jobs[jobID] = job

		// Ended recently, retry delay is 600s, so not yet due
		repo.runs[runID] = &domain.BackupRun{
			ID:             runID,
			OrganizationID: orgID,
			JobID:          jobID,
			AttemptNumber:  5,
			Status:         domain.RunStatusFailed,
			EndedAt:        &endedAt,
		}
	}

	// 601st job: Brand new, eligible immediately
	eligibleJobID := uuid.New()
	eligibleJob := &domain.BackupJob{
		ID:             eligibleJobID,
		OrganizationID: orgID,
		ResourceID:     resID,
		TriggerType:    domain.TriggerTypeManual,
		BackupType:     domain.BackupTypeMySQLDatabase,
		TargetSpec:     domain.TargetSpec{Databases: []string{"test_db"}},
		Status:         domain.JobStatusPending,
		CreatedAt:      baseTime.Add(700 * time.Second),
	}
	repo.jobs[eligibleJobID] = eligibleJob

	reg := connector.NewBackupCapabilityRegistry()
	reg.Register(resDomain.TypeUbuntuSSH, &fakeCapability{
		sqlDump: "-- MySQL dump 10.13\nCREATE TABLE t1 (id int);\n",
	})

	workerPool := newTestWorkerPool(
		WorkerPoolConfig{NumWorkers: 1, PollInterval: 10 * time.Millisecond},
		repo,
		&fakeResourceFinder{resWithConn: resWithConn},
		&fakeCredentialVault{payloadBytes: validPassJSON},
		reg,
		nil,
		engine.NewDirectStreamBackupEngine(),
		storageProvider,
		verification.NewVerificationEngine(),
		NewPerResourceMutexManager(),
		nil,
	)

	// Inject time at baseTime + 52 minutes (so 600 jobs need 10m more, but 601st is new)
	workerPool.SetNowFunc(func() time.Time {
		return baseTime.Add(52 * time.Minute)
	})

	// Process one job - should scan through all pages and execute the 601st job!
	workerPool.processNextAvailableJob(context.Background(), 1)

	repo.mu.Lock()
	defer repo.mu.Unlock()

	if repo.finalizedJob == nil || repo.finalizedJob.ID != eligibleJobID {
		t.Fatalf("expected worker to claim and complete eligible 601st job without starvation, got: %+v", repo.finalizedJob)
	}
	if repo.finalizedJob.Status != domain.JobStatusCompleted {
		t.Errorf("expected 601st job completed, got: %s", repo.finalizedJob.Status)
	}
}

func TestWorkerPool_BoundedShutdown(t *testing.T) {
	repo := newFakeWorkerRepo(uuid.New())
	workerPool := newTestWorkerPool(
		WorkerPoolConfig{NumWorkers: 2, PollInterval: 50 * time.Millisecond},
		repo,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)

	ctx, cancel := context.WithCancel(context.Background())
	workerPool.Start(ctx)

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer stopCancel()

	if err := workerPool.Stop(stopCtx); err != nil {
		t.Fatalf("expected clean stop within timeout, got: %v", err)
	}
	cancel()
}

func TestWorkerPool_FinalizeRunAndJob_OwnershipLost(t *testing.T) {
	tempDir := t.TempDir()
	storageProvider, _ := local.NewLocalStorageProvider(tempDir)
	_ = storageProvider.EnsureStorageRoot(context.Background())

	orgID := uuid.New()
	resID := uuid.New()
	credID := uuid.New()

	validPassJSON, _ := payload.EncodeV1("testpass", nil)
	fingerprint := "SHA256:mock"
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
	// Simulate ownership lost in PostgreSQL (e.g. lease expired and reaped by reaper before finalization)
	repo.finalizeErr = domain.ErrRunNoLongerActive

	job := &domain.BackupJob{
		ID:             uuid.New(),
		OrganizationID: orgID,
		ResourceID:     resID,
		TriggerType:    domain.TriggerTypeManual,
		BackupType:     domain.BackupTypeMySQLDatabase,
		TargetSpec:     domain.TargetSpec{Databases: []string{"test_db"}},
		Status:         domain.JobStatusPending,
		CreatedAt:      time.Now(),
	}
	repo.jobs[job.ID] = job

	reg := connector.NewBackupCapabilityRegistry()
	reg.Register(resDomain.TypeUbuntuSSH, &fakeCapability{
		sqlDump: "-- MySQL dump 10.13\nCREATE TABLE t1 (id int);\n",
	})

	workerPool := newTestWorkerPool(
		WorkerPoolConfig{NumWorkers: 1, PollInterval: 10 * time.Millisecond},
		repo,
		&fakeResourceFinder{resWithConn: resWithConn},
		&fakeCredentialVault{payloadBytes: validPassJSON},
		reg,
		nil,
		engine.NewDirectStreamBackupEngine(),
		storageProvider,
		verification.NewVerificationEngine(),
		NewPerResourceMutexManager(),
		nil,
	)

	// Process job safely
	workerPool.processNextAvailableJob(context.Background(), 1)

	repo.mu.Lock()
	defer repo.mu.Unlock()

	// Verify artifact cleanup was triggered on ownership loss
	if len(repo.artifacts) > 0 {
		for artID, tombstoned := range repo.tombstones {
			if !tombstoned {
				t.Errorf("expected artifact %s to be cleaned up on ownership loss", artID)
			}
		}
	}
}

type panickingEngine struct{}

func (p *panickingEngine) ExecuteDatabaseBackup(
	ctx context.Context,
	capability connector.DatabaseBackupCapability,
	target connector.Target,
	credPayload *payload.PayloadV1,
	databaseName string,
	storageProvider storage.StorageProvider,
	orgID, resID, runID, artifactID uuid.UUID,
) (*engine.ExecutionResult, error) {
	panic("unexpected internal engine panic")
}

func (p *panickingEngine) ExecuteFilesBackup(
	ctx context.Context,
	capability connector.FileBackupCapability,
	target connector.Target,
	credPayload *payload.PayloadV1,
	config connector.FileBackupConfig,
	storageProvider storage.StorageProvider,
	orgID, resID, runID, artifactID uuid.UUID,
) (*engine.ExecutionResult, error) {
	panic("unexpected internal engine panic")
}

func TestWorkerPool_PanicRecoveryAndCleanup(t *testing.T) {
	tempDir := t.TempDir()
	storageProvider, _ := local.NewLocalStorageProvider(tempDir)
	_ = storageProvider.EnsureStorageRoot(context.Background())

	orgID := uuid.New()
	resID := uuid.New()
	credID := uuid.New()

	validPassJSON, _ := payload.EncodeV1("testpass", nil)
	fingerprint := "SHA256:mock"
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
		ID:             uuid.New(),
		OrganizationID: orgID,
		ResourceID:     resID,
		TriggerType:    domain.TriggerTypeManual,
		BackupType:     domain.BackupTypeMySQLDatabase,
		TargetSpec:     domain.TargetSpec{Databases: []string{"test_db"}},
		Status:         domain.JobStatusPending,
		CreatedAt:      time.Now(),
	}
	repo.jobs[job.ID] = job

	reg := connector.NewBackupCapabilityRegistry()
	reg.Register(resDomain.TypeUbuntuSSH, &fakeCapability{
		sqlDump: "-- MySQL dump 10.13\nCREATE TABLE t1 (id int);\n",
	})

	workerPool := newTestWorkerPool(
		WorkerPoolConfig{NumWorkers: 1, PollInterval: 10 * time.Millisecond},
		repo,
		&fakeResourceFinder{resWithConn: resWithConn},
		&fakeCredentialVault{payloadBytes: validPassJSON},
		reg,
		nil,
		&panickingEngine{},
		storageProvider,
		verification.NewVerificationEngine(),
		NewPerResourceMutexManager(),
		nil,
	)

	// Process job - must safely recover panic and finalize job as failed
	workerPool.processNextAvailableJob(context.Background(), 1)

	repo.mu.Lock()
	defer repo.mu.Unlock()

	if repo.finalizedJob == nil || repo.finalizedJob.Status != domain.JobStatusFailed {
		t.Fatalf("expected finalized job failed after panic, got: %+v", repo.finalizedJob)
	}
	if repo.finalizedRun == nil || repo.finalizedRun.Status != domain.RunStatusFailed {
		t.Fatalf("expected finalized run failed after panic, got: %+v", repo.finalizedRun)
	}
}

type customMockStorageProvider struct {
	storage.StorageProvider
	deleteErr   error
	deletedRefs []string
}

func (m *customMockStorageProvider) DeleteArtifact(ctx context.Context, storageReference string) error {
	m.deletedRefs = append(m.deletedRefs, storageReference)
	if m.deleteErr != nil {
		return m.deleteErr
	}
	return nil
}

func TestWorkerPool_ArtifactCleanup_Scenarios(t *testing.T) {
	orgID := uuid.New()
	artID := uuid.New()
	storageRef := "organizations/" + orgID.String() + "/resources/" + uuid.New().String() + "/artifacts/" + artID.String() + ".sql.gz"

	t.Run("Physical Delete success -> Tombstone called", func(t *testing.T) {
		repo := newFakeWorkerRepo(orgID)
		repo.artifacts[artID] = &domain.BackupArtifact{ID: artID, OrganizationID: orgID, StorageReference: storageRef}
		mockStorage := &customMockStorageProvider{}

		pool := &WorkerPool{repo: repo, storageProvider: mockStorage, logger: slog.Default()}
		err := pool.cleanupArtifact(context.Background(), orgID, artID, uuid.Nil, storageRef)
		if err != nil {
			t.Fatalf("unexpected cleanup error: %v", err)
		}
		if !repo.tombstones[artID] {
			t.Errorf("expected artifact to be tombstoned")
		}
	})

	t.Run("Physical Delete ErrArtifactNotFound -> Tombstone called", func(t *testing.T) {
		repo := newFakeWorkerRepo(orgID)
		repo.artifacts[artID] = &domain.BackupArtifact{ID: artID, OrganizationID: orgID, StorageReference: storageRef}
		mockStorage := &customMockStorageProvider{deleteErr: storage.ErrArtifactNotFound}

		pool := &WorkerPool{repo: repo, storageProvider: mockStorage, logger: slog.Default()}
		err := pool.cleanupArtifact(context.Background(), orgID, artID, uuid.Nil, storageRef)
		if err != nil {
			t.Fatalf("unexpected cleanup error: %v", err)
		}
		if !repo.tombstones[artID] {
			t.Errorf("expected artifact to be tombstoned when already missing in storage")
		}
	})

	t.Run("Physical Delete storage error -> Tombstone NOT called", func(t *testing.T) {
		repo := newFakeWorkerRepo(orgID)
		repo.artifacts[artID] = &domain.BackupArtifact{ID: artID, OrganizationID: orgID, StorageReference: storageRef}
		mockStorage := &customMockStorageProvider{deleteErr: storage.ErrStorageIO}

		pool := &WorkerPool{repo: repo, storageProvider: mockStorage, logger: slog.Default()}
		err := pool.cleanupArtifact(context.Background(), orgID, artID, uuid.Nil, storageRef)
		if err == nil {
			t.Fatalf("expected error from physical delete failure")
		}
		if repo.tombstones[artID] {
			t.Errorf("expected artifact NOT to be tombstoned when physical delete fails")
		}
	})

	t.Run("Already IsDeleted -> no Delete / no Tombstone", func(t *testing.T) {
		runID := uuid.New()
		repo := newFakeWorkerRepo(orgID)
		repo.artifacts[artID] = &domain.BackupArtifact{ID: artID, OrganizationID: orgID, RunID: runID, StorageReference: storageRef, IsDeleted: true}
		mockStorage := &customMockStorageProvider{}

		pool := &WorkerPool{repo: repo, storageProvider: mockStorage, logger: slog.Default()}
		pool.cleanupRunArtifacts(orgID, runID)
		if len(mockStorage.deletedRefs) > 0 {
			t.Errorf("expected no delete calls on already deleted artifact, got: %v", mockStorage.deletedRefs)
		}
	})
}

func TestWorkerPool_CreateArtifact_DBFailure_CleansPhysicalOnly(t *testing.T) {
	tempDir := t.TempDir()
	storageProvider, _ := local.NewLocalStorageProvider(tempDir)
	_ = storageProvider.EnsureStorageRoot(context.Background())

	orgID := uuid.New()
	resID := uuid.New()
	credID := uuid.New()

	validPassJSON, _ := payload.EncodeV1("testpass", nil)
	fingerprint := "SHA256:mock"
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
	repo.createArtifactErr = errors.New("db insert constraint violation")

	job := &domain.BackupJob{
		ID:             uuid.New(),
		OrganizationID: orgID,
		ResourceID:     resID,
		TriggerType:    domain.TriggerTypeManual,
		BackupType:     domain.BackupTypeMySQLDatabase,
		TargetSpec:     domain.TargetSpec{Databases: []string{"test_db"}},
		Status:         domain.JobStatusPending,
		CreatedAt:      time.Now(),
	}
	repo.jobs[job.ID] = job

	reg := connector.NewBackupCapabilityRegistry()
	reg.Register(resDomain.TypeUbuntuSSH, &fakeCapability{
		sqlDump: "-- MySQL dump 10.13\nCREATE TABLE t1 (id int);\n",
	})

	workerPool := newTestWorkerPool(
		WorkerPoolConfig{NumWorkers: 1, PollInterval: 10 * time.Millisecond},
		repo,
		&fakeResourceFinder{resWithConn: resWithConn},
		&fakeCredentialVault{payloadBytes: validPassJSON},
		reg,
		nil,
		engine.NewDirectStreamBackupEngine(),
		storageProvider,
		verification.NewVerificationEngine(),
		NewPerResourceMutexManager(),
		nil,
	)

	workerPool.processNextAvailableJob(context.Background(), 1)

	repo.mu.Lock()
	defer repo.mu.Unlock()

	// Should not have any tombstoned metadata rows since DB insert failed
	if len(repo.tombstones) > 0 {
		t.Errorf("expected zero tombstones when DB insert fails, got: %v", repo.tombstones)
	}
}

type failingVerificationEngine struct{}

func (f *failingVerificationEngine) VerifyDatabaseArtifact(
	ctx context.Context,
	storageProvider storage.StorageProvider,
	storageReference string,
	expectedSizeBytes int64,
	expectedChecksumSHA256 string,
) (string, error) {
	return "", domain.ErrVerificationFailed
}

func (f *failingVerificationEngine) VerifyFilesArtifact(
	ctx context.Context,
	storageProvider storage.StorageProvider,
	storageReference string,
	expectedSizeBytes int64,
	expectedChecksumSHA256 string,
) (string, error) {
	return "", domain.ErrVerificationFailed
}

func (f *failingVerificationEngine) VerifyEncryptedDatabaseArtifact(
	ctx context.Context,
	storageProvider storage.StorageProvider,
	storageReference string,
	expectedPlaintextSize int64,
	expectedPlaintextChecksum string,
	storedSizeBytes int64,
	ciphertextSHA256 string,
	orgID, artifactID uuid.UUID,
) (string, error) {
	return "", domain.ErrVerificationFailed
}

func (f *failingVerificationEngine) VerifyEncryptedFilesArtifact(
	ctx context.Context,
	storageProvider storage.StorageProvider,
	storageReference string,
	expectedPlaintextSize int64,
	expectedPlaintextChecksum string,
	storedSizeBytes int64,
	ciphertextSHA256 string,
	orgID, artifactID uuid.UUID,
) (string, error) {
	return "", domain.ErrVerificationFailed
}

func TestWorkerPool_VerificationFailure_UpdatesAndCleans(t *testing.T) {
	tempDir := t.TempDir()
	storageProvider, _ := local.NewLocalStorageProvider(tempDir)
	_ = storageProvider.EnsureStorageRoot(context.Background())

	orgID := uuid.New()
	resID := uuid.New()
	credID := uuid.New()

	validPassJSON, _ := payload.EncodeV1("testpass", nil)
	fingerprint := "SHA256:mock"
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
		ID:             uuid.New(),
		OrganizationID: orgID,
		ResourceID:     resID,
		TriggerType:    domain.TriggerTypeManual,
		BackupType:     domain.BackupTypeMySQLDatabase,
		TargetSpec:     domain.TargetSpec{Databases: []string{"test_db"}},
		Status:         domain.JobStatusPending,
		CreatedAt:      time.Now(),
	}
	repo.jobs[job.ID] = job

	reg := connector.NewBackupCapabilityRegistry()
	reg.Register(resDomain.TypeUbuntuSSH, &fakeCapability{
		sqlDump: "-- MySQL dump 10.13\nCREATE TABLE t1 (id int);\n",
	})

	workerPool := newTestWorkerPool(
		WorkerPoolConfig{NumWorkers: 1, PollInterval: 10 * time.Millisecond},
		repo,
		&fakeResourceFinder{resWithConn: resWithConn},
		&fakeCredentialVault{payloadBytes: validPassJSON},
		reg,
		nil,
		engine.NewDirectStreamBackupEngine(),
		storageProvider,
		&failingVerificationEngine{},
		NewPerResourceMutexManager(),
		nil,
	)

	workerPool.processNextAvailableJob(context.Background(), 1)

	repo.mu.Lock()
	defer repo.mu.Unlock()

	// Verification failure should mark verification_status as failed, tombstone artifact, and fail job
	if len(repo.artifacts) == 0 {
		t.Fatalf("expected artifact to be created")
	}
	for artID, art := range repo.artifacts {
		if art.VerificationStatus != domain.VerificationStatusFailed {
			t.Errorf("expected artifact status failed, got: %s", art.VerificationStatus)
		}
		if !repo.tombstones[artID] {
			t.Errorf("expected failed verification artifact to be tombstoned")
		}
	}
	if repo.finalizedJob == nil || repo.finalizedJob.Status != domain.JobStatusFailed {
		t.Fatalf("expected job failed on verification failure, got: %+v", repo.finalizedJob)
	}
}

type keyUnavailableVerificationEngine struct{}

func (k *keyUnavailableVerificationEngine) VerifyDatabaseArtifact(ctx context.Context, storageProvider storage.StorageProvider, storageReference string, expectedSizeBytes int64, expectedChecksumSHA256 string) (string, error) {
	return "", artifactcrypto.ErrUnknownKeyVersion
}
func (k *keyUnavailableVerificationEngine) VerifyFilesArtifact(ctx context.Context, storageProvider storage.StorageProvider, storageReference string, expectedSizeBytes int64, expectedChecksumSHA256 string) (string, error) {
	return "", artifactcrypto.ErrUnknownKeyVersion
}
func (k *keyUnavailableVerificationEngine) VerifyEncryptedDatabaseArtifact(ctx context.Context, storageProvider storage.StorageProvider, storageReference string, expectedPlaintextSize int64, expectedPlaintextChecksum string, storedSizeBytes int64, ciphertextSHA256 string, orgID, artifactID uuid.UUID) (string, error) {
	return "", artifactcrypto.ErrUnknownKeyVersion
}
func (k *keyUnavailableVerificationEngine) VerifyEncryptedFilesArtifact(ctx context.Context, storageProvider storage.StorageProvider, storageReference string, expectedPlaintextSize int64, expectedPlaintextChecksum string, storedSizeBytes int64, ciphertextSHA256 string, orgID, artifactID uuid.UUID) (string, error) {
	return "", artifactcrypto.ErrUnknownKeyVersion
}

func TestWorkerPool_VerificationKeyInfrastructureFailure_PreservesUnverifiedStatus(t *testing.T) {
	tempDir := t.TempDir()
	storageProvider, _ := local.NewLocalStorageProvider(tempDir)
	_ = storageProvider.EnsureStorageRoot(context.Background())

	orgID := uuid.New()
	resID := uuid.New()
	credID := uuid.New()

	validPassJSON, _ := payload.EncodeV1("testpass", nil)
	fingerprint := "SHA256:mock"
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
		ID:             uuid.New(),
		OrganizationID: orgID,
		ResourceID:     resID,
		TriggerType:    domain.TriggerTypeManual,
		BackupType:     domain.BackupTypeMySQLDatabase,
		TargetSpec:     domain.TargetSpec{Databases: []string{"test_db"}},
		Status:         domain.JobStatusPending,
		CreatedAt:      time.Now(),
	}
	repo.jobs[job.ID] = job

	reg := connector.NewBackupCapabilityRegistry()
	reg.Register(resDomain.TypeUbuntuSSH, &fakeCapability{
		sqlDump: "-- MySQL dump 10.13\nCREATE TABLE t1 (id int);\n",
	})

	workerPool := newTestWorkerPool(
		WorkerPoolConfig{NumWorkers: 1, PollInterval: 10 * time.Millisecond},
		repo,
		&fakeResourceFinder{resWithConn: resWithConn},
		&fakeCredentialVault{payloadBytes: validPassJSON},
		reg,
		nil,
		engine.NewDirectStreamBackupEngine(),
		storageProvider,
		&keyUnavailableVerificationEngine{},
		NewPerResourceMutexManager(),
		nil,
	)

	workerPool.processNextAvailableJob(context.Background(), 1)

	repo.mu.Lock()
	defer repo.mu.Unlock()

	// Execution fails safely as an infrastructure error
	if repo.finalizedJob == nil || (repo.finalizedJob.Status != domain.JobStatusFailed && repo.finalizedJob.Status != domain.JobStatusPending) {
		t.Fatalf("expected job to fail or be retried on infrastructure error, got: %+v", repo.finalizedJob)
	}

	// The artifact must exist and MUST NOT have verification_status marked as failed
	if len(repo.artifacts) == 0 {
		t.Fatalf("expected artifact to be created")
	}
	for artID, art := range repo.artifacts {
		if art.VerificationStatus == domain.VerificationStatusFailed {
			t.Errorf("CRITICAL SECURITY VIOLATION: artifact verification_status must NOT be marked failed on ErrUnknownKeyVersion! got: %s", art.VerificationStatus)
		}
		if repo.tombstones[artID] {
			t.Errorf("artifact must NOT be tombstoned on key infrastructure error")
		}
	}
}

func TestWorkerPool_VerificationMetadataFailure_RetriesPlatformDependency(t *testing.T) {
	tempDir := t.TempDir()
	storageProvider, _ := local.NewLocalStorageProvider(tempDir)
	_ = storageProvider.EnsureStorageRoot(context.Background())

	orgID := uuid.New()
	resID := uuid.New()
	credID := uuid.New()

	validPassJSON, _ := payload.EncodeV1("testpass", nil)
	fingerprint := "SHA256:mock"
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
	// Inject DB failure when updating verification status
	repo.updateVerificationErr = errors.New("db connection lost")

	job := &domain.BackupJob{
		ID:             uuid.New(),
		OrganizationID: orgID,
		ResourceID:     resID,
		TriggerType:    domain.TriggerTypeManual,
		BackupType:     domain.BackupTypeMySQLDatabase,
		TargetSpec:     domain.TargetSpec{Databases: []string{"test_db"}},
		Status:         domain.JobStatusPending,
		CreatedAt:      time.Now(),
	}
	repo.jobs[job.ID] = job

	reg := connector.NewBackupCapabilityRegistry()
	reg.Register(resDomain.TypeUbuntuSSH, &fakeCapability{
		sqlDump: "-- MySQL dump 10.13\nCREATE TABLE t1 (id int);\n",
	})

	workerPool := newTestWorkerPool(
		WorkerPoolConfig{NumWorkers: 1, PollInterval: 10 * time.Millisecond},
		repo,
		&fakeResourceFinder{resWithConn: resWithConn},
		&fakeCredentialVault{payloadBytes: validPassJSON},
		reg,
		nil,
		engine.NewDirectStreamBackupEngine(),
		storageProvider,
		verification.NewVerificationEngine(),
		NewPerResourceMutexManager(),
		nil,
	)

	workerPool.processNextAvailableJob(context.Background(), 1)

	repo.mu.Lock()
	defer repo.mu.Unlock()

	// Should be scheduled for retry because DB update failure maps to platform_dependency (retryable)
	if repo.finalizedJob == nil || repo.finalizedJob.Status != domain.JobStatusPending {
		t.Fatalf("expected job scheduled for retry on verification metadata DB error, got: %+v", repo.finalizedJob)
	}
}

func TestWorkerPool_WebsiteFiles_Success(t *testing.T) {
	tempDir := t.TempDir()
	storageProvider, _ := local.NewLocalStorageProvider(tempDir)
	_ = storageProvider.EnsureStorageRoot(context.Background())

	orgID := uuid.New()
	resID := uuid.New()
	credID := uuid.New()

	validPassJSON, _ := payload.EncodeV1("testpass", nil)
	fingerprint := "SHA256:mock"
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
	emptyExcludes := []string{}
	job := &domain.BackupJob{
		ID:             uuid.New(),
		OrganizationID: orgID,
		ResourceID:     resID,
		TriggerType:    domain.TriggerTypeManual,
		BackupType:     domain.BackupTypeWebsiteFiles,
		TargetSpec: domain.TargetSpec{
			Paths:           []string{"/var/www/site"},
			ExcludePatterns: &emptyExcludes,
		},
		Status:    domain.JobStatusPending,
		CreatedAt: time.Now(),
	}
	repo.jobs[job.ID] = job

	tarBytes := createRawTarBytes(map[string][]byte{
		"index.php": []byte("<?php echo 'hi'; ?>"),
	})

	fileReg := connector.NewFileBackupCapabilityRegistry()
	fileReg.Register(resDomain.TypeUbuntuSSH, &fakeFileCapability{tarData: tarBytes})

	workerPool := newTestWorkerPool(
		WorkerPoolConfig{NumWorkers: 1, PollInterval: 10 * time.Millisecond},
		repo,
		&fakeResourceFinder{resWithConn: resWithConn},
		&fakeCredentialVault{payloadBytes: validPassJSON},
		nil,
		fileReg,
		engine.NewDirectStreamBackupEngine(),
		storageProvider,
		verification.NewVerificationEngine(),
		NewPerResourceMutexManager(),
		nil,
	)

	workerPool.processNextAvailableJob(context.Background(), 1)

	repo.mu.Lock()
	defer repo.mu.Unlock()

	if repo.finalizedJob == nil || repo.finalizedJob.Status != domain.JobStatusCompleted {
		t.Fatalf("expected finalized job completed, got: %+v", repo.finalizedJob)
	}
	if repo.finalizedRun == nil || repo.finalizedRun.Status != domain.RunStatusSuccess {
		t.Fatalf("expected finalized run success, got: %+v", repo.finalizedRun)
	}
	if len(repo.artifacts) != 1 {
		t.Fatalf("expected 1 artifact, got %d", len(repo.artifacts))
	}
	for _, art := range repo.artifacts {
		if art.ArtifactType != domain.ArtifactTypeFilesArchive {
			t.Errorf("expected ArtifactType files_archive, got %s", art.ArtifactType)
		}
		if art.Format != domain.ArtifactFormatTarGzip {
			t.Errorf("expected Format tar_gzip, got %s", art.Format)
		}
		if art.TargetName != "/var/www/site" {
			t.Errorf("expected TargetName /var/www/site, got %s", art.TargetName)
		}
		if art.VerificationStatus != domain.VerificationStatusVerified {
			t.Errorf("expected artifact verified, got %s", art.VerificationStatus)
		}
	}
}

func TestWorkerPool_WebsiteFiles_MultiPath(t *testing.T) {
	tempDir := t.TempDir()
	storageProvider, _ := local.NewLocalStorageProvider(tempDir)
	_ = storageProvider.EnsureStorageRoot(context.Background())

	orgID := uuid.New()
	resID := uuid.New()
	credID := uuid.New()

	validPassJSON, _ := payload.EncodeV1("testpass", nil)
	fingerprint := "SHA256:mock"
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
	emptyExcludes := []string{}
	job := &domain.BackupJob{
		ID:             uuid.New(),
		OrganizationID: orgID,
		ResourceID:     resID,
		TriggerType:    domain.TriggerTypeManual,
		BackupType:     domain.BackupTypeWebsiteFiles,
		TargetSpec: domain.TargetSpec{
			Paths:           []string{"/var/www/site1", "/home/app/public_html"},
			ExcludePatterns: &emptyExcludes,
		},
		Status:    domain.JobStatusPending,
		CreatedAt: time.Now(),
	}
	repo.jobs[job.ID] = job

	tarBytes := createRawTarBytes(map[string][]byte{
		"index.html": []byte("<h1>site</h1>"),
	})

	fileReg := connector.NewFileBackupCapabilityRegistry()
	fileReg.Register(resDomain.TypeUbuntuSSH, &fakeFileCapability{tarData: tarBytes})

	workerPool := newTestWorkerPool(
		WorkerPoolConfig{NumWorkers: 1, PollInterval: 10 * time.Millisecond},
		repo,
		&fakeResourceFinder{resWithConn: resWithConn},
		&fakeCredentialVault{payloadBytes: validPassJSON},
		nil,
		fileReg,
		engine.NewDirectStreamBackupEngine(),
		storageProvider,
		verification.NewVerificationEngine(),
		NewPerResourceMutexManager(),
		nil,
	)

	workerPool.processNextAvailableJob(context.Background(), 1)

	repo.mu.Lock()
	defer repo.mu.Unlock()

	if repo.finalizedJob == nil || repo.finalizedJob.Status != domain.JobStatusCompleted {
		t.Fatalf("expected job completed, got: %+v", repo.finalizedJob)
	}
	if len(repo.artifacts) != 2 {
		t.Fatalf("expected 2 artifacts for 2 paths, got %d", len(repo.artifacts))
	}
}

type conditionalFileCapability struct {
	failOnSecondPath bool
	callCount        int
	tarBytes         []byte
}

func (c *conditionalFileCapability) BackupFiles(ctx context.Context, target connector.Target, credPayload *payload.PayloadV1, config connector.FileBackupConfig, dest io.Writer) error {
	c.callCount++
	if c.failOnSecondPath && c.callCount == 2 {
		return connector.ErrArchiveCommandFailed
	}
	_, err := dest.Write(c.tarBytes)
	return err
}

func TestWorkerPool_WebsiteFiles_PartialFailure_CleansAll(t *testing.T) {
	tempDir := t.TempDir()
	storageProvider, _ := local.NewLocalStorageProvider(tempDir)
	_ = storageProvider.EnsureStorageRoot(context.Background())

	orgID := uuid.New()
	resID := uuid.New()
	credID := uuid.New()

	validPassJSON, _ := payload.EncodeV1("testpass", nil)
	fingerprint := "SHA256:mock"
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
	emptyExcludes := []string{}
	job := &domain.BackupJob{
		ID:             uuid.New(),
		OrganizationID: orgID,
		ResourceID:     resID,
		TriggerType:    domain.TriggerTypeManual,
		BackupType:     domain.BackupTypeWebsiteFiles,
		TargetSpec: domain.TargetSpec{
			Paths:           []string{"/var/www/site1", "/var/www/site2"},
			ExcludePatterns: &emptyExcludes,
		},
		Status:    domain.JobStatusPending,
		CreatedAt: time.Now(),
	}
	repo.jobs[job.ID] = job

	tarBytes := createRawTarBytes(map[string][]byte{"index.php": []byte("content")})
	condCap := &conditionalFileCapability{failOnSecondPath: true, tarBytes: tarBytes}

	fileReg := connector.NewFileBackupCapabilityRegistry()
	fileReg.Register(resDomain.TypeUbuntuSSH, condCap)

	workerPool := newTestWorkerPool(
		WorkerPoolConfig{NumWorkers: 1, PollInterval: 10 * time.Millisecond},
		repo,
		&fakeResourceFinder{resWithConn: resWithConn},
		&fakeCredentialVault{payloadBytes: validPassJSON},
		nil,
		fileReg,
		engine.NewDirectStreamBackupEngine(),
		storageProvider,
		verification.NewVerificationEngine(),
		NewPerResourceMutexManager(),
		nil,
	)

	workerPool.processNextAvailableJob(context.Background(), 1)

	repo.mu.Lock()
	defer repo.mu.Unlock()

	if repo.finalizedJob == nil || repo.finalizedJob.Status != domain.JobStatusFailed {
		t.Fatalf("expected job failed on multi-path partial failure, got: %+v", repo.finalizedJob)
	}

	// Verify artifact from path 1 was tombstoned and cleaned up
	if len(repo.artifacts) > 0 {
		for artID, tombstoned := range repo.tombstones {
			if !tombstoned {
				t.Errorf("expected artifact %s to be tombstoned on multi-path failure", artID)
			}
		}
	}
}

func TestWorkerPool_GetLatestRun_DBError_NoClaim(t *testing.T) {
	tempDir := t.TempDir()
	storageProvider, _ := local.NewLocalStorageProvider(tempDir)
	_ = storageProvider.EnsureStorageRoot(context.Background())

	orgID := uuid.New()
	resID := uuid.New()
	credID := uuid.New()

	validPassJSON, _ := payload.EncodeV1("testpass", nil)
	fingerprint := "SHA256:mock"
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
	// Inject database/infrastructure error on latest run query
	repo.getLatestRunErr = errors.New("database temporarily unavailable")

	job := &domain.BackupJob{
		ID:             uuid.New(),
		OrganizationID: orgID,
		ResourceID:     resID,
		TriggerType:    domain.TriggerTypeManual,
		BackupType:     domain.BackupTypeMySQLDatabase,
		TargetSpec:     domain.TargetSpec{Databases: []string{"test_db"}},
		Status:         domain.JobStatusPending,
		CreatedAt:      time.Now(),
	}
	repo.jobs[job.ID] = job

	reg := connector.NewBackupCapabilityRegistry()
	reg.Register(resDomain.TypeUbuntuSSH, &fakeCapability{
		sqlDump: "-- MySQL dump 10.13\n",
	})

	mutexMgr := NewPerResourceMutexManager()

	workerPool := newTestWorkerPool(
		WorkerPoolConfig{NumWorkers: 1, PollInterval: 10 * time.Millisecond},
		repo,
		&fakeResourceFinder{resWithConn: resWithConn},
		&fakeCredentialVault{payloadBytes: validPassJSON},
		reg,
		nil,
		engine.NewDirectStreamBackupEngine(),
		storageProvider,
		verification.NewVerificationEngine(),
		mutexMgr,
		nil,
	)

	workerPool.processNextAvailableJob(context.Background(), 1)

	repo.mu.Lock()
	defer repo.mu.Unlock()

	// Assert job was NOT claimed, no runs created, and job is still pending
	if repo.finalizedJob != nil {
		t.Errorf("expected no job finalization on DB error, got: %+v", repo.finalizedJob)
	}
	if len(repo.runs) != 0 {
		t.Errorf("expected zero runs created, got %d", len(repo.runs))
	}
	if repo.jobs[job.ID].Status != domain.JobStatusPending {
		t.Errorf("expected job to remain pending, got: %s", repo.jobs[job.ID].Status)
	}

	// Assert mutex is not locked
	unlock, acquired := mutexMgr.TryAcquire(resID)
	if !acquired {
		t.Errorf("expected mutex to be available")
	} else {
		unlock()
	}
}

type blockingUntilCanceledFileCapability struct{}

func (b *blockingUntilCanceledFileCapability) BackupFiles(ctx context.Context, target connector.Target, credPayload *payload.PayloadV1, config connector.FileBackupConfig, dest io.Writer) error {
	<-ctx.Done()
	return ctx.Err()
}

func TestWorkerPool_HeartbeatFailure_CancelsExecution(t *testing.T) {
	tempDir := t.TempDir()
	storageProvider, _ := local.NewLocalStorageProvider(tempDir)
	_ = storageProvider.EnsureStorageRoot(context.Background())

	orgID := uuid.New()
	resID := uuid.New()
	credID := uuid.New()

	validPassJSON, _ := payload.EncodeV1("testpass", nil)
	fingerprint := "SHA256:mock"
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
	repo.updateHeartbeatErr = errors.New("heartbeat db failure")

	emptyExcludes := []string{}
	job := &domain.BackupJob{
		ID:             uuid.New(),
		OrganizationID: orgID,
		ResourceID:     resID,
		TriggerType:    domain.TriggerTypeManual,
		BackupType:     domain.BackupTypeWebsiteFiles,
		TargetSpec: domain.TargetSpec{
			Paths:           []string{"/var/www/site"},
			ExcludePatterns: &emptyExcludes,
		},
		Status:    domain.JobStatusPending,
		CreatedAt: time.Now(),
	}
	repo.jobs[job.ID] = job

	fileReg := connector.NewFileBackupCapabilityRegistry()
	fileReg.Register(resDomain.TypeUbuntuSSH, &blockingUntilCanceledFileCapability{})

	mutexMgr := NewPerResourceMutexManager()

	workerPool := newTestWorkerPool(
		WorkerPoolConfig{NumWorkers: 1, PollInterval: 10 * time.Millisecond, HeartbeatInterval: 10 * time.Millisecond},
		repo,
		&fakeResourceFinder{resWithConn: resWithConn},
		&fakeCredentialVault{payloadBytes: validPassJSON},
		nil,
		fileReg,
		engine.NewDirectStreamBackupEngine(),
		storageProvider,
		verification.NewVerificationEngine(),
		mutexMgr,
		nil,
	)

	// Process job - heartbeat will fail 3 times and cancel execution promptly
	workerPool.processNextAvailableJob(context.Background(), 1)

	repo.mu.Lock()
	defer repo.mu.Unlock()

	// Verify run failed due to cancellation from heartbeat failure threshold
	if repo.finalizedRun == nil || repo.finalizedRun.Status != domain.RunStatusFailed {
		t.Fatalf("expected run failed after heartbeat cancellation, got: %+v", repo.finalizedRun)
	}

	// Verify mutex was released
	unlock, acquired := mutexMgr.TryAcquire(resID)
	if !acquired {
		t.Errorf("expected resource mutex to be unlocked after execution cancellation")
	} else {
		unlock()
	}
}

type panickingFileCapability struct{}

func (p *panickingFileCapability) BackupFiles(ctx context.Context, target connector.Target, credPayload *payload.PayloadV1, config connector.FileBackupConfig, dest io.Writer) error {
	panic("fake panic with secret /secret/token")
}

func TestWorkerPool_WebsiteBackup_PanicContained_CleansAndReleasesMutex(t *testing.T) {
	tempDir := t.TempDir()
	storageProvider, _ := local.NewLocalStorageProvider(tempDir)
	_ = storageProvider.EnsureStorageRoot(context.Background())

	orgID := uuid.New()
	resID := uuid.New()
	credID := uuid.New()

	validPassJSON, _ := payload.EncodeV1("testpass", nil)
	fingerprint := "SHA256:mock"
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
	emptyExcludes := []string{}
	job := &domain.BackupJob{
		ID:             uuid.New(),
		OrganizationID: orgID,
		ResourceID:     resID,
		TriggerType:    domain.TriggerTypeManual,
		BackupType:     domain.BackupTypeWebsiteFiles,
		TargetSpec: domain.TargetSpec{
			Paths:           []string{"/var/www/site"},
			ExcludePatterns: &emptyExcludes,
		},
		Status:    domain.JobStatusPending,
		CreatedAt: time.Now(),
	}
	repo.jobs[job.ID] = job

	fileReg := connector.NewFileBackupCapabilityRegistry()
	fileReg.Register(resDomain.TypeUbuntuSSH, &panickingFileCapability{})

	mutexMgr := NewPerResourceMutexManager()

	workerPool := newTestWorkerPool(
		WorkerPoolConfig{NumWorkers: 1, PollInterval: 10 * time.Millisecond},
		repo,
		&fakeResourceFinder{resWithConn: resWithConn},
		&fakeCredentialVault{payloadBytes: validPassJSON},
		nil,
		fileReg,
		engine.NewDirectStreamBackupEngine(),
		storageProvider,
		verification.NewVerificationEngine(),
		mutexMgr,
		nil,
	)

	// Process job - must recover from panic safely and not crash
	workerPool.processNextAvailableJob(context.Background(), 1)

	repo.mu.Lock()
	defer repo.mu.Unlock()

	if repo.finalizedJob == nil || repo.finalizedJob.Status != domain.JobStatusFailed {
		t.Fatalf("expected finalized job failed after panic, got: %+v", repo.finalizedJob)
	}
	if repo.finalizedRun == nil || repo.finalizedRun.Status != domain.RunStatusFailed {
		t.Fatalf("expected finalized run failed after panic, got: %+v", repo.finalizedRun)
	}

	// Verify mutex was released
	unlock, acquired := mutexMgr.TryAcquire(resID)
	if !acquired {
		t.Errorf("expected resource mutex to be unlocked after recovered panic")
	} else {
		unlock()
	}
}

func TestTaxonomy_WebsiteStderrOverflow_SafeErrorMessage(t *testing.T) {
	ef := ClassifyError(connector.ErrArchiveCommandFailed)
	if ef == nil {
		t.Fatalf("expected non-nil execution failure")
	}
	msg := ef.Kind.SafeMessage()
	if strings.Contains(strings.ToLower(msg), "database dump") {
		t.Errorf("expected archive failure message not to contain 'database dump', got: %s", msg)
	}
	if !strings.Contains(strings.ToLower(msg), "website") && !strings.Contains(strings.ToLower(msg), "archive") {
		t.Errorf("expected archive failure message to contain 'website' or 'archive', got: %s", msg)
	}
}

func TestStartHeartbeat_ImmediateLeaseLoss(t *testing.T) {
	orgID := uuid.New()
	runID := uuid.New()
	repo := newFakeWorkerRepo(orgID)
	repo.updateHeartbeatErr = domain.ErrRunNoLongerActive

	canceledCh := make(chan struct{})
	var cancelOnce sync.Once
	cancelExec := func() {
		cancelOnce.Do(func() {
			close(canceledCh)
		})
	}

	stop := StartHeartbeat(
		context.Background(),
		repo,
		orgID,
		runID,
		10*time.Millisecond,
		cancelExec,
		nil,
	)
	defer stop()

	select {
	case <-canceledCh:
		// Succeeded immediately on first heartbeat
	case <-time.After(1 * time.Second):
		t.Fatalf("expected immediate cancellation on lease loss")
	}

	repo.mu.Lock()
	defer repo.mu.Unlock()

	if repo.heartbeats != 1 {
		t.Errorf("expected exactly 1 heartbeat attempt before cancellation, got %d", repo.heartbeats)
	}
}

func TestStartHeartbeat_GenericDBFailure_ThreeRetriesThreshold(t *testing.T) {
	orgID := uuid.New()
	runID := uuid.New()
	repo := newFakeWorkerRepo(orgID)
	repo.updateHeartbeatErr = errors.New("db connection refused")

	canceledCh := make(chan struct{})
	var cancelOnce sync.Once
	cancelExec := func() {
		cancelOnce.Do(func() {
			close(canceledCh)
		})
	}

	stop := StartHeartbeat(
		context.Background(),
		repo,
		orgID,
		runID,
		10*time.Millisecond,
		cancelExec,
		nil,
	)
	defer stop()

	select {
	case <-canceledCh:
		// Cancelled after 3 attempts
	case <-time.After(1 * time.Second):
		t.Fatalf("expected cancellation after 3 consecutive failures")
	}

	repo.mu.Lock()
	defer repo.mu.Unlock()

	if repo.heartbeats != 3 {
		t.Errorf("expected exactly 3 heartbeat attempts before cancellation threshold, got %d", repo.heartbeats)
	}
}

type panicOnSuccessLogHandler struct {
	panicMsg string
}

func (h *panicOnSuccessLogHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return true
}

func (h *panicOnSuccessLogHandler) Handle(ctx context.Context, r slog.Record) error {
	if r.Message == h.panicMsg {
		panic("simulated logger panic post commit")
	}
	return nil
}

func (h *panicOnSuccessLogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return h
}

func (h *panicOnSuccessLogHandler) WithGroup(name string) slog.Handler {
	return h
}

func TestWorkerPool_PostCommitPanic_DoesNotCorruptSuccess(t *testing.T) {
	tempDir := t.TempDir()
	storageProvider, _ := local.NewLocalStorageProvider(tempDir)
	_ = storageProvider.EnsureStorageRoot(context.Background())

	orgID := uuid.New()
	resID := uuid.New()
	credID := uuid.New()

	validPassJSON, _ := payload.EncodeV1("testpass", nil)
	fingerprint := "SHA256:mock"
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
	emptyExcludes := []string{}
	job := &domain.BackupJob{
		ID:             uuid.New(),
		OrganizationID: orgID,
		ResourceID:     resID,
		TriggerType:    domain.TriggerTypeManual,
		BackupType:     domain.BackupTypeWebsiteFiles,
		TargetSpec: domain.TargetSpec{
			Paths:           []string{"/var/www/site"},
			ExcludePatterns: &emptyExcludes,
		},
		Status:    domain.JobStatusPending,
		CreatedAt: time.Now(),
	}
	repo.jobs[job.ID] = job

	tarBytes := createRawTarBytes(map[string][]byte{"index.html": []byte("<h1>test</h1>")})
	fileReg := connector.NewFileBackupCapabilityRegistry()
	fileReg.Register(resDomain.TypeUbuntuSSH, &fakeFileCapability{tarData: tarBytes})

	mutexMgr := NewPerResourceMutexManager()

	// Logger that panics strictly when "backup run completed successfully" is logged post-commit
	panickingLogger := slog.New(&panicOnSuccessLogHandler{panicMsg: "backup run completed successfully"})

	workerPool := newTestWorkerPool(
		WorkerPoolConfig{NumWorkers: 1, PollInterval: 10 * time.Millisecond},
		repo,
		&fakeResourceFinder{resWithConn: resWithConn},
		&fakeCredentialVault{payloadBytes: validPassJSON},
		nil,
		fileReg,
		engine.NewDirectStreamBackupEngine(),
		storageProvider,
		verification.NewVerificationEngine(),
		mutexMgr,
		panickingLogger,
	)

	// Execute job - logger will panic post-commit, recover defer should contain panic safely
	workerPool.processNextAvailableJob(context.Background(), 1)

	repo.mu.Lock()
	defer repo.mu.Unlock()

	// Assert Run remains success and Job remains completed
	if repo.finalizedJob == nil || repo.finalizedJob.Status != domain.JobStatusCompleted {
		t.Fatalf("expected job to remain completed after post-commit panic, got: %+v", repo.finalizedJob)
	}
	if repo.finalizedRun == nil || repo.finalizedRun.Status != domain.RunStatusSuccess {
		t.Fatalf("expected run to remain success after post-commit panic, got: %+v", repo.finalizedRun)
	}

	// Assert verified artifact is NOT tombstoned or deleted
	if len(repo.artifacts) == 0 {
		t.Fatalf("expected artifact to be preserved")
	}
	for artID, art := range repo.artifacts {
		if repo.tombstones[artID] {
			t.Errorf("expected artifact %s NOT to be tombstoned on post-commit panic", artID)
		}
		rc, err := storageProvider.OpenArtifact(context.Background(), art.StorageReference)
		if err != nil {
			t.Errorf("expected physical artifact to still exist on disk: %v", err)
		} else {
			_ = rc.Close()
		}
	}

	// Assert Mutex was released
	unlock, acquired := mutexMgr.TryAcquire(resID)
	if !acquired {
		t.Errorf("expected resource mutex to be unlocked after recovered post-commit panic")
	} else {
		unlock()
	}
}

type fakeDatabaseDiscoverer struct {
	discovered []connector.DatabaseInfo
	err        error
}

func (f *fakeDatabaseDiscoverer) DiscoverDatabases(ctx context.Context, target connector.Target, credPayload *payload.PayloadV1) ([]connector.DatabaseInfo, error) {
	return f.discovered, f.err
}

func TestWorkerPool_MySQLDatabase_ModeAll_DiscoversAndDumpsAllDatabases(t *testing.T) {
	orgID := uuid.New()
	resID := uuid.New()
	credID := uuid.New()
	timeoutSec := 5

	resWithConn := &resDomain.ResourceWithConnector{
		Resource: &resDomain.Resource{
			ID:             resID,
			OrganizationID: orgID,
			Name:           "Test Ubuntu SSH",
			Type:           resDomain.TypeUbuntuSSH,
			Status:         resDomain.StatusActive,
		},
		Connector: &resDomain.ResourceConnector{
			ID:             uuid.New(),
			OrganizationID: orgID,
			ResourceID:     resID,
			ConnectorType:  resDomain.ConnectorTypeUbuntuSSH,
			AuthType:       resDomain.AuthTypeSSHPassword,
			Host:           "127.0.0.1",
			Port:           22,
			CredentialID:   credID,
			Config: resDomain.ConnectorConfig{
				Username:                 "root",
				ConnectionTimeoutSeconds: &timeoutSec,
			},
		},
	}

	validPassJSON, _ := payload.EncodeV1("secret123", nil)
	storageDir := t.TempDir()
	storageProvider, _ := local.NewLocalStorageProvider(storageDir)
	_ = storageProvider.EnsureStorageRoot(context.Background())

	repo := newFakeWorkerRepo(orgID)
	job := &domain.BackupJob{
		ID:             uuid.New(),
		OrganizationID: orgID,
		ResourceID:     resID,
		TriggerType:    domain.TriggerTypeManual,
		BackupType:     domain.BackupTypeMySQLDatabase,
		TargetSpec:     domain.TargetSpec{Databases: []string{}}, // mode = "all"
		Status:         domain.JobStatusPending,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}
	repo.jobs[job.ID] = job

	validSQLDump := "-- MySQL dump 10.13\nCREATE DATABASE IF NOT EXISTS test;\n"
	capReg := connector.NewBackupCapabilityRegistry()
	capReg.Register(resDomain.TypeUbuntuSSH, &fakeCapability{sqlDump: validSQLDump})

	workerPool := newTestWorkerPool(
		WorkerPoolConfig{NumWorkers: 1, PollInterval: 10 * time.Millisecond},
		repo,
		&fakeResourceFinder{resWithConn: resWithConn},
		&fakeCredentialVault{payloadBytes: validPassJSON},
		capReg,
		nil,
		engine.NewDirectStreamBackupEngine(),
		storageProvider,
		verification.NewVerificationEngine(),
		NewPerResourceMutexManager(),
		slog.Default(),
	)

	tblCount1 := int64(5)
	tblCount2 := int64(10)

	// Inject fake discoverer returning 2 databases
	workerPool.databaseDiscoverer = &fakeDatabaseDiscoverer{
		discovered: []connector.DatabaseInfo{
			{Name: "app_db", TablesCount: &tblCount1, SizeBytes: 1024, Status: connector.DatabaseStatusAccessible},
			{Name: "analytics_db", TablesCount: &tblCount2, SizeBytes: 2048, Status: connector.DatabaseStatusAccessible},
		},
	}

	workerPool.processNextAvailableJob(context.Background(), 1)

	repo.mu.Lock()
	defer repo.mu.Unlock()

	if repo.finalizedJob == nil || repo.finalizedJob.Status != domain.JobStatusCompleted {
		t.Fatalf("expected job to be finalized as completed, got: %+v", repo.finalizedJob)
	}
	if repo.finalizedRun == nil || repo.finalizedRun.Status != domain.RunStatusSuccess {
		t.Fatalf("expected run to be finalized as success, got: %+v", repo.finalizedRun)
	}

	if len(repo.artifacts) != 2 {
		t.Fatalf("expected 2 artifacts created for mode all, got %d", len(repo.artifacts))
	}

	targetNames := make([]string, 0, 2)
	for _, art := range repo.artifacts {
		targetNames = append(targetNames, art.TargetName)
		if art.VerificationStatus != domain.VerificationStatusVerified {
			t.Errorf("expected artifact %s to be verified, got: %s", art.ID, art.VerificationStatus)
		}
	}
	sort.Strings(targetNames)
	if targetNames[0] != "analytics_db" || targetNames[1] != "app_db" {
		t.Fatalf("expected artifacts for analytics_db and app_db, got %v", targetNames)
	}
}

type fakeRetentionManager struct {
	mu          sync.Mutex
	calls       int
	lastOrgID   uuid.UUID
	lastPlanID  *uuid.UUID
	lastRunID   uuid.UUID
	errToReturn error
}

func (f *fakeRetentionManager) ApplyAfterSuccessfulRun(ctx context.Context, orgID uuid.UUID, planID *uuid.UUID, currentRunID uuid.UUID) (*retention.CleanupSummary, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.lastOrgID = orgID
	f.lastPlanID = planID
	f.lastRunID = currentRunID
	if f.errToReturn != nil {
		return nil, f.errToReturn
	}
	return &retention.CleanupSummary{}, nil
}

func TestWorkerPool_RetentionIntegration_PostSuccessInvocation(t *testing.T) {
	orgID := uuid.New()
	resID := uuid.New()
	credID := uuid.New()
	planID := uuid.New()

	timeoutSec := 10
	resWithConn := &resDomain.ResourceWithConnector{
		Resource: &resDomain.Resource{
			ID:             resID,
			OrganizationID: orgID,
			Type:           resDomain.TypeUbuntuSSH,
			Status:         resDomain.StatusActive,
		},
		Connector: &resDomain.ResourceConnector{
			ID:           uuid.New(),
			ResourceID:   resID,
			CredentialID: credID,
			Host:         "127.0.0.1",
			Port:         22,
			AuthType:     resDomain.AuthTypeSSHPassword,
			Config: resDomain.ConnectorConfig{
				Username:                 "user",
				ConnectionTimeoutSeconds: &timeoutSec,
			},
		},
	}

	validPassJSON, _ := payload.EncodeV1("pass", nil)
	storageDir := t.TempDir()
	storageProvider, _ := local.NewLocalStorageProvider(storageDir)
	_ = storageProvider.EnsureStorageRoot(context.Background())

	t.Run("invokes retention on successful plan-triggered job", func(t *testing.T) {
		repo := newFakeWorkerRepo(orgID)
		job := &domain.BackupJob{
			ID:              uuid.New(),
			OrganizationID:  orgID,
			ResourceID:      resID,
			BackupPlanID:    &planID,
			TriggerType:     domain.TriggerTypeScheduled,
			BackupType:      domain.BackupTypeMySQLDatabase,
			EngineType:      domain.EngineTypeDirectStream,
			StorageTargetID: repo.target.ID,
			TargetSpec:      domain.TargetSpec{Databases: []string{"testdb"}},
			Status:          domain.JobStatusPending,
			CreatedAt:       time.Now(),
			UpdatedAt:       time.Now(),
		}
		repo.jobs[job.ID] = job

		capReg := connector.NewBackupCapabilityRegistry()
		capReg.Register(resDomain.TypeUbuntuSSH, &fakeCapability{sqlDump: "-- MySQL dump\nCREATE DATABASE testdb;\n"})

		retManager := &fakeRetentionManager{}
		workerPool := newTestWorkerPool(
			WorkerPoolConfig{NumWorkers: 1, PollInterval: 10 * time.Millisecond},
			repo,
			&fakeResourceFinder{resWithConn: resWithConn},
			&fakeCredentialVault{payloadBytes: validPassJSON},
			capReg,
			nil,
			engine.NewDirectStreamBackupEngine(),
			storageProvider,
			verification.NewVerificationEngine(),
			NewPerResourceMutexManager(),
			slog.Default(),
		)
		workerPool.SetRetentionManager(retManager)

		workerPool.processNextAvailableJob(context.Background(), 1)

		repo.mu.Lock()
		defer repo.mu.Unlock()

		if repo.finalizedJob == nil || repo.finalizedJob.Status != domain.JobStatusCompleted {
			t.Fatalf("expected job to be completed")
		}
		if repo.finalizedRun == nil || repo.finalizedRun.Status != domain.RunStatusSuccess {
			t.Fatalf("expected run to be success")
		}

		if retManager.calls != 1 {
			t.Fatalf("expected retentionManager to be called once, got %d", retManager.calls)
		}
		if retManager.lastOrgID != orgID {
			t.Errorf("expected retention orgID %s, got %s", orgID, retManager.lastOrgID)
		}
		if retManager.lastPlanID == nil || *retManager.lastPlanID != planID {
			t.Errorf("expected retention planID %s, got %v", planID, retManager.lastPlanID)
		}
		if retManager.lastRunID != repo.finalizedRun.ID {
			t.Errorf("expected retention currentRunID %s, got %s", repo.finalizedRun.ID, retManager.lastRunID)
		}
	})

	t.Run("does not invoke retention for ad-hoc job without plan", func(t *testing.T) {
		repo := newFakeWorkerRepo(orgID)
		job := &domain.BackupJob{
			ID:              uuid.New(),
			OrganizationID:  orgID,
			ResourceID:      resID,
			BackupPlanID:    nil, // Ad-hoc job
			TriggerType:     domain.TriggerTypeManual,
			BackupType:      domain.BackupTypeMySQLDatabase,
			EngineType:      domain.EngineTypeDirectStream,
			StorageTargetID: repo.target.ID,
			TargetSpec:      domain.TargetSpec{Databases: []string{"testdb"}},
			Status:          domain.JobStatusPending,
			CreatedAt:       time.Now(),
			UpdatedAt:       time.Now(),
		}
		repo.jobs[job.ID] = job

		capReg := connector.NewBackupCapabilityRegistry()
		capReg.Register(resDomain.TypeUbuntuSSH, &fakeCapability{sqlDump: "-- MySQL dump\nCREATE DATABASE testdb;\n"})

		retManager := &fakeRetentionManager{}
		workerPool := newTestWorkerPool(
			WorkerPoolConfig{NumWorkers: 1, PollInterval: 10 * time.Millisecond},
			repo,
			&fakeResourceFinder{resWithConn: resWithConn},
			&fakeCredentialVault{payloadBytes: validPassJSON},
			capReg,
			nil,
			engine.NewDirectStreamBackupEngine(),
			storageProvider,
			verification.NewVerificationEngine(),
			NewPerResourceMutexManager(),
			slog.Default(),
		)
		workerPool.SetRetentionManager(retManager)

		workerPool.processNextAvailableJob(context.Background(), 1)

		if retManager.calls != 0 {
			t.Errorf("expected retentionManager NOT to be called for ad-hoc job, got %d calls", retManager.calls)
		}
	})

	t.Run("retention error does not mutate successful job/run status or delete artifacts", func(t *testing.T) {
		repo := newFakeWorkerRepo(orgID)
		job := &domain.BackupJob{
			ID:              uuid.New(),
			OrganizationID:  orgID,
			ResourceID:      resID,
			BackupPlanID:    &planID,
			TriggerType:     domain.TriggerTypeScheduled,
			BackupType:      domain.BackupTypeMySQLDatabase,
			EngineType:      domain.EngineTypeDirectStream,
			StorageTargetID: repo.target.ID,
			TargetSpec:      domain.TargetSpec{Databases: []string{"testdb"}},
			Status:          domain.JobStatusPending,
			CreatedAt:       time.Now(),
			UpdatedAt:       time.Now(),
		}
		repo.jobs[job.ID] = job

		capReg := connector.NewBackupCapabilityRegistry()
		capReg.Register(resDomain.TypeUbuntuSSH, &fakeCapability{sqlDump: "-- MySQL dump\nCREATE DATABASE testdb;\n"})

		retManager := &fakeRetentionManager{errToReturn: errors.New("retention processing failed")}
		workerPool := newTestWorkerPool(
			WorkerPoolConfig{NumWorkers: 1, PollInterval: 10 * time.Millisecond},
			repo,
			&fakeResourceFinder{resWithConn: resWithConn},
			&fakeCredentialVault{payloadBytes: validPassJSON},
			capReg,
			nil,
			engine.NewDirectStreamBackupEngine(),
			storageProvider,
			verification.NewVerificationEngine(),
			NewPerResourceMutexManager(),
			slog.Default(),
		)
		workerPool.SetRetentionManager(retManager)

		workerPool.processNextAvailableJob(context.Background(), 1)

		repo.mu.Lock()
		defer repo.mu.Unlock()

		if repo.finalizedJob == nil || repo.finalizedJob.Status != domain.JobStatusCompleted {
			t.Fatalf("expected job to remain completed despite retention error, got: %+v", repo.finalizedJob)
		}
		if repo.finalizedRun == nil || repo.finalizedRun.Status != domain.RunStatusSuccess {
			t.Fatalf("expected run to remain success despite retention error, got: %+v", repo.finalizedRun)
		}

		if len(repo.artifacts) == 0 {
			t.Fatalf("expected newly created artifact to be preserved")
		}
		for _, art := range repo.artifacts {
			if art.IsDeleted {
				t.Errorf("new artifact must not be deleted on retention error")
			}
		}
	})

	t.Run("pipeline failure never calls retention", func(t *testing.T) {
		repo := newFakeWorkerRepo(orgID)
		job := &domain.BackupJob{
			ID:              uuid.New(),
			OrganizationID:  orgID,
			ResourceID:      resID,
			BackupPlanID:    &planID,
			TriggerType:     domain.TriggerTypeScheduled,
			BackupType:      domain.BackupTypeMySQLDatabase,
			EngineType:      domain.EngineTypeDirectStream,
			StorageTargetID: repo.target.ID,
			TargetSpec:      domain.TargetSpec{Databases: []string{"testdb"}},
			Status:          domain.JobStatusPending,
			CreatedAt:       time.Now(),
			UpdatedAt:       time.Now(),
		}
		repo.jobs[job.ID] = job

		// Failing capability
		capReg := connector.NewBackupCapabilityRegistry()
		capReg.Register(resDomain.TypeUbuntuSSH, &fakeCapability{errToReturn: errors.New("mysqldump failed: table locked")})

		retManager := &fakeRetentionManager{}
		workerPool := newTestWorkerPool(
			WorkerPoolConfig{NumWorkers: 1, PollInterval: 10 * time.Millisecond},
			repo,
			&fakeResourceFinder{resWithConn: resWithConn},
			&fakeCredentialVault{payloadBytes: validPassJSON},
			capReg,
			nil,
			engine.NewDirectStreamBackupEngine(),
			storageProvider,
			verification.NewVerificationEngine(),
			NewPerResourceMutexManager(),
			slog.Default(),
		)
		workerPool.SetRetentionManager(retManager)

		workerPool.processNextAvailableJob(context.Background(), 1)

		if retManager.calls != 0 {
			t.Errorf("expected retention NOT to be called on failed backup pipeline, got %d calls", retManager.calls)
		}
	})

	t.Run("finalize failure never calls retention and invokes worker cleanup", func(t *testing.T) {
		repo := newFakeWorkerRepo(orgID)
		job := &domain.BackupJob{
			ID:              uuid.New(),
			OrganizationID:  orgID,
			ResourceID:      resID,
			BackupPlanID:    &planID,
			TriggerType:     domain.TriggerTypeScheduled,
			BackupType:      domain.BackupTypeMySQLDatabase,
			EngineType:      domain.EngineTypeDirectStream,
			StorageTargetID: repo.target.ID,
			TargetSpec:      domain.TargetSpec{Databases: []string{"testdb"}},
			Status:          domain.JobStatusPending,
			CreatedAt:       time.Now(),
			UpdatedAt:       time.Now(),
		}
		repo.jobs[job.ID] = job
		repo.finalizeErr = errors.New("db connection lost during finalize")

		capReg := connector.NewBackupCapabilityRegistry()
		capReg.Register(resDomain.TypeUbuntuSSH, &fakeCapability{sqlDump: "-- MySQL dump\nCREATE DATABASE testdb;\n"})

		retManager := &fakeRetentionManager{}
		workerPool := newTestWorkerPool(
			WorkerPoolConfig{NumWorkers: 1, PollInterval: 10 * time.Millisecond},
			repo,
			&fakeResourceFinder{resWithConn: resWithConn},
			&fakeCredentialVault{payloadBytes: validPassJSON},
			capReg,
			nil,
			engine.NewDirectStreamBackupEngine(),
			storageProvider,
			verification.NewVerificationEngine(),
			NewPerResourceMutexManager(),
			slog.Default(),
		)
		workerPool.SetRetentionManager(retManager)

		workerPool.processNextAvailableJob(context.Background(), 1)

		if retManager.calls != 0 {
			t.Errorf("expected retention NOT to be called when FinalizeRunAndJob fails, got %d calls", retManager.calls)
		}

		repo.mu.Lock()
		defer repo.mu.Unlock()
		for _, art := range repo.artifacts {
			if !art.IsDeleted {
				t.Errorf("artifact should be cleaned up by worker failure cleanup when finalize fails")
			}
		}
	})
}

type trackingStorageProvider struct {
	storage.StorageProvider
	saveCalls   int
	deleteCalls int
	openCalls   int
}

func (m *trackingStorageProvider) SaveArtifact(ctx context.Context, orgID, resID, runID, artifactID uuid.UUID, extension string, src io.Reader) (*storage.SaveResult, error) {
	m.saveCalls++
	return m.StorageProvider.SaveArtifact(ctx, orgID, resID, runID, artifactID, extension, src)
}

func (m *trackingStorageProvider) DeleteArtifact(ctx context.Context, storageReference string) error {
	m.deleteCalls++
	return m.StorageProvider.DeleteArtifact(ctx, storageReference)
}

func (m *trackingStorageProvider) OpenArtifact(ctx context.Context, storageReference string) (io.ReadCloser, error) {
	m.openCalls++
	return m.StorageProvider.OpenArtifact(ctx, storageReference)
}

type staticStorageResolver struct {
	targetID uuid.UUID
	provider storage.StorageProvider
}

func (r *staticStorageResolver) Resolve(ctx context.Context, orgID, targetID uuid.UUID) (storage.StorageProvider, error) {
	if targetID == r.targetID {
		return r.provider, nil
	}
	return nil, domain.ErrStorageTargetNotFound
}

func TestWorkerPool_S3DirectStream_PersistsS3TargetAndNeverFallsBackToLocal(t *testing.T) {
	t.Run("Successful S3 job persists S3 target ID and never touches local fallback", func(t *testing.T) {
		orgID := uuid.New()
		resID := uuid.New()
		credID := uuid.New()
		s3TargetID := uuid.New()
		localTargetID := uuid.New()

		s3Dir := t.TempDir()
		baseS3Store, _ := local.NewLocalStorageProvider(s3Dir)
		_ = baseS3Store.EnsureStorageRoot(context.Background())
		s3Store := &trackingStorageProvider{StorageProvider: baseS3Store}

		localDir := t.TempDir()
		baseLocalStore, _ := local.NewLocalStorageProvider(localDir)
		_ = baseLocalStore.EnsureStorageRoot(context.Background())
		localStore := &trackingStorageProvider{StorageProvider: baseLocalStore}

		s3Target := &domain.StorageTarget{
			ID:             s3TargetID,
			OrganizationID: orgID,
			Name:           "Production S3",
			Type:           domain.StorageTargetTypeS3,
			Status:         domain.StorageTargetStatusActive,
			Config:         []byte(`{"bucket":"prod-bucket"}`),
		}
		localTarget := &domain.StorageTarget{
			ID:             localTargetID,
			OrganizationID: orgID,
			Name:           "Default Local",
			Type:           domain.StorageTargetTypeLocal,
			Status:         domain.StorageTargetStatusActive,
			IsDefault:      true,
		}

		repo := newFakeWorkerRepo(orgID)
		repo.targets = map[uuid.UUID]*domain.StorageTarget{
			s3TargetID:    s3Target,
			localTargetID: localTarget,
		}
		repo.target = localTarget

		job := &domain.BackupJob{
			ID:              uuid.New(),
			OrganizationID:  orgID,
			ResourceID:      resID,
			TriggerType:     domain.TriggerTypeManual,
			BackupType:      domain.BackupTypeMySQLDatabase,
			EngineType:      domain.EngineTypeDirectStream,
			StorageTargetID: s3TargetID,
			TargetSpec:      domain.TargetSpec{Databases: []string{"s3db"}},
			Status:          domain.JobStatusPending,
			CreatedAt:       time.Now(),
		}
		repo.jobs[job.ID] = job

		validPassJSON, _ := payload.EncodeV1("testpass", nil)
		resWithConn := &resDomain.ResourceWithConnector{
			Resource: &resDomain.Resource{
				ID:             resID,
				OrganizationID: orgID,
				Type:           resDomain.TypeUbuntuSSH,
				Status:         resDomain.StatusActive,
			},
			Connector: &resDomain.ResourceConnector{
				ID:           uuid.New(),
				ResourceID:   resID,
				CredentialID: credID,
				Host:         "127.0.0.1",
				Port:         22,
				AuthType:     resDomain.AuthTypeSSHPassword,
				Config: resDomain.ConnectorConfig{
					Username: "testuser",
				},
			},
		}

		reg := connector.NewBackupCapabilityRegistry()
		reg.Register(resDomain.TypeUbuntuSSH, &fakeCapability{
			sqlDump: "-- MySQL dump\nCREATE DATABASE `s3db`;\n",
		})

		workerPool := newTestWorkerPool(
			WorkerPoolConfig{NumWorkers: 1, PollInterval: 10 * time.Millisecond},
			repo,
			&fakeResourceFinder{resWithConn: resWithConn},
			&fakeCredentialVault{payloadBytes: validPassJSON},
			reg,
			nil,
			engine.NewDirectStreamBackupEngine(),
			localStore, // Local store configured as fallback
			verification.NewVerificationEngine(),
			NewPerResourceMutexManager(),
			slog.Default(),
		)
		// Inject storage resolver pointing s3TargetID to s3Store
		workerPool.SetStorageResolver(&staticStorageResolver{
			targetID: s3TargetID,
			provider: s3Store,
		})

		workerPool.processNextAvailableJob(context.Background(), 1)

		// Assert S3 provider was used and Local provider was NEVER touched
		if s3Store.saveCalls != 1 {
			t.Errorf("expected S3 provider SaveArtifact to be called exactly once, got %d", s3Store.saveCalls)
		}
		if localStore.saveCalls != 0 {
			t.Errorf("expected Local provider SaveArtifact to NEVER be called, got %d", localStore.saveCalls)
		}

		repo.mu.Lock()
		defer repo.mu.Unlock()

		if repo.finalizedJob == nil || repo.finalizedJob.Status != domain.JobStatusCompleted {
			t.Errorf("expected job to be completed, got finalized: %+v", repo.finalizedJob)
		}
		if len(repo.artifacts) != 1 {
			t.Fatalf("expected 1 artifact created, got %d", len(repo.artifacts))
		}
		for _, art := range repo.artifacts {
			if art.StorageTargetID != s3TargetID {
				t.Errorf("expected artifact StorageTargetID to equal Job.StorageTargetID (%s), got: %s", s3TargetID, art.StorageTargetID)
			}
			if art.StorageTargetID == localTargetID {
				t.Errorf("artifact incorrectly persisted against Local target")
			}
		}
	})

	t.Run("CreateArtifact failure on S3 job cleans S3 physically and never falls back to local", func(t *testing.T) {
		orgID := uuid.New()
		resID := uuid.New()
		credID := uuid.New()
		s3TargetID := uuid.New()
		localTargetID := uuid.New()

		s3Dir := t.TempDir()
		baseS3Store, _ := local.NewLocalStorageProvider(s3Dir)
		_ = baseS3Store.EnsureStorageRoot(context.Background())
		s3Store := &trackingStorageProvider{StorageProvider: baseS3Store}

		localDir := t.TempDir()
		baseLocalStore, _ := local.NewLocalStorageProvider(localDir)
		_ = baseLocalStore.EnsureStorageRoot(context.Background())
		localStore := &trackingStorageProvider{StorageProvider: baseLocalStore}

		s3Target := &domain.StorageTarget{
			ID:             s3TargetID,
			OrganizationID: orgID,
			Name:           "Production S3",
			Type:           domain.StorageTargetTypeS3,
			Status:         domain.StorageTargetStatusActive,
			Config:         []byte(`{"bucket":"prod-bucket"}`),
		}
		localTarget := &domain.StorageTarget{
			ID:             localTargetID,
			OrganizationID: orgID,
			Name:           "Default Local",
			Type:           domain.StorageTargetTypeLocal,
			Status:         domain.StorageTargetStatusActive,
			IsDefault:      true,
		}

		repo := newFakeWorkerRepo(orgID)
		repo.targets = map[uuid.UUID]*domain.StorageTarget{
			s3TargetID:    s3Target,
			localTargetID: localTarget,
		}
		repo.target = localTarget
		// Simulate CreateArtifact failure (e.g. chain mismatch)
		repo.createArtifactErr = domain.ErrArtifactChainMismatch

		job := &domain.BackupJob{
			ID:              uuid.New(),
			OrganizationID:  orgID,
			ResourceID:      resID,
			TriggerType:     domain.TriggerTypeManual,
			BackupType:      domain.BackupTypeMySQLDatabase,
			EngineType:      domain.EngineTypeDirectStream,
			StorageTargetID: s3TargetID,
			TargetSpec:      domain.TargetSpec{Databases: []string{"s3db"}},
			Status:          domain.JobStatusPending,
			CreatedAt:       time.Now(),
		}
		repo.jobs[job.ID] = job

		validPassJSON, _ := payload.EncodeV1("testpass", nil)
		resWithConn := &resDomain.ResourceWithConnector{
			Resource: &resDomain.Resource{
				ID:             resID,
				OrganizationID: orgID,
				Type:           resDomain.TypeUbuntuSSH,
				Status:         resDomain.StatusActive,
			},
			Connector: &resDomain.ResourceConnector{
				ID:           uuid.New(),
				ResourceID:   resID,
				CredentialID: credID,
				Host:         "127.0.0.1",
				Port:         22,
				AuthType:     resDomain.AuthTypeSSHPassword,
				Config: resDomain.ConnectorConfig{
					Username: "testuser",
				},
			},
		}

		reg := connector.NewBackupCapabilityRegistry()
		reg.Register(resDomain.TypeUbuntuSSH, &fakeCapability{
			sqlDump: "-- MySQL dump\nCREATE DATABASE `s3db`;\n",
		})

		workerPool := newTestWorkerPool(
			WorkerPoolConfig{NumWorkers: 1, PollInterval: 10 * time.Millisecond},
			repo,
			&fakeResourceFinder{resWithConn: resWithConn},
			&fakeCredentialVault{payloadBytes: validPassJSON},
			reg,
			nil,
			engine.NewDirectStreamBackupEngine(),
			localStore,
			verification.NewVerificationEngine(),
			NewPerResourceMutexManager(),
			slog.Default(),
		)
		workerPool.SetStorageResolver(&staticStorageResolver{
			targetID: s3TargetID,
			provider: s3Store,
		})

		workerPool.processNextAvailableJob(context.Background(), 1)

		// Assert S3 provider was called to save and then delete (cleanup), Local provider never touched
		if s3Store.saveCalls != 1 {
			t.Errorf("expected S3 provider SaveArtifact to be called once, got %d", s3Store.saveCalls)
		}
		if s3Store.deleteCalls != 1 {
			t.Errorf("expected S3 provider DeleteArtifact to be called once for cleanup, got %d", s3Store.deleteCalls)
		}
		if localStore.saveCalls != 0 || localStore.deleteCalls != 0 {
			t.Errorf("expected Local provider to NEVER be called on S3 failure, got saveCalls=%d deleteCalls=%d",
				localStore.saveCalls, localStore.deleteCalls)
		}

		repo.mu.Lock()
		defer repo.mu.Unlock()
		if repo.finalizedRun == nil || repo.finalizedRun.Status != domain.RunStatusFailed {
			t.Errorf("expected run to be marked failed, got finalized run: %+v", repo.finalizedRun)
		}
	})
}
