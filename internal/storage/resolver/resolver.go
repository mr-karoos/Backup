package resolver

import (
	"context"
	"errors"
	"fmt"

	"backup-platform/internal/backup/domain"
	credDomain "backup-platform/internal/credential/domain"
	"backup-platform/internal/credential/payload"
	"backup-platform/internal/credential/secretcrypto"
	"backup-platform/internal/storage"
	"backup-platform/internal/storage/s3"
	"backup-platform/pkg/uuid"
)

// StorageTargetGetter retrieves a storage target by organization ID and target ID.
type StorageTargetGetter interface {
	GetStorageTargetByID(ctx context.Context, orgID, targetID uuid.UUID) (*domain.StorageTarget, error)
}

// CredentialVaultLoader loads decrypted credentials for use.
type CredentialVaultLoader interface {
	LoadCredentialForUse(ctx context.Context, orgID, credID uuid.UUID) (credDomain.Type, []byte, error)
}

// StorageResolver resolves the concrete storage.StorageProvider bound to a given StorageTargetID.
type StorageResolver struct {
	localStorage     storage.StorageProvider
	targetRepo       StorageTargetGetter
	vault            CredentialVaultLoader
	allowInsecure    bool
	privateAllowlist []string
}

// NewStorageResolver constructs a new StorageResolver instance.
func NewStorageResolver(
	localStorage storage.StorageProvider,
	targetRepo StorageTargetGetter,
	vault CredentialVaultLoader,
	allowInsecure bool,
	privateAllowlist []string,
) *StorageResolver {
	return &StorageResolver{
		localStorage:     localStorage,
		targetRepo:       targetRepo,
		vault:            vault,
		allowInsecure:    allowInsecure,
		privateAllowlist: privateAllowlist,
	}
}

// Resolve returns the concrete StorageProvider for the requested storageTargetID in the organization.
func (r *StorageResolver) Resolve(ctx context.Context, orgID, targetID uuid.UUID) (storage.StorageProvider, error) {
	if orgID == uuid.Nil || targetID == uuid.Nil {
		return nil, domain.ErrStorageTargetNotFound
	}

	target, err := r.targetRepo.GetStorageTargetByID(ctx, orgID, targetID)
	if err != nil {
		return nil, err
	}

	switch target.Type {
	case domain.StorageTargetTypeLocal:
		return r.localStorage, nil

	case domain.StorageTargetTypeS3, domain.StorageTargetTypeS3Compatible:
		s3Cfg, err := domain.ParseS3TargetConfig(target.Config)
		if err != nil {
			return nil, fmt.Errorf("invalid s3 target config: %w", err)
		}

		if target.CredentialID == nil || *target.CredentialID == uuid.Nil {
			return nil, domain.ErrStorageTargetCredentialRequired
		}

		cType, plaintextBytes, err := r.vault.LoadCredentialForUse(ctx, orgID, *target.CredentialID)
		if err != nil {
			return nil, fmt.Errorf("failed loading s3 credential: %w", err)
		}
		defer secretcrypto.ZeroBytes(plaintextBytes)

		if cType != credDomain.TypeS3Credentials {
			return nil, errors.New("invalid credential type for s3 storage target")
		}

		s3Payload, err := payload.DecodeS3(plaintextBytes)
		if err != nil {
			return nil, fmt.Errorf("failed decoding s3 credential payload: %w", err)
		}
		defer payload.ClearS3(s3Payload)

		provider, err := s3.NewS3StorageProvider(s3.S3ProviderConfig{
			Bucket:           s3Cfg.Bucket,
			Region:           s3Cfg.Region,
			Endpoint:         s3Cfg.Endpoint,
			ForcePathStyle:   s3Cfg.ForcePathStyle,
			Prefix:           s3Cfg.Prefix,
			AccessKeyID:      s3Payload.AccessKeyID,
			SecretAccessKey:  s3Payload.SecretAccessKey,
			SessionToken:     s3Payload.SessionToken,
			AllowInsecure:    r.allowInsecure,
			PrivateAllowlist: r.privateAllowlist,
		})
		if err != nil {
			return nil, fmt.Errorf("failed initializing s3 storage provider: %w", err)
		}

		return provider, nil

	default:
		return nil, domain.ErrStorageTargetNotSupported
	}
}
