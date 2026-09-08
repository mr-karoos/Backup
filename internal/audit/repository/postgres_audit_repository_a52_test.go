package repository

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"backup-platform/internal/audit/domain"
	"backup-platform/internal/platform/database"
	"backup-platform/internal/platform/migrations"
	"backup-platform/pkg/uuid"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5"
)

func TestPostgresAuditRepository_A52_Integration(t *testing.T) {
	testDBURL := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	if testDBURL == "" {
		t.Skip("skipping Step A.5.2 audit repository integration test: TEST_DATABASE_URL not set")
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

	if err := m.Migrate(10); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		t.Fatalf("failed migrating to version 10: %v", err)
	}

	pool, err := database.New(ctx, testDBURL)
	if err != nil {
		t.Fatalf("failed creating database pool: %v", err)
	}
	defer pool.Close()

	repo := NewPostgresAuditRepository(pool)

	orgA := uuid.New()
	orgB := uuid.New()
	userA := uuid.New()
	userB := uuid.New()
	entityA := uuid.New()

	cleanup := func() {
		cleanupCtx := context.Background()
		_, _ = conn.Exec(cleanupCtx, "DELETE FROM audit_logs WHERE organization_id IN ($1, $2) OR action LIKE 'test.a52.%'", orgA, orgB)
		_, _ = conn.Exec(cleanupCtx, "DELETE FROM users WHERE id IN ($1, $2)", userA, userB)
		_, _ = conn.Exec(cleanupCtx, "DELETE FROM organizations WHERE id IN ($1, $2)", orgA, orgB)
	}
	cleanup()
	defer cleanup()

	// Seed organizations
	now := time.Now().UTC().Truncate(time.Microsecond)
	_, err = conn.Exec(ctx, `INSERT INTO organizations (id, name, slug, created_at, updated_at) VALUES ($1, 'Org A', $2, $3, $3), ($4, 'Org B', $5, $3, $3)`,
		orgA, "org-a-"+orgA.String()[:8], now,
		orgB, "org-b-"+orgB.String()[:8],
	)
	if err != nil {
		t.Fatalf("failed creating test organizations: %v", err)
	}

	// Seed users
	_, err = conn.Exec(ctx, `INSERT INTO users (id, email, password_hash, full_name, created_at, updated_at) VALUES ($1, $2, 'hash', 'User A', $3, $3), ($4, $5, 'hash', 'User B', $3, $3)`,
		userA, "usera-"+userA.String()[:8]+"@test.local", now,
		userB, "userb-"+userB.String()[:8]+"@test.local",
	)
	if err != nil {
		t.Fatalf("failed creating test users: %v", err)
	}

	// 1. Seed Org A with exactly 105 audit rows
	// To test tie-breaking on created_at, multiple rows have the same created_at
	baseTime := now.Add(-2 * time.Hour)
	tieBreakTime := baseTime.Add(30 * time.Minute)

	type seededAuditRow struct {
		id         uuid.UUID
		orgID      *uuid.UUID
		userID     *uuid.UUID
		action     string
		entityType string
		entityID   *uuid.UUID
		createdAt  time.Time
	}
	var orgASeeded []seededAuditRow
	var orgBSeeded []seededAuditRow

	orgARowIDs := make(map[uuid.UUID]bool)
	for i := 0; i < 105; i++ {
		rowID := uuid.New()
		orgARowIDs[rowID] = true
		rowTime := baseTime.Add(time.Duration(i) * time.Second)
		if i >= 10 && i < 15 {
			// Rows 10..14 share the exact same timestamp to test secondary sort on id DESC
			rowTime = tieBreakTime
		}

		action := domain.ActionBackupDownload
		if i%2 == 0 {
			action = domain.ActionBackupDelete
		}
		entityType := domain.EntityTypeBackupArtifact
		if i%3 == 0 {
			entityType = domain.EntityTypeResource
		}

		entID := entityA
		if i%5 == 0 {
			entID = uuid.New()
		}

		ip := fmt.Sprintf("192.168.1.%d", (i%250)+1)
		ua := "test-agent-a52"
		meta := fmt.Sprintf(`{"index":%d,"seed":"orgA"}`, i)

		_, err = conn.Exec(ctx, `
			INSERT INTO audit_logs (id, organization_id, user_id, action, entity_type, entity_id, ip_address, user_agent, metadata, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		`, rowID, orgA, userA, action, entityType, entID, ip, ua, meta, rowTime)
		if err != nil {
			t.Fatalf("failed inserting Org A audit row %d: %v", i, err)
		}

		uID := userA
		eID := entID
		oID := orgA
		orgASeeded = append(orgASeeded, seededAuditRow{
			id:         rowID,
			orgID:      &oID,
			userID:     &uID,
			action:     action,
			entityType: entityType,
			entityID:   &eID,
			createdAt:  rowTime,
		})
	}

	// 2. Seed Org B with 10 audit rows
	for i := 0; i < 10; i++ {
		rowID := uuid.New()
		rowTime := baseTime.Add(time.Duration(i) * time.Minute)
		entID := uuid.New()
		_, err = conn.Exec(ctx, `
			INSERT INTO audit_logs (id, organization_id, user_id, action, entity_type, entity_id, ip_address, user_agent, metadata, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		`, rowID, orgB, userB, domain.ActionResourceCreate, domain.EntityTypeResource, entID, "10.0.0.1", "agent-b", `{"org":"B"}`, rowTime)
		if err != nil {
			t.Fatalf("failed inserting Org B audit row %d: %v", i, err)
		}

		uID := userB
		eID := entID
		oID := orgB
		orgBSeeded = append(orgBSeeded, seededAuditRow{
			id:         rowID,
			orgID:      &oID,
			userID:     &uID,
			action:     domain.ActionResourceCreate,
			entityType: domain.EntityTypeResource,
			entityID:   &eID,
			createdAt:  rowTime,
		})
	}

	// 3. Seed at least 2 global rows with organization_id = NULL
	for i := 0; i < 3; i++ {
		rowID := uuid.New()
		rowTime := baseTime.Add(time.Duration(i*5) * time.Minute)
		_, err = conn.Exec(ctx, `
			INSERT INTO audit_logs (id, organization_id, user_id, action, entity_type, entity_id, ip_address, user_agent, metadata, created_at)
			VALUES ($1, NULL, NULL, 'test.a52.global.event', 'system', $2, '127.0.0.1', 'system-daemon', '{"global":true}', $3)
		`, rowID, uuid.New(), rowTime)
		if err != nil {
			t.Fatalf("failed inserting global audit row %d: %v", i, err)
		}
	}

	t.Run("limit=50 paginates 105 rows in 3 pages (50 + 50 + 5) without duplicates or gaps", func(t *testing.T) {
		var collectedIDs []uuid.UUID
		var currentCursor *string

		// Page 1 (limit 50)
		p1Logs, p1HasMore, err := repo.ListPaginated(ctx, orgA, domain.AuditLogFilter{Limit: 50})
		if err != nil {
			t.Fatalf("page 1 failed: %v", err)
		}
		if len(p1Logs) != 50 {
			t.Fatalf("expected page 1 to have 50 items, got %d", len(p1Logs))
		}
		if !p1HasMore {
			t.Fatalf("expected page 1 has_more = true")
		}
		for _, l := range p1Logs {
			collectedIDs = append(collectedIDs, l.ID)
		}
		cur := domain.EncodeAuditCursor(p1Logs[len(p1Logs)-1].CreatedAt, p1Logs[len(p1Logs)-1].ID)
		currentCursor = &cur

		// Page 2 (limit 50)
		p2Time, p2ID, err := domain.DecodeAuditCursor(*currentCursor)
		if err != nil {
			t.Fatalf("failed decoding cursor for page 2: %v", err)
		}
		p2Logs, p2HasMore, err := repo.ListPaginated(ctx, orgA, domain.AuditLogFilter{
			Limit:           50,
			CursorCreatedAt: &p2Time,
			CursorID:        &p2ID,
		})
		if err != nil {
			t.Fatalf("page 2 failed: %v", err)
		}
		if len(p2Logs) != 50 {
			t.Fatalf("expected page 2 to have 50 items, got %d", len(p2Logs))
		}
		if !p2HasMore {
			t.Fatalf("expected page 2 has_more = true")
		}
		for _, l := range p2Logs {
			collectedIDs = append(collectedIDs, l.ID)
		}
		cur = domain.EncodeAuditCursor(p2Logs[len(p2Logs)-1].CreatedAt, p2Logs[len(p2Logs)-1].ID)
		currentCursor = &cur

		// Page 3 (limit 50, remaining 5)
		p3Time, p3ID, err := domain.DecodeAuditCursor(*currentCursor)
		if err != nil {
			t.Fatalf("failed decoding cursor for page 3: %v", err)
		}
		p3Logs, p3HasMore, err := repo.ListPaginated(ctx, orgA, domain.AuditLogFilter{
			Limit:           50,
			CursorCreatedAt: &p3Time,
			CursorID:        &p3ID,
		})
		if err != nil {
			t.Fatalf("page 3 failed: %v", err)
		}
		if len(p3Logs) != 5 {
			t.Fatalf("expected page 3 to have 5 items, got %d", len(p3Logs))
		}
		if p3HasMore {
			t.Fatalf("expected page 3 has_more = false")
		}
		for _, l := range p3Logs {
			collectedIDs = append(collectedIDs, l.ID)
		}

		if len(collectedIDs) != 105 {
			t.Fatalf("expected 105 collected items, got %d", len(collectedIDs))
		}

		// Verify no duplicates and no missing items
		seen := make(map[uuid.UUID]bool)
		for _, id := range collectedIDs {
			if seen[id] {
				t.Fatalf("DUPLICATE row detected in pagination: %s", id)
			}
			seen[id] = true
			if !orgARowIDs[id] {
				t.Fatalf("UNEXPECTED row ID in Org A results: %s", id)
			}
		}
	})

	t.Run("limit=100 paginates 105 rows in 2 pages (100 + 5)", func(t *testing.T) {
		p1Logs, p1HasMore, err := repo.ListPaginated(ctx, orgA, domain.AuditLogFilter{Limit: 100})
		if err != nil {
			t.Fatalf("page 1 failed: %v", err)
		}
		if len(p1Logs) != 100 {
			t.Fatalf("expected page 1 to have 100 items, got %d", len(p1Logs))
		}
		if !p1HasMore {
			t.Fatalf("expected page 1 has_more = true")
		}

		cur := domain.EncodeAuditCursor(p1Logs[len(p1Logs)-1].CreatedAt, p1Logs[len(p1Logs)-1].ID)
		p2Time, p2ID, err := domain.DecodeAuditCursor(cur)
		if err != nil {
			t.Fatalf("failed decoding cursor: %v", err)
		}

		p2Logs, p2HasMore, err := repo.ListPaginated(ctx, orgA, domain.AuditLogFilter{
			Limit:           100,
			CursorCreatedAt: &p2Time,
			CursorID:        &p2ID,
		})
		if err != nil {
			t.Fatalf("page 2 failed: %v", err)
		}
		if len(p2Logs) != 5 {
			t.Fatalf("expected page 2 to have 5 items, got %d", len(p2Logs))
		}
		if p2HasMore {
			t.Fatalf("expected page 2 has_more = false")
		}
	})

	t.Run("strict ordering by (created_at DESC, id DESC)", func(t *testing.T) {
		logs, _, err := repo.ListPaginated(ctx, orgA, domain.AuditLogFilter{Limit: 100})
		if err != nil {
			t.Fatalf("failed listing: %v", err)
		}
		for i := 1; i < len(logs); i++ {
			prev := logs[i-1]
			curr := logs[i]
			if curr.CreatedAt.After(prev.CreatedAt) {
				t.Fatalf("ordering violation at index %d: curr.CreatedAt %v is after prev.CreatedAt %v", i, curr.CreatedAt, prev.CreatedAt)
			}
			if curr.CreatedAt.Equal(prev.CreatedAt) {
				if curr.ID.String() >= prev.ID.String() {
					t.Fatalf("tie-breaking ordering violation on same timestamp: curr.ID %s >= prev.ID %s", curr.ID, prev.ID)
				}
			}
		}
	})

	t.Run("tenant isolation: Org A never sees Org B or global rows", func(t *testing.T) {
		logs, _, err := repo.ListPaginated(ctx, orgA, domain.AuditLogFilter{Limit: 100})
		if err != nil {
			t.Fatalf("failed listing: %v", err)
		}
		for _, l := range logs {
			if l.OrganizationID == nil {
				t.Fatalf("SECURITY VIOLATION: global audit row (orgID=nil) returned to Org A: %+v", l)
			}
			if *l.OrganizationID != orgA {
				t.Fatalf("SECURITY VIOLATION: Org B row returned to Org A: %+v", l)
			}
			if strings.HasPrefix(l.Action, "test.a52.global") {
				t.Fatalf("SECURITY VIOLATION: global action returned to Org A: %s", l.Action)
			}
		}
	})

	t.Run("filter: action", func(t *testing.T) {
		targetAction := domain.ActionBackupDownload
		expectedCount := 0
		for _, r := range orgASeeded {
			if r.action == targetAction {
				expectedCount++
			}
		}
		logs, _, err := repo.ListPaginated(ctx, orgA, domain.AuditLogFilter{
			Limit:  100,
			Action: &targetAction,
		})
		if err != nil {
			t.Fatalf("filter action failed: %v", err)
		}
		if len(logs) == 0 {
			t.Fatalf("expected non-empty results for action %s", targetAction)
		}
		if len(logs) != expectedCount {
			t.Fatalf("expected exactly %d rows for action %s, got %d", expectedCount, targetAction, len(logs))
		}
		for _, l := range logs {
			if l.Action != targetAction {
				t.Fatalf("expected action %s, got %s", targetAction, l.Action)
			}
			if l.OrganizationID == nil || *l.OrganizationID != orgA {
				t.Fatalf("SECURITY VIOLATION: non-Org-A row returned: %+v", l)
			}
		}
	})

	t.Run("filter: entity_type", func(t *testing.T) {
		targetEntityType := domain.EntityTypeResource
		expectedCount := 0
		for _, r := range orgASeeded {
			if r.entityType == targetEntityType {
				expectedCount++
			}
		}
		logs, _, err := repo.ListPaginated(ctx, orgA, domain.AuditLogFilter{
			Limit:      100,
			EntityType: &targetEntityType,
		})
		if err != nil {
			t.Fatalf("filter entity_type failed: %v", err)
		}
		if len(logs) == 0 {
			t.Fatalf("expected non-empty results for entity_type %s", targetEntityType)
		}
		if len(logs) != expectedCount {
			t.Fatalf("expected exactly %d rows for entity_type %s, got %d", expectedCount, targetEntityType, len(logs))
		}
		for _, l := range logs {
			if l.EntityType != targetEntityType {
				t.Fatalf("expected entity_type %s, got %s", targetEntityType, l.EntityType)
			}
			if l.OrganizationID == nil || *l.OrganizationID != orgA {
				t.Fatalf("SECURITY VIOLATION: non-Org-A row returned: %+v", l)
			}
		}
	})

	t.Run("filter: entity_id", func(t *testing.T) {
		expectedCount := 0
		for _, r := range orgASeeded {
			if r.entityID != nil && *r.entityID == entityA {
				expectedCount++
			}
		}
		logs, _, err := repo.ListPaginated(ctx, orgA, domain.AuditLogFilter{
			Limit:    100,
			EntityID: &entityA,
		})
		if err != nil {
			t.Fatalf("filter entity_id failed: %v", err)
		}
		if len(logs) == 0 {
			t.Fatalf("expected non-empty results for entity_id %s", entityA)
		}
		if len(logs) != expectedCount {
			t.Fatalf("expected exactly %d rows for entity_id %s, got %d", expectedCount, entityA, len(logs))
		}
		for _, l := range logs {
			if l.EntityID == nil || *l.EntityID != entityA {
				t.Fatalf("expected entity_id %s, got %v", entityA, l.EntityID)
			}
			if l.OrganizationID == nil || *l.OrganizationID != orgA {
				t.Fatalf("SECURITY VIOLATION: non-Org-A row returned: %+v", l)
			}
		}
	})

	t.Run("filter: user_id", func(t *testing.T) {
		expectedTotalCount := 0
		for _, r := range orgASeeded {
			if r.userID != nil && *r.userID == userA {
				expectedTotalCount++
			}
		}

		// Page 1: Limit 100 with UserID filter -> exactly 100 records, has_more=true
		p1Logs, p1HasMore, err := repo.ListPaginated(ctx, orgA, domain.AuditLogFilter{
			Limit:  100,
			UserID: &userA,
		})
		if err != nil {
			t.Fatalf("page 1 filter user_id failed: %v", err)
		}
		if len(p1Logs) != 100 {
			t.Fatalf("expected page 1 to have 100 items, got %d", len(p1Logs))
		}
		if !p1HasMore {
			t.Fatalf("expected page 1 has_more = true")
		}

		// Page 2: with cursor from last record -> exactly 5 records, has_more=false
		lastLog := p1Logs[len(p1Logs)-1]
		p2Time := lastLog.CreatedAt
		p2ID := lastLog.ID
		p2Logs, p2HasMore, err := repo.ListPaginated(ctx, orgA, domain.AuditLogFilter{
			Limit:           100,
			UserID:          &userA,
			CursorCreatedAt: &p2Time,
			CursorID:        &p2ID,
		})
		if err != nil {
			t.Fatalf("page 2 filter user_id failed: %v", err)
		}
		if len(p2Logs) != 5 {
			t.Fatalf("expected page 2 to have 5 items, got %d", len(p2Logs))
		}
		if p2HasMore {
			t.Fatalf("expected page 2 has_more = false")
		}

		allLogs := append(p1Logs, p2Logs...)
		if len(allLogs) != expectedTotalCount {
			t.Fatalf("expected total %d rows, got %d", expectedTotalCount, len(allLogs))
		}

		seen := make(map[uuid.UUID]bool)
		for _, l := range allLogs {
			if seen[l.ID] {
				t.Fatalf("DUPLICATE row detected in user_id filtered pagination: %s", l.ID)
			}
			seen[l.ID] = true
			if l.UserID == nil || *l.UserID != userA {
				t.Fatalf("expected user_id %s, got %v", userA, l.UserID)
			}
			if l.OrganizationID == nil || *l.OrganizationID != orgA {
				t.Fatalf("SECURITY VIOLATION: non-Org-A row returned: %+v", l)
			}
			if strings.HasPrefix(l.Action, "test.a52.global") {
				t.Fatalf("SECURITY VIOLATION: global action returned: %s", l.Action)
			}
		}
	})

	t.Run("filter: from", func(t *testing.T) {
		boundaryFrom := baseTime.Add(25 * time.Second)
		expectedCount := 0
		for _, r := range orgASeeded {
			if !r.createdAt.Before(boundaryFrom) {
				expectedCount++
			}
		}
		logs, _, err := repo.ListPaginated(ctx, orgA, domain.AuditLogFilter{
			Limit: 100,
			From:  &boundaryFrom,
		})
		if err != nil {
			t.Fatalf("filter from failed: %v", err)
		}
		if len(logs) == 0 {
			t.Fatalf("expected non-empty results for from boundary %v", boundaryFrom)
		}
		if len(logs) != expectedCount {
			t.Fatalf("expected exactly %d rows for from >= %v, got %d", expectedCount, boundaryFrom, len(logs))
		}
		for _, l := range logs {
			if l.CreatedAt.Before(boundaryFrom) {
				t.Fatalf("expected CreatedAt >= %v, got %v", boundaryFrom, l.CreatedAt)
			}
			if l.OrganizationID == nil || *l.OrganizationID != orgA {
				t.Fatalf("SECURITY VIOLATION: non-Org-A row returned: %+v", l)
			}
		}
	})

	t.Run("filter: to", func(t *testing.T) {
		boundaryTo := baseTime.Add(50 * time.Second)
		expectedCount := 0
		for _, r := range orgASeeded {
			if !r.createdAt.After(boundaryTo) {
				expectedCount++
			}
		}
		logs, _, err := repo.ListPaginated(ctx, orgA, domain.AuditLogFilter{
			Limit: 100,
			To:    &boundaryTo,
		})
		if err != nil {
			t.Fatalf("filter to failed: %v", err)
		}
		if len(logs) == 0 {
			t.Fatalf("expected non-empty results for to boundary %v", boundaryTo)
		}
		if len(logs) != expectedCount {
			t.Fatalf("expected exactly %d rows for to <= %v, got %d", expectedCount, boundaryTo, len(logs))
		}
		for _, l := range logs {
			if l.CreatedAt.After(boundaryTo) {
				t.Fatalf("expected CreatedAt <= %v, got %v", boundaryTo, l.CreatedAt)
			}
			if l.OrganizationID == nil || *l.OrganizationID != orgA {
				t.Fatalf("SECURITY VIOLATION: non-Org-A row returned: %+v", l)
			}
		}
	})

	t.Run("filter: zero-time To (year 0001) returns exactly 0 rows for year 2026 fixtures", func(t *testing.T) {
		zeroTo := time.Time{} // 0001-01-01 00:00:00 UTC
		logs, hasMore, err := repo.ListPaginated(ctx, orgA, domain.AuditLogFilter{
			Limit: 100,
			To:    &zeroTo,
		})
		if err != nil {
			t.Fatalf("filter zero-time To failed: %v", err)
		}
		if len(logs) != 0 {
			t.Fatalf("expected exactly 0 rows for zero-time To, got %d", len(logs))
		}
		if hasMore {
			t.Fatalf("expected has_more=false for zero-time To")
		}
	})

	t.Run("filter: zero-time From (year 0001) returns all Org A rows with pagination", func(t *testing.T) {
		zeroFrom := time.Time{} // 0001-01-01 00:00:00 UTC

		// Page 1: Limit 100 with zero From -> exactly 100 items, has_more=true
		p1Logs, p1HasMore, err := repo.ListPaginated(ctx, orgA, domain.AuditLogFilter{
			Limit: 100,
			From:  &zeroFrom,
		})
		if err != nil {
			t.Fatalf("page 1 filter zero-time From failed: %v", err)
		}
		if len(p1Logs) != 100 {
			t.Fatalf("expected page 1 to have 100 items for zero-time From, got %d", len(p1Logs))
		}
		if !p1HasMore {
			t.Fatalf("expected page 1 has_more=true for zero-time From")
		}

		// Page 2: with cursor from last record -> exactly 5 items, has_more=false
		lastLog := p1Logs[len(p1Logs)-1]
		p2Time := lastLog.CreatedAt
		p2ID := lastLog.ID
		p2Logs, p2HasMore, err := repo.ListPaginated(ctx, orgA, domain.AuditLogFilter{
			Limit:           100,
			From:            &zeroFrom,
			CursorCreatedAt: &p2Time,
			CursorID:        &p2ID,
		})
		if err != nil {
			t.Fatalf("page 2 filter zero-time From failed: %v", err)
		}
		if len(p2Logs) != 5 {
			t.Fatalf("expected page 2 to have 5 items for zero-time From, got %d", len(p2Logs))
		}
		if p2HasMore {
			t.Fatalf("expected page 2 has_more=false for zero-time From")
		}

		allLogs := append(p1Logs, p2Logs...)
		if len(allLogs) != len(orgASeeded) { // 105
			t.Fatalf("expected total %d rows, got %d", len(orgASeeded), len(allLogs))
		}

		seen := make(map[uuid.UUID]bool)
		for _, l := range allLogs {
			if seen[l.ID] {
				t.Fatalf("DUPLICATE row detected in zero-time From pagination: %s", l.ID)
			}
			seen[l.ID] = true
			if l.OrganizationID == nil || *l.OrganizationID != orgA {
				t.Fatalf("SECURITY VIOLATION: non-Org-A row returned: %+v", l)
			}
			if strings.HasPrefix(l.Action, "test.a52.global") {
				t.Fatalf("SECURITY VIOLATION: global action returned: %s", l.Action)
			}
		}
	})

	t.Run("filter: combined (action, entity_type, entity_id, user_id, from, to)", func(t *testing.T) {
		combAction := domain.ActionBackupDownload
		combEntityType := domain.EntityTypeBackupArtifact
		combFrom := baseTime.Add(1 * time.Second)
		combTo := baseTime.Add(60 * time.Second)

		expectedCount := 0
		for _, r := range orgASeeded {
			if r.action == combAction &&
				r.entityType == combEntityType &&
				(r.entityID != nil && *r.entityID == entityA) &&
				(r.userID != nil && *r.userID == userA) &&
				!r.createdAt.Before(combFrom) &&
				!r.createdAt.After(combTo) {
				expectedCount++
			}
		}

		logs, _, err := repo.ListPaginated(ctx, orgA, domain.AuditLogFilter{
			Limit:      100,
			Action:     &combAction,
			EntityType: &combEntityType,
			EntityID:   &entityA,
			UserID:     &userA,
			From:       &combFrom,
			To:         &combTo,
		})
		if err != nil {
			t.Fatalf("filter combined failed: %v", err)
		}
		if len(logs) == 0 {
			t.Fatalf("expected non-empty results for combined filters")
		}
		if len(logs) != expectedCount {
			t.Fatalf("expected exactly %d rows for combined filters, got %d", expectedCount, len(logs))
		}
		for _, l := range logs {
			if l.Action != combAction {
				t.Fatalf("combined violation: expected action %s, got %s", combAction, l.Action)
			}
			if l.EntityType != combEntityType {
				t.Fatalf("combined violation: expected entity_type %s, got %s", combEntityType, l.EntityType)
			}
			if l.EntityID == nil || *l.EntityID != entityA {
				t.Fatalf("combined violation: expected entity_id %s, got %v", entityA, l.EntityID)
			}
			if l.UserID == nil || *l.UserID != userA {
				t.Fatalf("combined violation: expected user_id %s, got %v", userA, l.UserID)
			}
			if l.CreatedAt.Before(combFrom) || l.CreatedAt.After(combTo) {
				t.Fatalf("combined violation: expected CreatedAt in [%v, %v], got %v", combFrom, combTo, l.CreatedAt)
			}
			if l.OrganizationID == nil || *l.OrganizationID != orgA {
				t.Fatalf("SECURITY VIOLATION: non-Org-A row returned: %+v", l)
			}
			if strings.HasPrefix(l.Action, "test.a52.global") {
				t.Fatalf("SECURITY VIOLATION: global action returned: %s", l.Action)
			}
		}
	})

	t.Run("tenant isolation: Org B independent tenant verification", func(t *testing.T) {
		logs, hasMore, err := repo.ListPaginated(ctx, orgB, domain.AuditLogFilter{Limit: 50})
		if err != nil {
			t.Fatalf("failed listing Org B logs: %v", err)
		}
		if hasMore {
			t.Fatalf("expected hasMore=false for Org B with limit 50")
		}
		if len(logs) != len(orgBSeeded) {
			t.Fatalf("expected exactly %d Org B logs, got %d", len(orgBSeeded), len(logs))
		}
		for _, l := range logs {
			if l.OrganizationID == nil {
				t.Fatalf("SECURITY VIOLATION: global audit row (orgID=nil) returned to Org B: %+v", l)
			}
			if *l.OrganizationID != orgB {
				t.Fatalf("SECURITY VIOLATION: non-Org-B row returned to Org B: %+v", l)
			}
			if *l.OrganizationID == orgA {
				t.Fatalf("SECURITY VIOLATION: Org A row returned to Org B: %+v", l)
			}
			if strings.HasPrefix(l.Action, "test.a52.global") {
				t.Fatalf("SECURITY VIOLATION: global action returned to Org B: %s", l.Action)
			}
		}
	})
}
