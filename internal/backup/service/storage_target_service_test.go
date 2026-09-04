package service

import (
	"context"
	"errors"
	"testing"

	"backup-platform/internal/backup/domain"
	credDomain "backup-platform/internal/credential/domain"
	orgDomain "backup-platform/internal/organization/domain"
	s3Storage "backup-platform/internal/storage/s3"
	"backup-platform/pkg/uuid"
)

type mockStorageTargetRepo struct {
	targets       map[uuid.UUID]*domain.StorageTarget
	artifactCount int64
	planCount     int64
	jobCount      int64
	repoCount     int64
}

func newMockStorageTargetRepo() *mockStorageTargetRepo {
	return &mockStorageTargetRepo{
		targets: make(map[uuid.UUID]*domain.StorageTarget),
	}
}

func (m *mockStorageTargetRepo) EnsureDefaultLocalStorageTarget(ctx context.Context, orgID uuid.UUID) (*domain.StorageTarget, error) {
	for _, t := range m.targets {
		if t.OrganizationID == orgID && t.IsDefault {
			return t, nil
		}
	}
	t := &domain.StorageTarget{
		ID:             uuid.New(),
		OrganizationID: orgID,
		Name:           "Default Local Storage",
		Type:           domain.StorageTargetTypeLocal,
		Status:         domain.StorageTargetStatusActive,
		IsDefault:      true,
	}
	m.targets[t.ID] = t
	return t, nil
}

func (m *mockStorageTargetRepo) GetStorageTargetByID(ctx context.Context, orgID, targetID uuid.UUID) (*domain.StorageTarget, error) {
	t, ok := m.targets[targetID]
	if !ok || t.OrganizationID != orgID {
		return nil, domain.ErrStorageTargetNotFound
	}
	return t, nil
}

func (m *mockStorageTargetRepo) CreateStorageTarget(ctx context.Context, target *domain.StorageTarget) (*domain.StorageTarget, error) {
	m.targets[target.ID] = target
	return target, nil
}

func (m *mockStorageTargetRepo) ListStorageTargets(ctx context.Context, orgID uuid.UUID) ([]*domain.StorageTarget, error) {
	var list []*domain.StorageTarget
	for _, t := range m.targets {
		if t.OrganizationID == orgID && t.Status != domain.StorageTargetStatusArchived {
			list = append(list, t)
		}
	}
	return list, nil
}

func (m *mockStorageTargetRepo) UpdateStorageTarget(ctx context.Context, target *domain.StorageTarget) (*domain.StorageTarget, error) {
	if _, ok := m.targets[target.ID]; !ok {
		return nil, domain.ErrStorageTargetNotFound
	}
	m.targets[target.ID] = target
	return target, nil
}

func (m *mockStorageTargetRepo) DeleteStorageTarget(ctx context.Context, orgID, targetID uuid.UUID) error {
	t, ok := m.targets[targetID]
	if !ok || t.OrganizationID != orgID {
		return domain.ErrStorageTargetNotFound
	}
	delete(m.targets, targetID)
	return nil
}

func (m *mockStorageTargetRepo) CountArtifactsByStorageTarget(ctx context.Context, orgID, targetID uuid.UUID) (int64, error) {
	return m.artifactCount, nil
}

func (m *mockStorageTargetRepo) CountPlansByStorageTarget(ctx context.Context, orgID, targetID uuid.UUID) (int64, error) {
	return m.planCount, nil
}

func (m *mockStorageTargetRepo) CountActiveJobsByStorageTarget(ctx context.Context, orgID, targetID uuid.UUID) (int64, error) {
	return m.jobCount, nil
}

func (m *mockStorageTargetRepo) CountRepositoriesByStorageTarget(ctx context.Context, orgID, targetID uuid.UUID) (int64, error) {
	return m.repoCount, nil
}

type mockCredFinder struct {
	creds map[uuid.UUID]*credDomain.CredentialMetadata
}

func (m *mockCredFinder) GetCredentialMetadata(ctx context.Context, orgID, credID uuid.UUID) (*credDomain.CredentialMetadata, error) {
	c, ok := m.creds[credID]
	if !ok || c.OrganizationID != orgID {
		return nil, credDomain.ErrCredentialNotFound
	}
	return c, nil
}

func TestStorageTargetService_CreateS3Target(t *testing.T) {
	repo := newMockStorageTargetRepo()
	credID := uuid.New()
	orgID := uuid.New()

	credFinder := &mockCredFinder{
		creds: map[uuid.UUID]*credDomain.CredentialMetadata{
			credID: {
				ID:             credID,
				OrganizationID: orgID,
				Type:           credDomain.TypeS3Credentials,
			},
		},
	}

	secPolicy := &s3Storage.EndpointSecurityPolicy{AllowInsecureHTTP: false}
	svc := NewStorageTargetService(repo, credFinder, secPolicy, nil)

	t.Run("valid S3 target creation", func(t *testing.T) {
		cfgJSON := []byte(`{"bucket":"my-backups","endpoint":"https://s3.eu-central-1.amazonaws.com","region":"eu-central-1"}`)
		target, err := svc.CreateStorageTarget(context.Background(), orgDomain.RoleAdmin, orgID, CreateStorageTargetInput{
			Name:         "AWS Primary S3",
			Type:         domain.StorageTargetTypeS3,
			CredentialID: &credID,
			Config:       cfgJSON,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if target.Type != domain.StorageTargetTypeS3 || target.Name != "AWS Primary S3" {
			t.Fatalf("unexpected target data: %+v", target)
		}
	})

	t.Run("missing credential rejected", func(t *testing.T) {
		cfgJSON := []byte(`{"bucket":"my-backups","endpoint":"https://s3.amazonaws.com"}`)
		_, err := svc.CreateStorageTarget(context.Background(), orgDomain.RoleAdmin, orgID, CreateStorageTargetInput{
			Name:   "No Cred S3",
			Type:   domain.StorageTargetTypeS3,
			Config: cfgJSON,
		})
		if !errors.Is(err, domain.ErrStorageTargetCredentialRequired) {
			t.Fatalf("expected ErrStorageTargetCredentialRequired, got %v", err)
		}
	})

	t.Run("insecure HTTP endpoint rejected in strict mode", func(t *testing.T) {
		cfgJSON := []byte(`{"bucket":"my-backups","endpoint":"http://s3.amazonaws.com"}`)
		_, err := svc.CreateStorageTarget(context.Background(), orgDomain.RoleAdmin, orgID, CreateStorageTargetInput{
			Name:         "HTTP Insecure S3",
			Type:         domain.StorageTargetTypeS3,
			CredentialID: &credID,
			Config:       cfgJSON,
		})
		if !errors.Is(err, domain.ErrInvalidStorageTargetConfig) {
			t.Fatalf("expected ErrInvalidStorageTargetConfig, got %v", err)
		}
	})

	t.Run("creating local storage target manually rejected", func(t *testing.T) {
		_, err := svc.CreateStorageTarget(context.Background(), orgDomain.RoleAdmin, orgID, CreateStorageTargetInput{
			Name: "Custom Local",
			Type: domain.StorageTargetTypeLocal,
		})
		if err == nil {
			t.Fatalf("expected error when attempting to manually create local target")
		}
	})
}

func TestStorageTargetService_Immutability(t *testing.T) {
	repo := newMockStorageTargetRepo()
	credID := uuid.New()
	orgID := uuid.New()
	targetID := uuid.New()

	credFinder := &mockCredFinder{
		creds: map[uuid.UUID]*credDomain.CredentialMetadata{
			credID: {
				ID:             credID,
				OrganizationID: orgID,
				Type:           credDomain.TypeS3Credentials,
			},
		},
	}

	cfgJSON := []byte(`{"bucket":"my-backups","endpoint":"https://s3.amazonaws.com","region":"us-east-1"}`)
	repo.targets[targetID] = &domain.StorageTarget{
		ID:             targetID,
		OrganizationID: orgID,
		Name:           "Production S3",
		Type:           domain.StorageTargetTypeS3,
		Status:         domain.StorageTargetStatusActive,
		CredentialID:   &credID,
		Config:         cfgJSON,
	}

	secPolicy := &s3Storage.EndpointSecurityPolicy{AllowInsecureHTTP: false}
	svc := NewStorageTargetService(repo, credFinder, secPolicy, nil)

	t.Run("changing bucket rejected when artifacts exist", func(t *testing.T) {
		repo.artifactCount = 5
		newCfg := []byte(`{"bucket":"different-bucket","endpoint":"https://s3.amazonaws.com","region":"us-east-1"}`)

		_, err := svc.UpdateStorageTarget(context.Background(), orgDomain.RoleAdmin, orgID, targetID, UpdateStorageTargetInput{
			Name:         "Production S3",
			Status:       domain.StorageTargetStatusActive,
			CredentialID: &credID,
			Config:       newCfg,
		})
		if !errors.Is(err, domain.ErrStorageTargetLocationImmutable) {
			t.Fatalf("expected ErrStorageTargetLocationImmutable, got %v", err)
		}
	})

	t.Run("changing name succeeds even when artifacts exist", func(t *testing.T) {
		repo.artifactCount = 5
		sameCfg := []byte(`{"bucket":"my-backups","endpoint":"https://s3.amazonaws.com","region":"us-east-1"}`)

		updated, err := svc.UpdateStorageTarget(context.Background(), orgDomain.RoleAdmin, orgID, targetID, UpdateStorageTargetInput{
			Name:         "Renamed Production S3",
			Status:       domain.StorageTargetStatusActive,
			CredentialID: &credID,
			Config:       sameCfg,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if updated.Name != "Renamed Production S3" {
			t.Fatalf("expected updated name, got %s", updated.Name)
		}
	})

	t.Run("changing location succeeds when 0 artifacts exist", func(t *testing.T) {
		repo.artifactCount = 0
		newCfg := []byte(`{"bucket":"brand-new-bucket","endpoint":"https://s3.amazonaws.com","region":"us-east-1"}`)

		updated, err := svc.UpdateStorageTarget(context.Background(), orgDomain.RoleAdmin, orgID, targetID, UpdateStorageTargetInput{
			Name:         "Renamed Production S3",
			Status:       domain.StorageTargetStatusActive,
			CredentialID: &credID,
			Config:       newCfg,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		parsed, _ := domain.ParseS3TargetConfig(updated.Config)
		if parsed.Bucket != "brand-new-bucket" {
			t.Fatalf("expected bucket updated to brand-new-bucket, got %s", parsed.Bucket)
		}
	})
}

func TestStorageTargetService_DeleteValidation(t *testing.T) {
	repo := newMockStorageTargetRepo()
	orgID := uuid.New()

	secPolicy := &s3Storage.EndpointSecurityPolicy{AllowInsecureHTTP: false}
	svc := NewStorageTargetService(repo, nil, secPolicy, nil)

	defaultTarget, _ := repo.EnsureDefaultLocalStorageTarget(context.Background(), orgID)

	t.Run("cannot delete default target", func(t *testing.T) {
		err := svc.DeleteStorageTarget(context.Background(), orgDomain.RoleAdmin, orgID, defaultTarget.ID)
		if !errors.Is(err, domain.ErrCannotDeleteDefaultStorageTarget) {
			t.Fatalf("expected ErrCannotDeleteDefaultStorageTarget, got %v", err)
		}
	})

	customID := uuid.New()
	repo.targets[customID] = &domain.StorageTarget{
		ID:             customID,
		OrganizationID: orgID,
		Name:           "Custom S3",
		Type:           domain.StorageTargetTypeS3,
		Status:         domain.StorageTargetStatusActive,
	}

	t.Run("cannot delete target referenced by artifacts", func(t *testing.T) {
		repo.artifactCount = 1
		repo.planCount = 0
		repo.jobCount = 0
		err := svc.DeleteStorageTarget(context.Background(), orgDomain.RoleAdmin, orgID, customID)
		if !errors.Is(err, domain.ErrStorageTargetInUse) {
			t.Fatalf("expected ErrStorageTargetInUse, got %v", err)
		}
	})

	t.Run("cannot delete target referenced by active plans", func(t *testing.T) {
		repo.artifactCount = 0
		repo.planCount = 2
		repo.jobCount = 0
		err := svc.DeleteStorageTarget(context.Background(), orgDomain.RoleAdmin, orgID, customID)
		if !errors.Is(err, domain.ErrStorageTargetInUse) {
			t.Fatalf("expected ErrStorageTargetInUse, got %v", err)
		}
	})

	t.Run("cannot delete target referenced by active jobs", func(t *testing.T) {
		repo.artifactCount = 0
		repo.planCount = 0
		repo.jobCount = 1
		err := svc.DeleteStorageTarget(context.Background(), orgDomain.RoleAdmin, orgID, customID)
		if !errors.Is(err, domain.ErrStorageTargetInUse) {
			t.Fatalf("expected ErrStorageTargetInUse, got %v", err)
		}
	})

	t.Run("delete unused target succeeds", func(t *testing.T) {
		repo.artifactCount = 0
		repo.planCount = 0
		repo.jobCount = 0
		err := svc.DeleteStorageTarget(context.Background(), orgDomain.RoleAdmin, orgID, customID)
		if err != nil {
			t.Fatalf("unexpected delete error: %v", err)
		}
		if _, ok := repo.targets[customID]; ok {
			t.Fatalf("target was not deleted from repo")
		}
	})
}

func TestStorageTargetService_RBAC(t *testing.T) {
	repo := newMockStorageTargetRepo()
	orgID := uuid.New()
	targetID := uuid.New()
	repo.targets[targetID] = &domain.StorageTarget{
		ID:             targetID,
		OrganizationID: orgID,
		Name:           "Test Target",
		Type:           domain.StorageTargetTypeLocal,
		Status:         domain.StorageTargetStatusActive,
	}

	secPolicy := &s3Storage.EndpointSecurityPolicy{AllowInsecureHTTP: false}
	svc := NewStorageTargetService(repo, nil, secPolicy, nil)
	ctx := context.Background()

	t.Run("GetStorageTarget allows Admin, Member, and Viewer", func(t *testing.T) {
		roles := []orgDomain.Role{orgDomain.RoleAdmin, orgDomain.RoleMember, orgDomain.RoleViewer}
		for _, role := range roles {
			target, err := svc.GetStorageTarget(ctx, role, orgID, targetID)
			if err != nil {
				t.Errorf("expected role %s to be allowed in GetStorageTarget, got: %v", role, err)
			}
			if target == nil || target.ID != targetID {
				t.Errorf("expected target %s returned for role %s", targetID, role)
			}
		}

		// Unknown / empty role rejected
		_, err := svc.GetStorageTarget(ctx, orgDomain.Role("anonymous"), orgID, targetID)
		if !errors.Is(err, domain.ErrUnauthorizedRole) {
			t.Errorf("expected ErrUnauthorizedRole for anonymous role, got: %v", err)
		}
	})

	t.Run("ListStorageTargets allows Admin, Member, and Viewer", func(t *testing.T) {
		roles := []orgDomain.Role{orgDomain.RoleAdmin, orgDomain.RoleMember, orgDomain.RoleViewer}
		for _, role := range roles {
			targets, err := svc.ListStorageTargets(ctx, role, orgID)
			if err != nil {
				t.Errorf("expected role %s to be allowed in ListStorageTargets, got: %v", role, err)
			}
			if len(targets) != 1 {
				t.Errorf("expected 1 target returned for role %s, got: %d", role, len(targets))
			}
		}

		// Unknown / empty role rejected
		_, err := svc.ListStorageTargets(ctx, orgDomain.Role("anonymous"), orgID)
		if !errors.Is(err, domain.ErrUnauthorizedRole) {
			t.Errorf("expected ErrUnauthorizedRole for anonymous role, got: %v", err)
		}
	})

	t.Run("Write operations remain strictly Admin-only", func(t *testing.T) {
		nonAdminRoles := []orgDomain.Role{orgDomain.RoleMember, orgDomain.RoleViewer, orgDomain.Role("anonymous")}
		for _, role := range nonAdminRoles {
			_, err := svc.CreateStorageTarget(ctx, role, orgID, CreateStorageTargetInput{})
			if !errors.Is(err, domain.ErrUnauthorizedRole) {
				t.Errorf("expected CreateStorageTarget to reject role %s with ErrUnauthorizedRole, got: %v", role, err)
			}

			_, err = svc.UpdateStorageTarget(ctx, role, orgID, targetID, UpdateStorageTargetInput{})
			if !errors.Is(err, domain.ErrUnauthorizedRole) {
				t.Errorf("expected UpdateStorageTarget to reject role %s with ErrUnauthorizedRole, got: %v", role, err)
			}

			err = svc.DeleteStorageTarget(ctx, role, orgID, targetID)
			if !errors.Is(err, domain.ErrUnauthorizedRole) {
				t.Errorf("expected DeleteStorageTarget to reject role %s with ErrUnauthorizedRole, got: %v", role, err)
			}
		}
	})
}

func TestStorageTargetService_RepositoryInUseProtection(t *testing.T) {
	ctx := context.Background()
	orgID := uuid.New()
	credID := uuid.New()
	targetID := uuid.New()

	repo := newMockStorageTargetRepo()
	credFinder := &mockCredFinder{
		creds: map[uuid.UUID]*credDomain.CredentialMetadata{
			credID: {
				ID:             credID,
				OrganizationID: orgID,
				Type:           credDomain.TypeS3Credentials,
			},
		},
	}
	secPolicy := &s3Storage.EndpointSecurityPolicy{AllowInsecureHTTP: false}
	svc := NewStorageTargetService(repo, credFinder, secPolicy, nil)

	origConfig := []byte(`{"bucket":"orig-bucket","endpoint":"https://s3.us-east-1.amazonaws.com","region":"us-east-1","force_path_style":false,"prefix":"backups/orig"}`)

	target := &domain.StorageTarget{
		ID:             targetID,
		OrganizationID: orgID,
		Name:           "Production S3",
		Type:           domain.StorageTargetTypeS3,
		Status:         domain.StorageTargetStatusActive,
		IsDefault:      false,
		Config:         origConfig,
		CredentialID:   &credID,
	}
	repo.targets[targetID] = target

	// Mark target as referenced by a restic repository
	repo.repoCount = 1

	t.Run("bucket change is blocked when repository exists", func(t *testing.T) {
		newCfg := []byte(`{"bucket":"new-bucket","endpoint":"https://s3.us-east-1.amazonaws.com","region":"us-east-1","force_path_style":false,"prefix":"backups/orig"}`)
		_, err := svc.UpdateStorageTarget(ctx, orgDomain.RoleAdmin, orgID, targetID, UpdateStorageTargetInput{
			Name:         "Production S3",
			Status:       domain.StorageTargetStatusActive,
			CredentialID: &credID,
			Config:       newCfg,
		})
		if !errors.Is(err, domain.ErrStorageTargetLocationImmutable) {
			t.Errorf("expected ErrStorageTargetLocationImmutable, got: %v", err)
		}
	})

	t.Run("endpoint change is blocked when repository exists", func(t *testing.T) {
		newCfg := []byte(`{"bucket":"orig-bucket","endpoint":"https://s3.eu-central-1.amazonaws.com","region":"us-east-1","force_path_style":false,"prefix":"backups/orig"}`)
		_, err := svc.UpdateStorageTarget(ctx, orgDomain.RoleAdmin, orgID, targetID, UpdateStorageTargetInput{
			Name:         "Production S3",
			Status:       domain.StorageTargetStatusActive,
			CredentialID: &credID,
			Config:       newCfg,
		})
		if !errors.Is(err, domain.ErrStorageTargetLocationImmutable) {
			t.Errorf("expected ErrStorageTargetLocationImmutable, got: %v", err)
		}
	})

	t.Run("region change is blocked when repository exists", func(t *testing.T) {
		newCfg := []byte(`{"bucket":"orig-bucket","endpoint":"https://s3.us-east-1.amazonaws.com","region":"eu-west-1","force_path_style":false,"prefix":"backups/orig"}`)
		_, err := svc.UpdateStorageTarget(ctx, orgDomain.RoleAdmin, orgID, targetID, UpdateStorageTargetInput{
			Name:         "Production S3",
			Status:       domain.StorageTargetStatusActive,
			CredentialID: &credID,
			Config:       newCfg,
		})
		if !errors.Is(err, domain.ErrStorageTargetLocationImmutable) {
			t.Errorf("expected ErrStorageTargetLocationImmutable, got: %v", err)
		}
	})

	t.Run("force_path_style change is blocked when repository exists", func(t *testing.T) {
		newCfg := []byte(`{"bucket":"orig-bucket","endpoint":"https://s3.us-east-1.amazonaws.com","region":"us-east-1","force_path_style":true,"prefix":"backups/orig"}`)
		_, err := svc.UpdateStorageTarget(ctx, orgDomain.RoleAdmin, orgID, targetID, UpdateStorageTargetInput{
			Name:         "Production S3",
			Status:       domain.StorageTargetStatusActive,
			CredentialID: &credID,
			Config:       newCfg,
		})
		if !errors.Is(err, domain.ErrStorageTargetLocationImmutable) {
			t.Errorf("expected ErrStorageTargetLocationImmutable, got: %v", err)
		}
	})

	t.Run("prefix change is blocked when repository exists", func(t *testing.T) {
		newCfg := []byte(`{"bucket":"orig-bucket","endpoint":"https://s3.us-east-1.amazonaws.com","region":"us-east-1","force_path_style":false,"prefix":"backups/new-prefix"}`)
		_, err := svc.UpdateStorageTarget(ctx, orgDomain.RoleAdmin, orgID, targetID, UpdateStorageTargetInput{
			Name:         "Production S3",
			Status:       domain.StorageTargetStatusActive,
			CredentialID: &credID,
			Config:       newCfg,
		})
		if !errors.Is(err, domain.ErrStorageTargetLocationImmutable) {
			t.Errorf("expected ErrStorageTargetLocationImmutable, got: %v", err)
		}
	})

	t.Run("delete target is blocked when repository exists", func(t *testing.T) {
		err := svc.DeleteStorageTarget(ctx, orgDomain.RoleAdmin, orgID, targetID)
		if !errors.Is(err, domain.ErrStorageTargetInUse) {
			t.Errorf("expected ErrStorageTargetInUse, got: %v", err)
		}
	})

	t.Run("archiving/disabling target is blocked when repository exists", func(t *testing.T) {
		_, err := svc.UpdateStorageTarget(ctx, orgDomain.RoleAdmin, orgID, targetID, UpdateStorageTargetInput{
			Name:         "Production S3",
			Status:       domain.StorageTargetStatusDisabled,
			CredentialID: &credID,
			Config:       origConfig,
		})
		if !errors.Is(err, domain.ErrStorageTargetInUse) {
			t.Errorf("expected ErrStorageTargetInUse, got: %v", err)
		}
	})

	t.Run("name change is allowed when repository exists", func(t *testing.T) {
		updated, err := svc.UpdateStorageTarget(ctx, orgDomain.RoleAdmin, orgID, targetID, UpdateStorageTargetInput{
			Name:         "Production S3 Renamed",
			Status:       domain.StorageTargetStatusActive,
			CredentialID: &credID,
			Config:       origConfig,
		})
		if err != nil {
			t.Fatalf("expected name update to succeed, got: %v", err)
		}
		if updated.Name != "Production S3 Renamed" {
			t.Errorf("expected updated name Production S3 Renamed, got %s", updated.Name)
		}
	})
}
