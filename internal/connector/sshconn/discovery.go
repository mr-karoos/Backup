package sshconn

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"backup-platform/internal/connector"
	"backup-platform/internal/credential/payload"

	"golang.org/x/crypto/ssh"
)

const (
	maxStdoutBytes int64 = 4 * 1024 * 1024 // 4 MiB stdout budget
	maxStderrBytes int64 = 64 * 1024       // 64 KiB stderr budget
)

const mysqlDiscoverySQL = "SELECT HEX(s.SCHEMA_NAME), COALESCE(SUM(CASE WHEN t.TABLE_TYPE = 'BASE TABLE' THEN COALESCE(t.DATA_LENGTH, 0) + COALESCE(t.INDEX_LENGTH, 0) ELSE 0 END), 0) AS size_bytes, COALESCE(SUM(CASE WHEN t.TABLE_TYPE = 'BASE TABLE' THEN 1 ELSE 0 END), 0) AS tables_count FROM information_schema.SCHEMATA AS s LEFT JOIN information_schema.TABLES AS t ON t.TABLE_SCHEMA = s.SCHEMA_NAME WHERE s.SCHEMA_NAME NOT IN ('information_schema', 'mysql', 'performance_schema', 'sys') GROUP BY s.SCHEMA_NAME ORDER BY s.SCHEMA_NAME;"

var (
	// Static discovery commands: zero user input or credentials interpolated
	mysqlDiscoveryCmd   = fmt.Sprintf("mysql -B --skip-column-names -e %q", mysqlDiscoverySQL)
	mariadbDiscoveryCmd = fmt.Sprintf("mariadb -B --skip-column-names -e %q", mysqlDiscoverySQL)
)

// SSHDatabaseDiscoverer implements MySQL database discovery over SSH.
type SSHDatabaseDiscoverer struct {
	dialer DialerFunc
}

// NewSSHDatabaseDiscoverer constructs a new SSHDatabaseDiscoverer.
func NewSSHDatabaseDiscoverer(dialer DialerFunc) *SSHDatabaseDiscoverer {
	if dialer == nil {
		dialer = NewSSHConnectionTester(nil).dialer
	}
	return &SSHDatabaseDiscoverer{
		dialer: dialer,
	}
}

// DiscoverDatabases connects via SSH and runs static information_schema query via mysql/mariadb CLI.
func (d *SSHDatabaseDiscoverer) DiscoverDatabases(
	ctx context.Context,
	target connector.Target,
	credPayload *payload.PayloadV1,
) ([]connector.DatabaseInfo, error) {
	client, probeRes, err := dialAuthenticatedSSHClient(ctx, d.dialer, target, credPayload)
	if err != nil {
		return nil, err
	}
	if probeRes != nil {
		return nil, errors.New("ssh connection failed")
	}
	defer client.Close()

	// Try mysql CLI first; fallback to mariadb CLI if mysql is not installed (exit code 127)
	output, err := runBoundedRemoteCommand(ctx, client.Client, mysqlDiscoveryCmd)
	if err != nil {
		var exitErr *ssh.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitStatus() == 127 {
			// mysql not found, try mariadb
			output, err = runBoundedRemoteCommand(ctx, client.Client, mariadbDiscoveryCmd)
		}
	}
	if err != nil {
		return nil, errors.New("remote mysql discovery command failed")
	}

	return parseMySQLTSVOutput(output)
}

// runBoundedRemoteCommand runs a remote command on the SSH client with production 4 MiB stdout and 64 KiB stderr limits.
func runBoundedRemoteCommand(ctx context.Context, client *ssh.Client, cmd string) ([]byte, error) {
	return runBoundedRemoteCommandWithLimits(ctx, client, cmd, maxStdoutBytes, maxStderrBytes)
}

// runBoundedRemoteCommandWithLimits executes a remote command, concurrently draining stdout and stderr within strict byte limits.
// If an output stream overflows or context is canceled, the SSH session is immediately aborted to terminate the remote process.
func runBoundedRemoteCommandWithLimits(
	ctx context.Context,
	client *ssh.Client,
	cmd string,
	stdoutLimit int64,
	stderrLimit int64,
) ([]byte, error) {
	session, err := client.NewSession()
	if err != nil {
		return nil, err
	}

	var closeOnce sync.Once
	closeSession := func() {
		closeOnce.Do(func() {
			_ = session.Close()
		})
	}
	defer closeSession()

	stdoutPipe, err := session.StdoutPipe()
	if err != nil {
		return nil, err
	}

	stderrPipe, err := session.StderrPipe()
	if err != nil {
		return nil, err
	}

	if err := session.Start(cmd); err != nil {
		return nil, err
	}

	// Close session immediately if parent context is canceled
	stopWait := context.AfterFunc(ctx, closeSession)
	defer stopWait()

	var (
		wg             sync.WaitGroup
		stdoutBuf      bytes.Buffer
		stdoutErr      error
		stdoutOverflow bool
		stderrErr      error
		stderrOverflow bool
	)

	// Goroutine 1: Concurrently drain bounded stdout (+1 byte for overflow detection)
	wg.Add(1)
	go func() {
		defer wg.Done()
		limitedStdout := io.LimitReader(stdoutPipe, stdoutLimit+1)
		nOut, err := io.Copy(&stdoutBuf, limitedStdout)
		if err != nil {
			stdoutErr = err
		}
		if nOut > stdoutLimit {
			stdoutOverflow = true
			closeSession()
		}
	}()

	// Goroutine 2: Concurrently drain bounded stderr (+1 byte for overflow detection; discarded to minimize RAM usage)
	wg.Add(1)
	go func() {
		defer wg.Done()
		limitedStderr := io.LimitReader(stderrPipe, stderrLimit+1)
		nErr, err := io.Copy(io.Discard, limitedStderr)
		if err != nil {
			stderrErr = err
		}
		if nErr > stderrLimit {
			stderrOverflow = true
			closeSession()
		}
	}()

	// Await full completion of both concurrent stream readers
	wg.Wait()

	// Wait for remote command completion (invoked exactly once on the main goroutine)
	waitErr := session.Wait()

	// 1. Check parent context cancellation
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// 2. Check stream overflows (root cause must not be obscured by session close errors)
	if stdoutOverflow {
		return nil, errors.New("remote command stdout exceeded maximum allowed limit")
	}
	if stderrOverflow {
		return nil, errors.New("remote command stderr exceeded maximum allowed limit")
	}

	// 3. Check I/O stream errors
	if stdoutErr != nil && !errors.Is(stdoutErr, io.EOF) {
		return nil, stdoutErr
	}
	if stderrErr != nil && !errors.Is(stderrErr, io.EOF) {
		return nil, errors.New("error reading remote command stderr")
	}

	// 4. Check command exit status
	if waitErr != nil {
		return nil, waitErr
	}

	return stdoutBuf.Bytes(), nil
}

// parseMySQLTSVOutput parses tab-separated output into a slice of DatabaseInfo.
func parseMySQLTSVOutput(output []byte) ([]connector.DatabaseInfo, error) {
	lines := strings.Split(string(output), "\n")
	result := make([]connector.DatabaseInfo, 0, len(lines))

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if len(line) == 0 {
			continue
		}

		parts := strings.Split(line, "\t")
		if len(parts) != 3 {
			return nil, errors.New("malformed remote mysql output: unexpected column count")
		}

		// 1. Column 1: Hex-encoded database name
		hexName := strings.TrimSpace(parts[0])
		rawNameBytes, err := hex.DecodeString(hexName)
		if err != nil {
			return nil, errors.New("malformed remote mysql output: invalid hex database name")
		}
		dbName := string(rawNameBytes)
		if !utf8.ValidString(dbName) {
			return nil, errors.New("malformed remote mysql output: invalid utf-8 database name")
		}

		// 2. Column 2: Size in bytes
		sizeBytes, err := strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64)
		if err != nil || sizeBytes < 0 {
			return nil, errors.New("malformed remote mysql output: invalid size_bytes")
		}

		// 3. Column 3: Table count
		tablesCount, err := strconv.ParseInt(strings.TrimSpace(parts[2]), 10, 64)
		if err != nil || tablesCount < 0 {
			return nil, errors.New("malformed remote mysql output: invalid tables_count")
		}

		result = append(result, connector.DatabaseInfo{
			Name:        dbName,
			SizeBytes:   sizeBytes,
			TablesCount: &tablesCount,
			Status:      connector.DatabaseStatusAccessible,
		})
	}

	return result, nil
}
