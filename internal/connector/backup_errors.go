package connector

import "errors"

var (
	// ErrSSHTimeout indicates a connection or handshake timeout over SSH.
	ErrSSHTimeout = errors.New("ssh timeout")

	// ErrSSHNetwork indicates a network or transport failure during SSH connection.
	ErrSSHNetwork = errors.New("ssh network failure")

	// ErrSSHAuthentication indicates that SSH authentication failed.
	ErrSSHAuthentication = errors.New("ssh authentication failed")

	// ErrSSHHostKeyMismatch indicates a remote host key fingerprint mismatch.
	ErrSSHHostKeyMismatch = errors.New("ssh host key mismatch")

	// ErrInvalidCredentialFormat indicates that the credential format or private key is malformed.
	ErrInvalidCredentialFormat = errors.New("invalid credential format")

	// ErrDumpToolMissing indicates that neither mysqldump nor mariadb-dump was found on the remote host.
	ErrDumpToolMissing = errors.New("database dump utility not found")

	// ErrDumpCommandFailed indicates that the remote dump command exited with a non-zero exit code.
	ErrDumpCommandFailed = errors.New("database dump command failed")

	// ErrArchiveToolMissing indicates that the tar archiving utility was not found on the remote host (exit 127).
	ErrArchiveToolMissing = errors.New("archive utility not found")

	// ErrArchiveCommandFailed indicates that the remote tar command exited with a non-zero exit code.
	ErrArchiveCommandFailed = errors.New("archive command failed")

	// ErrRemoteCommandStderrOverflow indicates that the remote command exceeded the allowed stderr buffer limit.
	ErrRemoteCommandStderrOverflow = errors.New("remote command stderr overflow")

	// ErrInvalidFileBackupConfig indicates that the file backup configuration or target spec is invalid.
	ErrInvalidFileBackupConfig = errors.New("invalid file backup configuration")
)
