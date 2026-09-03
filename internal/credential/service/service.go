package service

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"backup-platform/internal/credential/domain"
	"backup-platform/internal/credential/repository"
	"backup-platform/internal/credential/secretcrypto"
	"backup-platform/internal/platform/database"
	"backup-platform/internal/platform/logger"
	"backup-platform/pkg/uuid"
)

// VaultService handles tenant-scoped encrypted credential storage and secret retrieval.
type VaultService struct {
	cryptoEngine secretcrypto.Engine
	repo         repository.CredentialRepository
	txManager    database.TxManager
	logger       *slog.Logger
}

// NewVaultService constructs a new VaultService instance.
func NewVaultService(
	cryptoEngine secretcrypto.Engine,
	repo repository.CredentialRepository,
	txManager database.TxManager,
	log *slog.Logger,
) *VaultService {
	if log == nil {
		log = slog.Default()
	}
	return &VaultService{
		cryptoEngine: cryptoEngine,
		repo:         repo,
		txManager:    txManager,
		logger:       log,
	}
}

// CreateCredential encrypts the plaintext payload using AES-256-GCM bound to the organization and credential ID,
// persists the record to the database, and returns safe metadata without secret material.
// Only user-managed credential types are accepted through this method.
func (s *VaultService) CreateCredential(
	ctx context.Context,
	orgID uuid.UUID,
	name string,
	credType domain.Type,
	plaintextPayload []byte,
	fingerprint *string,
) (*domain.CredentialMetadata, error) {
	if orgID == uuid.Nil {
		return nil, domain.ErrInvalidOrganizationID
	}

	validName, err := domain.ValidateName(name)
	if err != nil {
		return nil, err
	}

	if err := domain.ValidateType(credType); err != nil {
		return nil, err
	}

	if !credType.IsUserManaged() {
		return nil, domain.ErrSystemCredentialRestricted
	}

	if len(plaintextPayload) == 0 {
		return nil, domain.ErrEmptyPlaintextSecret
	}

	// 1. Generate Credential UUID before encryption (required for AAD context binding)
	credID := uuid.New()

	// 2. Construct EncryptionContext binding ciphertext to organization and credential
	encCtx := secretcrypto.EncryptionContext{
		OrganizationID: orgID,
		CredentialID:   credID,
	}

	// 3. Make local defensive copy of secret payload and guarantee best-effort zeroization
	localCopy := make([]byte, len(plaintextPayload))
	copy(localCopy, plaintextPayload)
	defer secretcrypto.ZeroBytes(localCopy)

	// 4. Encrypt secret payload and zero local copy immediately after
	encrypted, err := s.cryptoEngine.Encrypt(localCopy, encCtx)
	secretcrypto.ZeroBytes(localCopy)
	if err != nil {
		logger.FromContext(ctx, s.logger).Warn("credential encryption failed")
		return nil, domain.ErrCredentialEncryptionFailed
	}

	now := time.Now().UTC()
	cred := &domain.Credential{
		ID:              credID,
		OrganizationID:  orgID,
		Name:            validName,
		Type:            credType,
		ManagedBy:       domain.ManagedByUser,
		EncryptedSecret: encrypted.Ciphertext,
		Nonce:           encrypted.Nonce,
		AuthTag:         encrypted.AuthTag,
		KeyVersion:      encrypted.KeyVersion,
		Fingerprint:     fingerprint,
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	// 5. Persist encrypted credential to repository
	if err := s.repo.Create(ctx, s.txManager.Querier(), cred); err != nil {
		logger.FromContext(ctx, s.logger).Error("credential persistence failed")
		return nil, domain.ErrCredentialServiceUnavailable
	}

	return &domain.CredentialMetadata{
		ID:             cred.ID,
		OrganizationID: cred.OrganizationID,
		Name:           cred.Name,
		Type:           cred.Type,
		ManagedBy:      cred.ManagedBy,
		Fingerprint:    cred.Fingerprint,
		KeyVersion:     cred.KeyVersion,
		CreatedAt:      cred.CreatedAt,
		UpdatedAt:      cred.UpdatedAt,
	}, nil
}

// CreateSystemCredential allows trusted platform subsystems to create system-managed credentials
// (such as Restic repository keys). Encrypted strictly using ENCRYPTION_MASTER_KEY.
func (s *VaultService) CreateSystemCredential(
	ctx context.Context,
	orgID uuid.UUID,
	name string,
	credType domain.Type,
	plaintextPayload []byte,
) (*domain.CredentialMetadata, error) {
	if orgID == uuid.Nil {
		return nil, domain.ErrInvalidOrganizationID
	}

	validName, err := domain.ValidateName(name)
	if err != nil {
		return nil, err
	}

	if err := domain.ValidateType(credType); err != nil {
		return nil, err
	}

	if len(plaintextPayload) == 0 {
		return nil, domain.ErrEmptyPlaintextSecret
	}

	credID := uuid.New()

	encCtx := secretcrypto.EncryptionContext{
		OrganizationID: orgID,
		CredentialID:   credID,
	}

	localCopy := make([]byte, len(plaintextPayload))
	copy(localCopy, plaintextPayload)
	defer secretcrypto.ZeroBytes(localCopy)

	encrypted, err := s.cryptoEngine.Encrypt(localCopy, encCtx)
	secretcrypto.ZeroBytes(localCopy)
	if err != nil {
		logger.FromContext(ctx, s.logger).Warn("system credential encryption failed")
		return nil, domain.ErrCredentialEncryptionFailed
	}

	now := time.Now().UTC()
	cred := &domain.Credential{
		ID:              credID,
		OrganizationID:  orgID,
		Name:            validName,
		Type:            credType,
		ManagedBy:       domain.ManagedBySystem,
		EncryptedSecret: encrypted.Ciphertext,
		Nonce:           encrypted.Nonce,
		AuthTag:         encrypted.AuthTag,
		KeyVersion:      encrypted.KeyVersion,
		Fingerprint:     nil,
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	if err := s.repo.Create(ctx, s.txManager.Querier(), cred); err != nil {
		logger.FromContext(ctx, s.logger).Error("system credential persistence failed")
		return nil, domain.ErrCredentialServiceUnavailable
	}

	return &domain.CredentialMetadata{
		ID:             cred.ID,
		OrganizationID: cred.OrganizationID,
		Name:           cred.Name,
		Type:           cred.Type,
		ManagedBy:      cred.ManagedBy,
		Fingerprint:    cred.Fingerprint,
		KeyVersion:     cred.KeyVersion,
		CreatedAt:      cred.CreatedAt,
		UpdatedAt:      cred.UpdatedAt,
	}, nil
}

// GetCredentialMetadata retrieves safe metadata for a single credential in an organization.
// System-managed credentials return domain.ErrCredentialNotFound to strictly hide their existence.
func (s *VaultService) GetCredentialMetadata(
	ctx context.Context,
	orgID uuid.UUID,
	credID uuid.UUID,
) (*domain.CredentialMetadata, error) {
	if orgID == uuid.Nil {
		return nil, domain.ErrInvalidOrganizationID
	}
	if credID == uuid.Nil {
		return nil, domain.ErrInvalidCredentialID
	}

	meta, err := s.repo.FindMetadataForOrganization(ctx, s.txManager.Querier(), orgID, credID)
	if err != nil {
		if errors.Is(err, domain.ErrCredentialNotFound) {
			return nil, domain.ErrCredentialNotFound
		}
		logger.FromContext(ctx, s.logger).Error("credential metadata lookup failed")
		return nil, domain.ErrCredentialServiceUnavailable
	}

	if meta.ManagedBy == domain.ManagedBySystem {
		return nil, domain.ErrCredentialNotFound
	}

	return meta, nil
}

// GetSystemCredentialMetadata allows internal subsystems to retrieve metadata for any credential (including system-managed).
func (s *VaultService) GetSystemCredentialMetadata(
	ctx context.Context,
	orgID uuid.UUID,
	credID uuid.UUID,
) (*domain.CredentialMetadata, error) {
	if orgID == uuid.Nil {
		return nil, domain.ErrInvalidOrganizationID
	}
	if credID == uuid.Nil {
		return nil, domain.ErrInvalidCredentialID
	}

	meta, err := s.repo.FindMetadataForOrganization(ctx, s.txManager.Querier(), orgID, credID)
	if err != nil {
		if errors.Is(err, domain.ErrCredentialNotFound) {
			return nil, domain.ErrCredentialNotFound
		}
		logger.FromContext(ctx, s.logger).Error("system credential metadata lookup failed")
		return nil, domain.ErrCredentialServiceUnavailable
	}

	return meta, nil
}

// ListCredentialMetadata retrieves all safe credential metadata for the given organization.
// System-managed credentials are automatically filtered out.
func (s *VaultService) ListCredentialMetadata(
	ctx context.Context,
	orgID uuid.UUID,
) ([]*domain.CredentialMetadata, error) {
	if orgID == uuid.Nil {
		return nil, domain.ErrInvalidOrganizationID
	}

	items, err := s.repo.ListMetadataForOrganization(ctx, s.txManager.Querier(), orgID)
	if err != nil {
		logger.FromContext(ctx, s.logger).Error("credential list failed")
		return nil, domain.ErrCredentialServiceUnavailable
	}

	return items, nil
}

// UpdateCredentialName updates only the name of an existing user-managed credential.
// Mutating system-managed credentials returns domain.ErrSystemCredentialRestricted.
func (s *VaultService) UpdateCredentialName(
	ctx context.Context,
	orgID uuid.UUID,
	credID uuid.UUID,
	name string,
) (*domain.CredentialMetadata, error) {
	if orgID == uuid.Nil {
		return nil, domain.ErrInvalidOrganizationID
	}
	if credID == uuid.Nil {
		return nil, domain.ErrInvalidCredentialID
	}

	validName, err := domain.ValidateName(name)
	if err != nil {
		return nil, err
	}

	current, err := s.repo.FindMetadataForOrganization(ctx, s.txManager.Querier(), orgID, credID)
	if err != nil {
		if errors.Is(err, domain.ErrCredentialNotFound) {
			return nil, domain.ErrCredentialNotFound
		}
		logger.FromContext(ctx, s.logger).Error("credential lookup failed for name update")
		return nil, domain.ErrCredentialServiceUnavailable
	}

	if current.ManagedBy == domain.ManagedBySystem {
		return nil, domain.ErrSystemCredentialRestricted
	}

	meta, err := s.repo.UpdateNameForOrganization(ctx, s.txManager.Querier(), orgID, credID, validName)
	if err != nil {
		if errors.Is(err, domain.ErrCredentialNotFound) {
			return nil, domain.ErrCredentialNotFound
		}
		logger.FromContext(ctx, s.logger).Error("credential name update failed")
		return nil, domain.ErrCredentialServiceUnavailable
	}

	return meta, nil
}

// ReplaceCredentialSecret re-encrypts and replaces the secret payload of an existing user credential.
// Mutating system-managed credentials returns domain.ErrSystemCredentialRestricted.
func (s *VaultService) ReplaceCredentialSecret(
	ctx context.Context,
	orgID uuid.UUID,
	credID uuid.UUID,
	name *string,
	plaintextPayload []byte,
	fingerprint *string,
) (*domain.CredentialMetadata, error) {
	if orgID == uuid.Nil {
		return nil, domain.ErrInvalidOrganizationID
	}
	if credID == uuid.Nil {
		return nil, domain.ErrInvalidCredentialID
	}
	if len(plaintextPayload) == 0 {
		return nil, domain.ErrEmptyPlaintextSecret
	}

	// 1. Fetch current metadata to verify existence, managed_by, type, and name
	current, err := s.repo.FindMetadataForOrganization(ctx, s.txManager.Querier(), orgID, credID)
	if err != nil {
		if errors.Is(err, domain.ErrCredentialNotFound) {
			return nil, domain.ErrCredentialNotFound
		}
		logger.FromContext(ctx, s.logger).Error("credential lookup failed for secret replacement")
		return nil, domain.ErrCredentialServiceUnavailable
	}

	if current.ManagedBy == domain.ManagedBySystem {
		return nil, domain.ErrSystemCredentialRestricted
	}

	targetName := current.Name
	if name != nil {
		validName, err := domain.ValidateName(*name)
		if err != nil {
			return nil, err
		}
		targetName = validName
	}

	// 2. Construct EncryptionContext binding to existing Organization ID and Credential ID
	encCtx := secretcrypto.EncryptionContext{
		OrganizationID: orgID,
		CredentialID:   credID,
	}

	// 3. Make local defensive copy and encrypt
	localCopy := make([]byte, len(plaintextPayload))
	copy(localCopy, plaintextPayload)
	defer secretcrypto.ZeroBytes(localCopy)

	encrypted, err := s.cryptoEngine.Encrypt(localCopy, encCtx)
	secretcrypto.ZeroBytes(localCopy)
	if err != nil {
		logger.FromContext(ctx, s.logger).Warn("credential secret re-encryption failed")
		return nil, domain.ErrCredentialEncryptionFailed
	}

	now := time.Now().UTC()
	cred := &domain.Credential{
		ID:              credID,
		OrganizationID:  orgID,
		Name:            targetName,
		Type:            current.Type,
		ManagedBy:       current.ManagedBy,
		EncryptedSecret: encrypted.Ciphertext,
		Nonce:           encrypted.Nonce,
		AuthTag:         encrypted.AuthTag,
		KeyVersion:      encrypted.KeyVersion,
		Fingerprint:     fingerprint,
		UpdatedAt:       now,
	}

	meta, err := s.repo.UpdateEncryptedForOrganization(ctx, s.txManager.Querier(), cred)
	if err != nil {
		if errors.Is(err, domain.ErrCredentialNotFound) {
			return nil, domain.ErrCredentialNotFound
		}
		logger.FromContext(ctx, s.logger).Error("credential secret update persistence failed")
		return nil, domain.ErrCredentialServiceUnavailable
	}

	return meta, nil
}

// DeleteCredential permanently deletes an unreferenced user credential from an organization.
// Deleting system-managed credentials returns domain.ErrSystemCredentialRestricted.
func (s *VaultService) DeleteCredential(
	ctx context.Context,
	orgID uuid.UUID,
	credID uuid.UUID,
) error {
	if orgID == uuid.Nil {
		return domain.ErrInvalidOrganizationID
	}
	if credID == uuid.Nil {
		return domain.ErrInvalidCredentialID
	}

	err := s.repo.DeleteForOrganization(ctx, s.txManager.Querier(), orgID, credID)
	if err != nil {
		if errors.Is(err, domain.ErrCredentialNotFound) {
			return domain.ErrCredentialNotFound
		}
		if errors.Is(err, domain.ErrSystemCredentialRestricted) {
			return domain.ErrSystemCredentialRestricted
		}
		if errors.Is(err, domain.ErrCredentialInUse) {
			return domain.ErrCredentialInUse
		}
		logger.FromContext(ctx, s.logger).Error("credential deletion failed")
		return domain.ErrCredentialServiceUnavailable
	}

	return nil
}

// DeleteSystemCredential allows internal platform subsystems to clean up system-managed credentials
// (e.g. upon repository provisioning failure atomicity rollback).
func (s *VaultService) DeleteSystemCredential(
	ctx context.Context,
	orgID uuid.UUID,
	credID uuid.UUID,
) error {
	if orgID == uuid.Nil {
		return domain.ErrInvalidOrganizationID
	}
	if credID == uuid.Nil {
		return domain.ErrInvalidCredentialID
	}

	return s.repo.DeleteForOrganization(ctx, s.txManager.Querier(), orgID, credID)
}

// LoadCredentialForUse retrieves and decrypts the credential for operational connector use.
// It returns the Credential Type and decrypted payload bytes in a single tenant-scoped encrypted lookup.
//
// Caller responsibility: The caller receives ownership of the decrypted []byte slice and is
// responsible for zeroing it with secretcrypto.ZeroBytes(payloadBytes) once it is no longer needed.
func (s *VaultService) LoadCredentialForUse(
	ctx context.Context,
	orgID uuid.UUID,
	credID uuid.UUID,
) (domain.Type, []byte, error) {
	if orgID == uuid.Nil {
		return "", nil, domain.ErrInvalidOrganizationID
	}
	if credID == uuid.Nil {
		return "", nil, domain.ErrInvalidCredentialID
	}

	cred, err := s.repo.FindEncryptedByIDForOrganization(ctx, s.txManager.Querier(), orgID, credID)
	if err != nil {
		if errors.Is(err, domain.ErrCredentialNotFound) {
			return "", nil, domain.ErrCredentialNotFound
		}
		logger.FromContext(ctx, s.logger).Error("credential lookup failed")
		return "", nil, domain.ErrCredentialServiceUnavailable
	}

	encSecret := secretcrypto.EncryptedSecret{
		Ciphertext: cred.EncryptedSecret,
		Nonce:      cred.Nonce,
		AuthTag:    cred.AuthTag,
		KeyVersion: cred.KeyVersion,
	}

	encCtx := secretcrypto.EncryptionContext{
		OrganizationID: orgID,
		CredentialID:   credID,
	}

	plaintext, err := s.cryptoEngine.Decrypt(encSecret, encCtx)
	if err != nil {
		logger.FromContext(ctx, s.logger).Warn("credential decryption failed")
		return "", nil, domain.ErrCredentialSecretUnavailable
	}

	return cred.Type, plaintext, nil
}

// LoadSecretForUse retrieves and decrypts the credential secret for internal worker/connector use.
//
// Caller responsibility: The caller receives ownership of the decrypted []byte slice and is
// responsible for zeroing it with secretcrypto.ZeroBytes(secret) once it is no longer needed.
func (s *VaultService) LoadSecretForUse(
	ctx context.Context,
	orgID uuid.UUID,
	credID uuid.UUID,
) ([]byte, error) {
	_, plaintext, err := s.LoadCredentialForUse(ctx, orgID, credID)
	return plaintext, err
}
