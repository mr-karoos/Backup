package sshconn

import (
	"context"
	"errors"
	"net"
	"strconv"
	"strings"
	"time"

	"backup-platform/internal/connector"
	"backup-platform/internal/credential/payload"
	"backup-platform/internal/credential/secretcrypto"
	resDomain "backup-platform/internal/resource/domain"

	"golang.org/x/crypto/ssh"
)

// AuthenticatedSSHClient represents an active, authenticated SSH client connection.
type AuthenticatedSSHClient struct {
	Client    *ssh.Client
	RawConn   net.Conn
	Latency   time.Duration
	CheckedAt time.Time
	AuthType  string
	Banner    string
}

// Close ensures both the SSH client session layer and underlying TCP connection are closed.
func (a *AuthenticatedSSHClient) Close() error {
	var errs []error
	if a.Client != nil {
		if err := a.Client.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if a.RawConn != nil {
		if err := a.RawConn.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// dialAuthenticatedSSHClient performs preflight validation, timeout budgeting, TCP dialing,
// strict host key fingerprint verification, and credential authentication to yield an AuthenticatedSSHClient.
func dialAuthenticatedSSHClient(
	ctx context.Context,
	dialer DialerFunc,
	target connector.Target,
	credPayload *payload.PayloadV1,
) (*AuthenticatedSSHClient, *connector.ProbeResult, error) {
	// 1. Mandatory Host Key Fingerprint Check (Preflight)
	if target.HostKeyFingerprint == nil || strings.TrimSpace(*target.HostKeyFingerprint) == "" {
		return nil, nil, resDomain.ErrInvalidHostKeyFingerprint
	}
	expectedFingerprint := *target.HostKeyFingerprint

	// 2. Resolve Connection Timeout as a single unified budget
	timeoutDuration := defaultConnectionTimeout
	if target.ConnectionTimeout != nil && *target.ConnectionTimeout > 0 {
		timeoutDuration = time.Duration(*target.ConnectionTimeout) * time.Second
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, timeoutDuration)
	defer cancel()

	// 3. Prepare Authentication Methods
	var authMethods []ssh.AuthMethod
	var authMethodName string

	switch target.AuthType {
	case resDomain.AuthTypeSSHPassword:
		authMethodName = "password"
		authMethods = []ssh.AuthMethod{
			ssh.Password(credPayload.Secret),
		}

	case resDomain.AuthTypeSSHKey:
		authMethodName = "publickey"
		secretBytes := []byte(credPayload.Secret)

		var signer ssh.Signer
		var parseErr error

		if credPayload.Passphrase != nil && len(*credPayload.Passphrase) > 0 {
			passBytes := []byte(*credPayload.Passphrase)
			signer, parseErr = ssh.ParsePrivateKeyWithPassphrase(secretBytes, passBytes)
			secretcrypto.ZeroBytes(passBytes)
		} else {
			signer, parseErr = ssh.ParsePrivateKey(secretBytes)
		}
		secretcrypto.ZeroBytes(secretBytes)

		if parseErr != nil {
			return nil, nil, connector.ErrInvalidCredentialFormat
		}

		authMethods = []ssh.AuthMethod{
			ssh.PublicKeys(signer),
		}

	default:
		return nil, nil, resDomain.ErrInvalidAuthType
	}

	// 4. Strict Host Key Callback (No InsecureIgnoreHostKey)
	var hostKeyErr *hostKeyMismatchError
	hostKeyCallback := func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		actualFP := ssh.FingerprintSHA256(key)
		if actualFP != expectedFingerprint {
			hostKeyErr = &hostKeyMismatchError{
				expected: expectedFingerprint,
				actual:   actualFP,
			}
			return hostKeyErr
		}
		return nil
	}

	clientConfig := &ssh.ClientConfig{
		User:            target.Username,
		Auth:            authMethods,
		HostKeyCallback: hostKeyCallback,
		Timeout:         timeoutDuration,
	}

	addr := net.JoinHostPort(target.Host, strconv.Itoa(target.Port))
	startTime := time.Now()

	// 5. Dial TCP with bounded context
	conn, err := dialer(timeoutCtx, "tcp", addr)
	if err != nil {
		checkedAt := time.Now().UTC()
		latency := time.Since(startTime)

		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}

		if errors.Is(timeoutCtx.Err(), context.DeadlineExceeded) || isNetTimeout(err) {
			return nil, &connector.ProbeResult{
				Success:     false,
				Latency:     latency,
				CheckedAt:   checkedAt,
				FailureKind: connector.FailureKindTimeout,
				SafeReason:  "connection timed out",
			}, nil
		}

		return nil, &connector.ProbeResult{
			Success:     false,
			Latency:     latency,
			CheckedAt:   checkedAt,
			FailureKind: connector.FailureKindConnFailed,
			SafeReason:  "remote connection failed",
		}, nil
	}

	// Set deadline on raw TCP connection according to remaining budget in timeoutCtx
	if deadline, ok := timeoutCtx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}

	// Interrupt handshake immediately if timeoutCtx is canceled or expires
	stopHandshakeWatch := context.AfterFunc(timeoutCtx, func() {
		_ = conn.Close()
	})
	defer stopHandshakeWatch()

	// 6. Perform SSH Handshake & Authentication
	sshConn, chans, reqs, err := ssh.NewClientConn(conn, addr, clientConfig)
	checkedAt := time.Now().UTC()
	latency := time.Since(startTime)

	if err != nil {
		_ = conn.Close()
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}

		if hostKeyErr != nil {
			return nil, &connector.ProbeResult{
				Success:     false,
				Latency:     latency,
				CheckedAt:   checkedAt,
				FailureKind: connector.FailureKindHostKeyMismatch,
				SafeReason:  "SSH host key verification failed",
			}, nil
		}

		if errors.Is(timeoutCtx.Err(), context.DeadlineExceeded) || isNetTimeout(err) {
			return nil, &connector.ProbeResult{
				Success:     false,
				Latency:     latency,
				CheckedAt:   checkedAt,
				FailureKind: connector.FailureKindTimeout,
				SafeReason:  "connection timed out",
			}, nil
		}

		errStr := strings.ToLower(err.Error())
		if strings.Contains(errStr, "unable to authenticate") ||
			strings.Contains(errStr, "auth failed") ||
			strings.Contains(errStr, "handshake failed: ssh: unable to authenticate") {
			return nil, &connector.ProbeResult{
				Success:     false,
				Latency:     latency,
				CheckedAt:   checkedAt,
				FailureKind: connector.FailureKindAuthFailed,
				SafeReason:  "authentication failed",
			}, nil
		}

		return nil, &connector.ProbeResult{
			Success:     false,
			Latency:     latency,
			CheckedAt:   checkedAt,
			FailureKind: connector.FailureKindConnFailed,
			SafeReason:  "remote connection failed",
		}, nil
	}

	banner := SanitizeBanner(string(sshConn.ServerVersion()))
	client := ssh.NewClient(sshConn, chans, reqs)

	return &AuthenticatedSSHClient{
		Client:    client,
		RawConn:   conn,
		Latency:   latency,
		CheckedAt: checkedAt,
		AuthType:  authMethodName,
		Banner:    banner,
	}, nil, nil
}
