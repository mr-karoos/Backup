package engine

import (
	"context"
	"errors"
	"io"
	"log/slog"

	"backup-platform/internal/backup/domain"
	"backup-platform/internal/backup/restic"
	"backup-platform/internal/connector"
	"backup-platform/internal/credential/payload"
	"backup-platform/pkg/uuid"
)

// ResticBackupEngine coordinates streaming backups into per-resource Restic repositories (ADR-017, ADR-031).
// It streams uncompressed, raw logical content (SQL or tar) directly through STDIN into the Restic process,
// relying on native repository compression and encryption.
type ResticBackupEngine struct {
	supervisor *GatedEOFSupervisor
	logger     *slog.Logger
}

// NewResticBackupEngine constructs a new ResticBackupEngine.
func NewResticBackupEngine(supervisor *GatedEOFSupervisor, logger *slog.Logger) *ResticBackupEngine {
	if logger == nil {
		logger = slog.Default()
	}
	return &ResticBackupEngine{
		supervisor: supervisor,
		logger:     logger,
	}
}

// ExecuteDatabaseBackup streams raw MySQL dump output from the connector capability into Restic via STDIN.
func (e *ResticBackupEngine) ExecuteDatabaseBackup(
	ctx context.Context,
	cap connector.DatabaseBackupCapability,
	target connector.Target,
	credPayload *payload.PayloadV1,
	databaseName string,
	repoTarget restic.RepositoryTarget,
	repoPassword []byte,
	orgID, resID, runID, artifactID uuid.UUID,
) (*ResticExecutionResult, error) {
	if cap == nil {
		return nil, errors.New("database backup capability cannot be nil")
	}
	if repoTarget == nil {
		return nil, errors.New("repository target cannot be nil")
	}
	if len(repoPassword) == 0 {
		return nil, errors.New("repository password cannot be empty")
	}
	if artifactID == uuid.Nil {
		return nil, errors.New("artifact ID must be pre-generated before backup start")
	}

	targetToken := BuildDeterministicTargetToken(domain.BackupTypeMySQLDatabase, databaseName)
	internalFilename := targetToken + ".sql"

	req := StdinBackupRequest{
		Target:           repoTarget,
		Password:         repoPassword,
		OrgID:            orgID,
		ResourceID:       resID,
		RunID:            runID,
		ArtifactID:       artifactID,
		BackupType:       domain.BackupTypeMySQLDatabase,
		TargetName:       databaseName,
		InternalFilename: internalFilename,
		StreamProducer: func(streamCtx context.Context, stdin io.Writer) error {
			return cap.BackupDatabase(streamCtx, target, credPayload, databaseName, stdin)
		},
	}

	return e.supervisor.ExecuteBackup(ctx, req)
}

// ExecuteFilesBackup streams raw tar archive output from the connector capability into Restic via STDIN.
func (e *ResticBackupEngine) ExecuteFilesBackup(
	ctx context.Context,
	fileCap connector.FileBackupCapability,
	target connector.Target,
	credPayload *payload.PayloadV1,
	config connector.FileBackupConfig,
	repoTarget restic.RepositoryTarget,
	repoPassword []byte,
	orgID, resID, runID, artifactID uuid.UUID,
) (*ResticExecutionResult, error) {
	if fileCap == nil {
		return nil, errors.New("file backup capability cannot be nil")
	}
	if repoTarget == nil {
		return nil, errors.New("repository target cannot be nil")
	}
	if len(repoPassword) == 0 {
		return nil, errors.New("repository password cannot be empty")
	}
	if artifactID == uuid.Nil {
		return nil, errors.New("artifact ID must be pre-generated before backup start")
	}

	targetToken := BuildDeterministicTargetToken(domain.BackupTypeWebsiteFiles, config.SourcePath)
	internalFilename := targetToken + ".tar"

	req := StdinBackupRequest{
		Target:           repoTarget,
		Password:         repoPassword,
		OrgID:            orgID,
		ResourceID:       resID,
		RunID:            runID,
		ArtifactID:       artifactID,
		BackupType:       domain.BackupTypeWebsiteFiles,
		TargetName:       config.SourcePath,
		InternalFilename: internalFilename,
		StreamProducer: func(streamCtx context.Context, stdin io.Writer) error {
			return fileCap.BackupFiles(streamCtx, target, credPayload, config, stdin)
		},
	}

	return e.supervisor.ExecuteBackup(ctx, req)
}
