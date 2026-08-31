package service

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"backup-platform/internal/organization/domain"
	orgRepo "backup-platform/internal/organization/repository"
	"backup-platform/internal/platform/database"
	"backup-platform/pkg/uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type mockQuerier struct{}

func (m *mockQuerier) Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}
func (m *mockQuerier) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return nil, nil
}
func (m *mockQuerier) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return nil
}

type mockTxManager struct {
	querier       database.Querier
	orgRepo       *inMemoryOrgRepo
	memberRepo    *inMemoryMemberRepo
	beginErr      error
	commitErr     error
	beginCalls    int
	commitCalls   int
	rollbackCalls int
}

func (m *mockTxManager) Querier() database.Querier { return m.querier }

func (m *mockTxManager) WithinTx(ctx context.Context, fn func(q database.Querier) error) error {
	m.beginCalls++
	if m.beginErr != nil {
		return m.beginErr
	}

	// Capture deep snapshot of repository state prior to transaction execution
	orgSnapshot := make(map[uuid.UUID]*domain.Organization)
	if m.orgRepo != nil {
		for k, v := range m.orgRepo.orgs {
			orgCopy := *v
			orgSnapshot[k] = &orgCopy
		}
	}

	memberSnapshot := make(map[string]*domain.Member)
	if m.memberRepo != nil {
		for k, v := range m.memberRepo.members {
			memberCopy := *v
			memberSnapshot[k] = &memberCopy
		}
	}

	// Execute transaction callback
	if err := fn(m.querier); err != nil {
		m.rollbackCalls++
		if m.orgRepo != nil {
			m.orgRepo.orgs = orgSnapshot
		}
		if m.memberRepo != nil {
			m.memberRepo.members = memberSnapshot
		}
		return err
	}

	// Execute simulated commit
	if m.commitErr != nil {
		m.rollbackCalls++
		if m.orgRepo != nil {
			m.orgRepo.orgs = orgSnapshot
		}
		if m.memberRepo != nil {
			m.memberRepo.members = memberSnapshot
		}
		return m.commitErr
	}

	m.commitCalls++
	return nil
}

type inMemoryOrgRepo struct {
	orgs        map[uuid.UUID]*domain.Organization
	overrideErr error
}

func newInMemoryOrgRepo() *inMemoryOrgRepo {
	return &inMemoryOrgRepo{orgs: make(map[uuid.UUID]*domain.Organization)}
}

func (r *inMemoryOrgRepo) Create(ctx context.Context, q database.Querier, org *domain.Organization) error {
	if r.overrideErr != nil {
		return r.overrideErr
	}
	for _, o := range r.orgs {
		if strings.EqualFold(o.Slug, org.Slug) {
			return domain.ErrDuplicateOrgSlug
		}
	}
	r.orgs[org.ID] = org
	return nil
}

func (r *inMemoryOrgRepo) FindByID(ctx context.Context, q database.Querier, id uuid.UUID) (*domain.Organization, error) {
	if r.overrideErr != nil {
		return nil, r.overrideErr
	}
	o, ok := r.orgs[id]
	if !ok {
		return nil, domain.ErrOrgNotFound
	}
	return o, nil
}

func (r *inMemoryOrgRepo) FindActiveByID(ctx context.Context, q database.Querier, id uuid.UUID) (*domain.Organization, error) {
	if r.overrideErr != nil {
		return nil, r.overrideErr
	}
	o, ok := r.orgs[id]
	if !ok || o.Status != domain.OrgStatusActive {
		return nil, domain.ErrOrgNotFound
	}
	return o, nil
}

func (r *inMemoryOrgRepo) FindBySlug(ctx context.Context, q database.Querier, slug string) (*domain.Organization, error) {
	for _, o := range r.orgs {
		if strings.EqualFold(o.Slug, slug) {
			return o, nil
		}
	}
	return nil, domain.ErrOrgNotFound
}

func (r *inMemoryOrgRepo) FindDefaultInternal(ctx context.Context, q database.Querier) (*domain.Organization, error) {
	for _, o := range r.orgs {
		if o.IsDefaultInternal {
			return o, nil
		}
	}
	return nil, domain.ErrOrgNotFound
}

func (r *inMemoryOrgRepo) UpdateActive(ctx context.Context, q database.Querier, id uuid.UUID, name string, metadata []byte) (*domain.Organization, error) {
	if r.overrideErr != nil {
		return nil, r.overrideErr
	}
	o, ok := r.orgs[id]
	if !ok || o.Status != domain.OrgStatusActive {
		return nil, domain.ErrOrgNotFound
	}
	if len(metadata) == 0 {
		metadata = []byte("{}")
	}
	o.Name = name
	o.Metadata = metadata
	o.UpdatedAt = time.Now().UTC()
	return o, nil
}

type inMemoryMemberRepo struct {
	members     map[string]*domain.Member
	userOrgs    map[uuid.UUID][]*orgRepo.UserMembershipWithOrg
	overrideErr error
}

func newInMemoryMemberRepo() *inMemoryMemberRepo {
	return &inMemoryMemberRepo{
		members:  make(map[string]*domain.Member),
		userOrgs: make(map[uuid.UUID][]*orgRepo.UserMembershipWithOrg),
	}
}

func (r *inMemoryMemberRepo) Create(ctx context.Context, q database.Querier, member *domain.Member) error {
	if r.overrideErr != nil {
		return r.overrideErr
	}
	key := member.OrganizationID.String() + ":" + member.UserID.String()
	if _, exists := r.members[key]; exists {
		return domain.ErrDuplicateMembership
	}
	r.members[key] = member
	return nil
}

func (r *inMemoryMemberRepo) FindMembership(ctx context.Context, q database.Querier, orgID, userID uuid.UUID) (*domain.Member, error) {
	key := orgID.String() + ":" + userID.String()
	m, ok := r.members[key]
	if !ok {
		return nil, domain.ErrMemberNotFound
	}
	return m, nil
}

func (r *inMemoryMemberRepo) FindActiveMembershipWithOrg(ctx context.Context, q database.Querier, orgID, userID uuid.UUID) (*orgRepo.UserMembershipWithOrg, error) {
	return nil, nil
}

func (r *inMemoryMemberRepo) ListUserOrganizations(ctx context.Context, q database.Querier, userID uuid.UUID) ([]*domain.Organization, error) {
	return nil, nil
}

func (r *inMemoryMemberRepo) ListUserMembershipsWithOrg(ctx context.Context, q database.Querier, userID uuid.UUID) ([]*orgRepo.UserMembershipWithOrg, error) {
	if r.overrideErr != nil {
		return nil, r.overrideErr
	}
	return r.userOrgs[userID], nil
}

func setupServiceTest() (*Service, *inMemoryOrgRepo, *inMemoryMemberRepo, *mockTxManager) {
	orgR := newInMemoryOrgRepo()
	memR := newInMemoryMemberRepo()
	txM := &mockTxManager{
		querier:    &mockQuerier{},
		orgRepo:    orgR,
		memberRepo: memR,
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := NewService(orgR, memR, txM, logger)
	return svc, orgR, memR, txM
}

func TestOrganizationService_CreateOrganization(t *testing.T) {
	ctx := context.Background()
	actorUserID := uuid.New()

	t.Run("successfully creates organization and creator admin membership atomically", func(t *testing.T) {
		svc, orgR, memR, txM := setupServiceTest()

		input := CreateOrganizationInput{
			Name:     "Acme Corporation",
			Slug:     "acme-corp",
			Metadata: []byte(`{"plan": "standard"}`),
		}

		org, err := svc.CreateOrganization(ctx, actorUserID, input)
		if err != nil {
			t.Fatalf("expected successful organization creation, got error: %v", err)
		}

		if org.Name != "Acme Corporation" {
			t.Errorf("expected name 'Acme Corporation', got: %s", org.Name)
		}
		if org.Slug != "acme-corp" {
			t.Errorf("expected slug 'acme-corp', got: %s", org.Slug)
		}
		if org.IsDefaultInternal {
			t.Errorf("expected is_default_internal = false")
		}
		if org.Status != domain.OrgStatusActive {
			t.Errorf("expected status = active")
		}
		if txM.commitCalls != 1 {
			t.Errorf("expected 1 successful commit call, got: %d", txM.commitCalls)
		}
		if txM.rollbackCalls != 0 {
			t.Errorf("expected 0 rollback calls, got: %d", txM.rollbackCalls)
		}

		// Verify organization was persisted
		if len(orgR.orgs) != 1 {
			t.Fatalf("expected exactly 1 organization in repo, got: %d", len(orgR.orgs))
		}
		persistedOrg, ok := orgR.orgs[org.ID]
		if !ok || persistedOrg == nil {
			t.Fatalf("expected organization to be in repo")
		}

		// Verify creator membership was persisted with Admin role
		if len(memR.members) != 1 {
			t.Fatalf("expected exactly 1 membership in repo, got: %d", len(memR.members))
		}
		memberKey := org.ID.String() + ":" + actorUserID.String()
		member, ok := memR.members[memberKey]
		if !ok || member == nil {
			t.Fatalf("expected creator admin membership to be created in repo")
		}
		if member.OrganizationID != org.ID {
			t.Errorf("expected membership OrgID %s, got: %s", org.ID, member.OrganizationID)
		}
		if member.UserID != actorUserID {
			t.Errorf("expected membership UserID %s, got: %s", actorUserID, member.UserID)
		}
		if member.Role != domain.RoleAdmin {
			t.Errorf("expected creator role to be admin, got: %s", member.Role)
		}
		if member.Status != domain.MemberStatusActive {
			t.Errorf("expected creator membership status to be active, got: %s", member.Status)
		}
	})

	t.Run("fails validation outside transaction for invalid input", func(t *testing.T) {
		svc, _, _, txM := setupServiceTest()

		// Invalid empty name
		_, err := svc.CreateOrganization(ctx, actorUserID, CreateOrganizationInput{
			Name: "",
			Slug: "valid-slug",
		})
		if !errors.Is(err, domain.ErrInvalidOrgName) {
			t.Errorf("expected ErrInvalidOrgName, got: %v", err)
		}
		if txM.beginCalls != 0 {
			t.Errorf("expected 0 transaction calls on validation failure, got: %d", txM.beginCalls)
		}

		// Invalid slug (contains digits)
		_, err = svc.CreateOrganization(ctx, actorUserID, CreateOrganizationInput{
			Name: "Valid Name",
			Slug: "acme-123",
		})
		if !errors.Is(err, domain.ErrInvalidOrgSlug) {
			t.Errorf("expected ErrInvalidOrgSlug, got: %v", err)
		}
		if txM.beginCalls != 0 {
			t.Errorf("expected 0 transaction calls on validation failure, got: %d", txM.beginCalls)
		}

		// Invalid metadata
		_, err = svc.CreateOrganization(ctx, actorUserID, CreateOrganizationInput{
			Name:     "Valid Name",
			Slug:     "valid-slug",
			Metadata: []byte(`[1, 2, 3]`),
		})
		if !errors.Is(err, domain.ErrInvalidMetadata) {
			t.Errorf("expected ErrInvalidMetadata, got: %v", err)
		}
		if txM.beginCalls != 0 {
			t.Errorf("expected 0 transaction calls on validation failure, got: %d", txM.beginCalls)
		}
	})

	t.Run("duplicate slug returns ErrDuplicateOrgSlug", func(t *testing.T) {
		svc, _, _, _ := setupServiceTest()

		input := CreateOrganizationInput{
			Name: "First Org",
			Slug: "acme-corp",
		}
		_, err := svc.CreateOrganization(ctx, actorUserID, input)
		if err != nil {
			t.Fatalf("first creation should succeed: %v", err)
		}

		// Second creation with same slug
		_, err = svc.CreateOrganization(ctx, actorUserID, CreateOrganizationInput{
			Name: "Second Org",
			Slug: "acme-corp",
		})
		if !errors.Is(err, domain.ErrDuplicateOrgSlug) {
			t.Errorf("expected ErrDuplicateOrgSlug, got: %v", err)
		}
	})

	t.Run("membership creation failure rolls back transaction and restores clean state", func(t *testing.T) {
		svc, orgR, memR, txM := setupServiceTest()
		memR.overrideErr = errors.New("pq: disk full or connection error")

		input := CreateOrganizationInput{
			Name: "Acme Corp",
			Slug: "acme-corp",
		}

		org, err := svc.CreateOrganization(ctx, actorUserID, input)
		if org != nil {
			t.Errorf("expected nil org on failure")
		}
		if !errors.Is(err, ErrOrganizationServiceUnavailable) {
			t.Errorf("expected ErrOrganizationServiceUnavailable, got: %v", err)
		}
		if txM.rollbackCalls != 1 {
			t.Errorf("expected 1 rollback call, got: %d", txM.rollbackCalls)
		}
		if txM.commitCalls != 0 {
			t.Errorf("expected 0 commit calls on rollback, got: %d", txM.commitCalls)
		}
		if len(orgR.orgs) != 0 {
			t.Errorf("expected 0 organizations in repo after rollback, got: %d", len(orgR.orgs))
		}
		if len(memR.members) != 0 {
			t.Errorf("expected 0 memberships in repo after rollback, got: %d", len(memR.members))
		}
	})

	t.Run("commit failure rolls back transaction and restores clean state", func(t *testing.T) {
		svc, orgR, memR, txM := setupServiceTest()
		txM.commitErr = errors.New("pq: simulated transaction commit failure")

		input := CreateOrganizationInput{
			Name: "Acme Corp",
			Slug: "acme-corp",
		}

		org, err := svc.CreateOrganization(ctx, actorUserID, input)
		if org != nil {
			t.Errorf("expected nil org on failure")
		}
		if !errors.Is(err, ErrOrganizationServiceUnavailable) {
			t.Errorf("expected ErrOrganizationServiceUnavailable, got: %v", err)
		}
		if txM.rollbackCalls != 1 {
			t.Errorf("expected 1 rollback call, got: %d", txM.rollbackCalls)
		}
		if txM.commitCalls != 0 {
			t.Errorf("expected 0 commit calls on rollback, got: %d", txM.commitCalls)
		}
		if len(orgR.orgs) != 0 {
			t.Errorf("expected 0 organizations in repo after rollback, got: %d", len(orgR.orgs))
		}
		if len(memR.members) != 0 {
			t.Errorf("expected 0 memberships in repo after rollback, got: %d", len(memR.members))
		}
	})
}

func TestOrganizationService_ListUserOrganizations(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()

	t.Run("returns list of user memberships", func(t *testing.T) {
		svc, _, memR, _ := setupServiceTest()

		orgID1 := uuid.New()
		orgID2 := uuid.New()
		now := time.Now().UTC()

		memR.userOrgs[userID] = []*orgRepo.UserMembershipWithOrg{
			{
				OrganizationID:     orgID1,
				OrganizationName:   "Org 1",
				Slug:               "org-one",
				IsDefaultInternal:  true,
				OrganizationStatus: domain.OrgStatusActive,
				Role:               domain.RoleAdmin,
				Status:             domain.MemberStatusActive,
				CreatedAt:          now,
			},
			{
				OrganizationID:     orgID2,
				OrganizationName:   "Org 2",
				Slug:               "org-two",
				IsDefaultInternal:  false,
				OrganizationStatus: domain.OrgStatusActive,
				Role:               domain.RoleMember,
				Status:             domain.MemberStatusActive,
				CreatedAt:          now,
			},
		}

		res, err := svc.ListUserOrganizations(ctx, userID)
		if err != nil {
			t.Fatalf("expected success, got: %v", err)
		}
		if len(res) != 2 {
			t.Fatalf("expected 2 organizations, got: %d", len(res))
		}
	})

	t.Run("returns empty slice when user has no organizations", func(t *testing.T) {
		svc, _, _, _ := setupServiceTest()

		res, err := svc.ListUserOrganizations(ctx, userID)
		if err != nil {
			t.Fatalf("expected success, got: %v", err)
		}
		if res == nil || len(res) != 0 {
			t.Errorf("expected non-nil empty slice, got: %v", res)
		}
	})

	t.Run("database failure returns safe ErrOrganizationServiceUnavailable", func(t *testing.T) {
		svc, _, memR, _ := setupServiceTest()
		memR.overrideErr = errors.New("pq: database down")

		res, err := svc.ListUserOrganizations(ctx, userID)
		if res != nil {
			t.Errorf("expected nil result on error")
		}
		if !errors.Is(err, ErrOrganizationServiceUnavailable) {
			t.Errorf("expected ErrOrganizationServiceUnavailable, got: %v", err)
		}
	})
}

func TestOrganizationService_GetActiveOrganization(t *testing.T) {
	ctx := context.Background()

	t.Run("successfully returns active organization", func(t *testing.T) {
		svc, orgR, _, _ := setupServiceTest()
		orgID := uuid.New()
		org := &domain.Organization{
			ID:                orgID,
			Name:              "Acme Corp",
			Slug:              "acme-corp",
			IsDefaultInternal: false,
			Status:            domain.OrgStatusActive,
			Metadata:          []byte(`{"plan":"enterprise"}`),
			CreatedAt:         time.Now().UTC(),
			UpdatedAt:         time.Now().UTC(),
		}
		orgR.orgs[orgID] = org

		res, err := svc.GetActiveOrganization(ctx, orgID)
		if err != nil {
			t.Fatalf("expected success, got: %v", err)
		}
		if res.ID != orgID || res.Name != "Acme Corp" || res.Status != domain.OrgStatusActive {
			t.Errorf("unexpected retrieved organization: %+v", res)
		}
	})

	t.Run("returns ErrOrgNotFound when organization does not exist or is inactive", func(t *testing.T) {
		svc, orgR, _, _ := setupServiceTest()
		activeOrgID := uuid.New()
		inactiveOrgID := uuid.New()

		orgR.orgs[activeOrgID] = &domain.Organization{
			ID:     activeOrgID,
			Name:   "Active",
			Status: domain.OrgStatusActive,
		}
		orgR.orgs[inactiveOrgID] = &domain.Organization{
			ID:     inactiveOrgID,
			Name:   "Inactive",
			Status: domain.OrgStatusSuspended,
		}

		// Nonexistent
		_, err := svc.GetActiveOrganization(ctx, uuid.New())
		if !errors.Is(err, domain.ErrOrgNotFound) {
			t.Errorf("expected ErrOrgNotFound for missing org, got: %v", err)
		}

		// Inactive
		_, err = svc.GetActiveOrganization(ctx, inactiveOrgID)
		if !errors.Is(err, domain.ErrOrgNotFound) {
			t.Errorf("expected ErrOrgNotFound for suspended org, got: %v", err)
		}
	})

	t.Run("database failure returns safe ErrOrganizationServiceUnavailable", func(t *testing.T) {
		svc, orgR, _, _ := setupServiceTest()
		orgR.overrideErr = errors.New("pq: connection pool exhausted")

		res, err := svc.GetActiveOrganization(ctx, uuid.New())
		if res != nil {
			t.Errorf("expected nil result on error")
		}
		if !errors.Is(err, ErrOrganizationServiceUnavailable) {
			t.Errorf("expected ErrOrganizationServiceUnavailable, got: %v", err)
		}
	})
}

func TestOrganizationService_UpdateOrganization(t *testing.T) {
	ctx := context.Background()
	orgID := uuid.New()
	now := time.Now().UTC()

	t.Run("successfully updates name and metadata of active organization", func(t *testing.T) {
		svc, orgR, _, _ := setupServiceTest()
		orgR.orgs[orgID] = &domain.Organization{
			ID:                orgID,
			Name:              "Acme Corporation",
			Slug:              "acme-corp",
			IsDefaultInternal: false,
			Status:            domain.OrgStatusActive,
			Metadata:          []byte(`{"plan":"standard"}`),
			CreatedAt:         now,
			UpdatedAt:         now,
		}

		input := UpdateOrganizationInput{
			Name:     "Acme Corporation International",
			Metadata: []byte(`{"plan":"enterprise","max_resources":50}`),
		}

		updated, err := svc.UpdateOrganization(ctx, orgID, input)
		if err != nil {
			t.Fatalf("expected success, got error: %v", err)
		}
		if updated.Name != "Acme Corporation International" {
			t.Errorf("expected updated name, got: %s", updated.Name)
		}
		if string(updated.Metadata) != `{"plan":"enterprise","max_resources":50}` {
			t.Errorf("expected updated metadata, got: %s", string(updated.Metadata))
		}
		if updated.Slug != "acme-corp" {
			t.Errorf("expected immutable slug, got: %s", updated.Slug)
		}
		if updated.Status != domain.OrgStatusActive {
			t.Errorf("expected immutable status, got: %s", updated.Status)
		}
		if updated.IsDefaultInternal {
			t.Errorf("expected immutable is_default_internal")
		}
	})

	t.Run("fails validation on invalid name", func(t *testing.T) {
		svc, _, _, _ := setupServiceTest()

		// Empty name
		_, err := svc.UpdateOrganization(ctx, orgID, UpdateOrganizationInput{
			Name:     "",
			Metadata: []byte(`{"plan":"standard"}`),
		})
		if !errors.Is(err, domain.ErrInvalidOrgName) {
			t.Errorf("expected ErrInvalidOrgName for empty name, got: %v", err)
		}

		// Name > 100 runes
		_, err = svc.UpdateOrganization(ctx, orgID, UpdateOrganizationInput{
			Name:     strings.Repeat("ش", 101),
			Metadata: []byte(`{"plan":"standard"}`),
		})
		if !errors.Is(err, domain.ErrInvalidOrgName) {
			t.Errorf("expected ErrInvalidOrgName for name > 100 runes, got: %v", err)
		}
	})

	t.Run("fails validation on invalid metadata", func(t *testing.T) {
		svc, _, _, _ := setupServiceTest()

		invalidMetas := [][]byte{
			nil,
			[]byte(""),
			[]byte("null"),
			[]byte(`[1, 2, 3]`),
			[]byte(`"string"`),
			[]byte(`123`),
			[]byte(`{unclosed`),
		}

		for _, m := range invalidMetas {
			_, err := svc.UpdateOrganization(ctx, orgID, UpdateOrganizationInput{
				Name:     "Valid Name",
				Metadata: m,
			})
			if !errors.Is(err, domain.ErrInvalidMetadata) {
				t.Errorf("expected ErrInvalidMetadata for metadata %q, got: %v", string(m), err)
			}
		}
	})

	t.Run("returns ErrOrgNotFound when organization does not exist or is inactive", func(t *testing.T) {
		svc, orgR, _, _ := setupServiceTest()
		inactiveOrgID := uuid.New()
		orgR.orgs[inactiveOrgID] = &domain.Organization{
			ID:     inactiveOrgID,
			Name:   "Inactive Org",
			Status: domain.OrgStatusSuspended,
		}

		input := UpdateOrganizationInput{
			Name:     "Updated Name",
			Metadata: []byte(`{}`),
		}

		// Missing org
		_, err := svc.UpdateOrganization(ctx, uuid.New(), input)
		if !errors.Is(err, domain.ErrOrgNotFound) {
			t.Errorf("expected ErrOrgNotFound for missing org, got: %v", err)
		}

		// Suspended org
		_, err = svc.UpdateOrganization(ctx, inactiveOrgID, input)
		if !errors.Is(err, domain.ErrOrgNotFound) {
			t.Errorf("expected ErrOrgNotFound for suspended org, got: %v", err)
		}
	})

	t.Run("database failure returns safe ErrOrganizationServiceUnavailable", func(t *testing.T) {
		svc, orgR, _, _ := setupServiceTest()
		orgR.overrideErr = errors.New("pq: database down")

		_, err := svc.UpdateOrganization(ctx, orgID, UpdateOrganizationInput{
			Name:     "Valid Name",
			Metadata: []byte(`{}`),
		})
		if !errors.Is(err, ErrOrganizationServiceUnavailable) {
			t.Errorf("expected ErrOrganizationServiceUnavailable, got: %v", err)
		}
	})
}
