package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"

	"backup-platform/internal/backup/domain"
	"backup-platform/internal/backup/repository"
	"backup-platform/internal/backup/restic"
	credDomain "backup-platform/internal/credential/domain"
	"backup-platform/internal/credential/secretcrypto"
	resDomain "backup-platform/internal/resource/domain"
	"backup-platform/pkg/uuid"
)

// SystemCredentialVault defines the interface for managing system-controlled credentials.
type SystemCredentialVault interface {
	CreateSystemCredential(ctx context.Context, orgID uuid.UUID, name string, credType credDomain.Type, plaintextPayload []byte) (*credDomain.CredentialMetadata, error)
	LoadCredentialForUse(ctx context.Context, orgID, credID uuid.UUID) (credDomain.Type, []byte, error)
	DeleteSystemCredential(ctx context.Context, orgID, credID uuid.UUID) error
}

// RepositoryTargetResolver defines the interface for resolving concrete RepositoryTarget instances.
type RepositoryTargetResolver interface {
	ResolveTarget(ctx context.Context, orgID, resourceID uuid.UUID, target *domain.StorageTarget) (restic.RepositoryTarget, error)
}

// ResticCommandRunner defines the safe subprocess runner interface for repository init and probe.
type ResticCommandRunner interface {
	Init(ctx context.Context, target restic.RepositoryTarget, password []byte) error
	Probe(ctx context.Context, target restic.RepositoryTarget, password []byte) error
}

// RepositoryService coordinates per-resource Restic repository provisioning, binding, and verification (ADR-033, ADR-034).
type RepositoryService struct {
	backupRepo        repository.BackupRepository
	storageTargetRepo repository.StorageTargetRepository
	resourceFinder    ResourceFinder
	vault             SystemCredentialVault
	targetResolver    RepositoryTargetResolver
	runner            ResticCommandRunner
	logger            *slog.Logger
}

// NewRepositoryService constructs a new RepositoryService.
func NewRepositoryService(
	backupRepo repository.BackupRepository,
	storageTargetRepo repository.StorageTargetRepository,
	resourceFinder ResourceFinder,
	vault SystemCredentialVault,
	targetResolver RepositoryTargetResolver,
	runner ResticCommandRunner,
	logger *slog.Logger,
) *RepositoryService {
	if logger == nil {
		logger = slog.Default()
	}
	return &RepositoryService{
		backupRepo:        backupRepo,
		storageTargetRepo: storageTargetRepo,
		resourceFinder:    resourceFinder,
		vault:             vault,
		targetResolver:    targetResolver,
		runner:            runner,
		logger:            logger,
	}
}

// EnsureRepository provisions or verifies a dedicated Restic repository for a resource (ADR-033).
// If the repository already exists:
//   - Verifies storage target identity (immutability: cannot change target without migration)
//   - Verifies connectivity and key validity via probe ("restic cat config")
//
// If the repository does not exist:
//   - Validates resource and storage target tenant integrity
//   - Generates a fresh 32-byte cryptographically secure password
//   - Saves the password as a system-managed restic_repository_key in Vault
//   - Runs "restic init" against the target
//   - Persists the canonical BackupRepository record in PostgreSQL
func (s *RepositoryService) EnsureRepository(
	ctx context.Context,
	orgID uuid.UUID,
	resourceID uuid.UUID,
	storageTargetID uuid.UUID,
) (*domain.BackupRepository, error) {
	if orgID == uuid.Nil || resourceID == uuid.Nil || storageTargetID == uuid.Nil {
		return nil, domain.ErrInvalidRepositoryBinding
	}

	// 1. Verify resource exists and is active
	res, err := s.resourceFinder.GetByID(ctx, orgID, resourceID)
	if err != nil {
		if errors.Is(err, resDomain.ErrResourceNotFound) {
			return nil, domain.ErrResourceNotFound
		}
		return nil, fmt.Errorf("failed looking up resource: %w", err)
	}
	if res.Status == resDomain.StatusArchived {
		return nil, domain.ErrResourceArchived
	}
	if res.Status == resDomain.StatusDisabled {
		return nil, domain.ErrResourceDisabled
	}

	// 2. Verify storage target exists and is active
	storageTarget, err := s.storageTargetRepo.GetStorageTargetByID(ctx, orgID, storageTargetID)
	if err != nil {
		if errors.Is(err, domain.ErrStorageTargetNotFound) {
			return nil, domain.ErrStorageTargetNotFound
		}
		return nil, fmt.Errorf("failed looking up storage target: %w", err)
	}
	if storageTarget.Status != domain.StorageTargetStatusActive {
		return nil, domain.ErrStorageTargetNotActive
	}
	if storageTarget.Type != domain.StorageTargetTypeLocal &&
		storageTarget.Type != domain.StorageTargetTypeS3 &&
		storageTarget.Type != domain.StorageTargetTypeS3Compatible {
		return nil, domain.ErrStorageTargetNotSupported
	}

	// 3. Check if repository already exists
	existingRepo, err := s.backupRepo.GetRepositoryByResourceID(ctx, orgID, resourceID)
	if err == nil && existingRepo != nil {
		// Existing repository: verify immutability and probe health
		if existingRepo.StorageTargetID != storageTargetID {
			return nil, domain.ErrRepositoryTargetMismatch
		}

		credType, password, err := s.vault.LoadCredentialForUse(ctx, orgID, existingRepo.CredentialID)
		if err != nil {
			return nil, fmt.Errorf("failed loading repository credential: %w", err)
		}
		defer secretcrypto.ZeroBytes(password)

		if credType != credDomain.TypeResticRepositoryKey {
			return nil, fmt.Errorf("corrupted repository credential type: %s", credType)
		}

		target, err := s.targetResolver.ResolveTarget(ctx, orgID, resourceID, storageTarget)
		if err != nil {
			return nil, err
		}
		defer target.Cleanup()

		// Defense in depth: compare resolved target Locator() with persisted repository_locator
		if target.Locator() != existingRepo.RepositoryLocator {
			return nil, fmt.Errorf("%w: repository locator drifted from '%s' to '%s'", domain.ErrRepositoryTargetMismatch, existingRepo.RepositoryLocator, target.Locator())
		}

		if err := s.runner.Probe(ctx, target, password); err != nil {
			return nil, fmt.Errorf("%w: %v", domain.ErrRepositoryCorrupted, err)
		}

		return existingRepo, nil
	} else if err != nil && !errors.Is(err, domain.ErrRepositoryNotFound) {
		return nil, fmt.Errorf("failed checking existing repository: %w", err)
	}

	// 4. Provision fresh repository
	rawKey := make([]byte, 32)
	if _, err := rand.Read(rawKey); err != nil {
		return nil, fmt.Errorf("failed generating secure repository password: %w", err)
	}
	defer secretcrypto.ZeroBytes(rawKey)

	repoKey := []byte(hex.EncodeToString(rawKey))
	defer secretcrypto.ZeroBytes(repoKey)

	credName := fmt.Sprintf("restic-repo-key-%s", resourceID)
	credMeta, err := s.vault.CreateSystemCredential(ctx, orgID, credName, credDomain.TypeResticRepositoryKey, repoKey)
	if err != nil {
		return nil, fmt.Errorf("failed storing system repository credential: %w", err)
	}

	target, err := s.targetResolver.ResolveTarget(ctx, orgID, resourceID, storageTarget)
	if err != nil {
		_ = s.vault.DeleteSystemCredential(ctx, orgID, credMeta.ID)
		return nil, err
	}
	defer target.Cleanup()

	// Initialize repository via Restic subprocess
	if err := s.runner.Init(ctx, target, repoKey); err != nil {
		_ = s.vault.DeleteSystemCredential(ctx, orgID, credMeta.ID)
		return nil, fmt.Errorf("restic repository initialization failed: %w", err)
	}

	// Persist canonical repository record
	newRepo := &domain.BackupRepository{
		ID:                uuid.New(),
		OrganizationID:    orgID,
		ResourceID:        resourceID,
		StorageTargetID:   storageTargetID,
		CredentialID:      credMeta.ID,
		RepositoryLocator: target.Locator(),
		Status:            domain.BackupRepositoryStatusActive,
		Metadata:          []byte("{}"),
	}

	saved, err := s.backupRepo.CreateRepository(ctx, newRepo)
	if err != nil {
		if errors.Is(err, domain.ErrRepositoryAlreadyExists) {
			// Concurrent initialization detected: clean up orphaned system credential and resolve existing
			_ = s.vault.DeleteSystemCredential(ctx, orgID, credMeta.ID)
			return s.EnsureRepository(ctx, orgID, resourceID, storageTargetID)
		}
		s.logger.Error("repository initialized physically but database record failed",
			"org_id", orgID, "resource_id", resourceID, "error", err)
		return nil, fmt.Errorf("%w: %v", domain.ErrRepositoryReconciliationRequired, err)
	}

	return saved, nil
}

// GetRepository retrieves an existing dedicated repository for a resource.
func (s *RepositoryService) GetRepository(ctx context.Context, orgID, resourceID uuid.UUID) (*domain.BackupRepository, error) {
	if orgID == uuid.Nil || resourceID == uuid.Nil {
		return nil, domain.ErrRepositoryNotFound
	}
	return s.backupRepo.GetRepositoryByResourceID(ctx, orgID, resourceID)
}
