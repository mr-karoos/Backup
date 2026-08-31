package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"unicode/utf8"

	"backup-platform/internal/organization/domain"
	orgRepo "backup-platform/internal/organization/repository"
	"backup-platform/internal/platform/database"
	"backup-platform/internal/platform/logger"
	"backup-platform/pkg/uuid"
)

var (
	ErrOrganizationServiceUnavailable = errors.New("organization service temporarily unavailable")
)

// CreateOrganizationInput represents the sanitized input for creating a new tenant organization.
type CreateOrganizationInput struct {
	Name     string
	Slug     string
	Metadata []byte
}

// UpdateOrganizationInput represents the sanitized input for updating an existing organization.
type UpdateOrganizationInput struct {
	Name     string
	Metadata []byte
}

// OrganizationService defines business operations for organizations.
type OrganizationService interface {
	CreateOrganization(ctx context.Context, actorUserID uuid.UUID, input CreateOrganizationInput) (*domain.Organization, error)
	ListUserOrganizations(ctx context.Context, userID uuid.UUID) ([]*orgRepo.UserMembershipWithOrg, error)
	GetActiveOrganization(ctx context.Context, id uuid.UUID) (*domain.Organization, error)
	UpdateOrganization(ctx context.Context, id uuid.UUID, input UpdateOrganizationInput) (*domain.Organization, error)
}

// Service implements OrganizationService with transactional consistency.
type Service struct {
	orgRepo    orgRepo.OrganizationRepository
	memberRepo orgRepo.MemberRepository
	txManager  database.TxManager
	logger     *slog.Logger
}

// NewService constructs a new Organization Service.
func NewService(
	orgRepo orgRepo.OrganizationRepository,
	memberRepo orgRepo.MemberRepository,
	txManager database.TxManager,
	log *slog.Logger,
) *Service {
	return &Service{
		orgRepo:    orgRepo,
		memberRepo: memberRepo,
		txManager:  txManager,
		logger:     log,
	}
}

// CreateOrganization atomically provisions a new tenant organization and assigns the creator as active admin.
func (s *Service) CreateOrganization(ctx context.Context, actorUserID uuid.UUID, input CreateOrganizationInput) (*domain.Organization, error) {
	// 1. Domain validation outside the database transaction
	org, err := domain.NewOrganizationWithMetadata(input.Name, input.Slug, input.Metadata, false)
	if err != nil {
		return nil, err
	}

	// 2. Atomic persistence within a managed transaction
	err = s.txManager.WithinTx(ctx, func(q database.Querier) error {
		// A. Insert organization entity
		if err := s.orgRepo.Create(ctx, q, org); err != nil {
			if errors.Is(err, domain.ErrDuplicateOrgSlug) {
				return domain.ErrDuplicateOrgSlug
			}
			reqLogger := logger.FromContext(ctx, s.logger)
			reqLogger.Error("failed to create organization record")
			return ErrOrganizationServiceUnavailable
		}

		// B. Create active Admin membership for the authenticated actor
		member, err := domain.NewMember(org.ID, actorUserID, domain.RoleAdmin)
		if err != nil {
			return err
		}

		if err := s.memberRepo.Create(ctx, q, member); err != nil {
			if errors.Is(err, domain.ErrDuplicateMembership) {
				return domain.ErrDuplicateMembership
			}
			reqLogger := logger.FromContext(ctx, s.logger)
			reqLogger.Error("failed to create creator membership record")
			return ErrOrganizationServiceUnavailable
		}

		return nil
	})

	if err != nil {
		if errors.Is(err, domain.ErrDuplicateOrgSlug) ||
			errors.Is(err, domain.ErrInvalidOrgName) ||
			errors.Is(err, domain.ErrInvalidOrgSlug) ||
			errors.Is(err, domain.ErrInvalidMetadata) ||
			errors.Is(err, domain.ErrDuplicateMembership) {
			return nil, err
		}
		return nil, ErrOrganizationServiceUnavailable
	}

	return org, nil
}

// ListUserOrganizations returns all active organizations the specified user holds active membership in.
func (s *Service) ListUserOrganizations(ctx context.Context, userID uuid.UUID) ([]*orgRepo.UserMembershipWithOrg, error) {
	q := s.txManager.Querier()
	memberships, err := s.memberRepo.ListUserMembershipsWithOrg(ctx, q, userID)
	if err != nil {
		reqLogger := logger.FromContext(ctx, s.logger)
		reqLogger.Error("failed to query user memberships")
		return nil, ErrOrganizationServiceUnavailable
	}

	if memberships == nil {
		return []*orgRepo.UserMembershipWithOrg{}, nil
	}

	return memberships, nil
}

// GetActiveOrganization retrieves an active organization by its UUID primary key.
func (s *Service) GetActiveOrganization(ctx context.Context, id uuid.UUID) (*domain.Organization, error) {
	q := s.txManager.Querier()
	org, err := s.orgRepo.FindActiveByID(ctx, q, id)
	if err != nil {
		if errors.Is(err, domain.ErrOrgNotFound) {
			return nil, domain.ErrOrgNotFound
		}
		reqLogger := logger.FromContext(ctx, s.logger)
		reqLogger.Error("failed to query active organization")
		return nil, ErrOrganizationServiceUnavailable
	}

	return org, nil
}

// UpdateOrganization validates editable fields and updates an active organization.
func (s *Service) UpdateOrganization(ctx context.Context, id uuid.UUID, input UpdateOrganizationInput) (*domain.Organization, error) {
	name := strings.TrimSpace(input.Name)
	nameRuneCount := utf8.RuneCountInString(name)
	if nameRuneCount == 0 || nameRuneCount > 100 {
		return nil, domain.ErrInvalidOrgName
	}

	trimmedMetadata := bytes.TrimSpace(input.Metadata)
	if len(trimmedMetadata) == 0 || !bytes.HasPrefix(trimmedMetadata, []byte("{")) || !bytes.HasSuffix(trimmedMetadata, []byte("}")) {
		return nil, domain.ErrInvalidMetadata
	}
	var obj map[string]any
	if err := json.Unmarshal(trimmedMetadata, &obj); err != nil || obj == nil {
		return nil, domain.ErrInvalidMetadata
	}

	q := s.txManager.Querier()
	org, err := s.orgRepo.UpdateActive(ctx, q, id, name, trimmedMetadata)
	if err != nil {
		if errors.Is(err, domain.ErrOrgNotFound) {
			return nil, domain.ErrOrgNotFound
		}
		reqLogger := logger.FromContext(ctx, s.logger)
		reqLogger.Error("failed to update active organization")
		return nil, ErrOrganizationServiceUnavailable
	}

	return org, nil
}
