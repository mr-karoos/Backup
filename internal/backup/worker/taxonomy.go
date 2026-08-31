package worker

import (
	"context"
	"errors"
	"syscall"

	"backup-platform/internal/backup/domain"
	"backup-platform/internal/connector"
	credDomain "backup-platform/internal/credential/domain"
	resDomain "backup-platform/internal/resource/domain"
	"backup-platform/internal/storage"
)

// FailureKind represents the typed classification of backup execution errors.
type FailureKind string

const (
	FailureKindNetwork              FailureKind = "network"
	FailureKindTimeout              FailureKind = "timeout"
	FailureKindConnectionReset      FailureKind = "connection_reset"
	FailureKindTemporaryStorageIO   FailureKind = "temporary_storage_io"
	FailureKindPlatformDependency   FailureKind = "platform_dependency"
	FailureKindWorkerInterrupted    FailureKind = "worker_interrupted"
	FailureKindLeaseInterrupted     FailureKind = "lease_interrupted"
	FailureKindAuthentication       FailureKind = "authentication"
	FailureKindHostKeyMismatch      FailureKind = "host_key_mismatch"
	FailureKindInvalidCredential    FailureKind = "invalid_credential"
	FailureKindInvalidConfiguration FailureKind = "invalid_configuration"
	FailureKindUnsupportedResource  FailureKind = "unsupported_resource"
	FailureKindDumpCommandFailed    FailureKind = "dump_command_failed"
	FailureKindDumpToolMissing      FailureKind = "dump_tool_missing"
	FailureKindArchiveCommandFailed FailureKind = "archive_command_failed"
	FailureKindArchiveToolMissing   FailureKind = "archive_tool_missing"
	FailureKindDatabaseNotFound     FailureKind = "database_not_found"
	FailureKindStorageFull          FailureKind = "storage_full"
	FailureKindVerification         FailureKind = "verification"
	FailureKindInternal             FailureKind = "internal"
)

// IsRetryable returns whether the failure kind represents a transient, retryable error.
func (k FailureKind) IsRetryable() bool {
	switch k {
	case FailureKindNetwork,
		FailureKindTimeout,
		FailureKindConnectionReset,
		FailureKindTemporaryStorageIO,
		FailureKindPlatformDependency,
		FailureKindWorkerInterrupted,
		FailureKindLeaseInterrupted:
		return true
	default:
		return false
	}
}

// SafeMessage returns a business-safe, non-leaking diagnostic message for DB persistence and logging.
func (k FailureKind) SafeMessage() string {
	switch k {
	case FailureKindNetwork:
		return "network connection error with remote host"
	case FailureKindTimeout:
		return "remote connection timed out"
	case FailureKindConnectionReset:
		return "remote connection reset"
	case FailureKindTemporaryStorageIO:
		return "temporary storage I/O error"
	case FailureKindPlatformDependency:
		return "platform dependency temporarily unavailable"
	case FailureKindWorkerInterrupted:
		return "backup execution was interrupted by worker shutdown"
	case FailureKindLeaseInterrupted:
		return "backup worker lease expired"
	case FailureKindAuthentication:
		return "authentication failed with remote host"
	case FailureKindHostKeyMismatch:
		return "remote host key mismatch detected"
	case FailureKindInvalidCredential:
		return "invalid credential for resource"
	case FailureKindInvalidConfiguration:
		return "invalid resource configuration"
	case FailureKindUnsupportedResource:
		return "unsupported resource type for backup"
	case FailureKindDumpCommandFailed:
		return "database dump command exited with error"
	case FailureKindDumpToolMissing:
		return "database dump utility not found on remote system"
	case FailureKindArchiveCommandFailed:
		return "website files archive command exited with error"
	case FailureKindArchiveToolMissing:
		return "website archive utility not found on remote system"
	case FailureKindDatabaseNotFound:
		return "target database not found on remote system"
	case FailureKindStorageFull:
		return "storage target out of disk space"
	case FailureKindVerification:
		return "backup artifact failed integrity verification"
	case FailureKindInternal:
		fallthrough
	default:
		return "internal backup service failure"
	}
}

// ExecutionFailure represents a classified backup failure.
type ExecutionFailure struct {
	Kind    FailureKind
	Cause   error
	Message string
}

func (f *ExecutionFailure) Error() string {
	if f.Message != "" {
		return f.Message
	}
	return f.Kind.SafeMessage()
}

func (f *ExecutionFailure) Unwrap() error {
	return f.Cause
}

// NewExecutionFailure constructs an ExecutionFailure with the specified kind and underlying cause.
func NewExecutionFailure(kind FailureKind, cause error) *ExecutionFailure {
	return &ExecutionFailure{
		Kind:    kind,
		Cause:   cause,
		Message: kind.SafeMessage(),
	}
}

// ClassifyError maps any error encountered during backup execution into a typed ExecutionFailure.
func ClassifyError(err error) *ExecutionFailure {
	if err == nil {
		return nil
	}

	var ef *ExecutionFailure
	if errors.As(err, &ef) {
		return ef
	}

	// 1. Context and Interruptions
	if errors.Is(err, context.Canceled) {
		return NewExecutionFailure(FailureKindWorkerInterrupted, err)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return NewExecutionFailure(FailureKindTimeout, err)
	}

	// 2. Storage Errors
	if errors.Is(err, storage.ErrStorageFull) || errors.Is(err, syscall.ENOSPC) {
		return NewExecutionFailure(FailureKindStorageFull, err)
	}
	if errors.Is(err, storage.ErrStorageIO) {
		return NewExecutionFailure(FailureKindTemporaryStorageIO, err)
	}
	if errors.Is(err, storage.ErrArtifactCollision) || errors.Is(err, storage.ErrInvalidStorageReference) {
		return NewExecutionFailure(FailureKindInternal, err)
	}

	// 3. Credential Errors
	if errors.Is(err, credDomain.ErrCredentialNotFound) || errors.Is(err, credDomain.ErrCredentialSecretUnavailable) {
		return NewExecutionFailure(FailureKindInvalidCredential, err)
	}
	if errors.Is(err, credDomain.ErrCredentialServiceUnavailable) {
		return NewExecutionFailure(FailureKindPlatformDependency, err)
	}

	// 4. Verification Errors
	if errors.Is(err, domain.ErrVerificationFailed) {
		return NewExecutionFailure(FailureKindVerification, err)
	}

	// 5. Connector & Transport Errors
	if errors.Is(err, connector.ErrSSHTimeout) {
		return NewExecutionFailure(FailureKindTimeout, err)
	}
	if errors.Is(err, connector.ErrSSHNetwork) {
		return NewExecutionFailure(FailureKindNetwork, err)
	}
	if errors.Is(err, connector.ErrSSHAuthentication) {
		return NewExecutionFailure(FailureKindAuthentication, err)
	}
	if errors.Is(err, connector.ErrSSHHostKeyMismatch) {
		return NewExecutionFailure(FailureKindHostKeyMismatch, err)
	}
	if errors.Is(err, connector.ErrInvalidCredentialFormat) {
		return NewExecutionFailure(FailureKindInvalidCredential, err)
	}
	if errors.Is(err, connector.ErrDumpToolMissing) {
		return NewExecutionFailure(FailureKindDumpToolMissing, err)
	}
	if errors.Is(err, connector.ErrDumpCommandFailed) {
		return NewExecutionFailure(FailureKindDumpCommandFailed, err)
	}
	if errors.Is(err, connector.ErrArchiveToolMissing) {
		return NewExecutionFailure(FailureKindArchiveToolMissing, err)
	}
	if errors.Is(err, connector.ErrArchiveCommandFailed) {
		return NewExecutionFailure(FailureKindArchiveCommandFailed, err)
	}
	if errors.Is(err, connector.ErrRemoteCommandStderrOverflow) {
		return NewExecutionFailure(FailureKindInternal, err)
	}

	// 6. Resource & Configuration Errors
	if errors.Is(err, resDomain.ErrInvalidHostKeyFingerprint) ||
		errors.Is(err, resDomain.ErrCorruptResourceData) ||
		errors.Is(err, resDomain.ErrInvalidCredentialReference) ||
		errors.Is(err, domain.ErrResourceDisabled) ||
		errors.Is(err, domain.ErrResourceArchived) ||
		errors.Is(err, domain.ErrResourceNotFound) ||
		errors.Is(err, resDomain.ErrResourceNotFound) ||
		errors.Is(err, domain.ErrInvalidTargetSpec) ||
		errors.Is(err, connector.ErrInvalidFileBackupConfig) ||
		errors.Is(err, domain.ErrUnsupportedBackupType) {
		return NewExecutionFailure(FailureKindInvalidConfiguration, err)
	}
	if errors.Is(err, domain.ErrUnsupportedResourceType) || errors.Is(err, domain.ErrStorageTargetNotSupported) {
		return NewExecutionFailure(FailureKindUnsupportedResource, err)
	}

	// 7. Default unclassified error
	return NewExecutionFailure(FailureKindPlatformDependency, err)
}
