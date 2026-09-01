package service

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"backup-platform/internal/backup/domain"
	"backup-platform/internal/backup/repository"
	"backup-platform/internal/backup/verification"
	orgDomain "backup-platform/internal/organization/domain"
	"backup-platform/internal/storage"
	"backup-platform/pkg/uuid"
)

// VerificationDetails defines the strict 4-field public details structure matching Frozen docs/API_DESIGN.md.
type VerificationDetails struct {
	ChecksumMatched      bool   `json:"checksum_matched"`
	ArchiveIntegrity     string `json:"archive_integrity"`
	CompressionValid     bool   `json:"compression_valid"`
	ExtractedSampleCheck string `json:"extracted_sample_check"`
}

// RunVerificationResult contains the consolidated result of verifying all artifacts of a backup run.
type RunVerificationResult struct {
	RunID              uuid.UUID                 `json:"run_id"`
	VerificationStatus domain.VerificationStatus `json:"verification_status"`
	VerifiedAt         time.Time                 `json:"verified_at"`
	Details            VerificationDetails       `json:"details"`
}

// VerificationService coordinates on-demand verification of backup runs and their artifacts.
type VerificationService struct {
	repo     repository.BackupRepository
	storage  storage.StorageProvider
	verifier verification.Verifier
	logger   *slog.Logger
	nowFunc  func() time.Time
}

// NewVerificationService constructs a new VerificationService.
func NewVerificationService(
	repo repository.BackupRepository,
	storageProvider storage.StorageProvider,
	verifier verification.Verifier,
	logger *slog.Logger,
) *VerificationService {
	if logger == nil {
		logger = slog.Default()
	}
	return &VerificationService{
		repo:     repo,
		storage:  storageProvider,
		verifier: verifier,
		logger:   logger,
		nowFunc:  time.Now,
	}
}

// SetNowFunc overrides the clock for deterministic testing.
func (s *VerificationService) SetNowFunc(f func() time.Time) {
	s.nowFunc = f
}

// VerifyRun executes on-demand physical and structural integrity verification on all active artifacts of a backup run.
func (s *VerificationService) VerifyRun(
	ctx context.Context,
	role orgDomain.Role,
	orgID, runID uuid.UUID,
) (*RunVerificationResult, error) {
	// 1. Service-Level RBAC Enforcement: Only admin and member can verify runs
	if role != orgDomain.RoleAdmin && role != orgDomain.RoleMember {
		return nil, domain.ErrUnauthorizedRole
	}

	if orgID == uuid.Nil || runID == uuid.Nil {
		return nil, domain.ErrRunNotFound
	}

	// 2. Fail closed on uninitialized core dependencies
	if s.repo == nil || s.storage == nil || s.verifier == nil {
		s.logger.Error("verification service dependencies not initialized")
		return nil, domain.ErrBackupServiceUnavailable
	}

	// 3. Query backup run scoped strictly to organization
	run, err := s.repo.GetRunByID(ctx, orgID, runID)
	if err != nil {
		if errors.Is(err, domain.ErrRunNotFound) {
			return nil, domain.ErrRunNotFound
		}
		s.logger.Error("failed querying backup run for verification",
			slog.String("org_id", orgID.String()),
			slog.String("run_id", runID.String()),
		)
		return nil, domain.ErrBackupServiceUnavailable
	}
	if run == nil {
		return nil, domain.ErrRunNotFound
	}

	// 4. Query artifacts for the run scoped to organization
	artifacts, err := s.repo.GetRunArtifacts(ctx, orgID, runID)
	if err != nil {
		s.logger.Error("failed querying artifacts for backup run verification",
			slog.String("org_id", orgID.String()),
			slog.String("run_id", runID.String()),
		)
		return nil, domain.ErrBackupServiceUnavailable
	}

	// 5. Filter active, non-deleted artifacts
	var activeArtifacts []*domain.BackupArtifact
	var hasDBArtifacts bool
	for _, art := range artifacts {
		if art != nil && !art.IsDeleted {
			activeArtifacts = append(activeArtifacts, art)
			if art.ArtifactType == domain.ArtifactTypeDatabaseDump || art.Format == domain.ArtifactFormatSQLGzip {
				hasDBArtifacts = true
			}
		}
	}

	if len(activeArtifacts) == 0 {
		return nil, domain.ErrNoVerifiableArtifacts
	}

	// 6. Verify each active artifact independently
	var failedCount int

	for _, art := range activeArtifacts {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		var verMsg string
		var verErr error

		switch art.Format {
		case domain.ArtifactFormatSQLGzip:
			verMsg, verErr = s.verifier.VerifyDatabaseArtifact(
				ctx,
				s.storage,
				art.StorageReference,
				art.SizeBytes,
				art.ChecksumHash,
			)
		case domain.ArtifactFormatTarGzip:
			verMsg, verErr = s.verifier.VerifyFilesArtifact(
				ctx,
				s.storage,
				art.StorageReference,
				art.SizeBytes,
				art.ChecksumHash,
			)
		default:
			if art.ArtifactType == domain.ArtifactTypeDatabaseDump {
				verMsg, verErr = s.verifier.VerifyDatabaseArtifact(
					ctx,
					s.storage,
					art.StorageReference,
					art.SizeBytes,
					art.ChecksumHash,
				)
			} else if art.ArtifactType == domain.ArtifactTypeFilesArchive {
				verMsg, verErr = s.verifier.VerifyFilesArtifact(
					ctx,
					s.storage,
					art.StorageReference,
					art.SizeBytes,
					art.ChecksumHash,
				)
			} else {
				verErr = errors.New("unsupported artifact format for verification")
			}
		}

		// Handle context cancellation / deadline expiration deterministically
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if errors.Is(verErr, context.Canceled) || errors.Is(verErr, context.DeadlineExceeded) {
			return nil, verErr
		}

		// Handle storage infrastructure failure (not an integrity failure)
		if verErr != nil && strings.HasPrefix(verErr.Error(), "failed opening artifact for verification") {
			s.logger.Error("storage infrastructure error opening artifact for verification",
				slog.String("org_id", orgID.String()),
				slog.String("artifact_id", art.ID.String()),
			)
			return nil, domain.ErrBackupServiceUnavailable
		}

		if verErr != nil {
			failedCount++
			safeDetails := sanitizeVerificationError(verErr)

			// Persist failed status in database per artifact
			if updateErr := s.repo.UpdateArtifactVerification(ctx, orgID, art.ID, domain.VerificationStatusFailed, &safeDetails); updateErr != nil {
				s.logger.Error("failed updating artifact verification status in repository",
					slog.String("org_id", orgID.String()),
					slog.String("artifact_id", art.ID.String()),
				)
				return nil, domain.ErrBackupServiceUnavailable
			}
		} else {
			// Persist verified status in database per artifact
			if updateErr := s.repo.UpdateArtifactVerification(ctx, orgID, art.ID, domain.VerificationStatusVerified, &verMsg); updateErr != nil {
				s.logger.Error("failed updating artifact verification status in repository",
					slog.String("org_id", orgID.String()),
					slog.String("artifact_id", art.ID.String()),
				)
				return nil, domain.ErrBackupServiceUnavailable
			}
		}
	}

	// 7. Aggregate overall verification result and details (Frozen 4-field contract)
	verifiedAt := s.nowFunc()
	var overallStatus domain.VerificationStatus
	var details VerificationDetails

	if failedCount == 0 {
		overallStatus = domain.VerificationStatusVerified
		sampleCheck := "not_applicable"
		if hasDBArtifacts {
			sampleCheck = "valid_sql_dump"
		}
		details = VerificationDetails{
			ChecksumMatched:      true,
			ArchiveIntegrity:     "passed",
			CompressionValid:     true,
			ExtractedSampleCheck: sampleCheck,
		}
	} else {
		overallStatus = domain.VerificationStatusFailed
		sampleCheck := "not_applicable"
		if hasDBArtifacts {
			sampleCheck = "failed"
		}
		details = VerificationDetails{
			ChecksumMatched:      false,
			ArchiveIntegrity:     "failed",
			CompressionValid:     false,
			ExtractedSampleCheck: sampleCheck,
		}
	}

	return &RunVerificationResult{
		RunID:              runID,
		VerificationStatus: overallStatus,
		VerifiedAt:         verifiedAt,
		Details:            details,
	}, nil
}

func sanitizeVerificationError(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "checksum mismatch"):
		return "checksum mismatch: hash does not match recorded checksum"
	case strings.Contains(msg, "artifact size mismatch"):
		return "artifact size mismatch: physical size differs from recorded size"
	case strings.Contains(msg, "verified artifact size is zero"), strings.Contains(msg, "size must be greater than zero"):
		return "artifact size is zero"
	case strings.Contains(msg, "not a valid gzip stream"):
		return "artifact is not a valid gzip stream"
	case strings.Contains(msg, "gzip stream integrity check failed"):
		return "gzip stream structural integrity check failed"
	case strings.Contains(msg, "gzip stream closure error"):
		return "gzip stream closure error"
	case strings.Contains(msg, "tar structural integrity check failed"):
		return "tar archive structural integrity check failed"
	case strings.Contains(msg, "unsafe absolute member path"):
		return "tar archive contains an unsafe absolute member path"
	case strings.Contains(msg, "unsafe parent traversal member path"):
		return "tar archive contains an unsafe parent traversal member path"
	case strings.Contains(msg, "decompressed database dump is empty"):
		return "decompressed database dump is empty"
	case strings.Contains(msg, "failed basic SQL dump format sanity check"):
		return "decompressed stream failed basic SQL dump format sanity check"
	case strings.Contains(msg, "tar archive is empty"):
		return "tar archive is empty (zero entries)"
	default:
		return "backup integrity verification failed"
	}
}
