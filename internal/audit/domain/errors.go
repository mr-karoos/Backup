package domain

import "errors"

var (
	// ErrAuditServiceUnavailable indicates that the audit persistence or database subsystem is temporarily unavailable.
	ErrAuditServiceUnavailable = errors.New("audit service is temporarily unavailable")

	// ErrUnauthorizedRole indicates that the user role does not possess the required audit permissions.
	ErrUnauthorizedRole = errors.New("unauthorized role for audit operation")

	// ErrInvalidAuditFilter indicates that the provided audit filter parameters fail validation.
	ErrInvalidAuditFilter = errors.New("invalid audit log filter parameters")
)
