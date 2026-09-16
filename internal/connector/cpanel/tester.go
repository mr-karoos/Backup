package cpanel

import (
	"context"
	"net/http"

	"backup-platform/internal/connector"
	"backup-platform/internal/credential/payload"
)

// CPanelConnectionTester tests connectivity and credentials against cPanel UAPI over strict HTTPS.
type CPanelConnectionTester struct {
	client HTTPDoer
}

// NewCPanelConnectionTester constructs a production cPanel connection tester with strict TLS and disabled redirects.
func NewCPanelConnectionTester(client HTTPDoer) *CPanelConnectionTester {
	if client == nil {
		client = &http.Client{
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
	}
	return &CPanelConnectionTester{
		client: client,
	}
}

// TestConnection executes an authenticated live probe to cPanel UAPI Variables/get_user_information.
func (t *CPanelConnectionTester) TestConnection(
	ctx context.Context,
	target connector.Target,
	credPayload *payload.PayloadV1,
) (*connector.ProbeResult, error) {
	body, latency, checkedAt, authMethodName, probeRes, err := executeUAPIRequest(
		ctx,
		t.client,
		target,
		credPayload,
		"/execute/Variables/get_user_information",
	)
	if err != nil {
		return nil, err
	}
	if probeRes != nil {
		return probeRes, nil
	}
	defer clear(body)

	norm, err := parseNormalizedUAPIResponse(body)
	if err != nil {
		return &connector.ProbeResult{
			Success:     false,
			Latency:     latency,
			CheckedAt:   checkedAt,
			FailureKind: connector.FailureKindRemoteAPIFailed,
			SafeReason:  "remote connection failed",
		}, nil
	}

	// UAPI status 1 indicates API execution success. Status != 1 is a remote API failure.
	if norm.Status != 1 {
		return &connector.ProbeResult{
			Success:     false,
			Latency:     latency,
			CheckedAt:   checkedAt,
			FailureKind: connector.FailureKindRemoteAPIFailed,
			SafeReason:  "remote service did not accept the connection",
		}, nil
	}

	details := map[string]any{
		"auth_method": authMethodName,
	}
	if norm.APIVersion > 0 {
		details["api_version"] = norm.APIVersion
	}

	return &connector.ProbeResult{
		Success:     true,
		Latency:     latency,
		CheckedAt:   checkedAt,
		FailureKind: connector.FailureKindNone,
		SafeReason:  "",
		Details:     details,
	}, nil
}
