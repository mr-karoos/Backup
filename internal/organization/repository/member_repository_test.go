package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"backup-platform/internal/organization/domain"
	"backup-platform/pkg/uuid"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestPostgresMemberRepository_Create_ErrorClassification(t *testing.T) {
	ctx := context.Background()
	repo := NewPostgresMemberRepository()
	member := &domain.Member{
		ID:             uuid.New(),
		OrganizationID: uuid.New(),
		UserID:         uuid.New(),
		Role:           domain.RoleAdmin,
		Status:         domain.MemberStatusActive,
		JoinedAt:       time.Now().UTC(),
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
	}

	t.Run("pg 23505 with uq_organization_members_org_user returns ErrDuplicateMembership", func(t *testing.T) {
		q := &fakeErrorQuerier{
			execErr: &pgconn.PgError{
				Code:           "23505",
				ConstraintName: "uq_organization_members_org_user",
			},
		}

		err := repo.Create(ctx, q, member)
		if !errors.Is(err, domain.ErrDuplicateMembership) {
			t.Errorf("expected ErrDuplicateMembership, got: %v", err)
		}
	})

	t.Run("pg 23505 with different constraint does not return ErrDuplicateMembership", func(t *testing.T) {
		unrelatedErr := &pgconn.PgError{
			Code:           "23505",
			ConstraintName: "organization_members_pkey",
		}
		q := &fakeErrorQuerier{execErr: unrelatedErr}

		err := repo.Create(ctx, q, member)
		if errors.Is(err, domain.ErrDuplicateMembership) {
			t.Errorf("did NOT expect ErrDuplicateMembership for different constraint, got: %v", err)
		}
		if !errors.Is(err, unrelatedErr) {
			t.Errorf("expected raw pg error to be preserved, got: %v", err)
		}
	})

	t.Run("non-23505 error is preserved as raw error", func(t *testing.T) {
		genericErr := errors.New("connection failed")
		q := &fakeErrorQuerier{execErr: genericErr}

		err := repo.Create(ctx, q, member)
		if !errors.Is(err, genericErr) {
			t.Errorf("expected raw error, got: %v", err)
		}
	})
}
