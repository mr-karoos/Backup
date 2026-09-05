package restic

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"backup-platform/internal/credential/secretcrypto"
)

// MaxOutputBytes limits captured output to prevent memory exhaustion from hostile subprocess outputs.
const MaxOutputBytes = 64 * 1024

// CommandRunner defines the safe execution boundary for Restic subprocess commands (ADR-031, ADR-035).
type CommandRunner interface {
	Init(ctx context.Context, target RepositoryTarget, password []byte) error
	Probe(ctx context.Context, target RepositoryTarget, password []byte) error
	Version(ctx context.Context) (string, error)
	ValidateVersion(ctx context.Context) error
	GetSnapshot(ctx context.Context, target RepositoryTarget, password []byte, snapshotID string) (*SnapshotItem, error)
	ListSnapshotNodes(ctx context.Context, target RepositoryTarget, password []byte, snapshotID string) ([]SnapshotNode, error)
	DumpSample(ctx context.Context, target RepositoryTarget, password []byte, snapshotID, internalFilename string, maxBytes int) ([]byte, error)
	DumpStream(ctx context.Context, target RepositoryTarget, password []byte, snapshotID, internalFilename string) (io.ReadCloser, error)
}

// SnapshotSummary represents the summary metrics of a snapshot.
type SnapshotSummary struct {
	TotalBytesProcessed int64 `json:"total_bytes_processed"`
	FilesNew            int   `json:"files_new"`
	DataAdded           int64 `json:"data_added"`
}

// SnapshotItem represents the JSON metadata of a snapshot from "restic snapshots --json".
type SnapshotItem struct {
	ID             string           `json:"id"`
	ShortID        string           `json:"short_id"`
	Time           time.Time        `json:"time"`
	Paths          []string         `json:"paths"`
	Tags           []string         `json:"tags"`
	Hostname       string           `json:"hostname"`
	Username       string           `json:"username"`
	ProgramVersion string           `json:"program_version"`
	Summary        *SnapshotSummary `json:"summary,omitempty"`
}

// SnapshotNode represents a file or directory node from "restic ls --json".
type SnapshotNode struct {
	Name string `json:"name"`
	Type string `json:"type"`
	Path string `json:"path"`
	Size int64  `json:"size"`
}

// ResticRunner implements CommandRunner by safely invoking the official Restic binary via exec.CommandContext.
type ResticRunner struct {
	binaryPath string
	logger     *slog.Logger
}

// NewResticRunner constructs a new ResticRunner.
// If binaryPath is specified, it uses it. If blank, it defaults to the trusted container path /usr/local/bin/restic.
// It NEVER reads environment variables (such as RESTIC_BINARY_PATH) directly;
// the decision of binary selection is strictly owned by the caller/configuration.
func NewResticRunner(binaryPath string, logger *slog.Logger) *ResticRunner {
	if logger == nil {
		logger = slog.Default()
	}

	resolvedPath := strings.TrimSpace(binaryPath)
	if resolvedPath == "" {
		resolvedPath = "/usr/local/bin/restic"
	}

	return &ResticRunner{
		binaryPath: resolvedPath,
		logger:     logger,
	}
}

// Init initializes a new Restic repository at the given target using the provided repository password.
func (r *ResticRunner) Init(ctx context.Context, target RepositoryTarget, password []byte) error {
	if target == nil {
		return errors.New("repository target is required")
	}
	if len(password) == 0 {
		return errors.New("repository password cannot be empty")
	}

	args := []string{"init"}
	_, err := r.runCommand(ctx, target, password, args)
	if err != nil {
		return fmt.Errorf("restic init failed: %w", err)
	}
	return nil
}

// Probe checks whether the repository at the given target exists and is readable using the provided password.
// It executes a non-destructive, read-only "cat config" command.
func (r *ResticRunner) Probe(ctx context.Context, target RepositoryTarget, password []byte) error {
	if target == nil {
		return errors.New("repository target is required")
	}
	if len(password) == 0 {
		return errors.New("repository password cannot be empty")
	}

	args := []string{"cat", "config"}
	_, err := r.runCommand(ctx, target, password, args)
	if err != nil {
		return fmt.Errorf("repository probe failed: %w", err)
	}
	return nil
}

// Version executes "restic version" and returns the version string.
func (r *ResticRunner) Version(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, r.binaryPath, "version")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("failed executing restic version: %w (stderr: %s)", err, stderr.String())
	}
	return strings.TrimSpace(stdout.String()), nil
}

// parseAndValidateResticVersion parses the output of "restic version" and asserts exact semantic version 0.19.1.
func parseAndValidateResticVersion(output string) error {
	fields := strings.Fields(strings.TrimSpace(output))
	if len(fields) < 2 || fields[0] != "restic" || fields[1] != "0.19.1" {
		return fmt.Errorf("unsupported restic version %q: expected exact version 0.19.1", output)
	}
	return nil
}

// ValidateVersion executes "restic version" and asserts that it matches the exact required version (0.19.1).
func (r *ResticRunner) ValidateVersion(ctx context.Context) error {
	v, err := r.Version(ctx)
	if err != nil {
		return fmt.Errorf("restic binary validation failed: %w", err)
	}
	return parseAndValidateResticVersion(v)
}

var (
	ErrSnapshotNotFound = errors.New("snapshot not found in repository")
)

// GetSnapshot retrieves the metadata for a specific snapshot by its ID.
func (r *ResticRunner) GetSnapshot(ctx context.Context, target RepositoryTarget, password []byte, snapshotID string) (*SnapshotItem, error) {
	if target == nil {
		return nil, errors.New("repository target is required")
	}
	if len(password) == 0 {
		return nil, errors.New("repository password cannot be empty")
	}
	cleanID := strings.TrimSpace(snapshotID)
	if cleanID == "" || strings.HasPrefix(cleanID, "-") {
		return nil, errors.New("invalid snapshot ID")
	}

	args := []string{"snapshots", cleanID, "--json"}
	out, err := r.runCommand(ctx, target, password, args)
	if err != nil {
		return nil, fmt.Errorf("failed retrieving snapshot: %w", err)
	}

	var snapshots []SnapshotItem
	if err := json.Unmarshal([]byte(out), &snapshots); err != nil {
		return nil, fmt.Errorf("malformed snapshot JSON: %w", err)
	}

	if len(snapshots) == 0 {
		return nil, ErrSnapshotNotFound
	}

	for _, s := range snapshots {
		if s.ID == cleanID || strings.HasPrefix(s.ID, cleanID) || s.ShortID == cleanID {
			return &s, nil
		}
	}

	return &snapshots[0], nil
}

// ListSnapshotNodes lists the files and directories inside a snapshot.
func (r *ResticRunner) ListSnapshotNodes(ctx context.Context, target RepositoryTarget, password []byte, snapshotID string) ([]SnapshotNode, error) {
	if target == nil {
		return nil, errors.New("repository target is required")
	}
	if len(password) == 0 {
		return nil, errors.New("repository password cannot be empty")
	}
	cleanID := strings.TrimSpace(snapshotID)
	if cleanID == "" || strings.HasPrefix(cleanID, "-") {
		return nil, errors.New("invalid snapshot ID")
	}

	args := []string{"ls", cleanID, "--json"}
	out, err := r.runCommand(ctx, target, password, args)
	if err != nil {
		return nil, fmt.Errorf("failed listing snapshot files: %w", err)
	}

	scanner := bufio.NewScanner(strings.NewReader(out))
	var nodes []SnapshotNode
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || !strings.HasPrefix(line, "{") {
			continue
		}
		var node SnapshotNode
		if err := json.Unmarshal([]byte(line), &node); err == nil && node.Name != "" {
			nodes = append(nodes, node)
		}
	}
	return nodes, nil
}

// DumpSample reads up to maxBytes from the specified file inside the snapshot using restic dump.
func (r *ResticRunner) DumpSample(ctx context.Context, target RepositoryTarget, password []byte, snapshotID, internalFilename string, maxBytes int) ([]byte, error) {
	if target == nil {
		return nil, errors.New("repository target is required")
	}
	if len(password) == 0 {
		return nil, errors.New("repository password cannot be empty")
	}
	cleanID := strings.TrimSpace(snapshotID)
	if cleanID == "" || strings.HasPrefix(cleanID, "-") {
		return nil, errors.New("invalid snapshot ID")
	}
	cleanFile := strings.TrimSpace(internalFilename)
	if cleanFile == "" || strings.HasPrefix(cleanFile, "-") {
		return nil, errors.New("invalid internal filename")
	}
	if maxBytes <= 0 {
		maxBytes = 64 * 1024
	}

	stream, err := r.DumpStream(ctx, target, password, cleanID, cleanFile)
	if err != nil {
		return nil, err
	}
	defer stream.Close()

	buf := make([]byte, maxBytes)
	n, err := io.ReadFull(stream, buf)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return nil, fmt.Errorf("failed reading dump sample: %w", err)
	}
	return buf[:n], nil
}

// dumpReadCloser wraps restic dump stdout and guarantees child process termination and cleanup on Close().
type dumpReadCloser struct {
	stdout    io.ReadCloser
	cmd       *exec.Cmd
	cancel    context.CancelFunc
	target    RepositoryTarget
	password  []byte
	waitDone  chan struct{}
	waitErr   error
	closeOnce sync.Once
}

func (d *dumpReadCloser) Read(p []byte) (int, error) {
	return d.stdout.Read(p)
}

func (d *dumpReadCloser) Close() error {
	var retErr error
	d.closeOnce.Do(func() {
		_ = d.stdout.Close()
		select {
		case <-d.waitDone:
			// Process already completed on its own
		default:
			d.cancel()
			if d.cmd.Process != nil {
				_ = d.cmd.Process.Kill()
			}
			<-d.waitDone
		}
		if d.waitErr != nil {
			errStr := d.waitErr.Error()
			if !errors.Is(d.waitErr, context.Canceled) &&
				!strings.Contains(errStr, "signal: killed") &&
				!strings.Contains(errStr, "Access is denied") {
				retErr = d.waitErr
			}
		}
		secretcrypto.ZeroBytes(d.password)
		if d.target != nil {
			d.target.Cleanup()
		}
	})
	return retErr
}

// DumpStream opens a streaming reader for the specified file inside the snapshot using restic dump.
func (r *ResticRunner) DumpStream(ctx context.Context, target RepositoryTarget, password []byte, snapshotID, internalFilename string) (io.ReadCloser, error) {
	if target == nil {
		return nil, errors.New("repository target is required")
	}
	if len(password) == 0 {
		return nil, errors.New("repository password cannot be empty")
	}
	cleanID := strings.TrimSpace(snapshotID)
	if cleanID == "" || strings.HasPrefix(cleanID, "-") {
		return nil, errors.New("invalid snapshot ID")
	}
	cleanFile := strings.TrimSpace(internalFilename)
	if cleanFile == "" || strings.HasPrefix(cleanFile, "-") {
		return nil, errors.New("invalid internal filename")
	}

	passwordCopy := make([]byte, len(password))
	copy(passwordCopy, password)

	baseEnv := filterCleanEnv(os.Environ())
	targetEnv := target.Env()
	repoURL := target.ResticRepositoryURL()

	childEnv := append(baseEnv,
		"RESTIC_REPOSITORY="+repoURL,
		"RESTIC_PASSWORD="+string(passwordCopy),
	)
	if len(targetEnv) > 0 {
		childEnv = append(childEnv, targetEnv...)
	}

	childCtx, childCancel := context.WithCancel(ctx)
	args := []string{"dump", cleanID, cleanFile}
	cmd := exec.CommandContext(childCtx, r.binaryPath, args...)
	cmd.Env = childEnv

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		childCancel()
		secretcrypto.ZeroBytes(passwordCopy)
		target.Cleanup()
		return nil, fmt.Errorf("failed creating dump stdout pipe: %w", err)
	}

	stderrBuf := newBoundedBuffer(MaxOutputBytes)
	cmd.Stderr = stderrBuf

	if err := cmd.Start(); err != nil {
		childCancel()
		_ = stdoutPipe.Close()
		secretcrypto.ZeroBytes(passwordCopy)
		target.Cleanup()
		sanitizedErr := sanitizeSecrets(err.Error(), string(password), targetEnv)
		return nil, fmt.Errorf("failed starting restic dump: %s", sanitizedErr)
	}

	waitDone := make(chan struct{})
	d := &dumpReadCloser{
		stdout:   stdoutPipe,
		cmd:      cmd,
		cancel:   childCancel,
		target:   target,
		password: passwordCopy,
		waitDone: waitDone,
	}

	go func() {
		wErr := cmd.Wait()
		if wErr != nil {
			d.waitErr = wErr
		}
		close(waitDone)
	}()

	return d, nil
}

// runCommand handles safe subprocess dispatch with child-only secret environment and sanitized output.
func (r *ResticRunner) runCommand(
	ctx context.Context,
	target RepositoryTarget,
	password []byte,
	args []string,
) (string, error) {
	// 1. Defensive copy of password for process execution
	passwordCopy := make([]byte, len(password))
	copy(passwordCopy, password)
	defer secretcrypto.ZeroBytes(passwordCopy)

	// 2. Clean filtered base environment without ambient RESTIC_*, AWS_*, or proxy variables
	baseEnv := filterCleanEnv(os.Environ())

	// 3. Capture target sensitive environment and repository URL before process launch/cleanup
	targetEnv := target.Env()
	repoURL := target.ResticRepositoryURL()

	// Child-only environment variables
	childEnv := append(baseEnv,
		"RESTIC_REPOSITORY="+repoURL,
		"RESTIC_PASSWORD="+string(passwordCopy),
	)
	if len(targetEnv) > 0 {
		childEnv = append(childEnv, targetEnv...)
	}

	// Subprocess command construction (NO shell interpolation, direct exec)
	cmd := exec.CommandContext(ctx, r.binaryPath, args...)
	cmd.Env = childEnv

	// Use bounded writers
	stdoutLimit := newBoundedBuffer(MaxOutputBytes)
	stderrLimit := newBoundedBuffer(MaxOutputBytes)
	cmd.Stdout = stdoutLimit
	cmd.Stderr = stderrLimit

	// 4. Execute subprocess
	runErr := cmd.Run()

	// 5. Sanitize stdout, stderr, and execution errors using original captured secrets BEFORE target.Cleanup()
	stdoutStr := sanitizeSecrets(stdoutLimit.String(), string(password), targetEnv)
	stderrStr := sanitizeSecrets(stderrLimit.String(), string(password), targetEnv)

	// 6. Zeroize temporary password copy
	secretcrypto.ZeroBytes(passwordCopy)

	// 7. Clean up target resources and zero in-memory credentials
	target.Cleanup()

	if runErr != nil {
		sanitizedErr := sanitizeSecrets(runErr.Error(), string(password), targetEnv)
		if stderrStr != "" {
			return stdoutStr, fmt.Errorf("%s: %s", sanitizedErr, stderrStr)
		}
		return stdoutStr, errors.New(sanitizedErr)
	}

	return stdoutStr, nil
}

// boundedBuffer prevents unbounded memory consumption by limiting written bytes.
type boundedBuffer struct {
	buf   bytes.Buffer
	limit int
}

func newBoundedBuffer(limit int) *boundedBuffer {
	return &boundedBuffer{limit: limit}
}

func (b *boundedBuffer) Write(p []byte) (n int, err error) {
	remaining := b.limit - b.buf.Len()
	if remaining <= 0 {
		return len(p), nil // discard overflow silently
	}
	if len(p) > remaining {
		_, _ = b.buf.Write(p[:remaining])
		return len(p), nil
	}
	return b.buf.Write(p)
}

func (b *boundedBuffer) String() string {
	return b.buf.String()
}

// filterCleanEnv removes any ambient RESTIC_*, AWS_*, or proxy variables from the host environment.
func filterCleanEnv(env []string) []string {
	var filtered []string
	for _, e := range env {
		upper := strings.ToUpper(e)
		if strings.HasPrefix(upper, "RESTIC_") ||
			strings.HasPrefix(upper, "AWS_") ||
			strings.HasPrefix(upper, "HTTP_PROXY=") ||
			strings.HasPrefix(upper, "HTTPS_PROXY=") ||
			strings.HasPrefix(upper, "ALL_PROXY=") ||
			strings.HasPrefix(upper, "NO_PROXY=") {
			continue
		}
		filtered = append(filtered, e)
	}
	return filtered
}

// sanitizeSecrets strips any sensitive password, AWS secret access key, session token, or key fragments from captured output/errors.
func sanitizeSecrets(input string, password string, envVars []string) string {
	result := input
	if password != "" {
		result = strings.ReplaceAll(result, password, "[REDACTED_PASSWORD]")
	}
	for _, ev := range envVars {
		parts := strings.SplitN(ev, "=", 2)
		if len(parts) == 2 && parts[1] != "" {
			keyUpper := strings.ToUpper(parts[0])
			if strings.Contains(keyUpper, "SECRET") ||
				strings.Contains(keyUpper, "TOKEN") ||
				strings.Contains(keyUpper, "PASSWORD") ||
				strings.Contains(keyUpper, "KEY") {
				result = strings.ReplaceAll(result, parts[1], "[REDACTED_SECRET]")
			}
		}
	}
	return result
}
