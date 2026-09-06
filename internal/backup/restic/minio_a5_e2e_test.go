package restic_test

import (
	"context"
	"io"
	"log/slog"
	"net/url"
	"os"
	"testing"
	"time"

	"backup-platform/internal/backup/domain"
	"backup-platform/internal/backup/engine"
	"backup-platform/internal/backup/restic"
	"backup-platform/pkg/uuid"
)

func TestResticRunner_A5_MinIO_E2E_MaintenanceLifecycle(t *testing.T) {
	resticBin := os.Getenv("RESTIC_BINARY_PATH")
	minioEndpoint := os.Getenv("TEST_MINIO_ENDPOINT")
	minioBucket := os.Getenv("TEST_MINIO_BUCKET")
	minioAccessKey := os.Getenv("TEST_MINIO_ACCESS_KEY")
	minioSecretKey := os.Getenv("TEST_MINIO_SECRET_KEY")

	if resticBin == "" || minioEndpoint == "" || minioBucket == "" || minioAccessKey == "" || minioSecretKey == "" {
		t.Skip("skipping MinIO live integration test: TEST_MINIO_* or RESTIC_BINARY_PATH not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
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
	password := []byte("minio-super-secure-restic-pw-a5!")

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
		[]string{hostOnly, "127.0.0.1", "localhost"},
	)
	if err != nil {
		t.Fatalf("failed creating s3 repository target: %v", err)
	}
	defer target.Cleanup()

	// 1. Initialize S3 Restic Repository
	if err := runner.Init(ctx, target, password); err != nil {
		t.Fatalf("failed initializing S3 repository: %v", err)
	}
	t.Log("Initialized S3 repository on MinIO successfully")

	// 2. Stream a backup into S3 using GatedEOFSupervisor
	sup := engine.NewGatedEOFSupervisor(resticBin, logger)
	sqlContent := []byte("CREATE TABLE test_maintenance_a5 (id INT); INSERT INTO test_maintenance_a5 VALUES (100);")

	backupReq := engine.StdinBackupRequest{
		Target:           target,
		Password:         password,
		OrgID:            orgID,
		ResourceID:       resID,
		RunID:            runID,
		ArtifactID:       artID,
		BackupType:       domain.BackupTypeMySQLDatabase,
		TargetName:       "test_db",
		InternalFilename: "test_db.sql",
		StreamProducer: func(pCtx context.Context, stdin io.Writer) error {
			_, err := stdin.Write(sqlContent)
			return err
		},
	}

	res, err := sup.ExecuteBackup(ctx, backupReq)
	if err != nil {
		t.Fatalf("failed streaming backup to S3 restic repo: %v", err)
	}
	if !domain.IsValidCanonicalResticSnapshotID(res.SnapshotID) {
		t.Fatalf("snapshot ID is not canonical 64-hex: %s", res.SnapshotID)
	}
	t.Logf("Created snapshot %s in S3 repository", res.SnapshotID)

	// 3. Check Subset 1/4 on S3 repository
	if err := runner.CheckSubset(ctx, target, password, 1, 4); err != nil {
		t.Fatalf("check subset 1/4 failed on S3 repository: %v", err)
	}
	t.Log("CheckSubset 1/4 passed on S3 repository")

	// 4. Forget snapshot in S3 repository
	if err := runner.ForgetSnapshot(ctx, target, password, res.SnapshotID); err != nil {
		t.Fatalf("failed forgetting snapshot in S3 repository: %v", err)
	}
	t.Logf("Successfully forgot snapshot %s in S3 repository", res.SnapshotID)

	// Verify snapshot is gone from S3 using VerifySnapshotAbsent
	if err := runner.VerifySnapshotAbsent(ctx, target, password, res.SnapshotID); err != nil {
		t.Fatalf("expected snapshot %s to be verified absent from S3 repo, but got err: %v", res.SnapshotID, err)
	}

	// 5. Prune S3 repository
	if err := runner.Prune(ctx, target, password); err != nil {
		t.Fatalf("failed pruning S3 repository: %v", err)
	}
	t.Log("Successfully pruned S3 repository on MinIO")

	// 6. Check Subset 1/1 on clean S3 repository
	if err := runner.CheckSubset(ctx, target, password, 1, 1); err != nil {
		t.Fatalf("post-prune check failed on S3 repository: %v", err)
	}
	t.Log("Post-prune CheckSubset 1/1 passed on S3 repository")
}
