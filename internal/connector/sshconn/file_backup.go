package sshconn

import (
	"context"
	"errors"
	"io"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	"backup-platform/internal/backup/domain"
	"backup-platform/internal/connector"
	"backup-platform/internal/credential/payload"
)

// SSHFileBackupCapability implements connector.FileBackupCapability over an SSH transport.
type SSHFileBackupCapability struct {
	dialer DialerFunc
}

// NewSSHFileBackupCapability initializes a new SSHFileBackupCapability with the default or custom dialer.
func NewSSHFileBackupCapability(dialer DialerFunc) *SSHFileBackupCapability {
	if dialer == nil {
		dialer = NewSSHConnectionTester(nil).dialer
	}
	return &SSHFileBackupCapability{dialer: dialer}
}

// BackupFiles streams an uncompressed tar archive of config.SourcePath from the remote SSH server into dest.
// Archive members are strictly relative to the source root (e.g. ./index.php) via 'tar -C <source> -cf - <excludes> -- .'.
func (c *SSHFileBackupCapability) BackupFiles(
	ctx context.Context,
	target connector.Target,
	credPayload *payload.PayloadV1,
	config connector.FileBackupConfig,
	dest io.Writer,
) error {
	normalizedPath, err := domain.ValidateAndNormalizePOSIXPath(config.SourcePath)
	if err != nil {
		return connector.ErrInvalidFileBackupConfig
	}

	for _, pat := range config.ExcludePatterns {
		if err := domain.ValidateExcludePattern(pat); err != nil {
			return connector.ErrInvalidFileBackupConfig
		}
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

	// Clear TCP deadline so long-running file streaming is not terminated by handshake timeout
	if client.RawConn != nil {
		if err := client.RawConn.SetDeadline(time.Time{}); err != nil {
			return connector.ErrSSHNetwork
		}
	}

	// Build safe GNU tar extraction command
	var cmd strings.Builder
	cmd.WriteString("tar -C ")
	cmd.WriteString(POSIXShellQuote(normalizedPath))
	cmd.WriteString(" -cf -")
	for _, pat := range config.ExcludePatterns {
		cmd.WriteString(" ")
		cmd.WriteString(POSIXShellQuote("--exclude=" + pat))
	}
	cmd.WriteString(" -- .")
	tarCmd := cmd.String()

	_, streamErr := streamRemoteBackupCommand(ctx, client.Client, tarCmd, dest, maxStderrBackupDrainBytes)
	if streamErr == nil {
		return nil
	}

	// If context was cancelled, preserve ctx.Err()
	if ctx.Err() != nil {
		return ctx.Err()
	}

	if errors.Is(streamErr, connector.ErrRemoteCommandStderrOverflow) {
		return connector.ErrArchiveCommandFailed
	}

	if isCommandNotFoundExit(streamErr) {
		return connector.ErrArchiveToolMissing
	}

	var exitErr *ssh.ExitError
	if errors.As(streamErr, &exitErr) {
		return connector.ErrArchiveCommandFailed
	}

	return streamErr
}
