package retention

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	auditDomain "backup-platform/internal/audit/domain"
	"backup-platform/internal/backup/domain"
	"backup-platform/pkg/uuid"
)

type mockRepo struct {
	plans              map[uuid.UUID]*domain.BackupPlan
	successfulRuns     map[uuid.UUID][]*domain.BackupRun
	artifacts          map[uuid.UUID][]*domain.BackupArtifact
	tombstoned         map[uuid.UUID]bool
	getPlanErr         error
	listSuccessfulErr  error
	getRunArtifactsErr error
	tombstoneErr       error
}

func newMockRepo() *mockRepo {
	return &mockRepo{
		plans:          make(map[uuid.UUID]*domain.BackupPlan),
		successfulRuns: make(map[uuid.UUID][]*domain.BackupRun),
		artifacts:      make(map[uuid.UUID][]*domain.BackupArtifact),
		tombstoned:     make(map[uuid.UUID]bool),
	}
}

func (m *mockRepo) GetPlanByID(ctx context.Context, orgID, planID uuid.UUID) (*domain.BackupPlan, error) {
	if m.getPlanErr != nil {
		return nil, m.getPlanErr
	}
	if p, ok := m.plans[planID]; ok && p.OrganizationID == orgID {
		return p, nil
	}
	return nil, domain.ErrPlanNotFound
}

func (m *mockRepo) ListSuccessfulRunsForPlan(ctx context.Context, orgID, planID uuid.UUID) ([]*domain.BackupRun, error) {
	if m.listSuccessfulErr != nil {
		return nil, m.listSuccessfulErr
	}
	runs, ok := m.successfulRuns[planID]
	if !ok {
		return nil, nil
	}
	var filtered []*domain.BackupRun
	for _, r := range runs {
		if r.OrganizationID == orgID && r.Status == domain.RunStatusSuccess {
			filtered = append(filtered, r)
		}
	}
	return filtered, nil
}

func (m *mockRepo) GetRunArtifacts(ctx context.Context, orgID, runID uuid.UUID) ([]*domain.BackupArtifact, error) {
	if m.getRunArtifactsErr != nil {
		return nil, m.getRunArtifactsErr
	}
	return m.artifacts[runID], nil
}

func (m *mockRepo) TombstoneArtifact(ctx context.Context, orgID, artifactID uuid.UUID) error {
	if m.tombstoneErr != nil {
		return m.tombstoneErr
	}
	m.tombstoned[artifactID] = true
	for _, arts := range m.artifacts {
		for _, a := range arts {
			if a.ID == artifactID && a.OrganizationID == orgID {
				a.IsDeleted = true
			}
		}
	}
	return nil
}

type mockStorage struct {
	deleted     []string
	deleteErr   error
	deleteCalls map[string]int
}

func newMockStorage() *mockStorage {
	return &mockStorage{
		deleteCalls: make(map[string]int),
	}
}

func (s *mockStorage) DeleteArtifact(ctx context.Context, storageRef string) error {
	s.deleteCalls[storageRef]++
	if s.deleteErr != nil {
		return s.deleteErr
	}
	s.deleted = append(s.deleted, storageRef)
	return nil
}

type mockAuditRecorder struct {
	logs      []*auditDomain.AuditLog
	recordErr error
}

func (a *mockAuditRecorder) Record(ctx context.Context, entry *auditDomain.AuditLog) error {
	if a.recordErr != nil {
		return a.recordErr
	}
	a.logs = append(a.logs, entry)
	return nil
}

func intPtr(i int) *int {
	return &i
}

func TestRetentionProcessor_NoOpScenarios(t *testing.T) {
	orgID := uuid.New()
	planID := uuid.New()
	currentRunID := uuid.New()

	t.Run("nil plan ID is no-op", func(t *testing.T) {
		repo := newMockRepo()
		store := newMockStorage()
		audit := &mockAuditRecorder{}
		proc := NewProcessor(repo, store, audit, slog.New(slog.NewTextHandler(io.Discard, nil)))

		summary, err := proc.ApplyAfterSuccessfulRun(context.Background(), orgID, nil, currentRunID)
		if err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		if summary.ArtifactsDeleted != 0 || len(store.deleted) != 0 {
			t.Errorf("expected 0 deletions, got %d", summary.ArtifactsDeleted)
		}
	})

	t.Run("nil org ID is no-op", func(t *testing.T) {
		repo := newMockRepo()
		store := newMockStorage()
		audit := &mockAuditRecorder{}
		proc := NewProcessor(repo, store, audit, slog.New(slog.NewTextHandler(io.Discard, nil)))

		summary, err := proc.ApplyAfterSuccessfulRun(context.Background(), uuid.Nil, &planID, currentRunID)
		if err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		if summary.ArtifactsDeleted != 0 {
			t.Errorf("expected 0 deletions, got %d", summary.ArtifactsDeleted)
		}
	})

	t.Run("plan not found returns cleanly", func(t *testing.T) {
		repo := newMockRepo()
		store := newMockStorage()
		audit := &mockAuditRecorder{}
		proc := NewProcessor(repo, store, audit, slog.New(slog.NewTextHandler(io.Discard, nil)))

		summary, err := proc.ApplyAfterSuccessfulRun(context.Background(), orgID, &planID, currentRunID)
		if err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		if summary.ArtifactsDeleted != 0 {
			t.Errorf("expected 0 deletions, got %d", summary.ArtifactsDeleted)
		}
	})

	t.Run("plan without retention policy is no-op", func(t *testing.T) {
		repo := newMockRepo()
		repo.plans[planID] = &domain.BackupPlan{
			ID:             planID,
			OrganizationID: orgID,
			RetentionCount: nil,
			RetentionDays:  nil,
		}
		store := newMockStorage()
		audit := &mockAuditRecorder{}
		proc := NewProcessor(repo, store, audit, slog.New(slog.NewTextHandler(io.Discard, nil)))

		summary, err := proc.ApplyAfterSuccessfulRun(context.Background(), orgID, &planID, currentRunID)
		if err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		if summary.ArtifactsDeleted != 0 {
			t.Errorf("expected 0 deletions, got %d", summary.ArtifactsDeleted)
		}
	})
}

func TestRetentionProcessor_RetentionCountOnly(t *testing.T) {
	orgID := uuid.New()
	planID := uuid.New()
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)

	repo := newMockRepo()
	repo.plans[planID] = &domain.BackupPlan{
		ID:             planID,
		OrganizationID: orgID,
		RetentionCount: intPtr(3), // Keep latest 3 runs
		RetentionDays:  nil,
	}

	// 5 successful runs: ordered newest to oldest
	runs := make([]*domain.BackupRun, 5)
	for i := 0; i < 5; i++ {
		ended := now.Add(-time.Duration(i*10) * time.Minute)
		runID := uuid.New()
		runs[i] = &domain.BackupRun{
			ID:             runID,
			OrganizationID: orgID,
			Status:         domain.RunStatusSuccess,
			EndedAt:        &ended,
		}
		artID := uuid.New()
		repo.artifacts[runID] = []*domain.BackupArtifact{
			{
				ID:               artID,
				OrganizationID:   orgID,
				RunID:            runID,
				StorageReference: "local://" + artID.String(),
				SizeBytes:        1024,
				IsDeleted:        false,
			},
		}
	}
	repo.successfulRuns[planID] = runs

	store := newMockStorage()
	audit := &mockAuditRecorder{}
	proc := NewProcessor(repo, store, audit, slog.New(slog.NewTextHandler(io.Discard, nil)))
	proc.SetNowFunc(func() time.Time { return now })

	currentRunID := runs[0].ID
	summary, err := proc.ApplyAfterSuccessfulRun(context.Background(), orgID, &planID, currentRunID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if summary.RunsEvaluated != 5 {
		t.Errorf("expected 5 evaluated runs, got %d", summary.RunsEvaluated)
	}
	if summary.RunsExpired != 2 {
		t.Errorf("expected 2 expired runs (index 3 and 4), got %d", summary.RunsExpired)
	}
	if summary.ArtifactsDeleted != 2 {
		t.Errorf("expected 2 deleted artifacts, got %d", summary.ArtifactsDeleted)
	}

	// Runs 0, 1, 2 kept; Runs 3, 4 deleted
	for i := 0; i < 3; i++ {
		artID := repo.artifacts[runs[i].ID][0].ID
		if repo.tombstoned[artID] {
			t.Errorf("run %d artifact should be kept", i)
		}
	}
	for i := 3; i < 5; i++ {
		artID := repo.artifacts[runs[i].ID][0].ID
		if !repo.tombstoned[artID] {
			t.Errorf("run %d artifact should be tombstoned", i)
		}
	}
}

func TestRetentionProcessor_RetentionDaysOnly_Boundary(t *testing.T) {
	orgID := uuid.New()
	planID := uuid.New()
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)

	repo := newMockRepo()
	repo.plans[planID] = &domain.BackupPlan{
		ID:             planID,
		OrganizationID: orgID,
		RetentionCount: nil,
		RetentionDays:  intPtr(7), // Keep runs within 7 days
	}

	// Runs at various ages:
	// Run 0: 1 day ago (Kept)
	// Run 1: 5 days ago (Kept)
	// Run 2: exactly 7 days ago (Boundary -> Kept)
	// Run 3: 7 days + 1 minute ago (Expired)
	// Run 4: 14 days ago (Expired)
	r0Ended := now.Add(-1 * 24 * time.Hour)
	r1Ended := now.Add(-5 * 24 * time.Hour)
	r2Ended := now.Add(-7 * 24 * time.Hour) // Exact boundary
	r3Ended := now.Add(-7*24*time.Hour - 1*time.Minute)
	r4Ended := now.Add(-14 * 24 * time.Hour)

	runs := []*domain.BackupRun{
		{ID: uuid.New(), OrganizationID: orgID, Status: domain.RunStatusSuccess, EndedAt: &r0Ended},
		{ID: uuid.New(), OrganizationID: orgID, Status: domain.RunStatusSuccess, EndedAt: &r1Ended},
		{ID: uuid.New(), OrganizationID: orgID, Status: domain.RunStatusSuccess, EndedAt: &r2Ended},
		{ID: uuid.New(), OrganizationID: orgID, Status: domain.RunStatusSuccess, EndedAt: &r3Ended},
		{ID: uuid.New(), OrganizationID: orgID, Status: domain.RunStatusSuccess, EndedAt: &r4Ended},
	}

	for _, r := range runs {
		artID := uuid.New()
		repo.artifacts[r.ID] = []*domain.BackupArtifact{
			{
				ID:               artID,
				OrganizationID:   orgID,
				RunID:            r.ID,
				StorageReference: "local://" + artID.String(),
				SizeBytes:        2048,
				IsDeleted:        false,
			},
		}
	}
	repo.successfulRuns[planID] = runs

	store := newMockStorage()
	audit := &mockAuditRecorder{}
	proc := NewProcessor(repo, store, audit, slog.New(slog.NewTextHandler(io.Discard, nil)))
	proc.SetNowFunc(func() time.Time { return now })

	currentRunID := runs[0].ID
	summary, err := proc.ApplyAfterSuccessfulRun(context.Background(), orgID, &planID, currentRunID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if summary.RunsExpired != 2 {
		t.Errorf("expected 2 expired runs (run 3 and run 4), got %d", summary.RunsExpired)
	}
	if summary.ArtifactsDeleted != 2 {
		t.Errorf("expected 2 deleted artifacts, got %d", summary.ArtifactsDeleted)
	}

	// Exact boundary (run 2) must be kept
	if repo.tombstoned[repo.artifacts[runs[2].ID][0].ID] {
		t.Errorf("exact 7-day boundary run should be KEPT")
	}
}

func TestRetentionProcessor_ConservativeOR(t *testing.T) {
	orgID := uuid.New()
	planID := uuid.New()
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)

	repo := newMockRepo()
	repo.plans[planID] = &domain.BackupPlan{
		ID:             planID,
		OrganizationID: orgID,
		RetentionCount: intPtr(2), // Keep latest 2 runs
		RetentionDays:  intPtr(5), // Keep runs within 5 days
	}

	// Runs:
	// Run 0: 1 day ago -> index 0 (<2), age 1d (<5d) -> KEPT by both
	// Run 1: 3 days ago -> index 1 (<2), age 3d (<5d) -> KEPT by both
	// Run 2: 4 days ago -> index 2 (>=2), age 4d (<5d) -> KEPT by days (OR semantics)!
	// Run 3: 10 days ago -> index 3 (>=2), age 10d (>5d) -> EXPIRED by both!
	r0Ended := now.Add(-1 * 24 * time.Hour)
	r1Ended := now.Add(-3 * 24 * time.Hour)
	r2Ended := now.Add(-4 * 24 * time.Hour)
	r3Ended := now.Add(-10 * 24 * time.Hour)

	runs := []*domain.BackupRun{
		{ID: uuid.New(), OrganizationID: orgID, Status: domain.RunStatusSuccess, EndedAt: &r0Ended},
		{ID: uuid.New(), OrganizationID: orgID, Status: domain.RunStatusSuccess, EndedAt: &r1Ended},
		{ID: uuid.New(), OrganizationID: orgID, Status: domain.RunStatusSuccess, EndedAt: &r2Ended},
		{ID: uuid.New(), OrganizationID: orgID, Status: domain.RunStatusSuccess, EndedAt: &r3Ended},
	}

	for _, r := range runs {
		artID := uuid.New()
		repo.artifacts[r.ID] = []*domain.BackupArtifact{
			{
				ID:               artID,
				OrganizationID:   orgID,
				RunID:            r.ID,
				StorageReference: "local://" + artID.String(),
				SizeBytes:        512,
				IsDeleted:        false,
			},
		}
	}
	repo.successfulRuns[planID] = runs

	store := newMockStorage()
	audit := &mockAuditRecorder{}
	proc := NewProcessor(repo, store, audit, slog.New(slog.NewTextHandler(io.Discard, nil)))
	proc.SetNowFunc(func() time.Time { return now })

	currentRunID := runs[0].ID
	summary, err := proc.ApplyAfterSuccessfulRun(context.Background(), orgID, &planID, currentRunID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if summary.RunsExpired != 1 {
		t.Errorf("expected exactly 1 expired run (run 3), got %d", summary.RunsExpired)
	}

	// Run 2 must be KEPT because it is within 5 days even though it is outside keep_last_2
	if repo.tombstoned[repo.artifacts[runs[2].ID][0].ID] {
		t.Errorf("run 2 should be KEPT by days rule under conservative OR semantics")
	}

	// Run 3 must be tombstoned
	if !repo.tombstoned[repo.artifacts[runs[3].ID][0].ID] {
		t.Errorf("run 3 should be tombstoned")
	}
}

func TestRetentionProcessor_OldRunsPreservedByCount(t *testing.T) {
	orgID := uuid.New()
	planID := uuid.New()
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)

	repo := newMockRepo()
	repo.plans[planID] = &domain.BackupPlan{
		ID:             planID,
		OrganizationID: orgID,
		RetentionCount: intPtr(5), // Keep latest 5 runs
		RetentionDays:  intPtr(1), // Keep runs within 1 day
	}

	// 3 runs exist from 10, 20, 30 days ago.
	// All are > 1 day old, but all 3 are within keep_last_5.
	// Conservative OR semantics: all 3 must be KEPT!
	r0Ended := now.Add(-10 * 24 * time.Hour)
	r1Ended := now.Add(-20 * 24 * time.Hour)
	r2Ended := now.Add(-30 * 24 * time.Hour)

	runs := []*domain.BackupRun{
		{ID: uuid.New(), OrganizationID: orgID, Status: domain.RunStatusSuccess, EndedAt: &r0Ended},
		{ID: uuid.New(), OrganizationID: orgID, Status: domain.RunStatusSuccess, EndedAt: &r1Ended},
		{ID: uuid.New(), OrganizationID: orgID, Status: domain.RunStatusSuccess, EndedAt: &r2Ended},
	}

	for _, r := range runs {
		artID := uuid.New()
		repo.artifacts[r.ID] = []*domain.BackupArtifact{
			{
				ID:               artID,
				OrganizationID:   orgID,
				RunID:            r.ID,
				StorageReference: "local://" + artID.String(),
				SizeBytes:        512,
				IsDeleted:        false,
			},
		}
	}
	repo.successfulRuns[planID] = runs

	store := newMockStorage()
	audit := &mockAuditRecorder{}
	proc := NewProcessor(repo, store, audit, slog.New(slog.NewTextHandler(io.Discard, nil)))
	proc.SetNowFunc(func() time.Time { return now })

	currentRunID := runs[0].ID
	summary, err := proc.ApplyAfterSuccessfulRun(context.Background(), orgID, &planID, currentRunID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if summary.RunsExpired != 0 {
		t.Errorf("expected 0 expired runs, got %d", summary.RunsExpired)
	}
	if summary.ArtifactsDeleted != 0 {
		t.Errorf("expected 0 deletions, got %d", summary.ArtifactsDeleted)
	}
}

func TestRetentionProcessor_CurrentRunProtectionInvariant(t *testing.T) {
	orgID := uuid.New()
	planID := uuid.New()
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)

	repo := newMockRepo()
	repo.plans[planID] = &domain.BackupPlan{
		ID:             planID,
		OrganizationID: orgID,
		RetentionCount: intPtr(1),
		RetentionDays:  intPtr(1),
	}

	oldTime := now.Add(-30 * 24 * time.Hour)
	currentRunID := uuid.New()
	currentRun := &domain.BackupRun{
		ID:             currentRunID,
		OrganizationID: orgID,
		Status:         domain.RunStatusSuccess,
		EndedAt:        &oldTime, // Even if old endedAt timestamp
	}

	artID := uuid.New()
	repo.artifacts[currentRunID] = []*domain.BackupArtifact{
		{
			ID:               artID,
			OrganizationID:   orgID,
			RunID:            currentRunID,
			StorageReference: "local://" + artID.String(),
			SizeBytes:        1024,
			IsDeleted:        false,
		},
	}
	repo.successfulRuns[planID] = []*domain.BackupRun{currentRun}

	store := newMockStorage()
	audit := &mockAuditRecorder{}
	proc := NewProcessor(repo, store, audit, slog.New(slog.NewTextHandler(io.Discard, nil)))
	proc.SetNowFunc(func() time.Time { return now })

	summary, err := proc.ApplyAfterSuccessfulRun(context.Background(), orgID, &planID, currentRunID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if summary.ArtifactsDeleted != 0 || repo.tombstoned[artID] {
		t.Errorf("current run must NEVER be deleted in its own retention invocation")
	}
}

func TestRetentionProcessor_MultiArtifactRunCleanup(t *testing.T) {
	orgID := uuid.New()
	planID := uuid.New()
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)

	repo := newMockRepo()
	repo.plans[planID] = &domain.BackupPlan{
		ID:             planID,
		OrganizationID: orgID,
		RetentionCount: intPtr(1),
	}

	r0Ended := now.Add(-1 * time.Hour)
	r1Ended := now.Add(-2 * time.Hour)

	run0 := &domain.BackupRun{ID: uuid.New(), OrganizationID: orgID, Status: domain.RunStatusSuccess, EndedAt: &r0Ended}
	run1 := &domain.BackupRun{ID: uuid.New(), OrganizationID: orgID, Status: domain.RunStatusSuccess, EndedAt: &r1Ended}

	art1 := &domain.BackupArtifact{
		ID:               uuid.New(),
		OrganizationID:   orgID,
		RunID:            run1.ID,
		StorageReference: "local://db.sql.gz",
		SizeBytes:        1000,
		IsDeleted:        false,
	}
	art2 := &domain.BackupArtifact{
		ID:               uuid.New(),
		OrganizationID:   orgID,
		RunID:            run1.ID,
		StorageReference: "local://files.tar.gz",
		SizeBytes:        2000,
		IsDeleted:        false,
	}

	repo.artifacts[run1.ID] = []*domain.BackupArtifact{art1, art2}
	repo.successfulRuns[planID] = []*domain.BackupRun{run0, run1}

	store := newMockStorage()
	audit := &mockAuditRecorder{}
	proc := NewProcessor(repo, store, audit, slog.New(slog.NewTextHandler(io.Discard, nil)))
	proc.SetNowFunc(func() time.Time { return now })

	summary, err := proc.ApplyAfterSuccessfulRun(context.Background(), orgID, &planID, run0.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if summary.ArtifactsDeleted != 2 {
		t.Errorf("expected 2 deleted artifacts for multi-artifact run, got %d", summary.ArtifactsDeleted)
	}
	if !repo.tombstoned[art1.ID] || !repo.tombstoned[art2.ID] {
		t.Errorf("both artifacts must be tombstoned")
	}
	if len(audit.logs) != 2 {
		t.Errorf("expected 2 audit logs, got %d", len(audit.logs))
	}
}

func TestRetentionProcessor_AlreadyDeletedArtifactSkipped(t *testing.T) {
	orgID := uuid.New()
	planID := uuid.New()
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)

	repo := newMockRepo()
	repo.plans[planID] = &domain.BackupPlan{
		ID:             planID,
		OrganizationID: orgID,
		RetentionCount: intPtr(1),
	}

	r0Ended := now.Add(-1 * time.Hour)
	r1Ended := now.Add(-2 * time.Hour)

	run0 := &domain.BackupRun{ID: uuid.New(), OrganizationID: orgID, Status: domain.RunStatusSuccess, EndedAt: &r0Ended}
	run1 := &domain.BackupRun{ID: uuid.New(), OrganizationID: orgID, Status: domain.RunStatusSuccess, EndedAt: &r1Ended}

	artAlreadyDeleted := &domain.BackupArtifact{
		ID:               uuid.New(),
		OrganizationID:   orgID,
		RunID:            run1.ID,
		StorageReference: "local://already-deleted",
		SizeBytes:        1000,
		IsDeleted:        true, // Already deleted
	}

	repo.artifacts[run1.ID] = []*domain.BackupArtifact{artAlreadyDeleted}
	repo.successfulRuns[planID] = []*domain.BackupRun{run0, run1}

	store := newMockStorage()
	audit := &mockAuditRecorder{}
	proc := NewProcessor(repo, store, audit, slog.New(slog.NewTextHandler(io.Discard, nil)))
	proc.SetNowFunc(func() time.Time { return now })

	summary, err := proc.ApplyAfterSuccessfulRun(context.Background(), orgID, &planID, run0.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if summary.ArtifactsDeleted != 0 {
		t.Errorf("expected 0 deleted artifacts, got %d", summary.ArtifactsDeleted)
	}
	if len(store.deleted) != 0 {
		t.Errorf("storage delete should not be called for already deleted artifact")
	}
}

func TestRetentionProcessor_PhysicalStorageDeleteFailure(t *testing.T) {
	orgID := uuid.New()
	planID := uuid.New()
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)

	repo := newMockRepo()
	repo.plans[planID] = &domain.BackupPlan{
		ID:             planID,
		OrganizationID: orgID,
		RetentionCount: intPtr(1),
	}

	r0Ended := now.Add(-1 * time.Hour)
	r1Ended := now.Add(-2 * time.Hour)

	run0 := &domain.BackupRun{ID: uuid.New(), OrganizationID: orgID, Status: domain.RunStatusSuccess, EndedAt: &r0Ended}
	run1 := &domain.BackupRun{ID: uuid.New(), OrganizationID: orgID, Status: domain.RunStatusSuccess, EndedAt: &r1Ended}

	artFail := &domain.BackupArtifact{
		ID:               uuid.New(),
		OrganizationID:   orgID,
		RunID:            run1.ID,
		StorageReference: "local://fail",
		SizeBytes:        1000,
		IsDeleted:        false,
	}

	repo.artifacts[run1.ID] = []*domain.BackupArtifact{artFail}
	repo.successfulRuns[planID] = []*domain.BackupRun{run0, run1}

	store := newMockStorage()
	store.deleteErr = errors.New("disk I/O error")
	audit := &mockAuditRecorder{}
	proc := NewProcessor(repo, store, audit, slog.New(slog.NewTextHandler(io.Discard, nil)))
	proc.SetNowFunc(func() time.Time { return now })

	summary, err := proc.ApplyAfterSuccessfulRun(context.Background(), orgID, &planID, run0.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Physical failure -> DO NOT tombstone, DO NOT audit
	if repo.tombstoned[artFail.ID] {
		t.Errorf("artifact must NOT be tombstoned when physical deletion fails")
	}
	if len(audit.logs) != 0 {
		t.Errorf("audit must NOT be emitted when physical deletion fails")
	}
	if summary.ArtifactsDeleted != 0 {
		t.Errorf("expected 0 successful deletions, got %d", summary.ArtifactsDeleted)
	}
}

func TestRetentionProcessor_DatabaseTombstoneFailure(t *testing.T) {
	orgID := uuid.New()
	planID := uuid.New()
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)

	repo := newMockRepo()
	repo.plans[planID] = &domain.BackupPlan{
		ID:             planID,
		OrganizationID: orgID,
		RetentionCount: intPtr(1),
	}

	r0Ended := now.Add(-1 * time.Hour)
	r1Ended := now.Add(-2 * time.Hour)

	run0 := &domain.BackupRun{ID: uuid.New(), OrganizationID: orgID, Status: domain.RunStatusSuccess, EndedAt: &r0Ended}
	run1 := &domain.BackupRun{ID: uuid.New(), OrganizationID: orgID, Status: domain.RunStatusSuccess, EndedAt: &r1Ended}

	art := &domain.BackupArtifact{
		ID:               uuid.New(),
		OrganizationID:   orgID,
		RunID:            run1.ID,
		StorageReference: "local://art",
		SizeBytes:        1000,
		IsDeleted:        false,
	}

	repo.artifacts[run1.ID] = []*domain.BackupArtifact{art}
	repo.successfulRuns[planID] = []*domain.BackupRun{run0, run1}
	repo.tombstoneErr = errors.New("db deadlocked")

	store := newMockStorage()
	audit := &mockAuditRecorder{}
	proc := NewProcessor(repo, store, audit, slog.New(slog.NewTextHandler(io.Discard, nil)))
	proc.SetNowFunc(func() time.Time { return now })

	summary, err := proc.ApplyAfterSuccessfulRun(context.Background(), orgID, &planID, run0.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(audit.logs) != 0 {
		t.Errorf("audit must NOT be emitted when database tombstone fails")
	}
	if summary.ArtifactsDeleted != 0 {
		t.Errorf("expected 0 successful deletions, got %d", summary.ArtifactsDeleted)
	}
}

func TestRetentionProcessor_AuditMetadataSanitization(t *testing.T) {
	orgID := uuid.New()
	planID := uuid.New()
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)

	repo := newMockRepo()
	repo.plans[planID] = &domain.BackupPlan{
		ID:             planID,
		OrganizationID: orgID,
		RetentionCount: intPtr(1),
		RetentionDays:  intPtr(30),
	}

	r0Ended := now.Add(-1 * time.Hour)
	r1Ended := now.Add(-35 * 24 * time.Hour)

	run0 := &domain.BackupRun{ID: uuid.New(), OrganizationID: orgID, Status: domain.RunStatusSuccess, EndedAt: &r0Ended}
	run1 := &domain.BackupRun{ID: uuid.New(), OrganizationID: orgID, Status: domain.RunStatusSuccess, EndedAt: &r1Ended}

	art := &domain.BackupArtifact{
		ID:               uuid.New(),
		OrganizationID:   orgID,
		RunID:            run1.ID,
		StorageReference: "local:///srv/backup-platform/artifacts/sensitive_file.sql.gz",
		SizeBytes:        4096,
		IsDeleted:        false,
	}

	repo.artifacts[run1.ID] = []*domain.BackupArtifact{art}
	repo.successfulRuns[planID] = []*domain.BackupRun{run0, run1}

	store := newMockStorage()
	audit := &mockAuditRecorder{}
	proc := NewProcessor(repo, store, audit, slog.New(slog.NewTextHandler(io.Discard, nil)))
	proc.SetNowFunc(func() time.Time { return now })

	summary, err := proc.ApplyAfterSuccessfulRun(context.Background(), orgID, &planID, run0.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if summary.ArtifactsDeleted != 1 {
		t.Fatalf("expected 1 deleted artifact, got %d", summary.ArtifactsDeleted)
	}

	if len(audit.logs) != 1 {
		t.Fatalf("expected 1 audit log, got %d", len(audit.logs))
	}

	entry := audit.logs[0]
	if entry.Action != auditDomain.ActionRetentionCleanup {
		t.Errorf("expected action %q, got %q", auditDomain.ActionRetentionCleanup, entry.Action)
	}
	if entry.EntityType != auditDomain.EntityTypeBackupArtifact {
		t.Errorf("expected entity_type %q, got %q", auditDomain.EntityTypeBackupArtifact, entry.EntityType)
	}
	if entry.UserID != nil {
		t.Errorf("expected nil UserID for system retention cleanup, got %v", entry.UserID)
	}
	if entry.IPAddress != nil || entry.UserAgent != nil {
		t.Errorf("expected nil IPAddress and UserAgent for retention cleanup")
	}

	var meta map[string]any
	if err := json.Unmarshal(entry.Metadata, &meta); err != nil {
		t.Fatalf("failed unmarshaling audit metadata: %v", err)
	}

	if meta["artifact_id"] != art.ID.String() {
		t.Errorf("metadata artifact_id mismatch")
	}
	if meta["backup_plan_id"] != planID.String() {
		t.Errorf("metadata backup_plan_id mismatch")
	}
	if meta["run_id"] != run1.ID.String() {
		t.Errorf("metadata run_id mismatch")
	}
	if float64(art.SizeBytes) != meta["size_bytes"].(float64) {
		t.Errorf("metadata size_bytes mismatch")
	}
	if float64(1) != meta["retention_count"].(float64) {
		t.Errorf("metadata retention_count mismatch")
	}
	if float64(30) != meta["retention_days"].(float64) {
		t.Errorf("metadata retention_days mismatch")
	}

	// Must NOT contain sensitive file paths or storage reference
	if _, exists := meta["storage_reference"]; exists {
		t.Errorf("metadata must not contain storage_reference")
	}
	if _, exists := meta["path"]; exists {
		t.Errorf("metadata must not contain path")
	}
}

func TestRetentionProcessor_TenantAndPlanIsolation(t *testing.T) {
	orgA := uuid.New()
	orgB := uuid.New()
	planA := uuid.New()
	planB := uuid.New()
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)

	repo := newMockRepo()
	repo.plans[planA] = &domain.BackupPlan{
		ID:             planA,
		OrganizationID: orgA,
		RetentionCount: intPtr(1),
	}
	repo.plans[planB] = &domain.BackupPlan{
		ID:             planB,
		OrganizationID: orgB,
		RetentionCount: intPtr(1),
	}

	rA0Ended := now.Add(-1 * time.Hour)
	rA1Ended := now.Add(-2 * time.Hour)
	runA0 := &domain.BackupRun{ID: uuid.New(), OrganizationID: orgA, Status: domain.RunStatusSuccess, EndedAt: &rA0Ended}
	runA1 := &domain.BackupRun{ID: uuid.New(), OrganizationID: orgA, Status: domain.RunStatusSuccess, EndedAt: &rA1Ended}

	rB0Ended := now.Add(-1 * time.Hour)
	rB1Ended := now.Add(-2 * time.Hour)
	runB0 := &domain.BackupRun{ID: uuid.New(), OrganizationID: orgB, Status: domain.RunStatusSuccess, EndedAt: &rB0Ended}
	runB1 := &domain.BackupRun{ID: uuid.New(), OrganizationID: orgB, Status: domain.RunStatusSuccess, EndedAt: &rB1Ended}

	artA1 := &domain.BackupArtifact{ID: uuid.New(), OrganizationID: orgA, RunID: runA1.ID, StorageReference: "local://artA1"}
	artB1 := &domain.BackupArtifact{ID: uuid.New(), OrganizationID: orgB, RunID: runB1.ID, StorageReference: "local://artB1"}

	repo.artifacts[runA1.ID] = []*domain.BackupArtifact{artA1}
	repo.artifacts[runB1.ID] = []*domain.BackupArtifact{artB1}

	repo.successfulRuns[planA] = []*domain.BackupRun{runA0, runA1}
	repo.successfulRuns[planB] = []*domain.BackupRun{runB0, runB1}

	store := newMockStorage()
	audit := &mockAuditRecorder{}
	proc := NewProcessor(repo, store, audit, slog.New(slog.NewTextHandler(io.Discard, nil)))
	proc.SetNowFunc(func() time.Time { return now })

	// Process Org A retention
	summaryA, err := proc.ApplyAfterSuccessfulRun(context.Background(), orgA, &planA, runA0.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if summaryA.ArtifactsDeleted != 1 {
		t.Errorf("expected 1 deleted artifact in org A, got %d", summaryA.ArtifactsDeleted)
	}
	if !repo.tombstoned[artA1.ID] {
		t.Errorf("artA1 should be tombstoned")
	}
	if repo.tombstoned[artB1.ID] {
		t.Errorf("artB1 in org B must NOT be touched by org A retention processing")
	}
}
