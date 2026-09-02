package worker_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"backup-platform/internal/backup/domain"
	backupEngine "backup-platform/internal/backup/engine"
	backupHttpapi "backup-platform/internal/backup/httpapi"
	backupService "backup-platform/internal/backup/service"
	backupVerification "backup-platform/internal/backup/verification"
	backupWorker "backup-platform/internal/backup/worker"
	"backup-platform/internal/connector"
	"backup-platform/internal/connector/sshconn"
	credDomain "backup-platform/internal/credential/domain"
	"backup-platform/internal/credential/payload"
	orgDomain "backup-platform/internal/organization/domain"
	orgHttpapi "backup-platform/internal/organization/httpapi"
	resDomain "backup-platform/internal/resource/domain"
	"backup-platform/internal/storage/local"
	"backup-platform/pkg/uuid"
)

func startMockSSHServer(t *testing.T, user, pass string, handlers map[string]func(ch ssh.Channel, req *ssh.Request)) (int, string, func()) {
	t.Helper()
	serverKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed generating rsa key: %v", err)
	}

	signer, err := ssh.NewSignerFromKey(serverKey)
	if err != nil {
		t.Fatalf("failed creating signer: %v", err)
	}

	fingerprint := ssh.FingerprintSHA256(signer.PublicKey())

	config := &ssh.ServerConfig{
		PasswordCallback: func(c ssh.ConnMetadata, p []byte) (*ssh.Permissions, error) {
			if c.User() == user && string(p) == pass {
				return nil, nil
			}
			return nil, ssh.ErrNoAuth
		},
	}
	config.AddHostKey(signer)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed listening: %v", err)
	}

	_, cancel := context.WithCancel(context.Background())

	go func() {
		for {
			tcpConn, err := listener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				sConn, chans, reqs, err := ssh.NewServerConn(c, config)
				if err != nil {
					return
				}
				defer sConn.Close()
				go ssh.DiscardRequests(reqs)

				for newChannel := range chans {
					if newChannel.ChannelType() != "session" {
						_ = newChannel.Reject(ssh.UnknownChannelType, "unknown channel")
						continue
					}
					ch, requests, err := newChannel.Accept()
					if err != nil {
						return
					}
					go func(ch ssh.Channel, in <-chan *ssh.Request) {
						defer ch.Close()
						for req := range in {
							if req.Type == "exec" {
								if len(req.Payload) >= 4 {
									cmdLen := int(req.Payload[0])<<24 | int(req.Payload[1])<<16 | int(req.Payload[2])<<8 | int(req.Payload[3])
									if len(req.Payload) >= 4+cmdLen {
										cmd := string(req.Payload[4 : 4+cmdLen])
										if handler, ok := handlers[cmd]; ok {
											handler(ch, req)
										} else {
											_ = req.Reply(true, nil)
											_, _ = ch.SendRequest("exit-status", false, []byte{0, 0, 0, 127})
										}
									}
								}
								return
							}
							_ = req.Reply(false, nil)
						}
					}(ch, requests)
				}
			}(tcpConn)
		}
	}()

	cleanup := func() {
		cancel()
		_ = listener.Close()
	}

	_, portStr, _ := net.SplitHostPort(listener.Addr().String())
	port, _ := strconv.Atoi(portStr)

	return port, fingerprint, cleanup
}

type memoryBackupRepo struct {
	mu           sync.Mutex
	jobs         map[uuid.UUID]*domain.BackupJob
	runs         map[uuid.UUID]*domain.BackupRun
	artifacts    map[uuid.UUID]*domain.BackupArtifact
	target       *domain.StorageTarget
	finalizedRun *domain.BackupRun
	finalizedJob *domain.BackupJob
}

func newMemoryBackupRepo(orgID uuid.UUID) *memoryBackupRepo {
	return &memoryBackupRepo{
		jobs:      make(map[uuid.UUID]*domain.BackupJob),
		runs:      make(map[uuid.UUID]*domain.BackupRun),
		artifacts: make(map[uuid.UUID]*domain.BackupArtifact),
		target: &domain.StorageTarget{
			ID:             uuid.New(),
			OrganizationID: orgID,
			Name:           "Default Local Storage",
			Type:           domain.StorageTargetTypeLocal,
			Status:         domain.StorageTargetStatusActive,
			IsDefault:      true,
		},
	}
}

func (r *memoryBackupRepo) EnsureDefaultLocalStorageTarget(ctx context.Context, orgID uuid.UUID) (*domain.StorageTarget, error) {
	return r.target, nil
}
func (r *memoryBackupRepo) GetStorageTargetByID(ctx context.Context, orgID, targetID uuid.UUID) (*domain.StorageTarget, error) {
	return r.target, nil
}
func (r *memoryBackupRepo) GetPlanByID(ctx context.Context, orgID, planID uuid.UUID) (*domain.BackupPlan, error) {
	return nil, nil
}
func (r *memoryBackupRepo) CreateJob(ctx context.Context, job *domain.BackupJob) (*domain.BackupJob, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.jobs[job.ID] = job
	return job, nil
}
func (r *memoryBackupRepo) GetJobByID(ctx context.Context, orgID, jobID uuid.UUID) (*domain.BackupJob, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.jobs[jobID], nil
}
func (r *memoryBackupRepo) GetActiveManualJobForResource(ctx context.Context, orgID, resourceID uuid.UUID) (*domain.BackupJob, error) {
	return nil, nil
}
func (r *memoryBackupRepo) GetActiveJobConflictForResource(ctx context.Context, orgID, resourceID uuid.UUID) (*domain.BackupJob, error) {
	return nil, nil
}
func (r *memoryBackupRepo) FindPendingJobs(ctx context.Context, limit int, afterCreatedAt *time.Time, afterID *uuid.UUID) ([]*domain.BackupJob, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var pending []*domain.BackupJob
	for _, j := range r.jobs {
		if j.Status == domain.JobStatusPending {
			pending = append(pending, j)
		}
	}
	return pending, nil
}
func (r *memoryBackupRepo) TransactionalClaimJob(ctx context.Context, orgID, jobID uuid.UUID) (*domain.BackupRun, *domain.BackupJob, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	j, ok := r.jobs[jobID]
	if !ok || j.Status != domain.JobStatusPending {
		return nil, nil, errors.New("not pending")
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
func (r *memoryBackupRepo) GetRunByID(ctx context.Context, orgID, runID uuid.UUID) (*domain.BackupRun, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.runs[runID], nil
}
func (r *memoryBackupRepo) GetLatestRunForJob(ctx context.Context, orgID, jobID uuid.UUID) (*domain.BackupRun, error) {
	return nil, domain.ErrRunNotFound
}
func (r *memoryBackupRepo) UpdateHeartbeat(ctx context.Context, orgID, runID uuid.UUID) error {
	return nil
}
func (r *memoryBackupRepo) FinalizeRunAndJob(ctx context.Context, orgID, runID, jobID uuid.UUID, runStatus domain.RunStatus, jobStatus domain.JobStatus, errMsg *string, logsSummary []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
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
func (r *memoryBackupRepo) CreateArtifact(ctx context.Context, artifact *domain.BackupArtifact) (*domain.BackupArtifact, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.artifacts[artifact.ID] = artifact
	return artifact, nil
}
func (r *memoryBackupRepo) UpdateArtifactVerification(ctx context.Context, orgID, artifactID uuid.UUID, status domain.VerificationStatus, details *string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if art, ok := r.artifacts[artifactID]; ok {
		art.VerificationStatus = status
		art.VerificationDetails = details
	}
	return nil
}
func (r *memoryBackupRepo) TombstoneArtifact(ctx context.Context, orgID, artifactID uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if art, ok := r.artifacts[artifactID]; ok {
		art.IsDeleted = true
	}
	return nil
}
func (r *memoryBackupRepo) GetRunArtifacts(ctx context.Context, orgID, runID uuid.UUID) ([]*domain.BackupArtifact, error) {
	return nil, nil
}
func (r *memoryBackupRepo) GetRunDetail(ctx context.Context, orgID, runID uuid.UUID) (*domain.BackupRunWithStats, error) {
	return nil, domain.ErrRunNotFound
}
func (r *memoryBackupRepo) ListRuns(ctx context.Context, orgID uuid.UUID, filter domain.RunFilter) ([]*domain.BackupRunWithStats, error) {
	return nil, nil
}
func (r *memoryBackupRepo) ListSuccessfulRunsForPlan(ctx context.Context, orgID, planID uuid.UUID) ([]*domain.BackupRun, error) {
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
	return res, nil
}
func (r *memoryBackupRepo) GetArtifactByID(ctx context.Context, orgID, artifactID uuid.UUID) (*domain.BackupArtifact, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if a, ok := r.artifacts[artifactID]; ok {
		return a, nil
	}
	return nil, domain.ErrArtifactNotFound
}
func (r *memoryBackupRepo) ListArtifacts(ctx context.Context, orgID uuid.UUID) ([]*domain.BackupArtifact, error) {
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
func (r *memoryBackupRepo) RecoverInterruptedRuns(ctx context.Context) ([]domain.RecoveredRunInfo, error) {
	return nil, nil
}
func (r *memoryBackupRepo) ReapStaleRuns(ctx context.Context) ([]domain.RecoveredRunInfo, error) {
	return nil, nil
}
func (r *memoryBackupRepo) CreateStorageTarget(ctx context.Context, target *domain.StorageTarget) (*domain.StorageTarget, error) {
	return nil, nil
}
func (r *memoryBackupRepo) ListStorageTargets(ctx context.Context, orgID uuid.UUID) ([]*domain.StorageTarget, error) {
	return nil, nil
}
func (r *memoryBackupRepo) UpdateStorageTarget(ctx context.Context, target *domain.StorageTarget) (*domain.StorageTarget, error) {
	return nil, nil
}
func (r *memoryBackupRepo) DeleteStorageTarget(ctx context.Context, orgID, targetID uuid.UUID) error {
	return nil
}
func (r *memoryBackupRepo) CountArtifactsByStorageTarget(ctx context.Context, orgID, targetID uuid.UUID) (int64, error) {
	return 0, nil
}
func (r *memoryBackupRepo) CountPlansByStorageTarget(ctx context.Context, orgID, targetID uuid.UUID) (int64, error) {
	return 0, nil
}
func (r *memoryBackupRepo) CountActiveJobsByStorageTarget(ctx context.Context, orgID, targetID uuid.UUID) (int64, error) {
	return 0, nil
}

type memoryResourceFinder struct {
	resWithConn *resDomain.ResourceWithConnector
}

func (f *memoryResourceFinder) GetByID(ctx context.Context, orgID, resourceID uuid.UUID) (*resDomain.Resource, error) {
	return f.resWithConn.Resource, nil
}
func (f *memoryResourceFinder) FindByIDForOrganization(ctx context.Context, orgID, resID uuid.UUID) (*resDomain.ResourceWithConnector, error) {
	return f.resWithConn, nil
}

type memoryCredentialVault struct {
	payloadBytes []byte
}

func (f *memoryCredentialVault) LoadCredentialForUse(ctx context.Context, orgID, credID uuid.UUID) (credDomain.Type, []byte, error) {
	buf := make([]byte, len(f.payloadBytes))
	copy(buf, f.payloadBytes)
	return credDomain.TypeSSHPassword, buf, nil
}

func TestPhase5_CompleteVerticalSlice_EndToEnd(t *testing.T) {
	orgID := uuid.New()
	userID := uuid.New()
	resID := uuid.New()
	credID := uuid.New()

	sshUser, sshPass := "ubuntu", "secret_ssh_password"
	rawMySQLDump := "-- MySQL dump 10.13  Distrib 8.0.32\nCREATE DATABASE `ecommerce_prod`;\nUSE `ecommerce_prod`;\nCREATE TABLE `orders` (id INT);\nINSERT INTO `orders` VALUES (1), (2), (3);\n"

	sshHandlers := map[string]func(ch ssh.Channel, req *ssh.Request){
		"mysqldump --single-transaction --quick --routines --triggers --hex-blob -- 'ecommerce_prod'": func(ch ssh.Channel, req *ssh.Request) {
			_ = req.Reply(true, nil)
			_, _ = ch.Write([]byte(rawMySQLDump))
			_, _ = ch.SendRequest("exit-status", false, []byte{0, 0, 0, 0})
		},
	}

	port, fingerprint, cleanupSSH := startMockSSHServer(t, sshUser, sshPass, sshHandlers)
	defer cleanupSSH()

	tempStorageDir := t.TempDir()
	storageProvider, err := local.NewLocalStorageProvider(tempStorageDir)
	if err != nil {
		t.Fatalf("failed initializing storage provider: %v", err)
	}
	_ = storageProvider.EnsureStorageRoot(context.Background())

	timeout := 10
	resWithConn := &resDomain.ResourceWithConnector{
		Resource: &resDomain.Resource{
			ID:             resID,
			OrganizationID: orgID,
			Name:           "Production Ubuntu Database",
			Type:           resDomain.TypeUbuntuSSH,
			Status:         resDomain.StatusActive,
			CreatedAt:      time.Now(),
			UpdatedAt:      time.Now(),
		},
		Connector: &resDomain.ResourceConnector{
			ID:                 uuid.New(),
			OrganizationID:     orgID,
			ResourceID:         resID,
			ConnectorType:      resDomain.ConnectorTypeUbuntuSSH,
			CredentialID:       credID,
			Host:               "127.0.0.1",
			Port:               port,
			AuthType:           resDomain.AuthTypeSSHPassword,
			HostKeyFingerprint: &fingerprint,
			Config: resDomain.ConnectorConfig{
				Username:                 sshUser,
				ConnectionTimeoutSeconds: &timeout,
			},
		},
	}

	repo := newMemoryBackupRepo(orgID)
	rf := &memoryResourceFinder{resWithConn: resWithConn}

	validPassJSON, _ := payload.EncodeV1(sshPass, nil)
	vault := &memoryCredentialVault{payloadBytes: validPassJSON}

	capabilityRegistry := connector.NewBackupCapabilityRegistry()
	capabilityRegistry.Register(resDomain.TypeUbuntuSSH, sshconn.NewSSHDatabaseBackupCapability(nil))

	directStreamEngine := backupEngine.NewDirectStreamBackupEngine()
	verificationEngine := backupVerification.NewVerificationEngine()
	mutexManager := backupWorker.NewPerResourceMutexManager()

	// 1. Initialize HTTP API & Service
	jobService := backupService.NewBackupJobService(repo, rf)
	httpHandler := backupHttpapi.NewHandler(jobService, nil, nil, nil, nil, nil)

	// 2. Perform HTTP Request: POST /api/v1/backup-jobs
	requestPayload := `{"resource_id":"` + resID.String() + `","backup_type":"mysql_database","target_spec":{"databases":["ecommerce_prod"]}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/backup-jobs", bytes.NewBufferString(requestPayload))
	req.Header.Set("Content-Type", "application/json")

	tenantCtx := &orgHttpapi.TenantContext{
		UserID:            userID,
		OrganizationID:    orgID,
		Role:              orgDomain.RoleAdmin,
		MembershipStatus:  orgDomain.MemberStatusActive,
		OrganizationName:  "Test Corp",
		OrganizationSlug:  "test-corp",
		IsDefaultInternal: false,
	}
	req = req.WithContext(orgHttpapi.WithTenantContext(req.Context(), tenantCtx))
	rec := httptest.NewRecorder()

	httpHandler.CreateBackupJob(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202 Accepted, got status %d: %s", rec.Code, rec.Body.String())
	}

	var resEnvelope struct {
		Data    backupHttpapi.BackupJobResponse `json:"data"`
		Message string                          `json:"message"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resEnvelope); err != nil {
		t.Fatalf("failed decoding API response: %v", err)
	}

	if resEnvelope.Data.Status != domain.JobStatusPending {
		t.Errorf("expected initial job status pending, got %s", resEnvelope.Data.Status)
	}
	jobID := resEnvelope.Data.ID

	// 3. Start Worker Pool to process the durable queue
	workerPool := backupWorker.NewWorkerPool(
		backupWorker.WorkerPoolConfig{NumWorkers: 1, PollInterval: 10 * time.Millisecond},
		repo,
		rf,
		vault,
		capabilityRegistry,
		nil,
		directStreamEngine,
		storageProvider,
		verificationEngine,
		mutexManager,
		nil,
	)

	workerCtx, cancelWorkers := context.WithCancel(context.Background())
	workerPool.Start(workerCtx)

	// 4. Await complete execution with deterministic polling
	deadline := time.Now().Add(5 * time.Second)
	completed := false
	for time.Now().Before(deadline) {
		repo.mu.Lock()
		j := repo.jobs[jobID]
		if j != nil && j.Status == domain.JobStatusCompleted {
			completed = true
			repo.mu.Unlock()
			break
		}
		repo.mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}

	_ = workerPool.Stop(context.Background())
	cancelWorkers()

	if !completed {
		t.Fatalf("timed out waiting for backup job to complete")
	}

	// 5. Assert End-to-End State Transitions
	repo.mu.Lock()
	defer repo.mu.Unlock()

	finalizedJob := repo.jobs[jobID]
	if finalizedJob.Status != domain.JobStatusCompleted {
		t.Fatalf("expected job to transition to completed, got: %s", finalizedJob.Status)
	}

	if repo.finalizedRun == nil || repo.finalizedRun.Status != domain.RunStatusSuccess {
		t.Fatalf("expected run to transition to success, got run: %+v", repo.finalizedRun)
	}

	if len(repo.artifacts) != 1 {
		t.Fatalf("expected exactly 1 artifact record, found %d", len(repo.artifacts))
	}

	var createdArtifact *domain.BackupArtifact
	for _, a := range repo.artifacts {
		createdArtifact = a
		break
	}

	if createdArtifact.VerificationStatus != domain.VerificationStatusVerified {
		t.Errorf("expected artifact verification_status verified, got: %s", createdArtifact.VerificationStatus)
	}
	if createdArtifact.SizeBytes <= 0 {
		t.Errorf("expected positive artifact size, got %d", createdArtifact.SizeBytes)
	}
	if createdArtifact.ChecksumHash == "" {
		t.Errorf("expected non-empty checksum hash")
	}

	// 6. Verify Physical File on Storage Provider
	rc, err := storageProvider.OpenArtifact(context.Background(), createdArtifact.StorageReference)
	if err != nil {
		t.Fatalf("failed opening physical artifact on disk: %v", err)
	}
	_ = rc.Close()
}

func TestE2E_WebsiteFilesBackupWorkflow(t *testing.T) {
	orgID := uuid.New()
	userID := uuid.New()
	resID := uuid.New()
	credID := uuid.New()

	sshUser := "deployer"
	sshPass := "secret-ssh-key-phrase"

	// Mock SSH server expecting tar command
	tarData := createTarArchive(map[string][]byte{
		"index.html":    []byte("<html><body>Hello World</body></html>"),
		"wp-config.php": []byte("<?php define('DB_NAME', 'prod'); ?>"),
	})

	sshHandlers := map[string]func(ch ssh.Channel, req *ssh.Request){
		"tar -C '/var/www/mywebsite' -cf - '--exclude=cache/*' -- .": func(ch ssh.Channel, req *ssh.Request) {
			_ = req.Reply(true, nil)
			_, _ = ch.Write(tarData)
			_, _ = ch.SendRequest("exit-status", false, []byte{0, 0, 0, 0})
		},
	}

	port, fingerprint, cleanupSSH := startMockSSHServer(t, sshUser, sshPass, sshHandlers)
	defer cleanupSSH()

	tempStorageDir := t.TempDir()
	storageProvider, err := local.NewLocalStorageProvider(tempStorageDir)
	if err != nil {
		t.Fatalf("failed initializing storage provider: %v", err)
	}
	_ = storageProvider.EnsureStorageRoot(context.Background())

	timeout := 10
	resWithConn := &resDomain.ResourceWithConnector{
		Resource: &resDomain.Resource{
			ID:             resID,
			OrganizationID: orgID,
			Name:           "Production Ubuntu Web Server",
			Type:           resDomain.TypeUbuntuSSH,
			Status:         resDomain.StatusActive,
			CreatedAt:      time.Now(),
			UpdatedAt:      time.Now(),
		},
		Connector: &resDomain.ResourceConnector{
			ID:                 uuid.New(),
			OrganizationID:     orgID,
			ResourceID:         resID,
			ConnectorType:      resDomain.ConnectorTypeUbuntuSSH,
			CredentialID:       credID,
			Host:               "127.0.0.1",
			Port:               port,
			AuthType:           resDomain.AuthTypeSSHPassword,
			HostKeyFingerprint: &fingerprint,
			Config: resDomain.ConnectorConfig{
				Username:                 sshUser,
				ConnectionTimeoutSeconds: &timeout,
			},
		},
	}

	repo := newMemoryBackupRepo(orgID)
	rf := &memoryResourceFinder{resWithConn: resWithConn}

	validPassJSON, _ := payload.EncodeV1(sshPass, nil)
	vault := &memoryCredentialVault{payloadBytes: validPassJSON}

	fileCapabilityRegistry := connector.NewFileBackupCapabilityRegistry()
	fileCapabilityRegistry.Register(resDomain.TypeUbuntuSSH, sshconn.NewSSHFileBackupCapability(nil))

	directStreamEngine := backupEngine.NewDirectStreamBackupEngine()
	verificationEngine := backupVerification.NewVerificationEngine()
	mutexManager := backupWorker.NewPerResourceMutexManager()

	// 1. Initialize HTTP API & Service
	jobService := backupService.NewBackupJobService(repo, rf)
	httpHandler := backupHttpapi.NewHandler(jobService, nil, nil, nil, nil, nil)

	// 2. Perform HTTP Request: POST /api/v1/backup-jobs for website files
	requestPayload := `{"resource_id":"` + resID.String() + `","backup_type":"website_files","target_spec":{"paths":["/var/www/mywebsite"],"exclude_patterns":["cache/*"]}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/backup-jobs", bytes.NewBufferString(requestPayload))
	req.Header.Set("Content-Type", "application/json")

	tenantCtx := &orgHttpapi.TenantContext{
		UserID:            userID,
		OrganizationID:    orgID,
		Role:              orgDomain.RoleAdmin,
		MembershipStatus:  orgDomain.MemberStatusActive,
		OrganizationName:  "Web Corp",
		OrganizationSlug:  "web-corp",
		IsDefaultInternal: false,
	}
	req = req.WithContext(orgHttpapi.WithTenantContext(req.Context(), tenantCtx))
	rec := httptest.NewRecorder()

	httpHandler.CreateBackupJob(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202 Accepted, got status %d: %s", rec.Code, rec.Body.String())
	}

	var resEnvelope struct {
		Data    backupHttpapi.BackupJobResponse `json:"data"`
		Message string                          `json:"message"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resEnvelope); err != nil {
		t.Fatalf("failed decoding API response: %v", err)
	}

	if resEnvelope.Data.Status != domain.JobStatusPending {
		t.Errorf("expected initial job status pending, got %s", resEnvelope.Data.Status)
	}
	jobID := resEnvelope.Data.ID

	// 3. Start Worker Pool to process the durable queue
	workerPool := backupWorker.NewWorkerPool(
		backupWorker.WorkerPoolConfig{NumWorkers: 1, PollInterval: 10 * time.Millisecond},
		repo,
		rf,
		vault,
		nil,
		fileCapabilityRegistry,
		directStreamEngine,
		storageProvider,
		verificationEngine,
		mutexManager,
		nil,
	)

	workerCtx, cancelWorkers := context.WithCancel(context.Background())
	workerPool.Start(workerCtx)

	// 4. Await complete execution with deterministic polling
	deadline := time.Now().Add(5 * time.Second)
	completed := false
	for time.Now().Before(deadline) {
		repo.mu.Lock()
		j := repo.jobs[jobID]
		if j != nil && j.Status == domain.JobStatusCompleted {
			completed = true
			repo.mu.Unlock()
			break
		}
		repo.mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}

	_ = workerPool.Stop(context.Background())
	cancelWorkers()

	if !completed {
		t.Fatalf("timed out waiting for backup job to complete")
	}

	// 5. Assert End-to-End State Transitions
	repo.mu.Lock()
	defer repo.mu.Unlock()

	finalizedJob := repo.jobs[jobID]
	if finalizedJob.Status != domain.JobStatusCompleted {
		t.Fatalf("expected job to transition to completed, got: %s", finalizedJob.Status)
	}

	if repo.finalizedRun == nil || repo.finalizedRun.Status != domain.RunStatusSuccess {
		t.Fatalf("expected run to transition to success, got run: %+v", repo.finalizedRun)
	}

	if len(repo.artifacts) != 1 {
		t.Fatalf("expected exactly 1 artifact record, found %d", len(repo.artifacts))
	}

	var createdArtifact *domain.BackupArtifact
	for _, a := range repo.artifacts {
		createdArtifact = a
		break
	}

	if createdArtifact.ArtifactType != domain.ArtifactTypeFilesArchive {
		t.Errorf("expected ArtifactType files_archive, got: %s", createdArtifact.ArtifactType)
	}
	if createdArtifact.Format != domain.ArtifactFormatTarGzip {
		t.Errorf("expected Format tar_gzip, got: %s", createdArtifact.Format)
	}
	if createdArtifact.TargetName != "/var/www/mywebsite" {
		t.Errorf("expected TargetName /var/www/mywebsite, got: %s", createdArtifact.TargetName)
	}
	if createdArtifact.VerificationStatus != domain.VerificationStatusVerified {
		t.Errorf("expected artifact verification_status verified, got: %s", createdArtifact.VerificationStatus)
	}
	if createdArtifact.SizeBytes <= 0 {
		t.Errorf("expected positive artifact size, got %d", createdArtifact.SizeBytes)
	}
	if createdArtifact.ChecksumHash == "" {
		t.Errorf("expected non-empty checksum hash")
	}
	if !strings.HasSuffix(createdArtifact.StorageReference, ".tar.gz") {
		t.Errorf("expected storage reference to end with .tar.gz, got: %s", createdArtifact.StorageReference)
	}

	// 6. Verify Physical File on Storage Provider and inspect decompressed content
	rc, err := storageProvider.OpenArtifact(context.Background(), createdArtifact.StorageReference)
	if err != nil {
		t.Fatalf("failed opening physical artifact on disk: %v", err)
	}
	defer rc.Close()

	gzr, err := gzip.NewReader(rc)
	if err != nil {
		t.Fatalf("failed creating gzip reader: %v", err)
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	foundMembers := make(map[string][]byte)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("failed reading tar entry: %v", err)
		}
		var content bytes.Buffer
		if _, err := io.Copy(&content, tr); err != nil {
			t.Fatalf("failed reading tar member content: %v", err)
		}
		foundMembers[hdr.Name] = content.Bytes()
	}

	if string(foundMembers["index.html"]) != "<html><body>Hello World</body></html>" {
		t.Errorf("unexpected index.html content: %q", string(foundMembers["index.html"]))
	}
	if string(foundMembers["wp-config.php"]) != "<?php define('DB_NAME', 'prod'); ?>" {
		t.Errorf("unexpected wp-config.php content: %q", string(foundMembers["wp-config.php"]))
	}
}

func createTarArchive(entries map[string][]byte) []byte {
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
