package sshconn

import (
	"context"
	"errors"
	"net"
	"strings"
	"time"
	"unicode/utf8"

	"backup-platform/internal/connector"
	"backup-platform/internal/credential/payload"
)

const (
	defaultConnectionTimeout = 15 * time.Second
	maxBannerLength          = 255
)

type hostKeyMismatchError struct {
	expected string
	actual   string
}

func (e *hostKeyMismatchError) Error() string {
	return "ssh: host key fingerprint mismatch"
}

// DialerFunc abstracts network dialing for testing.
type DialerFunc func(ctx context.Context, network, addr string) (net.Conn, error)

// SSHConnectionTester probes remote SSH servers and validates credentials and host keys.
type SSHConnectionTester struct {
	dialer DialerFunc
}

// NewSSHConnectionTester constructs an operational SSH connection tester.
func NewSSHConnectionTester(dialer DialerFunc) *SSHConnectionTester {
	if dialer == nil {
		dialer = (&net.Dialer{}).DialContext
	}
	return &SSHConnectionTester{
		dialer: dialer,
	}
}

// TestConnection executes an end-to-end SSH handshake probe without executing remote commands.
func (t *SSHConnectionTester) TestConnection(
	ctx context.Context,
	target connector.Target,
	credPayload *payload.PayloadV1,
) (*connector.ProbeResult, error) {
	client, probeRes, err := dialAuthenticatedSSHClient(ctx, t.dialer, target, credPayload)
	if err != nil {
		return nil, err
	}
	if probeRes != nil {
		return probeRes, nil
	}
	defer client.Close()

	return &connector.ProbeResult{
		Success:     true,
		Latency:     client.Latency,
		CheckedAt:   client.CheckedAt,
		FailureKind: connector.FailureKindNone,
		SafeReason:  "",
		Details: map[string]any{
			"server_banner": client.Banner,
			"auth_method":   client.AuthType,
		},
	}, nil
}

// SanitizeBanner ensures server banners contain valid printable UTF-8, no control characters, and max 255 runes.
func SanitizeBanner(raw string) string {
	if !utf8.ValidString(raw) {
		raw = strings.ToValidUTF8(raw, "")
	}

	var sb strings.Builder
	for _, r := range raw {
		// Strip ASCII control characters (0-31 and 127) except standard whitespace
		if r < 32 || r == 127 {
			continue
		}
		sb.WriteRune(r)
	}

	sanitized := strings.TrimSpace(sb.String())
	runes := []rune(sanitized)
	if len(runes) > maxBannerLength {
		sanitized = string(runes[:maxBannerLength])
	}
	return sanitized
}

func isNetTimeout(err error) bool {
	if err == nil {
		return false
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, "i/o timeout") || strings.Contains(errStr, "deadline exceeded")
}
