package domain

import (
	"bytes"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"backup-platform/pkg/uuid"
)

var (
	ErrOrgNotFound         = errors.New("organization not found")
	ErrDuplicateOrgSlug    = errors.New("organization with this slug already exists")
	ErrInvalidOrgName      = errors.New("organization name cannot be empty")
	ErrInvalidOrgSlug      = errors.New("organization slug is invalid")
	ErrInvalidMetadata     = errors.New("organization metadata must be a valid JSON object")
	ErrMemberNotFound      = errors.New("membership not found")
	ErrDuplicateMembership = errors.New("user is already a member of this organization")
	ErrInvalidRole         = errors.New("invalid organization role")
)

var slugRegex = regexp.MustCompile(`^[a-z]+(-[a-z]+)*$`)

type OrganizationStatus string

const (
	OrgStatusActive    OrganizationStatus = "active"
	OrgStatusSuspended OrganizationStatus = "suspended"
	OrgStatusArchived  OrganizationStatus = "archived"
)

type Role string

const (
	RoleAdmin  Role = "admin"
	RoleMember Role = "member"
	RoleViewer Role = "viewer"
)

type MemberStatus string

const (
	MemberStatusActive    MemberStatus = "active"
	MemberStatusInvited   MemberStatus = "invited"
	MemberStatusSuspended MemberStatus = "suspended"
)

// Organization represents a tenant organization entity.
type Organization struct {
	ID                uuid.UUID
	Name              string
	Slug              string
	IsDefaultInternal bool
	Status            OrganizationStatus
	Metadata          []byte
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// NewOrganization creates a new validated Organization with default empty metadata.
func NewOrganization(name, slug string, isDefaultInternal bool) (*Organization, error) {
	return NewOrganizationWithMetadata(name, slug, []byte("{}"), isDefaultInternal)
}

// NewOrganizationWithMetadata creates a new validated Organization with JSON object metadata.
func NewOrganizationWithMetadata(name, slug string, metadata []byte, isDefaultInternal bool) (*Organization, error) {
	name = strings.TrimSpace(name)
	nameRuneCount := utf8.RuneCountInString(name)
	if nameRuneCount == 0 || nameRuneCount > 100 {
		return nil, ErrInvalidOrgName
	}

	slug = strings.ToLower(strings.TrimSpace(slug))
	if len(slug) == 0 || len(slug) > 100 || !slugRegex.MatchString(slug) {
		return nil, ErrInvalidOrgSlug
	}

	cleanMetadata := []byte("{}")
	if len(metadata) > 0 {
		trimmed := bytes.TrimSpace(metadata)
		if len(trimmed) > 0 {
			if !bytes.HasPrefix(trimmed, []byte("{")) || !bytes.HasSuffix(trimmed, []byte("}")) {
				return nil, ErrInvalidMetadata
			}
			var obj map[string]any
			if err := json.Unmarshal(trimmed, &obj); err != nil || obj == nil {
				return nil, ErrInvalidMetadata
			}
			cleanMetadata = trimmed
		}
	}

	now := time.Now().UTC()
	return &Organization{
		ID:                uuid.New(),
		Name:              name,
		Slug:              slug,
		IsDefaultInternal: isDefaultInternal,
		Status:            OrgStatusActive,
		Metadata:          cleanMetadata,
		CreatedAt:         now,
		UpdatedAt:         now,
	}, nil
}

// Member represents the membership link between a User and an Organization.
type Member struct {
	ID             uuid.UUID
	OrganizationID uuid.UUID
	UserID         uuid.UUID
	Role           Role
	Status         MemberStatus
	JoinedAt       time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// NewMember creates a new validated Member.
func NewMember(orgID, userID uuid.UUID, role Role) (*Member, error) {
	switch role {
	case RoleAdmin, RoleMember, RoleViewer:
		// valid
	default:
		return nil, ErrInvalidRole
	}

	now := time.Now().UTC()
	return &Member{
		ID:             uuid.New(),
		OrganizationID: orgID,
		UserID:         userID,
		Role:           role,
		Status:         MemberStatusActive,
		JoinedAt:       now,
		CreatedAt:      now,
		UpdatedAt:      now,
	}, nil
}
