package domain

import (
	"time"

	"backup-platform/pkg/uuid"
)

// ConnectorType represents the adapter type for the connector.
type ConnectorType string

const (
	ConnectorTypeUbuntuSSH ConnectorType = "ubuntu_ssh"
	ConnectorTypeCPanel    ConnectorType = "cpanel"
)

// AuthType represents the authentication mechanism for connecting to the resource.
type AuthType string

const (
	AuthTypeSSHKey         AuthType = "ssh_key"
	AuthTypeSSHPassword    AuthType = "ssh_password"
	AuthTypeCPanelAPIToken AuthType = "cpanel_api_token"
	AuthTypeCPanelPassword AuthType = "cpanel_password"
)

// ConnectorConfig represents the typed JSONB config stored in resource_connectors.config.
type ConnectorConfig struct {
	Username                 string `json:"username"`
	ConnectionTimeoutSeconds *int   `json:"connection_timeout_seconds,omitempty"`
	UseHTTPS                 *bool  `json:"use_https,omitempty"`
}

// ResourceConnector represents the 1:1 connection adapter configuration for a resource.
type ResourceConnector struct {
	ID                 uuid.UUID
	OrganizationID     uuid.UUID
	ResourceID         uuid.UUID
	ConnectorType      ConnectorType
	CredentialID       uuid.UUID
	Host               string
	Port               int
	AuthType           AuthType
	HostKeyFingerprint *string
	Config             ConnectorConfig
	CreatedAt          time.Time
	UpdatedAt          time.Time
}
