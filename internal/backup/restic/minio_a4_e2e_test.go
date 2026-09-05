package restic_test

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"backup-platform/internal/backup/domain"
	"backup-platform/internal/backup/engine"
	"backup-platform/internal/backup/restic"
	"backup-platform/internal/backup/verification"
	"backup-platform/pkg/uuid"
)

func TestResticRunner_A4_MinIO_E2E_Lifecycle(t *testing.T) {
	resticBin := os.Getenv("RESTIC_BINARY_PATH")
	minioEndpoint := os.Getenv("TEST_MINIO_ENDPOINT")
	minioBucket := os.Getenv("TEST_MINIO_BUCKET")
	minioAccessKey := os.Getenv("TEST_MINIO_ACCESS_KEY")
	minioSecretKey := os.Getenv("TEST_MINIO_SECRET_KEY")

	if resticBin == "" || minioEndpoint == "" || minioBucket == "" || minioAccessKey == "" || minioSecretKey == "" {
		t.Skip("skipping MinIO live integration test: TEST_MINIO_* or RESTIC_BINARY_PATH not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	logger := slog.Default()
	runner := restic.NewResticRunner(resticBin, logger)

	if err := runner.ValidateVersion(ctx); err != nil {
		t.Fatalf("failed validating restic version: %v", err)
	}

	u, err := url.Parse(minioEndpoint)
	if err != nil {
		t.Fatalf("failed parsing minio endpoint URL: %v", err)
	}
	allowInsecure := (u.Scheme == "http")
	hostOnly := u.Hostname()

	orgID := uuid.New()
	resID := uuid.New()
	runID := uuid.New()
	artID := uuid.New()
	password := []byte("minio-super-secure-restic-pw-a4!")

	s3Cfg := domain.S3TargetConfig{
		Bucket:   minioBucket,
		Region:   "us-east-1",
		Endpoint: minioEndpoint,
	}

	target, err := restic.NewS3RepositoryTarget(
		"s3_compatible",
		s3Cfg,
		orgID,
		resID,
		minioAccessKey,
		minioSecretKey,
		nil,
		allowInsecure,
		[]string{hostOnly},
	)
	if err != nil {
		t.Fatalf("failed creating S3RepositoryTarget: %v", err)
	}
	defer target.Cleanup()

	// 1. Initialize repository on MinIO
	if err := runner.Init(ctx, target, password); err != nil {
		t.Fatalf("failed initializing restic repository on MinIO: %v", err)
	}

	// 2. Execute Gated EOF streaming backup via GatedEOFSupervisor
	supervisor := engine.NewGatedEOFSupervisor(resticBin, logger)
	dbName := "minio_store_db"
	targetToken := engine.BuildDeterministicTargetToken(domain.BackupTypeMySQLDatabase, dbName)
	internalFilename := targetToken + ".sql"

	originalPayload := []byte("-- MySQL dump 10.13\nCREATE DATABASE `minio_store_db`;\nUSE `minio_store_db`;\nCREATE TABLE `products` (id INT, title VARCHAR(255));\nINSERT INTO `products` VALUES (1, 'Cloud S3 Box');\n")

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
		t.Fatalf("failed executing Gated EOF backup to MinIO: %v", err)
	}
	if res.SnapshotID == "" {
		t.Fatalf("expected non-empty snapshot ID")
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
		t.Fatalf("Level-1 verification failed against MinIO snapshot: %v", err)
	}
	if !strings.Contains(verMsg, "verified") {
		t.Errorf("expected verification message to contain 'verified', got: %s", verMsg)
	}

	// 4. Test Streaming Download (DumpStream) roundtrip over MinIO
	streamRC, err := runner.DumpStream(ctx, target, password, res.SnapshotID, internalFilename)
	if err != nil {
		t.Fatalf("failed opening DumpStream from MinIO: %v", err)
	}
	defer streamRC.Close()

	var downloaded bytes.Buffer
	if _, err := io.Copy(&downloaded, streamRC); err != nil {
		t.Fatalf("failed reading DumpStream from MinIO: %v", err)
	}
	if err := streamRC.Close(); err != nil {
		t.Fatalf("failed closing DumpStream: %v", err)
	}

	if !bytes.Equal(downloaded.Bytes(), originalPayload) {
		t.Fatalf("downloaded payload from MinIO does not match original bytes!\nGot: %s\nExpected: %s", downloaded.String(), string(originalPayload))
	}
}
