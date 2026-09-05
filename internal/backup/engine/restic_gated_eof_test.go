package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"backup-platform/internal/backup/domain"
	"backup-platform/pkg/uuid"
)

// mockTarget implements restic.RepositoryTarget for supervisor testing.
type mockTarget struct {
	url     string
	env     []string
	locator string
}

func (m *mockTarget) Type() string {
	return "local"
}

func (m *mockTarget) ResticRepositoryURL() string {
	return m.url
}

func (m *mockTarget) Env() []string {
	return m.env
}

func (m *mockTarget) Locator() string {
	return m.locator
}

func (m *mockTarget) Cleanup() {}

var (
	mockBinaryPath string
	mockBinaryOnce sync.Once
)

func buildMockResticBinary(t *testing.T) string {
	mockBinaryOnce.Do(func() {
		tmpDir, err := os.MkdirTemp("", "mock-restic-*")
		if err != nil {
			panic(fmt.Sprintf("failed creating temp dir for mock restic: %v", err))
		}

		srcPath := filepath.Join(tmpDir, "main.go")
		binName := "restic"
		if strings.ToLower(os.Getenv("GOOS")) == "windows" || os.PathSeparator == '\\' {
			binName = "restic.exe"
		}
		binPath := filepath.Join(tmpDir, binName)

		code := `package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"
)

func main() {
	mode := os.Getenv("MOCK_RESTIC_MODE")
	if mode == "" {
		mode = "success"
	}

	switch mode {
	case "fail_exit_1":
		fmt.Fprintf(os.Stderr, "Fatal: unable to open repository at %s\n", os.Getenv("RESTIC_REPOSITORY"))
		os.Exit(1)

	case "leak_secrets_stderr":
		pw := os.Getenv("RESTIC_PASSWORD")
		sec := os.Getenv("AWS_SECRET_ACCESS_KEY")
		fmt.Fprintf(os.Stderr, "Authentication error with password %s and secret %s\n", pw, sec)
		os.Exit(1)

	case "corrupt_json":
		fmt.Println("{malformed-json-here")
		os.Exit(0)

	case "no_summary":
		fmt.Println("{\"message_type\":\"status\",\"percent_done\":0.5}")
		os.Exit(0)

	case "missing_snapshot_id":
		fmt.Println("{\"message_type\":\"summary\",\"files_new\":1,\"total_bytes_processed\":1024}")
		os.Exit(0)

	case "invalid_snapshot_id":
		fmt.Println("{\"message_type\":\"summary\",\"files_new\":1,\"snapshot_id\":\"not-a-valid-hex-id\",\"total_bytes_processed\":1024}")
		os.Exit(0)

	case "conflicting_summaries":
		fmt.Println("{\"message_type\":\"summary\",\"files_new\":1,\"snapshot_id\":\"1111111111111111111111111111111111111111111111111111111111111111\",\"total_bytes_processed\":1024}")
		fmt.Println("{\"message_type\":\"summary\",\"files_new\":1,\"snapshot_id\":\"2222222222222222222222222222222222222222222222222222222222222222\",\"total_bytes_processed\":1024}")
		os.Exit(0)

	case "duplicate_identical_summaries":
		fmt.Println("{\"message_type\":\"summary\",\"files_new\":1,\"snapshot_id\":\"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\",\"total_bytes_processed\":1024}")
		fmt.Println("{\"message_type\":\"summary\",\"files_new\":1,\"snapshot_id\":\"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\",\"total_bytes_processed\":1024}")
		os.Exit(0)

	case "dump_args":
		f, _ := os.Create(os.Getenv("MOCK_ARGS_FILE"))
		if f != nil {
			for _, a := range os.Args {
				f.WriteString(a + "\n")
			}
			f.Close()
		}
		fmt.Println("{\"message_type\":\"summary\",\"files_new\":1,\"snapshot_id\":\"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\",\"total_bytes_processed\":1024}")
		os.Exit(0)

	case "empty_output":
		os.Exit(0)

	case "eof_detector":
		// Reads stdin until EOF. If EOF received, writes "EOF_RECEIVED" to MOCK_EOF_FILE
		buf := make([]byte, 1024)
		for {
			_, err := os.Stdin.Read(buf)
			if err == io.EOF {
				f, _ := os.Create(os.Getenv("MOCK_EOF_FILE"))
				if f != nil {
					f.WriteString("EOF_RECEIVED\n")
					f.Close()
				}
				break
			}
			if err != nil {
				break
			}
		}
		fmt.Println("{\"message_type\":\"summary\",\"files_new\":1,\"snapshot_id\":\"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef\",\"total_bytes_processed\":2048}")
		os.Exit(0)

	case "hang_stdin":
		// Hangs reading stdin until process killed or EOF
		buf := make([]byte, 1024)
		for {
			_, err := os.Stdin.Read(buf)
			if err != nil {
				break
			}
		}
		time.Sleep(10 * time.Second)
		os.Exit(0)

	default: // "success"
		// Read all stdin
		scanner := bufio.NewScanner(os.Stdin)
		totalBytes := int64(0)
		for scanner.Scan() {
			totalBytes += int64(len(scanner.Bytes()))
		}
		summary := map[string]any{
			"message_type":          "summary",
			"files_new":             1,
			"files_changed":         0,
			"files_unmodified":      0,
			"data_blobs":            1,
			"tree_blobs":            1,
			"data_added":            totalBytes,
			"total_files_processed": 1,
			"total_bytes_processed": totalBytes,
			"snapshot_id":           "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		}
		out, _ := json.Marshal(summary)
		fmt.Println(string(out))
		os.Exit(0)
	}
}
`
		if err := os.WriteFile(srcPath, []byte(code), 0600); err != nil {
			panic(fmt.Sprintf("failed writing mock restic source: %v", err))
		}

		cmd := exec.Command("go", "build", "-o", binPath, srcPath)
		cmd.Env = os.Environ()
		out, err := cmd.CombinedOutput()
		if err != nil {
			panic(fmt.Sprintf("failed building mock restic: %v\nOutput: %s", err, string(out)))
		}

		mockBinaryPath = binPath
	})
	return mockBinaryPath
}

func TestGatedEOFSupervisor_Matrix_A_Through_Y(t *testing.T) {
	mockBin := buildMockResticBinary(t)
	supervisor := NewGatedEOFSupervisor(mockBin, slog.Default())

	validTarget := &mockTarget{
		url:     "local:/tmp/test-repo",
		env:     []string{"AWS_SECRET_ACCESS_KEY=my-super-secret-aws-key"},
		locator: "test-locator",
	}
	validPassword := []byte("top-secret-restic-password-123")
	orgID := uuid.New()
	resID := uuid.New()
	runID := uuid.New()
	artID := uuid.New()

	baseReq := func() StdinBackupRequest {
		return StdinBackupRequest{
			Target:           validTarget,
			Password:         validPassword,
			OrgID:            orgID,
			ResourceID:       resID,
			RunID:            runID,
			ArtifactID:       artID,
			BackupType:       domain.BackupTypeMySQLDatabase,
			TargetName:       "production_db",
			InternalFilename: "production_db.sql",
			StreamProducer: func(ctx context.Context, stdin io.Writer) error {
				_, err := stdin.Write([]byte("CREATE TABLE users (id INT);\n"))
				return err
			},
		}
	}

	// Matrix A: Nil Target -> error
	t.Run("Scenario_A_NilTarget", func(t *testing.T) {
		req := baseReq()
		req.Target = nil
		_, err := supervisor.ExecuteBackup(context.Background(), req)
		if err == nil || !strings.Contains(err.Error(), "repository target is required") {
			t.Fatalf("expected repository target is required error, got: %v", err)
		}
	})

	// Matrix B: Empty Password -> error
	t.Run("Scenario_B_EmptyPassword", func(t *testing.T) {
		req := baseReq()
		req.Password = nil
		_, err := supervisor.ExecuteBackup(context.Background(), req)
		if err == nil || !strings.Contains(err.Error(), "repository password is required") {
			t.Fatalf("expected repository password is required error, got: %v", err)
		}
	})

	// Matrix C: Nil ArtifactID -> error
	t.Run("Scenario_C_NilArtifactID", func(t *testing.T) {
		req := baseReq()
		req.ArtifactID = uuid.Nil
		_, err := supervisor.ExecuteBackup(context.Background(), req)
		if err == nil || !strings.Contains(err.Error(), "artifact ID must be pre-generated") {
			t.Fatalf("expected artifact ID error, got: %v", err)
		}
	})

	// Matrix D: Invalid BackupType -> error
	t.Run("Scenario_D_InvalidBackupType", func(t *testing.T) {
		req := baseReq()
		req.BackupType = domain.BackupType("invalid_type")
		_, err := supervisor.ExecuteBackup(context.Background(), req)
		if err == nil || !strings.Contains(err.Error(), "invalid backup type") {
			t.Fatalf("expected invalid backup type error, got: %v", err)
		}
	})

	// Matrix E: Empty TargetName -> error
	t.Run("Scenario_E_EmptyTargetName", func(t *testing.T) {
		req := baseReq()
		req.TargetName = "   "
		_, err := supervisor.ExecuteBackup(context.Background(), req)
		if err == nil || !strings.Contains(err.Error(), "target name cannot be empty") {
			t.Fatalf("expected target name cannot be empty error, got: %v", err)
		}
	})

	// Matrix F: Empty InternalFilename -> error
	t.Run("Scenario_F_EmptyInternalFilename", func(t *testing.T) {
		req := baseReq()
		req.InternalFilename = " "
		_, err := supervisor.ExecuteBackup(context.Background(), req)
		if err == nil || !strings.Contains(err.Error(), "invalid internal filename") {
			t.Fatalf("expected invalid internal filename error, got: %v", err)
		}
	})

	// Matrix G: Path traversal in InternalFilename -> error
	t.Run("Scenario_G_PathTraversalInternalFilename", func(t *testing.T) {
		req := baseReq()
		req.InternalFilename = "../escaped.sql"
		_, err := supervisor.ExecuteBackup(context.Background(), req)
		if err == nil || !strings.Contains(err.Error(), "invalid internal filename") {
			t.Fatalf("expected invalid internal filename error, got: %v", err)
		}
	})

	// Matrix H: Pre-canceled context -> returns ctx.Err()
	t.Run("Scenario_H_PreCanceledContext", func(t *testing.T) {
		req := baseReq()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := supervisor.ExecuteBackup(ctx, req)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got: %v", err)
		}
	})

	// Matrix I: Non-existent binary path -> returns exec error
	t.Run("Scenario_I_NonExistentBinary", func(t *testing.T) {
		badSupervisor := NewGatedEOFSupervisor("/path/to/non/existent/restic-binary-xyz", slog.Default())
		req := baseReq()
		_, err := badSupervisor.ExecuteBackup(context.Background(), req)
		if err == nil || !strings.Contains(err.Error(), "failed starting restic process") {
			t.Fatalf("expected failed starting restic process error, got: %v", err)
		}
	})

	// Matrix J: Producer returns error -> supervisor kills child, returns producer error
	t.Run("Scenario_J_ProducerReturnsError", func(t *testing.T) {
		t.Setenv("MOCK_RESTIC_MODE", "hang_stdin")
		producerErr := errors.New("database connector stream failed midway")
		req := baseReq()
		req.StreamProducer = func(ctx context.Context, stdin io.Writer) error {
			_, _ = stdin.Write([]byte("partial-data-before-crash"))
			return producerErr
		}
		_, err := supervisor.ExecuteBackup(context.Background(), req)
		if err == nil || !errors.Is(err, producerErr) {
			t.Fatalf("expected producer error %v, got: %v", producerErr, err)
		}
	})

	// Matrix K: Producer panics -> supervisor recovers panic, kills child, returns panic error
	t.Run("Scenario_K_ProducerPanics", func(t *testing.T) {
		t.Setenv("MOCK_RESTIC_MODE", "hang_stdin")
		req := baseReq()
		req.StreamProducer = func(ctx context.Context, stdin io.Writer) error {
			_, _ = stdin.Write([]byte("some data before panic"))
			panic("simulated critical nil pointer dereference in stream provider")
		}
		_, err := supervisor.ExecuteBackup(context.Background(), req)
		if err == nil || !strings.Contains(err.Error(), "backup stream producer panicked") {
			t.Fatalf("expected panic recovery error, got: %v", err)
		}
	})

	// Matrix L: Context canceled while streaming -> child process killed, returns context error
	t.Run("Scenario_L_ContextCanceledDuringStream", func(t *testing.T) {
		t.Setenv("MOCK_RESTIC_MODE", "hang_stdin")
		ctx, cancel := context.WithCancel(context.Background())
		req := baseReq()
		req.StreamProducer = func(pCtx context.Context, stdin io.Writer) error {
			_, _ = stdin.Write([]byte("streaming chunk 1"))
			cancel() // cancel parent context
			<-pCtx.Done()
			return pCtx.Err()
		}
		_, err := supervisor.ExecuteBackup(ctx, req)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got: %v", err)
		}
	})

	// Matrix M: Child process exits non-zero (exit code 1) -> returns error with stderr parsed
	t.Run("Scenario_M_ChildExitNonZero", func(t *testing.T) {
		t.Setenv("MOCK_RESTIC_MODE", "fail_exit_1")
		req := baseReq()
		_, err := supervisor.ExecuteBackup(context.Background(), req)
		if err == nil || !strings.Contains(err.Error(), "Fatal: unable to open repository") {
			t.Fatalf("expected exit error with stderr output, got: %v", err)
		}
	})

	// Matrix N: Child process outputs nothing (empty output) -> returns missing summary
	t.Run("Scenario_N_ChildEmptyOutput", func(t *testing.T) {
		t.Setenv("MOCK_RESTIC_MODE", "empty_output")
		req := baseReq()
		_, err := supervisor.ExecuteBackup(context.Background(), req)
		if err == nil || !strings.Contains(err.Error(), "missing summary event in restic backup output") {
			t.Fatalf("expected missing summary event error, got: %v", err)
		}
	})

	// Matrix O: Child process outputs non-JSON garbage -> returns missing summary
	t.Run("Scenario_O_ChildMalformedJSON", func(t *testing.T) {
		t.Setenv("MOCK_RESTIC_MODE", "corrupt_json")
		req := baseReq()
		_, err := supervisor.ExecuteBackup(context.Background(), req)
		if err == nil || !strings.Contains(err.Error(), "missing summary event in restic backup output") {
			t.Fatalf("expected missing summary event error, got: %v", err)
		}
	})

	// Matrix P: Child process outputs JSON without message_type summary -> returns missing summary
	t.Run("Scenario_P_NoSummaryEventType", func(t *testing.T) {
		t.Setenv("MOCK_RESTIC_MODE", "no_summary")
		req := baseReq()
		_, err := supervisor.ExecuteBackup(context.Background(), req)
		if err == nil || !strings.Contains(err.Error(), "missing summary event in restic backup output") {
			t.Fatalf("expected missing summary event error, got: %v", err)
		}
	})

	// Matrix Q: Child process outputs summary with empty snapshot_id -> returns error
	t.Run("Scenario_Q_MissingSnapshotID", func(t *testing.T) {
		t.Setenv("MOCK_RESTIC_MODE", "missing_snapshot_id")
		req := baseReq()
		_, err := supervisor.ExecuteBackup(context.Background(), req)
		if err == nil || !strings.Contains(err.Error(), "restic backup summary missing snapshot_id") {
			t.Fatalf("expected restic backup summary missing snapshot_id error, got: %v", err)
		}
	})

	// Matrix R: Child process outputs summary with non-hex snapshot_id -> returns invalid snapshot ID format
	t.Run("Scenario_R_InvalidSnapshotIDFormat", func(t *testing.T) {
		t.Setenv("MOCK_RESTIC_MODE", "invalid_snapshot_id")
		req := baseReq()
		_, err := supervisor.ExecuteBackup(context.Background(), req)
		if err == nil || !strings.Contains(err.Error(), "invalid snapshot ID format") {
			t.Fatalf("expected invalid snapshot ID format error, got: %v", err)
		}
	})

	// Matrix S: Child process outputs conflicting duplicate summaries -> returns error
	t.Run("Scenario_S_ConflictingDuplicateSummaries", func(t *testing.T) {
		t.Setenv("MOCK_RESTIC_MODE", "conflicting_summaries")
		req := baseReq()
		_, err := supervisor.ExecuteBackup(context.Background(), req)
		if err == nil || !strings.Contains(err.Error(), "conflicting duplicate snapshot summary events") {
			t.Fatalf("expected conflicting duplicate snapshot summary events error, got: %v", err)
		}
	})

	// Matrix T: Child process outputs identical duplicate summaries -> accepted
	t.Run("Scenario_T_IdenticalDuplicateSummaries", func(t *testing.T) {
		t.Setenv("MOCK_RESTIC_MODE", "duplicate_identical_summaries")
		req := baseReq()
		res, err := supervisor.ExecuteBackup(context.Background(), req)
		if err != nil {
			t.Fatalf("expected success with identical duplicate summaries, got: %v", err)
		}
		if res.SnapshotID != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
			t.Fatalf("unexpected snapshot ID: %s", res.SnapshotID)
		}
	})

	// Matrix U: Success: producer writes data, returns nil, valid summary returned
	t.Run("Scenario_U_Success", func(t *testing.T) {
		t.Setenv("MOCK_RESTIC_MODE", "success")
		req := baseReq()
		req.StreamProducer = func(ctx context.Context, stdin io.Writer) error {
			_, err := stdin.Write([]byte("SELECT * FROM test;\n"))
			return err
		}
		res, err := supervisor.ExecuteBackup(context.Background(), req)
		if err != nil {
			t.Fatalf("expected successful execution, got: %v", err)
		}
		if res.SnapshotID != "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef" {
			t.Fatalf("unexpected snapshot ID: %s", res.SnapshotID)
		}
		if res.ArtifactID != artID {
			t.Fatalf("expected artifact ID %s, got: %s", artID, res.ArtifactID)
		}
	})

	// Matrix V: Secret scrubbing in error output
	t.Run("Scenario_V_SecretScrubbing", func(t *testing.T) {
		t.Setenv("MOCK_RESTIC_MODE", "leak_secrets_stderr")
		req := baseReq()
		_, err := supervisor.ExecuteBackup(context.Background(), req)
		if err == nil {
			t.Fatalf("expected error from leak_secrets_stderr, got nil")
		}
		errStr := err.Error()
		if strings.Contains(errStr, string(validPassword)) {
			t.Fatalf("SECURITY LEAK: raw password found in error message: %s", errStr)
		}
		if strings.Contains(errStr, "my-super-secret-aws-key") {
			t.Fatalf("SECURITY LEAK: AWS secret key found in error message: %s", errStr)
		}
		if !strings.Contains(errStr, "[REDACTED_PASSWORD]") {
			t.Errorf("expected [REDACTED_PASSWORD] placeholder in error, got: %s", errStr)
		}
		if !strings.Contains(errStr, "[REDACTED_SECRET]") {
			t.Errorf("expected [REDACTED_SECRET] placeholder in error, got: %s", errStr)
		}
	})

	// Matrix W: Target token generation for MySQL
	t.Run("Scenario_W_TargetTokenMySQL", func(t *testing.T) {
		token := BuildDeterministicTargetToken(domain.BackupTypeMySQLDatabase, "my-app_production.v2")
		if token != "my-app_production_v2" {
			t.Errorf("unexpected token: %q", token)
		}

		leadingHyphen := BuildDeterministicTargetToken(domain.BackupTypeMySQLDatabase, "-db")
		if !strings.HasPrefix(leadingHyphen, "target_") {
			t.Errorf("expected leading hyphen to be prefixed with target_, got: %s", leadingHyphen)
		}

		emptyToken := BuildDeterministicTargetToken(domain.BackupTypeMySQLDatabase, "   !@#$%^   ")
		if emptyToken != "default" {
			t.Errorf("expected empty/special target name to resolve to 'default', got: %q", emptyToken)
		}
	})

	// Matrix X: Target token generation for website files
	t.Run("Scenario_X_TargetTokenFiles", func(t *testing.T) {
		token := BuildDeterministicTargetToken(domain.BackupTypeWebsiteFiles, "/var/www/html/site1")
		if token != "var_www_html_site1" {
			t.Errorf("expected slashes converted to underscores, got: %q", token)
		}
	})

	// Matrix Y: Discrete Argv & 6 Tags Verification
	t.Run("Scenario_Y_DiscreteArgvAnd6Tags", func(t *testing.T) {
		argsFile := filepath.Join(t.TempDir(), "args.txt")
		t.Setenv("MOCK_RESTIC_MODE", "dump_args")
		t.Setenv("MOCK_ARGS_FILE", argsFile)

		req := baseReq()
		_, err := supervisor.ExecuteBackup(context.Background(), req)
		if err != nil {
			t.Fatalf("expected ExecuteBackup to succeed, got: %v", err)
		}

		argsBytes, err := os.ReadFile(argsFile)
		if err != nil {
			t.Fatalf("failed reading dumped args: %v", err)
		}
		args := strings.Split(strings.TrimSpace(string(argsBytes)), "\n")

		// Verify discrete arguments
		expectedTags := map[string]bool{
			"platform=backup-platform-v1": false,
			"org=" + orgID.String():       false,
			"resource=" + resID.String():  false,
			"run=" + runID.String():       false,
			"artifact=" + artID.String():  false,
			"target=production_db":        false,
		}

		hasBackup := false
		hasStdin := false
		hasStdinFilename := false
		hasJSON := false

		for i, arg := range args {
			if arg == "backup" {
				hasBackup = true
			}
			if arg == "--stdin" {
				hasStdin = true
			}
			if arg == "--stdin-filename" && i+1 < len(args) && args[i+1] == "production_db.sql" {
				hasStdinFilename = true
			}
			if arg == "--json" {
				hasJSON = true
			}
			if arg == "--tag" && i+1 < len(args) {
				tagVal := args[i+1]
				if _, ok := expectedTags[tagVal]; ok {
					expectedTags[tagVal] = true
				}
			}
		}

		if !hasBackup {
			t.Errorf("missing 'backup' argument")
		}
		if !hasStdin {
			t.Errorf("missing '--stdin' argument")
		}
		if !hasStdinFilename {
			t.Errorf("missing '--stdin-filename production_db.sql' argument")
		}
		if !hasJSON {
			t.Errorf("missing '--json' argument")
		}
		for tag, found := range expectedTags {
			if !found {
				t.Errorf("missing mandatory tag: %s", tag)
			}
		}
	})
}

// TestGatedEOF_StrictGatedProtocol proves that:
// 1. If producer returns an error, child process NEVER receives an EOF; it is hard-killed.
// 2. If producer panics, child process NEVER receives an EOF; it is hard-killed.
// 3. If producer succeeds, child process receives exactly one EOF and produces artifact.
func TestGatedEOF_StrictGatedProtocol(t *testing.T) {
	mockBin := buildMockResticBinary(t)
	supervisor := NewGatedEOFSupervisor(mockBin, slog.Default())

	validTarget := &mockTarget{
		url:     "local:/tmp/test-repo",
		env:     nil,
		locator: "test-locator",
	}

	t.Run("Proves_ProducerError_HardKillsWithoutEOF", func(t *testing.T) {
		eofFile := filepath.Join(t.TempDir(), "eof_received.txt")
		t.Setenv("MOCK_RESTIC_MODE", "eof_detector")
		t.Setenv("MOCK_EOF_FILE", eofFile)

		simulatedErr := errors.New("simulated connector failure")

		req := StdinBackupRequest{
			Target:           validTarget,
			Password:         []byte("secure-pw"),
			OrgID:            uuid.New(),
			ResourceID:       uuid.New(),
			RunID:            uuid.New(),
			ArtifactID:       uuid.New(),
			BackupType:       domain.BackupTypeMySQLDatabase,
			TargetName:       "db",
			InternalFilename: "db.sql",
			StreamProducer: func(ctx context.Context, stdin io.Writer) error {
				_, _ = stdin.Write([]byte("chunk 1"))
				// Simulate failure
				return simulatedErr
			},
		}

		_, err := supervisor.ExecuteBackup(context.Background(), req)
		if !errors.Is(err, simulatedErr) {
			t.Fatalf("expected simulated error %v, got: %v", simulatedErr, err)
		}

		// Verify EOF was NEVER received by child process
		if _, statErr := os.Stat(eofFile); !os.IsNotExist(statErr) {
			t.Fatalf("SECURITY VIOLATION: child process received EOF despite producer error!")
		}
	})

	t.Run("Proves_ProducerPanic_HardKillsWithoutEOF", func(t *testing.T) {
		eofFile := filepath.Join(t.TempDir(), "eof_received.txt")
		t.Setenv("MOCK_RESTIC_MODE", "eof_detector")
		t.Setenv("MOCK_EOF_FILE", eofFile)

		req := StdinBackupRequest{
			Target:           validTarget,
			Password:         []byte("secure-pw"),
			OrgID:            uuid.New(),
			ResourceID:       uuid.New(),
			RunID:            uuid.New(),
			ArtifactID:       uuid.New(),
			BackupType:       domain.BackupTypeMySQLDatabase,
			TargetName:       "db",
			InternalFilename: "db.sql",
			StreamProducer: func(ctx context.Context, stdin io.Writer) error {
				_, _ = stdin.Write([]byte("chunk 1"))
				panic("simulated critical crash")
			},
		}

		_, err := supervisor.ExecuteBackup(context.Background(), req)
		if err == nil || !strings.Contains(err.Error(), "backup stream producer panicked") {
			t.Fatalf("expected panic recovery error, got: %v", err)
		}

		// Verify EOF was NEVER received by child process
		if _, statErr := os.Stat(eofFile); !os.IsNotExist(statErr) {
			t.Fatalf("SECURITY VIOLATION: child process received EOF despite producer panic!")
		}
	})

	t.Run("Proves_ProducerSuccess_SendsEOFAndCompletes", func(t *testing.T) {
		eofFile := filepath.Join(t.TempDir(), "eof_received.txt")
		t.Setenv("MOCK_RESTIC_MODE", "eof_detector")
		t.Setenv("MOCK_EOF_FILE", eofFile)

		req := StdinBackupRequest{
			Target:           validTarget,
			Password:         []byte("secure-pw"),
			OrgID:            uuid.New(),
			ResourceID:       uuid.New(),
			RunID:            uuid.New(),
			ArtifactID:       uuid.New(),
			BackupType:       domain.BackupTypeMySQLDatabase,
			TargetName:       "db",
			InternalFilename: "db.sql",
			StreamProducer: func(ctx context.Context, stdin io.Writer) error {
				_, err := stdin.Write([]byte("chunk 1 of healthy backup stream\n"))
				return err
			},
		}

		res, err := supervisor.ExecuteBackup(context.Background(), req)
		if err != nil {
			t.Fatalf("expected successful backup, got: %v", err)
		}
		if res.SnapshotID != "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef" {
			t.Fatalf("unexpected snapshot ID: %s", res.SnapshotID)
		}

		// Verify EOF was received by child process
		if _, statErr := os.Stat(eofFile); os.IsNotExist(statErr) {
			t.Fatalf("expected child process to receive EOF on producer success, but eofFile was not created")
		}
	})
}
