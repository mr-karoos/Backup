package domain

import "errors"

var (
	// ErrInvalidResourceName indicates the resource name violates length or formatting rules.
	ErrInvalidResourceName = errors.New("invalid resource name")

	// ErrInvalidResourceType indicates an unknown or unsupported resource type.
	ErrInvalidResourceType = errors.New("invalid resource type")

	// ErrInvalidResourceStatus indicates an invalid resource status transition or value.
	ErrInvalidResourceStatus = errors.New("invalid resource status")

	// ErrInvalidConnectorHost indicates an invalid connector host format or length.
	ErrInvalidConnectorHost = errors.New("invalid connector host")

	// ErrInvalidConnectorPort indicates a port outside the valid TCP range 1-65535.
	ErrInvalidConnectorPort = errors.New("invalid connector port")

	// ErrInvalidConnectorUsername indicates an invalid connector username.
	ErrInvalidConnectorUsername = errors.New("invalid connector username")

	// ErrInvalidAuthType indicates an unknown or incompatible authentication type.
	ErrInvalidAuthType = errors.New("invalid authentication type")

	// ErrInvalidHostKeyFingerprint indicates a malformed SSH host key fingerprint.
	ErrInvalidHostKeyFingerprint = errors.New("invalid host key fingerprint")

	// ErrInvalidConnectionTimeout indicates an invalid connection timeout value.
	ErrInvalidConnectionTimeout = errors.New("invalid connection timeout")

	// ErrInvalidConnectorConfig indicates unsupported or incompatible connector configuration options for the resource type.
	ErrInvalidConnectorConfig = errors.New("invalid connector config")

	// ErrInvalidCredentialReference indicates the referenced credential was not found or is incompatible.
	ErrInvalidCredentialReference = errors.New("invalid credential reference")

	// ErrResourceNotFound indicates the requested resource does not exist in the tenant organization.
	ErrResourceNotFound = errors.New("resource not found")

	// ErrResourceArchived indicates the resource is in an archived state and cannot be modified.
	ErrResourceArchived = errors.New("resource is archived")

	// ErrResourceConflict indicates a logical conflict, such as a duplicate 1:1 connector constraint.
	ErrResourceConflict = errors.New("resource conflict")

	// ErrResourceServiceUnavailable indicates a database or infrastructure failure during resource operations.
	ErrResourceServiceUnavailable = errors.New("resource service unavailable")

	// ErrCorruptResourceData indicates stored resource or connector data violates domain integrity rules.
	ErrCorruptResourceData = errors.New("corrupted resource data in storage")
)
