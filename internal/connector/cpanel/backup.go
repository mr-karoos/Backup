package cpanel

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/gorilla/websocket"

	"backup-platform/internal/connector"
	"backup-platform/internal/credential/payload"
	resDomain "backup-platform/internal/resource/domain"
)

// WebSocketConn abstracts a WebSocket connection for operational streaming and testability.
type WebSocketConn interface {
	NextReader() (messageType int, r io.Reader, err error)
	Close() error
}

// WebSocketDialer abstracts the WebSocket dial operation for testability and injection.
type WebSocketDialer interface {
	DialContext(ctx context.Context, urlStr string, requestHeader http.Header) (WebSocketConn, *http.Response, error)
}

type gorillaWebSocketDialer struct {
	dialer *websocket.Dialer
}

func (g *gorillaWebSocketDialer) DialContext(ctx context.Context, urlStr string, requestHeader http.Header) (WebSocketConn, *http.Response, error) {
	conn, resp, err := g.dialer.DialContext(ctx, urlStr, requestHeader)
	if err != nil {
		return nil, resp, err
	}
	return conn, resp, nil
}

// CPanelDatabaseBackupCapability implements connector.DatabaseBackupCapability over cPanel WebSocket transport.
type CPanelDatabaseBackupCapability struct {
	dialer WebSocketDialer
}

// NewCPanelDatabaseBackupCapability constructs a CPanelDatabaseBackupCapability.
func NewCPanelDatabaseBackupCapability(dialer WebSocketDialer) *CPanelDatabaseBackupCapability {
	if dialer == nil {
		dialer = &gorillaWebSocketDialer{
			dialer: &websocket.Dialer{
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: false,
				},
			},
		}
	}
	return &CPanelDatabaseBackupCapability{dialer: dialer}
}

// BackupDatabase streams a MySQL database dump from the remote cPanel server into dest over WebSocket.
func (c *CPanelDatabaseBackupCapability) BackupDatabase(
	ctx context.Context,
	target connector.Target,
	credPayload *payload.PayloadV1,
	databaseName string,
	dest io.Writer,
) error {
	// 1. Mandatory HTTPS / TLS enforcement
	if target.UseHTTPS != nil && !*target.UseHTTPS {
		return resDomain.ErrInvalidConnectorConfig
	}

	// 2. Validate database name before initiating any network call
	if err := connector.ValidateDatabaseName(databaseName); err != nil {
		return fmt.Errorf("invalid database name: %w", err)
	}

	// 3. Validate cPanel username operational constraints
	if err := resDomain.ValidateCPanelOperationalUsername(target.Username); err != nil {
		return err
	}

	// 4. Validate credential payload
	if credPayload == nil || credPayload.Secret == "" {
		return connector.ErrInvalidCredentialFormat
	}
	defer payload.Clear(credPayload)

	// 5. Construct WebSocket query params with net/url.Values
	q := url.Values{}
	q.Set("dbname", databaseName)
	q.Set("include_data", "1")
	q.Set("encoding", "utf8mb4")

	wsURL := url.URL{
		Scheme:   "wss",
		Host:     net.JoinHostPort(target.Host, strconv.Itoa(target.Port)),
		Path:     "/websocket/MysqlDump",
		RawQuery: q.Encode(),
	}

	// 6. Build headers based on AuthType
	header := make(http.Header)
	switch target.AuthType {
	case resDomain.AuthTypeCPanelAPIToken:
		header.Set("Authorization", fmt.Sprintf("cpanel %s:%s", target.Username, credPayload.Secret))
	case resDomain.AuthTypeCPanelPassword:
		auth := base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s", target.Username, credPayload.Secret)))
		header.Set("Authorization", "Basic "+auth)
	default:
		return resDomain.ErrInvalidAuthType
	}

	// 7. Resolve dial timeout
	timeoutDuration := defaultConnectionTimeout
	if target.ConnectionTimeout != nil && *target.ConnectionTimeout > 0 {
		timeoutDuration = time.Duration(*target.ConnectionTimeout) * time.Second
	}

	dialCtx, dialCancel := context.WithTimeout(ctx, timeoutDuration)
	defer dialCancel()

	// 8. Dial WebSocket connection
	conn, resp, dialErr := c.dialer.DialContext(dialCtx, wsURL.String(), header)
	// Delete sensitive authorization header and clear payload immediately after DialContext returns
	header.Del("Authorization")
	payload.Clear(credPayload)

	if dialErr != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if isTLSCertVerificationError(dialErr) {
			return connector.ErrCPanelTLSVerification
		}
		if errors.Is(dialCtx.Err(), context.DeadlineExceeded) || isNetTimeout(dialErr) {
			return connector.ErrCPanelTimeout
		}
		if resp != nil && (resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden) {
			return connector.ErrCPanelAuthentication
		}
		return connector.ErrCPanelNetwork
	}
	if conn == nil {
		return connector.ErrCPanelNetwork
	}
	defer conn.Close()

	// 9. Context cancellation supervisor during streaming
	stopWait := context.AfterFunc(ctx, func() {
		_ = conn.Close()
	})
	defer stopWait()

	// 10. Stream frames into dest
	for {
		_, r, err := conn.NextReader()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if websocket.IsCloseError(err, websocket.CloseNormalClosure) {
				return nil
			}
			var closeErr *websocket.CloseError
			if errors.As(err, &closeErr) {
				if closeErr.Code == websocket.CloseNormalClosure {
					return nil
				}
				return connector.ErrCPanelDumpFailed
			}
			if isNetTimeout(err) {
				return connector.ErrCPanelTimeout
			}
			var netErr net.Error
			if errors.As(err, &netErr) {
				return connector.ErrCPanelNetwork
			}
			return connector.ErrCPanelDumpFailed
		}

		if _, copyErr := io.Copy(dest, r); copyErr != nil {
			if errors.Is(ctx.Err(), context.Canceled) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return ctx.Err()
			}
			return copyErr
		}
	}
}
