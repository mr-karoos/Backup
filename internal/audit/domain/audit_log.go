package domain

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"backup-platform/pkg/uuid"
)

// Standard Audit Log Action constants.
const (
	ActionBackupDownload       = "backup.download"
	ActionBackupDelete         = "backup.delete"
	ActionRetentionCleanup     = "retention.cleanup"
	ActionAuthLoginSuccess     = "auth.login.success"
	ActionAuthLoginFailed      = "auth.login.failed"
	ActionAuthLogout           = "auth.logout"
	ActionResourceCreate       = "resource.create"
	ActionResourceUpdate       = "resource.update"
	ActionResourceArchive      = "resource.archive"
	ActionCredentialCreate     = "credential.create"
	ActionCredentialUpdate     = "credential.update"
	ActionCredentialDelete     = "credential.delete"
	ActionPlanUpdate           = "backup_plan.update"
	ActionMaintenancePrune     = "maintenance.prune"
	ActionMaintenanceCheck     = "maintenance.check"
	ActionMaintenancePruneFail = "maintenance.prune.failed"
	ActionMaintenanceCheckFail = "maintenance.check.failed"
)

// Standard Audit Log EntityType constants.
const (
	EntityTypeBackupArtifact   = "backup_artifact"
	EntityTypeBackupRun        = "backup_run"
	EntityTypeBackupJob        = "backup_job"
	EntityTypeBackupPlan       = "backup_plan"
	EntityTypeResource         = "resource"
	EntityTypeCredential       = "credential"
	EntityTypeUser             = "user"
	EntityTypeOrganization     = "organization"
	EntityTypeStorageTarget    = "storage_target"
	EntityTypeBackupRepository = "backup_repository"
	EntityTypeSystem           = "system"
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

// AuditLogFilter defines query criteria and cursor parameters for listing audit logs.
type AuditLogFilter struct {
	Limit           int
	CursorCreatedAt *time.Time
	CursorID        *uuid.UUID
	Action          *string
	EntityType      *string
	EntityID        *uuid.UUID
	UserID          *uuid.UUID
	From            *time.Time
	To              *time.Time
}

// AuditLogListResult encapsulates a paginated page of audit log entries.
type AuditLogListResult struct {
	Logs       []*AuditLog
	NextCursor *string
	HasMore    bool
}

// EncodeAuditCursor serializes created_at and id into a URL-safe base64 opaque cursor string.
func EncodeAuditCursor(t time.Time, id uuid.UUID) string {
	raw := fmt.Sprintf("%s,%s", t.UTC().Format(time.RFC3339Nano), id.String())
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

// DecodeAuditCursor parses a canonical URL-safe base64 opaque cursor string into created_at and id.
func DecodeAuditCursor(cursor string) (time.Time, uuid.UUID, error) {
	if cursor == "" {
		return time.Time{}, uuid.Nil, fmt.Errorf("cursor is empty")
	}

	// Reject any padding characters ('=') or standard base64 characters ('+', '/') or whitespace
	if strings.ContainsAny(cursor, "=+/ \t\r\n") {
		return time.Time{}, uuid.Nil, fmt.Errorf("non-canonical base64 cursor")
	}

	decoded, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return time.Time{}, uuid.Nil, fmt.Errorf("malformed base64 cursor: %w", err)
	}

	parts := strings.Split(string(decoded), ",")
	if len(parts) != 2 {
		return time.Time{}, uuid.Nil, fmt.Errorf("invalid cursor components: expected exactly two")
	}

	t, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return time.Time{}, uuid.Nil, fmt.Errorf("invalid cursor timestamp format: %w", err)
	}

	id, err := uuid.Parse(parts[1])
	if err != nil || id == uuid.Nil {
		return time.Time{}, uuid.Nil, fmt.Errorf("invalid cursor UUID: %w", err)
	}

	// Canonical re-encode verification: ensures strict round-trip identity and rejects alternate encodings
	canonical := EncodeAuditCursor(t, id)
	if canonical != cursor {
		return time.Time{}, uuid.Nil, fmt.Errorf("cursor is not in canonical form")
	}

	return t, id, nil
}
