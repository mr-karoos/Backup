package service

import (
	"context"
	"errors"
	"log/slog"
	"time"

	credDomain "backup-platform/internal/credential/domain"
	"backup-platform/internal/platform/database"
	"backup-platform/internal/platform/logger"
	"backup-platform/internal/resource/domain"
	"backup-platform/internal/resource/repository"
	"backup-platform/pkg/uuid"
)

// CredentialMetadataReader defines the tenant-scoped credential lookup operation required by ResourceService.
type CredentialMetadataReader interface {
	FindMetadataForOrganization(ctx context.Context, q database.Querier, orgID, credID uuid.UUID) (*credDomain.CredentialMetadata, error)
}

// CreateConnectorInput represents connector parameters for creating or updating a resource.
type CreateConnectorInput struct {
	Host               string
	Port               int
	AuthType           domain.AuthType
	Username           string
	CredentialID       uuid.UUID
	HostKeyFingerprint *string
	ConnectionTimeout  *int
	UseHTTPS           *bool
}

// CreateResourceInput encapsulates the input parameters for atomic resource and connector creation.
type CreateResourceInput struct {
	Name      string
	Type      domain.Type
	Connector CreateConnectorInput
}

// UpdateResourceInput encapsulates the input parameters for atomic resource and connector replacement.
type UpdateResourceInput struct {
	Name      string
	Connector CreateConnectorInput
}

// Service provides resource lifecycle management, credential association validation, and role-safe operations.
type Service struct {
	resourceRepo   repository.ResourceRepository
	credentialRepo CredentialMetadataReader
	txManager      database.TxManager
	logger         *slog.Logger
}

// NewService constructs a new Resource Application Service.
func NewService(
	resourceRepo repository.ResourceRepository,
	credentialRepo CredentialMetadataReader,
	txManager database.TxManager,
	log *slog.Logger,
) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{
		resourceRepo:   resourceRepo,
		credentialRepo: credentialRepo,
		txManager:      txManager,
		logger:         log,
	}
}

// CreateResource atomically validates credentials and creates a resource with its 1:1 connector.
func (s *Service) CreateResource(ctx context.Context, orgID uuid.UUID, input CreateResourceInput) (*domain.ResourceWithConnector, error) {
	if orgID == uuid.Nil {
		return nil, domain.ErrInvalidCredentialReference
	}

	validName, err := domain.ValidateResourceName(input.Name)
	if err != nil {
		return nil, err
	}

	if err := domain.ValidateResourceType(input.Type); err != nil {
		return nil, err
	}

	validHost, err := domain.ValidateConnectorHost(input.Connector.Host)
	if err != nil {
		return nil, err
	}

	if err := domain.ValidateConnectorPort(input.Connector.Port); err != nil {
		return nil, err
	}

	connectorConfig, err := domain.ValidateConnectorConfig(input.Type, input.Connector.Username, input.Connector.ConnectionTimeout, input.Connector.UseHTTPS)
	if err != nil {
		return nil, err
	}

	if err := domain.ValidateAuthType(input.Connector.AuthType, input.Type); err != nil {
		return nil, err
	}

	validFingerprint, err := domain.ValidateHostKeyFingerprint(input.Connector.HostKeyFingerprint, input.Type)
	if err != nil {
		return nil, err
	}

	if input.Connector.CredentialID == uuid.Nil {
		return nil, domain.ErrInvalidCredentialReference
	}

	// 1. Validate credential belongs to this organization and inspect safe metadata (never decrypts)
	credMeta, err := s.credentialRepo.FindMetadataForOrganization(ctx, s.txManager.Querier(), orgID, input.Connector.CredentialID)
	if err != nil {
		if errors.Is(err, credDomain.ErrCredentialNotFound) {
			return nil, domain.ErrInvalidCredentialReference
		}
		reqLogger := logger.FromContext(ctx, s.logger)
		reqLogger.Error("credential metadata lookup failed during resource creation")
		return nil, domain.ErrResourceServiceUnavailable
	}

	// 2. Validate credential type compatibility with connector auth type
	if err := domain.ValidateCredentialTypeCompatibility(input.Connector.AuthType, credMeta.Type); err != nil {
		return nil, err
	}

	// 3. Construct entities
	resourceID := uuid.New()
	connectorID := uuid.New()
	now := time.Now().UTC()

	res := &domain.Resource{
		ID:             resourceID,
		OrganizationID: orgID,
		Name:           validName,
		Type:           input.Type,
		Status:         domain.StatusActive,
		Metadata:       []byte("{}"),
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	conn := &domain.ResourceConnector{
		ID:                 connectorID,
		OrganizationID:     orgID,
		ResourceID:         resourceID,
		ConnectorType:      domain.ConnectorType(input.Type),
		CredentialID:       input.Connector.CredentialID,
		Host:               validHost,
		Port:               input.Connector.Port,
		AuthType:           input.Connector.AuthType,
		HostKeyFingerprint: validFingerprint,
		Config:             *connectorConfig,
		CreatedAt:          now,
		UpdatedAt:          now,
	}

	// 4. Atomically persist resource + connector
	err = s.txManager.WithinTx(ctx, func(tx database.Querier) error {
		if err := s.resourceRepo.CreateResource(ctx, tx, res); err != nil {
			return err
		}
		if err := s.resourceRepo.CreateConnector(ctx, tx, conn); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, domain.ErrInvalidCredentialReference) || errors.Is(err, domain.ErrResourceConflict) {
			return nil, err
		}
		reqLogger := logger.FromContext(ctx, s.logger)
		reqLogger.Error("resource creation transaction failed")
		return nil, domain.ErrResourceServiceUnavailable
	}

	return &domain.ResourceWithConnector{
		Resource:              res,
		Connector:             conn,
		CredentialName:        credMeta.Name,
		CredentialFingerprint: credMeta.Fingerprint,
	}, nil
}

// GetResource retrieves a specific non-archived resource and its connector within the tenant organization.
func (s *Service) GetResource(ctx context.Context, orgID, resID uuid.UUID) (*domain.ResourceWithConnector, error) {
	if orgID == uuid.Nil || resID == uuid.Nil {
		return nil, domain.ErrResourceNotFound
	}

	item, err := s.resourceRepo.FindByIDForOrganization(ctx, s.txManager.Querier(), orgID, resID)
	if err != nil {
		if errors.Is(err, domain.ErrResourceNotFound) {
			return nil, domain.ErrResourceNotFound
		}
		reqLogger := logger.FromContext(ctx, s.logger)
		reqLogger.Error("resource lookup failed")
		return nil, domain.ErrResourceServiceUnavailable
	}

	return item, nil
}

// ListResources lists all active/non-archived resources for an organization.
func (s *Service) ListResources(ctx context.Context, orgID uuid.UUID) ([]*domain.ResourceWithConnector, error) {
	if orgID == uuid.Nil {
		return nil, domain.ErrResourceNotFound
	}

	items, err := s.resourceRepo.ListForOrganization(ctx, s.txManager.Querier(), orgID)
	if err != nil {
		reqLogger := logger.FromContext(ctx, s.logger)
		reqLogger.Error("resource list failed")
		return nil, domain.ErrResourceServiceUnavailable
	}

	return items, nil
}

// UpdateResource atomically updates editable resource attributes (name) and connector configuration.
func (s *Service) UpdateResource(ctx context.Context, orgID, resID uuid.UUID, input UpdateResourceInput) (*domain.ResourceWithConnector, error) {
	if orgID == uuid.Nil || resID == uuid.Nil {
		return nil, domain.ErrResourceNotFound
	}

	validName, err := domain.ValidateResourceName(input.Name)
	if err != nil {
		return nil, err
	}

	// 1. Load existing resource to verify existence, tenant ownership, and immutable Type
	existing, err := s.resourceRepo.FindByIDForOrganization(ctx, s.txManager.Querier(), orgID, resID)
	if err != nil {
		if errors.Is(err, domain.ErrResourceNotFound) {
			return nil, domain.ErrResourceNotFound
		}
		reqLogger := logger.FromContext(ctx, s.logger)
		reqLogger.Error("resource lookup failed during update")
		return nil, domain.ErrResourceServiceUnavailable
	}

	validHost, err := domain.ValidateConnectorHost(input.Connector.Host)
	if err != nil {
		return nil, err
	}

	if err := domain.ValidateConnectorPort(input.Connector.Port); err != nil {
		return nil, err
	}

	connectorConfig, err := domain.ValidateConnectorConfig(existing.Resource.Type, input.Connector.Username, input.Connector.ConnectionTimeout, input.Connector.UseHTTPS)
	if err != nil {
		return nil, err
	}

	// Auth type must be compatible with the immutable resource type
	if err := domain.ValidateAuthType(input.Connector.AuthType, existing.Resource.Type); err != nil {
		return nil, err
	}

	validFingerprint, err := domain.ValidateHostKeyFingerprint(input.Connector.HostKeyFingerprint, existing.Resource.Type)
	if err != nil {
		return nil, err
	}

	if input.Connector.CredentialID == uuid.Nil {
		return nil, domain.ErrInvalidCredentialReference
	}

	// 2. Validate new credential belongs to this organization
	credMeta, err := s.credentialRepo.FindMetadataForOrganization(ctx, s.txManager.Querier(), orgID, input.Connector.CredentialID)
	if err != nil {
		if errors.Is(err, credDomain.ErrCredentialNotFound) {
			return nil, domain.ErrInvalidCredentialReference
		}
		reqLogger := logger.FromContext(ctx, s.logger)
		reqLogger.Error("credential metadata lookup failed during resource update")
		return nil, domain.ErrResourceServiceUnavailable
	}

	// 3. Validate credential type compatibility with connector auth type
	if err := domain.ValidateCredentialTypeCompatibility(input.Connector.AuthType, credMeta.Type); err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	updatedRes := &domain.Resource{
		ID:                   existing.Resource.ID,
		OrganizationID:       orgID,
		Name:                 validName,
		Type:                 existing.Resource.Type,
		Status:               existing.Resource.Status,
		LastConnectionTestAt: existing.Resource.LastConnectionTestAt,
		LastConnectionStatus: existing.Resource.LastConnectionStatus,
		LastConnectionError:  existing.Resource.LastConnectionError,
		Metadata:             existing.Resource.Metadata,
		CreatedAt:            existing.Resource.CreatedAt,
		UpdatedAt:            now,
	}

	updatedConn := &domain.ResourceConnector{
		ID:                 existing.Connector.ID,
		OrganizationID:     orgID,
		ResourceID:         existing.Resource.ID,
		ConnectorType:      existing.Connector.ConnectorType,
		CredentialID:       input.Connector.CredentialID,
		Host:               validHost,
		Port:               input.Connector.Port,
		AuthType:           input.Connector.AuthType,
		HostKeyFingerprint: validFingerprint,
		Config:             *connectorConfig,
		CreatedAt:          existing.Connector.CreatedAt,
		UpdatedAt:          now,
	}

	// 4. Atomically update resource and connector
	err = s.txManager.WithinTx(ctx, func(tx database.Querier) error {
		if err := s.resourceRepo.UpdateResource(ctx, tx, updatedRes); err != nil {
			return err
		}
		if err := s.resourceRepo.UpdateConnector(ctx, tx, updatedConn); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, domain.ErrResourceNotFound) || errors.Is(err, domain.ErrInvalidCredentialReference) {
			return nil, err
		}
		reqLogger := logger.FromContext(ctx, s.logger)
		reqLogger.Error("resource update transaction failed")
		return nil, domain.ErrResourceServiceUnavailable
	}

	return &domain.ResourceWithConnector{
		Resource:              updatedRes,
		Connector:             updatedConn,
		CredentialName:        credMeta.Name,
		CredentialFingerprint: credMeta.Fingerprint,
	}, nil
}

// ArchiveResource soft-deletes a resource by setting status = 'archived'.
func (s *Service) ArchiveResource(ctx context.Context, orgID, resID uuid.UUID) error {
	if orgID == uuid.Nil || resID == uuid.Nil {
		return domain.ErrResourceNotFound
	}

	err := s.resourceRepo.ArchiveForOrganization(ctx, s.txManager.Querier(), orgID, resID)
	if err != nil {
		if errors.Is(err, domain.ErrResourceNotFound) {
			return domain.ErrResourceNotFound
		}
		reqLogger := logger.FromContext(ctx, s.logger)
		reqLogger.Error("resource archive failed")
		return domain.ErrResourceServiceUnavailable
	}

	return nil
}
