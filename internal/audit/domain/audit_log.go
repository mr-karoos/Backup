package domain

import (
	"encoding/json"
	"time"

	"backup-platform/pkg/uuid"
)

// Standard Audit Log Action constants.
const (
	ActionBackupDownload   = "backup.download"
	ActionBackupDelete     = "backup.delete"
	ActionRetentionCleanup = "retention.cleanup"
	ActionAuthLoginSuccess = "auth.login.success"
	ActionAuthLoginFailed  = "auth.login.failed"
	ActionAuthLogout       = "auth.logout"
	ActionResourceCreate   = "resource.create"
	ActionResourceUpdate   = "resource.update"
	ActionResourceArchive  = "resource.archive"
	ActionCredentialCreate = "credential.create"
	ActionCredentialUpdate = "credential.update"
	ActionCredentialDelete = "credential.delete"
	ActionPlanUpdate       = "backup_plan.update"
)

// Standard Audit Log EntityType constants.
const (
	EntityTypeBackupArtifact = "backup_artifact"
	EntityTypeBackupRun      = "backup_run"
	EntityTypeBackupJob      = "backup_job"
	EntityTypeBackupPlan     = "backup_plan"
	EntityTypeResource       = "resource"
	EntityTypeCredential     = "credential"
	EntityTypeUser           = "user"
	EntityTypeOrganization   = "organization"
	EntityTypeStorageTarget  = "storage_target"
	EntityTypeSystem         = "system"
)

// AuditLog represents an append-oriented immutable audit log event.
type AuditLog struct {
	ID             uuid.UUID
	OrganizationID *uuid.UUID
	UserID         *uuid.UUID
	Action         string
	EntityType     string
	EntityID       *uuid.UUID
	IPAddress      *string
	UserAgent      *string
	Metadata       json.RawMessage
	CreatedAt      time.Time
}
