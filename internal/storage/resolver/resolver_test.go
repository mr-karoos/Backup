package resolver

import (
	"context"
	"errors"
	"testing"

	"backup-platform/internal/backup/domain"
	credDomain "backup-platform/internal/credential/domain"
	"backup-platform/internal/credential/payload"
	"backup-platform/internal/storage"
	"backup-platform/pkg/uuid"
)

type mockStorageTargetRepo struct {
	targets map[string]*domain.StorageTarget
}

func (m *mockStorageTargetRepo) GetStorageTargetByID(ctx context.Context, orgID, targetID uuid.UUID) (*domain.StorageTarget, error) {
	key := orgID.String() + ":" + targetID.String()
	t, ok := m.targets[key]
	if !ok {
		return nil, domain.ErrStorageTargetNotFound
	}
	return t, nil
}

type mockVaultLoader struct {
	credentials map[string][]byte
}

func (m *mockVaultLoader) LoadCredentialForUse(ctx context.Context, orgID, credID uuid.UUID) (credDomain.Type, []byte, error) {
	key := orgID.String() + ":" + credID.String()
	b, ok := m.credentials[key]
	if !ok {
		return "", nil, errors.New("credential not found")
	}
	res := make([]byte, len(b))
	copy(res, b)
	return credDomain.TypeS3Credentials, res, nil
}

func TestStorageResolver_Resolve(t *testing.T) {
	orgID := uuid.New()
	localTargetID := uuid.New()
	s3TargetID := uuid.New()
	disabledTargetID := uuid.New()
	credID := uuid.New()

	s3PayloadBytes, err := payload.EncodeS3V1("MYKEY", "MYSECRET", nil)
	if err != nil {
		t.Fatalf("failed encoding s3 payload: %v", err)
	}

	mockRepo := &mockStorageTargetRepo{
		targets: map[string]*domain.StorageTarget{
			orgID.String() + ":" + localTargetID.String(): {
				ID:             localTargetID,
				OrganizationID: orgID,
				Name:           "Local Target",
				Type:           domain.StorageTargetTypeLocal,
				Status:         domain.StorageTargetStatusActive,
				IsDefault:      true,
			},
			orgID.String() + ":" + s3TargetID.String(): {
				ID:             s3TargetID,
				OrganizationID: orgID,
				Name:           "S3 Target",
				Type:           domain.StorageTargetTypeS3,
				Status:         domain.StorageTargetStatusActive,
				CredentialID:   &credID,
				Config:         []byte(`{"bucket":"my-bucket","region":"us-east-1"}`),
			},
			orgID.String() + ":" + disabledTargetID.String(): {
				ID:             disabledTargetID,
				OrganizationID: orgID,
				Name:           "Disabled Target",
				Type:           domain.StorageTargetTypeLocal,
				Status:         domain.StorageTargetStatusDisabled,
			},
		},
	}

	mockVault := &mockVaultLoader{
		credentials: map[string][]byte{
			orgID.String() + ":" + credID.String(): s3PayloadBytes,
		},
	}

	var mockLocal storage.StorageProvider
	resolver := NewStorageResolver(mockLocal, mockRepo, mockVault, false, nil)
	ctx := context.Background()

	t.Run("resolve local target", func(t *testing.T) {
		prov, err := resolver.Resolve(ctx, orgID, localTargetID)
		if err != nil {
			t.Fatalf("unexpected error resolving local: %v", err)
		}
		if prov != mockLocal {
			t.Errorf("expected local provider instance")
		}
	})

	t.Run("resolve s3 target", func(t *testing.T) {
		prov, err := resolver.Resolve(ctx, orgID, s3TargetID)
		if err != nil {
			t.Fatalf("unexpected error resolving s3: %v", err)
		}
		if prov == nil {
			t.Errorf("expected s3 provider instance, got nil")
		}
	})

	t.Run("resolve disabled target fails", func(t *testing.T) {
		_, err := resolver.Resolve(ctx, orgID, disabledTargetID)
		if !errors.Is(err, domain.ErrStorageTargetNotActive) {
			t.Errorf("expected ErrStorageTargetNotActive, got %v", err)
		}
	})

	t.Run("resolve nonexistent target fails", func(t *testing.T) {
		_, err := resolver.Resolve(ctx, orgID, uuid.New())
		if !errors.Is(err, domain.ErrStorageTargetNotFound) {
			t.Errorf("expected ErrStorageTargetNotFound, got %v", err)
		}
	})
}
