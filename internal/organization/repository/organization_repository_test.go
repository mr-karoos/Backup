package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"backup-platform/internal/organization/domain"
	"backup-platform/pkg/uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type fakeErrorQuerier struct {
	execErr error
}

func (f *fakeErrorQuerier) Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, f.execErr
}

func (f *fakeErrorQuerier) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return nil, f.execErr
}

func (f *fakeErrorQuerier) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return nil
}

type fakeRow struct {
	scanErr error
	values  []any
}

func (r *fakeRow) Scan(dest ...any) error {
	if r.scanErr != nil {
		return r.scanErr
	}
	for i, v := range r.values {
		if i >= len(dest) {
			break
		}
		switch d := dest[i].(type) {
		case *uuid.UUID:
			*d = v.(uuid.UUID)
		case *string:
			*d = v.(string)
		case *bool:
			*d = v.(bool)
		case *[]byte:
			*d = v.([]byte)
		case *time.Time:
			*d = v.(time.Time)
		}
	}
	return nil
}

type fakeQueryRowQuerier struct {
	row pgx.Row
}

func (f *fakeQueryRowQuerier) Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}

func (f *fakeQueryRowQuerier) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return nil, nil
}

func (f *fakeQueryRowQuerier) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return f.row
}

func TestPostgresOrganizationRepository_Create_ErrorClassification(t *testing.T) {
	ctx := context.Background()
	repo := NewPostgresOrganizationRepository()
	org := &domain.Organization{
		ID:        uuid.New(),
		Name:      "Acme Corp",
		Slug:      "acme-corp",
		Status:    domain.OrgStatusActive,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}

	t.Run("pg 23505 with organizations_slug_key returns ErrDuplicateOrgSlug", func(t *testing.T) {
		q := &fakeErrorQuerier{
			execErr: &pgconn.PgError{
				Code:           "23505",
				ConstraintName: "organizations_slug_key",
			},
		}

		err := repo.Create(ctx, q, org)
		if !errors.Is(err, domain.ErrDuplicateOrgSlug) {
			t.Errorf("expected ErrDuplicateOrgSlug, got: %v", err)
		}
	})

	t.Run("pg 23505 with different constraint does not return ErrDuplicateOrgSlug", func(t *testing.T) {
		unrelatedErr := &pgconn.PgError{
			Code:           "23505",
			ConstraintName: "organizations_pkey",
		}
		q := &fakeErrorQuerier{execErr: unrelatedErr}

		err := repo.Create(ctx, q, org)
		if errors.Is(err, domain.ErrDuplicateOrgSlug) {
			t.Errorf("did NOT expect ErrDuplicateOrgSlug for different constraint, got: %v", err)
		}
		if !errors.Is(err, unrelatedErr) {
			t.Errorf("expected raw pg error to be preserved, got: %v", err)
		}
	})

	t.Run("non-23505 error is preserved as raw error", func(t *testing.T) {
		genericErr := errors.New("connection failed")
		q := &fakeErrorQuerier{execErr: genericErr}

		err := repo.Create(ctx, q, org)
		if !errors.Is(err, genericErr) {
			t.Errorf("expected raw error, got: %v", err)
		}
	})
}

func TestPostgresOrganizationRepository_FindActiveByID(t *testing.T) {
	ctx := context.Background()
	repo := NewPostgresOrganizationRepository()
	orgID := uuid.New()
	now := time.Now().UTC()

	t.Run("successfully scans active organization", func(t *testing.T) {
		q := &fakeQueryRowQuerier{
			row: &fakeRow{
				values: []any{
					orgID,
					"Acme Corporation",
					"acme-corp",
					false,
					"active",
					[]byte(`{"plan":"enterprise"}`),
					now,
					now,
				},
			},
		}

		org, err := repo.FindActiveByID(ctx, q, orgID)
		if err != nil {
			t.Fatalf("expected success, got: %v", err)
		}
		if org.ID != orgID || org.Name != "Acme Corporation" || org.Slug != "acme-corp" || org.Status != domain.OrgStatusActive {
			t.Errorf("unexpected scanned organization: %+v", org)
		}
		if string(org.Metadata) != `{"plan":"enterprise"}` {
			t.Errorf("unexpected metadata: %s", string(org.Metadata))
		}
	})

	t.Run("returns domain.ErrOrgNotFound on pgx.ErrNoRows", func(t *testing.T) {
		q := &fakeQueryRowQuerier{
			row: &fakeRow{scanErr: pgx.ErrNoRows},
		}

		org, err := repo.FindActiveByID(ctx, q, orgID)
		if org != nil {
			t.Errorf("expected nil org on ErrNoRows")
		}
		if !errors.Is(err, domain.ErrOrgNotFound) {
			t.Errorf("expected ErrOrgNotFound, got: %v", err)
		}
	})

	t.Run("preserves raw database infrastructure errors", func(t *testing.T) {
		dbErr := errors.New("pq: connection reset by peer")
		q := &fakeQueryRowQuerier{
			row: &fakeRow{scanErr: dbErr},
		}

		org, err := repo.FindActiveByID(ctx, q, orgID)
		if org != nil {
			t.Errorf("expected nil org on DB error")
		}
		if !errors.Is(err, dbErr) {
			t.Errorf("expected dbErr preserved, got: %v", err)
		}
	})
}

func TestPostgresOrganizationRepository_UpdateActive(t *testing.T) {
	ctx := context.Background()
	repo := NewPostgresOrganizationRepository()
	orgID := uuid.New()
	createdAt := time.Now().UTC().Add(-1 * time.Hour)
	updatedAt := time.Now().UTC()

	t.Run("successfully updates active organization and returns updated detail", func(t *testing.T) {
		q := &fakeQueryRowQuerier{
			row: &fakeRow{
				values: []any{
					orgID,
					"Acme Corp International",
					"acme-corp",
					false,
					"active",
					[]byte(`{"plan":"enterprise","max_resources":50}`),
					createdAt,
					updatedAt,
				},
			},
		}

		org, err := repo.UpdateActive(ctx, q, orgID, "Acme Corp International", []byte(`{"plan":"enterprise","max_resources":50}`))
		if err != nil {
			t.Fatalf("expected success, got: %v", err)
		}
		if org.ID != orgID {
			t.Errorf("expected org ID %s, got: %s", orgID, org.ID)
		}
		if org.Name != "Acme Corp International" {
			t.Errorf("expected updated name, got: %s", org.Name)
		}
		if org.Slug != "acme-corp" {
			t.Errorf("expected unchanged slug, got: %s", org.Slug)
		}
		if org.Status != domain.OrgStatusActive {
			t.Errorf("expected unchanged active status, got: %s", org.Status)
		}
		if org.IsDefaultInternal {
			t.Errorf("expected is_default_internal = false")
		}
		if org.CreatedAt != createdAt {
			t.Errorf("expected createdAt to remain unchanged")
		}
		if org.UpdatedAt != updatedAt {
			t.Errorf("expected updatedAt to be new timestamp")
		}
	})

	t.Run("returns domain.ErrOrgNotFound on pgx.ErrNoRows", func(t *testing.T) {
		q := &fakeQueryRowQuerier{
			row: &fakeRow{scanErr: pgx.ErrNoRows},
		}

		org, err := repo.UpdateActive(ctx, q, orgID, "Acme", []byte("{}"))
		if org != nil {
			t.Errorf("expected nil org on ErrNoRows")
		}
		if !errors.Is(err, domain.ErrOrgNotFound) {
			t.Errorf("expected ErrOrgNotFound, got: %v", err)
		}
	})

	t.Run("preserves raw database infrastructure errors", func(t *testing.T) {
		dbErr := errors.New("pq: disk failure")
		q := &fakeQueryRowQuerier{
			row: &fakeRow{scanErr: dbErr},
		}

		org, err := repo.UpdateActive(ctx, q, orgID, "Acme", []byte("{}"))
		if org != nil {
			t.Errorf("expected nil org on DB error")
		}
		if !errors.Is(err, dbErr) {
			t.Errorf("expected dbErr preserved, got: %v", err)
		}
	})
}
