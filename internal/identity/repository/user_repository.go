package repository

import (
	"context"
	"errors"
	"strings"
	"time"

	"backup-platform/internal/identity/domain"
	"backup-platform/internal/platform/database"
	"backup-platform/pkg/uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// UserRepository defines data access operations for global users.
type UserRepository interface {
	Create(ctx context.Context, q database.Querier, user *domain.User) error
	FindByID(ctx context.Context, q database.Querier, id uuid.UUID) (*domain.User, error)
	FindByEmail(ctx context.Context, q database.Querier, email string) (*domain.User, error)
	HasSystemAdmin(ctx context.Context, q database.Querier) (bool, error)
	UpdateStatus(ctx context.Context, q database.Querier, id uuid.UUID, status domain.UserStatus) error
}

// PostgresUserRepository implements UserRepository using PostgreSQL.
type PostgresUserRepository struct{}

// NewPostgresUserRepository constructs a new PostgresUserRepository.
func NewPostgresUserRepository() *PostgresUserRepository {
	return &PostgresUserRepository{}
}

// Create inserts a new user record into the users table.
func (r *PostgresUserRepository) Create(ctx context.Context, q database.Querier, user *domain.User) error {
	const query = `
		INSERT INTO users (
			id, email, password_hash, full_name, is_system_admin, status, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8
		)
	`

	_, err := q.Exec(
		ctx,
		query,
		user.ID,
		strings.ToLower(user.Email),
		user.PasswordHash,
		user.FullName,
		user.IsSystemAdmin,
		string(user.Status),
		user.CreatedAt,
		user.UpdatedAt,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" { // unique_violation
			return domain.ErrDuplicateEmail
		}
		return err
	}

	return nil
}

// FindByID retrieves a user by their UUID primary key.
func (r *PostgresUserRepository) FindByID(ctx context.Context, q database.Querier, id uuid.UUID) (*domain.User, error) {
	const query = `
		SELECT id, email, password_hash, full_name, is_system_admin, status, created_at, updated_at
		FROM users
		WHERE id = $1
	`

	var u domain.User
	var statusStr string

	err := q.QueryRow(ctx, query, id).Scan(
		&u.ID,
		&u.Email,
		&u.PasswordHash,
		&u.FullName,
		&u.IsSystemAdmin,
		&statusStr,
		&u.CreatedAt,
		&u.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrUserNotFound
		}
		return nil, err
	}

	u.Status = domain.UserStatus(statusStr)
	return &u, nil
}

// FindByEmail retrieves a user by their normalized lowercase email.
func (r *PostgresUserRepository) FindByEmail(ctx context.Context, q database.Querier, email string) (*domain.User, error) {
	const query = `
		SELECT id, email, password_hash, full_name, is_system_admin, status, created_at, updated_at
		FROM users
		WHERE email = $1
	`

	var u domain.User
	var statusStr string

	err := q.QueryRow(ctx, query, strings.ToLower(strings.TrimSpace(email))).Scan(
		&u.ID,
		&u.Email,
		&u.PasswordHash,
		&u.FullName,
		&u.IsSystemAdmin,
		&statusStr,
		&u.CreatedAt,
		&u.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrUserNotFound
		}
		return nil, err
	}

	u.Status = domain.UserStatus(statusStr)
	return &u, nil
}

// HasSystemAdmin checks if any active system administrator exists in the database.
func (r *PostgresUserRepository) HasSystemAdmin(ctx context.Context, q database.Querier) (bool, error) {
	const query = `
		SELECT EXISTS(
			SELECT 1 FROM users WHERE is_system_admin = true AND status = 'active'
		)
	`

	var exists bool
	err := q.QueryRow(ctx, query).Scan(&exists)
	if err != nil {
		return false, err
	}

	return exists, nil
}

// UpdateStatus updates the status and updated_at timestamp of a user.
func (r *PostgresUserRepository) UpdateStatus(ctx context.Context, q database.Querier, id uuid.UUID, status domain.UserStatus) error {
	const query = `
		UPDATE users
		SET status = $1, updated_at = $2
		WHERE id = $3
	`

	tag, err := q.Exec(ctx, query, string(status), time.Now().UTC(), id)
	if err != nil {
		return err
	}

	if tag.RowsAffected() == 0 {
		return domain.ErrUserNotFound
	}

	return nil
}
