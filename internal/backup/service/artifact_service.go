package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"

	auditDomain "backup-platform/internal/audit/domain"
	auditService "backup-platform/internal/audit/service"
	"backup-platform/internal/backup/domain"
	"backup-platform/internal/backup/repository"
	orgDomain "backup-platform/internal/organization/domain"
	"backup-platform/internal/storage"
	"backup-platform/pkg/uuid"
)

// ArtifactService coordinates artifact queries, authorized streaming downloads, and physical deletions.
type ArtifactService struct {
	repo          repository.BackupRepository
	storage       storage.StorageProvider
	auditRecorder auditService.AuditRecorder
	logger        *slog.Logger
}

// NewArtifactService constructs a new ArtifactService.
func NewArtifactService(
	repo repository.BackupRepository,
	storageProvider storage.StorageProvider,
	auditRecorder auditService.AuditRecorder,
	logger *slog.Logger,
) *ArtifactService {
	if logger == nil {
		logger = slog.Default()
	}
	return &ArtifactService{
		repo:          repo,
		storage:       storageProvider,
		auditRecorder: auditRecorder,
		logger:        logger,
	}
}

// ListArtifacts returns all active (non-deleted) artifacts for an organization.
func (s *ArtifactService) ListArtifacts(
	ctx context.Context,
	role orgDomain.Role,
	orgID uuid.UUID,
) ([]*domain.BackupArtifact, error) {
	if orgID == uuid.Nil {
		return nil, domain.ErrBackupServiceUnavailable
	}

	artifacts, err := s.repo.ListArtifacts(ctx, orgID)
	if err != nil {
		s.logger.Error("failed listing backup artifacts", slog.String("org_id", orgID.String()))
		return nil, domain.ErrBackupServiceUnavailable
	}

	return artifacts, nil
}

// GetArtifact retrieves a single active artifact metadata record scoped by organization.
func (s *ArtifactService) GetArtifact(
	ctx context.Context,
	role orgDomain.Role,
	orgID, artifactID uuid.UUID,
) (*domain.BackupArtifact, error) {
	if orgID == uuid.Nil || artifactID == uuid.Nil {
		return nil, domain.ErrArtifactNotFound
	}

	artifact, err := s.repo.GetArtifactByID(ctx, orgID, artifactID)
	if err != nil {
		if errors.Is(err, domain.ErrArtifactNotFound) {
			return nil, domain.ErrArtifactNotFound
		}
		s.logger.Error("failed getting backup artifact", slog.String("org_id", orgID.String()), slog.String("artifact_id", artifactID.String()))
		return nil, domain.ErrBackupServiceUnavailable
	}

	if artifact.IsDeleted {
		return nil, domain.ErrArtifactNotFound
	}

	return artifact, nil
}

// OpenArtifactDownload validates permissions, ensures the artifact is not deleted, and opens the physical storage stream.
func (s *ArtifactService) OpenArtifactDownload(
	ctx context.Context,
	role orgDomain.Role,
	orgID, artifactID uuid.UUID,
) (*domain.BackupArtifact, io.ReadCloser, error) {
	// 1. RBAC check: only admin and member can download artifacts
	if role != orgDomain.RoleAdmin && role != orgDomain.RoleMember {
		return nil, nil, domain.ErrUnauthorizedRole
	}

	if orgID == uuid.Nil || artifactID == uuid.Nil {
		return nil, nil, domain.ErrArtifactNotFound
	}

	// 2. Fetch artifact metadata scoped by organization
	artifact, err := s.repo.GetArtifactByID(ctx, orgID, artifactID)
	if err != nil {
		if errors.Is(err, domain.ErrArtifactNotFound) {
			return nil, nil, domain.ErrArtifactNotFound
		}
		s.logger.Error("failed querying artifact for download", slog.String("org_id", orgID.String()), slog.String("artifact_id", artifactID.String()))
		return nil, nil, domain.ErrBackupServiceUnavailable
	}

	// 3. Reject tombstoned / deleted artifact without attempting physical storage open
	if artifact.IsDeleted {
		return nil, nil, domain.ErrArtifactNotFound
	}

	// 4. Open artifact stream from StorageProvider
	reader, err := s.storage.OpenArtifact(ctx, artifact.StorageReference)
	if err != nil {
		s.logger.Error("failed opening physical artifact for download")
		if errors.Is(err, storage.ErrArtifactNotFound) {
			return nil, nil, domain.ErrArtifactNotFound
		}
		return nil, nil, domain.ErrBackupServiceUnavailable
	}

	return artifact, reader, nil
}

// RecordDownloadAudit logs a successful download event.
func (s *ArtifactService) RecordDownloadAudit(
	ctx context.Context,
	orgID, userID, artifactID uuid.UUID,
	sizeBytes int64,
	clientIP, userAgent string,
) {
	if s.auditRecorder == nil {
		return
	}

	metaObj := map[string]any{
		"artifact_id": artifactID.String(),
		"size_bytes":  sizeBytes,
	}
	metaBytes, _ := json.Marshal(metaObj)

	var ipPtr, uaPtr *string
	if clientIP != "" {
		ipPtr = &clientIP
	}
	if userAgent != "" {
		uaPtr = &userAgent
	}

	entry := &auditDomain.AuditLog{
		ID:             uuid.New(),
		OrganizationID: &orgID,
		UserID:         &userID,
		Action:         auditDomain.ActionBackupDownload,
		EntityType:     auditDomain.EntityTypeBackupArtifact,
		EntityID:       &artifactID,
		IPAddress:      ipPtr,
		UserAgent:      uaPtr,
		Metadata:       metaBytes,
	}

	_ = s.auditRecorder.Record(ctx, entry)
}

// DeleteArtifact performs physical deletion first, then tombstones metadata, and records audit event upon success.
func (s *ArtifactService) DeleteArtifact(
	ctx context.Context,
	role orgDomain.Role,
	orgID, userID, artifactID uuid.UUID,
	clientIP, userAgent string,
) error {
	// 1. RBAC check: only admin can delete artifacts
	if role != orgDomain.RoleAdmin {
		return domain.ErrUnauthorizedRole
	}

	if orgID == uuid.Nil || artifactID == uuid.Nil {
		return domain.ErrArtifactNotFound
	}

	// 2. Fetch artifact metadata scoped by organization
	artifact, err := s.repo.GetArtifactByID(ctx, orgID, artifactID)
	if err != nil {
		if errors.Is(err, domain.ErrArtifactNotFound) {
			return domain.ErrArtifactNotFound
		}
		s.logger.Error("failed querying artifact for deletion", slog.String("org_id", orgID.String()), slog.String("artifact_id", artifactID.String()))
		return domain.ErrBackupServiceUnavailable
	}

	// 3. If already tombstoned, return ErrArtifactNotFound without deleting physical file again
	if artifact.IsDeleted {
		return domain.ErrArtifactNotFound
	}

	// 4. Physical Deletion First: delete bytes from storage provider
	if err := s.storage.DeleteArtifact(ctx, artifact.StorageReference); err != nil {
		s.logger.Error("physical artifact deletion failed")
		// Do NOT tombstone database record if physical delete failed!
		return domain.ErrArtifactDeleteFailed
	}

	// 5. Database Tombstone: mark is_deleted = true, deleted_at = NOW()
	if err := s.repo.TombstoneArtifact(ctx, orgID, artifactID); err != nil {
		s.logger.Error("failed recording database tombstone for artifact", slog.String("org_id", orgID.String()), slog.String("artifact_id", artifactID.String()))
		return domain.ErrArtifactDeleteFailed
	}

	// 6. Record Audit Event: backup.delete
	if s.auditRecorder != nil {
		metaObj := map[string]any{
			"artifact_id": artifactID.String(),
			"target_name": artifact.TargetName,
			"size_bytes":  artifact.SizeBytes,
		}
		metaBytes, _ := json.Marshal(metaObj)

		var ipPtr, uaPtr *string
		if clientIP != "" {
			ipPtr = &clientIP
		}
		if userAgent != "" {
			uaPtr = &userAgent
		}

		entry := &auditDomain.AuditLog{
			ID:             uuid.New(),
			OrganizationID: &orgID,
			UserID:         &userID,
			Action:         auditDomain.ActionBackupDelete,
			EntityType:     auditDomain.EntityTypeBackupArtifact,
			EntityID:       &artifactID,
			IPAddress:      ipPtr,
			UserAgent:      uaPtr,
			Metadata:       metaBytes,
		}
		_ = s.auditRecorder.Record(ctx, entry)
	}

	return nil
}
