package connector

import (
	"context"
	"time"

	"backup-platform/internal/credential/payload"
	resDomain "backup-platform/internal/resource/domain"
	"backup-platform/pkg/uuid"
)

// FailureKind represents internal taxonomy for expected remote connection and authentication failures.
type FailureKind string

const (
	FailureKindNone                  FailureKind = ""
	FailureKindTimeout               FailureKind = "timeout"
	FailureKindConnFailed            FailureKind = "connection_failed"
	FailureKindAuthFailed            FailureKind = "authentication_failed"
	FailureKindHostKeyMismatch       FailureKind = "host_key_mismatch"
	FailureKindTLSVerificationFailed FailureKind = "tls_verification_failed"
	FailureKindRemoteAPIFailed       FailureKind = "remote_api_failed"
)

// Target contains the operational connection configuration parameters for a remote resource.
type Target struct {
	ResourceID         uuid.UUID
	OrganizationID     uuid.UUID
	ResourceType       resDomain.Type
	Host               string
	Port               int
	AuthType           resDomain.AuthType
	Username           string
	HostKeyFingerprint *string
	ConnectionTimeout  *int
	UseHTTPS           *bool
}

// ProbeResult encapsulates the outcome of a live operational connection test.
type ProbeResult struct {
	Success     bool
	Latency     time.Duration
	CheckedAt   time.Time
	FailureKind FailureKind
	SafeReason  string
	Details     map[string]any
}

// ConnectionTester defines the capability interface for operational connection probing.
type ConnectionTester interface {
	TestConnection(ctx context.Context, target Target, credPayload *payload.PayloadV1) (*ProbeResult, error)
}
