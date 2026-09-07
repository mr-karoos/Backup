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
	"sync"
	"time"

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

const (
	// MaxBackupJSONLineBytes is the maximum allowed length for a single JSON line/event from restic backup --json.
	// Single events exceeding this limit fail closed to prevent memory exhaustion.
	MaxBackupJSONLineBytes = 64 * 1024
)

// StreamParseResticBackupStdout concurrently parses JSON lines from restic backup --json stdout.
// It fails closed on any malformed JSON event, oversized event, duplicate summary, missing summary,
// or invalid non-64-hex snapshot ID. Progress and status events are discarded immediately to preserve bounded memory.
func StreamParseResticBackupStdout(r io.Reader) (*ResticBackupSummary, error) {
	scanner := bufio.NewScanner(r)
	buf := make([]byte, 16*1024)
	scanner.Buffer(buf, MaxBackupJSONLineBytes)

	var summary *ResticBackupSummary

	for scanner.Scan() {
		lineBytes := bytes.TrimSpace(scanner.Bytes())
		if len(lineBytes) == 0 {
			continue
		}

		// Must be valid JSON
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(lineBytes, &raw); err != nil {
			return nil, fmt.Errorf("malformed JSON in restic backup output: %w", err)
		}

		var msgType string
		if mtRaw, ok := raw["message_type"]; ok {
			_ = json.Unmarshal(mtRaw, &msgType)
		}

		switch msgType {
		case "summary":
			if summary != nil {
				return nil, errors.New("conflicting duplicate snapshot summary events in restic output")
			}
			var s ResticBackupSummary
			if err := json.Unmarshal(lineBytes, &s); err != nil {
				return nil, fmt.Errorf("malformed restic summary event: %w", err)
			}
			if s.SnapshotID == "" {
				return nil, errors.New("restic backup summary missing snapshot_id")
			}
			if !domain.IsValidCanonicalResticSnapshotID(s.SnapshotID) {
				return nil, fmt.Errorf("invalid snapshot ID format: expected 64 lowercase hex characters, got %q", s.SnapshotID)
			}
			summary = &s

		case "status":
			// Valid status/progress event - discard immediately to maintain bounded memory
			continue

		default:
			// Other valid JSON event types - discard immediately
			continue
		}
	}

	if err := scanner.Err(); err != nil {
		if errors.Is(err, bufio.ErrTooLong) {
			return nil, fmt.Errorf("oversized hostile event in restic backup output exceeding %d bytes limit: %w", MaxBackupJSONLineBytes, err)
		}
		return nil, fmt.Errorf("error reading restic backup stdout: %w", err)
	}

	if summary == nil {
		return nil, errors.New("missing summary event in restic backup output")
	}

	return summary, nil
}

// parseResticBackupSummary parses the JSON output from restic backup --json and returns the unique summary.
func parseResticBackupSummary(output string) (*ResticBackupSummary, error) {
	return StreamParseResticBackupStdout(strings.NewReader(output))
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

	stdoutReader, stdoutWriter, err := os.Pipe()
	if err != nil {
		_ = stdinPipe.Close()
		return nil, fmt.Errorf("failed creating restic stdout pipe: %w", err)
	}
	cmd.Stdout = stdoutWriter

	stderrBuf := newBoundedBuffer(restic.MaxOutputBytes)
	cmd.Stderr = stderrBuf

	// 5. Start Restic process
	if err := cmd.Start(); err != nil {
		_ = stdinPipe.Close()
		_ = stdoutWriter.Close()
		_ = stdoutReader.Close()
		return nil, fmt.Errorf("failed starting restic process: %w", err)
	}
	// The parent must close its descriptor for the write end of the pipe
	// immediately after process launch so that the reader observes EOF
	// when the child exits.
	_ = stdoutWriter.Close()

	// Safe closer for stdoutReader to prevent double close and unblock reads on abort
	var stdoutReaderOnce sync.Once
	safeCloseStdoutReader := func() error {
		var closeErr error
		stdoutReaderOnce.Do(func() {
			closeErr = stdoutReader.Close()
		})
		return closeErr
	}
	defer safeCloseStdoutReader()

	// Channel for concurrent streaming stdout JSON parser
	type stdoutResult struct {
		summary *ResticBackupSummary
		err     error
	}
	stdoutDoneChan := make(chan stdoutResult, 1)
	go func() {
		defer safeCloseStdoutReader()
		s, sErr := StreamParseResticBackupStdout(stdoutReader)
		stdoutDoneChan <- stdoutResult{summary: s, err: sErr}
	}()

	// Channel for child process completion (cmd.Wait() is called in exactly ONE goroutine)
	type childOutcome struct {
		err error
	}
	childDoneChan := make(chan childOutcome, 1)
	go func() {
		wErr := cmd.Wait()
		childDoneChan <- childOutcome{err: wErr}
	}()

	// Safe closer for stdinPipe to prevent double close
	var stdinOnce sync.Once
	safeCloseStdin := func() error {
		var closeErr error
		stdinOnce.Do(func() {
			closeErr = stdinPipe.Close()
		})
		return closeErr
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

	// 7. Concurrent State Machine: supervisor concurrently accounts for:
	// - producer result (prodChan)
	// - child process result (childDoneChan)
	// - stdout parser result (stdoutDoneChan)
	// - ctx.Done()

	var (
		parsedSummary *ResticBackupSummary
		parserErr     error
		prodDone      bool
	)

	for {
		select {
		case stdoutRes := <-stdoutDoneChan:
			stdoutDoneChan = nil // do not select again
			if stdoutRes.err != nil {
				// Parser error before successful completion is fatal!
				parserErr = stdoutRes.err
				// 1. cancel producer context immediately
				prodCancel()
				// 2. initiate hard termination of Restic
				if cmd.Process != nil {
					_ = cmd.Process.Kill()
				}
				// 3. unblock producer writer safely
				_ = safeCloseStdin()
				// 4. reap child
				childRes := <-childDoneChan
				// 5. boundedly wait for producer shutdown
				if !prodDone {
					select {
					case <-prodChan:
					case <-time.After(3 * time.Second):
					}
				}
				if childRes.err != nil && strings.Contains(parserErr.Error(), "missing summary event") {
					sanitizedStderr := sanitizeSecrets(stderrBuf.String(), string(req.Password), targetEnv)
					return nil, fmt.Errorf("restic backup process failed: %w (stderr: %s)", childRes.err, sanitizedStderr)
				}
				return nil, fmt.Errorf("failed parsing restic summary output: %w", parserErr)
			}
			parsedSummary = stdoutRes.summary
			// Parser completed successfully before child/producer exit: retain summary and continue loop
			continue

		case outcome := <-prodChan:
			prodChan = nil
			prodDone = true
			if outcome.panicked || outcome.err != nil {
				// Producer failure or panic:
				// DO NOT SEND GRACEFUL EOF!
				// 1. Hard kill Restic child immediately
				if cmd.Process != nil {
					_ = cmd.Process.Kill()
				}
				// 2. Reap child
				<-childDoneChan
				// 3. Cancel producer context
				prodCancel()
				// 4. Close parent pipe only AFTER live child can no longer observe graceful EOF
				_ = safeCloseStdin()
				// 5. Close stdout reader and drain stdout scanner if still active
				_ = safeCloseStdoutReader()
				if stdoutDoneChan != nil {
					<-stdoutDoneChan
				}

				if outcome.panicked {
					return nil, errors.New("backup stream producer panicked during execution")
				}
				return nil, fmt.Errorf("backup streaming failed: %w", outcome.err)
			}

			// Producer succeeded (err == nil, no panic):
			// Now and ONLY now, supervisor closes Restic STDIN pipe (Graceful EOF).
			if closeErr := safeCloseStdin(); closeErr != nil {
				prodCancel()
				if cmd.Process != nil {
					_ = cmd.Process.Kill()
				}
				_ = safeCloseStdoutReader()
				<-childDoneChan
				if stdoutDoneChan != nil {
					<-stdoutDoneChan
				}
				return nil, fmt.Errorf("failed closing restic stdin: %w", closeErr)
			}

			// Producer has finished and graceful EOF was sent.
			// Now wait for child process and/or stdout parser to complete.
			for childDoneChan != nil || (stdoutDoneChan != nil && parsedSummary == nil) {
				select {
				case stdoutRes := <-stdoutDoneChan:
					stdoutDoneChan = nil
					if stdoutRes.err != nil {
						// Parser error while waiting for child to exit
						prodCancel()
						if cmd.Process != nil {
							_ = cmd.Process.Kill()
						}
						if childDoneChan != nil {
							childRes := <-childDoneChan
							childDoneChan = nil
							if childRes.err != nil && strings.Contains(stdoutRes.err.Error(), "missing summary event") {
								sanitizedStderr := sanitizeSecrets(stderrBuf.String(), string(req.Password), targetEnv)
								return nil, fmt.Errorf("restic backup process failed: %w (stderr: %s)", childRes.err, sanitizedStderr)
							}
						}
						return nil, fmt.Errorf("failed parsing restic summary output: %w", stdoutRes.err)
					}
					parsedSummary = stdoutRes.summary

				case childRes := <-childDoneChan:
					childDoneChan = nil
					// Child exited. If stdout is still reading, await its termination.
					if stdoutDoneChan != nil {
						stdoutRes := <-stdoutDoneChan
						stdoutDoneChan = nil
						if stdoutRes.err != nil {
							if childRes.err != nil && strings.Contains(stdoutRes.err.Error(), "missing summary event") {
								sanitizedStderr := sanitizeSecrets(stderrBuf.String(), string(req.Password), targetEnv)
								return nil, fmt.Errorf("restic backup process failed: %w (stderr: %s)", childRes.err, sanitizedStderr)
							}
							return nil, fmt.Errorf("failed parsing restic summary output: %w", stdoutRes.err)
						}
						parsedSummary = stdoutRes.summary
					}

					childCancel()

					if childRes.err != nil {
						sanitizedStderr := sanitizeSecrets(stderrBuf.String(), string(req.Password), targetEnv)
						return nil, fmt.Errorf("restic backup process failed: %w (stderr: %s)", childRes.err, sanitizedStderr)
					}

					if parsedSummary == nil {
						return nil, errors.New("missing summary event in restic backup output")
					}
					if parsedSummary.TotalBytesProcessed <= 0 && parsedSummary.FilesNew == 0 {
						return nil, errors.New("restic backup produced empty logical snapshot")
					}

					return &ResticExecutionResult{
						ArtifactID:       req.ArtifactID,
						SnapshotID:       parsedSummary.SnapshotID,
						LogicalSizeBytes: parsedSummary.TotalBytesProcessed,
						InternalFilename: req.InternalFilename,
						TargetToken:      targetToken,
					}, nil

				case <-ctx.Done():
					prodCancel()
					if cmd.Process != nil {
						_ = cmd.Process.Kill()
					}
					_ = safeCloseStdoutReader()
					if childDoneChan != nil {
						<-childDoneChan
					}
					if stdoutDoneChan != nil {
						<-stdoutDoneChan
					}
					return nil, ctx.Err()
				}
			}

		case childRes := <-childDoneChan:
			// Restic child exited prematurely while producer was still active!
			// 1. Immediately cancel producer context
			prodCancel()
			// 2. Close stdin pipe to unblock any blocked write
			_ = safeCloseStdin()
			// 3. Bounded wait for producer to stop
			if !prodDone {
				select {
				case <-prodChan:
				case <-time.After(3 * time.Second):
				}
			}
			// 4. Drain stdout scanner if not already done
			_ = safeCloseStdoutReader()
			if stdoutDoneChan != nil {
				<-stdoutDoneChan
			}

			sanitizedStderr := sanitizeSecrets(stderrBuf.String(), string(req.Password), targetEnv)
			if childRes.err != nil {
				return nil, fmt.Errorf("restic backup process exited prematurely: %w (stderr: %s)", childRes.err, sanitizedStderr)
			}
			return nil, fmt.Errorf("restic backup process exited prematurely with code 0 (stderr: %s)", sanitizedStderr)

		case <-ctx.Done():
			// Context cancellation while producer and child were running
			prodCancel()
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
			_ = safeCloseStdin()
			_ = safeCloseStdoutReader()
			<-childDoneChan
			if !prodDone {
				select {
				case <-prodChan:
				case <-time.After(3 * time.Second):
				}
			}
			if stdoutDoneChan != nil {
				<-stdoutDoneChan
			}
			return nil, ctx.Err()
		}
	}
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
