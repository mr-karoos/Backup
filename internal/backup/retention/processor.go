package retention

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	auditDomain "backup-platform/internal/audit/domain"
	"backup-platform/internal/backup/domain"
	"backup-platform/internal/storage"
	"backup-platform/pkg/uuid"
)

// PlanAndRunRepository defines the minimal database operations needed by the retention processor.
type PlanAndRunRepository interface {
	GetPlanByID(ctx context.Context, orgID, planID uuid.UUID) (*domain.BackupPlan, error)
	ListSuccessfulRunsForPlan(ctx context.Context, orgID, planID uuid.UUID) ([]*domain.BackupRun, error)
	GetRunArtifacts(ctx context.Context, orgID, runID uuid.UUID) ([]*domain.BackupArtifact, error)
	TombstoneArtifact(ctx context.Context, orgID, artifactID uuid.UUID) error
}

// StorageProvider defines the artifact deletion operation for storage targets.
type StorageProvider interface {
	DeleteArtifact(ctx context.Context, storageRef string) error
}

// AuditRecorder defines the audit log emission interface.
type AuditRecorder interface {
	Record(ctx context.Context, entry *auditDomain.AuditLog) error
}

// CleanupSummary captures deterministic execution statistics for retention runs.
type CleanupSummary struct {
	RunsEvaluated      int
	RunsExpired        int
	ArtifactsAttempted int
	ArtifactsDeleted   int
}

// Processor manages automated retention policy execution for backup plan runs.
type Processor struct {
	repo            PlanAndRunRepository
	storageProvider StorageProvider
	storageResolver storage.StorageProviderResolver
	auditRecorder   AuditRecorder
	logger          *slog.Logger
	nowFunc         func() time.Time
}

// NewProcessor constructs a new retention Processor.
func NewProcessor(
	repo PlanAndRunRepository,
	storageProvider StorageProvider,
	auditRecorder AuditRecorder,
	logger *slog.Logger,
) *Processor {
	if logger == nil {
		logger = slog.Default()
	}
	return &Processor{
		repo:            repo,
		storageProvider: storageProvider,
		auditRecorder:   auditRecorder,
		logger:          logger,
		nowFunc:         time.Now,
	}
}

// SetStorageResolver configures a dynamic storage provider resolver.
func (p *Processor) SetStorageResolver(resolver storage.StorageProviderResolver) {
	p.storageResolver = resolver
}

// SetNowFunc injects a custom clock supplier for deterministic unit and integration testing.
func (p *Processor) SetNowFunc(f func() time.Time) {
	if f != nil {
		p.nowFunc = f
	}
}

// ApplyAfterSuccessfulRun evaluates and applies the plan's retention policy following a successful BackupRun.
// It uses conservative OR semantics and ensures that errors during cleanup do not corrupt the successful run state.
func (p *Processor) ApplyAfterSuccessfulRun(
	ctx context.Context,
	orgID uuid.UUID,
	planID *uuid.UUID,
	currentRunID uuid.UUID,
) (*CleanupSummary, error) {
	summary := &CleanupSummary{}

	if planID == nil || *planID == uuid.Nil || orgID == uuid.Nil {
		return summary, nil
	}

	plan, err := p.repo.GetPlanByID(ctx, orgID, *planID)
	if err != nil {
		if errors.Is(err, domain.ErrPlanNotFound) {
			return summary, nil
		}
		p.logger.Warn("failed to fetch backup plan for retention processing",
			slog.String("org_id", orgID.String()),
			slog.String("plan_id", planID.String()),
		)
		return summary, fmt.Errorf("failed fetching backup plan for retention: %w", err)
	}

	if plan == nil || (plan.RetentionCount == nil && plan.RetentionDays == nil) {
		// No retention policy configured -> no-op
		return summary, nil
	}

	successfulRuns, err := p.repo.ListSuccessfulRunsForPlan(ctx, orgID, *planID)
	if err != nil {
		p.logger.Warn("failed to list successful runs for retention evaluation",
			slog.String("org_id", orgID.String()),
			slog.String("plan_id", planID.String()),
		)
		return summary, fmt.Errorf("failed listing successful runs for retention: %w", err)
	}

	summary.RunsEvaluated = len(successfulRuns)
	if len(successfulRuns) == 0 {
		return summary, nil
	}

	now := p.nowFunc()
	var cutoff time.Time
	if plan.RetentionDays != nil {
		cutoff = now.Add(-time.Duration(*plan.RetentionDays) * 24 * time.Hour)
	}

	var candidateRuns []*domain.BackupRun
	for idx, run := range successfulRuns {
		if run == nil {
			continue
		}

		// Invariant: The current run that just succeeded must NEVER be deleted in its own retention invocation
		if run.ID == currentRunID {
			continue
		}

		// Defensive: Successful runs without an ended_at timestamp must never expire
		if run.EndedAt == nil {
			continue
		}

		keepByCount := false
		if plan.RetentionCount != nil && idx < *plan.RetentionCount {
			keepByCount = true
		}

		keepByDays := false
		if plan.RetentionDays != nil && run.EndedAt != nil {
			// Boundary at cutoff is kept (!run.EndedAt.Before(cutoff))
			if !run.EndedAt.Before(cutoff) {
				keepByDays = true
			}
		}

		var keep bool
		if plan.RetentionCount != nil && plan.RetentionDays != nil {
			// Conservative OR: Keep if EITHER policy satisfies retention
			keep = keepByCount || keepByDays
		} else if plan.RetentionCount != nil {
			keep = keepByCount
		} else if plan.RetentionDays != nil {
			keep = keepByDays
		} else {
			keep = true
		}

		if !keep {
			candidateRuns = append(candidateRuns, run)
		}
	}

	summary.RunsExpired = len(candidateRuns)
	if len(candidateRuns) == 0 {
		return summary, nil
	}

	// Fail closed: Mandatory operational dependencies must be present before executing cleanup
	if p.storageProvider == nil && p.storageResolver == nil {
		p.logger.Error("storage provider dependency is missing; aborting retention cleanup",
			slog.String("org_id", orgID.String()),
			slog.String("plan_id", planID.String()),
		)
		return summary, errors.New("retention cleanup aborted: missing storage provider")
	}

	if p.auditRecorder == nil {
		p.logger.Error("audit recorder dependency is missing; aborting retention cleanup",
			slog.String("org_id", orgID.String()),
			slog.String("plan_id", planID.String()),
		)
		return summary, errors.New("retention cleanup aborted: missing audit recorder")
	}

	for _, run := range candidateRuns {
		artifacts, err := p.repo.GetRunArtifacts(ctx, orgID, run.ID)
		if err != nil {
			p.logger.Warn("failed querying run artifacts during retention cleanup",
				slog.String("org_id", orgID.String()),
				slog.String("run_id", run.ID.String()),
			)
			continue
		}

		for _, art := range artifacts {
			if art == nil || art.IsDeleted {
				continue
			}

			summary.ArtifactsAttempted++

			storeProvider := p.storageProvider
			if p.storageResolver != nil && art.StorageTargetID != uuid.Nil {
				resolved, err := p.storageResolver.Resolve(ctx, orgID, art.StorageTargetID)
				if err != nil {
					p.logger.Warn("failed resolving storage provider during retention cleanup",
						slog.String("org_id", orgID.String()),
						slog.String("artifact_id", art.ID.String()),
						slog.String("target_id", art.StorageTargetID.String()),
						slog.String("error", err.Error()),
					)
					continue
				}
				storeProvider = resolved
			}

			if storeProvider == nil {
				p.logger.Warn("no storage provider available for artifact retention deletion",
					slog.String("org_id", orgID.String()),
					slog.String("artifact_id", art.ID.String()),
				)
				continue
			}

			// 1. Physical file deletion first
			delErr := storeProvider.DeleteArtifact(ctx, art.StorageReference)
			if delErr != nil && !errors.Is(delErr, storage.ErrArtifactNotFound) {
				p.logger.Warn("failed to delete artifact physical file during retention",
					slog.String("org_id", orgID.String()),
					slog.String("artifact_id", art.ID.String()),
				)
				// DO NOT tombstone in database or emit audit on physical deletion failure
				continue
			}

			// 2. Database tombstone: is_deleted = true, deleted_at = NOW()
			tbErr := p.repo.TombstoneArtifact(ctx, orgID, art.ID)
			if tbErr != nil {
				p.logger.Warn("failed tombstoning retention-deleted artifact in database",
					slog.String("org_id", orgID.String()),
					slog.String("artifact_id", art.ID.String()),
				)
				// DO NOT emit audit or fake rollback physical file
				continue
			}

			summary.ArtifactsDeleted++

			// 3. Record retention audit event (best-effort preservation on audit recording failure)
			metaObj := map[string]any{
				"artifact_id":    art.ID.String(),
				"backup_plan_id": plan.ID.String(),
				"run_id":         run.ID.String(),
				"size_bytes":     art.SizeBytes,
			}
			if plan.RetentionCount != nil {
				metaObj["retention_count"] = *plan.RetentionCount
			}
			if plan.RetentionDays != nil {
				metaObj["retention_days"] = *plan.RetentionDays
			}
			metaBytes, _ := json.Marshal(metaObj)

			auditLog := &auditDomain.AuditLog{
				ID:             uuid.New(),
				OrganizationID: &orgID,
				UserID:         nil, // system-generated
				Action:         auditDomain.ActionRetentionCleanup,
				EntityType:     auditDomain.EntityTypeBackupArtifact,
				EntityID:       &art.ID,
				IPAddress:      nil,
				UserAgent:      nil,
				Metadata:       metaBytes,
				CreatedAt:      p.nowFunc(),
			}

			if auditErr := p.auditRecorder.Record(ctx, auditLog); auditErr != nil {
				p.logger.Warn("failed recording audit log for retention cleanup",
					slog.String("org_id", orgID.String()),
					slog.String("artifact_id", art.ID.String()),
				)
			}
		}
	}

	return summary, nil
}
