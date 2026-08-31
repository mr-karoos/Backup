package repository

import (
	"context"
	"errors"
	"time"

	"backup-platform/internal/organization/domain"
	"backup-platform/internal/platform/database"
	"backup-platform/pkg/uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// UserMembershipWithOrg encapsulates a combined view of an active membership and its parent organization.
type UserMembershipWithOrg struct {
	OrganizationID     uuid.UUID
	OrganizationName   string
	Slug               string
	IsDefaultInternal  bool
	OrganizationStatus domain.OrganizationStatus
	Role               domain.Role
	Status             domain.MemberStatus
	CreatedAt          time.Time
}

// MemberRepository defines data access operations for organization memberships.
type MemberRepository interface {
	Create(ctx context.Context, q database.Querier, member *domain.Member) error
	FindMembership(ctx context.Context, q database.Querier, orgID, userID uuid.UUID) (*domain.Member, error)
	FindActiveMembershipWithOrg(ctx context.Context, q database.Querier, orgID, userID uuid.UUID) (*UserMembershipWithOrg, error)
	ListUserOrganizations(ctx context.Context, q database.Querier, userID uuid.UUID) ([]*domain.Organization, error)
	ListUserMembershipsWithOrg(ctx context.Context, q database.Querier, userID uuid.UUID) ([]*UserMembershipWithOrg, error)
}

// PostgresMemberRepository implements MemberRepository using PostgreSQL.
type PostgresMemberRepository struct{}

// NewPostgresMemberRepository constructs a new PostgresMemberRepository.
func NewPostgresMemberRepository() *PostgresMemberRepository {
	return &PostgresMemberRepository{}
}

// Create inserts a new organization member record.
func (r *PostgresMemberRepository) Create(ctx context.Context, q database.Querier, member *domain.Member) error {
	const query = `
		INSERT INTO organization_members (
			id, organization_id, user_id, role, status, joined_at, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8
		)
	`

	_, err := q.Exec(
		ctx,
		query,
		member.ID,
		member.OrganizationID,
		member.UserID,
		string(member.Role),
		string(member.Status),
		member.JoinedAt,
		member.CreatedAt,
		member.UpdatedAt,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "uq_organization_members_org_user" {
			return domain.ErrDuplicateMembership
		}
		return err
	}

	return nil
}

// FindMembership checks if a membership exists for a given organization and user.
func (r *PostgresMemberRepository) FindMembership(ctx context.Context, q database.Querier, orgID, userID uuid.UUID) (*domain.Member, error) {
	const query = `
		SELECT id, organization_id, user_id, role, status, joined_at, created_at, updated_at
		FROM organization_members
		WHERE organization_id = $1 AND user_id = $2
	`

	var m domain.Member
	var roleStr, statusStr string

	err := q.QueryRow(ctx, query, orgID, userID).Scan(
		&m.ID,
		&m.OrganizationID,
		&m.UserID,
		&roleStr,
		&statusStr,
		&m.JoinedAt,
		&m.CreatedAt,
		&m.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrMemberNotFound
		}
		return nil, err
	}

	m.Role = domain.Role(roleStr)
	m.Status = domain.MemberStatus(statusStr)
	return &m, nil
}

// FindActiveMembershipWithOrg resolves an active membership in an active organization for the given user in a single query.
func (r *PostgresMemberRepository) FindActiveMembershipWithOrg(ctx context.Context, q database.Querier, orgID, userID uuid.UUID) (*UserMembershipWithOrg, error) {
	const query = `
		SELECT 
			m.organization_id,
			o.name,
			o.slug,
			o.is_default_internal,
			o.status,
			m.role,
			m.status,
			o.created_at
		FROM organization_members m
		INNER JOIN organizations o ON o.id = m.organization_id
		WHERE m.organization_id = $1 
		  AND m.user_id = $2 
		  AND m.status = 'active' 
		  AND o.status = 'active'
		LIMIT 1
	`

	var item UserMembershipWithOrg
	var orgStatusStr, roleStr, statusStr string
	err := q.QueryRow(ctx, query, orgID, userID).Scan(
		&item.OrganizationID,
		&item.OrganizationName,
		&item.Slug,
		&item.IsDefaultInternal,
		&orgStatusStr,
		&roleStr,
		&statusStr,
		&item.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	item.OrganizationStatus = domain.OrganizationStatus(orgStatusStr)
	item.Role = domain.Role(roleStr)
	item.Status = domain.MemberStatus(statusStr)
	return &item, nil
}

// ListUserOrganizations returns all organizations a user is an active member of.
func (r *PostgresMemberRepository) ListUserOrganizations(ctx context.Context, q database.Querier, userID uuid.UUID) ([]*domain.Organization, error) {
	const query = `
		SELECT o.id, o.name, o.slug, o.is_default_internal, o.status, o.metadata, o.created_at, o.updated_at
		FROM organizations o
		INNER JOIN organization_members m ON m.organization_id = o.id
		WHERE m.user_id = $1 AND m.status = 'active' AND o.status = 'active'
		ORDER BY o.is_default_internal DESC, o.name ASC
	`

	rows, err := q.Query(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var orgs []*domain.Organization
	for rows.Next() {
		var o domain.Organization
		var statusStr string
		err := rows.Scan(
			&o.ID,
			&o.Name,
			&o.Slug,
			&o.IsDefaultInternal,
			&statusStr,
			&o.Metadata,
			&o.CreatedAt,
			&o.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		o.Status = domain.OrganizationStatus(statusStr)
		orgs = append(orgs, &o)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return orgs, nil
}

// ListUserMembershipsWithOrg retrieves all active memberships along with organization details in a single query.
func (r *PostgresMemberRepository) ListUserMembershipsWithOrg(ctx context.Context, q database.Querier, userID uuid.UUID) ([]*UserMembershipWithOrg, error) {
	const query = `
		SELECT 
			m.organization_id,
			o.name,
			o.slug,
			o.is_default_internal,
			o.status,
			m.role,
			m.status,
			o.created_at
		FROM organization_members m
		INNER JOIN organizations o ON o.id = m.organization_id
		WHERE m.user_id = $1 AND m.status = 'active' AND o.status = 'active'
		ORDER BY o.is_default_internal DESC, o.name ASC
	`

	rows, err := q.Query(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []*UserMembershipWithOrg
	for rows.Next() {
		var item UserMembershipWithOrg
		var orgStatusStr, roleStr, statusStr string
		err := rows.Scan(
			&item.OrganizationID,
			&item.OrganizationName,
			&item.Slug,
			&item.IsDefaultInternal,
			&orgStatusStr,
			&roleStr,
			&statusStr,
			&item.CreatedAt,
		)
		if err != nil {
			return nil, err
		}
		item.OrganizationStatus = domain.OrganizationStatus(orgStatusStr)
		item.Role = domain.Role(roleStr)
		item.Status = domain.MemberStatus(statusStr)
		list = append(list, &item)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return list, nil
}
