package service

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"backup-platform/internal/connector"
	"backup-platform/internal/credential/payload"
	"backup-platform/internal/credential/secretcrypto"
	"backup-platform/internal/platform/database"
	"backup-platform/internal/platform/logger"
	"backup-platform/internal/resource/domain"
	"backup-platform/internal/resource/repository"
	"backup-platform/pkg/uuid"
)

// DatabaseDiscoveryService coordinates secure MySQL database discovery across connectors and runtime credentials.
type DatabaseDiscoveryService struct {
	resourceRepo repository.ResourceRepository
	vault        VaultReader
	registry     *connector.DiscoveryRegistry
	txManager    database.TxManager
	logger       *slog.Logger
}

// NewDatabaseDiscoveryService creates an operational DatabaseDiscoveryService instance.
func NewDatabaseDiscoveryService(
	resourceRepo repository.ResourceRepository,
	vault VaultReader,
	registry *connector.DiscoveryRegistry,
	txManager database.TxManager,
	log *slog.Logger,
) *DatabaseDiscoveryService {
	if log == nil {
		log = slog.Default()
	}
	return &DatabaseDiscoveryService{
		resourceRepo: resourceRepo,
		vault:        vault,
		registry:     registry,
		txManager:    txManager,
		logger:       log,
	}
}

// DiscoverDatabases performs tenant-isolated, ephemeral MySQL discovery for an active or disabled resource.
func (s *DatabaseDiscoveryService) DiscoverDatabases(
	ctx context.Context,
	orgID, resID uuid.UUID,
) ([]connector.DatabaseInfo, error) {
	reqLogger := logger.FromContext(ctx, s.logger)

	// 1. Validate UUIDs
	if orgID == uuid.Nil || resID == uuid.Nil {
		return nil, domain.ErrResourceNotFound
	}

	// 2. Tenant-scoped Resource & Connector lookup
	res, err := s.resourceRepo.FindByIDForOrganization(ctx, s.txManager.Querier(), orgID, resID)
	if err != nil {
		if errors.Is(err, domain.ErrResourceNotFound) {
			return nil, domain.ErrResourceNotFound
		}
		if errors.Is(err, domain.ErrCorruptResourceData) {
			reqLogger.Error("corrupt resource data encountered during database discovery")
			return nil, domain.ErrResourceServiceUnavailable
		}
		reqLogger.Error("database query failed during database discovery resource lookup")
		return nil, domain.ErrResourceServiceUnavailable
	}

	if res.Connector == nil {
		reqLogger.Error("resource connector is missing")
		return nil, domain.ErrResourceServiceUnavailable
	}

	// 3. Preflight operational validation (before decrypting credentials or network I/O)
	switch res.Resource.Type {
	case domain.TypeUbuntuSSH:
		if res.Connector.HostKeyFingerprint == nil || strings.TrimSpace(*res.Connector.HostKeyFingerprint) == "" {
			return nil, domain.ErrInvalidHostKeyFingerprint
		}

	case domain.TypeCPanel:
		if res.Connector.Config.UseHTTPS != nil && !*res.Connector.Config.UseHTTPS {
			return nil, domain.ErrInvalidConnectorConfig
		}
		if err := domain.ValidateCPanelOperationalUsername(res.Connector.Config.Username); err != nil {
			return nil, domain.ErrInvalidConnectorConfig
		}

	default:
		return nil, domain.ErrInvalidResourceType
	}

	// 4. Resolve Database Discoverer from Registry before decrypting credentials
	discoverer, err := s.registry.Get(res.Resource.Type)
	if err != nil {
		reqLogger.Error("no database discoverer registered for resource type")
		return nil, domain.ErrResourceServiceUnavailable
	}

	// 5. Load Credential from Runtime Vault
	credType, decryptedPayload, err := s.vault.LoadCredentialForUse(ctx, orgID, res.Connector.CredentialID)
	if err != nil {
		reqLogger.Error("failed to load credential from vault for database discovery")
		return nil, domain.ErrResourceServiceUnavailable
	}

	// 6. Validate runtime credential compatibility against connector auth type
	if err := domain.ValidateCredentialTypeCompatibility(res.Connector.AuthType, credType); err != nil {
		secretcrypto.ZeroBytes(decryptedPayload)
		reqLogger.Error("incompatible credential type loaded for database discovery")
		return nil, domain.ErrResourceServiceUnavailable
	}

	// 7. Decode Credential Payload and immediately zero decrypted JSON buffer
	credPayload, err := payload.Decode(decryptedPayload)
	secretcrypto.ZeroBytes(decryptedPayload)
	if err != nil {
		reqLogger.Error("failed to decode credential payload for database discovery")
		return nil, domain.ErrResourceServiceUnavailable
	}
	defer payload.Clear(credPayload)

	// 8. Prepare Connector Target
	target := connector.Target{
		Host:               res.Connector.Host,
		Port:               res.Connector.Port,
		Username:           res.Connector.Config.Username,
		AuthType:           res.Connector.AuthType,
		HostKeyFingerprint: res.Connector.HostKeyFingerprint,
		UseHTTPS:           res.Connector.Config.UseHTTPS,
		ConnectionTimeout:  res.Connector.Config.ConnectionTimeoutSeconds,
	}

	// 9. Execute Discovery with immediate payload string reference cleanup
	rawDatabases, discErr := discoverer.DiscoverDatabases(ctx, target, credPayload)
	payload.Clear(credPayload)

	if discErr != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		reqLogger.Error("remote database discovery failed")
		return nil, domain.ErrResourceServiceUnavailable
	}

	// 10. Normalize, filter system DBs, validate integrity, and sort ascending
	databases, normErr := connector.NormalizeDiscoveredDatabases(rawDatabases)
	if normErr != nil {
		reqLogger.Error("database discovery result normalization failed")
		return nil, domain.ErrResourceServiceUnavailable
	}

	return databases, nil
}
