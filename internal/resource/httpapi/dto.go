package httpapi

import (
	"time"

	orgDomain "backup-platform/internal/organization/domain"
	"backup-platform/internal/resource/domain"
)

// ConnectorConfigRequest represents optional configuration parameters in connector requests.
type ConnectorConfigRequest struct {
	ConnectionTimeoutSeconds *int  `json:"connection_timeout_seconds,omitempty"`
	UseHTTPS                 *bool `json:"use_https,omitempty"`
}

// CreateConnectorRequest represents the input schema for connector definition.
type CreateConnectorRequest struct {
	Host               string                  `json:"host"`
	Port               int                     `json:"port"`
	AuthType           string                  `json:"auth_type"`
	Username           string                  `json:"username"`
	CredentialID       string                  `json:"credential_id"`
	HostKeyFingerprint *string                 `json:"host_key_fingerprint,omitempty"`
	Config             *ConnectorConfigRequest `json:"config,omitempty"`
}

// CreateResourceRequest represents the strict input schema for creating a resource.
type CreateResourceRequest struct {
	Name      *string                 `json:"name"`
	Type      *string                 `json:"type"`
	Connector *CreateConnectorRequest `json:"connector"`
}

// UpdateResourceRequest represents the strict input schema for replacing a resource.
type UpdateResourceRequest struct {
	Name      *string                 `json:"name"`
	Connector *CreateConnectorRequest `json:"connector"`
}

// ResourceCreateResponse represents the safe response schema for a newly created resource.
type ResourceCreateResponse struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Type      string    `json:"type"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

// ResourceUpdateResponse represents the safe response schema for an updated resource.
type ResourceUpdateResponse struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Type      string    `json:"type"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ConnectorResponse represents connector details formatted according to role visibility.
type ConnectorResponse struct {
	Host               string  `json:"host"`
	Port               int     `json:"port"`
	AuthType           string  `json:"auth_type,omitempty"`
	Username           string  `json:"username,omitempty"`
	HostKeyFingerprint *string `json:"host_key_fingerprint,omitempty"`
	CredentialID       string  `json:"credential_id,omitempty"`
	CredentialName     string  `json:"credential_name,omitempty"`
}

// ResourceResponse represents a full resource with role-filtered connector details.
type ResourceResponse struct {
	ID                   string             `json:"id"`
	Name                 string             `json:"name"`
	Type                 string             `json:"type"`
	Status               string             `json:"status"`
	LastConnectionTestAt *time.Time         `json:"last_connection_test_at"`
	LastConnectionStatus *string            `json:"last_connection_status"`
	Connector            *ConnectorResponse `json:"connector,omitempty"`
	CreatedAt            time.Time          `json:"created_at"`
}

// MapResourceToResponse converts a domain ResourceWithConnector into a role-scoped ResourceResponse.
func MapResourceToResponse(item *domain.ResourceWithConnector, role orgDomain.Role) ResourceResponse {
	resp := ResourceResponse{
		ID:                   item.Resource.ID.String(),
		Name:                 item.Resource.Name,
		Type:                 string(item.Resource.Type),
		Status:               string(item.Resource.Status),
		LastConnectionTestAt: item.Resource.LastConnectionTestAt,
		CreatedAt:            item.Resource.CreatedAt,
	}

	if item.Resource.LastConnectionStatus != nil {
		s := string(*item.Resource.LastConnectionStatus)
		resp.LastConnectionStatus = &s
	}

	switch role {
	case orgDomain.RoleAdmin:
		resp.Connector = &ConnectorResponse{
			Host:               item.Connector.Host,
			Port:               item.Connector.Port,
			AuthType:           string(item.Connector.AuthType),
			Username:           item.Connector.Config.Username,
			HostKeyFingerprint: item.Connector.HostKeyFingerprint,
			CredentialID:       item.Connector.CredentialID.String(),
			CredentialName:     item.CredentialName,
		}
	case orgDomain.RoleMember:
		resp.Connector = &ConnectorResponse{
			Host: item.Connector.Host,
			Port: item.Connector.Port,
		}
	case orgDomain.RoleViewer:
		resp.Connector = nil
	default:
		resp.Connector = nil
	}

	return resp
}

// ConnectionTestResponse defines the safe HTTP response data schema for a test connection operation.
type ConnectionTestResponse struct {
	Status    string         `json:"status"` // "success" | "failed"
	LatencyMS int64          `json:"latency_ms"`
	CheckedAt time.Time      `json:"checked_at"`
	Details   map[string]any `json:"details"`
}

// DiscoveredDatabaseResponse defines the safe HTTP response data schema for a discovered database.
type DiscoveredDatabaseResponse struct {
	Name        string `json:"name"`
	SizeBytes   int64  `json:"size_bytes"`
	TablesCount *int64 `json:"tables_count"` // Explicit pointer so null is preserved without omitempty
	Status      string `json:"status"`
}
