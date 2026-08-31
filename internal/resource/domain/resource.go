package domain

import (
	"encoding/json"
	"time"

	"backup-platform/pkg/uuid"
)

// Type represents the canonical target resource type.
type Type string

const (
	TypeUbuntuSSH Type = "ubuntu_ssh"
	TypeCPanel    Type = "cpanel"
)

// Status represents the operational and lifecycle status of a resource.
type Status string

const (
	StatusActive      Status = "active"
	StatusUnreachable Status = "unreachable"
	StatusDisabled    Status = "disabled"
	StatusError       Status = "error"
	StatusArchived    Status = "archived"
)

// ConnectionStatus represents the outcome of a connection health check.
type ConnectionStatus string

const (
	ConnectionStatusSuccess ConnectionStatus = "success"
	ConnectionStatusFailed  ConnectionStatus = "failed"
)

// Resource represents a target server, host, or environment within an organization.
type Resource struct {
	ID                   uuid.UUID
	OrganizationID       uuid.UUID
	Name                 string
	Type                 Type
	Status               Status
	LastConnectionTestAt *time.Time
	LastConnectionStatus *ConnectionStatus
	LastConnectionError  *string
	Metadata             json.RawMessage
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

// ResourceWithConnector aggregates a Resource, its 1:1 ResourceConnector, and safe Credential metadata.
type ResourceWithConnector struct {
	Resource              *Resource
	Connector             *ResourceConnector
	CredentialName        string
	CredentialFingerprint *string
}
