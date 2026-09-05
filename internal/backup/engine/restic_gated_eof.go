package engine

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
	"regexp"
	"strings"

	"backup-platform/internal/backup/domain"
	"backup-platform/internal/backup/restic"
	"backup-platform/internal/credential/secretcrypto"
	"backup-platform/pkg/uuid"
)

var (
	hexSnapshotIDRegex = regexp.MustCompile(`^[0-9a-f]{8,64}$`)
)

// Deterministic safe target token builder.
func BuildDeterministicTargetToken(backupType domain.BackupType, targetName string) string {
	clean := strings.TrimSpace(targetName)
	clean = strings.ReplaceAll(clean, "\\", "/")
	clean = strings.Trim(clean, "/")
	clean = strings.ReplaceAll(clean, "/", "_")

	var sb strings.Builder
	for _, r := range clean {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			sb.WriteRune(r)
		} else {
			sb.WriteRune('_')
		}
	}
	token := sb.String()
	token = strings.Trim(token, "_")
	if token == "" {
		token = "default"
	}
	if strings.HasPrefix(token, "-") {
		token = "target_" + token
	}
	return strings.ToLower(token)
}

// ResticBackupSummary represents the JSON summary output emitted by restic backup --json.
type ResticBackupSummary struct {
	MessageType         string `json:"message_type"`
	FilesNew            int    `json:"files_new"`
	FilesChanged        int    `json:"files_changed"`
	FilesUnmodified     int    `json:"files_unmodified"`
	DataBlobs           int    `json:"data_blobs"`
	TreeBlobs           int    `json:"tree_blobs"`
	DataAdded           int64  `json:"data_added"`
	TotalFilesProcessed int    `json:"total_files_processed"`
	TotalBytesProcessed int64  `json:"total_bytes_processed"`
	SnapshotID          string `json:"snapshot_id"`
}

// parseResticBackupSummary parses the JSON output from restic backup --json and returns the unique summary.
func parseResticBackupSummary(output string) (*ResticBackupSummary, error) {
	scanner := bufio.NewScanner(strings.NewReader(output))
	var summary *ResticBackupSummary

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || !strings.HasPrefix(line, "{") {
			continue
		}

		var obj map[string]any
		if err := json.Unmarshal([]byte(line), &obj); err != nil {
			continue // skip non-JSON or partial line
		}

		msgType, _ := obj["message_type"].(string)
		if msgType == "summary" {
			var s ResticBackupSummary
			if err := json.Unmarshal([]byte(line), &s); err != nil {
				return nil, fmt.Errorf("malformed restic summary event: %w", err)
			}
			if summary != nil && summary.SnapshotID != s.SnapshotID {
				return nil, errors.New("conflicting duplicate snapshot summary events in restic output")
			}
			summary = &s
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading restic output: %w", err)
	}

	if summary == nil {
		return nil, errors.New("missing summary event in restic backup output")
	}

	if summary.SnapshotID == "" {
		return nil, errors.New("restic backup summary missing snapshot_id")
	}

	if !hexSnapshotIDRegex.MatchString(summary.SnapshotID) {
		return nil, fmt.Errorf("invalid snapshot ID format: %q", summary.SnapshotID)
	}

	return summary, nil
}

// StdinBackupRequest encapsulates all parameters required for a Fail-Closed Gated EOF backup.
type StdinBackupRequest struct {
	Target           restic.RepositoryTarget
	Password         []byte
	OrgID            uuid.UUID
	ResourceID       uuid.UUID
	RunID            uuid.UUID
	ArtifactID       uuid.UUID
	BackupType       domain.BackupType
	TargetName       string
	InternalFilename string
	StreamProducer   func(ctx context.Context, stdin io.Writer) error
}

// ResticExecutionResult contains the metadata produced by a successful Restic backup.
type ResticExecutionResult struct {
	ArtifactID       uuid.UUID
	SnapshotID       string
	LogicalSizeBytes int64
	InternalFilename string
	TargetToken      string
}

// GatedEOFSupervisor coordinates the Fail-Closed Gated EOF streaming pipeline (ADR-031).
type GatedEOFSupervisor struct {
	binaryPath string
	logger     *slog.Logger
}

// NewGatedEOFSupervisor constructs a new GatedEOFSupervisor.
func NewGatedEOFSupervisor(binaryPath string, logger *slog.Logger) *GatedEOFSupervisor {
	if logger == nil {
		logger = slog.Default()
	}
	resolvedPath := strings.TrimSpace(binaryPath)
	if resolvedPath == "" {
		resolvedPath = "/usr/local/bin/restic"
	}
	return &GatedEOFSupervisor{
		binaryPath: resolvedPath,
		logger:     logger,
	}
}

// ExecuteBackup runs the child process with a writable stdin pipe, streams the producer data,
// and strictly enforces the Gated EOF protocol: Graceful EOF is ONLY sent if the producer returns err == nil.
func (s *GatedEOFSupervisor) ExecuteBackup(ctx context.Context, req StdinBackupRequest) (*ResticExecutionResult, error) {
	// 1. Pre-condition validations
	if req.Target == nil {
		return nil, errors.New("repository target is required")
	}
	if len(req.Password) == 0 {
		return nil, errors.New("repository password is required")
	}
	if req.ArtifactID == uuid.Nil {
		return nil, errors.New("artifact ID must be pre-generated before process launch")
	}
	if req.OrgID == uuid.Nil || req.ResourceID == uuid.Nil || req.RunID == uuid.Nil {
		return nil, errors.New("organization, resource, and run IDs are required")
	}
	if req.StreamProducer == nil {
		return nil, errors.New("stream producer function is required")
	}
	if req.BackupType != domain.BackupTypeMySQLDatabase && req.BackupType != domain.BackupTypeWebsiteFiles {
		return nil, errors.New("invalid backup type")
	}
	if strings.TrimSpace(req.TargetName) == "" {
		return nil, errors.New("target name cannot be empty")
	}
	trimmedFilename := strings.TrimSpace(req.InternalFilename)
	if trimmedFilename == "" || strings.HasPrefix(trimmedFilename, "-") || strings.Contains(trimmedFilename, "/") || strings.Contains(trimmedFilename, "\\") || strings.Contains(trimmedFilename, "..") {
		return nil, errors.New("invalid internal filename")
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// 2. Build deterministic target token and mandatory six snapshot tags (ADR-031)
	targetToken := BuildDeterministicTargetToken(req.BackupType, req.TargetName)
	tagPlatform := "platform=backup-platform-v1"
	tagOrg := "org=" + req.OrgID.String()
	tagRes := "resource=" + req.ResourceID.String()
	tagRun := "run=" + req.RunID.String()
	tagArt := "artifact=" + req.ArtifactID.String()
	tagTarget := "target=" + targetToken

	args := []string{
		"backup",
		"--stdin",
		"--stdin-filename", req.InternalFilename,
		"--tag", tagPlatform,
		"--tag", tagOrg,
		"--tag", tagRes,
		"--tag", tagRun,
		"--tag", tagArt,
		"--tag", tagTarget,
		"--json",
	}

	// 3. Child-only environment setup
	passwordCopy := make([]byte, len(req.Password))
	copy(passwordCopy, req.Password)
	defer secretcrypto.ZeroBytes(passwordCopy)

	targetEnv := req.Target.Env()
	repoURL := req.Target.ResticRepositoryURL()
	baseEnv := filterCleanEnv(os.Environ())

	childEnv := append(baseEnv,
		"RESTIC_REPOSITORY="+repoURL,
		"RESTIC_PASSWORD="+string(passwordCopy),
	)
	if len(targetEnv) > 0 {
		childEnv = append(childEnv, targetEnv...)
	}

	// 4. Create child command with cancellation
	childCtx, childCancel := context.WithCancel(ctx)
	defer childCancel()

	cmd := exec.CommandContext(childCtx, s.binaryPath, args...)
	cmd.Env = childEnv

	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("failed creating restic stdin pipe: %w", err)
	}

	stdoutBuf := newBoundedBuffer(restic.MaxOutputBytes)
	stderrBuf := newBoundedBuffer(restic.MaxOutputBytes)
	cmd.Stdout = stdoutBuf
	cmd.Stderr = stderrBuf

	// 5. Start Restic process
	if err := cmd.Start(); err != nil {
		_ = stdinPipe.Close()
		return nil, fmt.Errorf("failed starting restic process: %w", err)
	}

	// 6. Producer execution in dedicated goroutine with panic recovery
	prodCtx, prodCancel := context.WithCancel(childCtx)
	defer prodCancel()

	type prodOutcome struct {
		err      error
		panicked bool
	}
	prodChan := make(chan prodOutcome, 1)

	go func() {
		defer func() {
			if r := recover(); r != nil {
				prodChan <- prodOutcome{
					err:      fmt.Errorf("panic in backup stream producer: %v", r),
					panicked: true,
				}
			}
		}()
		pErr := req.StreamProducer(prodCtx, stdinPipe)
		prodChan <- prodOutcome{err: pErr, panicked: false}
	}()

	// 7. Supervisor waiting on producer completion or context cancellation
	var outcome prodOutcome
	select {
	case <-ctx.Done():
		// Context cancellation / timeout: Kill child, close pipe, reap child, wait producer
		childCancel()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = stdinPipe.Close()
		_ = cmd.Wait()
		prodCancel()
		<-prodChan
		return nil, ctx.Err()

	case outcome = <-prodChan:
		// Producer completed
	}

	// 8. Gated EOF Gatekeeper:
	// If producer panicked or returned an error: DO NOT SEND EOF!
	// Hard-terminate Restic immediately, close pipe, reap child.
	if outcome.panicked || outcome.err != nil {
		childCancel()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = stdinPipe.Close()
		_ = cmd.Wait()

		if outcome.panicked {
			return nil, errors.New("backup stream producer panicked during execution")
		}
		return nil, fmt.Errorf("backup streaming failed: %w", outcome.err)
	}

	// 9. Connector succeeded (err == nil, no panic):
	// Now and ONLY now, supervisor closes Restic STDIN pipe (Graceful EOF).
	if closeErr := stdinPipe.Close(); closeErr != nil {
		childCancel()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
		return nil, fmt.Errorf("failed closing restic stdin: %w", closeErr)
	}

	// 10. Wait for Restic child process to exit
	waitErr := cmd.Wait()
	childCancel()

	if waitErr != nil {
		sanitizedStderr := sanitizeSecrets(stderrBuf.String(), string(req.Password), targetEnv)
		return nil, fmt.Errorf("restic backup process failed: %w (stderr: %s)", waitErr, sanitizedStderr)
	}

	// 11. Parse and validate JSON summary output
	summary, parseErr := parseResticBackupSummary(stdoutBuf.String())
	if parseErr != nil {
		return nil, fmt.Errorf("failed parsing restic summary output: %w", parseErr)
	}

	if summary.TotalBytesProcessed <= 0 && summary.FilesNew == 0 {
		return nil, errors.New("restic backup produced empty logical snapshot")
	}

	return &ResticExecutionResult{
		ArtifactID:       req.ArtifactID,
		SnapshotID:       summary.SnapshotID,
		LogicalSizeBytes: summary.TotalBytesProcessed,
		InternalFilename: req.InternalFilename,
		TargetToken:      targetToken,
	}, nil
}

// boundedBuffer for safe subprocess stdout/stderr capture
type boundedBuffer struct {
	buf   bytes.Buffer
	limit int
}

func newBoundedBuffer(limit int) *boundedBuffer {
	return &boundedBuffer{limit: limit}
}

func (b *boundedBuffer) Write(p []byte) (n int, err error) {
	rem := b.limit - b.buf.Len()
	if rem <= 0 {
		return len(p), nil
	}
	if len(p) > rem {
		_, _ = b.buf.Write(p[:rem])
		return len(p), nil
	}
	return b.buf.Write(p)
}

func (b *boundedBuffer) String() string {
	return b.buf.String()
}

func filterCleanEnv(env []string) []string {
	var out []string
	for _, e := range env {
		u := strings.ToUpper(e)
		if strings.HasPrefix(u, "RESTIC_") ||
			strings.HasPrefix(u, "AWS_") ||
			strings.HasPrefix(u, "HTTP_PROXY=") ||
			strings.HasPrefix(u, "HTTPS_PROXY=") ||
			strings.HasPrefix(u, "ALL_PROXY=") ||
			strings.HasPrefix(u, "NO_PROXY=") {
			continue
		}
		out = append(out, e)
	}
	return out
}

func sanitizeSecrets(input, password string, envVars []string) string {
	res := input
	if password != "" {
		res = strings.ReplaceAll(res, password, "[REDACTED_PASSWORD]")
	}
	for _, ev := range envVars {
		parts := strings.SplitN(ev, "=", 2)
		if len(parts) == 2 && parts[1] != "" {
			k := strings.ToUpper(parts[0])
			if strings.Contains(k, "SECRET") ||
				strings.Contains(k, "TOKEN") ||
				strings.Contains(k, "PASSWORD") ||
				strings.Contains(k, "KEY") {
				res = strings.ReplaceAll(res, parts[1], "[REDACTED_SECRET]")
			}
		}
	}
	return res
}
