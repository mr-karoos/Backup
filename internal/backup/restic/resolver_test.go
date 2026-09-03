package restic

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"backup-platform/internal/backup/domain"
	credDomain "backup-platform/internal/credential/domain"
	"backup-platform/internal/credential/payload"
	"backup-platform/pkg/uuid"
)

type mockStorageTargetGetter struct {
	targets map[uuid.UUID]*domain.StorageTarget
}

func (m *mockStorageTargetGetter) GetStorageTargetByID(ctx context.Context, orgID, targetID uuid.UUID) (*domain.StorageTarget, error) {
	if t, ok := m.targets[targetID]; ok && t.OrganizationID == orgID {
		return t, nil
	}
	return nil, domain.ErrStorageTargetNotFound
}

type mockCredentialLoader struct {
	creds map[uuid.UUID][]byte
}

func (m *mockCredentialLoader) LoadCredentialForUse(ctx context.Context, orgID, credID uuid.UUID) (credDomain.Type, []byte, error) {
	if c, ok := m.creds[credID]; ok {
		return credDomain.TypeS3Credentials, c, nil
	}
	return "", nil, errors.New("credential not found")
}

func TestTargetResolver(t *testing.T) {
	ctx := context.Background()
	orgID := uuid.New()
	resourceID := uuid.New()
	storageRoot := t.TempDir()

	getter := &mockStorageTargetGetter{targets: make(map[uuid.UUID]*domain.StorageTarget)}
	loader := &mockCredentialLoader{creds: make(map[uuid.UUID][]byte)}

	resolver := NewTargetResolver(getter, loader, storageRoot, true, []string{"127.0.0.1"})

	t.Run("resolves local storage target", func(t *testing.T) {
		localTargetID := uuid.New()
		target := &domain.StorageTarget{
			ID:             localTargetID,
			OrganizationID: orgID,
			Name:           "Local Target",
			Type:           domain.StorageTargetTypeLocal,
			Status:         domain.StorageTargetStatusActive,
		}
		getter.targets[localTargetID] = target

		resolved, err := resolver.ResolveTarget(ctx, orgID, resourceID, target)
		if err != nil {
			t.Fatalf("expected success, got: %v", err)
		}
		if resolved.Type() != string(domain.StorageTargetTypeLocal) {
			t.Errorf("expected local target type, got: %s", resolved.Type())
		}
	})

	t.Run("resolves S3 storage target with decrypted credentials", func(t *testing.T) {
		s3TargetID := uuid.New()
		s3CredID := uuid.New()

		s3PayloadBytes, err := payload.EncodeS3V1("MYKEY", "MYSECRET", nil)
		if err != nil {
			t.Fatalf("failed encoding s3 payload: %v", err)
		}
		loader.creds[s3CredID] = s3PayloadBytes

		cfgBytes, _ := json.Marshal(map[string]any{
			"bucket": "test-bucket",
			"region": "us-east-1",
		})

		target := &domain.StorageTarget{
			ID:             s3TargetID,
			OrganizationID: orgID,
			Name:           "S3 Target",
			Type:           domain.StorageTargetTypeS3,
			Status:         domain.StorageTargetStatusActive,
			CredentialID:   &s3CredID,
			Config:         cfgBytes,
		}
		getter.targets[s3TargetID] = target

		resolved, err := resolver.ResolveTarget(ctx, orgID, resourceID, target)
		if err != nil {
			t.Fatalf("expected success, got: %v", err)
		}
		if resolved.Type() != string(domain.StorageTargetTypeS3) {
			t.Errorf("expected s3 target type, got: %s", resolved.Type())
		}
		defer resolved.Cleanup()
	})

	t.Run("rejects inactive storage target", func(t *testing.T) {
		target := &domain.StorageTarget{
			ID:             uuid.New(),
			OrganizationID: orgID,
			Name:           "Disabled Target",
			Type:           domain.StorageTargetTypeLocal,
			Status:         domain.StorageTargetStatusDisabled,
		}

		_, err := resolver.ResolveTarget(ctx, orgID, resourceID, target)
		if !errors.Is(err, domain.ErrStorageTargetNotActive) {
			t.Errorf("expected ErrStorageTargetNotActive, got: %v", err)
		}
	})

	t.Run("rejects cross-tenant storage target", func(t *testing.T) {
		target := &domain.StorageTarget{
			ID:             uuid.New(),
			OrganizationID: uuid.New(), // other org
			Name:           "Other Org Target",
			Type:           domain.StorageTargetTypeLocal,
			Status:         domain.StorageTargetStatusActive,
		}

		_, err := resolver.ResolveTarget(ctx, orgID, resourceID, target)
		if !errors.Is(err, domain.ErrStorageTargetNotFound) {
			t.Errorf("expected ErrStorageTargetNotFound, got: %v", err)
		}
	})
}
