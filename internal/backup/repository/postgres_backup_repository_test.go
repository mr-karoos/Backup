package repository

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"backup-platform/internal/backup/domain"
	"backup-platform/internal/platform/database"
	"backup-platform/pkg/uuid"
)

// Mock Querier & TxManager for Repository Unit Tests
type mockQuerier struct {
	execFunc     func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	queryFunc    func(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	queryRowFunc func(ctx context.Context, sql string, args ...any) pgx.Row
}

func (m *mockQuerier) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if m.execFunc != nil {
		return m.execFunc(ctx, sql, args...)
	}
	return pgconn.NewCommandTag(""), nil
}

func (m *mockQuerier) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if m.queryFunc != nil {
		return m.queryFunc(ctx, sql, args...)
	}
	return nil, nil
}

func (m *mockQuerier) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if m.queryRowFunc != nil {
		return m.queryRowFunc(ctx, sql, args...)
	}
	return &mockRow{}
}

type mockRow struct {
	scanFunc func(dest ...any) error
}

func (r *mockRow) Scan(dest ...any) error {
	if r.scanFunc != nil {
		return r.scanFunc(dest...)
	}
	return pgx.ErrNoRows
}

type mockTxManager struct {
	querier *mockQuerier
}

func (m *mockTxManager) Querier() database.Querier {
	return m.querier
}

func (m *mockTxManager) WithinTx(ctx context.Context, fn func(q database.Querier) error) error {
	return fn(m.querier)
}

func TestPostgresBackupRepository_CreateArtifact_ChainIntegrity(t *testing.T) {
	t.Run("CreateArtifact returns ErrArtifactChainMismatch on mismatch / non-running run", func(t *testing.T) {
		q := &mockQuerier{
			queryRowFunc: func(ctx context.Context, sql string, args ...any) pgx.Row {
				return &mockRow{
					scanFunc: func(dest ...any) error {
						return pgx.ErrNoRows // Query returned 0 rows because WHERE conditions or JOINs failed
					},
				}
			},
		}
		repo := NewPostgresBackupRepository(&mockTxManager{querier: q})

		art := &domain.BackupArtifact{
			ID:              uuid.New(),
			OrganizationID:  uuid.New(),
			RunID:           uuid.New(),
			ResourceID:      uuid.New(),
			StorageTargetID: uuid.New(),
			ArtifactType:    domain.ArtifactTypeDatabaseDump,
			Format:          domain.ArtifactFormatSQLGzip,
			TargetName:      "test_db",
			SizeBytes:       1024,
			ChecksumHash:    "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		}

		_, err := repo.CreateArtifact(context.Background(), art)
		if !errors.Is(err, domain.ErrArtifactChainMismatch) {
			t.Fatalf("expected ErrArtifactChainMismatch, got: %v", err)
		}
	})
}

func TestPostgresBackupRepository_FinalizeRunAndJob_StateGuard(t *testing.T) {
	t.Run("Returns ErrRunNoLongerActive if run is no longer running", func(t *testing.T) {
		q := &mockQuerier{
			execFunc: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				// Simulate 0 rows updated because run status was already failed or reaped
				return pgconn.NewCommandTag("UPDATE 0"), nil
			},
		}
		repo := NewPostgresBackupRepository(&mockTxManager{querier: q})

		err := repo.FinalizeRunAndJob(
			context.Background(),
			uuid.New(), uuid.New(), uuid.New(),
			domain.RunStatusSuccess, domain.JobStatusCompleted,
			nil, nil,
		)
		if !errors.Is(err, domain.ErrRunNoLongerActive) {
			t.Fatalf("expected ErrRunNoLongerActive, got: %v", err)
		}
	})
}

func TestPostgresBackupRepository_RecoveryAndReaper_ErrorPropagation(t *testing.T) {
	t.Run("RecoverInterruptedRuns propagates transaction error", func(t *testing.T) {
		runID := uuid.New()
		orgID := uuid.New()
		jobID := uuid.New()
		attempt := 1

		rowsScanned := false
		q := &mockQuerier{
			queryFunc: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
				return &mockRunsRows{
					runs: []struct {
						id      uuid.UUID
						orgID   uuid.UUID
						jobID   uuid.UUID
						attempt int
					}{
						{id: runID, orgID: orgID, jobID: jobID, attempt: attempt},
					},
				}, nil
			},
			execFunc: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				rowsScanned = true
				return pgconn.NewCommandTag(""), errors.New("db connection lost")
			},
		}
		repo := NewPostgresBackupRepository(&mockTxManager{querier: q})

		_, err := repo.RecoverInterruptedRuns(context.Background())
		if err == nil {
			t.Fatalf("expected error from RecoverInterruptedRuns, got nil")
		}
		if !rowsScanned {
			t.Fatalf("expected recovery query execution")
		}
	})

	t.Run("ReapStaleRuns propagates transaction error", func(t *testing.T) {
		runID := uuid.New()
		orgID := uuid.New()
		jobID := uuid.New()
		attempt := 1

		q := &mockQuerier{
			queryFunc: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
				return &mockRunsRows{
					runs: []struct {
						id      uuid.UUID
						orgID   uuid.UUID
						jobID   uuid.UUID
						attempt int
					}{
						{id: runID, orgID: orgID, jobID: jobID, attempt: attempt},
					},
				}, nil
			},
			execFunc: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				return pgconn.NewCommandTag(""), errors.New("db deadlocked")
			},
		}
		repo := NewPostgresBackupRepository(&mockTxManager{querier: q})

		_, err := repo.ReapStaleRuns(context.Background())
		if err == nil {
			t.Fatalf("expected error from ReapStaleRuns, got nil")
		}
	})

	t.Run("Heartbeat-wins race: candidate A won by heartbeat, candidate B still stale and transitions", func(t *testing.T) {
		runA := uuid.New()
		orgA := uuid.New()
		jobA := uuid.New()

		runB := uuid.New()
		orgB := uuid.New()
		jobB := uuid.New()

		jobAUpdated := false
		jobBUpdated := false

		q := &mockQuerier{
			queryFunc: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
				return &mockRunsRows{
					runs: []struct {
						id      uuid.UUID
						orgID   uuid.UUID
						jobID   uuid.UUID
						attempt int
					}{
						{id: runA, orgID: orgA, jobID: jobA, attempt: 1},
						{id: runB, orgID: orgB, jobID: jobB, attempt: 2},
					},
				}, nil
			},
			execFunc: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				if strings.Contains(sql, "UPDATE backup_runs") {
					// Check which run is being updated
					for _, arg := range args {
						if id, ok := arg.(uuid.UUID); ok {
							if id == runA {
								// Heartbeat renewed lease -> 0 rows affected
								return pgconn.NewCommandTag("UPDATE 0"), nil
							}
							if id == runB {
								// Still stale -> 1 row affected
								return pgconn.NewCommandTag("UPDATE 1"), nil
							}
						}
					}
				}
				if strings.Contains(sql, "UPDATE backup_jobs") {
					for _, arg := range args {
						if id, ok := arg.(uuid.UUID); ok {
							if id == jobA {
								jobAUpdated = true
							}
							if id == jobB {
								jobBUpdated = true
								return pgconn.NewCommandTag("UPDATE 1"), nil
							}
						}
					}
				}
				return pgconn.NewCommandTag("UPDATE 0"), nil
			},
		}
		repo := NewPostgresBackupRepository(&mockTxManager{querier: q})

		reaped, err := repo.ReapStaleRuns(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(reaped) != 1 {
			t.Fatalf("expected exactly 1 reaped run, got %d", len(reaped))
		}
		if reaped[0].ID != runB {
			t.Errorf("expected candidate B to be reaped, got: %s", reaped[0].ID)
		}
		if jobAUpdated {
			t.Errorf("job A must NOT be updated when heartbeat won race on run A")
		}
		if !jobBUpdated {
			t.Errorf("job B must be updated when run B was reaped")
		}
	})

	t.Run("ReapStaleRuns preserves successfully transitioned runs when subsequent candidate fails", func(t *testing.T) {
		runA := uuid.New()
		orgA := uuid.New()
		jobA := uuid.New()

		runB := uuid.New()
		orgB := uuid.New()
		jobB := uuid.New()

		q := &mockQuerier{
			queryFunc: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
				return &mockRunsRows{
					runs: []struct {
						id      uuid.UUID
						orgID   uuid.UUID
						jobID   uuid.UUID
						attempt int
					}{
						{id: runA, orgID: orgA, jobID: jobA, attempt: 1},
						{id: runB, orgID: orgB, jobID: jobB, attempt: 1},
					},
				}, nil
			},
			execFunc: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				for _, arg := range args {
					if id, ok := arg.(uuid.UUID); ok {
						if id == runA || id == jobA {
							return pgconn.NewCommandTag("UPDATE 1"), nil
						}
						if id == runB {
							return pgconn.NewCommandTag(""), errors.New("database connection reset")
						}
					}
				}
				return pgconn.NewCommandTag("UPDATE 1"), nil
			},
		}
		repo := NewPostgresBackupRepository(&mockTxManager{querier: q})

		reaped, err := repo.ReapStaleRuns(context.Background())
		if err == nil {
			t.Fatalf("expected non-nil error from ReapStaleRuns on candidate B failure")
		}
		if len(reaped) != 1 {
			t.Fatalf("expected candidate A to be preserved in returned reaped slice, got %d", len(reaped))
		}
		if reaped[0].ID != runA {
			t.Errorf("expected candidate A in reaped slice, got: %s", reaped[0].ID)
		}
	})

	t.Run("RecoverInterruptedRuns preserves successfully transitioned runs when subsequent candidate fails", func(t *testing.T) {
		runA := uuid.New()
		orgA := uuid.New()
		jobA := uuid.New()

		runB := uuid.New()
		orgB := uuid.New()
		jobB := uuid.New()

		q := &mockQuerier{
			queryFunc: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
				return &mockRunsRows{
					runs: []struct {
						id      uuid.UUID
						orgID   uuid.UUID
						jobID   uuid.UUID
						attempt int
					}{
						{id: runA, orgID: orgA, jobID: jobA, attempt: 1},
						{id: runB, orgID: orgB, jobID: jobB, attempt: 1},
					},
				}, nil
			},
			execFunc: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				for _, arg := range args {
					if id, ok := arg.(uuid.UUID); ok {
						if id == runA || id == jobA {
							return pgconn.NewCommandTag("UPDATE 1"), nil
						}
						if id == runB {
							return pgconn.NewCommandTag(""), errors.New("database deadlock")
						}
					}
				}
				return pgconn.NewCommandTag("UPDATE 1"), nil
			},
		}
		repo := NewPostgresBackupRepository(&mockTxManager{querier: q})

		recovered, err := repo.RecoverInterruptedRuns(context.Background())
		if err == nil {
			t.Fatalf("expected non-nil error from RecoverInterruptedRuns on candidate B failure")
		}
		if len(recovered) != 1 {
			t.Fatalf("expected candidate A to be preserved in returned recovered slice, got %d", len(recovered))
		}
		if recovered[0].ID != runA {
			t.Errorf("expected candidate A in recovered slice, got: %s", recovered[0].ID)
		}
	})
}

func TestPostgresBackupRepository_GetLatestRunForJob_Sentinel(t *testing.T) {
	t.Run("Returns ErrRunNotFound when no run exists", func(t *testing.T) {
		q := &mockQuerier{
			queryRowFunc: func(ctx context.Context, sql string, args ...any) pgx.Row {
				return &mockRow{
					scanFunc: func(dest ...any) error {
						return pgx.ErrNoRows
					},
				}
			},
		}
		repo := NewPostgresBackupRepository(&mockTxManager{querier: q})

		run, err := repo.GetLatestRunForJob(context.Background(), uuid.New(), uuid.New())
		if run != nil {
			t.Fatalf("expected nil run")
		}
		if !errors.Is(err, domain.ErrRunNotFound) {
			t.Fatalf("expected ErrRunNotFound, got: %v", err)
		}
	})
}

func TestPostgresBackupRepository_UpdateHeartbeat_Sentinel(t *testing.T) {
	t.Run("Returns ErrRunNoLongerActive on UPDATE 0 rows", func(t *testing.T) {
		q := &mockQuerier{
			execFunc: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				return pgconn.NewCommandTag("UPDATE 0"), nil
			},
		}
		repo := NewPostgresBackupRepository(&mockTxManager{querier: q})

		err := repo.UpdateHeartbeat(context.Background(), uuid.New(), uuid.New())
		if !errors.Is(err, domain.ErrRunNoLongerActive) {
			t.Fatalf("expected ErrRunNoLongerActive, got: %v", err)
		}
	})

	t.Run("Propagates database execution error", func(t *testing.T) {
		dbErr := errors.New("connection reset by peer")
		q := &mockQuerier{
			execFunc: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				return pgconn.NewCommandTag(""), dbErr
			},
		}
		repo := NewPostgresBackupRepository(&mockTxManager{querier: q})

		err := repo.UpdateHeartbeat(context.Background(), uuid.New(), uuid.New())
		if err == nil || errors.Is(err, domain.ErrRunNoLongerActive) {
			t.Fatalf("expected raw db error propagation, got: %v", err)
		}
	})
}

func TestPostgresBackupRepository_PlanCRUD(t *testing.T) {
	orgID := uuid.New()
	resID := uuid.New()
	planID := uuid.New()

	t.Run("CreatePlan succeeds", func(t *testing.T) {
		q := &mockQuerier{
			queryRowFunc: func(ctx context.Context, sql string, args ...any) pgx.Row {
				return &mockRow{
					scanFunc: func(dest ...any) error {
						return nil
					},
				}
			},
		}
		repo := NewPostgresBackupRepository(&mockTxManager{querier: q})

		cron := "0 2 * * *"
		plan := &domain.BackupPlan{
			ID:                planID,
			OrganizationID:    orgID,
			ResourceID:        resID,
			Name:              "Daily Backup",
			BackupType:        domain.BackupTypeMySQLDatabase,
			TargetSpec:        domain.TargetSpec{Databases: []string{"db1"}},
			ScheduleCron:      &cron,
			ScheduleTimezone:  "UTC",
			IsScheduleEnabled: true,
			Status:            domain.PlanStatusActive,
		}

		created, err := repo.CreatePlan(context.Background(), plan)
		if err != nil {
			t.Fatalf("CreatePlan failed: %v", err)
		}
		if created.ID != planID {
			t.Fatalf("expected plan ID %s, got %s", planID, created.ID)
		}
	})

	t.Run("CreatePlan returns ErrResourceNotFound on FK violation", func(t *testing.T) {
		q := &mockQuerier{
			queryRowFunc: func(ctx context.Context, sql string, args ...any) pgx.Row {
				return &mockRow{
					scanFunc: func(dest ...any) error {
						return &pgconn.PgError{Code: "23503"}
					},
				}
			},
		}
		repo := NewPostgresBackupRepository(&mockTxManager{querier: q})

		plan := &domain.BackupPlan{
			ID:             planID,
			OrganizationID: orgID,
			ResourceID:     resID,
			Name:           "Daily Backup",
			BackupType:     domain.BackupTypeMySQLDatabase,
			TargetSpec:     domain.TargetSpec{Databases: []string{"db1"}},
		}

		_, err := repo.CreatePlan(context.Background(), plan)
		if !errors.Is(err, domain.ErrResourceNotFound) {
			t.Fatalf("expected ErrResourceNotFound, got: %v", err)
		}
	})

	t.Run("ArchivePlan returns ErrPlanNotFound on 0 rows affected", func(t *testing.T) {
		q := &mockQuerier{
			execFunc: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				return pgconn.NewCommandTag("UPDATE 0"), nil
			},
		}
		repo := NewPostgresBackupRepository(&mockTxManager{querier: q})

		err := repo.ArchivePlan(context.Background(), orgID, planID)
		if !errors.Is(err, domain.ErrPlanNotFound) {
			t.Fatalf("expected ErrPlanNotFound, got: %v", err)
		}
	})

	t.Run("ArchivePlan succeeds on 1 row affected", func(t *testing.T) {
		q := &mockQuerier{
			execFunc: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				return pgconn.NewCommandTag("UPDATE 1"), nil
			},
		}
		repo := NewPostgresBackupRepository(&mockTxManager{querier: q})

		err := repo.ArchivePlan(context.Background(), orgID, planID)
		if err != nil {
			t.Fatalf("expected nil error, got: %v", err)
		}
	})
}

func TestPostgresBackupRepository_EnqueueScheduledJobAndAdvanceNextRun(t *testing.T) {
	orgID := uuid.New()
	resID := uuid.New()
	planID := uuid.New()
	jobID := uuid.New()
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	futureNextRun := now.Add(24 * time.Hour)

	t.Run("Normal scheduled enqueue successfully inserts job and advances next_run_at", func(t *testing.T) {
		var updatedNextRunAt time.Time
		advanceCalled := false

		q := &mockQuerier{
			queryRowFunc: func(ctx context.Context, sql string, args ...any) pgx.Row {
				switch {
				case strings.Contains(sql, "FOR UPDATE"):
					return &mockRow{
						scanFunc: func(dest ...any) error {
							*(dest[0].(*uuid.UUID)) = planID
							*(dest[1].(*uuid.UUID)) = orgID
							*(dest[2].(*uuid.UUID)) = resID
							*(dest[3].(*domain.BackupType)) = domain.BackupTypeMySQLDatabase
							*(dest[4].(*[]byte)) = []byte(`{"databases":["db1"]}`)
							*(dest[5].(*domain.PlanStatus)) = domain.PlanStatusActive
							*(dest[6].(*bool)) = true
							*(dest[7].(**time.Time)) = &now
							*(dest[8].(*time.Time)) = now
							return nil
						},
					}
				case strings.Contains(sql, "INSERT INTO backup_jobs"):
					return &mockRow{
						scanFunc: func(dest ...any) error {
							*(dest[0].(*uuid.UUID)) = jobID
							*(dest[1].(*uuid.UUID)) = orgID
							*(dest[2].(*uuid.UUID)) = resID
							*(dest[3].(**uuid.UUID)) = &planID
							*(dest[4].(*domain.TriggerType)) = domain.TriggerTypeScheduled
							*(dest[5].(**uuid.UUID)) = nil
							*(dest[6].(*domain.BackupType)) = domain.BackupTypeMySQLDatabase
							*(dest[7].(*[]byte)) = []byte(`{"databases":["db1"]}`)
							*(dest[8].(*domain.JobStatus)) = domain.JobStatusPending
							*(dest[9].(*time.Time)) = now
							*(dest[10].(*time.Time)) = now
							return nil
						},
					}
				case strings.Contains(sql, "SELECT id FROM backup_jobs"):
					return &mockRow{
						scanFunc: func(dest ...any) error {
							return pgx.ErrNoRows // No pending job
						},
					}
				default:
					return &mockRow{}
				}
			},
			execFunc: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				if strings.Contains(sql, "UPDATE backup_plans") {
					advanceCalled = true
					if nr, ok := args[0].(*time.Time); ok && nr != nil {
						updatedNextRunAt = *nr
					}
					return pgconn.NewCommandTag("UPDATE 1"), nil
				}
				return pgconn.NewCommandTag(""), nil
			},
		}

		repo := NewPostgresBackupRepository(&mockTxManager{querier: q})
		jobToInsert := &domain.BackupJob{
			ID:         jobID,
			BackupType: domain.BackupTypeMySQLDatabase,
			TargetSpec: domain.TargetSpec{Databases: []string{"db1"}},
		}

		enqueued, err := repo.EnqueueScheduledJobAndAdvanceNextRun(context.Background(), planID, now, jobToInsert, &futureNextRun)
		if err != nil {
			t.Fatalf("expected nil error, got: %v", err)
		}
		if enqueued == nil || enqueued.ID != jobID {
			t.Fatalf("expected enqueued job with ID %s, got %+v", jobID, enqueued)
		}
		if !advanceCalled || !updatedNextRunAt.Equal(futureNextRun) {
			t.Fatalf("expected next_run_at advanced to %v, got %v (called=%v)", futureNextRun, updatedNextRunAt, advanceCalled)
		}
	})

	t.Run("Duplicate pending scheduled job detected by checkQuery skips insert and advances next_run_at", func(t *testing.T) {
		advanceCalled := false
		insertAttempted := false

		q := &mockQuerier{
			queryRowFunc: func(ctx context.Context, sql string, args ...any) pgx.Row {
				switch {
				case strings.Contains(sql, "FOR UPDATE"):
					return &mockRow{
						scanFunc: func(dest ...any) error {
							*(dest[0].(*uuid.UUID)) = planID
							*(dest[1].(*uuid.UUID)) = orgID
							*(dest[2].(*uuid.UUID)) = resID
							*(dest[3].(*domain.BackupType)) = domain.BackupTypeMySQLDatabase
							*(dest[4].(*[]byte)) = []byte(`{"databases":["db1"]}`)
							*(dest[5].(*domain.PlanStatus)) = domain.PlanStatusActive
							*(dest[6].(*bool)) = true
							*(dest[7].(**time.Time)) = &now
							*(dest[8].(*time.Time)) = now
							return nil
						},
					}
				case strings.Contains(sql, "INSERT INTO backup_jobs"):
					insertAttempted = true
					return &mockRow{}
				case strings.Contains(sql, "SELECT id FROM backup_jobs"):
					return &mockRow{
						scanFunc: func(dest ...any) error {
							*(dest[0].(*uuid.UUID)) = uuid.New() // Existing pending job found
							return nil
						},
					}
				default:
					return &mockRow{}
				}
			},
			execFunc: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				if strings.Contains(sql, "UPDATE backup_plans") {
					advanceCalled = true
					return pgconn.NewCommandTag("UPDATE 1"), nil
				}
				return pgconn.NewCommandTag(""), nil
			},
		}

		repo := NewPostgresBackupRepository(&mockTxManager{querier: q})
		jobToInsert := &domain.BackupJob{
			ID:         jobID,
			BackupType: domain.BackupTypeMySQLDatabase,
			TargetSpec: domain.TargetSpec{Databases: []string{"db1"}},
		}

		enqueued, err := repo.EnqueueScheduledJobAndAdvanceNextRun(context.Background(), planID, now, jobToInsert, &futureNextRun)
		if err != nil {
			t.Fatalf("expected nil error, got: %v", err)
		}
		if enqueued != nil {
			t.Fatalf("expected nil enqueued job when duplicate pending exists, got: %+v", enqueued)
		}
		if insertAttempted {
			t.Fatalf("expected insert not to be attempted when checkQuery found pending job")
		}
		if !advanceCalled {
			t.Fatalf("expected next_run_at to be advanced even when duplicate pending exists")
		}
	})

	t.Run("ON CONFLICT DO NOTHING returns pgx.ErrNoRows on race condition and safely advances next_run_at", func(t *testing.T) {
		advanceCalled := false

		q := &mockQuerier{
			queryRowFunc: func(ctx context.Context, sql string, args ...any) pgx.Row {
				switch {
				case strings.Contains(sql, "FOR UPDATE"):
					return &mockRow{
						scanFunc: func(dest ...any) error {
							*(dest[0].(*uuid.UUID)) = planID
							*(dest[1].(*uuid.UUID)) = orgID
							*(dest[2].(*uuid.UUID)) = resID
							*(dest[3].(*domain.BackupType)) = domain.BackupTypeMySQLDatabase
							*(dest[4].(*[]byte)) = []byte(`{"databases":["db1"]}`)
							*(dest[5].(*domain.PlanStatus)) = domain.PlanStatusActive
							*(dest[6].(*bool)) = true
							*(dest[7].(**time.Time)) = &now
							*(dest[8].(*time.Time)) = now
							return nil
						},
					}
				case strings.Contains(sql, "INSERT INTO backup_jobs"):
					return &mockRow{
						scanFunc: func(dest ...any) error {
							// ON CONFLICT DO NOTHING skipped insert, Scan returns ErrNoRows without transaction abort
							return pgx.ErrNoRows
						},
					}
				case strings.Contains(sql, "SELECT id FROM backup_jobs"):
					return &mockRow{
						scanFunc: func(dest ...any) error {
							return pgx.ErrNoRows // Passed initial check
						},
					}
				default:
					return &mockRow{}
				}
			},
			execFunc: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				if strings.Contains(sql, "UPDATE backup_plans") {
					advanceCalled = true
					return pgconn.NewCommandTag("UPDATE 1"), nil
				}
				return pgconn.NewCommandTag(""), nil
			},
		}

		repo := NewPostgresBackupRepository(&mockTxManager{querier: q})
		jobToInsert := &domain.BackupJob{
			ID:         jobID,
			BackupType: domain.BackupTypeMySQLDatabase,
			TargetSpec: domain.TargetSpec{Databases: []string{"db1"}},
		}

		enqueued, err := repo.EnqueueScheduledJobAndAdvanceNextRun(context.Background(), planID, now, jobToInsert, &futureNextRun)
		if err != nil {
			t.Fatalf("expected nil error on conflict race condition, got: %v", err)
		}
		if enqueued != nil {
			t.Fatalf("expected nil enqueued job on conflict race condition, got: %+v", enqueued)
		}
		if !advanceCalled {
			t.Fatalf("expected next_run_at update to execute safely in same transaction after conflict DO NOTHING")
		}
	})

	t.Run("Concurrently modified plan skips enqueue and next_run_at advance", func(t *testing.T) {
		advanceCalled := false
		insertAttempted := false

		q := &mockQuerier{
			queryRowFunc: func(ctx context.Context, sql string, args ...any) pgx.Row {
				switch {
				case strings.Contains(sql, "FOR UPDATE"):
					return &mockRow{
						scanFunc: func(dest ...any) error {
							*(dest[0].(*uuid.UUID)) = planID
							*(dest[1].(*uuid.UUID)) = orgID
							*(dest[2].(*uuid.UUID)) = resID
							*(dest[3].(*domain.BackupType)) = domain.BackupTypeMySQLDatabase
							*(dest[4].(*[]byte)) = []byte(`{"databases":["db1"]}`)
							*(dest[5].(*domain.PlanStatus)) = domain.PlanStatusActive
							*(dest[6].(*bool)) = true
							*(dest[7].(**time.Time)) = &now
							*(dest[8].(*time.Time)) = now.Add(5 * time.Minute) // Modified concurrently
							return nil
						},
					}
				case strings.Contains(sql, "INSERT INTO backup_jobs"):
					insertAttempted = true
					return &mockRow{}
				default:
					return &mockRow{}
				}
			},
			execFunc: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				if strings.Contains(sql, "UPDATE backup_plans") {
					advanceCalled = true
				}
				return pgconn.NewCommandTag(""), nil
			},
		}

		repo := NewPostgresBackupRepository(&mockTxManager{querier: q})
		jobToInsert := &domain.BackupJob{
			ID:         jobID,
			BackupType: domain.BackupTypeMySQLDatabase,
			TargetSpec: domain.TargetSpec{Databases: []string{"db1"}},
		}

		enqueued, err := repo.EnqueueScheduledJobAndAdvanceNextRun(context.Background(), planID, now, jobToInsert, &futureNextRun)
		if err != nil {
			t.Fatalf("expected nil error, got: %v", err)
		}
		if enqueued != nil {
			t.Fatalf("expected nil enqueued job for concurrently modified plan")
		}
		if insertAttempted || advanceCalled {
			t.Fatalf("expected neither insert nor advance to be called for concurrently modified plan")
		}
	})
}

type mockRunsRows struct {
	runs []struct {
		id      uuid.UUID
		orgID   uuid.UUID
		jobID   uuid.UUID
		attempt int
	}
	idx int
}

func (r *mockRunsRows) Close()                                       {}
func (r *mockRunsRows) Err() error                                   { return nil }
func (r *mockRunsRows) CommandTag() pgconn.CommandTag                { return pgconn.NewCommandTag("") }
func (r *mockRunsRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (r *mockRunsRows) RawValues() [][]byte                          { return nil }
func (r *mockRunsRows) Values() ([]any, error)                       { return nil, nil }
func (r *mockRunsRows) Conn() *pgx.Conn                              { return nil }
func (r *mockRunsRows) Next() bool {
	return r.idx < len(r.runs)
}
func (r *mockRunsRows) Scan(dest ...any) error {
	curr := r.runs[r.idx]
	r.idx++

	*(dest[0].(*uuid.UUID)) = curr.id
	*(dest[1].(*uuid.UUID)) = curr.orgID
	*(dest[2].(*uuid.UUID)) = curr.jobID
	*(dest[3].(*int)) = curr.attempt
	return nil
}

func TestPostgresBackupRepository_ListRuns_FiltersAndOrdering(t *testing.T) {
	orgID := uuid.New()
	resID := uuid.New()
	jobID := uuid.New()
	status := domain.RunStatusSuccess
	now := time.Now()
	fromDate := now.Add(-24 * time.Hour)
	toDate := now

	t.Run("applies all filters and deterministic ordering", func(t *testing.T) {
		var capturedSQL string
		var capturedArgs []any

		q := &mockQuerier{
			queryFunc: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
				capturedSQL = sql
				capturedArgs = args
				return &mockEmptyRows{}, nil
			},
		}

		repo := NewPostgresBackupRepository(&mockTxManager{querier: q})

		filter := domain.RunFilter{
			ResourceID: &resID,
			JobID:      &jobID,
			Status:     &status,
			FromDate:   &fromDate,
			ToDate:     &toDate,
		}

		_, err := repo.ListRuns(context.Background(), orgID, filter)
		if err != nil {
			t.Fatalf("expected nil error, got: %v", err)
		}

		if !strings.Contains(capturedSQL, "WHERE r.organization_id = $1") {
			t.Errorf("expected SQL to be scoped by organization_id $1")
		}
		if !strings.Contains(capturedSQL, "AND j.resource_id = $2") {
			t.Errorf("expected SQL to filter by resource_id $2")
		}
		if !strings.Contains(capturedSQL, "AND r.job_id = $3") {
			t.Errorf("expected SQL to filter by job_id $3")
		}
		if !strings.Contains(capturedSQL, "AND r.status = $4") {
			t.Errorf("expected SQL to filter by status $4")
		}
		if !strings.Contains(capturedSQL, "AND r.created_at >= $5") {
			t.Errorf("expected SQL to filter by from_date $5")
		}
		if !strings.Contains(capturedSQL, "AND r.created_at <= $6") {
			t.Errorf("expected SQL to filter by to_date $6")
		}
		if !strings.Contains(capturedSQL, "ORDER BY r.started_at DESC, r.id DESC") {
			t.Errorf("expected deterministic ordering by started_at DESC, id DESC")
		}

		if len(capturedArgs) != 6 {
			t.Fatalf("expected 6 arguments, got %d", len(capturedArgs))
		}
		if capturedArgs[0] != orgID {
			t.Errorf("arg 0 mismatch: expected orgID %v, got %v", orgID, capturedArgs[0])
		}
		if capturedArgs[1] != resID {
			t.Errorf("arg 1 mismatch: expected resID %v, got %v", resID, capturedArgs[1])
		}
		if capturedArgs[2] != jobID {
			t.Errorf("arg 2 mismatch: expected jobID %v, got %v", jobID, capturedArgs[2])
		}
		if capturedArgs[3] != string(status) {
			t.Errorf("arg 3 mismatch: expected status %v, got %v", string(status), capturedArgs[3])
		}
		if capturedArgs[4] != fromDate {
			t.Errorf("arg 4 mismatch: expected fromDate %v, got %v", fromDate, capturedArgs[4])
		}
		if capturedArgs[5] != toDate {
			t.Errorf("arg 5 mismatch: expected toDate %v, got %v", toDate, capturedArgs[5])
		}
	})
}

func TestPostgresBackupRepository_GetRunDetail_Aggregation(t *testing.T) {
	orgID := uuid.New()
	runID := uuid.New()
	jobID := uuid.New()
	resID := uuid.New()
	now := time.Now()

	t.Run("returns aggregated totals and run metadata", func(t *testing.T) {
		q := &mockQuerier{
			queryRowFunc: func(ctx context.Context, sql string, args ...any) pgx.Row {
				if !strings.Contains(sql, "WHERE r.organization_id = $1 AND r.id = $2") {
					t.Errorf("GetRunDetail must be scoped by organization_id and run_id")
				}
				if !strings.Contains(sql, "COALESCE(SUM(a.size_bytes), 0)::bigint AS total_artifact_size_bytes") {
					t.Errorf("GetRunDetail must aggregate total_artifact_size_bytes")
				}
				if !strings.Contains(sql, "COUNT(a.id)::int AS artifacts_count") {
					t.Errorf("GetRunDetail must count artifacts")
				}

				return &mockRow{
					scanFunc: func(dest ...any) error {
						*(dest[0].(*uuid.UUID)) = runID
						*(dest[1].(*uuid.UUID)) = orgID
						*(dest[2].(*uuid.UUID)) = jobID
						*(dest[3].(*int)) = 1
						*(dest[4].(*domain.RunStatus)) = domain.RunStatusSuccess
						*(dest[5].(*time.Time)) = now
						*(dest[6].(**time.Time)) = &now
						*(dest[7].(*time.Time)) = now
						*(dest[8].(*time.Time)) = now.Add(2 * time.Minute)
						*(dest[9].(**string)) = nil
						*(dest[10].(*[]byte)) = []byte("[]")
						*(dest[11].(*time.Time)) = now
						*(dest[12].(*time.Time)) = now
						*(dest[13].(*uuid.UUID)) = resID
						*(dest[14].(*int64)) = 52428800
						*(dest[15].(*int)) = 2
						return nil
					},
				}
			},
		}

		repo := NewPostgresBackupRepository(&mockTxManager{querier: q})
		stat, err := repo.GetRunDetail(context.Background(), orgID, runID)
		if err != nil {
			t.Fatalf("expected nil error, got: %v", err)
		}
		if stat == nil || stat.Run.ID != runID || stat.ResourceID != resID {
			t.Fatalf("unexpected run detail data")
		}
		if stat.TotalArtifactSizeBytes != 52428800 {
			t.Errorf("expected total size 52428800, got %d", stat.TotalArtifactSizeBytes)
		}
		if stat.ArtifactsCount != 2 {
			t.Errorf("expected count 2, got %d", stat.ArtifactsCount)
		}
	})

	t.Run("returns ErrRunNotFound when pgx.ErrNoRows", func(t *testing.T) {
		q := &mockQuerier{
			queryRowFunc: func(ctx context.Context, sql string, args ...any) pgx.Row {
				return &mockRow{
					scanFunc: func(dest ...any) error {
						return pgx.ErrNoRows
					},
				}
			},
		}

		repo := NewPostgresBackupRepository(&mockTxManager{querier: q})
		_, err := repo.GetRunDetail(context.Background(), orgID, runID)
		if !errors.Is(err, domain.ErrRunNotFound) {
			t.Fatalf("expected ErrRunNotFound, got: %v", err)
		}
	})
}

func TestPostgresBackupRepository_ArtifactRepositoryRegression(t *testing.T) {
	orgID := uuid.New()
	artID := uuid.New()

	t.Run("GetArtifactByID is organization scoped and returns ErrArtifactNotFound on no rows", func(t *testing.T) {
		var capturedSQL string
		var capturedArgs []any

		q := &mockQuerier{
			queryRowFunc: func(ctx context.Context, sql string, args ...any) pgx.Row {
				capturedSQL = sql
				capturedArgs = args
				return &mockRow{
					scanFunc: func(dest ...any) error {
						return pgx.ErrNoRows
					},
				}
			},
		}

		repo := NewPostgresBackupRepository(&mockTxManager{querier: q})
		_, err := repo.GetArtifactByID(context.Background(), orgID, artID)
		if !errors.Is(err, domain.ErrArtifactNotFound) {
			t.Fatalf("expected ErrArtifactNotFound, got: %v", err)
		}

		if !strings.Contains(capturedSQL, "WHERE organization_id = $1 AND id = $2") {
			t.Errorf("GetArtifactByID must be scoped by organization_id $1 and id $2, got SQL: %s", capturedSQL)
		}
		if len(capturedArgs) != 2 || capturedArgs[0] != orgID || capturedArgs[1] != artID {
			t.Errorf("unexpected arguments: %v", capturedArgs)
		}
	})

	t.Run("ListArtifacts is organization scoped, excludes deleted, and orders deterministically", func(t *testing.T) {
		var capturedSQL string
		var capturedArgs []any

		q := &mockQuerier{
			queryFunc: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
				capturedSQL = sql
				capturedArgs = args
				return &mockEmptyRows{}, nil
			},
		}

		repo := NewPostgresBackupRepository(&mockTxManager{querier: q})
		_, err := repo.ListArtifacts(context.Background(), orgID)
		if err != nil {
			t.Fatalf("expected nil error, got: %v", err)
		}

		if !strings.Contains(capturedSQL, "WHERE organization_id = $1 AND is_deleted = false") {
			t.Errorf("ListArtifacts must filter is_deleted = false, got SQL: %s", capturedSQL)
		}
		if !strings.Contains(capturedSQL, "ORDER BY created_at DESC, id DESC") {
			t.Errorf("ListArtifacts must order by created_at DESC, id DESC, got SQL: %s", capturedSQL)
		}
		if len(capturedArgs) != 1 || capturedArgs[0] != orgID {
			t.Errorf("unexpected arguments: %v", capturedArgs)
		}
	})

	t.Run("TombstoneArtifact updates is_deleted and deleted_at scoped by organization", func(t *testing.T) {
		var capturedSQL string
		var capturedArgs []any

		q := &mockQuerier{
			execFunc: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				capturedSQL = sql
				capturedArgs = args
				return pgconn.NewCommandTag("UPDATE 1"), nil
			},
		}

		repo := NewPostgresBackupRepository(&mockTxManager{querier: q})
		err := repo.TombstoneArtifact(context.Background(), orgID, artID)
		if err != nil {
			t.Fatalf("expected nil error, got: %v", err)
		}

		if !strings.Contains(capturedSQL, "UPDATE backup_artifacts") ||
			!strings.Contains(capturedSQL, "is_deleted = true") ||
			!strings.Contains(capturedSQL, "deleted_at = NOW()") ||
			!strings.Contains(capturedSQL, "WHERE id = $1 AND organization_id = $2") {
			t.Errorf("TombstoneArtifact query mismatch, got SQL: %s", capturedSQL)
		}

		if len(capturedArgs) != 2 || capturedArgs[0] != artID || capturedArgs[1] != orgID {
			t.Errorf("unexpected arguments: %v", capturedArgs)
		}
	})

	t.Run("TombstoneArtifact returns ErrArtifactNotFound on 0 rows affected", func(t *testing.T) {
		q := &mockQuerier{
			execFunc: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				return pgconn.NewCommandTag("UPDATE 0"), nil
			},
		}

		repo := NewPostgresBackupRepository(&mockTxManager{querier: q})
		err := repo.TombstoneArtifact(context.Background(), orgID, artID)
		if !errors.Is(err, domain.ErrArtifactNotFound) {
			t.Fatalf("expected ErrArtifactNotFound, got: %v", err)
		}
	})

	t.Run("ListSuccessfulRunsForPlan is organization-scoped, plan-scoped, filters status=success, and orders by ended_at DESC, id DESC", func(t *testing.T) {
		var capturedSQL string
		var capturedArgs []any

		planID := uuid.New()
		q := &mockQuerier{
			queryFunc: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
				capturedSQL = sql
				capturedArgs = args
				return &mockEmptyRows{}, nil
			},
		}

		repo := NewPostgresBackupRepository(&mockTxManager{querier: q})
		_, err := repo.ListSuccessfulRunsForPlan(context.Background(), orgID, planID)
		if err != nil {
			t.Fatalf("expected nil error, got: %v", err)
		}

		if !strings.Contains(capturedSQL, "WHERE r.organization_id = $1") ||
			!strings.Contains(capturedSQL, "AND j.backup_plan_id = $2") ||
			!strings.Contains(capturedSQL, "AND r.status = 'success'") ||
			!strings.Contains(capturedSQL, "AND r.ended_at IS NOT NULL") {
			t.Errorf("ListSuccessfulRunsForPlan WHERE clause mismatch, got SQL: %s", capturedSQL)
		}

		if !strings.Contains(capturedSQL, "ORDER BY r.ended_at DESC, r.id DESC") {
			t.Errorf("ListSuccessfulRunsForPlan ORDER BY mismatch, got SQL: %s", capturedSQL)
		}

		if len(capturedArgs) != 2 || capturedArgs[0] != orgID || capturedArgs[1] != planID {
			t.Errorf("unexpected arguments: %v", capturedArgs)
		}
	})
}

type mockEmptyRows struct{}

func (r *mockEmptyRows) Close()                                       {}
func (r *mockEmptyRows) Err() error                                   { return nil }
func (r *mockEmptyRows) CommandTag() pgconn.CommandTag                { return pgconn.NewCommandTag("") }
func (r *mockEmptyRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (r *mockEmptyRows) RawValues() [][]byte                          { return nil }
func (r *mockEmptyRows) Values() ([]any, error)                       { return nil, nil }
func (r *mockEmptyRows) Conn() *pgx.Conn                              { return nil }
func (r *mockEmptyRows) Next() bool                                   { return false }
func (r *mockEmptyRows) Scan(dest ...any) error                       { return pgx.ErrNoRows }
