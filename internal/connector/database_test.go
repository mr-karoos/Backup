package connector

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"backup-platform/internal/credential/payload"
	resDomain "backup-platform/internal/resource/domain"
)

type dummyDiscoverer struct{}

func (d *dummyDiscoverer) DiscoverDatabases(ctx context.Context, target Target, credPayload *payload.PayloadV1) ([]DatabaseInfo, error) {
	return []DatabaseInfo{{Name: "test_db", SizeBytes: 1024, Status: DatabaseStatusAccessible}}, nil
}

func TestDiscoveryRegistry_RegisterAndGet(t *testing.T) {
	r := NewDiscoveryRegistry()

	_, err := r.Get(resDomain.TypeUbuntuSSH)
	if !errors.Is(err, ErrNoDiscovererRegistered) {
		t.Errorf("expected ErrNoDiscovererRegistered, got: %v", err)
	}

	disc := &dummyDiscoverer{}
	r.Register(resDomain.TypeUbuntuSSH, disc)

	got, err := r.Get(resDomain.TypeUbuntuSSH)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != disc {
		t.Errorf("expected registered discoverer, got: %v", got)
	}

	_, err = r.Get(resDomain.TypeCPanel)
	if !errors.Is(err, ErrNoDiscovererRegistered) {
		t.Errorf("expected ErrNoDiscovererRegistered for cpanel, got: %v", err)
	}
}

func TestIsSystemDatabase(t *testing.T) {
	systemDBs := []string{
		"information_schema",
		"mysql",
		"performance_schema",
		"sys",
		"INFORMATION_SCHEMA",
		"MYSQL",
		"Performance_Schema",
		"Sys",
	}
	for _, name := range systemDBs {
		if !IsSystemDatabase(name) {
			t.Errorf("expected %q to be recognized as system database", name)
		}
	}

	userDBs := []string{
		"  mysql  ",
		"mysql ",
		" mysql",
		"my_database",
		"mysql_app",
		"app_mysql",
		"app_information_schema",
		"system",
		"perf_schema",
	}
	for _, name := range userDBs {
		if IsSystemDatabase(name) {
			t.Errorf("expected %q to NOT be recognized as system database", name)
		}
	}
}

func TestNormalizeDiscoveredDatabases(t *testing.T) {
	tables48 := int64(48)
	tables0 := int64(0)

	t.Run("Valid databases with exact name preservation, sorting and system DB filtering", func(t *testing.T) {
		raw := []DatabaseInfo{
			{Name: "zeta_db", SizeBytes: 500, TablesCount: &tables48, Status: DatabaseStatusAccessible},
			{Name: "mysql", SizeBytes: 1000, TablesCount: nil, Status: DatabaseStatusAccessible},              // System DB - filtered
			{Name: "INFORMATION_SCHEMA", SizeBytes: 200, TablesCount: nil, Status: DatabaseStatusAccessible},  // System DB - filtered
			{Name: "alpha_db", SizeBytes: 104857600, TablesCount: &tables0, Status: DatabaseStatusAccessible}, // TablesCount 0 is valid
			{Name: "middle_db", SizeBytes: 2048, TablesCount: nil, Status: DatabaseStatusAccessible},          // nil tables_count is valid (cPanel)
			{Name: "sys", SizeBytes: 50, TablesCount: nil, Status: DatabaseStatusAccessible},                  // System DB - filtered
			{Name: "دیتابیس_فارسی", SizeBytes: 4096, TablesCount: nil, Status: DatabaseStatusAccessible},      // Unicode name preserved
		}

		normalized, err := NormalizeDiscoveredDatabases(raw)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(normalized) != 4 {
			t.Fatalf("expected 4 databases after filtering, got: %d", len(normalized))
		}

		// Assert alphabetical ascending sort and exact name identity
		if normalized[0].Name != "alpha_db" || normalized[0].SizeBytes != 104857600 || *normalized[0].TablesCount != 0 {
			t.Errorf("unexpected entry 0: %+v", normalized[0])
		}
		if normalized[1].Name != "middle_db" || normalized[1].SizeBytes != 2048 || normalized[1].TablesCount != nil {
			t.Errorf("unexpected entry 1: %+v", normalized[1])
		}
		if normalized[2].Name != "zeta_db" || normalized[2].SizeBytes != 500 || *normalized[2].TablesCount != 48 {
			t.Errorf("unexpected entry 2: %+v", normalized[2])
		}
		if normalized[3].Name != "دیتابیس_فارسی" || normalized[3].SizeBytes != 4096 || normalized[3].TablesCount != nil {
			t.Errorf("unexpected entry 3: %+v", normalized[3])
		}

		for _, db := range normalized {
			if db.Status != DatabaseStatusAccessible {
				t.Errorf("expected status 'accessible', got: %s", db.Status)
			}
		}
	})

	t.Run("Preserves exact database name identity without silent trimming", func(t *testing.T) {
		raw := []DatabaseInfo{
			{Name: " app_db ", SizeBytes: 100, Status: DatabaseStatusAccessible},
		}
		normalized, err := NormalizeDiscoveredDatabases(raw)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(normalized) != 1 || normalized[0].Name != " app_db " {
			t.Fatalf("expected exact name %q, got %q", " app_db ", normalized[0].Name)
		}
	})

	t.Run("Defensive copy of TablesCount pointer", func(t *testing.T) {
		originalCount := int64(10)
		raw := []DatabaseInfo{
			{Name: "app_db", SizeBytes: 100, TablesCount: &originalCount, Status: DatabaseStatusAccessible},
		}
		normalized, err := NormalizeDiscoveredDatabases(raw)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Mutate original integer
		originalCount = 999

		// Normalized copy must remain unchanged at 10
		if *normalized[0].TablesCount != 10 {
			t.Errorf("expected copied tables_count to be 10, got: %d", *normalized[0].TablesCount)
		}
	})

	t.Run("Rejects invalid or unaccessible status", func(t *testing.T) {
		raw := []DatabaseInfo{
			{Name: "app_db", SizeBytes: 100, Status: "corrupted_status"},
		}
		_, err := NormalizeDiscoveredDatabases(raw)
		if err == nil {
			t.Fatalf("expected error for invalid database status, got nil")
		}
	})

	t.Run("Rejects duplicate database names using exact identity", func(t *testing.T) {
		raw := []DatabaseInfo{
			{Name: "app_db", SizeBytes: 100, Status: DatabaseStatusAccessible},
			{Name: "app_db", SizeBytes: 200, Status: DatabaseStatusAccessible},
		}
		_, err := NormalizeDiscoveredDatabases(raw)
		if err == nil {
			t.Fatalf("expected error for duplicate database names, got nil")
		}
	})

	t.Run("Accepts distinct names differing by whitespace without false duplicate detection", func(t *testing.T) {
		raw := []DatabaseInfo{
			{Name: "app_db", SizeBytes: 100, Status: DatabaseStatusAccessible},
			{Name: " app_db", SizeBytes: 200, Status: DatabaseStatusAccessible},
		}
		norm, err := NormalizeDiscoveredDatabases(raw)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(norm) != 2 {
			t.Fatalf("expected 2 distinct databases, got %d", len(norm))
		}
	})

	t.Run("Rejects empty or whitespace-only database name", func(t *testing.T) {
		_, err := NormalizeDiscoveredDatabases([]DatabaseInfo{{Name: "", SizeBytes: 100, Status: DatabaseStatusAccessible}})
		if err == nil {
			t.Fatalf("expected error for empty database name, got nil")
		}

		_, err = NormalizeDiscoveredDatabases([]DatabaseInfo{{Name: "   ", SizeBytes: 100, Status: DatabaseStatusAccessible}})
		if err == nil {
			t.Fatalf("expected error for whitespace-only database name, got nil")
		}
	})

	t.Run("Rejects control characters in name", func(t *testing.T) {
		invalidNames := []string{
			"db\x00name",
			"db\nname",
			"db\rname",
			"db\tname",
			"db\x1fname",
		}
		for _, name := range invalidNames {
			_, err := NormalizeDiscoveredDatabases([]DatabaseInfo{{Name: name, SizeBytes: 100, Status: DatabaseStatusAccessible}})
			if err == nil {
				t.Errorf("expected error for name with control characters %q, got nil", name)
			}
		}
	})

	t.Run("Rejects overlong database name (> 64 runes)", func(t *testing.T) {
		longName := strings.Repeat("a", 65)
		_, err := NormalizeDiscoveredDatabases([]DatabaseInfo{{Name: longName, SizeBytes: 100, Status: DatabaseStatusAccessible}})
		if err == nil {
			t.Fatalf("expected error for overlong database name, got nil")
		}
	})

	t.Run("Accepts 64-rune valid database name", func(t *testing.T) {
		valid64 := strings.Repeat("a", 64)
		norm, err := NormalizeDiscoveredDatabases([]DatabaseInfo{{Name: valid64, SizeBytes: 100, Status: DatabaseStatusAccessible}})
		if err != nil {
			t.Fatalf("unexpected error for 64-char name: %v", err)
		}
		if len(norm) != 1 {
			t.Fatalf("expected 1 result, got %d", len(norm))
		}
	})

	t.Run("Rejects negative SizeBytes", func(t *testing.T) {
		_, err := NormalizeDiscoveredDatabases([]DatabaseInfo{{Name: "app_db", SizeBytes: -1, Status: DatabaseStatusAccessible}})
		if err == nil {
			t.Fatalf("expected error for negative size_bytes, got nil")
		}
	})

	t.Run("Rejects negative TablesCount", func(t *testing.T) {
		neg := int64(-5)
		_, err := NormalizeDiscoveredDatabases([]DatabaseInfo{{Name: "app_db", SizeBytes: 100, TablesCount: &neg, Status: DatabaseStatusAccessible}})
		if err == nil {
			t.Fatalf("expected error for negative tables_count, got nil")
		}
	})

	t.Run("Rejects more than 10,000 databases", func(t *testing.T) {
		raw := make([]DatabaseInfo, 10001)
		for i := 0; i < 10001; i++ {
			raw[i] = DatabaseInfo{Name: fmt.Sprintf("db_%d", i), SizeBytes: 100, Status: DatabaseStatusAccessible}
		}
		_, err := NormalizeDiscoveredDatabases(raw)
		if err == nil {
			t.Fatalf("expected error for > 10,000 databases, got nil")
		}
	})
}
