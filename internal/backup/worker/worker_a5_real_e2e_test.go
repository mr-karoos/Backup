package worker_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5"

	auditDomain "backup-platform/internal/audit/domain"
	auditRepo "backup-platform/internal/audit/repository"
	auditService "backup-platform/internal/audit/service"
	"backup-platform/internal/backup/domain"
	"backup-platform/internal/backup/repository"
	"backup-platform/internal/backup/restic"
	"backup-platform/internal/backup/retention"
	"backup-platform/internal/backup/scheduler"
	"backup-platform/internal/backup/worker"
	credDomain "backup-platform/internal/credential/domain"
	credentialRepo "backup-platform/internal/credential/repository"
	"backup-platform/internal/credential/secretcrypto"
	credentialService "backup-platform/internal/credential/service"
	"backup-platform/internal/platform/database"
	"backup-platform/internal/platform/migrations"
	"backup-platform/pkg/uuid"
)

type dummyStorageProvider struct{}

func (d *dummyStorageProvider) DeleteArtifact(ctx context.Context, storageRef string) error {
	return nil
}

func TestWorkerPool_StepA5_RealE2E_RetentionAndMaintenance(t *testing.T) {
	testDBURL := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	if testDBURL == "" {
		t.Skip("skipping Step A.5 Real E2E test: TEST_DATABASE_URL not set")
	}

	resticBin := os.Getenv("TEST_RESTIC_BINARY")
	if resticBin == "" {
		if _, err := os.Stat(`C:\Users\Kroos\AppData\Local\Temp\restic-bin\restic.exe`); err == nil {
			resticBin = `C:\Users\Kroos\AppData\Local\Temp\restic-bin\restic.exe`
		} else if _, err := exec.LookPath("restic"); err == nil {
			resticBin = "restic"
		} else {
			t.Skip("skipping real restic test: restic binary not found")
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	conn, err := pgx.Connect(ctx, testDBURL)
	if err != nil {
		t.Fatalf("failed connecting to test db: %v", err)
	}
	defer func() { _ = conn.Close(ctx) }()

	var pgVersion string
	if err := conn.QueryRow(ctx, "SHOW server_version;").Scan(&pgVersion); err != nil {
		t.Fatalf("failed querying postgres version: %v", err)
	}
	t.Logf("PostgreSQL server_version: %s", pgVersion)

	d, err := iofs.New(migrations.FS, "sql")
	if err != nil {
		t.Fatalf("failed creating iofs driver: %v", err)
	}
	m, err := migrate.NewWithSourceInstance("iofs", d, testDBURL)
	if err != nil {
		t.Fatalf("failed creating migrate instance: %v", err)
	}
	defer func() { _, _ = m.Close() }()

	if err := m.Migrate(10); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		t.Fatalf("failed migrating to v10: %v", err)
	}

	pool, err := database.New(ctx, testDBURL)
	if err != nil {
		t.Fatalf("failed creating pool: %v", err)
	}
	defer pool.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	backupRepository := repository.NewPostgresBackupRepository(pool)
	credRepository := credentialRepo.NewPostgresCredentialRepository()
	masterKey := []byte("12345678901234567890123456789012")
	keyProvider, err := secretcrypto.NewStaticKeyProvider(masterKey, 1)
	if err != nil {
		t.Fatalf("failed creating static key provider: %v", err)
	}
	cryptoEngine, err := secretcrypto.NewAESGCMEngine(keyProvider)
	if err != nil {
		t.Fatalf("failed creating crypto engine: %v", err)
	}
	credVaultService := credentialService.NewVaultService(cryptoEngine, credRepository, pool, nil)
	auditRepository := auditRepo.NewPostgresAuditRepository(pool)
	auditRecorder := auditService.NewAuditService(auditRepository, logger)

	// Create temp directory for local repository
	tempDir, err := os.MkdirTemp("", "restic-a5-worker-e2e-*")
	if err != nil {
		t.Fatalf("failed creating temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	resticRunner := restic.NewResticRunner(resticBin, logger)
	resticCoordinator := restic.NewRepositoryOperationCoordinator()
	resticTargetResolver := restic.NewTargetResolver(
		backupRepository,
		credVaultService,
		tempDir,
		true,
		[]string{"localhost", "127.0.0.1"},
	)

	orgID := uuid.New()
	resID := uuid.New()
	targetID := uuid.New()
	credID := uuid.New()
	repoID := uuid.New()

	cleanup := func() {
		cleanupCtx := context.Background()
		_, _ = conn.Exec(cleanupCtx, "DELETE FROM audit_logs WHERE organization_id = $1", orgID)
		_, _ = conn.Exec(cleanupCtx, "DELETE FROM repository_maintenance_runs WHERE job_id IN (SELECT id FROM repository_maintenance_jobs WHERE organization_id = $1)", orgID)
		_, _ = conn.Exec(cleanupCtx, "DELETE FROM repository_maintenance_jobs WHERE organization_id = $1", orgID)
		_, _ = conn.Exec(cleanupCtx, "DELETE FROM backup_artifacts WHERE organization_id = $1", orgID)
		_, _ = conn.Exec(cleanupCtx, "DELETE FROM backup_runs WHERE organization_id = $1", orgID)
		_, _ = conn.Exec(cleanupCtx, "DELETE FROM backup_jobs WHERE organization_id = $1", orgID)
		_, _ = conn.Exec(cleanupCtx, "DELETE FROM backup_plans WHERE organization_id = $1", orgID)
		_, _ = conn.Exec(cleanupCtx, "DELETE FROM backup_repositories WHERE organization_id = $1", orgID)
		_, _ = conn.Exec(cleanupCtx, "DELETE FROM credentials WHERE organization_id = $1", orgID)
		_, _ = conn.Exec(cleanupCtx, "DELETE FROM storage_targets WHERE organization_id = $1", orgID)
		_, _ = conn.Exec(cleanupCtx, "DELETE FROM resources WHERE organization_id = $1", orgID)
		_, _ = conn.Exec(cleanupCtx, "DELETE FROM organizations WHERE id = $1", orgID)
	}
	defer cleanup()

	// Seed tenant environment
	slug := fmt.Sprintf("org-a5-worker-%d", time.Now().UnixNano())
	if _, err := conn.Exec(ctx, `INSERT INTO organizations (id, name, slug) VALUES ($1, 'Org A5 Worker', $2);`, orgID, slug); err != nil {
		t.Fatalf("failed seeding org: %v", err)
	}
	repoPass := "restic-test-worker-pw-a5"
	credMeta, err := credVaultService.CreateSystemCredential(ctx, orgID, "Restic Key", credDomain.TypeResticRepositoryKey, []byte(repoPass))
	if err != nil {
		t.Fatalf("failed creating restic key: %v", err)
	}
	credID = credMeta.ID

	if _, err := conn.Exec(ctx, `INSERT INTO storage_targets (id, organization_id, name, type, config, status) VALUES ($1, $2, 'Target Local', 'local', $3::jsonb, 'active');`, targetID, orgID, fmt.Sprintf(`{"storage_path":"%s"}`, strings.ReplaceAll(tempDir, `\`, `\\`))); err != nil {
		t.Fatalf("failed seeding storage target: %v", err)
	}
	if _, err := conn.Exec(ctx, `INSERT INTO resources (id, organization_id, name, type, status) VALUES ($1, $2, 'Resource A5', 'ubuntu_ssh', 'active');`, resID, orgID); err != nil {
		t.Fatalf("failed seeding resource: %v", err)
	}

	storageTarget, err := backupRepository.GetStorageTargetByID(ctx, orgID, targetID)
	if err != nil {
		t.Fatalf("failed getting storage target: %v", err)
	}
	target, err := resticTargetResolver.ResolveTarget(ctx, orgID, resID, storageTarget)
	if err != nil {
		t.Fatalf("failed resolving target: %v", err)
	}
	defer target.Cleanup()
	repoPath := target.ResticRepositoryURL()
	repoLocator := target.Locator()

	if _, err := conn.Exec(ctx, `INSERT INTO backup_repositories (id, organization_id, resource_id, storage_target_id, credential_id, repository_locator, status) VALUES ($1, $2, $3, $4, $5, $6, 'active');`, repoID, orgID, resID, targetID, credID, repoLocator); err != nil {
		t.Fatalf("failed seeding repo: %v", err)
	}

	// 1. Initialize Restic repository
	if err := resticRunner.Init(ctx, target, []byte(repoPass)); err != nil {
		t.Fatalf("failed initializing repository: %v", err)
	}
	t.Log("Initialized local restic repository successfully")

	// 2. Perform a real restic backup of a test file
	testFile := filepath.Join(tempDir, "sample.txt")
	_ = os.WriteFile(testFile, []byte("Step A5 Real Worker E2E Payload"), 0600)
	cmd := exec.CommandContext(ctx, resticBin, "-r", repoPath, "backup", testFile)
	cmd.Env = append(os.Environ(), "RESTIC_PASSWORD="+repoPass)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed running backup: %v (output: %s)", err, string(out))
	}

	// 3. Find snapshot ID
	snapCmd := exec.CommandContext(ctx, resticBin, "-r", repoPath, "snapshots", "--json")
	snapCmd.Env = append(os.Environ(), "RESTIC_PASSWORD="+repoPass)
	out, err := snapCmd.Output()
	if err != nil {
		t.Fatalf("failed listing snapshots: %v", err)
	}
	var items []restic.SnapshotItem
	if err := json.Unmarshal(out, &items); err != nil {
		t.Fatalf("failed unmarshaling snapshot items: %v", err)
	}
	if len(items) == 0 {
		t.Fatalf("no snapshots found")
	}
	snapID := items[0].ID
	t.Logf("Created real snapshot: %s", snapID)

	// 4. Seed an expired run and artifact in PostgreSQL
	keepCount := 1
	planID := uuid.New()
	_, err = conn.Exec(ctx, `
		INSERT INTO backup_plans (id, organization_id, resource_id, storage_target_id, name, backup_type, target_spec, engine_type, schedule_cron, is_schedule_enabled, retention_count)
		VALUES ($1, $2, $3, $4, 'Plan Ret A5', 'mysql_database', '{"databases":["test"]}'::jsonb, 'restic', '0 2 * * *', true, $5);
	`, planID, orgID, resID, targetID, keepCount)
	if err != nil {
		t.Fatalf("failed seeding plan: %v", err)
	}

	jobOld := uuid.New()
	runOld := uuid.New()
	artOld := uuid.New()
	endedOld := time.Now().Add(-2 * time.Hour)
	_, err = conn.Exec(ctx, `
		INSERT INTO backup_jobs (id, organization_id, resource_id, backup_plan_id, trigger_type, backup_type, engine_type, storage_target_id, status, target_spec)
		VALUES ($1, $2, $3, $4, 'scheduled', 'mysql_database', 'restic', $5, 'running', '{"databases":["test"]}'::jsonb);
	`, jobOld, orgID, resID, planID, targetID)
	if err != nil {
		t.Fatalf("failed seeding old job: %v", err)
	}
	_, err = conn.Exec(ctx, `
		INSERT INTO backup_runs (id, organization_id, job_id, attempt_number, status, started_at)
		VALUES ($1, $2, $3, 1, 'running', NOW() - INTERVAL '3 hours');
	`, runOld, orgID, jobOld)
	if err != nil {
		t.Fatalf("failed seeding old run: %v", err)
	}

	logicalSize := int64(1024)
	_, err = backupRepository.CreateArtifact(ctx, &domain.BackupArtifact{
		ID:                 artOld,
		OrganizationID:     orgID,
		RunID:              runOld,
		ResourceID:         resID,
		StorageTargetID:    targetID,
		ArtifactType:       domain.ArtifactTypeDatabaseDump,
		Format:             domain.ArtifactFormatResticSnapshot,
		TargetName:         "db_dump",
		RepositoryID:       &repoID,
		SnapshotID:         snapID,
		LogicalSizeBytes:   &logicalSize,
		VerificationStatus: domain.VerificationStatusVerified,
	})
	if err != nil {
		t.Fatalf("failed creating artifact in db: %v", err)
	}

	_, err = conn.Exec(ctx, `UPDATE backup_jobs SET status = 'completed' WHERE id = $1;`, jobOld)
	if err != nil {
		t.Fatalf("failed completing old job: %v", err)
	}
	_, err = conn.Exec(ctx, `UPDATE backup_runs SET status = 'success', ended_at = $1 WHERE id = $2;`, endedOld, runOld)
	if err != nil {
		t.Fatalf("failed completing old run: %v", err)
	}

	// Seed current run (which stays kept)
	jobCurrent := uuid.New()
	runCurrent := uuid.New()
	endedCurrent := time.Now()
	_, err = conn.Exec(ctx, `
		INSERT INTO backup_jobs (id, organization_id, resource_id, backup_plan_id, trigger_type, backup_type, engine_type, storage_target_id, status, target_spec)
		VALUES ($1, $2, $3, $4, 'scheduled', 'mysql_database', 'restic', $5, 'completed', '{"databases":["test"]}'::jsonb);
	`, jobCurrent, orgID, resID, planID, targetID)
	if err != nil {
		t.Fatalf("failed seeding current job: %v", err)
	}
	_, err = conn.Exec(ctx, `
		INSERT INTO backup_runs (id, organization_id, job_id, attempt_number, status, started_at, ended_at)
		VALUES ($1, $2, $3, 1, 'success', NOW(), $4);
	`, runCurrent, orgID, jobCurrent, endedCurrent)
	if err != nil {
		t.Fatalf("failed seeding current run: %v", err)
	}

	// 5. Evaluate retention -> must enqueue restic_forget without deleting storage or tombstoning yet
	retentionProc := retention.NewProcessor(backupRepository, &dummyStorageProvider{}, auditRecorder, logger)
	retentionProc.SetMaintenanceEnqueuer(backupRepository)

	summary, err := retentionProc.ApplyAfterSuccessfulRun(ctx, orgID, &planID, runCurrent)
	if err != nil {
		t.Fatalf("retention evaluation failed: %v", err)
	}
	if summary.MaintenanceJobsQueued != 1 {
		t.Fatalf("expected 1 maintenance job queued for retention, got %d", summary.MaintenanceJobsQueued)
	}
	t.Log("Retention processor successfully enqueued restic_forget job")

	// Verify artifact is NOT yet tombstoned before maintenance worker runs
	artCheck, _ := backupRepository.GetArtifactByID(ctx, orgID, artOld)
	if artCheck.IsDeleted {
		t.Fatalf("artifact should not be tombstoned yet before maintenance worker executes forget")
	}

	// 6. Start RepositoryMaintenanceWorker and process the restic_forget job
	maintCfg := worker.DefaultMaintenanceWorkerConfig()
	maintWorker := worker.NewRepositoryMaintenanceWorker(
		backupRepository,
		backupRepository,
		backupRepository,
		credVaultService,
		resticTargetResolver,
		resticRunner,
		resticCoordinator,
		auditRecorder,
		maintCfg,
		logger,
	)

	// Process forget job
	processed := maintWorker.ProcessNextJob(ctx)
	if !processed {
		t.Fatalf("expected maintenance worker to process restic_forget job")
	}

	// Verify snapshot is now gone from real restic repository
	verifySnap := exec.CommandContext(ctx, resticBin, "-r", repoPath, "snapshots", "--json")
	verifySnap.Env = append(os.Environ(), "RESTIC_PASSWORD="+repoPass)
	vOut, _ := verifySnap.Output()
	if strings.Contains(string(vOut), snapID) {
		t.Fatalf("forgotten snapshot still in restic repo: %s", string(vOut))
	}
	t.Log("Verified snapshot removed from Restic repository via Forget")

	// Verify artifact is now tombstoned in PostgreSQL
	artCheck, _ = backupRepository.GetArtifactByID(ctx, orgID, artOld)
	if !artCheck.IsDeleted {
		t.Fatalf("artifact should now be tombstoned in database")
	}
	t.Log("Verified artifact tombstoned in PostgreSQL (is_deleted = true)")

	// Verify debounced prune was automatically queued
	maintJobs, err := backupRepository.ListMaintenanceJobs(ctx, orgID, repoID, 10)
	if err != nil {
		t.Fatalf("failed listing maintenance jobs: %v", err)
	}
	var foundPrune bool
	for _, j := range maintJobs {
		if j.OperationType == domain.MaintenanceOpResticPrune && (j.Status == domain.MaintenanceJobPending || j.Status == domain.MaintenanceJobRunning) {
			foundPrune = true
			break
		}
	}
	if !foundPrune {
		t.Fatalf("expected debounced restic_prune job to be queued in maintenance repository")
	}
	t.Log("Verified debounced restic_prune job queued after forget")

	// Process prune job
	processed = maintWorker.ProcessNextJob(ctx)
	if !processed {
		t.Fatalf("expected maintenance worker to process restic_prune job")
	}
	t.Log("Verified restic_prune processed successfully by maintenance worker")

	// 7. Test MaintenanceScheduler deterministic rotation & execution
	schedCfg := scheduler.MaintenanceSchedulerConfig{
		PollInterval:     10 * time.Minute,
		TotalSubsets:     4,
		DeepCheckEnabled: true,
	}
	maintSched := scheduler.NewMaintenanceScheduler(backupRepository, schedCfg, logger)

	// First deep check: should be subset 1/4
	checkJob1, err := maintSched.EnqueueNextDeepCheck(ctx, orgID, repoID)
	if err != nil {
		t.Fatalf("failed enqueuing deep check 1: %v", err)
	}
	if *checkJob1.SubsetIndex != 1 || *checkJob1.SubsetTotal != 4 {
		t.Fatalf("expected deep check 1/4, got %d/%d", *checkJob1.SubsetIndex, *checkJob1.SubsetTotal)
	}

	// Process deep check 1
	processed = maintWorker.ProcessNextJob(ctx)
	if !processed {
		t.Fatalf("expected maintenance worker to process deep check 1")
	}
	t.Log("Verified deep check 1/4 executed and completed by maintenance worker")

	// Next deep check: deterministic rotation should give 2/4!
	checkJob2, err := maintSched.EnqueueNextDeepCheck(ctx, orgID, repoID)
	if err != nil {
		t.Fatalf("failed enqueuing deep check 2: %v", err)
	}
	if *checkJob2.SubsetIndex != 2 || *checkJob2.SubsetTotal != 4 {
		t.Fatalf("expected deep check 2/4 from rotation, got %d/%d", *checkJob2.SubsetIndex, *checkJob2.SubsetTotal)
	}

	// Process deep check 2
	processed = maintWorker.ProcessNextJob(ctx)
	if !processed {
		t.Fatalf("expected maintenance worker to process deep check 2")
	}
	t.Log("Verified deep check 2/4 executed and completed by maintenance worker")

	// Verify audit logs were recorded
	rows, err := conn.Query(ctx, "SELECT action, entity_id FROM audit_logs WHERE organization_id = $1", orgID)
	if err != nil {
		t.Fatalf("failed querying audit logs: %v", err)
	}
	defer rows.Close()
	var foundAudit bool
	for rows.Next() {
		var action string
		var entID *uuid.UUID
		if err := rows.Scan(&action, &entID); err != nil {
			t.Fatalf("failed scanning audit row: %v", err)
		}
		if action == string(auditDomain.ActionRetentionCleanup) && entID != nil && *entID == artOld {
			foundAudit = true
			break
		}
	}
	if !foundAudit {
		t.Fatalf("expected audit log for retention cleanup of artOld")
	}
	t.Log("Verified retention cleanup audit log recorded in PostgreSQL")
}
