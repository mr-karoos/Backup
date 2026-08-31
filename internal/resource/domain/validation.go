package domain

import (
	"net"
	"strings"
	"unicode/utf8"

	credDomain "backup-platform/internal/credential/domain"
)

// ValidateResourceName validates and normalizes a resource name.
func ValidateResourceName(name string) (string, error) {
	trimmed := strings.TrimSpace(name)
	runeCount := utf8.RuneCountInString(trimmed)
	if runeCount < 1 || runeCount > 100 {
		return "", ErrInvalidResourceName
	}
	return trimmed, nil
}

// ValidateResourceType ensures the resource type is supported.
func ValidateResourceType(t Type) error {
	switch t {
	case TypeUbuntuSSH, TypeCPanel:
		return nil
	default:
		return ErrInvalidResourceType
	}
}

// ValidateResourceStatus ensures the resource status is valid.
func ValidateResourceStatus(s Status) error {
	switch s {
	case StatusActive, StatusUnreachable, StatusDisabled, StatusError, StatusArchived:
		return nil
	default:
		return ErrInvalidResourceStatus
	}
}

// ValidateConnectorHost validates and normalizes a connector host (IPv4, IPv6, or DNS hostname).
// IPv6 addresses in bracketed format are canonicalized to unbracketed format.
// DNS hostnames are canonicalized to lowercase.
func ValidateConnectorHost(host string) (string, error) {
	trimmed := strings.TrimSpace(host)
	if len(trimmed) < 1 || len(trimmed) > 255 {
		return "", ErrInvalidConnectorHost
	}

	// Reject whitespace, URL schemes, forward/backward slashes, query/fragment/auth characters, underscores, and consecutive dots
	if strings.ContainsAny(trimmed, " \t\r\n/\\@?#_") ||
		strings.Contains(trimmed, "://") ||
		strings.Contains(trimmed, "..") {
		return "", ErrInvalidConnectorHost
	}

	// 1. Bracketed IPv6 (e.g. [2001:db8::1]) -> canonicalize to unbracketed form
	if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
		inner := trimmed[1 : len(trimmed)-1]
		if !strings.Contains(inner, ":") {
			return "", ErrInvalidConnectorHost
		}
		ip := net.ParseIP(inner)
		if ip == nil {
			return "", ErrInvalidConnectorHost
		}
		return ip.String(), nil
	}

	// 2. Unbracketed IP (IPv4 or IPv6)
	if ip := net.ParseIP(trimmed); ip != nil {
		return ip.String(), nil
	}

	// 3. Reject host:port syntax (e.g., example.com:22, 192.168.1.1:22)
	if strings.Contains(trimmed, ":") {
		return "", ErrInvalidConnectorHost
	}

	// 4. DNS Hostname validation (RFC 1123)
	if strings.HasPrefix(trimmed, ".") || strings.HasSuffix(trimmed, ".") ||
		strings.HasPrefix(trimmed, "-") || strings.HasSuffix(trimmed, "-") {
		return "", ErrInvalidConnectorHost
	}

	labels := strings.Split(trimmed, ".")
	for _, label := range labels {
		if len(label) < 1 || len(label) > 63 {
			return "", ErrInvalidConnectorHost
		}
		if strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return "", ErrInvalidConnectorHost
		}
		for i := 0; i < len(label); i++ {
			b := label[i]
			isAlpha := (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
			isDigit := (b >= '0' && b <= '9')
			isHyphen := (b == '-')
			if !isAlpha && !isDigit && !isHyphen {
				return "", ErrInvalidConnectorHost
			}
		}
	}

	return strings.ToLower(trimmed), nil
}

// ValidateConnectorPort validates that the port is within the valid TCP range 1-65535.
func ValidateConnectorPort(port int) error {
	if port < 1 || port > 65535 {
		return ErrInvalidConnectorPort
	}
	return nil
}

// ValidateUsername validates and normalizes a connector username.
func ValidateUsername(username string) (string, error) {
	trimmed := strings.TrimSpace(username)
	runeCount := utf8.RuneCountInString(trimmed)
	if runeCount < 1 || runeCount > 255 {
		return "", ErrInvalidConnectorUsername
	}
	return trimmed, nil
}

// ValidateAuthType ensures the auth type is compatible with the resource type.
func ValidateAuthType(authType AuthType, resourceType Type) error {
	switch resourceType {
	case TypeUbuntuSSH:
		if authType == AuthTypeSSHKey || authType == AuthTypeSSHPassword {
			return nil
		}
	case TypeCPanel:
		if authType == AuthTypeCPanelAPIToken || authType == AuthTypeCPanelPassword {
			return nil
		}
	}
	return ErrInvalidAuthType
}

// ValidateHostKeyFingerprint validates an optional SSH host key fingerprint.
func ValidateHostKeyFingerprint(fp *string, resourceType Type) (*string, error) {
	if resourceType == TypeCPanel {
		if fp != nil && strings.TrimSpace(*fp) != "" {
			return nil, ErrInvalidHostKeyFingerprint
		}
		return nil, nil
	}

	if fp == nil {
		return nil, nil
	}

	trimmed := strings.TrimSpace(*fp)
	if trimmed == "" {
		return nil, nil
	}

	if len(trimmed) > 255 {
		return nil, ErrInvalidHostKeyFingerprint
	}

	if !strings.HasPrefix(trimmed, "SHA256:") || len(trimmed) <= len("SHA256:") {
		return nil, ErrInvalidHostKeyFingerprint
	}

	return &trimmed, nil
}

// ValidateConnectionTimeout validates an optional connection timeout in seconds (1 to 300).
func ValidateConnectionTimeout(timeout *int) error {
	if timeout == nil {
		return nil
	}
	if *timeout < 1 || *timeout > 300 {
		return ErrInvalidConnectionTimeout
	}
	return nil
}

// ValidateConnectorConfig validates connector configuration attributes based on the resource type.
// For ubuntu_ssh, use_https is strictly unsupported and must be nil.
// For cpanel, use_https is optional.
func ValidateConnectorConfig(resType Type, username string, timeout *int, useHTTPS *bool) (*ConnectorConfig, error) {
	validUsername, err := ValidateUsername(username)
	if err != nil {
		return nil, err
	}

	if err := ValidateConnectionTimeout(timeout); err != nil {
		return nil, err
	}

	switch resType {
	case TypeUbuntuSSH:
		if useHTTPS != nil {
			return nil, ErrInvalidConnectorConfig
		}
		return &ConnectorConfig{
			Username:                 validUsername,
			ConnectionTimeoutSeconds: timeout,
			UseHTTPS:                 nil,
		}, nil

	case TypeCPanel:
		return &ConnectorConfig{
			Username:                 validUsername,
			ConnectionTimeoutSeconds: timeout,
			UseHTTPS:                 useHTTPS,
		}, nil

	default:
		return nil, ErrInvalidResourceType
	}
}

// ValidateCredentialTypeCompatibility ensures the referenced credential's type matches the connector's auth type.
func ValidateCredentialTypeCompatibility(authType AuthType, credType credDomain.Type) error {
	switch authType {
	case AuthTypeSSHKey:
		if credType != credDomain.TypeSSHPrivateKey {
			return ErrInvalidCredentialReference
		}
	case AuthTypeSSHPassword:
		if credType != credDomain.TypeSSHPassword {
			return ErrInvalidCredentialReference
		}
	case AuthTypeCPanelAPIToken:
		if credType != credDomain.TypeCPanelAPIToken {
			return ErrInvalidCredentialReference
		}
	case AuthTypeCPanelPassword:
		if credType != credDomain.TypeCPanelPassword {
			return ErrInvalidCredentialReference
		}
	default:
		return ErrInvalidCredentialReference
	}
	return nil
}

// ValidateConnectorType ensures the connector type is strictly compatible with the resource type.
func ValidateConnectorType(resourceType Type, connectorType ConnectorType) error {
	switch resourceType {
	case TypeUbuntuSSH:
		if connectorType == ConnectorTypeUbuntuSSH {
			return nil
		}
	case TypeCPanel:
		if connectorType == ConnectorTypeCPanel {
			return nil
		}
	}
	return ErrInvalidResourceType
}

// ValidateCPanelOperationalUsername validates preflight and runtime operational constraints for cPanel usernames.
// Rules: non-empty, lowercase only, no colon (:), no carriage return (\r), no line feed (\n), no NUL (\x00).
func ValidateCPanelOperationalUsername(username string) error {
	if len(username) == 0 {
		return ErrInvalidConnectorConfig
	}
	if username != strings.ToLower(username) {
		return ErrInvalidConnectorConfig
	}
	if strings.ContainsAny(username, "\r\n\x00:") {
		return ErrInvalidConnectorConfig
	}
	return nil
}
