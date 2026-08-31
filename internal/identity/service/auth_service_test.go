package service

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"

	identityDomain "backup-platform/internal/identity/domain"
	orgDomain "backup-platform/internal/organization/domain"
	orgRepo "backup-platform/internal/organization/repository"
	"backup-platform/internal/platform/database"
	"backup-platform/pkg/uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// In-Memory Test Fixtures for Auth Core Hardening

type MockQuerier struct{}

func (m *MockQuerier) Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}
func (m *MockQuerier) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return nil, nil
}
func (m *MockQuerier) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return nil
}

type MockTxManager struct {
	querier       database.Querier
	sessionRepo   *InMemorySessionRepo
	beginErr      error
	commitErr     error
	BeginCount    int
	CommitCount   int
	RollbackCount int
}

func NewMockTxManager(sessionRepo *InMemorySessionRepo) *MockTxManager {
	return &MockTxManager{
		querier:     &MockQuerier{},
		sessionRepo: sessionRepo,
	}
}
func (m *MockTxManager) Querier() database.Querier { return m.querier }
func (m *MockTxManager) WithinTx(ctx context.Context, fn func(q database.Querier) error) error {
	m.BeginCount++
	if m.beginErr != nil {
		return m.beginErr
	}

	var snapshot map[uuid.UUID]*identityDomain.Session
	if m.sessionRepo != nil {
		snapshot = m.sessionRepo.cloneSessions()
	}

	if err := fn(m.querier); err != nil {
		m.RollbackCount++
		if m.sessionRepo != nil {
			m.sessionRepo.restoreSessions(snapshot)
		}
		return err
	}

	if m.commitErr != nil {
		m.RollbackCount++
		if m.sessionRepo != nil {
			m.sessionRepo.restoreSessions(snapshot)
		}
		return m.commitErr
	}

	m.CommitCount++
	return nil
}

type InMemoryUserRepo struct {
	users       map[uuid.UUID]*identityDomain.User
	overrideErr error
}

func NewInMemoryUserRepo() *InMemoryUserRepo {
	return &InMemoryUserRepo{users: make(map[uuid.UUID]*identityDomain.User)}
}

func (r *InMemoryUserRepo) Create(ctx context.Context, q database.Querier, user *identityDomain.User) error {
	if r.overrideErr != nil {
		return r.overrideErr
	}
	r.users[user.ID] = user
	return nil
}

func (r *InMemoryUserRepo) FindByID(ctx context.Context, q database.Querier, id uuid.UUID) (*identityDomain.User, error) {
	if r.overrideErr != nil {
		return nil, r.overrideErr
	}
	u, ok := r.users[id]
	if !ok {
		return nil, identityDomain.ErrUserNotFound
	}
	return u, nil
}

func (r *InMemoryUserRepo) FindByEmail(ctx context.Context, q database.Querier, email string) (*identityDomain.User, error) {
	if r.overrideErr != nil {
		return nil, r.overrideErr
	}
	for _, u := range r.users {
		if strings.EqualFold(u.Email, email) {
			return u, nil
		}
	}
	return nil, identityDomain.ErrUserNotFound
}

func (r *InMemoryUserRepo) HasSystemAdmin(ctx context.Context, q database.Querier) (bool, error) {
	if r.overrideErr != nil {
		return false, r.overrideErr
	}
	for _, u := range r.users {
		if u.IsSystemAdmin && u.Status == identityDomain.UserStatusActive {
			return true, nil
		}
	}
	return false, nil
}

func (r *InMemoryUserRepo) UpdateStatus(ctx context.Context, q database.Querier, id uuid.UUID, status identityDomain.UserStatus) error {
	if r.overrideErr != nil {
		return r.overrideErr
	}
	u, ok := r.users[id]
	if !ok {
		return identityDomain.ErrUserNotFound
	}
	u.Status = status
	return nil
}

type InMemorySessionRepo struct {
	sessions    map[uuid.UUID]*identityDomain.Session
	overrideErr error
}

func NewInMemorySessionRepo() *InMemorySessionRepo {
	return &InMemorySessionRepo{sessions: make(map[uuid.UUID]*identityDomain.Session)}
}

func (r *InMemorySessionRepo) cloneSessions() map[uuid.UUID]*identityDomain.Session {
	cp := make(map[uuid.UUID]*identityDomain.Session, len(r.sessions))
	for k, v := range r.sessions {
		sessionCopy := *v
		cp[k] = &sessionCopy
	}
	return cp
}

func (r *InMemorySessionRepo) restoreSessions(snapshot map[uuid.UUID]*identityDomain.Session) {
	r.sessions = snapshot
}

func (r *InMemorySessionRepo) Create(ctx context.Context, q database.Querier, s *identityDomain.Session) error {
	if r.overrideErr != nil {
		return r.overrideErr
	}
	r.sessions[s.ID] = s
	return nil
}

func (r *InMemorySessionRepo) FindByID(ctx context.Context, q database.Querier, id uuid.UUID) (*identityDomain.Session, error) {
	if r.overrideErr != nil {
		return nil, r.overrideErr
	}
	s, ok := r.sessions[id]
	if !ok {
		return nil, identityDomain.ErrSessionNotFound
	}
	return s, nil
}

func (r *InMemorySessionRepo) FindByRefreshTokenHash(ctx context.Context, q database.Querier, hash string) (*identityDomain.Session, error) {
	if r.overrideErr != nil {
		return nil, r.overrideErr
	}
	for _, s := range r.sessions {
		if s.RefreshTokenHash == hash {
			return s, nil
		}
	}
	return nil, identityDomain.ErrSessionNotFound
}

func (r *InMemorySessionRepo) RotateRefreshToken(ctx context.Context, q database.Querier, sessionID uuid.UUID, oldHash, newHash string, now time.Time) error {
	if r.overrideErr != nil {
		return r.overrideErr
	}
	s, ok := r.sessions[sessionID]
	if !ok {
		return identityDomain.ErrSessionNotFound
	}
	if s.RefreshTokenHash != oldHash || s.IsRevoked() || s.IsExpired(now) {
		return identityDomain.ErrInvalidSession
	}

	s.RefreshTokenHash = newHash
	s.LastUsedAt = now
	return nil
}

func (r *InMemorySessionRepo) RevokeByID(ctx context.Context, q database.Querier, id uuid.UUID, now time.Time) error {
	if r.overrideErr != nil {
		return r.overrideErr
	}
	s, ok := r.sessions[id]
	if !ok {
		return identityDomain.ErrSessionNotFound
	}
	s.Revoke(now)
	return nil
}

func (r *InMemorySessionRepo) RevokeAllForUser(ctx context.Context, q database.Querier, userID uuid.UUID, now time.Time) error {
	if r.overrideErr != nil {
		return r.overrideErr
	}
	for _, s := range r.sessions {
		if s.UserID == userID && !s.IsRevoked() {
			s.Revoke(now)
		}
	}
	return nil
}

type InMemoryMemberRepo struct {
	userOrgs    map[uuid.UUID][]*orgDomain.Organization
	overrideErr error
}

func NewInMemoryMemberRepo() *InMemoryMemberRepo {
	return &InMemoryMemberRepo{userOrgs: make(map[uuid.UUID][]*orgDomain.Organization)}
}

func (r *InMemoryMemberRepo) Create(ctx context.Context, q database.Querier, member *orgDomain.Member) error {
	return nil
}
func (r *InMemoryMemberRepo) FindMembership(ctx context.Context, q database.Querier, orgID, userID uuid.UUID) (*orgDomain.Member, error) {
	return nil, nil
}
func (r *InMemoryMemberRepo) ListUserOrganizations(ctx context.Context, q database.Querier, userID uuid.UUID) ([]*orgDomain.Organization, error) {
	if r.overrideErr != nil {
		return nil, r.overrideErr
	}
	return r.userOrgs[userID], nil
}
func (r *InMemoryMemberRepo) ListUserMembershipsWithOrg(ctx context.Context, q database.Querier, userID uuid.UUID) ([]*orgRepo.UserMembershipWithOrg, error) {
	if r.overrideErr != nil {
		return nil, r.overrideErr
	}
	var list []*orgRepo.UserMembershipWithOrg
	for _, o := range r.userOrgs[userID] {
		list = append(list, &orgRepo.UserMembershipWithOrg{
			OrganizationID:     o.ID,
			OrganizationName:   o.Name,
			Slug:               o.Slug,
			IsDefaultInternal:  o.IsDefaultInternal,
			OrganizationStatus: o.Status,
			Role:               orgDomain.RoleAdmin,
			Status:             orgDomain.MemberStatusActive,
			CreatedAt:          o.CreatedAt,
		})
	}
	return list, nil
}
func (r *InMemoryMemberRepo) FindActiveMembershipWithOrg(ctx context.Context, q database.Querier, orgID, userID uuid.UUID) (*orgRepo.UserMembershipWithOrg, error) {
	if r.overrideErr != nil {
		return nil, r.overrideErr
	}
	for _, o := range r.userOrgs[userID] {
		if o.ID == orgID && o.Status == orgDomain.OrgStatusActive {
			return &orgRepo.UserMembershipWithOrg{
				OrganizationID:     o.ID,
				OrganizationName:   o.Name,
				Slug:               o.Slug,
				IsDefaultInternal:  o.IsDefaultInternal,
				OrganizationStatus: o.Status,
				Role:               orgDomain.RoleAdmin,
				Status:             orgDomain.MemberStatusActive,
				CreatedAt:          o.CreatedAt,
			}, nil
		}
	}
	return nil, nil
}

// CountingPasswordHasher records verify invocations, hash values, and simulated failures
type CountingPasswordHasher struct {
	VerifyCalls int
	LastHash    string
	verifyErr   error
}

func (h *CountingPasswordHasher) Hash(password string) (string, error) {
	return "$argon2id$v=19$m=65536,t=3,p=4$fake_salt$fake_hash", nil
}

func (h *CountingPasswordHasher) Verify(password, hash string) (bool, error) {
	h.VerifyCalls++
	h.LastHash = hash
	if h.verifyErr != nil {
		return false, h.verifyErr
	}
	return hash != dummyArgon2idHash && !strings.Contains(password, "Wrong"), nil
}

type DynamicJWTService struct {
	realService TokenService
	failErr     error
}

func (d *DynamicJWTService) GenerateAccessToken(userID, sessionID uuid.UUID, isSystemAdmin bool) (string, time.Time, error) {
	if d.failErr != nil {
		return "", time.Time{}, d.failErr
	}
	return d.realService.GenerateAccessToken(userID, sessionID, isSystemAdmin)
}
func (d *DynamicJWTService) ValidateAccessToken(tokenStr string) (*TokenPayload, error) {
	if d.failErr != nil {
		return nil, d.failErr
	}
	return d.realService.ValidateAccessToken(tokenStr)
}

func setupTestAuthService(t *testing.T) (*AuthService, *InMemoryUserRepo, *InMemorySessionRepo, *InMemoryMemberRepo, *MockTxManager, *DynamicJWTService) {
	userRepo := NewInMemoryUserRepo()
	sessionRepo := NewInMemorySessionRepo()
	memberRepo := NewInMemoryMemberRepo()
	hasher := NewDefaultArgon2idHasher()
	tokenGen := NewSecureTokenGenerator()
	jwtKey := []byte("test-secret-key-at-least-32-bytes-long!")
	realJWTService, err := NewJWTService(jwtKey)
	if err != nil {
		t.Fatalf("failed to init JWT service: %v", err)
	}
	dynamicJWT := &DynamicJWTService{realService: realJWTService}
	txManager := NewMockTxManager(sessionRepo)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	authSvc := NewAuthService(
		userRepo,
		sessionRepo,
		memberRepo,
		hasher,
		tokenGen,
		dynamicJWT,
		txManager,
		logger,
	)

	return authSvc, userRepo, sessionRepo, memberRepo, txManager, dynamicJWT
}

func TestAuthService_LoginSuccess_SafeUserResult(t *testing.T) {
	authSvc, userRepo, sessionRepo, memberRepo, _, _ := setupTestAuthService(t)
	hasher := NewDefaultArgon2idHasher()

	password := "SecretPassword123!"
	pwdHash, _ := hasher.Hash(password)
	user, err := identityDomain.NewUser("admin@example.com", pwdHash, "Admin User", true)
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}
	_ = userRepo.Create(context.Background(), nil, user)

	internalOrg, _ := orgDomain.NewOrganization("Internal Organization", "internal", true)
	memberRepo.userOrgs[user.ID] = []*orgDomain.Organization{internalOrg}

	ip := "10.0.0.1"
	ua := "TestClient/1.0"

	res, err := authSvc.Login(context.Background(), "ADMIN@example.com", password, ClientMetadata{
		IPAddress: &ip,
		UserAgent: &ua,
	})
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}

	// 1. Assert AuthenticatedUser does not contain or expose PasswordHash
	val := reflect.ValueOf(res.User)
	typ := val.Type()
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if strings.Contains(strings.ToLower(field.Name), "password") {
			t.Errorf("CRITICAL SECURITY FLAW: AuthenticatedUser exposes field '%s'", field.Name)
		}
	}

	if res.User.ID != user.ID {
		t.Errorf("expected user ID match")
	}
	if res.User.Email != "admin@example.com" {
		t.Errorf("expected user Email match")
	}
	if res.User.FullName != "Admin User" {
		t.Errorf("expected user FullName match")
	}
	if !res.User.IsSystemAdmin {
		t.Errorf("expected is_system_admin = true")
	}
	if res.User.Status != identityDomain.UserStatusActive {
		t.Errorf("expected active status")
	}

	// 2. Assert RawRefreshToken is NOT in database
	savedSession, err := sessionRepo.FindByID(context.Background(), nil, res.SessionID)
	if err != nil {
		t.Fatalf("failed to find created session: %v", err)
	}
	if savedSession.RefreshTokenHash == res.RawRefreshToken {
		t.Errorf("raw refresh token was persisted directly in database")
	}
	if len(savedSession.RefreshTokenHash) != 64 {
		t.Errorf("expected 64 hex characters for SHA-256 hash")
	}
}

func TestAuthService_LoginTimingEqualizationPath(t *testing.T) {
	userRepo := NewInMemoryUserRepo()
	sessionRepo := NewInMemorySessionRepo()
	memberRepo := NewInMemoryMemberRepo()
	countingHasher := &CountingPasswordHasher{}
	tokenGen := NewSecureTokenGenerator()
	jwtService, _ := NewJWTService([]byte("test-secret-key-at-least-32-bytes-long!"))
	txManager := NewMockTxManager(sessionRepo)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	authSvc := NewAuthService(
		userRepo,
		sessionRepo,
		memberRepo,
		countingHasher,
		tokenGen,
		jwtService,
		txManager,
		logger,
	)

	// Setup users
	userHash := "$argon2id$v=19$m=65536,t=3,p=4$user_salt$user_hash"
	activeUser, _ := identityDomain.NewUser("active@example.com", userHash, "Active", false)
	inactiveUser, _ := identityDomain.NewUser("inactive@example.com", userHash, "Inactive", false)
	inactiveUser.Status = identityDomain.UserStatusInactive
	blockedUser, _ := identityDomain.NewUser("blocked@example.com", userHash, "Blocked", false)
	blockedUser.Status = identityDomain.UserStatusBlocked

	_ = userRepo.Create(context.Background(), nil, activeUser)
	_ = userRepo.Create(context.Background(), nil, inactiveUser)
	_ = userRepo.Create(context.Background(), nil, blockedUser)

	t.Run("unknown email calls dummy verify", func(t *testing.T) {
		countingHasher.VerifyCalls = 0
		_, err := authSvc.Login(context.Background(), "unknown@example.com", "Password123!", ClientMetadata{})
		if !errors.Is(err, identityDomain.ErrInvalidCredentials) {
			t.Errorf("expected ErrInvalidCredentials, got: %v", err)
		}
		if countingHasher.VerifyCalls != 1 {
			t.Errorf("expected 1 verify call on unknown email, got: %d", countingHasher.VerifyCalls)
		}
		if countingHasher.LastHash != dummyArgon2idHash {
			t.Errorf("expected dummy hash used during timing equalization")
		}
	})

	t.Run("active user wrong password calls real verify", func(t *testing.T) {
		countingHasher.VerifyCalls = 0
		_, err := authSvc.Login(context.Background(), "active@example.com", "WrongPassword123!", ClientMetadata{})
		if !errors.Is(err, identityDomain.ErrInvalidCredentials) {
			t.Errorf("expected ErrInvalidCredentials, got: %v", err)
		}
		if countingHasher.VerifyCalls != 1 {
			t.Errorf("expected 1 verify call on wrong password, got: %d", countingHasher.VerifyCalls)
		}
		if countingHasher.LastHash != userHash {
			t.Errorf("expected user hash used for active user")
		}
	})

	t.Run("inactive user calls real verify", func(t *testing.T) {
		countingHasher.VerifyCalls = 0
		_, err := authSvc.Login(context.Background(), "inactive@example.com", "Password123!", ClientMetadata{})
		if !errors.Is(err, identityDomain.ErrInvalidCredentials) {
			t.Errorf("expected ErrInvalidCredentials, got: %v", err)
		}
		if countingHasher.VerifyCalls != 1 {
			t.Errorf("expected 1 verify call for inactive user, got: %d", countingHasher.VerifyCalls)
		}
		if countingHasher.LastHash != userHash {
			t.Errorf("expected user hash used for inactive user")
		}
	})

	t.Run("blocked user calls real verify", func(t *testing.T) {
		countingHasher.VerifyCalls = 0
		_, err := authSvc.Login(context.Background(), "blocked@example.com", "Password123!", ClientMetadata{})
		if !errors.Is(err, identityDomain.ErrInvalidCredentials) {
			t.Errorf("expected ErrInvalidCredentials, got: %v", err)
		}
		if countingHasher.VerifyCalls != 1 {
			t.Errorf("expected 1 verify call for blocked user, got: %d", countingHasher.VerifyCalls)
		}
		if countingHasher.LastHash != userHash {
			t.Errorf("expected user hash used for blocked user")
		}
	})
}

func TestAuthService_LoginCorruptStoredHash(t *testing.T) {
	userRepo := NewInMemoryUserRepo()
	sessionRepo := NewInMemorySessionRepo()
	memberRepo := NewInMemoryMemberRepo()
	countingHasher := &CountingPasswordHasher{verifyErr: errors.New("malformed hash data")}
	tokenGen := NewSecureTokenGenerator()
	jwtService, _ := NewJWTService([]byte("test-secret-key-at-least-32-bytes-long!"))
	txManager := NewMockTxManager(sessionRepo)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	authSvc := NewAuthService(
		userRepo,
		sessionRepo,
		memberRepo,
		countingHasher,
		tokenGen,
		jwtService,
		txManager,
		logger,
	)

	user, _ := identityDomain.NewUser("active@example.com", "$corrupt$hash", "User", false)
	_ = userRepo.Create(context.Background(), nil, user)

	_, err := authSvc.Login(context.Background(), "active@example.com", "Password123!", ClientMetadata{})
	if err == nil {
		t.Fatal("expected error on corrupt hash verification, got nil")
	}

	// 1. Must be ErrAuthServiceUnavailable
	if !errors.Is(err, identityDomain.ErrAuthServiceUnavailable) {
		t.Errorf("expected ErrAuthServiceUnavailable on corrupt hash, got: %v", err)
	}

	// 2. Must NOT be ErrInvalidCredentials
	if errors.Is(err, identityDomain.ErrInvalidCredentials) {
		t.Errorf("corrupt hash must NOT be disguised as ErrInvalidCredentials")
	}

	// 3. Must not leak internal parser details
	if strings.Contains(err.Error(), "malformed") || strings.Contains(err.Error(), "$corrupt") {
		t.Errorf("internal hash parser details leaked in error: %s", err.Error())
	}
}

func TestAuthService_LoginTransactionRollback_NoOrphanSession(t *testing.T) {
	authSvc, userRepo, sessionRepo, _, txManager, dynamicJWT := setupTestAuthService(t)
	hasher := NewDefaultArgon2idHasher()

	password := "SecretPassword123!"
	pwdHash, _ := hasher.Hash(password)
	user, _ := identityDomain.NewUser("admin@example.com", pwdHash, "Admin", true)
	_ = userRepo.Create(context.Background(), nil, user)

	// Induce JWT generation failure
	dynamicJWT.failErr = ErrTokenGeneration

	_, err := authSvc.Login(context.Background(), "admin@example.com", password, ClientMetadata{})
	if err == nil {
		t.Fatal("expected login failure due to JWT error, got nil")
	}
	if !errors.Is(err, identityDomain.ErrAuthServiceUnavailable) {
		t.Errorf("expected ErrAuthServiceUnavailable, got: %v", err)
	}

	// 1. Transaction rollback assertion
	if txManager.RollbackCount != 1 {
		t.Errorf("expected transaction to be rolled back, got rollback count: %d", txManager.RollbackCount)
	}

	// 2. Real session cleanup assertion (no orphan session in store!)
	if len(sessionRepo.sessions) != 0 {
		t.Errorf("ORPHAN SESSION DETECTED: expected 0 final persisted sessions, got: %d", len(sessionRepo.sessions))
	}
}

func TestAuthService_RefreshTransactionRollback_PreservesOldToken(t *testing.T) {
	authSvc, userRepo, sessionRepo, _, txManager, dynamicJWT := setupTestAuthService(t)
	hasher := NewDefaultArgon2idHasher()

	password := "SecretPassword123!"
	pwdHash, _ := hasher.Hash(password)
	user, _ := identityDomain.NewUser("user@example.com", pwdHash, "User", false)
	_ = userRepo.Create(context.Background(), nil, user)

	// Step 1: Login successfully
	loginRes, err := authSvc.Login(context.Background(), "user@example.com", password, ClientMetadata{})
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}

	oldRefreshToken := loginRes.RawRefreshToken
	sessionID := loginRes.SessionID
	sessionBeforeRefresh := sessionRepo.sessions[sessionID]
	originalHash := sessionBeforeRefresh.RefreshTokenHash

	// Step 2: Inject failure during JWT signing inside Refresh
	dynamicJWT.failErr = ErrTokenGeneration

	_, err = authSvc.Refresh(context.Background(), oldRefreshToken)
	if err == nil {
		t.Fatal("expected refresh failure due to JWT failure, got nil")
	}
	if !errors.Is(err, identityDomain.ErrAuthServiceUnavailable) {
		t.Errorf("expected ErrAuthServiceUnavailable, got: %v", err)
	}

	// 1. Rollback assertion
	if txManager.RollbackCount != 1 {
		t.Errorf("expected rollback count = 1, got: %d", txManager.RollbackCount)
	}

	// 2. Hash must be restored to original hash
	if sessionRepo.sessions[sessionID].RefreshTokenHash != originalHash {
		t.Errorf("hash was corrupted/partially committed after failed refresh")
	}

	// Step 3: Restore healthy JWT service and retry with same old token
	dynamicJWT.failErr = nil

	refreshRes, err := authSvc.Refresh(context.Background(), oldRefreshToken)
	if err != nil {
		t.Fatalf("expected retry with old refresh token to succeed after rollback, got: %v", err)
	}
	if refreshRes.SessionID != sessionID {
		t.Errorf("session ID mismatch")
	}
	if refreshRes.RawRefreshToken == oldRefreshToken {
		t.Errorf("new refresh token must differ from old refresh token")
	}
}

func TestAuthService_ErrorClassification_DatabaseFailures(t *testing.T) {
	authSvc, userRepo, sessionRepo, memberRepo, _, _ := setupTestAuthService(t)
	rawDBErr := errors.New("pq: connection reset by peer (sensitive database internal details)")

	t.Run("db failure during login user query", func(t *testing.T) {
		userRepo.overrideErr = rawDBErr
		_, err := authSvc.Login(context.Background(), "user@example.com", "Password123!", ClientMetadata{})
		if !errors.Is(err, identityDomain.ErrAuthServiceUnavailable) {
			t.Errorf("expected ErrAuthServiceUnavailable on DB error, got: %v", err)
		}
		if errors.Is(err, identityDomain.ErrInvalidCredentials) {
			t.Errorf("DB failure must NOT be disguised as ErrInvalidCredentials")
		}
		if strings.Contains(err.Error(), "pq:") || strings.Contains(err.Error(), "connection reset") {
			t.Errorf("raw database details leaked in error: %s", err.Error())
		}
		userRepo.overrideErr = nil
	})

	t.Run("db failure during refresh lookup", func(t *testing.T) {
		sessionRepo.overrideErr = rawDBErr
		_, err := authSvc.Refresh(context.Background(), "valid-token-string-12345")
		if !errors.Is(err, identityDomain.ErrAuthServiceUnavailable) {
			t.Errorf("expected ErrAuthServiceUnavailable on DB error during refresh, got: %v", err)
		}
		if errors.Is(err, identityDomain.ErrInvalidSession) {
			t.Errorf("DB failure must NOT be disguised as ErrInvalidSession")
		}
		sessionRepo.overrideErr = nil
	})

	t.Run("db failure during revoke", func(t *testing.T) {
		sessionRepo.overrideErr = rawDBErr
		err := authSvc.RevokeSession(context.Background(), uuid.New())
		if !errors.Is(err, identityDomain.ErrAuthServiceUnavailable) {
			t.Errorf("expected ErrAuthServiceUnavailable on DB error during revoke, got: %v", err)
		}
		sessionRepo.overrideErr = nil
	})

	t.Run("db failure during org resolution", func(t *testing.T) {
		hasher := NewDefaultArgon2idHasher()
		pwdHash, _ := hasher.Hash("Password123!")
		user, _ := identityDomain.NewUser("orguser@example.com", pwdHash, "Org User", false)
		_ = userRepo.Create(context.Background(), nil, user)

		memberRepo.overrideErr = rawDBErr
		_, err := authSvc.Login(context.Background(), "orguser@example.com", "Password123!", ClientMetadata{})
		if !errors.Is(err, identityDomain.ErrAuthServiceUnavailable) {
			t.Errorf("expected ErrAuthServiceUnavailable on org DB failure, got: %v", err)
		}
		memberRepo.overrideErr = nil
	})
}

func TestAuthService_ValidateAuthenticatedSession(t *testing.T) {
	authSvc, userRepo, _, _, _, _ := setupTestAuthService(t)
	hasher := NewDefaultArgon2idHasher()

	password := "SecretPassword123!"
	pwdHash, _ := hasher.Hash(password)

	userA, _ := identityDomain.NewUser("userA@example.com", pwdHash, "User A", false)
	userB, _ := identityDomain.NewUser("userB@example.com", pwdHash, "User B", false)
	inactiveUser, _ := identityDomain.NewUser("inactive@example.com", pwdHash, "Inactive", false)
	inactiveUser.Status = identityDomain.UserStatusInactive

	_ = userRepo.Create(context.Background(), nil, userA)
	_ = userRepo.Create(context.Background(), nil, userB)
	_ = userRepo.Create(context.Background(), nil, inactiveUser)

	loginResA, _ := authSvc.Login(context.Background(), "userA@example.com", password, ClientMetadata{})

	t.Run("valid session and user", func(t *testing.T) {
		res, err := authSvc.ValidateAuthenticatedSession(context.Background(), userA.ID, loginResA.SessionID)
		if err != nil {
			t.Fatalf("expected validation success, got: %v", err)
		}
		if res.User.ID != userA.ID {
			t.Errorf("expected User ID match")
		}
		if res.SessionID != loginResA.SessionID {
			t.Errorf("expected Session ID match")
		}
	})

	t.Run("session belongs to different user", func(t *testing.T) {
		_, err := authSvc.ValidateAuthenticatedSession(context.Background(), userB.ID, loginResA.SessionID)
		if !errors.Is(err, identityDomain.ErrInvalidSession) {
			t.Errorf("expected ErrInvalidSession when session belongs to another user, got: %v", err)
		}
	})

	t.Run("inactive user session", func(t *testing.T) {
		loginInactive, _ := authSvc.Login(context.Background(), "userA@example.com", password, ClientMetadata{})
		// Transition user to inactive
		userA.Status = identityDomain.UserStatusInactive

		_, err := authSvc.ValidateAuthenticatedSession(context.Background(), userA.ID, loginInactive.SessionID)
		if !errors.Is(err, identityDomain.ErrInvalidSession) {
			t.Errorf("expected ErrInvalidSession for inactive user, got: %v", err)
		}
		userA.Status = identityDomain.UserStatusActive // reset
	})

	t.Run("revoked session", func(t *testing.T) {
		_ = authSvc.RevokeSession(context.Background(), loginResA.SessionID)
		_, err := authSvc.ValidateAuthenticatedSession(context.Background(), userA.ID, loginResA.SessionID)
		if !errors.Is(err, identityDomain.ErrInvalidSession) {
			t.Errorf("expected ErrInvalidSession for revoked session, got: %v", err)
		}
	})
}

func TestAuthService_SessionRevocationAndIdempotency(t *testing.T) {
	authSvc, userRepo, _, _, _, _ := setupTestAuthService(t)
	hasher := NewDefaultArgon2idHasher()

	password := "SecretPassword123!"
	pwdHash, _ := hasher.Hash(password)

	userA, _ := identityDomain.NewUser("userA@example.com", pwdHash, "User A", false)
	userB, _ := identityDomain.NewUser("userB@example.com", pwdHash, "User B", false)
	_ = userRepo.Create(context.Background(), nil, userA)
	_ = userRepo.Create(context.Background(), nil, userB)

	// User A creates 2 sessions
	resA1, _ := authSvc.Login(context.Background(), "userA@example.com", password, ClientMetadata{})
	resA2, _ := authSvc.Login(context.Background(), "userA@example.com", password, ClientMetadata{})

	// User B creates 1 session
	resB, _ := authSvc.Login(context.Background(), "userB@example.com", password, ClientMetadata{})

	t.Run("revoke single session and test idempotency", func(t *testing.T) {
		err := authSvc.RevokeSession(context.Background(), resA1.SessionID)
		if err != nil {
			t.Fatalf("revoke session failed: %v", err)
		}

		// Validating revoked session returns ErrInvalidSession
		_, err = authSvc.ValidateAuthenticatedSession(context.Background(), userA.ID, resA1.SessionID)
		if !errors.Is(err, identityDomain.ErrInvalidSession) {
			t.Errorf("expected ErrInvalidSession for revoked session, got: %v", err)
		}

		// Refresh on revoked session fails
		_, err = authSvc.Refresh(context.Background(), resA1.RawRefreshToken)
		if !errors.Is(err, identityDomain.ErrInvalidSession) {
			t.Errorf("expected ErrInvalidSession for refresh on revoked session, got: %v", err)
		}

		// Revoke again -> must be idempotent without error
		err = authSvc.RevokeSession(context.Background(), resA1.SessionID)
		if err != nil {
			t.Errorf("idempotent revocation failed: %v", err)
		}

		// User A's second session is still active
		_, err = authSvc.ValidateAuthenticatedSession(context.Background(), userA.ID, resA2.SessionID)
		if err != nil {
			t.Errorf("second session should still be valid: %v", err)
		}
	})

	t.Run("revoke all sessions for user A", func(t *testing.T) {
		err := authSvc.RevokeAllUserSessions(context.Background(), userA.ID)
		if err != nil {
			t.Fatalf("revoke all sessions failed: %v", err)
		}

		// Both sessions of User A are now invalid
		_, err = authSvc.ValidateAuthenticatedSession(context.Background(), userA.ID, resA1.SessionID)
		if !errors.Is(err, identityDomain.ErrInvalidSession) {
			t.Errorf("expected ErrInvalidSession for session 1")
		}
		_, err = authSvc.ValidateAuthenticatedSession(context.Background(), userA.ID, resA2.SessionID)
		if !errors.Is(err, identityDomain.ErrInvalidSession) {
			t.Errorf("expected ErrInvalidSession for session 2")
		}

		// User B's session MUST still be active and valid
		_, err = authSvc.ValidateAuthenticatedSession(context.Background(), userB.ID, resB.SessionID)
		if err != nil {
			t.Errorf("user B session was affected by user A revocation: %v", err)
		}
	})
}

func TestAuthService_ExpiredSessionValidation(t *testing.T) {
	authSvc, userRepo, sessionRepo, _, _, _ := setupTestAuthService(t)
	hasher := NewDefaultArgon2idHasher()

	password := "SecretPassword123!"
	pwdHash, _ := hasher.Hash(password)
	user, _ := identityDomain.NewUser("user@example.com", pwdHash, "User", false)
	_ = userRepo.Create(context.Background(), nil, user)

	// Create an already-expired session
	tokenGen := NewSecureTokenGenerator()
	rawToken, tokenHash, _ := tokenGen.GenerateRefreshToken()
	expiredSession, _ := identityDomain.NewSession(user.ID, tokenHash, nil, nil, -1*time.Hour)
	_ = sessionRepo.Create(context.Background(), nil, expiredSession)

	t.Run("validate expired session fails", func(t *testing.T) {
		_, err := authSvc.ValidateAuthenticatedSession(context.Background(), user.ID, expiredSession.ID)
		if !errors.Is(err, identityDomain.ErrInvalidSession) {
			t.Errorf("expected ErrInvalidSession for expired session, got: %v", err)
		}
	})

	t.Run("refresh expired session fails", func(t *testing.T) {
		_, err := authSvc.Refresh(context.Background(), rawToken)
		if !errors.Is(err, identityDomain.ErrInvalidSession) {
			t.Errorf("expected ErrInvalidSession on refreshing expired session, got: %v", err)
		}
	})
}
