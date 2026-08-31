package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"backup-platform/internal/connector"
	"backup-platform/internal/connector/sshconn"
	credDomain "backup-platform/internal/credential/domain"
	"backup-platform/internal/credential/payload"
	"backup-platform/internal/credential/secretcrypto"
	"backup-platform/internal/platform/database"
	"backup-platform/internal/platform/logger"
	"backup-platform/internal/resource/domain"
	"backup-platform/internal/resource/repository"
	"backup-platform/pkg/uuid"
)

// VaultReader defines the minimal credential loading interface required for connection testing.
type VaultReader interface {
	LoadCredentialForUse(ctx context.Context, orgID, credID uuid.UUID) (credDomain.Type, []byte, error)
}

// ConnectionTestResponseData contains the sanitized result of an operational connection probe.
type ConnectionTestResponseData struct {
	Status    string         `json:"status"` // "success" | "failed"
	LatencyMS int64          `json:"latency_ms"`
	CheckedAt time.Time      `json:"checked_at"`
	Details   map[string]any `json:"details"`
}

// ConnectionTestService coordinates live operational connection tests and state updates.
type ConnectionTestService struct {
	resourceRepo repository.ResourceRepository
	vault        VaultReader
	registry     *connector.Registry
	txManager    database.TxManager
	logger       *slog.Logger
}

// NewConnectionTestService constructs a new ConnectionTestService.
func NewConnectionTestService(
	resourceRepo repository.ResourceRepository,
	vault VaultReader,
	registry *connector.Registry,
	txManager database.TxManager,
	appLogger *slog.Logger,
) *ConnectionTestService {
	return &ConnectionTestService{
		resourceRepo: resourceRepo,
		vault:        vault,
		registry:     registry,
		txManager:    txManager,
		logger:       appLogger,
	}
}

// TestConnection executes an end-to-end operational connection probe against the target resource.
// No database transaction is open during the network I/O.
func (s *ConnectionTestService) TestConnection(
	ctx context.Context,
	orgID, resID uuid.UUID,
) (*ConnectionTestResponseData, error) {
	// 1. Check parent context cancellation
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// 2. Validate Input Identifiers
	if orgID == uuid.Nil || resID == uuid.Nil {
		return nil, domain.ErrResourceNotFound
	}

	// 3. Tenant-scoped Resource & Connector lookup
	res, err := s.resourceRepo.FindByIDForOrganization(ctx, s.txManager.Querier(), orgID, resID)
	if err != nil {
		if errors.Is(err, domain.ErrResourceNotFound) {
			return nil, domain.ErrResourceNotFound
		}
		logger.FromContext(ctx, s.logger).Error("resource lookup failed during connection test")
		return nil, domain.ErrResourceServiceUnavailable
	}

	// 4. Preflight operational validation (before Vault decrypt to avoid unnecessary secret exposure)
	if res.Resource.Type == domain.TypeUbuntuSSH {
		if res.Connector.HostKeyFingerprint == nil || *res.Connector.HostKeyFingerprint == "" {
			return nil, domain.ErrInvalidHostKeyFingerprint
		}
	}
	if res.Resource.Type == domain.TypeCPanel {
		if res.Connector.Config.UseHTTPS != nil && !*res.Connector.Config.UseHTTPS {
			return nil, domain.ErrInvalidConnectorConfig
		}
		if err := domain.ValidateCPanelOperationalUsername(res.Connector.Config.Username); err != nil {
			return nil, err
		}
	}

	// 5. Resolve capability connection tester
	tester, err := s.registry.Get(res.Resource.Type)
	if err != nil {
		logger.FromContext(ctx, s.logger).Error("no connection tester registered for resource type")
		return nil, domain.ErrResourceServiceUnavailable
	}

	// 6. Tenant-scoped Credential loading & decryption from Vault
	credType, decryptedPayload, err := s.vault.LoadCredentialForUse(ctx, orgID, res.Connector.CredentialID)
	if err != nil {
		if errors.Is(err, credDomain.ErrCredentialNotFound) {
			logger.FromContext(ctx, s.logger).Error("credential referenced by connector not found")
			return nil, domain.ErrResourceServiceUnavailable
		}
		logger.FromContext(ctx, s.logger).Error("failed to load credential for connection test")
		return nil, domain.ErrResourceServiceUnavailable
	}
	defer secretcrypto.ZeroBytes(decryptedPayload)

	// 7. Defense-in-depth: Validate credential type compatibility with connector auth type
	if err := domain.ValidateCredentialTypeCompatibility(res.Connector.AuthType, credType); err != nil {
		logger.FromContext(ctx, s.logger).Error("stored credential type incompatible with connector auth type")
		return nil, domain.ErrResourceServiceUnavailable
	}

	// 8. Parse versioned plaintext payload and immediately zero decrypted JSON bytes before network I/O
	credPayload, err := payload.Decode(decryptedPayload)
	secretcrypto.ZeroBytes(decryptedPayload)
	if err != nil {
		logger.FromContext(ctx, s.logger).Error("failed to decode credential payload")
		return nil, domain.ErrResourceServiceUnavailable
	}
	defer payload.Clear(credPayload)

	// 9. Build target probe configuration
	target := connector.Target{
		ResourceID:         res.Resource.ID,
		OrganizationID:     res.Resource.OrganizationID,
		ResourceType:       res.Resource.Type,
		Host:               res.Connector.Host,
		Port:               res.Connector.Port,
		AuthType:           res.Connector.AuthType,
		Username:           res.Connector.Config.Username,
		HostKeyFingerprint: res.Connector.HostKeyFingerprint,
		ConnectionTimeout:  res.Connector.Config.ConnectionTimeoutSeconds,
		UseHTTPS:           res.Connector.Config.UseHTTPS,
	}

	// 10. Execute live operational network probe (NO DB TRANSACTION HELD)
	probeResult, err := tester.TestConnection(ctx, target, credPayload)

	// Immediately clear decoded payload string references
	payload.Clear(credPayload)

	if err != nil {
		// If caller parent context was canceled, return context error directly without updating DB
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		// Preflight domain validation errors
		if errors.Is(err, domain.ErrInvalidHostKeyFingerprint) ||
			errors.Is(err, domain.ErrInvalidConnectorConfig) ||
			errors.Is(err, domain.ErrInvalidAuthType) {
			return nil, err
		}

		// Internal tester failure (including internal context.Canceled / DeadlineExceeded when parent ctx is active)
		logger.FromContext(ctx, s.logger).Error("internal failure executing connection probe")
		return nil, domain.ErrResourceServiceUnavailable
	}

	// 11. Strict ProbeResult Invariant Validation & Canonical Reason Derivation
	canonicalReason, err := validateAndDeriveFailureReason(res.Resource.Type, probeResult)
	if err != nil {
		logger.FromContext(ctx, s.logger).Error("connection probe result contract violation")
		return nil, domain.ErrResourceServiceUnavailable
	}

	// 12. Determine status transitions
	var newResourceStatus domain.Status
	if res.Resource.Status == domain.StatusDisabled {
		// Disabled resource remains disabled
		newResourceStatus = domain.StatusDisabled
	} else if probeResult.Success {
		newResourceStatus = domain.StatusActive
	} else {
		newResourceStatus = domain.StatusUnreachable
	}

	var connStatus domain.ConnectionStatus
	var lastError *string

	if probeResult.Success {
		connStatus = domain.ConnectionStatusSuccess
		lastError = nil
	} else {
		connStatus = domain.ConnectionStatusFailed
		lastError = &canonicalReason
	}

	// 13. Persist connection state to database (short DB update)
	err = s.resourceRepo.UpdateConnectionTestStateForOrganization(
		ctx,
		s.txManager.Querier(),
		orgID,
		resID,
		probeResult.CheckedAt,
		connStatus,
		lastError,
		newResourceStatus,
	)
	if err != nil {
		if errors.Is(err, domain.ErrResourceNotFound) {
			return nil, domain.ErrResourceNotFound
		}
		logger.FromContext(ctx, s.logger).Error("failed to persist connection test state")
		return nil, domain.ErrResourceServiceUnavailable
	}

	// 14. Build strictly allowlisted client response DTO
	statusStr := "success"
	if !probeResult.Success {
		statusStr = "failed"
	}

	safeDetails := make(map[string]any)
	if probeResult.Success {
		// Derive auth_method locally from platform configuration (never trust tester-provided auth_method)
		authMethod := deriveLocalAuthMethod(res.Connector.AuthType)
		if authMethod != "" {
			safeDetails["auth_method"] = authMethod
		}

		switch res.Resource.Type {
		case domain.TypeUbuntuSSH:
			if bannerRaw, ok := probeResult.Details["server_banner"].(string); ok {
				cleanedBanner := sshconn.SanitizeBanner(bannerRaw)
				if cleanedBanner != "" {
					safeDetails["server_banner"] = cleanedBanner
				}
			}

		case domain.TypeCPanel:
			if apiVer, ok := probeResult.Details["api_version"].(int); ok && apiVer > 0 {
				safeDetails["api_version"] = apiVer
			}
		}
	} else {
		safeDetails["reason"] = canonicalReason
	}

	return &ConnectionTestResponseData{
		Status:    statusStr,
		LatencyMS: probeResult.Latency.Milliseconds(),
		CheckedAt: probeResult.CheckedAt,
		Details:   safeDetails,
	}, nil
}

func validateAndDeriveFailureReason(resType domain.Type, probe *connector.ProbeResult) (string, error) {
	if probe == nil {
		return "", errors.New("nil probe result")
	}
	if probe.CheckedAt.IsZero() {
		return "", errors.New("zero checked_at timestamp")
	}
	if probe.Latency < 0 {
		return "", errors.New("negative probe latency")
	}

	if probe.Success {
		if probe.FailureKind != connector.FailureKindNone {
			return "", fmt.Errorf("inconsistent probe result: success=true with failure_kind=%q", probe.FailureKind)
		}
		if resType == domain.TypeCPanel {
			if probe.Details == nil {
				return "", errors.New("cPanel success probe missing details map")
			}
			apiVer, ok := probe.Details["api_version"].(int)
			if !ok || apiVer <= 0 {
				return "", errors.New("cPanel success probe missing or invalid integer api_version")
			}
		}
		return "", nil
	}

	// Failed probe: derive canonical reason strictly from known FailureKind enum
	switch probe.FailureKind {
	case connector.FailureKindTimeout:
		return "connection timed out", nil
	case connector.FailureKindAuthFailed:
		return "authentication failed", nil
	case connector.FailureKindHostKeyMismatch:
		return "SSH host key verification failed", nil
	case connector.FailureKindTLSVerificationFailed:
		return "TLS certificate verification failed", nil
	case connector.FailureKindConnFailed:
		return "remote service did not accept the connection", nil
	case connector.FailureKindRemoteAPIFailed:
		return "remote service did not accept the connection", nil
	default:
		// FailureKindNone on failure or any unknown failure kind -> internal contract violation
		return "", fmt.Errorf("invalid failure kind for failed probe: %q", probe.FailureKind)
	}
}

func deriveLocalAuthMethod(authType domain.AuthType) string {
	switch authType {
	case domain.AuthTypeSSHKey:
		return "publickey"
	case domain.AuthTypeSSHPassword:
		return "password"
	case domain.AuthTypeCPanelAPIToken:
		return "api_token"
	case domain.AuthTypeCPanelPassword:
		return "password"
	default:
		return ""
	}
}
