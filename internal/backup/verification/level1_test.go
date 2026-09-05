package verification

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"backup-platform/internal/backup/domain"
	"backup-platform/internal/backup/restic"
	"backup-platform/pkg/uuid"
)

// mockResticRunner implements restic.CommandRunner for Level-1 verification testing.
type mockResticRunner struct {
	getSnapshotFunc       func(ctx context.Context, target restic.RepositoryTarget, password []byte, snapshotID string) (*restic.SnapshotItem, error)
	listSnapshotNodesFunc func(ctx context.Context, target restic.RepositoryTarget, password []byte, snapshotID string) ([]restic.SnapshotNode, error)
	dumpSampleFunc        func(ctx context.Context, target restic.RepositoryTarget, password []byte, snapshotID, internalFilename string, maxBytes int) ([]byte, error)
	dumpStreamFunc        func(ctx context.Context, target restic.RepositoryTarget, password []byte, snapshotID, internalFilename string) (io.ReadCloser, error)
}

func (m *mockResticRunner) ValidateVersion(ctx context.Context) error {
	return nil
}

func (m *mockResticRunner) Version(ctx context.Context) (string, error) {
	return "restic 0.19.1", nil
}

func (m *mockResticRunner) Init(ctx context.Context, target restic.RepositoryTarget, password []byte) error {
	return nil
}

func (m *mockResticRunner) Probe(ctx context.Context, target restic.RepositoryTarget, password []byte) error {
	return nil
}

func (m *mockResticRunner) GetSnapshot(ctx context.Context, target restic.RepositoryTarget, password []byte, snapshotID string) (*restic.SnapshotItem, error) {
	if m.getSnapshotFunc != nil {
		return m.getSnapshotFunc(ctx, target, password, snapshotID)
	}
	return nil, restic.ErrSnapshotNotFound
}

func (m *mockResticRunner) ListSnapshotNodes(ctx context.Context, target restic.RepositoryTarget, password []byte, snapshotID string) ([]restic.SnapshotNode, error) {
	if m.listSnapshotNodesFunc != nil {
		return m.listSnapshotNodesFunc(ctx, target, password, snapshotID)
	}
	return nil, nil
}

func (m *mockResticRunner) DumpSample(ctx context.Context, target restic.RepositoryTarget, password []byte, snapshotID, internalFilename string, maxBytes int) ([]byte, error) {
	if m.dumpSampleFunc != nil {
		return m.dumpSampleFunc(ctx, target, password, snapshotID, internalFilename, maxBytes)
	}
	return nil, nil
}

func (m *mockResticRunner) DumpStream(ctx context.Context, target restic.RepositoryTarget, password []byte, snapshotID, internalFilename string) (io.ReadCloser, error) {
	if m.dumpStreamFunc != nil {
		return m.dumpStreamFunc(ctx, target, password, snapshotID, internalFilename)
	}
	return nil, nil
}

type mockTarget struct{}

func (m *mockTarget) Type() string                { return "local" }
func (m *mockTarget) Locator() string             { return "loc" }
func (m *mockTarget) ResticRepositoryURL() string { return "/tmp/repo" }
func (m *mockTarget) Env() []string               { return nil }
func (m *mockTarget) Cleanup()                    {}

func TestVerificationEngine_VerifyResticSnapshot(t *testing.T) {
	engine := NewVerificationEngine()
	target := &mockTarget{}
	password := []byte("secret-pw")
	snapID := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	orgID := uuid.New()
	resID := uuid.New()
	runID := uuid.New()
	artID := uuid.New()
	targetToken := "prod_db"
	internalFilename := "prod_db.sql"
	expectedLogicalSize := int64(4096)

	validTags := []string{
		"platform=backup-platform-v1",
		"org=" + orgID.String(),
		"resource=" + resID.String(),
		"run=" + runID.String(),
		"artifact=" + artID.String(),
		"target=" + targetToken,
	}

	validSnapshot := &restic.SnapshotItem{
		ID:      snapID,
		ShortID: snapID[:8],
		Time:    time.Now(),
		Tags:    validTags,
	}

	validNodes := []restic.SnapshotNode{
		{
			Name: internalFilename,
			Path: "/" + internalFilename,
			Type: "file",
			Size: expectedLogicalSize,
		},
	}

	validSample := []byte("-- MySQL dump 10.13\nCREATE TABLE users (id int);\n")

	t.Run("Success_AllChecksPass", func(t *testing.T) {
		runner := &mockResticRunner{
			getSnapshotFunc: func(ctx context.Context, target restic.RepositoryTarget, password []byte, snapshotID string) (*restic.SnapshotItem, error) {
				return validSnapshot, nil
			},
			listSnapshotNodesFunc: func(ctx context.Context, target restic.RepositoryTarget, password []byte, snapshotID string) ([]restic.SnapshotNode, error) {
				return validNodes, nil
			},
			dumpSampleFunc: func(ctx context.Context, target restic.RepositoryTarget, password []byte, snapshotID, fn string, maxBytes int) ([]byte, error) {
				return validSample, nil
			},
		}

		msg, err := engine.VerifyResticSnapshot(
			context.Background(),
			runner,
			target,
			password,
			snapID,
			orgID,
			resID,
			runID,
			artID,
			targetToken,
			internalFilename,
			expectedLogicalSize,
		)
		if err != nil {
			t.Fatalf("expected verification to succeed, got: %v", err)
		}
		if !strings.Contains(msg, "verified") {
			t.Errorf("expected verification message to contain 'verified', got: %s", msg)
		}
	})

	t.Run("SnapshotNotFound_FailsVerification", func(t *testing.T) {
		runner := &mockResticRunner{
			getSnapshotFunc: func(ctx context.Context, target restic.RepositoryTarget, password []byte, snapshotID string) (*restic.SnapshotItem, error) {
				return nil, restic.ErrSnapshotNotFound
			},
		}

		_, err := engine.VerifyResticSnapshot(
			context.Background(),
			runner,
			target,
			password,
			snapID,
			orgID,
			resID,
			runID,
			artID,
			targetToken,
			internalFilename,
			expectedLogicalSize,
		)
		if err == nil || !errors.Is(err, domain.ErrVerificationFailed) {
			t.Fatalf("expected ErrVerificationFailed when snapshot not found, got: %v", err)
		}
	})

	t.Run("InfrastructureError_DoesNotFalselyClaimCorruption", func(t *testing.T) {
		infraErr := errors.New("network timeout reaching storage target")
		runner := &mockResticRunner{
			getSnapshotFunc: func(ctx context.Context, target restic.RepositoryTarget, password []byte, snapshotID string) (*restic.SnapshotItem, error) {
				return nil, infraErr
			},
		}

		_, err := engine.VerifyResticSnapshot(
			context.Background(),
			runner,
			target,
			password,
			snapID,
			orgID,
			resID,
			runID,
			artID,
			targetToken,
			internalFilename,
			expectedLogicalSize,
		)
		if err == nil {
			t.Fatalf("expected error, got nil")
		}
		if errors.Is(err, domain.ErrVerificationFailed) {
			t.Fatalf("infrastructure error must NOT be classified as ErrVerificationFailed: %v", err)
		}
		if !strings.Contains(err.Error(), "repository infrastructure error") {
			t.Fatalf("expected repository infrastructure error, got: %v", err)
		}
	})

	t.Run("MissingTag_FailsVerification", func(t *testing.T) {
		// Omit the run tag
		corruptedTags := []string{
			"platform=backup-platform-v1",
			"org=" + orgID.String(),
			"resource=" + resID.String(),
			"artifact=" + artID.String(),
			"target=" + targetToken,
		}
		runner := &mockResticRunner{
			getSnapshotFunc: func(ctx context.Context, target restic.RepositoryTarget, password []byte, snapshotID string) (*restic.SnapshotItem, error) {
				return &restic.SnapshotItem{ID: snapID, Tags: corruptedTags}, nil
			},
		}

		_, err := engine.VerifyResticSnapshot(
			context.Background(),
			runner,
			target,
			password,
			snapID,
			orgID,
			resID,
			runID,
			artID,
			targetToken,
			internalFilename,
			expectedLogicalSize,
		)
		if err == nil || !errors.Is(err, domain.ErrVerificationFailed) {
			t.Fatalf("expected ErrVerificationFailed for missing tag, got: %v", err)
		}
		if !strings.Contains(err.Error(), "missing mandatory snapshot tag") {
			t.Errorf("expected error to mention missing mandatory snapshot tag, got: %v", err)
		}
	})

	t.Run("InternalNodeMissing_FailsVerification", func(t *testing.T) {
		runner := &mockResticRunner{
			getSnapshotFunc: func(ctx context.Context, target restic.RepositoryTarget, password []byte, snapshotID string) (*restic.SnapshotItem, error) {
				return validSnapshot, nil
			},
			listSnapshotNodesFunc: func(ctx context.Context, target restic.RepositoryTarget, password []byte, snapshotID string) ([]restic.SnapshotNode, error) {
				return []restic.SnapshotNode{
					{Name: "different_file.sql", Path: "/different_file.sql", Size: 100},
				}, nil
			},
		}

		_, err := engine.VerifyResticSnapshot(
			context.Background(),
			runner,
			target,
			password,
			snapID,
			orgID,
			resID,
			runID,
			artID,
			targetToken,
			internalFilename,
			expectedLogicalSize,
		)
		if err == nil || !errors.Is(err, domain.ErrVerificationFailed) {
			t.Fatalf("expected ErrVerificationFailed when internal node is missing, got: %v", err)
		}
	})

	t.Run("ZeroSizeBytes_FailsVerification", func(t *testing.T) {
		runner := &mockResticRunner{
			getSnapshotFunc: func(ctx context.Context, target restic.RepositoryTarget, password []byte, snapshotID string) (*restic.SnapshotItem, error) {
				return validSnapshot, nil
			},
			listSnapshotNodesFunc: func(ctx context.Context, target restic.RepositoryTarget, password []byte, snapshotID string) ([]restic.SnapshotNode, error) {
				return []restic.SnapshotNode{
					{Name: internalFilename, Path: "/" + internalFilename, Size: 0},
				}, nil
			},
		}

		_, err := engine.VerifyResticSnapshot(
			context.Background(),
			runner,
			target,
			password,
			snapID,
			orgID,
			resID,
			runID,
			artID,
			targetToken,
			internalFilename,
			0, // expected size also zero
		)
		if err == nil || !errors.Is(err, domain.ErrVerificationFailed) {
			t.Fatalf("expected ErrVerificationFailed when size is zero, got: %v", err)
		}
	})

	t.Run("DumpSampleFailure_FailsVerification", func(t *testing.T) {
		runner := &mockResticRunner{
			getSnapshotFunc: func(ctx context.Context, target restic.RepositoryTarget, password []byte, snapshotID string) (*restic.SnapshotItem, error) {
				return validSnapshot, nil
			},
			listSnapshotNodesFunc: func(ctx context.Context, target restic.RepositoryTarget, password []byte, snapshotID string) ([]restic.SnapshotNode, error) {
				return validNodes, nil
			},
			dumpSampleFunc: func(ctx context.Context, target restic.RepositoryTarget, password []byte, snapshotID, fn string, maxBytes int) ([]byte, error) {
				return nil, errors.New("blob corrupted: hash mismatch")
			},
		}

		_, err := engine.VerifyResticSnapshot(
			context.Background(),
			runner,
			target,
			password,
			snapID,
			orgID,
			resID,
			runID,
			artID,
			targetToken,
			internalFilename,
			expectedLogicalSize,
		)
		if err == nil || !errors.Is(err, domain.ErrVerificationFailed) {
			t.Fatalf("expected ErrVerificationFailed when dump fails, got: %v", err)
		}
	})

	t.Run("EmptyDumpSample_FailsVerification", func(t *testing.T) {
		runner := &mockResticRunner{
			getSnapshotFunc: func(ctx context.Context, target restic.RepositoryTarget, password []byte, snapshotID string) (*restic.SnapshotItem, error) {
				return validSnapshot, nil
			},
			listSnapshotNodesFunc: func(ctx context.Context, target restic.RepositoryTarget, password []byte, snapshotID string) ([]restic.SnapshotNode, error) {
				return validNodes, nil
			},
			dumpSampleFunc: func(ctx context.Context, target restic.RepositoryTarget, password []byte, snapshotID, fn string, maxBytes int) ([]byte, error) {
				return []byte{}, nil
			},
		}

		_, err := engine.VerifyResticSnapshot(
			context.Background(),
			runner,
			target,
			password,
			snapID,
			orgID,
			resID,
			runID,
			artID,
			targetToken,
			internalFilename,
			expectedLogicalSize,
		)
		if err == nil || !errors.Is(err, domain.ErrVerificationFailed) {
			t.Fatalf("expected ErrVerificationFailed when dump sample is empty, got: %v", err)
		}
	})

	t.Run("Preconditions_ValidateInput", func(t *testing.T) {
		runner := &mockResticRunner{}

		// Nil runner
		_, err := engine.VerifyResticSnapshot(context.Background(), nil, target, password, snapID, orgID, resID, runID, artID, targetToken, internalFilename, expectedLogicalSize)
		if err == nil || !strings.Contains(err.Error(), "runner cannot be nil") {
			t.Errorf("expected runner cannot be nil, got: %v", err)
		}

		// Nil target
		_, err = engine.VerifyResticSnapshot(context.Background(), runner, nil, password, snapID, orgID, resID, runID, artID, targetToken, internalFilename, expectedLogicalSize)
		if err == nil || !strings.Contains(err.Error(), "target cannot be nil") {
			t.Errorf("expected target cannot be nil, got: %v", err)
		}

		// Empty password
		_, err = engine.VerifyResticSnapshot(context.Background(), runner, target, nil, snapID, orgID, resID, runID, artID, targetToken, internalFilename, expectedLogicalSize)
		if err == nil || !strings.Contains(err.Error(), "password cannot be empty") {
			t.Errorf("expected password cannot be empty, got: %v", err)
		}

		// Empty snapshot ID
		_, err = engine.VerifyResticSnapshot(context.Background(), runner, target, password, "", orgID, resID, runID, artID, targetToken, internalFilename, expectedLogicalSize)
		if err == nil || !errors.Is(err, domain.ErrVerificationFailed) {
			t.Errorf("expected ErrVerificationFailed for empty snapshot ID, got: %v", err)
		}
	})
}
