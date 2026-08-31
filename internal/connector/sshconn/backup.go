package sshconn

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	"backup-platform/internal/connector"
	"backup-platform/internal/credential/payload"
)

const (
	maxStderrBackupDrainBytes = 64 * 1024 // 64 KiB
)

// POSIXShellQuote safely escapes a string for use as a single argument in POSIX shells.
// It wraps the entire string in single quotes and safely escapes embedded single quotes.
func POSIXShellQuote(s string) string {
	if len(s) == 0 {
		return "''"
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// SSHDatabaseBackupCapability implements connector.DatabaseBackupCapability over an SSH transport.
type SSHDatabaseBackupCapability struct {
	dialer DialerFunc
}

// NewSSHDatabaseBackupCapability initializes a new SSHDatabaseBackupCapability with the default or custom dialer.
func NewSSHDatabaseBackupCapability(dialer DialerFunc) *SSHDatabaseBackupCapability {
	if dialer == nil {
		dialer = NewSSHConnectionTester(nil).dialer
	}
	return &SSHDatabaseBackupCapability{dialer: dialer}
}

// BackupDatabase streams a MySQL database dump from the remote SSH server into dest.
// If mysqldump is missing (exit status 127) and 0 bytes have been written to dest, it falls back to mariadb-dump.
func (c *SSHDatabaseBackupCapability) BackupDatabase(
	ctx context.Context,
	target connector.Target,
	credPayload *payload.PayloadV1,
	databaseName string,
	dest io.Writer,
) error {
	if err := connector.ValidateDatabaseName(databaseName); err != nil {
		return fmt.Errorf("invalid database name: %w", err)
	}

	client, probeRes, err := dialAuthenticatedSSHClient(ctx, c.dialer, target, credPayload)
	// Clear sensitive credential payload immediately after SSH authentication attempt
	payload.Clear(credPayload)

	if err != nil {
		return err
	}
	if probeRes != nil {
		switch probeRes.FailureKind {
		case connector.FailureKindTimeout:
			return connector.ErrSSHTimeout
		case connector.FailureKindAuthFailed:
			return connector.ErrSSHAuthentication
		case connector.FailureKindHostKeyMismatch:
			return connector.ErrSSHHostKeyMismatch
		case connector.FailureKindConnFailed:
			fallthrough
		default:
			return connector.ErrSSHNetwork
		}
	}
	defer client.Close()

	// Clear TCP deadline so long-running backup dumps are not terminated by connection timeout
	if client.RawConn != nil {
		if err := client.RawConn.SetDeadline(time.Time{}); err != nil {
			return connector.ErrSSHNetwork
		}
	}

	// 1. Attempt backup with mysqldump
	quotedDB := POSIXShellQuote(databaseName)
	primaryCmd := fmt.Sprintf("mysqldump --single-transaction --quick --routines --triggers --hex-blob -- %s", quotedDB)

	bytesWritten, err := streamRemoteBackupCommand(ctx, client.Client, primaryCmd, dest, maxStderrBackupDrainBytes)
	if err == nil {
		return nil
	}

	// If context was cancelled, preserve ctx.Err()
	if ctx.Err() != nil {
		return ctx.Err()
	}

	// 2. Fallback to mariadb-dump only if exit status is 127 AND no bytes were written to destination
	if bytesWritten == 0 && isCommandNotFoundExit(err) {
		fallbackCmd := fmt.Sprintf("mariadb-dump --single-transaction --quick --routines --triggers --hex-blob -- %s", quotedDB)
		_, fbErr := streamRemoteBackupCommand(ctx, client.Client, fallbackCmd, dest, maxStderrBackupDrainBytes)
		if fbErr == nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if errors.Is(fbErr, connector.ErrRemoteCommandStderrOverflow) {
			return connector.ErrDumpCommandFailed
		}
		if isCommandNotFoundExit(fbErr) {
			return connector.ErrDumpToolMissing
		}
		var exitErr *ssh.ExitError
		if errors.As(fbErr, &exitErr) {
			return connector.ErrDumpCommandFailed
		}
		return fbErr
	}

	if errors.Is(err, connector.ErrRemoteCommandStderrOverflow) {
		return connector.ErrDumpCommandFailed
	}

	if isCommandNotFoundExit(err) {
		return connector.ErrDumpToolMissing
	}

	var exitErr *ssh.ExitError
	if errors.As(err, &exitErr) {
		return connector.ErrDumpCommandFailed
	}

	return err
}

func isCommandNotFoundExit(err error) bool {
	var exitErr *ssh.ExitError
	return errors.As(err, &exitErr) && exitErr.ExitStatus() == 127
}

type countingWriter struct {
	w       io.Writer
	written int64
}

func (cw *countingWriter) Write(p []byte) (int, error) {
	n, err := cw.w.Write(p)
	cw.written += int64(n)
	return n, err
}

// streamRemoteBackupCommand executes a remote command and streams its stdout directly into dest.
func streamRemoteBackupCommand(
	ctx context.Context,
	client *ssh.Client,
	cmd string,
	dest io.Writer,
	stderrLimit int64,
) (int64, error) {
	session, err := client.NewSession()
	if err != nil {
		return 0, connector.ErrSSHNetwork
	}
	defer session.Close()

	stdoutPipe, err := session.StdoutPipe()
	if err != nil {
		return 0, connector.ErrSSHNetwork
	}

	stderrPipe, err := session.StderrPipe()
	if err != nil {
		return 0, connector.ErrSSHNetwork
	}

	var sessionCloseOnce sync.Once
	closeSession := func() {
		sessionCloseOnce.Do(func() {
			_ = session.Close()
		})
	}

	stopWait := context.AfterFunc(ctx, closeSession)
	defer stopWait()

	if err := session.Start(cmd); err != nil {
		return 0, connector.ErrSSHNetwork
	}

	// Drain stderr up to stderrLimit concurrently and detect overflow
	var stderrOverflow bool
	var stderrMu sync.Mutex
	stderrDone := make(chan struct{})
	go func() {
		defer close(stderrDone)
		var buf [512]byte
		var totalStderr int64
		for {
			n, rErr := stderrPipe.Read(buf[:])
			if n > 0 {
				totalStderr += int64(n)
				if totalStderr > stderrLimit {
					stderrMu.Lock()
					stderrOverflow = true
					stderrMu.Unlock()
					closeSession()
					return
				}
			}
			if rErr != nil {
				return
			}
		}
	}()

	cw := &countingWriter{w: dest}
	_, copyErr := io.Copy(cw, stdoutPipe)
	if copyErr != nil {
		closeSession()
	}

	<-stderrDone

	waitErr := session.Wait()

	stderrMu.Lock()
	overflow := stderrOverflow
	stderrMu.Unlock()

	if ctx.Err() != nil {
		return cw.written, ctx.Err()
	}

	if copyErr != nil {
		return cw.written, copyErr
	}

	if overflow {
		return cw.written, connector.ErrRemoteCommandStderrOverflow
	}

	if waitErr != nil {
		var exitErr *ssh.ExitError
		if errors.As(waitErr, &exitErr) {
			return cw.written, waitErr
		}
		return cw.written, connector.ErrSSHNetwork
	}

	return cw.written, nil
}
