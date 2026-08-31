package bootstrap

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	identityDomain "backup-platform/internal/identity/domain"
	orgDomain "backup-platform/internal/organization/domain"
	orgRepo "backup-platform/internal/organization/repository"
	"backup-platform/internal/platform/database"
	"backup-platform/pkg/uuid"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// FakeQuerier implements database.Querier as a no-op placeholder for memory repos.
type FakeQuerier struct{}

func (f *FakeQuerier) Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}
func (f *FakeQuerier) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return nil, nil
}
func (f *FakeQuerier) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return nil
}

// FakeTxManager implements database.TxManager for unit testing.
type FakeTxManager struct {
	querier       database.Querier
	beginErr      error
	commitErr     error
	BeginCount    int
	CommitCount   int
	RollbackCount int
}

func NewFakeTxManager() *FakeTxManager {
	return &FakeTxManager{
		querier: &FakeQuerier{},
	}
}

func (m *FakeTxManager) Querier() database.Querier {
	return m.querier
}

func (m *FakeTxManager) WithinTx(ctx context.Context, fn func(q database.Querier) error) error {
	m.BeginCount++
	if m.beginErr != nil {
		return m.beginErr
	}

	if err := fn(m.querier); err != nil {
		m.RollbackCount++
		return err
	}

	if m.commitErr != nil {
		m.RollbackCount++
		return m.commitErr
	}

	m.CommitCount++
	return nil
}

// InMemoryIdentityStore holds in-memory domain entities across repositories.
type InMemoryIdentityStore struct {
	users       map[uuid.UUID]*identityDomain.User
	orgs        map[uuid.UUID]*orgDomain.Organization
	members     map[string]*orgDomain.Member // key: orgID:userID
	hasAdminErr error
	createErr   error
}

func NewInMemoryIdentityStore() *InMemoryIdentityStore {
	return &InMemoryIdentityStore{
		users:   make(map[uuid.UUID]*identityDomain.User),
		orgs:    make(map[uuid.UUID]*orgDomain.Organization),
		members: make(map[string]*orgDomain.Member),
	}
}

// User repository implementation
type InMemoryUserRepo struct{ store *InMemoryIdentityStore }

func (r *InMemoryUserRepo) Create(ctx context.Context, q database.Querier, user *identityDomain.User) error {
	if r.store.createErr != nil {
		return r.store.createErr
	}
	r.store.users[user.ID] = user
	return nil
}

func (r *InMemoryUserRepo) FindByID(ctx context.Context, q database.Querier, id uuid.UUID) (*identityDomain.User, error) {
	u, ok := r.store.users[id]
	if !ok {
		return nil, identityDomain.ErrUserNotFound
	}
	return u, nil
}

func (r *InMemoryUserRepo) FindByEmail(ctx context.Context, q database.Querier, email string) (*identityDomain.User, error) {
	for _, u := range r.store.users {
		if strings.EqualFold(u.Email, email) {
			return u, nil
		}
	}
	return nil, identityDomain.ErrUserNotFound
}

func (r *InMemoryUserRepo) HasSystemAdmin(ctx context.Context, q database.Querier) (bool, error) {
	if r.store.hasAdminErr != nil {
		return false, r.store.hasAdminErr
	}
	for _, u := range r.store.users {
		if u.IsSystemAdmin && u.Status == identityDomain.UserStatusActive {
			return true, nil
		}
	}
	return false, nil
}

func (r *InMemoryUserRepo) UpdateStatus(ctx context.Context, q database.Querier, id uuid.UUID, status identityDomain.UserStatus) error {
	u, ok := r.store.users[id]
	if !ok {
		return identityDomain.ErrUserNotFound
	}
	u.Status = status
	return nil
}

// Organization repository implementation
type InMemoryOrgRepo struct{ store *InMemoryIdentityStore }

func (r *InMemoryOrgRepo) Create(ctx context.Context, q database.Querier, org *orgDomain.Organization) error {
	r.store.orgs[org.ID] = org
	return nil
}

func (r *InMemoryOrgRepo) FindByID(ctx context.Context, q database.Querier, id uuid.UUID) (*orgDomain.Organization, error) {
	o, ok := r.store.orgs[id]
	if !ok {
		return nil, orgDomain.ErrOrgNotFound
	}
	return o, nil
}

func (r *InMemoryOrgRepo) FindActiveByID(ctx context.Context, q database.Querier, id uuid.UUID) (*orgDomain.Organization, error) {
	o, ok := r.store.orgs[id]
	if !ok || o.Status != orgDomain.OrgStatusActive {
		return nil, orgDomain.ErrOrgNotFound
	}
	return o, nil
}

func (r *InMemoryOrgRepo) FindBySlug(ctx context.Context, q database.Querier, slug string) (*orgDomain.Organization, error) {
	for _, o := range r.store.orgs {
		if strings.EqualFold(o.Slug, slug) {
			return o, nil
		}
	}
	return nil, orgDomain.ErrOrgNotFound
}

func (r *InMemoryOrgRepo) FindDefaultInternal(ctx context.Context, q database.Querier) (*orgDomain.Organization, error) {
	for _, o := range r.store.orgs {
		if o.IsDefaultInternal {
			return o, nil
		}
	}
	return nil, orgDomain.ErrOrgNotFound
}

func (r *InMemoryOrgRepo) UpdateActive(ctx context.Context, q database.Querier, id uuid.UUID, name string, metadata []byte) (*orgDomain.Organization, error) {
	o, ok := r.store.orgs[id]
	if !ok || o.Status != orgDomain.OrgStatusActive {
		return nil, orgDomain.ErrOrgNotFound
	}
	if len(metadata) == 0 {
		metadata = []byte("{}")
	}
	o.Name = name
	o.Metadata = metadata
	o.UpdatedAt = time.Now().UTC()
	return o, nil
}

// Member repository implementation
type InMemoryMemberRepo struct{ store *InMemoryIdentityStore }

func (r *InMemoryMemberRepo) Create(ctx context.Context, q database.Querier, member *orgDomain.Member) error {
	key := member.OrganizationID.String() + ":" + member.UserID.String()
	r.store.members[key] = member
	return nil
}

func (r *InMemoryMemberRepo) FindMembership(ctx context.Context, q database.Querier, orgID, userID uuid.UUID) (*orgDomain.Member, error) {
	key := orgID.String() + ":" + userID.String()
	m, ok := r.store.members[key]
	if !ok {
		return nil, orgDomain.ErrMemberNotFound
	}
	return m, nil
}

func (r *InMemoryMemberRepo) ListUserOrganizations(ctx context.Context, q database.Querier, userID uuid.UUID) ([]*orgDomain.Organization, error) {
	var res []*orgDomain.Organization
	for _, m := range r.store.members {
		if m.UserID == userID && m.Status == orgDomain.MemberStatusActive {
			if o, ok := r.store.orgs[m.OrganizationID]; ok {
				res = append(res, o)
			}
		}
	}
	return res, nil
}

func (r *InMemoryMemberRepo) ListUserMembershipsWithOrg(ctx context.Context, q database.Querier, userID uuid.UUID) ([]*orgRepo.UserMembershipWithOrg, error) {
	var res []*orgRepo.UserMembershipWithOrg
	for _, m := range r.store.members {
		if m.UserID == userID && m.Status == orgDomain.MemberStatusActive {
			if o, ok := r.store.orgs[m.OrganizationID]; ok {
				res = append(res, &orgRepo.UserMembershipWithOrg{
					OrganizationID:     o.ID,
					OrganizationName:   o.Name,
					Slug:               o.Slug,
					IsDefaultInternal:  o.IsDefaultInternal,
					OrganizationStatus: o.Status,
					Role:               m.Role,
					Status:             m.Status,
					CreatedAt:          o.CreatedAt,
				})
			}
		}
	}
	return res, nil
}

func (r *InMemoryMemberRepo) FindActiveMembershipWithOrg(ctx context.Context, q database.Querier, orgID, userID uuid.UUID) (*orgRepo.UserMembershipWithOrg, error) {
	key := orgID.String() + ":" + userID.String()
	m, ok := r.store.members[key]
	if !ok || m.Status != orgDomain.MemberStatusActive {
		return nil, nil
	}
	o, ok := r.store.orgs[orgID]
	if !ok || o.Status != orgDomain.OrgStatusActive {
		return nil, nil
	}
	return &orgRepo.UserMembershipWithOrg{
		OrganizationID:     o.ID,
		OrganizationName:   o.Name,
		Slug:               o.Slug,
		IsDefaultInternal:  o.IsDefaultInternal,
		OrganizationStatus: o.Status,
		Role:               m.Role,
		Status:             m.Status,
		CreatedAt:          o.CreatedAt,
	}, nil
}

// TestPasswordHasher tracks hashing calls
type TestPasswordHasher struct {
	HashCount int
}

func (h *TestPasswordHasher) Hash(password string) (string, error) {
	h.HashCount++
	return "$argon2id$v=19$m=65536,t=3,p=4$fake_salt$fake_hash", nil
}

func (h *TestPasswordHasher) Verify(password, hash string) (bool, error) {
	return true, nil
}

func TestBootstrap_DirectServiceRun_FirstRun(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	store := NewInMemoryIdentityStore()
	userRepo := &InMemoryUserRepo{store: store}
	orgRepo := &InMemoryOrgRepo{store: store}
	memberRepo := &InMemoryMemberRepo{store: store}
	txManager := NewFakeTxManager()
	hasher := &TestPasswordHasher{}

	rawPassword := "SuperSecretAdminPassword2026!"
	svc := NewService(
		Config{
			AdminEmail:    "admin@example.com",
			AdminPassword: rawPassword,
		},
		txManager,
		userRepo,
		orgRepo,
		memberRepo,
		hasher,
		logger,
	)

	// Execute Service.Run directly
	err := svc.Run(context.Background())
	if err != nil {
		t.Fatalf("expected Service.Run to succeed on first run, got error: %v", err)
	}

	// 1. Transaction assertions
	if txManager.BeginCount != 1 || txManager.CommitCount != 1 || txManager.RollbackCount != 0 {
		t.Errorf("expected 1 begin, 1 commit, 0 rollbacks; got: begin=%d, commit=%d, rollback=%d",
			txManager.BeginCount, txManager.CommitCount, txManager.RollbackCount)
	}

	// 2. Secret memory lifetime assertion
	if svc.cfg.AdminPassword != "" {
		t.Errorf("expected AdminPassword to be wiped from service struct state after Run()")
	}

	// 3. User entity assertions
	if len(store.users) != 1 {
		t.Fatalf("expected exactly 1 user created, got: %d", len(store.users))
	}
	savedUser, err := userRepo.FindByEmail(context.Background(), nil, "admin@example.com")
	if err != nil {
		t.Fatalf("failed to find created user: %v", err)
	}
	if !savedUser.IsSystemAdmin {
		t.Errorf("expected user.IsSystemAdmin = true")
	}
	if savedUser.Status != identityDomain.UserStatusActive {
		t.Errorf("expected user.Status = active, got: %s", savedUser.Status)
	}
	if savedUser.Email != "admin@example.com" {
		t.Errorf("expected user.Email = admin@example.com, got: %s", savedUser.Email)
	}
	if strings.Contains(savedUser.PasswordHash, rawPassword) {
		t.Errorf("CRITICAL SECURITY FLAW: raw password found in password hash string!")
	}

	// 4. Organization entity assertions
	if len(store.orgs) != 1 {
		t.Fatalf("expected exactly 1 organization created, got: %d", len(store.orgs))
	}
	savedOrg, err := orgRepo.FindDefaultInternal(context.Background(), nil)
	if err != nil {
		t.Fatalf("failed to find created default internal org: %v", err)
	}
	if !savedOrg.IsDefaultInternal {
		t.Errorf("expected org.IsDefaultInternal = true")
	}
	if savedOrg.Slug != "internal" {
		t.Errorf("expected org.Slug = internal, got: %s", savedOrg.Slug)
	}
	if savedOrg.Status != orgDomain.OrgStatusActive {
		t.Errorf("expected org.Status = active, got: %s", savedOrg.Status)
	}

	// 5. Membership entity assertions
	if len(store.members) != 1 {
		t.Fatalf("expected exactly 1 membership created, got: %d", len(store.members))
	}
	savedMember, err := memberRepo.FindMembership(context.Background(), nil, savedOrg.ID, savedUser.ID)
	if err != nil {
		t.Fatalf("failed to find created membership: %v", err)
	}
	if savedMember.Role != orgDomain.RoleAdmin {
		t.Errorf("expected member.Role = admin, got: %s", savedMember.Role)
	}
	if savedMember.Status != orgDomain.MemberStatusActive {
		t.Errorf("expected member.Status = active, got: %s", savedMember.Status)
	}
}

func TestBootstrap_DirectServiceRun_Idempotency(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	store := NewInMemoryIdentityStore()
	userRepo := &InMemoryUserRepo{store: store}
	orgRepo := &InMemoryOrgRepo{store: store}
	memberRepo := &InMemoryMemberRepo{store: store}
	txManager := NewFakeTxManager()
	hasher := &TestPasswordHasher{}

	// First run
	svc1 := NewService(
		Config{
			AdminEmail:    "admin@example.com",
			AdminPassword: "SecretPassword123!",
		},
		txManager,
		userRepo,
		orgRepo,
		memberRepo,
		hasher,
		logger,
	)
	if err := svc1.Run(context.Background()); err != nil {
		t.Fatalf("first run failed: %v", err)
	}

	userCountAfterFirst := len(store.users)
	orgCountAfterFirst := len(store.orgs)
	memberCountAfterFirst := len(store.members)
	hasherCountAfterFirst := hasher.HashCount

	if hasherCountAfterFirst != 1 {
		t.Fatalf("expected hasher to be called exactly once during first run, got: %d", hasherCountAfterFirst)
	}

	// Second run with new service instance on same store
	svc2 := NewService(
		Config{
			AdminEmail:    "admin@example.com",
			AdminPassword: "SecretPassword123!",
		},
		txManager,
		userRepo,
		orgRepo,
		memberRepo,
		hasher,
		logger,
	)
	if err := svc2.Run(context.Background()); err != nil {
		t.Fatalf("second run failed: %v", err)
	}

	// Assertions: No new entities created, hasher was NOT called during second run
	if hasher.HashCount != hasherCountAfterFirst {
		t.Errorf("idempotency violation: hasher was called during second run (before: %d, after: %d)",
			hasherCountAfterFirst, hasher.HashCount)
	}
	if len(store.users) != userCountAfterFirst {
		t.Errorf("idempotency failed: user count changed from %d to %d", userCountAfterFirst, len(store.users))
	}
	if len(store.orgs) != orgCountAfterFirst {
		t.Errorf("idempotency failed: org count changed from %d to %d", orgCountAfterFirst, len(store.orgs))
	}
	if len(store.members) != memberCountAfterFirst {
		t.Errorf("idempotency failed: member count changed from %d to %d", memberCountAfterFirst, len(store.members))
	}
	if userCountAfterFirst != 1 || orgCountAfterFirst != 1 || memberCountAfterFirst != 1 {
		t.Errorf("expected exactly 1 of each entity after idempotent run")
	}
}

func TestBootstrap_DirectServiceRun_MissingCredentials(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	store := NewInMemoryIdentityStore()
	userRepo := &InMemoryUserRepo{store: store}
	orgRepo := &InMemoryOrgRepo{store: store}
	memberRepo := &InMemoryMemberRepo{store: store}
	txManager := NewFakeTxManager()
	hasher := &TestPasswordHasher{}

	svc := NewService(
		Config{
			AdminEmail:    "",
			AdminPassword: "",
		},
		txManager,
		userRepo,
		orgRepo,
		memberRepo,
		hasher,
		logger,
	)

	err := svc.Run(context.Background())
	if err != nil {
		t.Fatalf("expected nil error when credentials missing, got: %v", err)
	}

	if txManager.BeginCount != 0 {
		t.Errorf("expected 0 transactions started when credentials missing, got: %d", txManager.BeginCount)
	}
	if hasher.HashCount != 0 {
		t.Errorf("expected hasher not invoked when credentials missing, got: %d", hasher.HashCount)
	}
	if len(store.users) != 0 || len(store.orgs) != 0 || len(store.members) != 0 {
		t.Errorf("expected 0 entities created when credentials missing")
	}
}

func TestBootstrap_DirectServiceRun_SafeErrorsOnDatabaseFailure(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	store := NewInMemoryIdentityStore()
	store.createErr = errors.New("pq: connection reset by peer (raw database sensitive detail)")

	userRepo := &InMemoryUserRepo{store: store}
	orgRepo := &InMemoryOrgRepo{store: store}
	memberRepo := &InMemoryMemberRepo{store: store}
	txManager := NewFakeTxManager()
	hasher := &TestPasswordHasher{}

	adminEmail := "admin@example.com"
	rawPassword := "SecretPassword123!"

	svc := NewService(
		Config{
			AdminEmail:    adminEmail,
			AdminPassword: rawPassword,
		},
		txManager,
		userRepo,
		orgRepo,
		memberRepo,
		hasher,
		logger,
	)

	err := svc.Run(context.Background())
	if err == nil {
		t.Fatal("expected error on database failure, got nil")
	}

	// 1. Error sentinel assertion
	if !errors.Is(err, ErrBootstrapFailed) {
		t.Errorf("expected ErrBootstrapFailed, got: %v", err)
	}

	// 2. Safe error string assertions: no raw DB details, no password, no email leaked
	errStr := err.Error()
	if strings.Contains(errStr, "pq:") {
		t.Errorf("raw PostgreSQL error leaked into returned error: %s", errStr)
	}
	if strings.Contains(errStr, "connection reset") {
		t.Errorf("internal connection details leaked into returned error: %s", errStr)
	}
	if strings.Contains(errStr, rawPassword) {
		t.Errorf("password leaked into returned error: %s", errStr)
	}
	if strings.Contains(errStr, adminEmail) {
		t.Errorf("email leaked into returned error: %s", errStr)
	}

	// 3. Rollback assertion
	if txManager.RollbackCount != 1 {
		t.Errorf("expected transaction to be rolled back on failure, rollback count: %d", txManager.RollbackCount)
	}
}
