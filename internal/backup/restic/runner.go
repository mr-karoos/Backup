package restic

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"

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
