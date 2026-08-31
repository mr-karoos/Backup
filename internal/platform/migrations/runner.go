package migrations

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5"
)

const (
	// MinimumPostgresVersionNum is 150000 (PostgreSQL 15.0).
	MinimumPostgresVersionNum = 150000
)

// Safe sentinel errors that do not leak raw DSNs or database credentials.
var (
	ErrInitMigration              = errors.New("failed to initialize database migrations")
	ErrApplyMigration             = errors.New("failed to apply database migrations")
	ErrUnsupportedPostgresVersion = errors.New("unsupported postgresql version: requires postgresql 15 or newer")
	ErrPostgresVersionCheckFailed = errors.New("failed to verify postgresql server version")
)

// ValidatePostgresVersionNum checks if a raw server_version_num string meets the minimum requirement (>= 150000).
func ValidatePostgresVersionNum(versionStr string) error {
	trimmed := strings.TrimSpace(versionStr)
	if trimmed == "" {
		return ErrPostgresVersionCheckFailed
	}

	versionNum, err := strconv.Atoi(trimmed)
	if err != nil {
		return ErrPostgresVersionCheckFailed
	}

	if versionNum < MinimumPostgresVersionNum {
		return ErrUnsupportedPostgresVersion
	}

	return nil
}

// CheckPostgresVersion performs a preflight check against the target PostgreSQL instance.
func CheckPostgresVersion(ctx context.Context, databaseURL string) error {
	conn, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		return ErrPostgresVersionCheckFailed
	}
	defer func() {
		_ = conn.Close(ctx)
	}()

	var versionStr string
	if err := conn.QueryRow(ctx, "SHOW server_version_num;").Scan(&versionStr); err != nil {
		return ErrPostgresVersionCheckFailed
	}

	return ValidatePostgresVersionNum(versionStr)
}

// Run applies all pending database migrations using embedded SQL scripts after performing a version preflight check.
func Run(databaseURL string) error {
	preflightCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := CheckPostgresVersion(preflightCtx, databaseURL); err != nil {
		return err
	}

	d, err := iofs.New(FS, "sql")
	if err != nil {
		return ErrInitMigration
	}

	m, err := migrate.NewWithSourceInstance("iofs", d, databaseURL)
	if err != nil {
		return ErrInitMigration
	}
	defer func() {
		_, _ = m.Close()
	}()

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return ErrApplyMigration
	}

	return nil
}
