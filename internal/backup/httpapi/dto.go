package httpapi

import (
	"time"

	"backup-platform/internal/backup/domain"
	"backup-platform/internal/backup/service"
	"backup-platform/pkg/uuid"
)

// CreateBackupJobRequest defines the JSON payload for creating a manual backup job.
type CreateBackupJobRequest struct {
	BackupPlanID    *uuid.UUID         `json:"backup_plan_id,omitempty"`
	ResourceID      *uuid.UUID         `json:"resource_id,omitempty"`
	BackupType      domain.BackupType  `json:"backup_type,omitempty"`
	EngineType      *domain.EngineType `json:"engine_type,omitempty"`
	StorageTargetID *uuid.UUID         `json:"storage_target_id,omitempty"`
	TargetSpec      *domain.TargetSpec `json:"target_spec,omitempty"`
}

// BackupJobResponse defines the JSON response returned for a backup job.
type BackupJobResponse struct {
	ID              uuid.UUID          `json:"id"`
	ResourceID      uuid.UUID          `json:"resource_id"`
	BackupPlanID    *uuid.UUID         `json:"backup_plan_id"`
	BackupType      domain.BackupType  `json:"backup_type"`
	EngineType      domain.EngineType  `json:"engine_type"`
	StorageTargetID uuid.UUID          `json:"storage_target_id"`
	TargetSpec      domain.TargetSpec  `json:"target_spec"`
	Status          domain.JobStatus   `json:"status"`
	TriggerType     domain.TriggerType `json:"trigger_type"`
	CreatedAt       time.Time          `json:"created_at"`
}

func toBackupJobResponse(j *domain.BackupJob) *BackupJobResponse {
	if j == nil {
		return nil
	}
	return &BackupJobResponse{
		ID:              j.ID,
		ResourceID:      j.ResourceID,
		BackupPlanID:    j.BackupPlanID,
		BackupType:      j.BackupType,
		EngineType:      j.EngineType,
		StorageTargetID: j.StorageTargetID,
		TargetSpec:      j.TargetSpec,
		Status:          j.Status,
		TriggerType:     j.TriggerType,
		CreatedAt:       j.CreatedAt,
	}
}

// DatabaseSelectionDTO defines API target selection for MySQL databases.
type DatabaseSelectionDTO struct {
	Mode      string   `json:"mode"`
	Databases []string `json:"databases,omitempty"`
}

// FileSelectionDTO defines API target selection for website files.
type FileSelectionDTO struct {
	Paths           []string  `json:"paths"`
	ExcludePatterns *[]string `json:"exclude_patterns"`
}

// ScheduleDTO defines the schedule configuration in API requests and responses.
type ScheduleDTO struct {
	IsEnabled      bool    `json:"is_enabled"`
	CronExpression *string `json:"cron_expression,omitempty"`
	Timezone       string  `json:"timezone"`
	NextRunAt      *string `json:"next_run_at,omitempty"`
}

// RetentionPolicyDTO defines retention policy configuration in API requests and responses.
type RetentionPolicyDTO struct {
	KeepLastN *int `json:"keep_last_n,omitempty"`
	KeepDays  *int `json:"keep_days,omitempty"`
}

// CreateBackupPlanRequest defines the JSON payload for creating a backup plan.
type CreateBackupPlanRequest struct {
	Name              string                `json:"name"`
	ResourceID        uuid.UUID             `json:"resource_id"`
	BackupType        domain.BackupType     `json:"backup_type"`
	EngineType        *domain.EngineType    `json:"engine_type,omitempty"`
	StorageTargetID   *uuid.UUID            `json:"storage_target_id,omitempty"`
	DatabaseSelection *DatabaseSelectionDTO `json:"database_selection,omitempty"`
	FileSelection     *FileSelectionDTO     `json:"file_selection,omitempty"`
	Schedule          ScheduleDTO           `json:"schedule"`
	RetentionPolicy   *RetentionPolicyDTO   `json:"retention_policy,omitempty"`
}

// UpdateBackupPlanRequest defines the JSON payload for updating a backup plan.
type UpdateBackupPlanRequest struct {
	Name              string                `json:"name"`
	EngineType        *domain.EngineType    `json:"engine_type,omitempty"`
	StorageTargetID   *uuid.UUID            `json:"storage_target_id,omitempty"`
	DatabaseSelection *DatabaseSelectionDTO `json:"database_selection,omitempty"`
	FileSelection     *FileSelectionDTO     `json:"file_selection,omitempty"`
	Schedule          ScheduleDTO           `json:"schedule"`
	RetentionPolicy   *RetentionPolicyDTO   `json:"retention_policy,omitempty"`
	Status            domain.PlanStatus     `json:"status"`
}

// CreateBackupPlanResponse defines the minimal response returned on plan creation.
type CreateBackupPlanResponse struct {
	ID              uuid.UUID         `json:"id"`
	Name            string            `json:"name"`
	ResourceID      uuid.UUID         `json:"resource_id"`
	EngineType      domain.EngineType `json:"engine_type"`
	StorageTargetID uuid.UUID         `json:"storage_target_id"`
	Status          domain.PlanStatus `json:"status"`
	CreatedAt       time.Time         `json:"created_at"`
}

// BackupPlanResponse defines the representation of a backup plan in list and detail responses.
type BackupPlanResponse struct {
	ID                uuid.UUID             `json:"id"`
	ResourceID        uuid.UUID             `json:"resource_id"`
	ResourceName      string                `json:"resource_name"`
	Name              string                `json:"name"`
	BackupType        domain.BackupType     `json:"backup_type"`
	EngineType        domain.EngineType     `json:"engine_type"`
	StorageTargetID   uuid.UUID             `json:"storage_target_id"`
	Status            domain.PlanStatus     `json:"status"`
	DatabaseSelection *DatabaseSelectionDTO `json:"database_selection,omitempty"`
	FileSelection     *FileSelectionDTO     `json:"file_selection,omitempty"`
	Schedule          ScheduleDTO           `json:"schedule"`
	RetentionPolicy   *RetentionPolicyDTO   `json:"retention_policy,omitempty"`
	CreatedAt         time.Time             `json:"created_at"`
	UpdatedAt         *time.Time            `json:"updated_at,omitempty"`
}

func toCreateBackupPlanResponse(p *domain.BackupPlan) *CreateBackupPlanResponse {
	if p == nil {
		return nil
	}
	return &CreateBackupPlanResponse{
		ID:              p.ID,
		Name:            p.Name,
		ResourceID:      p.ResourceID,
		EngineType:      p.EngineType,
		StorageTargetID: p.StorageTargetID,
		Status:          p.Status,
		CreatedAt:       p.CreatedAt,
	}
}

func toBackupPlanResponse(p *domain.BackupPlan, resourceName string, includeUpdatedAt bool) *BackupPlanResponse {
	if p == nil {
		return nil
	}

	var dbSel *DatabaseSelectionDTO
	var fileSel *FileSelectionDTO

	switch p.BackupType {
	case domain.BackupTypeMySQLDatabase:
		if len(p.TargetSpec.Databases) == 0 {
			dbSel = &DatabaseSelectionDTO{
				Mode: "all",
			}
		} else {
			dbSel = &DatabaseSelectionDTO{
				Mode:      "selected",
				Databases: p.TargetSpec.Databases,
			}
		}
	case domain.BackupTypeWebsiteFiles:
		excludes := []string{}
		if p.TargetSpec.ExcludePatterns != nil {
			excludes = *p.TargetSpec.ExcludePatterns
		}
		fileSel = &FileSelectionDTO{
			Paths:           p.TargetSpec.Paths,
			ExcludePatterns: &excludes,
		}
	}

	var formattedNextRun *string
	if p.NextRunAt != nil {
		loc, err := time.LoadLocation(p.ScheduleTimezone)
		if err == nil {
			localTime := p.NextRunAt.In(loc)
			formatted := localTime.Format(time.RFC3339)
			formattedNextRun = &formatted
		} else {
			formatted := p.NextRunAt.Format(time.RFC3339)
			formattedNextRun = &formatted
		}
	}

	sched := ScheduleDTO{
		IsEnabled:      p.IsScheduleEnabled,
		CronExpression: p.ScheduleCron,
		Timezone:       p.ScheduleTimezone,
		NextRunAt:      formattedNextRun,
	}

	var retPolicy *RetentionPolicyDTO
	if p.RetentionCount != nil || p.RetentionDays != nil {
		retPolicy = &RetentionPolicyDTO{
			KeepLastN: p.RetentionCount,
			KeepDays:  p.RetentionDays,
		}
	}

	var updatedAt *time.Time
	if includeUpdatedAt {
		updatedAt = &p.UpdatedAt
	}

	return &BackupPlanResponse{
		ID:                p.ID,
		ResourceID:        p.ResourceID,
		ResourceName:      resourceName,
		Name:              p.Name,
		BackupType:        p.BackupType,
		EngineType:        p.EngineType,
		StorageTargetID:   p.StorageTargetID,
		Status:            p.Status,
		DatabaseSelection: dbSel,
		FileSelection:     fileSel,
		Schedule:          sched,
		RetentionPolicy:   retPolicy,
		CreatedAt:         p.CreatedAt,
		UpdatedAt:         updatedAt,
	}
}

// BackupRunResponse defines the JSON response for a backup run in list and detail endpoints.
type BackupRunResponse struct {
	ID                     uuid.UUID        `json:"id"`
	JobID                  uuid.UUID        `json:"job_id"`
	ResourceID             uuid.UUID        `json:"resource_id"`
	AttemptNumber          int              `json:"attempt_number"`
	Status                 domain.RunStatus `json:"status"`
	StartedAt              time.Time        `json:"started_at"`
	EndedAt                *time.Time       `json:"ended_at"`
	DurationSeconds        *int64           `json:"duration_seconds"`
	TotalArtifactSizeBytes int64            `json:"total_artifact_size_bytes"`
	ErrorMessage           *string          `json:"error_message"`
	ArtifactsCount         int              `json:"artifacts_count"`
	CreatedAt              time.Time        `json:"created_at"`
}

func toBackupRunResponse(stat *domain.BackupRunWithStats) *BackupRunResponse {
	if stat == nil {
		return nil
	}

	var durationSec *int64
	if stat.Run.EndedAt != nil && !stat.Run.StartedAt.IsZero() {
		d := int64(stat.Run.EndedAt.Sub(stat.Run.StartedAt).Seconds())
		if d < 0 {
			d = 0
		}
		durationSec = &d
	}

	return &BackupRunResponse{
		ID:                     stat.Run.ID,
		JobID:                  stat.Run.JobID,
		ResourceID:             stat.ResourceID,
		AttemptNumber:          stat.Run.AttemptNumber,
		Status:                 stat.Run.Status,
		StartedAt:              stat.Run.StartedAt,
		EndedAt:                stat.Run.EndedAt,
		DurationSeconds:        durationSec,
		TotalArtifactSizeBytes: stat.TotalArtifactSizeBytes,
		ErrorMessage:           stat.Run.ErrorMessage,
		ArtifactsCount:         stat.ArtifactsCount,
		CreatedAt:              stat.Run.CreatedAt,
	}
}

// BackupArtifactResponse defines the public representation of a backup artifact.
type BackupArtifactResponse struct {
	ID                 uuid.UUID                 `json:"id"`
	RunID              uuid.UUID                 `json:"run_id"`
	ResourceID         uuid.UUID                 `json:"resource_id"`
	ArtifactName       string                    `json:"artifact_name"`
	SizeBytes          *int64                    `json:"size_bytes"`
	ChecksumSHA256     *string                   `json:"checksum_sha256"`
	CompressionType    string                    `json:"compression_type"`
	VerificationStatus domain.VerificationStatus `json:"verification_status"`
	VerifiedAt         *time.Time                `json:"verified_at"`
	LogicalSizeBytes   *int64                    `json:"logical_size_bytes,omitempty"`
	CreatedAt          time.Time                 `json:"created_at"`
}

func toBackupArtifactResponse(a *domain.BackupArtifact) *BackupArtifactResponse {
	if a == nil {
		return nil
	}

	var sizeBytes *int64
	var checksumSHA256 *string
	var logicalSizeBytes *int64
	compType := "gzip"

	if a.Format == domain.ArtifactFormatResticSnapshot {
		compType = "restic"
		logicalSizeBytes = a.LogicalSizeBytes
	} else {
		s := a.SizeBytes
		sizeBytes = &s
		h := a.ChecksumHash
		checksumSHA256 = &h
	}

	return &BackupArtifactResponse{
		ID:                 a.ID,
		RunID:              a.RunID,
		ResourceID:         a.ResourceID,
		ArtifactName:       SafeArtifactFilenameWithType(a.TargetName, a.Format, a.ArtifactType, a.ID),
		SizeBytes:          sizeBytes,
		ChecksumSHA256:     checksumSHA256,
		CompressionType:    compType,
		VerificationStatus: a.VerificationStatus,
		VerifiedAt:         a.VerifiedAt,
		LogicalSizeBytes:   logicalSizeBytes,
		CreatedAt:          a.CreatedAt,
	}
}

// SafeArtifactFilenameWithType generates a deterministic, safe, logical filename for a backup artifact with type awareness.
func SafeArtifactFilenameWithType(targetName string, format domain.ArtifactFormat, artType domain.ArtifactType, artifactID uuid.UUID) string {
	return domain.SafeArtifactFilenameWithType(targetName, format, artType, artifactID)
}

// SafeArtifactFilename generates a deterministic, safe, logical filename for a backup artifact.
func SafeArtifactFilename(targetName string, format domain.ArtifactFormat, artifactID uuid.UUID) string {
	return domain.SafeArtifactFilename(targetName, format, artifactID)
}

// VerificationDetails defines the strict 4-field public details structure matching Section 14.5 of docs/API_DESIGN.md.
type VerificationDetails struct {
	ChecksumMatched      bool   `json:"checksum_matched"`
	ArchiveIntegrity     string `json:"archive_integrity"`
	CompressionValid     bool   `json:"compression_valid"`
	ExtractedSampleCheck string `json:"extracted_sample_check"`
}

// VerifyBackupRunResponse defines the JSON response returned for on-demand backup run verification.
type VerifyBackupRunResponse struct {
	RunID              uuid.UUID                 `json:"run_id"`
	VerificationStatus domain.VerificationStatus `json:"verification_status"`
	VerifiedAt         time.Time                 `json:"verified_at"`
	Details            VerificationDetails       `json:"details"`
}

func toVerifyBackupRunResponse(res *service.RunVerificationResult) *VerifyBackupRunResponse {
	if res == nil {
		return nil
	}
	return &VerifyBackupRunResponse{
		RunID:              res.RunID,
		VerificationStatus: res.VerificationStatus,
		VerifiedAt:         res.VerifiedAt,
		Details: VerificationDetails{
			ChecksumMatched:      res.Details.ChecksumMatched,
			ArchiveIntegrity:     res.Details.ArchiveIntegrity,
			CompressionValid:     res.Details.CompressionValid,
			ExtractedSampleCheck: res.Details.ExtractedSampleCheck,
		},
	}
}

// S3TargetConfigDTO defines the S3 target configuration in API requests and responses.
type S3TargetConfigDTO struct {
	Bucket         string `json:"bucket"`
	Endpoint       string `json:"endpoint"`
	Region         string `json:"region"`
	ForcePathStyle bool   `json:"force_path_style"`
}

// CreateStorageTargetRequest defines the JSON payload for creating a storage target.
type CreateStorageTargetRequest struct {
	Name         string                   `json:"name"`
	Type         domain.StorageTargetType `json:"type"`
	S3Config     *S3TargetConfigDTO       `json:"s3_config,omitempty"`
	CredentialID *uuid.UUID               `json:"credential_id,omitempty"`
}

// UpdateStorageTargetRequest defines the JSON payload for updating a storage target.
type UpdateStorageTargetRequest struct {
	Name         *string                     `json:"name,omitempty"`
	S3Config     *S3TargetConfigDTO          `json:"s3_config,omitempty"`
	CredentialID *uuid.UUID                  `json:"credential_id,omitempty"`
	Status       *domain.StorageTargetStatus `json:"status,omitempty"`
}

// StorageTargetResponse defines the JSON response returned for a storage target.
type StorageTargetResponse struct {
	ID           uuid.UUID                  `json:"id"`
	Name         string                     `json:"name"`
	Type         domain.StorageTargetType   `json:"type"`
	Status       domain.StorageTargetStatus `json:"status"`
	IsDefault    bool                       `json:"is_default"`
	S3Config     *S3TargetConfigDTO         `json:"s3_config,omitempty"`
	CredentialID *uuid.UUID                 `json:"credential_id,omitempty"`
	CreatedAt    time.Time                  `json:"created_at"`
	UpdatedAt    time.Time                  `json:"updated_at"`
}

func toStorageTargetResponse(t *domain.StorageTarget) *StorageTargetResponse {
	if t == nil {
		return nil
	}
	resp := &StorageTargetResponse{
		ID:           t.ID,
		Name:         t.Name,
		Type:         t.Type,
		Status:       t.Status,
		IsDefault:    t.IsDefault,
		CredentialID: t.CredentialID,
		CreatedAt:    t.CreatedAt,
		UpdatedAt:    t.UpdatedAt,
	}
	if t.Type == domain.StorageTargetTypeS3 {
		s3Cfg, _ := domain.ParseS3TargetConfig(t.Config)
		if s3Cfg != nil {
			resp.S3Config = &S3TargetConfigDTO{
				Bucket:         s3Cfg.Bucket,
				Endpoint:       s3Cfg.Endpoint,
				Region:         s3Cfg.Region,
				ForcePathStyle: s3Cfg.ForcePathStyle,
			}
		}
	}
	return resp
}
