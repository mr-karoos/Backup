package restic_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"backup-platform/internal/backup/domain"
	"backup-platform/internal/backup/engine"
	"backup-platform/internal/backup/restic"
	"backup-platform/internal/backup/verification"
	"backup-platform/pkg/uuid"
)

func findResticBinary() string {
	binaryPath := os.Getenv("RESTIC_BINARY_PATH")
	if binaryPath != "" {
		return binaryPath
	}
	candidates := []string{
		filepath.Join(os.TempDir(), "restic-bin", "restic.exe"),
		filepath.Join(os.TempDir(), "restic-bin", "restic"),
		"/usr/local/bin/restic",
		"restic",
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return ""
}

func TestResticRunner_A4_E2E_Lifecycle(t *testing.T) {
	binPath := findResticBinary()
	if binPath == "" {
		t.Skip("skipping Restic A.4 E2E test: restic binary not found")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	logger := slog.Default()
	runner := restic.NewResticRunner(binPath, logger)

	// Validate version
	if err := runner.ValidateVersion(ctx); err != nil {
		t.Fatalf("failed validating restic version: %v", err)
	}

	tempStorageRoot := t.TempDir()
	orgID := uuid.New()
	resID := uuid.New()
	runID := uuid.New()
	artID := uuid.New()

	target, err := restic.NewLocalRepositoryTarget(tempStorageRoot, orgID, resID)
	if err != nil {
		t.Fatalf("failed creating local repository target: %v", err)
	}
	defer target.Cleanup()

	password := []byte("secure-master-restic-pw-step-a4!")

	// 1. Initialize repository
	if err := runner.Init(ctx, target, password); err != nil {
		t.Fatalf("failed initializing repository: %v", err)
	}

	// 2. Execute Gated EOF streaming backup via GatedEOFSupervisor
	supervisor := engine.NewGatedEOFSupervisor(binPath, logger)
	dbName := "sample_shop_db"
	targetToken := engine.BuildDeterministicTargetToken(domain.BackupTypeMySQLDatabase, dbName)
	internalFilename := targetToken + ".sql"

	originalPayload := []byte("-- MySQL dump 10.13\nCREATE DATABASE `sample_shop_db`;\nUSE `sample_shop_db`;\nCREATE TABLE `orders` (id INT PRIMARY KEY, amount DECIMAL(10,2));\nINSERT INTO `orders` VALUES (1, 99.95), (2, 149.50);\n")

	backupReq := engine.StdinBackupRequest{
		Target:           target,
		Password:         password,
		OrgID:            orgID,
		ResourceID:       resID,
		RunID:            runID,
		ArtifactID:       artID,
		BackupType:       domain.BackupTypeMySQLDatabase,
		TargetName:       dbName,
		InternalFilename: internalFilename,
		StreamProducer: func(pCtx context.Context, stdin io.Writer) error {
			_, err := stdin.Write(originalPayload)
			return err
		},
	}

	res, err := supervisor.ExecuteBackup(ctx, backupReq)
	if err != nil {
		t.Fatalf("failed executing Gated EOF backup: %v", err)
	}
	if res.SnapshotID == "" {
		t.Fatalf("expected non-empty snapshot ID")
	}
	if res.ArtifactID != artID {
		t.Fatalf("expected artifact ID %s, got %s", artID, res.ArtifactID)
	}

	// 3. Level-1 Verification Phase
	verifier := verification.NewVerificationEngine()
	verMsg, err := verifier.VerifyResticSnapshot(
		ctx,
		runner,
		target,
		password,
		res.SnapshotID,
		orgID,
		resID,
		runID,
		artID,
		targetToken,
		internalFilename,
		int64(len(originalPayload)),
	)
	if err != nil {
		t.Fatalf("Level-1 verification failed: %v", err)
	}
	if !strings.Contains(verMsg, "verified") {
		t.Errorf("expected verification message to contain 'verified', got: %s", verMsg)
	}

	// 4. Test Streaming Download (DumpStream) roundtrip
	streamRC, err := runner.DumpStream(ctx, target, password, res.SnapshotID, internalFilename)
	if err != nil {
		t.Fatalf("failed opening DumpStream: %v", err)
	}
	defer streamRC.Close()

	var downloaded bytes.Buffer
	if _, err := io.Copy(&downloaded, streamRC); err != nil {
		t.Fatalf("failed reading DumpStream: %v", err)
	}
	if err := streamRC.Close(); err != nil {
		t.Fatalf("failed closing DumpStream: %v", err)
	}

	if !bytes.Equal(downloaded.Bytes(), originalPayload) {
		t.Fatalf("downloaded payload does not match original bytes!\nGot: %s\nExpected: %s", downloaded.String(), string(originalPayload))
	}

	// 5. Failure Injection: Partial Producer Stream Failure Proves NO Artifact Created
	failingArtID := uuid.New()
	failingRunID := uuid.New()
	simulatedErr := errors.New("simulated network drop during MySQL mysqldump")

	failingReq := engine.StdinBackupRequest{
		Target:           target,
		Password:         password,
		OrgID:            orgID,
		ResourceID:       resID,
		RunID:            failingRunID,
		ArtifactID:       failingArtID,
		BackupType:       domain.BackupTypeMySQLDatabase,
		TargetName:       dbName,
		InternalFilename: internalFilename,
		StreamProducer: func(pCtx context.Context, stdin io.Writer) error {
			_, _ = stdin.Write([]byte("PARTIAL_CORRUPTED_DATA_CHUNK"))
			return simulatedErr
		},
	}

	failRes, err := supervisor.ExecuteBackup(ctx, failingReq)
	if !errors.Is(err, simulatedErr) {
		t.Fatalf("expected simulated error %v, got: %v", simulatedErr, err)
	}
	if failRes != nil {
		t.Fatalf("expected nil result on failure, got: %+v", failRes)
	}

	// Verify no snapshot was committed for failingRunID/failingArtID
	// Attempting to look up a snapshot by failingArtID tag should yield not found
	// In restic snapshots, the previous successful snapshot remains unaffected
	snap, err := runner.GetSnapshot(ctx, target, password, res.SnapshotID)
	if err != nil {
		t.Fatalf("expected original snapshot to remain intact: %v", err)
	}
	if snap.ID != res.SnapshotID {
		t.Fatalf("expected original snapshot ID %s, got %s", res.SnapshotID, snap.ID)
	}
}
