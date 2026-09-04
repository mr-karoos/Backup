package restic

import (
	"context"
	"errors"
	"fmt"

	"backup-platform/internal/backup/domain"
	credDomain "backup-platform/internal/credential/domain"
	"backup-platform/internal/credential/payload"
	"backup-platform/internal/credential/secretcrypto"
	"backup-platform/pkg/uuid"
)

// StorageTargetGetter provides storage target lookup.
type StorageTargetGetter interface {
	GetStorageTargetByID(ctx context.Context, orgID, targetID uuid.UUID) (*domain.StorageTarget, error)
}

// CredentialLoader provides secret decryption for target credentials.
type CredentialLoader interface {
	LoadCredentialForUse(ctx context.Context, orgID, credID uuid.UUID) (credDomain.Type, []byte, error)
}

// TargetResolver resolves concrete RepositoryTarget instances from target configurations.
type TargetResolver struct {
	targetGetter     StorageTargetGetter
	credLoader       CredentialLoader
	storageRoot      string
	allowInsecure    bool
	privateAllowlist []string
}

// NewTargetResolver constructs a new TargetResolver.
func NewTargetResolver(
	targetGetter StorageTargetGetter,
	credLoader CredentialLoader,
	storageRoot string,
	allowInsecure bool,
	privateAllowlist []string,
) *TargetResolver {
	return &TargetResolver{
		targetGetter:     targetGetter,
		credLoader:       credLoader,
		storageRoot:      storageRoot,
		allowInsecure:    allowInsecure,
		privateAllowlist: privateAllowlist,
	}
}

// ResolveTarget constructs a RepositoryTarget for a resource and storage target.
func (r *TargetResolver) ResolveTarget(
	ctx context.Context,
	orgID uuid.UUID,
	resourceID uuid.UUID,
	target *domain.StorageTarget,
) (RepositoryTarget, error) {
	if target == nil {
		return nil, domain.ErrStorageTargetNotFound
	}
	if target.OrganizationID != orgID {
		return nil, domain.ErrStorageTargetNotFound
	}
	if target.Status != domain.StorageTargetStatusActive {
		return nil, domain.ErrStorageTargetNotActive
	}

	switch target.Type {
	case domain.StorageTargetTypeLocal:
		return NewLocalRepositoryTarget(r.storageRoot, orgID, resourceID)

	case domain.StorageTargetTypeS3, domain.StorageTargetTypeS3Compatible:
		if target.CredentialID == nil || *target.CredentialID == uuid.Nil {
			return nil, domain.ErrStorageTargetCredentialRequired
		}

		s3Cfg, err := domain.ParseS3TargetConfig(target.Config)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", domain.ErrInvalidStorageTargetConfig, err)
		}

		credType, decryptedSecret, err := r.credLoader.LoadCredentialForUse(ctx, orgID, *target.CredentialID)
		if err != nil {
			return nil, fmt.Errorf("failed loading storage target credentials: %w", err)
		}
		defer secretcrypto.ZeroBytes(decryptedSecret)

		if credType != credDomain.TypeS3Credentials {
			return nil, fmt.Errorf("unexpected credential type %s for s3 target", credType)
		}

		s3Payload, err := payload.DecodeS3(decryptedSecret)
		if err != nil {
			return nil, fmt.Errorf("failed decoding s3 credentials payload: %w", err)
		}
		defer payload.ClearS3(s3Payload)

		repoTarget, err := NewS3RepositoryTarget(
			string(target.Type),
			*s3Cfg,
			orgID,
			resourceID,
			s3Payload.AccessKeyID,
			s3Payload.SecretAccessKey,
			s3Payload.SessionToken,
			r.allowInsecure,
			r.privateAllowlist,
		)
		if err != nil {
			return nil, fmt.Errorf("failed creating s3 repository target: %w", err)
		}
		return repoTarget, nil

	default:
		return nil, domain.ErrIncompatibleEngineStorage
	}
}

// ResolveTargetForRepository resolves a concrete RepositoryTarget directly from a persisted BackupRepository record.
func (r *TargetResolver) ResolveTargetForRepository(
	ctx context.Context,
	repo *domain.BackupRepository,
) (RepositoryTarget, error) {
	if repo == nil {
		return nil, errors.New("backup repository cannot be nil")
	}

	target, err := r.targetGetter.GetStorageTargetByID(ctx, repo.OrganizationID, repo.StorageTargetID)
	if err != nil {
		return nil, err
	}

	resolvedTarget, err := r.ResolveTarget(ctx, repo.OrganizationID, repo.ResourceID, target)
	if err != nil {
		return nil, err
	}

	// Defense in depth: compare resolved target Locator() with persisted repository_locator
	if resolvedTarget.Locator() != repo.RepositoryLocator {
		resolvedTarget.Cleanup()
		return nil, fmt.Errorf("%w: repository locator drifted from '%s' to '%s'", domain.ErrRepositoryTargetMismatch, repo.RepositoryLocator, resolvedTarget.Locator())
	}

	return resolvedTarget, nil
}
