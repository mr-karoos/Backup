package connector

import (
	"context"
	"errors"
	"sort"
	"strings"
	"unicode/utf8"

	"backup-platform/internal/credential/payload"
)

const (
	maxDiscoveredDatabases = 10000
	maxDatabaseNameLength  = 64
)

// DatabaseStatus represents the operational accessibility status of a discovered database.
type DatabaseStatus string

const (
	DatabaseStatusAccessible DatabaseStatus = "accessible"
)

// DatabaseInfo represents metadata for a discovered MySQL database.
type DatabaseInfo struct {
	Name        string         `json:"name"`
	SizeBytes   int64          `json:"size_bytes"`
	TablesCount *int64         `json:"tables_count"` // nil for cPanel, integer for Ubuntu SSH
	Status      DatabaseStatus `json:"status"`       // strictly "accessible"
}

// DatabaseDiscoverer defines the capability interface for operational MySQL database discovery.
type DatabaseDiscoverer interface {
	DiscoverDatabases(
		ctx context.Context,
		target Target,
		credPayload *payload.PayloadV1,
	) ([]DatabaseInfo, error)
}

// IsSystemDatabase returns true if the database name matches standard MySQL internal schemas (exact case-insensitive match).
func IsSystemDatabase(name string) bool {
	return strings.EqualFold(name, "information_schema") ||
		strings.EqualFold(name, "mysql") ||
		strings.EqualFold(name, "performance_schema") ||
		strings.EqualFold(name, "sys")
}

// ValidateDatabaseName verifies that a database name is non-empty, non-whitespace-only,
// valid UTF-8, within the 64-character limit, and contains no control characters.
func ValidateDatabaseName(name string) error {
	if len(name) == 0 {
		return errors.New("empty database name")
	}
	if strings.TrimSpace(name) == "" {
		return errors.New("whitespace-only database name")
	}
	if !utf8.ValidString(name) {
		return errors.New("invalid utf-8 database name")
	}
	if utf8.RuneCountInString(name) > maxDatabaseNameLength {
		return errors.New("database name exceeds maximum allowed length")
	}
	if strings.ContainsAny(name, "\x00\r\n\t") {
		return errors.New("database name contains forbidden control characters")
	}
	for _, r := range name {
		if r < 32 || r == 127 {
			return errors.New("database name contains ascii control characters")
		}
	}
	return nil
}

// NormalizeDiscoveredDatabases validates, filters system databases, rejects duplicates, and deterministically sorts discovered databases.
// Preserves exact database name identity without silent rewriting or trimming.
func NormalizeDiscoveredDatabases(raw []DatabaseInfo) ([]DatabaseInfo, error) {
	if len(raw) > maxDiscoveredDatabases {
		return nil, errors.New("exceeded maximum discovered databases limit")
	}

	seen := make(map[string]struct{}, len(raw))
	result := make([]DatabaseInfo, 0, len(raw))

	for _, db := range raw {
		// 1. Validate Database Name integrity (exact name preserved, no silent rewriting)
		if err := ValidateDatabaseName(db.Name); err != nil {
			return nil, err
		}

		// 2. Filter system databases (exact case-insensitive match)
		if IsSystemDatabase(db.Name) {
			continue
		}

		// 3. Reject duplicate database names using exact identity (fail closed)
		if _, exists := seen[db.Name]; exists {
			return nil, errors.New("duplicate database name in discovery result")
		}
		seen[db.Name] = struct{}{}

		// 4. Validate Status integrity (must strictly match DatabaseStatusAccessible)
		if db.Status != DatabaseStatusAccessible {
			return nil, errors.New("invalid or unaccessible database status in discovery result")
		}

		// 5. Validate SizeBytes
		if db.SizeBytes < 0 {
			return nil, errors.New("negative database size in discovery result")
		}

		// 6. Validate and defensively copy TablesCount
		var tablesCountCopy *int64
		if db.TablesCount != nil {
			if *db.TablesCount < 0 {
				return nil, errors.New("negative tables count in discovery result")
			}
			val := *db.TablesCount
			tablesCountCopy = &val
		}

		result = append(result, DatabaseInfo{
			Name:        db.Name,
			SizeBytes:   db.SizeBytes,
			TablesCount: tablesCountCopy,
			Status:      DatabaseStatusAccessible,
		})
	}

	// 7. Deterministic alphabetical sort by exact Name ascending
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})

	return result, nil
}
