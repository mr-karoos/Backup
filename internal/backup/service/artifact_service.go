package service

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"

	"backup-platform/internal/artifactcrypto"
	auditDomain "backup-platform/internal/audit/domain"
	auditService "backup-platform/internal/audit/service"
	"backup-platform/internal/backup/domain"
	"backup-platform/internal/backup/repository"
	"backup-platform/internal/backup/restic"
	credDomain "backup-platform/internal/credential/domain"
	"backup-platform/internal/credential/secretcrypto"
	orgDomain "backup-platform/internal/organization/domain"
	"backup-platform/internal/storage"
	"backup-platform/pkg/uuid"
)

// DownloadDescriptor encapsulates the streaming payload, content metadata, and lifecycle cleanup for an artifact download.
type DownloadDescriptor struct {
	Artifact              *domain.BackupArtifact
	Reader                io.ReadCloser
	Filename              string
	ContentType           string
	OptionalContentLength *int64
}

// ArtifactService coordinates artifact queries, authorized streaming downloads, and physical deletions.
type ArtifactService struct {
	repo            repository.BackupRepository
	storage         storage.StorageProvider
	storageResolver storage.StorageProviderResolver
	keyProvider     artifactcrypto.KeyProvider
	auditRecorder   auditService.AuditRecorder
	logger          *slog.Logger

	// Restic dependencies
	resticRunner   restic.CommandRunner
	coordinator    restic.RepositoryOperationCoordinator
	vault          SystemCredentialVault
	targetResolver RepositoryTargetResolver
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

// SetResticDependencies injects the components necessary for streaming downloads of Restic repository snapshots.
func (s *ArtifactService) SetResticDependencies(
	runner restic.CommandRunner,
	coordinator restic.RepositoryOperationCoordinator,
	vault SystemCredentialVault,
	targetResolver RepositoryTargetResolver,
) {
	s.resticRunner = runner
	s.coordinator = coordinator
	s.vault = vault
	s.targetResolver = targetResolver
}

// SetKeyProvider sets or updates the artifact key provider for decrypting downloads.
func (s *ArtifactService) SetKeyProvider(keyProvider artifactcrypto.KeyProvider) {
	s.keyProvider = keyProvider
}

// SetStorageResolver configures a dynamic storage provider resolver.
func (s *ArtifactService) SetStorageResolver(resolver storage.StorageProviderResolver) {
	s.storageResolver = resolver
}

func (s *ArtifactService) resolveStorageProvider(ctx context.Context, orgID, targetID uuid.UUID) (storage.StorageProvider, error) {
	if s.storageResolver != nil && targetID != uuid.Nil {
		return s.storageResolver.Resolve(ctx, orgID, targetID)
	}
	if s.storage != nil {
		return s.storage, nil
	}
	return nil, errors.New("no storage provider configured")
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

// OpenArtifactDownload validates permissions, ensures the artifact is not deleted, and opens the physical storage stream or Restic dump stream.
func (s *ArtifactService) OpenArtifactDownload(
	ctx context.Context,
	role orgDomain.Role,
	orgID, artifactID uuid.UUID,
) (*DownloadDescriptor, error) {
	// 1. RBAC check: only admin and member can download artifacts
	if role != orgDomain.RoleAdmin && role != orgDomain.RoleMember {
		return nil, domain.ErrUnauthorizedRole
	}

	if orgID == uuid.Nil || artifactID == uuid.Nil {
		return nil, domain.ErrArtifactNotFound
	}

	// 2. Fetch artifact metadata scoped by organization
	artifact, err := s.repo.GetArtifactByID(ctx, orgID, artifactID)
	if err != nil {
		if errors.Is(err, domain.ErrArtifactNotFound) {
			return nil, domain.ErrArtifactNotFound
		}
		s.logger.Error("failed querying artifact for download", slog.String("org_id", orgID.String()), slog.String("artifact_id", artifactID.String()))
		return nil, domain.ErrBackupServiceUnavailable
	}

	// 3. Reject tombstoned / deleted artifact without attempting physical storage open
	if artifact.IsDeleted {
		return nil, domain.ErrArtifactNotFound
	}

	// 4. Branch by format: Restic Snapshot vs Direct Stream
	if artifact.Format == domain.ArtifactFormatResticSnapshot {
		return s.openResticArtifactDownload(ctx, orgID, artifact)
	}

	// 5. Open artifact stream from StorageProvider (Direct Stream)
	provider, err := s.resolveStorageProvider(ctx, orgID, artifact.StorageTargetID)
	if err != nil {
		s.logger.Error("failed resolving storage provider for artifact download", slog.String("target_id", artifact.StorageTargetID.String()), slog.String("error", err.Error()))
		return nil, domain.ErrBackupServiceUnavailable
	}
	reader, err := provider.OpenArtifact(ctx, artifact.StorageReference)
	if err != nil {
		s.logger.Error("failed opening physical artifact for download")
		if errors.Is(err, storage.ErrArtifactNotFound) {
			return nil, domain.ErrArtifactNotFound
		}
		return nil, domain.ErrBackupServiceUnavailable
	}

	var streamReader io.ReadCloser = reader
	if artifact.StoredSizeBytes != nil {
		if s.keyProvider == nil {
			_ = reader.Close()
			return nil, errors.New("artifact key provider cannot be nil: decryption required")
		}
		decReader, err := artifactcrypto.NewDecryptReader(reader, s.keyProvider, artifact.OrganizationID, artifact.ID)
		if err != nil {
			_ = reader.Close()
			s.logger.Error("failed initializing decrypt reader for download", slog.String("error", err.Error()))
			return nil, fmt.Errorf("failed initializing decrypt reader: %w", err)
		}
		if pErr := decReader.ParsePrologue(); pErr != nil {
			_ = decReader.Close()
			s.logger.Error("failed parsing artifact encryption prologue for download", slog.String("error", pErr.Error()))
			return nil, pErr
		}
		streamReader = decReader
	}

	var optLen *int64
	if artifact.SizeBytes > 0 {
		sz := artifact.SizeBytes
		optLen = &sz
	}

	return &DownloadDescriptor{
		Artifact:              artifact,
		Reader:                streamReader,
		Filename:              domain.SafeArtifactFilenameWithType(artifact.TargetName, artifact.Format, artifact.ArtifactType, artifact.ID),
		ContentType:           "application/gzip",
		OptionalContentLength: optLen,
	}, nil
}

func (s *ArtifactService) openResticArtifactDownload(
	ctx context.Context,
	orgID uuid.UUID,
	artifact *domain.BackupArtifact,
) (*DownloadDescriptor, error) {
	if s.resticRunner == nil || s.coordinator == nil || s.vault == nil || s.targetResolver == nil {
		s.logger.Error("restic download dependencies not configured")
		return nil, domain.ErrBackupServiceUnavailable
	}

	if artifact.RepositoryID == nil || *artifact.RepositoryID == uuid.Nil || artifact.SnapshotID == "" {
		s.logger.Error("invalid restic artifact metadata: missing repository_id or snapshot_id")
		return nil, domain.ErrBackupServiceUnavailable
	}

	repoID := *artifact.RepositoryID

	// 1. Acquire shared lock on repository
	releaseLock, err := s.coordinator.AcquireShared(ctx, repoID)
	if err != nil {
		s.logger.Error("failed acquiring shared lock for restic download", slog.String("repo_id", repoID.String()), slog.String("error", err.Error()))
		return nil, domain.ErrBackupServiceUnavailable
	}
	var releaseOnce sync.Once
	safeReleaseLock := func() {
		releaseOnce.Do(func() {
			releaseLock()
		})
	}
	var transferred bool
	defer func() {
		if !transferred {
			safeReleaseLock()
		}
	}()

	// 2. Fetch repository metadata
	repo, err := s.repo.GetRepositoryByID(ctx, orgID, repoID)
	if err != nil {
		s.logger.Error("failed loading restic repository record", slog.String("repo_id", repoID.String()), slog.String("error", err.Error()))
		return nil, domain.ErrBackupServiceUnavailable
	}

	// 3. Fetch storage target
	storageTarget, err := s.repo.GetStorageTargetByID(ctx, orgID, repo.StorageTargetID)
	if err != nil {
		s.logger.Error("failed loading storage target for restic download", slog.String("target_id", repo.StorageTargetID.String()), slog.String("error", err.Error()))
		return nil, domain.ErrBackupServiceUnavailable
	}

	// 4. Resolve concrete repository target
	target, err := s.targetResolver.ResolveTarget(ctx, orgID, repo.ResourceID, storageTarget)
	if err != nil {
		s.logger.Error("failed resolving repository target for restic download", slog.String("error", err.Error()))
		return nil, domain.ErrBackupServiceUnavailable
	}

	// 5. Load repository key
	credType, repoKey, err := s.vault.LoadCredentialForUse(ctx, orgID, repo.CredentialID)
	if err != nil {
		target.Cleanup()
		s.logger.Error("failed loading repository key for restic download", slog.String("error", err.Error()))
		return nil, domain.ErrBackupServiceUnavailable
	}
	if credType != credDomain.TypeResticRepositoryKey || len(repoKey) == 0 {
		secretcrypto.ZeroBytes(repoKey)
		target.Cleanup()
		s.logger.Error("invalid repository credential type for restic download")
		return nil, domain.ErrBackupServiceUnavailable
	}

	// 6. Resolve internal filename in snapshot
	internalFilename := ""
	nodes, listErr := s.resticRunner.ListSnapshotNodes(ctx, target, repoKey, artifact.SnapshotID)
	if listErr == nil {
		for _, node := range nodes {
			if node.Type == "file" || (node.Type == "" && !strings.HasSuffix(node.Name, "/")) {
				internalFilename = node.Name
				break
			}
		}
	}
	if internalFilename == "" {
		base := domain.SafeArtifactFilenameWithType(artifact.TargetName, artifact.Format, artifact.ArtifactType, artifact.ID)
		if artifact.ArtifactType == domain.ArtifactTypeDatabaseDump {
			internalFilename = strings.TrimSuffix(base, ".sql.gz") + ".sql"
		} else {
			internalFilename = strings.TrimSuffix(base, ".tar.gz") + ".tar"
		}
	}

	// 7. Start restic dump streaming
	rawStream, err := s.resticRunner.DumpStream(ctx, target, repoKey, artifact.SnapshotID, internalFilename)
	if err != nil {
		secretcrypto.ZeroBytes(repoKey)
		target.Cleanup()
		s.logger.Error("failed opening restic dump stream", slog.String("error", err.Error()))
		return nil, domain.ErrBackupServiceUnavailable
	}

	// 8. Stream on-the-fly gzip compression into io.Pipe
	pr, pw := io.Pipe()
	gw := gzip.NewWriter(pw)

	go func() {
		var copyErr error
		_, copyErr = io.Copy(gw, rawStream)
		if closeErr := gw.Close(); copyErr == nil {
			copyErr = closeErr
		}
		_ = pw.CloseWithError(copyErr)
	}()

	wrappedReader := &resticDownloadReader{
		pipeReader:    pr,
		rawStream:     rawStream,
		releaseLock:   safeReleaseLock,
		cleanupTarget: target.Cleanup,
		repoKey:       repoKey,
	}

	// Lock will be released by wrappedReader.Close()
	transferred = true

	return &DownloadDescriptor{
		Artifact:              artifact,
		Reader:                wrappedReader,
		Filename:              domain.SafeArtifactFilenameWithType(artifact.TargetName, artifact.Format, artifact.ArtifactType, artifact.ID),
		ContentType:           "application/gzip",
		OptionalContentLength: nil,
	}, nil
}

type resticDownloadReader struct {
	pipeReader    io.ReadCloser
	rawStream     io.ReadCloser
	releaseLock   func()
	cleanupTarget func()
	repoKey       []byte
	closeOnce     sync.Once
	closeErr      error
}

func (r *resticDownloadReader) Read(p []byte) (int, error) {
	return r.pipeReader.Read(p)
}

func (r *resticDownloadReader) Close() error {
	r.closeOnce.Do(func() {
		var errs []error
		if r.pipeReader != nil {
			if err := r.pipeReader.Close(); err != nil && !errors.Is(err, io.ErrClosedPipe) {
				errs = append(errs, err)
			}
		}
		if r.rawStream != nil {
			if err := r.rawStream.Close(); err != nil {
				errs = append(errs, err)
			}
		}
		if r.cleanupTarget != nil {
			r.cleanupTarget()
		}
		if r.releaseLock != nil {
			r.releaseLock()
		}
		if len(r.repoKey) > 0 {
			secretcrypto.ZeroBytes(r.repoKey)
		}
		if len(errs) > 0 {
			r.closeErr = errs[0]
		}
	})
	return r.closeErr
}

// RecordDownloadAudit logs a successful download event and returns an error if audit recording fails.
func (s *ArtifactService) RecordDownloadAudit(
	ctx context.Context,
	orgID, userID, artifactID uuid.UUID,
	sizeBytes int64,
	clientIP, userAgent string,
) error {
	if s.auditRecorder == nil {
		return nil
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

	if err := s.auditRecorder.Record(ctx, entry); err != nil {
		s.logger.Error("failed recording audit log for artifact download",
			slog.String("org_id", orgID.String()),
			slog.String("artifact_id", artifactID.String()),
		)
		return err
	}
	return nil
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

	if artifact.Format == domain.ArtifactFormatResticSnapshot {
		return fmt.Errorf("%w: deletion of restic snapshots is not supported in this version", domain.ErrBackupServiceUnavailable)
	}

	// 4. Physical Deletion First: delete bytes from storage provider
	provider, err := s.resolveStorageProvider(ctx, orgID, artifact.StorageTargetID)
	if err != nil {
		s.logger.Error("failed resolving storage provider for artifact deletion", slog.String("target_id", artifact.StorageTargetID.String()), slog.String("error", err.Error()))
		return domain.ErrArtifactDeleteFailed
	}
	if err := provider.DeleteArtifact(ctx, artifact.StorageReference); err != nil {
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
		if err := s.auditRecorder.Record(ctx, entry); err != nil {
			s.logger.Error("failed recording audit log for artifact deletion",
				slog.String("org_id", orgID.String()),
				slog.String("artifact_id", artifactID.String()),
			)
		}
	}

	return nil
}
