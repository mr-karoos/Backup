package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"backup-platform/internal/backup/domain"
	"backup-platform/internal/backup/restic"
	credDomain "backup-platform/internal/credential/domain"
	resDomain "backup-platform/internal/resource/domain"
	"backup-platform/pkg/uuid"
)

type mockSystemVault struct {
	creds       map[uuid.UUID][]byte
	meta        map[uuid.UUID]*credDomain.CredentialMetadata
	createErr   error
	loadErr     error
	deleteErr   error
	deleteCalls []uuid.UUID
}

func (m *mockSystemVault) CreateSystemCredential(
	ctx context.Context,
	orgID uuid.UUID,
	name string,
	credType credDomain.Type,
	plaintextPayload []byte,
) (*credDomain.CredentialMetadata, error) {
	if m.createErr != nil {
		return nil, m.createErr
	}
	id := uuid.New()
	stored := make([]byte, len(plaintextPayload))
	copy(stored, plaintextPayload)
	m.creds[id] = stored
	meta := &credDomain.CredentialMetadata{
		ID:             id,
		OrganizationID: orgID,
		Name:           name,
		Type:           credType,
		ManagedBy:      credDomain.ManagedBySystem,
		KeyVersion:     1,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}
	m.meta[id] = meta
	return meta, nil
}

func (m *mockSystemVault) LoadCredentialForUse(ctx context.Context, orgID, credID uuid.UUID) (credDomain.Type, []byte, error) {
	if m.loadErr != nil {
		return "", nil, m.loadErr
	}
	if b, ok := m.creds[credID]; ok {
		cpy := make([]byte, len(b))
		copy(cpy, b)
		return credDomain.TypeResticRepositoryKey, cpy, nil
	}
	return "", nil, credDomain.ErrCredentialNotFound
}

func (m *mockSystemVault) DeleteSystemCredential(ctx context.Context, orgID, credID uuid.UUID) error {
	m.deleteCalls = append(m.deleteCalls, credID)
	if m.deleteErr != nil {
		return m.deleteErr
	}
	delete(m.creds, credID)
	delete(m.meta, credID)
	return nil
}

type mockResticRunner struct {
	initErr    error
	probeErr   error
	initCalls  int
	probeCalls int
}

func (m *mockResticRunner) Init(ctx context.Context, target restic.RepositoryTarget, password []byte) error {
	m.initCalls++
	return m.initErr
}

func (m *mockResticRunner) Probe(ctx context.Context, target restic.RepositoryTarget, password []byte) error {
	m.probeCalls++
	return m.probeErr
}

type mockResticTarget struct {
	tType   string
	locator string
	url     string
	cleaned bool
}

func (m *mockResticTarget) Type() string                { return m.tType }
func (m *mockResticTarget) Locator() string             { return m.locator }
func (m *mockResticTarget) ResticRepositoryURL() string { return m.url }
func (m *mockResticTarget) Env() []string               { return nil }
func (m *mockResticTarget) Cleanup()                    { m.cleaned = true }

type mockTargetResolver struct {
	resolvedTarget restic.RepositoryTarget
	resolveErr     error
}

func (m *mockTargetResolver) ResolveTarget(ctx context.Context, orgID, resourceID uuid.UUID, target *domain.StorageTarget) (restic.RepositoryTarget, error) {
	if m.resolveErr != nil {
		return nil, m.resolveErr
	}
	if m.resolvedTarget != nil {
		return m.resolvedTarget, nil
	}
	return &mockResticTarget{
		tType:   string(target.Type),
		locator: "repositories/test/" + resourceID.String(),
		url:     "/var/data/repositories/test/" + resourceID.String(),
	}, nil
}

type fullMockBackupRepo struct {
	mockBackupRepo
	reposByResID map[uuid.UUID]*domain.BackupRepository
	reposByID    map[uuid.UUID]*domain.BackupRepository
	createErr    error
}

func (f *fullMockBackupRepo) CreateRepository(ctx context.Context, repo *domain.BackupRepository) (*domain.BackupRepository, error) {
	if f.createErr != nil {
		return nil, f.createErr
	}
	if _, exists := f.reposByResID[repo.ResourceID]; exists {
		return nil, domain.ErrRepositoryAlreadyExists
	}
	f.reposByResID[repo.ResourceID] = repo
	f.reposByID[repo.ID] = repo
	return repo, nil
}

func (f *fullMockBackupRepo) GetRepositoryByResourceID(ctx context.Context, orgID, resourceID uuid.UUID) (*domain.BackupRepository, error) {
	if r, ok := f.reposByResID[resourceID]; ok && r.OrganizationID == orgID {
		return r, nil
	}
	return nil, domain.ErrRepositoryNotFound
}

func (f *fullMockBackupRepo) GetRepositoryByID(ctx context.Context, orgID, repoID uuid.UUID) (*domain.BackupRepository, error) {
	if r, ok := f.reposByID[repoID]; ok && r.OrganizationID == orgID {
		return r, nil
	}
	return nil, domain.ErrRepositoryNotFound
}

func TestRepositoryService_EnsureRepository(t *testing.T) {
	ctx := context.Background()
	orgID := uuid.New()
	resID := uuid.New()
	targetID := uuid.New()

	setup := func() (
		*fullMockBackupRepo,
		*mockResourceFinder,
		*mockSystemVault,
		*mockTargetResolver,
		*mockResticRunner,
		*RepositoryService,
	) {
		freshResource := &resDomain.Resource{
			ID:             resID,
			OrganizationID: orgID,
			Name:           "Test Resource",
			Status:         resDomain.StatusActive,
		}

		freshTarget := &domain.StorageTarget{
			ID:             targetID,
			OrganizationID: orgID,
			Name:           "Local Target",
			Type:           domain.StorageTargetTypeLocal,
			Status:         domain.StorageTargetStatusActive,
		}

		repo := &fullMockBackupRepo{
			reposByResID: make(map[uuid.UUID]*domain.BackupRepository),
			reposByID:    make(map[uuid.UUID]*domain.BackupRepository),
		}
		repo.mockBackupRepo.targets = map[uuid.UUID]*domain.StorageTarget{targetID: freshTarget}

		resFinder := &mockResourceFinder{
			resources: map[uuid.UUID]*resDomain.Resource{resID: freshResource},
		}

		vault := &mockSystemVault{
			creds: make(map[uuid.UUID][]byte),
			meta:  make(map[uuid.UUID]*credDomain.CredentialMetadata),
		}

		resolver := &mockTargetResolver{}
		runner := &mockResticRunner{}

		svc := NewRepositoryService(repo, repo, resFinder, vault, resolver, runner, nil)
		return repo, resFinder, vault, resolver, runner, svc
	}

	t.Run("successfully provisions fresh local repository", func(t *testing.T) {
		_, _, vault, _, runner, svc := setup()

		repo, err := svc.EnsureRepository(ctx, orgID, resID, targetID)
		if err != nil {
			t.Fatalf("expected success, got: %v", err)
		}
		if repo.ResourceID != resID || repo.OrganizationID != orgID || repo.StorageTargetID != targetID {
			t.Errorf("unexpected repository: %+v", repo)
		}
		if runner.initCalls != 1 {
			t.Errorf("expected 1 init call, got %d", runner.initCalls)
		}
		if len(vault.creds) != 1 {
			t.Errorf("expected 1 system credential in vault, got %d", len(vault.creds))
		}
	})

	t.Run("returns existing repository if already created and probe passes", func(t *testing.T) {
		repoMock, _, vault, _, runner, svc := setup()

		// First ensure creates
		repo1, err := svc.EnsureRepository(ctx, orgID, resID, targetID)
		if err != nil {
			t.Fatalf("first ensure failed: %v", err)
		}

		// Second ensure reuses and probes
		repo2, err := svc.EnsureRepository(ctx, orgID, resID, targetID)
		if err != nil {
			t.Fatalf("second ensure failed: %v", err)
		}

		if repo1.ID != repo2.ID {
			t.Errorf("expected same repository ID, got %s and %s", repo1.ID, repo2.ID)
		}
		if runner.initCalls != 1 {
			t.Errorf("expected init called only once, got %d", runner.initCalls)
		}
		if runner.probeCalls != 1 {
			t.Errorf("expected 1 probe call on second ensure, got %d", runner.probeCalls)
		}
		if len(vault.creds) != 1 {
			t.Errorf("expected credential count to remain 1")
		}
		_ = repoMock
	})

	t.Run("rejects storage target change on existing repository", func(t *testing.T) {
		repoMock, _, _, _, _, svc := setup()

		newTargetID := uuid.New()
		repoMock.mockBackupRepo.targets[newTargetID] = &domain.StorageTarget{
			ID:             newTargetID,
			OrganizationID: orgID,
			Name:           "Other Target",
			Type:           domain.StorageTargetTypeLocal,
			Status:         domain.StorageTargetStatusActive,
		}

		// Create with targetID
		_, err := svc.EnsureRepository(ctx, orgID, resID, targetID)
		if err != nil {
			t.Fatalf("ensure failed: %v", err)
		}

		// Attempt ensure with newTargetID
		_, err = svc.EnsureRepository(ctx, orgID, resID, newTargetID)
		if !errors.Is(err, domain.ErrRepositoryTargetMismatch) {
			t.Errorf("expected ErrRepositoryTargetMismatch, got: %v", err)
		}
	})

	t.Run("returns ErrRepositoryCorrupted when probe fails on existing repository", func(t *testing.T) {
		_, _, _, _, runner, svc := setup()

		// Create
		_, err := svc.EnsureRepository(ctx, orgID, resID, targetID)
		if err != nil {
			t.Fatalf("ensure failed: %v", err)
		}

		// Set probe to fail
		runner.probeErr = errors.New("config file missing")

		_, err = svc.EnsureRepository(ctx, orgID, resID, targetID)
		if !errors.Is(err, domain.ErrRepositoryCorrupted) {
			t.Errorf("expected ErrRepositoryCorrupted, got: %v", err)
		}
	})

	t.Run("rolls back system credential if restic init fails", func(t *testing.T) {
		_, _, vault, _, runner, svc := setup()

		runner.initErr = errors.New("permission denied on local path")

		_, err := svc.EnsureRepository(ctx, orgID, resID, targetID)
		if err == nil || !strings.Contains(err.Error(), "permission denied") {
			t.Fatalf("expected permission denied error, got: %v", err)
		}

		if len(vault.creds) != 0 {
			t.Errorf("expected system credential to be rolled back, but found %d", len(vault.creds))
		}
		if len(vault.deleteCalls) != 1 {
			t.Errorf("expected 1 DeleteSystemCredential call, got %d", len(vault.deleteCalls))
		}
	})

	t.Run("rejects archived or disabled resource", func(t *testing.T) {
		_, resFinder, _, _, _, svc := setup()

		resFinder.resources[resID].Status = resDomain.StatusArchived
		_, err := svc.EnsureRepository(ctx, orgID, resID, targetID)
		if !errors.Is(err, domain.ErrResourceArchived) {
			t.Errorf("expected ErrResourceArchived, got: %v", err)
		}

		resFinder.resources[resID].Status = resDomain.StatusDisabled
		_, err = svc.EnsureRepository(ctx, orgID, resID, targetID)
		if !errors.Is(err, domain.ErrResourceDisabled) {
			t.Errorf("expected ErrResourceDisabled, got: %v", err)
		}
	})

	t.Run("rejects inactive or unsupported storage target", func(t *testing.T) {
		repoMock, _, _, _, _, svc := setup()

		repoMock.mockBackupRepo.targets[targetID].Status = domain.StorageTargetStatusDisabled
		_, err := svc.EnsureRepository(ctx, orgID, resID, targetID)
		if !errors.Is(err, domain.ErrStorageTargetNotActive) {
			t.Errorf("expected ErrStorageTargetNotActive, got: %v", err)
		}

		repoMock.mockBackupRepo.targets[targetID].Status = domain.StorageTargetStatusActive
		repoMock.mockBackupRepo.targets[targetID].Type = domain.StorageTargetTypeRemoteSSH
		_, err = svc.EnsureRepository(ctx, orgID, resID, targetID)
		if !errors.Is(err, domain.ErrStorageTargetNotSupported) {
			t.Errorf("expected ErrStorageTargetNotSupported, got: %v", err)
		}
	})
}
