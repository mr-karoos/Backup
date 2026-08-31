package cpanel

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"backup-platform/internal/connector"
	"backup-platform/internal/credential/payload"
	resDomain "backup-platform/internal/resource/domain"
)

const (
	defaultConnectionTimeout = 15 * time.Second
	maxResponseBodyBytes     = 1 << 20 // 1 MiB max response limit
)

// HTTPDoer abstracts HTTP client operations for testing and production injection.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// executeUAPIRequest executes an authenticated HTTPS request against a cPanel UAPI endpoint with strict security invariants.
func executeUAPIRequest(
	ctx context.Context,
	client HTTPDoer,
	target connector.Target,
	credPayload *payload.PayloadV1,
	endpointPath string,
) ([]byte, time.Duration, time.Time, string, *connector.ProbeResult, error) {
	// 1. Mandatory HTTPS enforcement
	if target.UseHTTPS != nil && !*target.UseHTTPS {
		return nil, 0, time.Time{}, "", nil, resDomain.ErrInvalidConnectorConfig
	}

	// 2. Preflight username operational checks
	if err := resDomain.ValidateCPanelOperationalUsername(target.Username); err != nil {
		return nil, 0, time.Time{}, "", nil, err
	}

	// 3. Resolve Connection Timeout
	timeoutDuration := defaultConnectionTimeout
	if target.ConnectionTimeout != nil && *target.ConnectionTimeout > 0 {
		timeoutDuration = time.Duration(*target.ConnectionTimeout) * time.Second
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, timeoutDuration)
	defer cancel()

	// 4. Build UAPI endpoint URL
	u := url.URL{
		Scheme: "https",
		Host:   net.JoinHostPort(target.Host, strconv.Itoa(target.Port)),
		Path:   endpointPath,
	}

	req, err := http.NewRequestWithContext(timeoutCtx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, 0, time.Time{}, "", nil, fmt.Errorf("failed to construct cPanel HTTP request: %w", err)
	}

	// 5. Configure Authentication & Authorization Header
	var authMethodName string
	switch target.AuthType {
	case resDomain.AuthTypeCPanelAPIToken:
		authMethodName = "api_token"
		req.Header.Set("Authorization", fmt.Sprintf("cpanel %s:%s", target.Username, credPayload.Secret))

	case resDomain.AuthTypeCPanelPassword:
		authMethodName = "password"
		req.SetBasicAuth(target.Username, credPayload.Secret)

	default:
		return nil, 0, time.Time{}, "", nil, resDomain.ErrInvalidAuthType
	}

	// Defensive cleanup of Authorization header reference on all return paths
	defer req.Header.Del("Authorization")

	startTime := time.Now()

	// 6. Execute HTTP Request
	resp, err := client.Do(req)

	checkedAt := time.Now().UTC()
	latency := time.Since(startTime)

	if err != nil {
		// If caller parent context was canceled, return error directly (do not treat as remote probe timeout)
		if ctx.Err() != nil {
			return nil, latency, checkedAt, authMethodName, nil, ctx.Err()
		}

		if errors.Is(timeoutCtx.Err(), context.DeadlineExceeded) || isNetTimeout(err) {
			return nil, latency, checkedAt, authMethodName, &connector.ProbeResult{
				Success:     false,
				Latency:     latency,
				CheckedAt:   checkedAt,
				FailureKind: connector.FailureKindTimeout,
				SafeReason:  "connection timed out",
			}, nil
		}

		if isTLSCertVerificationError(err) {
			return nil, latency, checkedAt, authMethodName, &connector.ProbeResult{
				Success:     false,
				Latency:     latency,
				CheckedAt:   checkedAt,
				FailureKind: connector.FailureKindTLSVerificationFailed,
				SafeReason:  "TLS certificate verification failed",
			}, nil
		}

		return nil, latency, checkedAt, authMethodName, &connector.ProbeResult{
			Success:     false,
			Latency:     latency,
			CheckedAt:   checkedAt,
			FailureKind: connector.FailureKindConnFailed,
			SafeReason:  "connection failed",
		}, nil
	}
	defer resp.Body.Close()

	// 7. Handle HTTP Status Code Classification
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, latency, checkedAt, authMethodName, &connector.ProbeResult{
			Success:     false,
			Latency:     latency,
			CheckedAt:   checkedAt,
			FailureKind: connector.FailureKindAuthFailed,
			SafeReason:  "authentication failed",
		}, nil
	}

	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		// Redirect encountered (redirects are disabled in production)
		return nil, latency, checkedAt, authMethodName, &connector.ProbeResult{
			Success:     false,
			Latency:     latency,
			CheckedAt:   checkedAt,
			FailureKind: connector.FailureKindRemoteAPIFailed,
			SafeReason:  "remote connection failed",
		}, nil
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, latency, checkedAt, authMethodName, &connector.ProbeResult{
			Success:     false,
			Latency:     latency,
			CheckedAt:   checkedAt,
			FailureKind: connector.FailureKindRemoteAPIFailed,
			SafeReason:  "remote connection failed",
		}, nil
	}

	// 8. Bounded Body Read (Max 1 MiB)
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBodyBytes+1))
	if err != nil {
		return nil, latency, checkedAt, authMethodName, &connector.ProbeResult{
			Success:     false,
			Latency:     latency,
			CheckedAt:   checkedAt,
			FailureKind: connector.FailureKindRemoteAPIFailed,
			SafeReason:  "remote connection failed",
		}, nil
	}

	if len(body) > maxResponseBodyBytes {
		clear(body)
		return nil, latency, checkedAt, authMethodName, &connector.ProbeResult{
			Success:     false,
			Latency:     latency,
			CheckedAt:   checkedAt,
			FailureKind: connector.FailureKindRemoteAPIFailed,
			SafeReason:  "remote connection failed",
		}, nil
	}

	return body, latency, checkedAt, authMethodName, nil, nil
}

func isTLSCertVerificationError(err error) bool {
	if err == nil {
		return false
	}
	var (
		uaErr  x509.UnknownAuthorityError
		hnErr  x509.HostnameError
		ciErr  x509.CertificateInvalidError
		cvErr  x509.ConstraintViolationError
		tlsErr *tls.CertificateVerificationError
	)
	return errors.As(err, &uaErr) ||
		errors.As(err, &hnErr) ||
		errors.As(err, &ciErr) ||
		errors.As(err, &cvErr) ||
		errors.As(err, &tlsErr)
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
