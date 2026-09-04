package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"backup-platform/internal/backup/domain"
	"backup-platform/internal/backup/repository"
	credDomain "backup-platform/internal/credential/domain"
	orgDomain "backup-platform/internal/organization/domain"
	s3Storage "backup-platform/internal/storage/s3"
	"backup-platform/pkg/uuid"
)

// CredentialMetadataFinder allows validating credential existence and type for storage targets.
type CredentialMetadataFinder interface {
	GetCredentialMetadata(ctx context.Context, orgID, credID uuid.UUID) (*credDomain.CredentialMetadata, error)
}

// CreateStorageTargetInput encapsulates parameters for creating a new StorageTarget.
type CreateStorageTargetInput struct {
	Name         string                   `json:"name"`
	Type         domain.StorageTargetType `json:"type"`
	CredentialID *uuid.UUID               `json:"credential_id,omitempty"`
	Config       json.RawMessage          `json:"config,omitempty"`
}

// UpdateStorageTargetInput encapsulates parameters for updating an existing StorageTarget.
type UpdateStorageTargetInput struct {
	Name         string                     `json:"name"`
	Status       domain.StorageTargetStatus `json:"status"`
	CredentialID *uuid.UUID                 `json:"credential_id,omitempty"`
	Config       json.RawMessage            `json:"config,omitempty"`
}

// StorageTargetService coordinates lifecycle management, authorization, immutability, and validation for storage targets.
type StorageTargetService struct {
	repo             repository.StorageTargetRepository
	credentialFinder CredentialMetadataFinder
	securityPolicy   *s3Storage.EndpointSecurityPolicy
	logger           *slog.Logger
}

// NewStorageTargetService constructs a new StorageTargetService.
func NewStorageTargetService(
	repo repository.StorageTargetRepository,
	credentialFinder CredentialMetadataFinder,
	securityPolicy *s3Storage.EndpointSecurityPolicy,
	logger *slog.Logger,
) *StorageTargetService {
	if logger == nil {
		logger = slog.Default()
	}
	return &StorageTargetService{
		repo:             repo,
		credentialFinder: credentialFinder,
		securityPolicy:   securityPolicy,
		logger:           logger,
	}
}

// CreateStorageTarget creates a new tenant-scoped storage target.
func (s *StorageTargetService) CreateStorageTarget(
	ctx context.Context,
	userRole orgDomain.Role,
	orgID uuid.UUID,
	input CreateStorageTargetInput,
) (*domain.StorageTarget, error) {
	if userRole != orgDomain.RoleAdmin {
		return nil, domain.ErrUnauthorizedRole
	}
	if orgID == uuid.Nil {
		return nil, domain.ErrBackupServiceUnavailable
	}

	validName, err := domain.ValidateStorageTargetName(input.Name)
	if err != nil {
		return nil, domain.ErrInvalidStorageTargetName
	}

	var targetConfigBytes []byte
	var credentialID *uuid.UUID

	switch input.Type {
	case domain.StorageTargetTypeS3:
		if input.CredentialID == nil || *input.CredentialID == uuid.Nil {
			return nil, domain.ErrStorageTargetCredentialRequired
		}
		if s.credentialFinder != nil {
			meta, err := s.credentialFinder.GetCredentialMetadata(ctx, orgID, *input.CredentialID)
			if err != nil {
				if errors.Is(err, credDomain.ErrCredentialNotFound) {
					return nil, errors.New("referenced credential does not exist in organization")
				}
				return nil, domain.ErrBackupServiceUnavailable
			}
			if meta.Type != credDomain.TypeS3Credentials {
				return nil, errors.New("credential must be of type s3_credentials")
			}
		}
		credentialID = input.CredentialID

		s3Config, err := domain.ParseS3TargetConfig(input.Config)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", domain.ErrInvalidStorageTargetConfig, err)
		}
		if s.securityPolicy != nil {
			if _, err := s.securityPolicy.ValidateEndpointURL(s3Config.Endpoint); err != nil {
				return nil, fmt.Errorf("%w: invalid s3 endpoint: %v", domain.ErrInvalidStorageTargetConfig, err)
			}
		}
		cfgJSON, err := json.Marshal(s3Config)
		if err != nil {
			return nil, fmt.Errorf("%w: failed marshaling s3 config: %v", domain.ErrInvalidStorageTargetConfig, err)
		}
		targetConfigBytes = cfgJSON

	case domain.StorageTargetTypeLocal:
		return nil, errors.New("manual creation of local storage targets is not permitted; default local target is provisioned per organization")

	default:
		return nil, domain.ErrStorageTargetNotSupported
	}

	target := &domain.StorageTarget{
		ID:             uuid.New(),
		OrganizationID: orgID,
		Name:           validName,
		Type:           input.Type,
		Status:         domain.StorageTargetStatusActive,
		IsDefault:      false,
		CredentialID:   credentialID,
		Config:         targetConfigBytes,
	}

	created, err := s.repo.CreateStorageTarget(ctx, target)
	if err != nil {
		return nil, err
	}
	return created, nil
}

// GetStorageTarget retrieves a storage target by ID within the organization.
func (s *StorageTargetService) GetStorageTarget(
	ctx context.Context,
	userRole orgDomain.Role,
	orgID, targetID uuid.UUID,
) (*domain.StorageTarget, error) {
	if userRole != orgDomain.RoleAdmin && userRole != orgDomain.RoleMember && userRole != orgDomain.RoleViewer {
		return nil, domain.ErrUnauthorizedRole
	}
	if orgID == uuid.Nil || targetID == uuid.Nil {
		return nil, domain.ErrStorageTargetNotFound
	}

	target, err := s.repo.GetStorageTargetByID(ctx, orgID, targetID)
	if err != nil {
		if errors.Is(err, domain.ErrStorageTargetNotFound) || errors.Is(err, domain.ErrStorageTargetNotSupported) {
			return nil, domain.ErrStorageTargetNotFound
		}
		return nil, domain.ErrBackupServiceUnavailable
	}
	return target, nil
}

// ListStorageTargets lists all active storage targets for an organization.
func (s *StorageTargetService) ListStorageTargets(
	ctx context.Context,
	userRole orgDomain.Role,
	orgID uuid.UUID,
) ([]*domain.StorageTarget, error) {
	if userRole != orgDomain.RoleAdmin && userRole != orgDomain.RoleMember && userRole != orgDomain.RoleViewer {
		return nil, domain.ErrUnauthorizedRole
	}
	if orgID == uuid.Nil {
		return nil, domain.ErrBackupServiceUnavailable
	}

	targets, err := s.repo.ListStorageTargets(ctx, orgID)
	if err != nil {
		return nil, domain.ErrBackupServiceUnavailable
	}
	return targets, nil
}

// UpdateStorageTarget updates an existing storage target, enforcing location immutability if artifacts exist.
func (s *StorageTargetService) UpdateStorageTarget(
	ctx context.Context,
	userRole orgDomain.Role,
	orgID, targetID uuid.UUID,
	input UpdateStorageTargetInput,
) (*domain.StorageTarget, error) {
	if userRole != orgDomain.RoleAdmin {
		return nil, domain.ErrUnauthorizedRole
	}
	if orgID == uuid.Nil || targetID == uuid.Nil {
		return nil, domain.ErrStorageTargetNotFound
	}

	existing, err := s.repo.GetStorageTargetByID(ctx, orgID, targetID)
	if err != nil {
		if errors.Is(err, domain.ErrStorageTargetNotFound) || errors.Is(err, domain.ErrStorageTargetNotSupported) {
			return nil, domain.ErrStorageTargetNotFound
		}
		return nil, domain.ErrBackupServiceUnavailable
	}

	nameToUse := existing.Name
	if input.Name != "" {
		validName, err := domain.ValidateStorageTargetName(input.Name)
		if err != nil {
			return nil, domain.ErrInvalidStorageTargetName
		}
		nameToUse = validName
	}

	statusToUse := existing.Status
	if input.Status != "" {
		switch input.Status {
		case domain.StorageTargetStatusActive, domain.StorageTargetStatusDisabled, domain.StorageTargetStatusArchived:
			statusToUse = input.Status
		default:
			return nil, errors.New("invalid storage target status")
		}
	}

	// Default targets cannot be disabled or archived
	if existing.IsDefault && statusToUse != domain.StorageTargetStatusActive {
		return nil, domain.ErrCannotDeleteDefaultStorageTarget
	}

	updatedConfigBytes := existing.Config
	updatedCredID := existing.CredentialID

	if existing.Type == domain.StorageTargetTypeS3 {
		if input.CredentialID != nil && *input.CredentialID != uuid.Nil {
			if s.credentialFinder != nil {
				meta, err := s.credentialFinder.GetCredentialMetadata(ctx, orgID, *input.CredentialID)
				if err != nil {
					if errors.Is(err, credDomain.ErrCredentialNotFound) {
						return nil, errors.New("referenced credential does not exist in organization")
					}
					return nil, domain.ErrBackupServiceUnavailable
				}
				if meta.Type != credDomain.TypeS3Credentials {
					return nil, errors.New("credential must be of type s3_credentials")
				}
			}
			updatedCredID = input.CredentialID
		}

		if len(input.Config) > 0 {
			newS3Config, err := domain.ParseS3TargetConfig(input.Config)
			if err != nil {
				return nil, fmt.Errorf("%w: %v", domain.ErrInvalidStorageTargetConfig, err)
			}
			if s.securityPolicy != nil {
				if _, err := s.securityPolicy.ValidateEndpointURL(newS3Config.Endpoint); err != nil {
					return nil, fmt.Errorf("%w: invalid s3 endpoint: %v", domain.ErrInvalidStorageTargetConfig, err)
				}
			}

			// Immutability check: if target has historical artifacts or restic repositories, location fields (Bucket, Endpoint, Region, ForcePathStyle, Prefix) CANNOT change
			existingS3Config, err := domain.ParseS3TargetConfig(existing.Config)
			if err == nil {
				locationChanged := existingS3Config.Bucket != newS3Config.Bucket ||
					existingS3Config.Endpoint != newS3Config.Endpoint ||
					existingS3Config.Region != newS3Config.Region ||
					existingS3Config.ForcePathStyle != newS3Config.ForcePathStyle ||
					existingS3Config.Prefix != newS3Config.Prefix

				if locationChanged {
					artifactCount, err := s.repo.CountArtifactsByStorageTarget(ctx, orgID, targetID)
					if err != nil {
						return nil, domain.ErrBackupServiceUnavailable
					}
					if artifactCount > 0 {
						return nil, domain.ErrStorageTargetLocationImmutable
					}

					repoCount, err := s.repo.CountRepositoriesByStorageTarget(ctx, orgID, targetID)
					if err != nil {
						return nil, domain.ErrBackupServiceUnavailable
					}
					if repoCount > 0 {
						return nil, domain.ErrStorageTargetLocationImmutable
					}
				}
			}

			cfgJSON, err := json.Marshal(newS3Config)
			if err != nil {
				return nil, fmt.Errorf("%w: failed marshaling s3 config: %v", domain.ErrInvalidStorageTargetConfig, err)
			}
			updatedConfigBytes = cfgJSON
		}
	}

	// If changing status away from active, verify no restic repositories depend on this target
	if statusToUse != domain.StorageTargetStatusActive && existing.Status == domain.StorageTargetStatusActive {
		repoCount, err := s.repo.CountRepositoriesByStorageTarget(ctx, orgID, targetID)
		if err != nil {
			return nil, domain.ErrBackupServiceUnavailable
		}
		if repoCount > 0 {
			return nil, domain.ErrStorageTargetInUse
		}
	}

	targetToUpdate := &domain.StorageTarget{
		ID:             targetID,
		OrganizationID: orgID,
		Name:           nameToUse,
		Type:           existing.Type,
		Status:         statusToUse,
		IsDefault:      existing.IsDefault,
		Config:         updatedConfigBytes,
		CredentialID:   updatedCredID,
		CreatedAt:      existing.CreatedAt,
	}

	updated, err := s.repo.UpdateStorageTarget(ctx, targetToUpdate)
	if err != nil {
		return nil, err
	}
	return updated, nil
}

// DeleteStorageTarget deletes a storage target, rejecting deletion if it is default or referenced by artifacts, plans, active jobs, or restic repositories.
func (s *StorageTargetService) DeleteStorageTarget(
	ctx context.Context,
	userRole orgDomain.Role,
	orgID, targetID uuid.UUID,
) error {
	if userRole != orgDomain.RoleAdmin {
		return domain.ErrUnauthorizedRole
	}
	if orgID == uuid.Nil || targetID == uuid.Nil {
		return domain.ErrStorageTargetNotFound
	}

	existing, err := s.repo.GetStorageTargetByID(ctx, orgID, targetID)
	if err != nil {
		if errors.Is(err, domain.ErrStorageTargetNotFound) || errors.Is(err, domain.ErrStorageTargetNotSupported) {
			return domain.ErrStorageTargetNotFound
		}
		return domain.ErrBackupServiceUnavailable
	}

	if existing.IsDefault {
		return domain.ErrCannotDeleteDefaultStorageTarget
	}

	// Verify target is not in use by artifacts
	artCount, err := s.repo.CountArtifactsByStorageTarget(ctx, orgID, targetID)
	if err != nil {
		return domain.ErrBackupServiceUnavailable
	}
	if artCount > 0 {
		return domain.ErrStorageTargetInUse
	}

	// Verify target is not in use by active plans
	planCount, err := s.repo.CountPlansByStorageTarget(ctx, orgID, targetID)
	if err != nil {
		return domain.ErrBackupServiceUnavailable
	}
	if planCount > 0 {
		return domain.ErrStorageTargetInUse
	}

	// Verify target is not in use by active jobs
	jobCount, err := s.repo.CountActiveJobsByStorageTarget(ctx, orgID, targetID)
	if err != nil {
		return domain.ErrBackupServiceUnavailable
	}
	if jobCount > 0 {
		return domain.ErrStorageTargetInUse
	}

	// Verify target is not in use by restic repositories
	repoCount, err := s.repo.CountRepositoriesByStorageTarget(ctx, orgID, targetID)
	if err != nil {
		return domain.ErrBackupServiceUnavailable
	}
	if repoCount > 0 {
		return domain.ErrStorageTargetInUse
	}

	return s.repo.DeleteStorageTarget(ctx, orgID, targetID)
}
