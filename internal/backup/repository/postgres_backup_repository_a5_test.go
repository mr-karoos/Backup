package repository

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5"

	"backup-platform/internal/backup/domain"
	"backup-platform/internal/platform/database"
	"backup-platform/internal/platform/migrations"
	"backup-platform/pkg/uuid"
)

func TestPostgresBackupRepository_StepA5_Integration(t *testing.T) {
	testDBURL := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	if testDBURL == "" {
		t.Skip("skipping Step A.5 repository integration test: TEST_DATABASE_URL not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	conn, err := pgx.Connect(ctx, testDBURL)
	if err != nil {
		t.Fatalf("failed connecting to test database: %v", err)
	}
	defer func() {
		_ = conn.Close(ctx)
	}()

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
		t.Fatalf("failed initializing migrate instance: %v", err)
	}
	defer func() {
		_, _ = m.Close()
	}()

	// Migrate through version 10 (Step A.5 maintenance queue & runs)
	if err := m.Migrate(10); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		t.Fatalf("failed migrating to version 10: %v", err)
	}

	pool, err := database.New(ctx, testDBURL)
	if err != nil {
		t.Fatalf("failed creating database pool: %v", err)
	}
	defer pool.Close()

	repo := NewPostgresBackupRepository(pool)

	orgID := uuid.New()
	resID := uuid.New()
	targetID := uuid.New()
	credID := uuid.New()
	repoID := uuid.New()
	cleanup := func() {
		cleanupCtx := context.Background()
		_, _ = conn.Exec(cleanupCtx, "DELETE FROM repository_maintenance_runs WHERE job_id IN (SELECT id FROM repository_maintenance_jobs WHERE organization_id = $1)", orgID)
		_, _ = conn.Exec(cleanupCtx, "DELETE FROM repository_maintenance_jobs WHERE organization_id = $1", orgID)
		_, _ = conn.Exec(cleanupCtx, "DELETE FROM backup_artifacts WHERE organization_id = $1", orgID)
		_, _ = conn.Exec(cleanupCtx, "DELETE FROM backup_runs WHERE organization_id = $1", orgID)
		_, _ = conn.Exec(cleanupCtx, "DELETE FROM backup_jobs WHERE organization_id = $1", orgID)
		_, _ = conn.Exec(cleanupCtx, "DELETE FROM backup_repositories WHERE organization_id = $1", orgID)
		_, _ = conn.Exec(cleanupCtx, "DELETE FROM credentials WHERE organization_id = $1", orgID)
		_, _ = conn.Exec(cleanupCtx, "DELETE FROM storage_targets WHERE organization_id = $1", orgID)
		_, _ = conn.Exec(cleanupCtx, "DELETE FROM resources WHERE organization_id = $1", orgID)
		_, _ = conn.Exec(cleanupCtx, "DELETE FROM organizations WHERE id = $1", orgID)
	}
	defer cleanup()

	// Seed tenant environment
	slug := fmt.Sprintf("org-a5-%d", time.Now().UnixNano())
	if _, err := conn.Exec(ctx, `INSERT INTO organizations (id, name, slug) VALUES ($1, 'Org A5 Test', $2);`, orgID, slug); err != nil {
		t.Fatalf("failed seeding organization: %v", err)
	}
	if _, err := conn.Exec(ctx, `INSERT INTO credentials (id, organization_id, name, type, managed_by, encrypted_secret, nonce, auth_tag, key_version, created_at, updated_at) VALUES ($1, $2, 'Restic Repo Key', 'restic_repository_key', 'system', '\x01', '\x02', '\x03', 1, NOW(), NOW());`, credID, orgID); err != nil {
		t.Fatalf("failed seeding credential: %v", err)
	}
	if _, err := conn.Exec(ctx, `INSERT INTO storage_targets (id, organization_id, name, type, config, status) VALUES ($1, $2, 'Target A5 Local', 'local', '{"storage_path":"/tmp/a5"}'::jsonb, 'active');`, targetID, orgID); err != nil {
		t.Fatalf("failed seeding storage target: %v", err)
	}
	if _, err := conn.Exec(ctx, `INSERT INTO resources (id, organization_id, name, type, status) VALUES ($1, $2, 'Resource A5', 'ubuntu_ssh', 'active');`, resID, orgID); err != nil {
		t.Fatalf("failed seeding resource: %v", err)
	}
	if _, err := conn.Exec(ctx, `INSERT INTO backup_repositories (id, organization_id, resource_id, storage_target_id, credential_id, repository_locator, status) VALUES ($1, $2, $3, $4, $5, '/tmp/a5-repo', 'active');`, repoID, orgID, resID, targetID, credID); err != nil {
		t.Fatalf("failed seeding backup repository: %v", err)
	}

	// Seed a dummy run & artifact for forget testing
	runID := uuid.New()
	artID := uuid.New()
	jobID := uuid.New()
	snapID := "1111222233334444555566667777888899990000aaaabbbbccccddddeeeeffff"

	if _, err := conn.Exec(ctx, `
		INSERT INTO backup_jobs (id, organization_id, resource_id, storage_target_id, trigger_type, backup_type, engine_type, target_spec, status)
		VALUES ($1, $2, $3, $4, 'manual', 'mysql_database', 'restic', '{"database_name":"db"}'::jsonb, 'running');
	`, jobID, orgID, resID, targetID); err != nil {
		t.Fatalf("failed seeding job: %v", err)
	}
	if _, err := conn.Exec(ctx, `
		INSERT INTO backup_runs (id, organization_id, job_id, attempt_number, status, started_at)
		VALUES ($1, $2, $3, 1, 'running', NOW());
	`, runID, orgID, jobID); err != nil {
		t.Fatalf("failed seeding run: %v", err)
	}
	logicalSize := int64(1024)
	if _, err := repo.CreateArtifact(ctx, &domain.BackupArtifact{
		ID:                 artID,
		OrganizationID:     orgID,
		RunID:              runID,
		ResourceID:         resID,
		StorageTargetID:    targetID,
		ArtifactType:       domain.ArtifactTypeDatabaseDump,
		Format:             domain.ArtifactFormatResticSnapshot,
		TargetName:         "db_dump",
		RepositoryID:       &repoID,
		SnapshotID:         snapID,
		LogicalSizeBytes:   &logicalSize,
		VerificationStatus: domain.VerificationStatusUnverified,
	}); err != nil {
		t.Fatalf("failed creating artifact: %v", err)
	}

	var forgetJobID, pruneJobID uuid.UUID

	t.Run("EnqueueMaintenanceJob and Deduplication", func(t *testing.T) {
		// Enqueue Forget
		forgetJob, err := repo.EnqueueMaintenanceJob(ctx, domain.EnqueueMaintenanceJobParams{
			OrganizationID: orgID,
			RepositoryID:   repoID,
			OperationType:  domain.MaintenanceOpResticForget,
			ArtifactID:     &artID,
			SnapshotID:     snapID,
		})
		if err != nil {
			t.Fatalf("unexpected error enqueuing forget: %v", err)
		}
		if forgetJob.Status != domain.MaintenanceJobPending {
			t.Fatalf("expected pending status, got %s", forgetJob.Status)
		}
		forgetJobID = forgetJob.ID

		// Enqueue duplicate active Forget -> deduplication should return existing job
		dupForget, err := repo.EnqueueMaintenanceJob(ctx, domain.EnqueueMaintenanceJobParams{
			OrganizationID: orgID,
			RepositoryID:   repoID,
			OperationType:  domain.MaintenanceOpResticForget,
			ArtifactID:     &artID,
			SnapshotID:     snapID,
		})
		if err != nil {
			t.Fatalf("unexpected error on duplicate forget: %v", err)
		}
		if dupForget.ID != forgetJob.ID {
			t.Fatalf("expected deduplication to return job %s, got %s", forgetJob.ID, dupForget.ID)
		}

		// Enqueue Prune
		pruneJob, err := repo.EnqueueMaintenanceJob(ctx, domain.EnqueueMaintenanceJobParams{
			OrganizationID: orgID,
			RepositoryID:   repoID,
			OperationType:  domain.MaintenanceOpResticPrune,
		})
		if err != nil {
			t.Fatalf("unexpected error enqueuing prune: %v", err)
		}
		if pruneJob.OperationType != domain.MaintenanceOpResticPrune {
			t.Fatalf("expected restic_prune, got %s", pruneJob.OperationType)
		}
		pruneJobID = pruneJob.ID

		// Enqueue duplicate active Prune -> deduplication should return existing job
		dupPrune, err := repo.EnqueueMaintenanceJob(ctx, domain.EnqueueMaintenanceJobParams{
			OrganizationID: orgID,
			RepositoryID:   repoID,
			OperationType:  domain.MaintenanceOpResticPrune,
		})
		if err != nil {
			t.Fatalf("unexpected error on duplicate prune: %v", err)
		}
		if dupPrune.ID != pruneJob.ID {
			t.Fatalf("expected deduplication to return prune job %s, got %s", pruneJob.ID, dupPrune.ID)
		}
	})

	t.Run("ClaimNextMaintenanceJob and Heartbeat", func(t *testing.T) {
		// Oldest pending job should be forgetJob
		claimedJob, run, err := repo.ClaimNextMaintenanceJob(ctx, 2*time.Minute)
		if err != nil {
			t.Fatalf("unexpected error claiming maintenance job: %v", err)
		}
		if claimedJob == nil || run == nil {
			t.Fatalf("expected a job and run to be claimed")
		}
		if claimedJob.ID != forgetJobID {
			t.Fatalf("expected claimed job %s, got %s", forgetJobID, claimedJob.ID)
		}
		if claimedJob.Status != domain.MaintenanceJobRunning {
			t.Fatalf("expected claimed job status running, got %s", claimedJob.Status)
		}
		if run.Status != domain.MaintenanceRunRunning {
			t.Fatalf("expected run status running, got %s", run.Status)
		}
		if run.AttemptNumber != 1 {
			t.Fatalf("expected attempt number 1, got %d", run.AttemptNumber)
		}

		// Heartbeat run
		if err := repo.HeartbeatMaintenanceRun(ctx, orgID, run.ID, 5*time.Minute); err != nil {
			t.Fatalf("failed heartbeating maintenance run: %v", err)
		}

		// Complete job
		if err := repo.CompleteMaintenanceJob(ctx, orgID, claimedJob.ID, run.ID, []byte("{\"result\":\"ok\"}")); err != nil {
			t.Fatalf("failed completing maintenance job: %v", err)
		}

		// Verify job is now completed
		updatedJob, err := repo.GetMaintenanceJobByID(ctx, orgID, claimedJob.ID)
		if err != nil {
			t.Fatalf("failed fetching updated job: %v", err)
		}
		if updatedJob.Status != domain.MaintenanceJobCompleted {
			t.Fatalf("expected job status completed, got %s", updatedJob.Status)
		}
	})

	t.Run("FailMaintenanceJob with Retryable vs Non-retryable", func(t *testing.T) {
		// Claim next job (prune)
		claimedJob, run, err := repo.ClaimNextMaintenanceJob(ctx, 2*time.Minute)
		if err != nil || claimedJob == nil {
			t.Fatalf("expected prune job to be claimed, got err: %v", err)
		}
		if claimedJob.ID != pruneJobID {
			t.Fatalf("expected claimed job %s, got %s", pruneJobID, claimedJob.ID)
		}
		if run.AttemptNumber != 1 {
			t.Fatalf("expected attempt number 1, got %d", run.AttemptNumber)
		}

		// Fail with retryable = true -> job status should revert to pending with backoff
		if err := repo.FailMaintenanceJob(ctx, orgID, claimedJob.ID, run.ID, "temporary failure", true); err != nil {
			t.Fatalf("failed failing maintenance job: %v", err)
		}

		recheckedJob, err := repo.GetMaintenanceJobByID(ctx, orgID, claimedJob.ID)
		if err != nil {
			t.Fatalf("failed getting job: %v", err)
		}
		if recheckedJob.Status != domain.MaintenanceJobPending {
			t.Fatalf("expected status pending for retryable failure, got %s", recheckedJob.Status)
		}
		if recheckedJob.AttemptCount != 1 {
			t.Fatalf("expected attempt count 1, got %d", recheckedJob.AttemptCount)
		}
		if recheckedJob.NextAttemptAt.IsZero() || !recheckedJob.NextAttemptAt.After(time.Now()) {
			t.Fatalf("expected next_attempt_at to be set into the future, got %v", recheckedJob.NextAttemptAt)
		}

		// Verify that ClaimNextMaintenanceJob does not prematurely claim during backoff delay
		prematureJob, prematureRun, err := repo.ClaimNextMaintenanceJob(ctx, 2*time.Minute)
		if err != nil {
			t.Fatalf("unexpected error claiming during backoff: %v", err)
		}
		if prematureJob != nil || prematureRun != nil {
			t.Fatalf("expected no job to be claimable during backoff, got job: %v", prematureJob)
		}

		// Fast-forward backoff expiration via database update without sleeping 60s
		if _, err := conn.Exec(ctx, "UPDATE repository_maintenance_jobs SET next_attempt_at = NOW() - INTERVAL '1 second' WHERE id = $1;", claimedJob.ID); err != nil {
			t.Fatalf("failed fast-forwarding next_attempt_at: %v", err)
		}

		// Claim it again -> should now claim prune job with attempt number 2
		reclaimedJob, run2, err := repo.ClaimNextMaintenanceJob(ctx, 2*time.Minute)
		if err != nil || reclaimedJob == nil {
			t.Fatalf("expected re-claimed job, got err: %v", err)
		}
		if reclaimedJob.ID != claimedJob.ID {
			t.Fatalf("expected reclaimed job ID %s, got %s", claimedJob.ID, reclaimedJob.ID)
		}
		if run2.AttemptNumber != 2 {
			t.Fatalf("expected attempt number 2, got %d", run2.AttemptNumber)
		}
		if reclaimedJob.AttemptCount != 2 {
			t.Fatalf("expected attempt count 2, got %d", reclaimedJob.AttemptCount)
		}

		// Fail with retryable = false -> job status should be failed (and error_message truncated to <= 1024 bytes)
		longErrMsg := strings.Repeat("A", 5000)
		if err := repo.FailMaintenanceJob(ctx, orgID, reclaimedJob.ID, run2.ID, longErrMsg, false); err != nil {
			t.Fatalf("failed failing maintenance job permanently: %v", err)
		}

		var storedErrMsg *string
		if err := conn.QueryRow(ctx, "SELECT error_message FROM repository_maintenance_runs WHERE id = $1;", run2.ID).Scan(&storedErrMsg); err != nil {
			t.Fatalf("failed querying run error_message: %v", err)
		}
		if storedErrMsg == nil || len(*storedErrMsg) > 1024 || !strings.HasSuffix(*storedErrMsg, "... [truncated]") {
			t.Fatalf("expected stored error_message to be truncated <= 1024 bytes, got %v", storedErrMsg)
		}

		failedJob, err := repo.GetMaintenanceJobByID(ctx, orgID, reclaimedJob.ID)
		if err != nil {
			t.Fatalf("failed getting job: %v", err)
		}
		if failedJob.Status != domain.MaintenanceJobFailed {
			t.Fatalf("expected status failed for permanent failure, got %s", failedJob.Status)
		}
	})

	t.Run("GetLastSuccessfulDeepCheckSubset", func(t *testing.T) {
		// Enqueue Deep Check (subset 1/4)
		subIdx := 1
		subTot := 4
		enqueuedCheck, err := repo.EnqueueMaintenanceJob(ctx, domain.EnqueueMaintenanceJobParams{
			OrganizationID: orgID,
			RepositoryID:   repoID,
			OperationType:  domain.MaintenanceOpResticDeepCheck,
			SubsetIndex:    &subIdx,
			SubsetTotal:    &subTot,
		})
		if err != nil {
			t.Fatalf("unexpected error enqueuing deep check: %v", err)
		}
		if *enqueuedCheck.SubsetIndex != 1 || *enqueuedCheck.SubsetTotal != 4 {
			t.Fatalf("expected 1/4, got %d/%d", *enqueuedCheck.SubsetIndex, *enqueuedCheck.SubsetTotal)
		}

		// Deduplication check for deep check
		dupCheck, err := repo.EnqueueMaintenanceJob(ctx, domain.EnqueueMaintenanceJobParams{
			OrganizationID: orgID,
			RepositoryID:   repoID,
			OperationType:  domain.MaintenanceOpResticDeepCheck,
			SubsetIndex:    &subIdx,
			SubsetTotal:    &subTot,
		})
		if err != nil {
			t.Fatalf("unexpected error on duplicate deep check: %v", err)
		}
		if dupCheck.ID != enqueuedCheck.ID {
			t.Fatalf("expected deep check deduplication to return job %s, got %s", enqueuedCheck.ID, dupCheck.ID)
		}

		// Claim deep check job
		checkJob, run, err := repo.ClaimNextMaintenanceJob(ctx, 2*time.Minute)
		if err != nil || checkJob == nil {
			t.Fatalf("expected deep check job to be claimed, got err: %v", err)
		}
		if checkJob.ID != enqueuedCheck.ID {
			t.Fatalf("expected claimed job %s, got %s", enqueuedCheck.ID, checkJob.ID)
		}
		if run.AttemptNumber != 1 {
			t.Fatalf("expected attempt number 1, got %d", run.AttemptNumber)
		}

		// Complete deep check as success
		if err := repo.CompleteMaintenanceJob(ctx, orgID, checkJob.ID, run.ID, []byte("{\"check\":\"success\"}")); err != nil {
			t.Fatalf("failed completing deep check: %v", err)
		}

		// Query last successful subset
		lastSubset, err := repo.GetLastSuccessfulDeepCheckSubset(ctx, orgID, repoID)
		if err != nil {
			t.Fatalf("failed querying last successful subset: %v", err)
		}
		if lastSubset != 1 {
			t.Fatalf("expected last successful subset 1, got %d", lastSubset)
		}
	})

	t.Run("Rollback Guard: Down migration refuses rollback if jobs/runs exist", func(t *testing.T) {
		// Try to migrate down to 9 while jobs and runs exist -> must fail closed
		err := m.Migrate(9)
		if err == nil {
			t.Fatalf("expected migration rollback to fail when maintenance jobs/runs exist, but got nil")
		}
		if !strings.Contains(err.Error(), "cannot rollback migration 000010") {
			t.Fatalf("expected fail-closed guard error message, got: %v", err)
		}
		t.Logf("Rollback guard properly prevented drop: %v", err)

		// Delete jobs and runs, then verify rollback succeeds
		_, err = conn.Exec(ctx, `
			DELETE FROM repository_maintenance_runs;
			DELETE FROM repository_maintenance_jobs;
		`)
		// Reset dirty flag from failed rollback attempt
		if err := m.Force(10); err != nil {
			t.Fatalf("failed resetting dirty flag to version 10: %v", err)
		}

		// Rollback to v9 should now succeed cleanly
		if err := m.Migrate(9); err != nil {
			t.Fatalf("failed rolling back to version 9 after cleanup: %v", err)
		}
		t.Log("Successfully rolled back to version 9 after data cleanup")

		// Re-apply migration 10
		if err := m.Migrate(10); err != nil {
			t.Fatalf("failed re-migrating to version 10: %v", err)
		}
		t.Log("Successfully re-migrated to version 10")
	})
}

func TestSanitizeMaintenanceErrorMessage(t *testing.T) {
	t.Run("empty input", func(t *testing.T) {
		if got := sanitizeMaintenanceErrorMessage(""); got != "" {
			t.Fatalf("expected empty string, got %q", got)
		}
	})

	t.Run("whitespace trimming", func(t *testing.T) {
		if got := sanitizeMaintenanceErrorMessage("   error with whitespace   \n\t"); got != "error with whitespace" {
			t.Fatalf("expected trimmed error, got %q", got)
		}
		if got := sanitizeMaintenanceErrorMessage("   \n\t  "); got != "" {
			t.Fatalf("expected empty string for whitespace-only, got %q", got)
		}
	})

	t.Run("short message unchanged", func(t *testing.T) {
		msg := "connection refused by target host"
		if got := sanitizeMaintenanceErrorMessage(msg); got != msg {
			t.Fatalf("expected %q, got %q", msg, got)
		}
	})

	t.Run("marker only added on truncation", func(t *testing.T) {
		short := "short failure without truncation"
		gotShort := sanitizeMaintenanceErrorMessage(short)
		if strings.Contains(gotShort, "[truncated]") {
			t.Fatalf("marker should not be added to short message, got %q", gotShort)
		}

		long := strings.Repeat("B", 1025)
		gotLong := sanitizeMaintenanceErrorMessage(long)
		if !strings.HasSuffix(gotLong, "... [truncated]") {
			t.Fatalf("marker must be added to truncated message, got %q", gotLong)
		}
	})

	t.Run("exactly 1024 ASCII bytes", func(t *testing.T) {
		msg := strings.Repeat("x", 1024)
		got := sanitizeMaintenanceErrorMessage(msg)
		if len(got) != 1024 {
			t.Fatalf("expected len 1024, got %d", len(got))
		}
		if got != msg {
			t.Fatalf("expected exact match without truncation")
		}
		if strings.Contains(got, "[truncated]") {
			t.Fatalf("marker should not be added when len is exactly 1024")
		}
	})

	t.Run("1025 ASCII bytes", func(t *testing.T) {
		msg := strings.Repeat("y", 1025)
		got := sanitizeMaintenanceErrorMessage(msg)
		if len(got) > 1024 {
			t.Fatalf("expected len <= 1024, got %d", len(got))
		}
		if len(got) != 1024 {
			t.Fatalf("expected exact len 1024 for ASCII truncation, got %d", len(got))
		}
		if !strings.HasSuffix(got, "... [truncated]") {
			t.Fatalf("expected truncation suffix, got %q", got)
		}
	})

	t.Run("5000 ASCII bytes", func(t *testing.T) {
		msg := strings.Repeat("z", 5000)
		got := sanitizeMaintenanceErrorMessage(msg)
		if len(got) > 1024 {
			t.Fatalf("expected len <= 1024, got %d", len(got))
		}
		if len(got) != 1024 {
			t.Fatalf("expected exact len 1024 for ASCII truncation, got %d", len(got))
		}
		if !strings.HasSuffix(got, "... [truncated]") {
			t.Fatalf("expected truncation suffix, got %q", got)
		}
	})

	t.Run("valid multi-byte UTF-8 near boundary", func(t *testing.T) {
		// Budget is 1024 - 15 = 1009 bytes.
		// 1. Two-byte runes (e.g. 'é' = 2 bytes)
		twoByteMsg := strings.Repeat("é", 600) // 1200 bytes
		gotTwo := sanitizeMaintenanceErrorMessage(twoByteMsg)
		if len(gotTwo) > 1024 {
			t.Fatalf("expected len <= 1024, got %d", len(gotTwo))
		}
		if !utf8.ValidString(gotTwo) {
			t.Fatalf("expected valid UTF-8, got invalid string")
		}
		if !strings.HasSuffix(gotTwo, "... [truncated]") {
			t.Fatalf("expected truncation suffix, got %q", gotTwo)
		}

		// 2. Three-byte runes (e.g. '世' = 3 bytes)
		threeByteMsg := strings.Repeat("世", 400) // 1200 bytes
		gotThree := sanitizeMaintenanceErrorMessage(threeByteMsg)
		if len(gotThree) > 1024 {
			t.Fatalf("expected len <= 1024, got %d", len(gotThree))
		}
		if !utf8.ValidString(gotThree) {
			t.Fatalf("expected valid UTF-8, got invalid string")
		}
		if !strings.HasSuffix(gotThree, "... [truncated]") {
			t.Fatalf("expected truncation suffix, got %q", gotThree)
		}

		// 3. Four-byte runes (e.g. '🚀' = 4 bytes)
		fourByteMsg := strings.Repeat("🚀", 300) // 1200 bytes
		gotFour := sanitizeMaintenanceErrorMessage(fourByteMsg)
		if len(gotFour) > 1024 {
			t.Fatalf("expected len <= 1024, got %d", len(gotFour))
		}
		if !utf8.ValidString(gotFour) {
			t.Fatalf("expected valid UTF-8, got invalid string")
		}
		if !strings.HasSuffix(gotFour, "... [truncated]") {
			t.Fatalf("expected truncation suffix, got %q", gotFour)
		}

		// 4. Multi-byte rune straddling the budget boundary
		// Budget is 1009. Place 1008 ASCII bytes + 1 three-byte rune '世' (bytes 1008, 1009, 1010) + suffix
		straddleMsg := strings.Repeat("a", 1008) + "世" + strings.Repeat("b", 100)
		gotStraddle := sanitizeMaintenanceErrorMessage(straddleMsg)
		if len(gotStraddle) > 1024 {
			t.Fatalf("expected len <= 1024, got %d", len(gotStraddle))
		}
		if !utf8.ValidString(gotStraddle) {
			t.Fatalf("expected valid UTF-8, got invalid string")
		}
		if !strings.HasSuffix(gotStraddle, "... [truncated]") {
			t.Fatalf("expected truncation suffix, got %q", gotStraddle)
		}
		// The 3-byte rune started at 1008 and could not fit in 1009 bytes, so prefix must be exactly 1008 'a's
		expectedPrefix := strings.Repeat("a", 1008)
		if !strings.HasPrefix(gotStraddle, expectedPrefix) {
			t.Fatalf("expected prefix of 1008 'a's")
		}
		if len(gotStraddle) != 1008+15 {
			t.Fatalf("expected len %d, got %d", 1008+15, len(gotStraddle))
		}
	})
}
