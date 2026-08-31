package database

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Safe sentinel errors to prevent exposing raw connection strings or credentials.
var (
	ErrInvalidConfig     = errors.New("invalid database configuration format")
	ErrPoolInitFailed    = errors.New("failed to initialize database connection pool")
	ErrStartupPingFailed = errors.New("database connection unavailable during startup")
	ErrPoolNotReady      = errors.New("database pool is not initialized")
	ErrPingFailed        = errors.New("database ping failed")
)

// Querier defines standard SQL execution operations supported by both *pgxpool.Pool and pgx.Tx.
type Querier interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// TxManager abstracts transaction execution and querier access for data operations.
type TxManager interface {
	Querier() Querier
	WithinTx(ctx context.Context, fn func(q Querier) error) error
}

// DB represents the minimal interface required for database connectivity and health monitoring.
type DB interface {
	Ping(ctx context.Context) error
	Close()
}

// PostgresDB manages the pgx connection pool for PostgreSQL and implements TxManager.
type PostgresDB struct {
	pool *pgxpool.Pool
}

// New initializes and validates a new PostgreSQL connection pool without leaking raw DSNs.
func New(ctx context.Context, databaseURL string) (*PostgresDB, error) {
	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, ErrInvalidConfig
	}

	// Conservative connection pool tuning for single-binary modular monolith
	poolConfig.MaxConns = 20
	poolConfig.MinConns = 2
	poolConfig.MaxConnLifetime = 1 * time.Hour
	poolConfig.MaxConnIdleTime = 15 * time.Minute
	poolConfig.HealthCheckPeriod = 1 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, ErrPoolInitFailed
	}

	// Validate connectivity at startup with a dedicated timeout
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, ErrStartupPingFailed
	}

	return &PostgresDB{pool: pool}, nil
}

// Ping checks if the PostgreSQL database is reachable.
func (p *PostgresDB) Ping(ctx context.Context) error {
	if p == nil || p.pool == nil {
		return ErrPoolNotReady
	}
	if err := p.pool.Ping(ctx); err != nil {
		return ErrPingFailed
	}
	return nil
}

// Querier returns the standard connection pool querier.
func (p *PostgresDB) Querier() Querier {
	if p == nil {
		return nil
	}
	return p.pool
}

// WithinTx executes a callback inside a managed PostgreSQL transaction.
// If the callback returns an error, the transaction is rolled back; otherwise, it is committed.
func (p *PostgresDB) WithinTx(ctx context.Context, fn func(q Querier) error) error {
	if p == nil || p.pool == nil {
		return ErrPoolNotReady
	}

	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	if err := fn(tx); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// Begin starts a new database transaction manually.
func (p *PostgresDB) Begin(ctx context.Context) (pgx.Tx, error) {
	if p == nil || p.pool == nil {
		return nil, ErrPoolNotReady
	}
	return p.pool.Begin(ctx)
}

// Pool returns the underlying pgxpool.Pool instance for data access.
func (p *PostgresDB) Pool() *pgxpool.Pool {
	if p == nil {
		return nil
	}
	return p.pool
}

// Close closes all open connections in the pool gracefully.
func (p *PostgresDB) Close() {
	if p != nil && p.pool != nil {
		p.pool.Close()
	}
}
