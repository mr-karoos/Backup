package httpapi

import (
	"time"

	"backup-platform/internal/audit/domain"
	"backup-platform/pkg/uuid"
)

// AuditLogDTO represents the safe, public DTO for an audit log event.
// It explicitly excludes internal metadata, secrets, and organization identifiers.
type AuditLogDTO struct {
	ID         uuid.UUID  `json:"id"`
	UserID     *uuid.UUID `json:"user_id"`
	Action     string     `json:"action"`
	EntityType string     `json:"entity_type"`
	EntityID   *uuid.UUID `json:"entity_id"`
	IPAddress  *string    `json:"ip_address"`
	UserAgent  *string    `json:"user_agent"`
	CreatedAt  time.Time  `json:"created_at"`
}

// PaginationMeta encapsulates keyset cursor pagination metadata.
type PaginationMeta struct {
	NextCursor *string `json:"next_cursor"`
	HasMore    bool    `json:"has_more"`
}

// AuditLogListResponse represents the top-level response envelope for paginated audit logs.
type AuditLogListResponse struct {
	Data      []AuditLogDTO  `json:"data"`
	Page      PaginationMeta `json:"page"`
	Message   string         `json:"message,omitempty"`
	RequestID string         `json:"request_id,omitempty"`
}

// ToAuditLogDTO transforms a domain AuditLog into a safe public DTO.
func ToAuditLogDTO(entry *domain.AuditLog) AuditLogDTO {
	if entry == nil {
		return AuditLogDTO{}
	}
	return AuditLogDTO{
		ID:         entry.ID,
		UserID:     entry.UserID,
		Action:     entry.Action,
		EntityType: entry.EntityType,
		EntityID:   entry.EntityID,
		IPAddress:  entry.IPAddress,
		UserAgent:  entry.UserAgent,
		CreatedAt:  entry.CreatedAt,
	}
}
