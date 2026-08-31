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
