package cpanel

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"backup-platform/internal/connector"
	"backup-platform/internal/credential/payload"
	resDomain "backup-platform/internal/resource/domain"
	"backup-platform/pkg/uuid"
)

// mockWebSocketConn simulates a WebSocket connection with predefined frames and error.
type mockWebSocketConn struct {
	frames       [][]byte
	frameIndex   int
	closeErr     error
	closed       bool
	mu           sync.Mutex
	onClose      func()
	onNextReader func()
}

func (m *mockWebSocketConn) NextReader() (int, io.Reader, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.onNextReader != nil {
		m.onNextReader()
	}

	if m.closed {
		return 0, nil, errors.New("connection closed")
	}

	if m.frameIndex < len(m.frames) {
		data := m.frames[m.frameIndex]
		m.frameIndex++
		return websocket.BinaryMessage, bytes.NewReader(data), nil
	}

	if m.closeErr != nil {
		return 0, nil, m.closeErr
	}

	return 0, nil, &websocket.CloseError{Code: websocket.CloseNormalClosure, Text: "normal closure"}
}

func (m *mockWebSocketConn) Close() error {
	m.mu.Lock()
	m.closed = true
	m.mu.Unlock()
	if m.onClose != nil {
		m.onClose()
	}
	return nil
}

// mockWebSocketDialer simulates the dial operation, capturing parameters for assertions.
type mockWebSocketDialer struct {
	conn       WebSocketConn
	resp       *http.Response
	err        error
	lastURL    string
	lastHeader http.Header
	rawHeader  http.Header
	dialDelay  time.Duration
	onDial     func(ctx context.Context, urlStr string, header http.Header)
}

func (m *mockWebSocketDialer) DialContext(ctx context.Context, urlStr string, header http.Header) (WebSocketConn, *http.Response, error) {
	m.lastURL = urlStr
	m.lastHeader = header.Clone()
	m.rawHeader = header
	if m.onDial != nil {
		m.onDial(ctx, urlStr, header)
	}
	if m.dialDelay > 0 {
		select {
		case <-time.After(m.dialDelay):
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		}
	}
	if m.err != nil {
		return nil, m.resp, m.err
	}
	return m.conn, m.resp, nil
}

func testTarget(authType resDomain.AuthType) connector.Target {
	useHTTPS := true
	timeout := 5
	return connector.Target{
		ResourceID:        uuid.New(),
		OrganizationID:    uuid.New(),
		ResourceType:      resDomain.TypeCPanel,
		Host:              "cpanel.example.com",
		Port:              2083,
		AuthType:          authType,
		Username:          "cpaneluser",
		ConnectionTimeout: &timeout,
		UseHTTPS:          &useHTTPS,
	}
}

// 1. API Token Auth header
func TestCPanelBackup_APITokenAuth(t *testing.T) {
	dialer := &mockWebSocketDialer{
		conn: &mockWebSocketConn{
			frames: [][]byte{[]byte("-- MySQL dump")},
		},
	}
	cap := NewCPanelDatabaseBackupCapability(dialer)

	credPayload := &payload.PayloadV1{
		Version: 1,
		Secret:  "API-TOKEN-VALUE-999",
	}

	var buf bytes.Buffer
	err := cap.BackupDatabase(context.Background(), testTarget(resDomain.AuthTypeCPanelAPIToken), credPayload, "app_db", &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	authHeader := dialer.lastHeader.Get("Authorization")
	expected := "cpanel cpaneluser:API-TOKEN-VALUE-999"
	if authHeader != expected {
		t.Fatalf("authorization header for API token did not match expected structure")
	}
}

// 2. Password Basic Auth header
func TestCPanelBackup_PasswordBasicAuth(t *testing.T) {
	dialer := &mockWebSocketDialer{
		conn: &mockWebSocketConn{
			frames: [][]byte{[]byte("-- MySQL dump")},
		},
	}
	cap := NewCPanelDatabaseBackupCapability(dialer)

	credPayload := &payload.PayloadV1{
		Version: 1,
		Secret:  "SecretPass123!",
	}

	var buf bytes.Buffer
	err := cap.BackupDatabase(context.Background(), testTarget(resDomain.AuthTypeCPanelPassword), credPayload, "app_db", &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	authHeader := dialer.lastHeader.Get("Authorization")
	expectedBasic := "Basic " + base64.StdEncoding.EncodeToString([]byte("cpaneluser:SecretPass123!"))
	if authHeader != expectedBasic {
		t.Fatalf("authorization header for password basic auth did not match expected structure")
	}
}

// 3. URL Encoding & parameters
func TestCPanelBackup_URLEncoding(t *testing.T) {
	dialer := &mockWebSocketDialer{
		conn: &mockWebSocketConn{
			frames: [][]byte{[]byte("data")},
		},
	}
	cap := NewCPanelDatabaseBackupCapability(dialer)

	secretToken := "token-secret-xyz"
	credPayload := &payload.PayloadV1{
		Version: 1,
		Secret:  secretToken,
	}

	// Database name requiring URL query escaping: '+' encodes to '%2B' and '=' encodes to '%3D'
	dbName := "db_prod+2026=final"

	var buf bytes.Buffer
	err := cap.BackupDatabase(context.Background(), testTarget(resDomain.AuthTypeCPanelAPIToken), credPayload, dbName, &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	parsed, err := url.Parse(dialer.lastURL)
	if err != nil {
		t.Fatalf("failed to parse url: %v", err)
	}

	if parsed.Scheme != "wss" {
		t.Fatalf("expected scheme wss, got %s", parsed.Scheme)
	}
	if parsed.Host != "cpanel.example.com:2083" {
		t.Fatalf("expected host cpanel.example.com:2083, got %s", parsed.Host)
	}
	if parsed.Path != "/websocket/MysqlDump" {
		t.Fatalf("expected path /websocket/MysqlDump, got %s", parsed.Path)
	}

	// Verify unescaped query parameter
	q := parsed.Query()
	if q.Get("dbname") != dbName {
		t.Fatalf("expected dbname %s, got %s", dbName, q.Get("dbname"))
	}
	if q.Get("include_data") != "1" {
		t.Fatalf("expected include_data 1, got %s", q.Get("include_data"))
	}
	if q.Get("encoding") != "utf8mb4" {
		t.Fatalf("expected encoding utf8mb4, got %s", q.Get("encoding"))
	}

	// Verify percent-encoded representation in RawQuery
	if !strings.Contains(parsed.RawQuery, "dbname=db_prod%2B2026%3Dfinal") {
		t.Fatalf("expected percent-encoded dbname in RawQuery, got: %s", parsed.RawQuery)
	}

	// Invariant: Secret/token must NEVER appear in the URL
	if strings.Contains(dialer.lastURL, secretToken) {
		t.Fatalf("CRITICAL: credential token leaked in URL")
	}
}

// 4. Invalid database names are rejected before any network call
func TestCPanelBackup_InvalidDBName(t *testing.T) {
	dialerCalled := false
	dialer := &mockWebSocketDialer{
		onDial: func(ctx context.Context, urlStr string, header http.Header) {
			dialerCalled = true
		},
	}
	cap := NewCPanelDatabaseBackupCapability(dialer)

	invalidNames := []string{
		"",
		" ",
		"   ",
		"db\x00null",
		"db\ninjection",
		"db\r\ninjection",
		"db\tinjection",
		"db\x1fname",
		strings.Repeat("a", 65),
		"invalid-\xff-utf8",
	}

	for _, dbName := range invalidNames {
		dialerCalled = false
		credPayload := &payload.PayloadV1{Version: 1, Secret: "secret"}
		var buf bytes.Buffer
		err := cap.BackupDatabase(context.Background(), testTarget(resDomain.AuthTypeCPanelAPIToken), credPayload, dbName, &buf)
		if err == nil {
			t.Errorf("expected error for dbName %q, got nil", dbName)
		}
		if dialerCalled {
			t.Errorf("dialer was called for invalid dbName %q", dbName)
		}
	}
}

// 5. Invalid cPanel username rejected
func TestCPanelBackup_InvalidUsername(t *testing.T) {
	dialerCalled := false
	dialer := &mockWebSocketDialer{
		onDial: func(ctx context.Context, urlStr string, header http.Header) {
			dialerCalled = true
		},
	}
	cap := NewCPanelDatabaseBackupCapability(dialer)

	invalidUsernames := []string{
		"RootUser",    // uppercase forbidden
		"user:name",   // colon forbidden
		"user\nname",  // newline forbidden
		"user\rname",  // carriage return forbidden
		"user\x00bad", // null byte forbidden
		"",            // empty forbidden
	}

	for _, u := range invalidUsernames {
		dialerCalled = false
		target := testTarget(resDomain.AuthTypeCPanelAPIToken)
		target.Username = u

		credPayload := &payload.PayloadV1{Version: 1, Secret: "secret"}
		var buf bytes.Buffer
		err := cap.BackupDatabase(context.Background(), target, credPayload, "mydb", &buf)
		if !errors.Is(err, resDomain.ErrInvalidConnectorConfig) {
			t.Fatalf("expected ErrInvalidConnectorConfig for username %q, got: %v", u, err)
		}
		if dialerCalled {
			t.Fatalf("dialer was called for invalid username %q", u)
		}
	}
}

// 6. Insecure HTTP rejected (fail closed)
func TestCPanelBackup_InsecureHTTPSRejected(t *testing.T) {
	dialer := &mockWebSocketDialer{}
	cap := NewCPanelDatabaseBackupCapability(dialer)

	target := testTarget(resDomain.AuthTypeCPanelAPIToken)
	f := false
	target.UseHTTPS = &f

	credPayload := &payload.PayloadV1{Version: 1, Secret: "secret"}
	var buf bytes.Buffer
	err := cap.BackupDatabase(context.Background(), target, credPayload, "mydb", &buf)
	if !errors.Is(err, resDomain.ErrInvalidConnectorConfig) {
		t.Fatalf("expected ErrInvalidConnectorConfig, got %v", err)
	}
}

// 7. Invalid AuthType rejected
func TestCPanelBackup_InvalidAuthType(t *testing.T) {
	dialer := &mockWebSocketDialer{}
	cap := NewCPanelDatabaseBackupCapability(dialer)

	target := testTarget(resDomain.AuthTypeSSHKey) // Not cpanel auth type
	credPayload := &payload.PayloadV1{Version: 1, Secret: "secret"}
	var buf bytes.Buffer
	err := cap.BackupDatabase(context.Background(), target, credPayload, "mydb", &buf)
	if !errors.Is(err, resDomain.ErrInvalidAuthType) {
		t.Fatalf("expected ErrInvalidAuthType, got %v", err)
	}
}

// 8. Empty credential rejected
func TestCPanelBackup_EmptyCredential(t *testing.T) {
	dialer := &mockWebSocketDialer{}
	cap := NewCPanelDatabaseBackupCapability(dialer)

	var buf bytes.Buffer
	err := cap.BackupDatabase(context.Background(), testTarget(resDomain.AuthTypeCPanelAPIToken), nil, "mydb", &buf)
	if !errors.Is(err, connector.ErrInvalidCredentialFormat) {
		t.Fatalf("expected ErrInvalidCredentialFormat, got %v", err)
	}

	emptySecret := &payload.PayloadV1{Version: 1, Secret: ""}
	err = cap.BackupDatabase(context.Background(), testTarget(resDomain.AuthTypeCPanelAPIToken), emptySecret, "mydb", &buf)
	if !errors.Is(err, connector.ErrInvalidCredentialFormat) {
		t.Fatalf("expected ErrInvalidCredentialFormat, got %v", err)
	}
}

// 9. Credential payload memory cleared immediately
func TestCPanelBackup_CredentialCleared(t *testing.T) {
	dialer := &mockWebSocketDialer{
		conn: &mockWebSocketConn{
			frames: [][]byte{[]byte("data")},
		},
	}
	cap := NewCPanelDatabaseBackupCapability(dialer)

	credPayload := &payload.PayloadV1{
		Version: 1,
		Secret:  "SENSITIVE-TOKEN-STRING",
	}

	var buf bytes.Buffer
	_ = cap.BackupDatabase(context.Background(), testTarget(resDomain.AuthTypeCPanelAPIToken), credPayload, "mydb", &buf)

	if credPayload.Secret != "" {
		t.Fatalf("expected credPayload.Secret to be wiped, but it remained non-empty (len=%d)", len(credPayload.Secret))
	}
}

// 10. Authorization header and credential cleared immediately after DialContext, before streaming begins
func TestCPanelBackup_AuthorizationHeaderAndCredentialClearedBeforeStreaming(t *testing.T) {
	secretToken := "SUPER-SENSITIVE-AUTH-TOKEN-12345"
	target := testTarget(resDomain.AuthTypeCPanelAPIToken)
	credPayload := &payload.PayloadV1{
		Version: 1,
		Secret:  secretToken,
	}

	var authHeaderDuringDial string
	var rawHeaderRef http.Header
	var nextReaderInvoked bool

	mockConn := &mockWebSocketConn{
		frames: [][]byte{[]byte("-- dump stream chunk")},
	}

	dialer := &mockWebSocketDialer{
		conn: mockConn,
	}

	mockConn.onNextReader = func() {
		nextReaderInvoked = true
		// Verify original Authorization header in rawHeader map is deleted
		if rawHeaderRef != nil && rawHeaderRef.Get("Authorization") != "" {
			t.Fatalf("expected Authorization header to be deleted before first NextReader, but it remained non-empty (len=%d)", len(rawHeaderRef.Get("Authorization")))
		}
		// Verify credPayload.Secret is cleared before first NextReader
		if credPayload.Secret != "" {
			t.Fatalf("expected credPayload.Secret to be wiped before first NextReader, but it remained non-empty (len=%d)", len(credPayload.Secret))
		}
	}

	dialer.onDial = func(ctx context.Context, urlStr string, header http.Header) {
		rawHeaderRef = header
		authHeaderDuringDial = header.Get("Authorization")
	}

	cap := NewCPanelDatabaseBackupCapability(dialer)
	var buf bytes.Buffer
	err := cap.BackupDatabase(context.Background(), target, credPayload, "mydb", &buf)
	if err != nil {
		t.Fatalf("unexpected backup failure: %v", err)
	}

	if !nextReaderInvoked {
		t.Fatalf("NextReader was never called")
	}

	// Verify Authorization header was present during dial
	expectedPrefix := fmt.Sprintf("cpanel %s:", target.Username)
	if !strings.HasPrefix(authHeaderDuringDial, expectedPrefix) {
		t.Fatalf("expected Authorization header during dial to have prefix %q, got length %d", expectedPrefix, len(authHeaderDuringDial))
	}
	if len(authHeaderDuringDial) <= len(expectedPrefix) {
		t.Fatalf("expected non-empty secret in Authorization header during dial")
	}

	// Verify header was deleted from rawHeader
	if rawHeaderRef.Get("Authorization") != "" {
		t.Fatalf("Authorization header in rawHeader was not deleted (len=%d)", len(rawHeaderRef.Get("Authorization")))
	}
	// Verify credPayload is wiped
	if credPayload.Secret != "" {
		t.Fatalf("credPayload.Secret was not wiped (len=%d)", len(credPayload.Secret))
	}
}

// 11. Stream piping single frame
func TestCPanelBackup_StreamPiping(t *testing.T) {
	dumpContent := []byte("-- MySQL dump 10.13\nCREATE DATABASE `mydb`;\n")
	dialer := &mockWebSocketDialer{
		conn: &mockWebSocketConn{
			frames: [][]byte{dumpContent},
		},
	}
	cap := NewCPanelDatabaseBackupCapability(dialer)

	credPayload := &payload.PayloadV1{Version: 1, Secret: "tok"}
	var buf bytes.Buffer
	err := cap.BackupDatabase(context.Background(), testTarget(resDomain.AuthTypeCPanelAPIToken), credPayload, "mydb", &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !bytes.Equal(buf.Bytes(), dumpContent) {
		t.Fatalf("stream content mismatch: got %q, want %q", buf.String(), string(dumpContent))
	}
}

// 11. Multi-frame streaming preserves ordering
func TestCPanelBackup_MultiFrameOrdering(t *testing.T) {
	chunks := [][]byte{
		[]byte("CHUNK-1;"),
		[]byte("CHUNK-2;"),
		[]byte("CHUNK-3;"),
		[]byte("CHUNK-4-FINAL;"),
	}
	dialer := &mockWebSocketDialer{
		conn: &mockWebSocketConn{
			frames: chunks,
		},
	}
	cap := NewCPanelDatabaseBackupCapability(dialer)

	credPayload := &payload.PayloadV1{Version: 1, Secret: "tok"}
	var buf bytes.Buffer
	err := cap.BackupDatabase(context.Background(), testTarget(resDomain.AuthTypeCPanelAPIToken), credPayload, "mydb", &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "CHUNK-1;CHUNK-2;CHUNK-3;CHUNK-4-FINAL;"
	if buf.String() != expected {
		t.Fatalf("ordering mismatch: got %q, want %q", buf.String(), expected)
	}
}

// 12. Close code 1000 is success
func TestCPanelBackup_CloseNormal1000_Success(t *testing.T) {
	dialer := &mockWebSocketDialer{
		conn: &mockWebSocketConn{
			frames:   [][]byte{[]byte("dump data")},
			closeErr: &websocket.CloseError{Code: websocket.CloseNormalClosure, Text: "normal"},
		},
	}
	cap := NewCPanelDatabaseBackupCapability(dialer)

	credPayload := &payload.PayloadV1{Version: 1, Secret: "tok"}
	var buf bytes.Buffer
	err := cap.BackupDatabase(context.Background(), testTarget(resDomain.AuthTypeCPanelAPIToken), credPayload, "mydb", &buf)
	if err != nil {
		t.Fatalf("expected nil for close 1000, got: %v", err)
	}
}

// 13. Close code 1011 is ErrCPanelDumpFailed
func TestCPanelBackup_Close1011_Failure(t *testing.T) {
	dialer := &mockWebSocketDialer{
		conn: &mockWebSocketConn{
			frames:   [][]byte{[]byte("partial")},
			closeErr: &websocket.CloseError{Code: websocket.CloseInternalServerErr, Text: "server error"},
		},
	}
	cap := NewCPanelDatabaseBackupCapability(dialer)

	credPayload := &payload.PayloadV1{Version: 1, Secret: "tok"}
	var buf bytes.Buffer
	err := cap.BackupDatabase(context.Background(), testTarget(resDomain.AuthTypeCPanelAPIToken), credPayload, "mydb", &buf)
	if !errors.Is(err, connector.ErrCPanelDumpFailed) {
		t.Fatalf("expected ErrCPanelDumpFailed for code 1011, got: %v", err)
	}
}

// 14. Close code 4000 is ErrCPanelDumpFailed
func TestCPanelBackup_Close4000_Failure(t *testing.T) {
	dialer := &mockWebSocketDialer{
		conn: &mockWebSocketConn{
			closeErr: &websocket.CloseError{Code: 4000, Text: "cpanel dump exception"},
		},
	}
	cap := NewCPanelDatabaseBackupCapability(dialer)

	credPayload := &payload.PayloadV1{Version: 1, Secret: "tok"}
	var buf bytes.Buffer
	err := cap.BackupDatabase(context.Background(), testTarget(resDomain.AuthTypeCPanelAPIToken), credPayload, "mydb", &buf)
	if !errors.Is(err, connector.ErrCPanelDumpFailed) {
		t.Fatalf("expected ErrCPanelDumpFailed for code 4000, got: %v", err)
	}
}

// 15. Handshake 401/403 is ErrCPanelAuthentication
func TestCPanelBackup_HandshakeAuthFailure401_403(t *testing.T) {
	for _, statusCode := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		dialer := &mockWebSocketDialer{
			resp: &http.Response{
				StatusCode: statusCode,
				Status:     fmt.Sprintf("%d Error", statusCode),
			},
			err: fmt.Errorf("websocket: bad handshake with status %d", statusCode),
		}
		cap := NewCPanelDatabaseBackupCapability(dialer)

		credPayload := &payload.PayloadV1{Version: 1, Secret: "tok"}
		var buf bytes.Buffer
		err := cap.BackupDatabase(context.Background(), testTarget(resDomain.AuthTypeCPanelAPIToken), credPayload, "mydb", &buf)
		if !errors.Is(err, connector.ErrCPanelAuthentication) {
			t.Fatalf("expected ErrCPanelAuthentication for status %d, got: %v", statusCode, err)
		}
	}
}

// 16. TLS Certificate error classification (unknown CA, hostname mismatch, expired/invalid cert)
func TestCPanelBackup_TLSCertError(t *testing.T) {
	tlsErrors := []struct {
		name string
		err  error
	}{
		{
			name: "Unknown authority / CA",
			err:  x509.UnknownAuthorityError{},
		},
		{
			name: "Hostname mismatch",
			err:  x509.HostnameError{Host: "cpanel.example.com", Certificate: &x509.Certificate{}},
		},
		{
			name: "Expired certificate",
			err:  x509.CertificateInvalidError{Reason: x509.Expired},
		},
		{
			name: "Constraint violation",
			err:  x509.ConstraintViolationError{},
		},
		{
			name: "Typed tls.CertificateVerificationError",
			err:  &tls.CertificateVerificationError{UnverifiedCertificates: []*x509.Certificate{{}}},
		},
	}

	for _, tt := range tlsErrors {
		t.Run(tt.name, func(t *testing.T) {
			dialer := &mockWebSocketDialer{
				err: tt.err,
			}
			cap := NewCPanelDatabaseBackupCapability(dialer)

			credPayload := &payload.PayloadV1{Version: 1, Secret: "tok"}
			var buf bytes.Buffer
			err := cap.BackupDatabase(context.Background(), testTarget(resDomain.AuthTypeCPanelAPIToken), credPayload, "mydb", &buf)
			if !errors.Is(err, connector.ErrCPanelTLSVerification) {
				t.Fatalf("expected ErrCPanelTLSVerification for %s, got: %v", tt.name, err)
			}
		})
	}
}

// 17. No string-heuristic false positives for 401, 403, certificate, or x509
func TestCPanelBackup_NoStringHeuristicFalsePositives(t *testing.T) {
	testCases := []struct {
		name        string
		dialErr     error
		resp        *http.Response
		expectedErr error
	}{
		{
			name:        "Error contains 401 but no HTTP response",
			dialErr:     errors.New("upstream proxy returned gateway error 401"),
			resp:        nil,
			expectedErr: connector.ErrCPanelNetwork,
		},
		{
			name:        "Error contains 403 but HTTP status is 500",
			dialErr:     errors.New("token verification failure 403 in log"),
			resp:        &http.Response{StatusCode: http.StatusInternalServerError, Status: "500 Internal Server Error"},
			expectedErr: connector.ErrCPanelNetwork,
		},
		{
			name:        "Error contains certificate text without typed TLS error",
			dialErr:     errors.New("client certificate file unreadable"),
			resp:        nil,
			expectedErr: connector.ErrCPanelNetwork,
		},
		{
			name:        "Error contains x509 text without typed TLS error",
			dialErr:     errors.New("failed parsing local x509 configuration"),
			resp:        nil,
			expectedErr: connector.ErrCPanelNetwork,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			dialer := &mockWebSocketDialer{
				err:  tc.dialErr,
				resp: tc.resp,
			}
			cap := NewCPanelDatabaseBackupCapability(dialer)

			credPayload := &payload.PayloadV1{Version: 1, Secret: "tok"}
			var buf bytes.Buffer
			err := cap.BackupDatabase(context.Background(), testTarget(resDomain.AuthTypeCPanelAPIToken), credPayload, "mydb", &buf)
			if !errors.Is(err, tc.expectedErr) {
				t.Fatalf("expected %v, got: %v", tc.expectedErr, err)
			}
			if errors.Is(err, connector.ErrCPanelAuthentication) {
				t.Fatalf("unexpected ErrCPanelAuthentication from string heuristic")
			}
			if errors.Is(err, connector.ErrCPanelTLSVerification) {
				t.Fatalf("unexpected ErrCPanelTLSVerification from string heuristic")
			}
		})
	}
}

// 18. Network error classification
func TestCPanelBackup_NetworkError(t *testing.T) {
	dialer := &mockWebSocketDialer{
		err: &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")},
	}
	cap := NewCPanelDatabaseBackupCapability(dialer)

	credPayload := &payload.PayloadV1{Version: 1, Secret: "tok"}
	var buf bytes.Buffer
	err := cap.BackupDatabase(context.Background(), testTarget(resDomain.AuthTypeCPanelAPIToken), credPayload, "mydb", &buf)
	if !errors.Is(err, connector.ErrCPanelNetwork) {
		t.Fatalf("expected ErrCPanelNetwork, got: %v", err)
	}
}

// 18. Timeout error classification
type mockTimeoutError struct{}

func (m mockTimeoutError) Error() string   { return "i/o timeout" }
func (m mockTimeoutError) Timeout() bool   { return true }
func (m mockTimeoutError) Temporary() bool { return true }

func TestCPanelBackup_TimeoutError(t *testing.T) {
	dialer := &mockWebSocketDialer{
		err: mockTimeoutError{},
	}
	cap := NewCPanelDatabaseBackupCapability(dialer)

	credPayload := &payload.PayloadV1{Version: 1, Secret: "tok"}
	var buf bytes.Buffer
	err := cap.BackupDatabase(context.Background(), testTarget(resDomain.AuthTypeCPanelAPIToken), credPayload, "mydb", &buf)
	if !errors.Is(err, connector.ErrCPanelTimeout) {
		t.Fatalf("expected ErrCPanelTimeout, got: %v", err)
	}
}

// 19. Context cancellation terminates streaming and closes connection
func TestCPanelBackup_ContextCancellation(t *testing.T) {
	connClosed := make(chan struct{})
	var closeOnce sync.Once
	mockConn := &mockWebSocketConn{
		frames: [][]byte{[]byte("first-frame")},
		onClose: func() {
			closeOnce.Do(func() {
				close(connClosed)
			})
		},
	}

	ctx, cancel := context.WithCancel(context.Background())

	dialer := &mockWebSocketDialer{
		conn: mockConn,
	}
	cap := NewCPanelDatabaseBackupCapability(dialer)

	// Writer that triggers cancellation on first write
	cw := &cancellingWriter{cancel: cancel}

	credPayload := &payload.PayloadV1{Version: 1, Secret: "tok"}
	err := cap.BackupDatabase(ctx, testTarget(resDomain.AuthTypeCPanelAPIToken), credPayload, "mydb", cw)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got: %v", err)
	}

	select {
	case <-connClosed:
		// Conn close supervisor invoked
	case <-time.After(1 * time.Second):
		t.Fatalf("connection was not closed on context cancellation")
	}
}

type cancellingWriter struct {
	cancel context.CancelFunc
}

func (c *cancellingWriter) Write(p []byte) (int, error) {
	c.cancel()
	return len(p), nil
}

// 20. Parent context deadline exceeded during dial returns context.DeadlineExceeded
func TestCPanelBackup_ParentContextDeadlineExceeded_Dial(t *testing.T) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-1*time.Minute))
	defer cancel()

	dialer := &mockWebSocketDialer{
		err: context.DeadlineExceeded,
	}
	cap := NewCPanelDatabaseBackupCapability(dialer)

	credPayload := &payload.PayloadV1{Version: 1, Secret: "tok"}
	var buf bytes.Buffer
	err := cap.BackupDatabase(ctx, testTarget(resDomain.AuthTypeCPanelAPIToken), credPayload, "mydb", &buf)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context.DeadlineExceeded, got: %v", err)
	}
	if errors.Is(err, connector.ErrCPanelTimeout) {
		t.Fatalf("expected parent context error to take precedence over ErrCPanelTimeout")
	}
}

// 21. Handshake timeout with parent context alive returns ErrCPanelTimeout
func TestCPanelBackup_HandshakeTimeout_ReturnsErrCPanelTimeout(t *testing.T) {
	dialer := &mockWebSocketDialer{
		err: mockTimeoutError{},
	}
	cap := NewCPanelDatabaseBackupCapability(dialer)

	credPayload := &payload.PayloadV1{Version: 1, Secret: "tok"}
	var buf bytes.Buffer
	err := cap.BackupDatabase(context.Background(), testTarget(resDomain.AuthTypeCPanelAPIToken), credPayload, "mydb", &buf)
	if !errors.Is(err, connector.ErrCPanelTimeout) {
		t.Fatalf("expected ErrCPanelTimeout, got: %v", err)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("did not expect context.DeadlineExceeded when parent context is alive")
	}
}

// 22. Parent context deadline exceeded during streaming returns context.DeadlineExceeded
func TestCPanelBackup_ParentContextDeadlineExceeded_Streaming(t *testing.T) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(10*time.Millisecond))
	defer cancel()

	mockConn := &mockWebSocketConn{
		frames: [][]byte{[]byte("frame-1"), []byte("frame-2")},
	}
	dialer := &mockWebSocketDialer{conn: mockConn}
	cap := NewCPanelDatabaseBackupCapability(dialer)

	hw := &hookWriter{
		fn: func() {
			time.Sleep(25 * time.Millisecond)
		},
	}

	credPayload := &payload.PayloadV1{Version: 1, Secret: "tok"}
	err := cap.BackupDatabase(ctx, testTarget(resDomain.AuthTypeCPanelAPIToken), credPayload, "mydb", hw)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context.DeadlineExceeded during streaming, got: %v", err)
	}
	if errors.Is(err, connector.ErrCPanelTimeout) {
		t.Fatalf("did not expect ErrCPanelTimeout when parent deadline exceeded")
	}
}

type hookWriter struct {
	fn func()
}

func (h *hookWriter) Write(p []byte) (int, error) {
	if h.fn != nil {
		h.fn()
	}
	return len(p), nil
}

// 23. No secret leaks in error strings
func TestCPanelBackup_NoSecretLeaks(t *testing.T) {
	superSecret := "SECRET-SUPER-SENSITIVE-998877"
	dialer := &mockWebSocketDialer{
		err: fmt.Errorf("transport failure with header Authorization: cpanel cpaneluser:%s", superSecret),
	}
	cap := NewCPanelDatabaseBackupCapability(dialer)

	credPayload := &payload.PayloadV1{Version: 1, Secret: superSecret}
	var buf bytes.Buffer
	err := cap.BackupDatabase(context.Background(), testTarget(resDomain.AuthTypeCPanelAPIToken), credPayload, "mydb", &buf)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}

	if strings.Contains(err.Error(), superSecret) {
		t.Fatalf("CRITICAL: sensitive secret leaked in error string")
	}
}

// 24. Registry resolution for cPanel
func TestCPanelBackup_RegistryResolution(t *testing.T) {
	registry := connector.NewBackupCapabilityRegistry()
	cpanelCap := NewCPanelDatabaseBackupCapability(nil)
	registry.Register(resDomain.TypeCPanel, cpanelCap)

	resolved, ok := registry.Get(resDomain.TypeCPanel)
	if !ok || resolved == nil {
		t.Fatalf("expected cpanel backup capability to be resolved from registry")
	}
	if resolved != cpanelCap {
		t.Fatalf("resolved capability pointer mismatch")
	}
}

// 25. Real TLS WebSocket end-to-end roundtrip test
func TestCPanelBackup_RealTLSRoundTrip(t *testing.T) {
	upgrader := websocket.Upgrader{}
	expectedDump := "-- MySQL dump 10.13\n-- Table structure for table `users`\nINSERT INTO `users` VALUES (1, 'alice');\n"

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/websocket/MysqlDump" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "cpanel cpaneluser:REAL-TOKEN-123" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.URL.Query().Get("dbname") != "prod_db" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()

		// Stream frames
		_ = ws.WriteMessage(websocket.BinaryMessage, []byte(expectedDump[:20]))
		_ = ws.WriteMessage(websocket.BinaryMessage, []byte(expectedDump[20:]))
		// Clean close frame
		_ = ws.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "done"))
	}))
	defer server.Close()

	host, port := parseServerHostPort(t, server)

	// Custom dialer sharing server's TLS config for valid certificate verification
	tlsDialer := &gorillaWebSocketDialer{
		dialer: &websocket.Dialer{
			TLSClientConfig: server.Client().Transport.(*http.Transport).TLSClientConfig,
		},
	}

	cap := NewCPanelDatabaseBackupCapability(tlsDialer)
	target := connector.Target{
		ResourceID:     uuid.New(),
		OrganizationID: uuid.New(),
		ResourceType:   resDomain.TypeCPanel,
		Host:           host,
		Port:           port,
		AuthType:       resDomain.AuthTypeCPanelAPIToken,
		Username:       "cpaneluser",
	}

	credPayload := &payload.PayloadV1{
		Version: 1,
		Secret:  "REAL-TOKEN-123",
	}

	var buf bytes.Buffer
	err := cap.BackupDatabase(context.Background(), target, credPayload, "prod_db", &buf)
	if err != nil {
		t.Fatalf("unexpected error during real TLS websocket dump: %v", err)
	}

	if buf.String() != expectedDump {
		t.Fatalf("dump mismatch: got %q, want %q", buf.String(), expectedDump)
	}
}
