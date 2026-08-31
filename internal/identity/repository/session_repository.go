package repository

import (
	"context"
	"errors"
	"time"

	"backup-platform/internal/identity/domain"
	"backup-platform/internal/platform/database"
	"backup-platform/pkg/uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// SessionRepository defines data access operations for user sessions.
type SessionRepository interface {
	Create(ctx context.Context, q database.Querier, session *domain.Session) error
	FindByID(ctx context.Context, q database.Querier, id uuid.UUID) (*domain.Session, error)
	FindByRefreshTokenHash(ctx context.Context, q database.Querier, hash string) (*domain.Session, error)
	RotateRefreshToken(ctx context.Context, q database.Querier, sessionID uuid.UUID, oldHash, newHash string, now time.Time) error
	RevokeByID(ctx context.Context, q database.Querier, id uuid.UUID, now time.Time) error
	RevokeAllForUser(ctx context.Context, q database.Querier, userID uuid.UUID, now time.Time) error
}

// PostgresSessionRepository implements SessionRepository using PostgreSQL.
type PostgresSessionRepository struct{}

// NewPostgresSessionRepository constructs a new PostgresSessionRepository.
func NewPostgresSessionRepository() *PostgresSessionRepository {
	return &PostgresSessionRepository{}
}

// Create inserts a new session record into user_sessions.
func (r *PostgresSessionRepository) Create(ctx context.Context, q database.Querier, session *domain.Session) error {
	const query = `
		INSERT INTO user_sessions (
			id, user_id, refresh_token_hash, ip_address, user_agent, created_at, last_used_at, expires_at, revoked_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9
		)
	`

	_, err := q.Exec(
		ctx,
		query,
		session.ID,
		session.UserID,
		session.RefreshTokenHash,
		session.IPAddress,
		session.UserAgent,
		session.CreatedAt,
		session.LastUsedAt,
		session.ExpiresAt,
		session.RevokedAt,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" { // unique_violation on refresh_token_hash
			return domain.ErrInvalidRefreshToken
		}
		return err
	}

	return nil
}

// FindByID retrieves a session by its UUID primary key.
func (r *PostgresSessionRepository) FindByID(ctx context.Context, q database.Querier, id uuid.UUID) (*domain.Session, error) {
	const query = `
		SELECT id, user_id, refresh_token_hash, ip_address, user_agent, created_at, last_used_at, expires_at, revoked_at
		FROM user_sessions
		WHERE id = $1
	`

	var s domain.Session
	err := q.QueryRow(ctx, query, id).Scan(
		&s.ID,
		&s.UserID,
		&s.RefreshTokenHash,
		&s.IPAddress,
		&s.UserAgent,
		&s.CreatedAt,
		&s.LastUsedAt,
		&s.ExpiresAt,
		&s.RevokedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrSessionNotFound
		}
		return nil, err
	}

	return &s, nil
}

// FindByRefreshTokenHash retrieves a session by the SHA-256 hash of its refresh token.
func (r *PostgresSessionRepository) FindByRefreshTokenHash(ctx context.Context, q database.Querier, hash string) (*domain.Session, error) {
	const query = `
		SELECT id, user_id, refresh_token_hash, ip_address, user_agent, created_at, last_used_at, expires_at, revoked_at
		FROM user_sessions
		WHERE refresh_token_hash = $1
	`

	var s domain.Session
	err := q.QueryRow(ctx, query, hash).Scan(
		&s.ID,
		&s.UserID,
		&s.RefreshTokenHash,
		&s.IPAddress,
		&s.UserAgent,
		&s.CreatedAt,
		&s.LastUsedAt,
		&s.ExpiresAt,
		&s.RevokedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrSessionNotFound
		}
		return nil, err
	}

	return &s, nil
}

// RotateRefreshToken performs an atomic conditional rotation from oldHash to newHash.
// Fails if the session was already rotated, revoked, or has expired.
func (r *PostgresSessionRepository) RotateRefreshToken(ctx context.Context, q database.Querier, sessionID uuid.UUID, oldHash, newHash string, now time.Time) error {
	const query = `
		UPDATE user_sessions
		SET refresh_token_hash = $1, last_used_at = $2
		WHERE id = $3 AND refresh_token_hash = $4 AND revoked_at IS NULL AND expires_at > $2
	`

	nowUTC := now.UTC()
	tag, err := q.Exec(ctx, query, newHash, nowUTC, sessionID, oldHash)
	if err != nil {
		return err
	}

	if tag.RowsAffected() == 0 {
		return domain.ErrInvalidSession
	}

	return nil
}

// RevokeByID marks an active session as revoked by ID.
func (r *PostgresSessionRepository) RevokeByID(ctx context.Context, q database.Querier, id uuid.UUID, now time.Time) error {
	const query = `
		UPDATE user_sessions
		SET revoked_at = $1
		WHERE id = $2 AND revoked_at IS NULL
	`

	_, err := q.Exec(ctx, query, now.UTC(), id)
	return err
}

// RevokeAllForUser marks all active sessions of a user as revoked.
func (r *PostgresSessionRepository) RevokeAllForUser(ctx context.Context, q database.Querier, userID uuid.UUID, now time.Time) error {
	const query = `
		UPDATE user_sessions
		SET revoked_at = $1
		WHERE user_id = $2 AND revoked_at IS NULL
	`

	_, err := q.Exec(ctx, query, now.UTC(), userID)
	return err
}
