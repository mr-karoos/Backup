package cpanel

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"backup-platform/internal/connector"
	"backup-platform/internal/credential/payload"
	resDomain "backup-platform/internal/resource/domain"
	"backup-platform/pkg/uuid"
)

func parseServerHostPort(t *testing.T, s *httptest.Server) (string, int) {
	t.Helper()
	u, err := url.Parse(s.URL)
	if err != nil {
		t.Fatalf("failed to parse test server url: %v", err)
	}
	host := u.Hostname()
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatalf("failed to parse port: %v", err)
	}
	return host, port
}

func TestCPanelConnectionTester_APITokenAuth_Success(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/execute/Variables/get_user_information" {
			http.NotFound(w, r)
			return
		}

		authHeader := r.Header.Get("Authorization")
		if authHeader != "cpanel mycpanel:SECRET-TOKEN-123" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"result":{"status":0}}`))
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"apiversion":3,"result":{"status":1}}`))
	}))
	defer server.Close()

	host, port := parseServerHostPort(t, server)

	tester := NewCPanelConnectionTester(server.Client())
	target := connector.Target{
		ResourceID:     uuid.New(),
		OrganizationID: uuid.New(),
		ResourceType:   resDomain.TypeCPanel,
		Host:           host,
		Port:           port,
		AuthType:       resDomain.AuthTypeCPanelAPIToken,
		Username:       "mycpanel",
	}

	credPayload := &payload.PayloadV1{
		Version: 1,
		Secret:  "SECRET-TOKEN-123",
	}

	result, err := tester.TestConnection(context.Background(), target, credPayload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Success {
		t.Fatalf("expected probe success, got failure: kind=%s, reason=%s", result.FailureKind, result.SafeReason)
	}
	if result.Details["auth_method"] != "api_token" {
		t.Errorf("expected auth_method 'api_token', got: %v", result.Details["auth_method"])
	}
	if result.Details["api_version"] != 3 {
		t.Errorf("expected api_version 3, got: %v", result.Details["api_version"])
	}
}

func TestCPanelConnectionTester_PasswordAuth_Success(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != "mycpanel" || pass != "secret-password" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"result":{"status":0}}`))
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"apiversion":3,"result":{"status":1}}`))
	}))
	defer server.Close()

	host, port := parseServerHostPort(t, server)

	tester := NewCPanelConnectionTester(server.Client())
	target := connector.Target{
		ResourceID:     uuid.New(),
		OrganizationID: uuid.New(),
		ResourceType:   resDomain.TypeCPanel,
		Host:           host,
		Port:           port,
		AuthType:       resDomain.AuthTypeCPanelPassword,
		Username:       "mycpanel",
	}

	credPayload := &payload.PayloadV1{
		Version: 1,
		Secret:  "secret-password",
	}

	result, err := tester.TestConnection(context.Background(), target, credPayload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Success {
		t.Fatalf("expected probe success, got failure: kind=%s, reason=%s", result.FailureKind, result.SafeReason)
	}
	if result.Details["auth_method"] != "password" {
		t.Errorf("expected auth_method 'password', got: %v", result.Details["auth_method"])
	}
	if result.Details["api_version"] != 3 {
		t.Errorf("expected api_version 3, got: %v", result.Details["api_version"])
	}
}

func TestCPanelConnectionTester_HTTP401_AuthenticationFailed(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"Unauthorized"}`))
	}))
	defer server.Close()

	host, port := parseServerHostPort(t, server)
	tester := NewCPanelConnectionTester(server.Client())
	target := connector.Target{
		ResourceID:     uuid.New(),
		OrganizationID: uuid.New(),
		ResourceType:   resDomain.TypeCPanel,
		Host:           host,
		Port:           port,
		AuthType:       resDomain.AuthTypeCPanelAPIToken,
		Username:       "mycpanel",
	}

	result, err := tester.TestConnection(context.Background(), target, &payload.PayloadV1{Version: 1, Secret: "bad"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Success {
		t.Fatalf("expected failure on 401")
	}
	if result.FailureKind != connector.FailureKindAuthFailed {
		t.Errorf("expected FailureKindAuthFailed, got: %s", result.FailureKind)
	}
	if result.SafeReason != "authentication failed" {
		t.Errorf("expected 'authentication failed', got: %s", result.SafeReason)
	}
}

func TestCPanelConnectionTester_HTTP403_AuthenticationFailed(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"Forbidden"}`))
	}))
	defer server.Close()

	host, port := parseServerHostPort(t, server)
	tester := NewCPanelConnectionTester(server.Client())
	target := connector.Target{
		ResourceID:     uuid.New(),
		OrganizationID: uuid.New(),
		ResourceType:   resDomain.TypeCPanel,
		Host:           host,
		Port:           port,
		AuthType:       resDomain.AuthTypeCPanelAPIToken,
		Username:       "mycpanel",
	}

	result, err := tester.TestConnection(context.Background(), target, &payload.PayloadV1{Version: 1, Secret: "bad"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Success {
		t.Fatalf("expected failure on 403")
	}
	if result.FailureKind != connector.FailureKindAuthFailed {
		t.Errorf("expected FailureKindAuthFailed, got: %s", result.FailureKind)
	}
	if result.SafeReason != "authentication failed" {
		t.Errorf("expected 'authentication failed', got: %s", result.SafeReason)
	}
}

func TestCPanelConnectionTester_UAPIStatusZero_RemoteAPIFailed(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		// UAPI status = 0 with internal error text that must not leak
		_, _ = w.Write([]byte(`{"apiversion":3,"result":{"status":0,"errors":["Access denied for user mycpanel on database server /var/cpanel/secrets"]}}`))
	}))
	defer server.Close()

	host, port := parseServerHostPort(t, server)
	tester := NewCPanelConnectionTester(server.Client())
	target := connector.Target{
		ResourceID:     uuid.New(),
		OrganizationID: uuid.New(),
		ResourceType:   resDomain.TypeCPanel,
		Host:           host,
		Port:           port,
		AuthType:       resDomain.AuthTypeCPanelAPIToken,
		Username:       "mycpanel",
	}

	result, err := tester.TestConnection(context.Background(), target, &payload.PayloadV1{Version: 1, Secret: "token"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Success {
		t.Fatalf("expected failure for UAPI status = 0")
	}
	if result.FailureKind != connector.FailureKindRemoteAPIFailed {
		t.Errorf("expected FailureKindRemoteAPIFailed, got: %s", result.FailureKind)
	}
	if result.SafeReason != "remote service did not accept the connection" {
		t.Errorf("expected safe reason 'remote service did not accept the connection', got: %s", result.SafeReason)
	}
	// Check no leakage of errors
	for k, v := range result.Details {
		if strings.Contains(k, "error") || strings.Contains(k, "access") {
			t.Errorf("leaked field in details: %s=%v", k, v)
		}
	}
}

func TestCPanelConnectionTester_APIVersionStringInjection_Rejected(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"apiversion":"REFLECTED-SECRET-TOKEN","result":{"status":1}}`))
	}))
	defer server.Close()

	host, port := parseServerHostPort(t, server)
	tester := NewCPanelConnectionTester(server.Client())
	target := connector.Target{
		ResourceID:     uuid.New(),
		OrganizationID: uuid.New(),
		ResourceType:   resDomain.TypeCPanel,
		Host:           host,
		Port:           port,
		AuthType:       resDomain.AuthTypeCPanelAPIToken,
		Username:       "mycpanel",
	}

	result, err := tester.TestConnection(context.Background(), target, &payload.PayloadV1{Version: 1, Secret: "token"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Success {
		t.Fatalf("expected failure when apiversion is a string injection")
	}
	if result.FailureKind != connector.FailureKindRemoteAPIFailed {
		t.Errorf("expected FailureKindRemoteAPIFailed, got: %s", result.FailureKind)
	}
	if strings.Contains(result.SafeReason, "REFLECTED") {
		t.Errorf("reflected string found in SafeReason: %s", result.SafeReason)
	}
	if result.Details != nil {
		for _, v := range result.Details {
			if v == "REFLECTED-SECRET-TOKEN" {
				t.Errorf("reflected string found in Details")
			}
		}
	}
}

func TestCPanelConnectionTester_APIVersionObjectInjection_Rejected(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"apiversion":{"token":"VERY-SENSITIVE"},"result":{"status":1}}`))
	}))
	defer server.Close()

	host, port := parseServerHostPort(t, server)
	tester := NewCPanelConnectionTester(server.Client())
	target := connector.Target{
		ResourceID:     uuid.New(),
		OrganizationID: uuid.New(),
		ResourceType:   resDomain.TypeCPanel,
		Host:           host,
		Port:           port,
		AuthType:       resDomain.AuthTypeCPanelAPIToken,
		Username:       "mycpanel",
	}

	result, err := tester.TestConnection(context.Background(), target, &payload.PayloadV1{Version: 1, Secret: "token"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Success {
		t.Fatalf("expected failure when apiversion is an object")
	}
	if result.FailureKind != connector.FailureKindRemoteAPIFailed {
		t.Errorf("expected FailureKindRemoteAPIFailed, got: %s", result.FailureKind)
	}
}

func TestCPanelConnectionTester_InvalidAPIVersion_NegativeOrZero(t *testing.T) {
	cases := []int{0, -1}
	for _, ver := range cases {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"apiversion":` + strconv.Itoa(ver) + `,"result":{"status":1}}`))
		}))

		host, port := parseServerHostPort(t, server)
		tester := NewCPanelConnectionTester(server.Client())
		target := connector.Target{
			ResourceID:     uuid.New(),
			OrganizationID: uuid.New(),
			ResourceType:   resDomain.TypeCPanel,
			Host:           host,
			Port:           port,
			AuthType:       resDomain.AuthTypeCPanelAPIToken,
			Username:       "mycpanel",
		}

		result, err := tester.TestConnection(context.Background(), target, &payload.PayloadV1{Version: 1, Secret: "token"})
		server.Close()

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Success {
			t.Fatalf("expected failure for api version %d", ver)
		}
		if result.FailureKind != connector.FailureKindRemoteAPIFailed {
			t.Errorf("expected FailureKindRemoteAPIFailed, got: %s", result.FailureKind)
		}
	}
}

func TestCPanelConnectionTester_UsernameValidation(t *testing.T) {
	var httpCalls int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&httpCalls, 1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"apiversion":3,"result":{"status":1}}`))
	}))
	defer server.Close()

	host, port := parseServerHostPort(t, server)
	tester := NewCPanelConnectionTester(server.Client())

	invalidUsernames := []string{
		"MyCPanel",     // Uppercase rejected
		"user:name",    // Colon delimiter rejected
		"user\nname",   // Newline rejected
		"user\rname",   // CR rejected
		"user\x00name", // NUL rejected
	}

	for _, u := range invalidUsernames {
		target := connector.Target{
			ResourceID:     uuid.New(),
			OrganizationID: uuid.New(),
			ResourceType:   resDomain.TypeCPanel,
			Host:           host,
			Port:           port,
			AuthType:       resDomain.AuthTypeCPanelAPIToken,
			Username:       u,
		}

		_, err := tester.TestConnection(context.Background(), target, &payload.PayloadV1{Version: 1, Secret: "token"})
		if !errors.Is(err, resDomain.ErrInvalidConnectorConfig) {
			t.Errorf("expected ErrInvalidConnectorConfig for username %q, got: %v", u, err)
		}
	}

	if atomic.LoadInt32(&httpCalls) != 0 {
		t.Errorf("expected 0 HTTP calls for invalid usernames, got %d", atomic.LoadInt32(&httpCalls))
	}
}

func TestCPanelConnectionTester_StrictTLSVerification_RejectsUntrustedCert(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"apiversion":3,"result":{"status":1}}`))
	}))
	defer server.Close()

	host, port := parseServerHostPort(t, server)

	// Default production tester with standard root CAs (does not trust ephemeral test TLS cert)
	tester := NewCPanelConnectionTester(nil)
	target := connector.Target{
		ResourceID:     uuid.New(),
		OrganizationID: uuid.New(),
		ResourceType:   resDomain.TypeCPanel,
		Host:           host,
		Port:           port,
		AuthType:       resDomain.AuthTypeCPanelAPIToken,
		Username:       "mycpanel",
	}

	result, err := tester.TestConnection(context.Background(), target, &payload.PayloadV1{Version: 1, Secret: "token"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Success {
		t.Fatalf("SECURITY FLAW: untrusted test certificate was accepted by default client")
	}
	if result.FailureKind != connector.FailureKindTLSVerificationFailed {
		t.Errorf("expected FailureKindTLSVerificationFailed, got: %s", result.FailureKind)
	}
	if result.SafeReason != "TLS certificate verification failed" {
		t.Errorf("expected 'TLS certificate verification failed', got: %s", result.SafeReason)
	}
}

func TestCPanelConnectionTester_UseHTTPSFalse_PreflightRejection(t *testing.T) {
	useHTTPS := false
	tester := NewCPanelConnectionTester(nil)
	target := connector.Target{
		ResourceID:     uuid.New(),
		OrganizationID: uuid.New(),
		ResourceType:   resDomain.TypeCPanel,
		Host:           "cpanel.example.com",
		Port:           2083,
		AuthType:       resDomain.AuthTypeCPanelAPIToken,
		Username:       "mycpanel",
		UseHTTPS:       &useHTTPS,
	}

	_, err := tester.TestConnection(context.Background(), target, &payload.PayloadV1{Version: 1, Secret: "token"})
	if !errors.Is(err, resDomain.ErrInvalidConnectorConfig) {
		t.Errorf("expected ErrInvalidConnectorConfig when use_https=false, got: %v", err)
	}
}

func TestCPanelConnectionTester_RedirectDisabled(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://redirected.example.com", http.StatusFound)
	}))
	defer server.Close()

	host, port := parseServerHostPort(t, server)
	client := server.Client()
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}

	tester := NewCPanelConnectionTester(client)
	target := connector.Target{
		ResourceID:     uuid.New(),
		OrganizationID: uuid.New(),
		ResourceType:   resDomain.TypeCPanel,
		Host:           host,
		Port:           port,
		AuthType:       resDomain.AuthTypeCPanelAPIToken,
		Username:       "mycpanel",
	}

	result, err := tester.TestConnection(context.Background(), target, &payload.PayloadV1{Version: 1, Secret: "token"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Success {
		t.Fatalf("expected failure on HTTP redirect")
	}
	if result.FailureKind != connector.FailureKindRemoteAPIFailed {
		t.Errorf("expected FailureKindRemoteAPIFailed, got: %s", result.FailureKind)
	}
}

func TestCPanelConnectionTester_OversizedBody(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		junk := strings.Repeat("A", 1500*1024)
		_, _ = w.Write([]byte(junk))
	}))
	defer server.Close()

	host, port := parseServerHostPort(t, server)
	tester := NewCPanelConnectionTester(server.Client())
	target := connector.Target{
		ResourceID:     uuid.New(),
		OrganizationID: uuid.New(),
		ResourceType:   resDomain.TypeCPanel,
		Host:           host,
		Port:           port,
		AuthType:       resDomain.AuthTypeCPanelAPIToken,
		Username:       "mycpanel",
	}

	result, err := tester.TestConnection(context.Background(), target, &payload.PayloadV1{Version: 1, Secret: "token"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Success {
		t.Fatalf("expected failure for oversized response body")
	}
	if result.FailureKind != connector.FailureKindRemoteAPIFailed {
		t.Errorf("expected FailureKindRemoteAPIFailed, got: %s", result.FailureKind)
	}
}

func TestCPanelConnectionTester_MalformedJSON(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{not valid json`))
	}))
	defer server.Close()

	host, port := parseServerHostPort(t, server)
	tester := NewCPanelConnectionTester(server.Client())
	target := connector.Target{
		ResourceID:     uuid.New(),
		OrganizationID: uuid.New(),
		ResourceType:   resDomain.TypeCPanel,
		Host:           host,
		Port:           port,
		AuthType:       resDomain.AuthTypeCPanelAPIToken,
		Username:       "mycpanel",
	}

	result, err := tester.TestConnection(context.Background(), target, &payload.PayloadV1{Version: 1, Secret: "token"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Success {
		t.Fatalf("expected failure for malformed JSON")
	}
	if result.FailureKind != connector.FailureKindRemoteAPIFailed {
		t.Errorf("expected FailureKindRemoteAPIFailed, got: %s", result.FailureKind)
	}
}

func TestCPanelConnectionTester_Timeout(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Wait until client context expires
		<-r.Context().Done()
	}))
	defer server.Close()

	host, port := parseServerHostPort(t, server)
	tester := NewCPanelConnectionTester(server.Client())
	timeoutSec := 1
	target := connector.Target{
		ResourceID:        uuid.New(),
		OrganizationID:    uuid.New(),
		ResourceType:      resDomain.TypeCPanel,
		Host:              host,
		Port:              port,
		AuthType:          resDomain.AuthTypeCPanelAPIToken,
		Username:          "mycpanel",
		ConnectionTimeout: &timeoutSec,
	}

	result, err := tester.TestConnection(context.Background(), target, &payload.PayloadV1{Version: 1, Secret: "token"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Success {
		t.Fatalf("expected failure on timeout")
	}
	if result.FailureKind != connector.FailureKindTimeout {
		t.Errorf("expected FailureKindTimeout, got: %s", result.FailureKind)
	}
	if result.SafeReason != "connection timed out" {
		t.Errorf("expected 'connection timed out', got: %s", result.SafeReason)
	}
}

func TestCPanelConnectionTester_CallerCancellation(t *testing.T) {
	reqReceived := make(chan struct{})
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-reqReceived:
		default:
			close(reqReceived)
		}
		<-r.Context().Done()
	}))
	defer server.Close()

	host, port := parseServerHostPort(t, server)
	tester := NewCPanelConnectionTester(server.Client())
	target := connector.Target{
		ResourceID:     uuid.New(),
		OrganizationID: uuid.New(),
		ResourceType:   resDomain.TypeCPanel,
		Host:           host,
		Port:           port,
		AuthType:       resDomain.AuthTypeCPanelAPIToken,
		Username:       "mycpanel",
	}

	parentCtx, cancel := context.WithCancel(context.Background())
	go func() {
		<-reqReceived
		cancel()
	}()

	_, err := tester.TestConnection(parentCtx, target, &payload.PayloadV1{Version: 1, Secret: "token"})
	if err == nil {
		t.Fatalf("expected error when caller parent context is canceled")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got: %v", err)
	}
}
