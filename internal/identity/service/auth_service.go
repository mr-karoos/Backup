package service

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	identityDomain "backup-platform/internal/identity/domain"
	identityRepo "backup-platform/internal/identity/repository"
	orgDomain "backup-platform/internal/organization/domain"
	orgRepo "backup-platform/internal/organization/repository"
	"backup-platform/internal/platform/database"
	"backup-platform/pkg/uuid"
)

const (
	SessionLifetime = 7 * 24 * time.Hour

	// Precomputed valid Argon2id hash (m=65536, t=3, p=4) used to equalize response timing when user is not found.
	dummyArgon2idHash = "$argon2id$v=19$m=65536,t=3,p=4$dHVtbXlzYWx0MTIzNDU2$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
)

// AuthenticatedUser provides a sanitized, safe representation of an authenticated user (without PasswordHash).
type AuthenticatedUser struct {
	ID            uuid.UUID
	Email         string
	FullName      string
	IsSystemAdmin bool
	Status        identityDomain.UserStatus
	CreatedAt     time.Time
}

// ClientMetadata encapsulates optional client connection metadata.
type ClientMetadata struct {
	IPAddress *string
	UserAgent *string
}

// AuthResult represents the output of a successful login authentication.
type AuthResult struct {
	User                AuthenticatedUser
	SessionID           uuid.UUID
	AccessToken         string
	AccessTokenExpires  time.Time
	RawRefreshToken     string
	RefreshTokenExpires time.Time
	DefaultOrgID        *uuid.UUID
}

// RefreshResult represents the output of a successful refresh token rotation.
type RefreshResult struct {
	UserID              uuid.UUID
	SessionID           uuid.UUID
	AccessToken         string
	AccessTokenExpires  time.Time
	RawRefreshToken     string
	RefreshTokenExpires time.Time
}

// SessionValidationResult represents the safe output of validating an active authenticated session.
type SessionValidationResult struct {
	User      AuthenticatedUser
	SessionID uuid.UUID
	ExpiresAt time.Time
}

// AuthService coordinates authentication, session management, token issuance, and revocation.
type AuthService struct {
	userRepo    identityRepo.UserRepository
	sessionRepo identityRepo.SessionRepository
	memberRepo  orgRepo.MemberRepository
	hasher      identityDomain.PasswordHasher
	tokenGen    TokenGenerator
	jwtService  TokenService
	txManager   database.TxManager
	logger      *slog.Logger
}

// NewAuthService constructs a new AuthService.
func NewAuthService(
	userRepo identityRepo.UserRepository,
	sessionRepo identityRepo.SessionRepository,
	memberRepo orgRepo.MemberRepository,
	hasher identityDomain.PasswordHasher,
	tokenGen TokenGenerator,
	jwtService TokenService,
	txManager database.TxManager,
	logger *slog.Logger,
) *AuthService {
	return &AuthService{
		userRepo:    userRepo,
		sessionRepo: sessionRepo,
		memberRepo:  memberRepo,
		hasher:      hasher,
		tokenGen:    tokenGen,
		jwtService:  jwtService,
		txManager:   txManager,
		logger:      logger,
	}
}

// Login authenticates a user by email and password, returning an access JWT and raw refresh token.
func (s *AuthService) Login(ctx context.Context, email, password string, meta ClientMetadata) (*AuthResult, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" || password == "" {
		return nil, identityDomain.ErrInvalidCredentials
	}

	q := s.txManager.Querier()

	// 1. Fetch user by email
	user, err := s.userRepo.FindByEmail(ctx, q, email)
	if err != nil {
		if errors.Is(err, identityDomain.ErrUserNotFound) {
			// Timing equalization: perform dummy Argon2id verification to reduce timing leak between unknown email and bad password
			_, _ = s.hasher.Verify(password, dummyArgon2idHash)
			return nil, identityDomain.ErrInvalidCredentials
		}
		s.logger.Error("database query failed during user login lookup")
		return nil, identityDomain.ErrAuthServiceUnavailable
	}

	// 2. Verify password against Argon2id hash for ALL existing users before checking status (timing equalization)
	matched, err := s.hasher.Verify(password, user.PasswordHash)
	if err != nil {
		s.logger.Error("password verification failed due to invalid stored credential state")
		return nil, identityDomain.ErrAuthServiceUnavailable
	}

	// 3. Reject if password did not match OR user is not active (equalizes timing for wrong password, inactive, and blocked)
	if !matched || user.Status != identityDomain.UserStatusActive {
		return nil, identityDomain.ErrInvalidCredentials
	}

	// 4. Generate opaque refresh token and its SHA-256 hash
	rawRefreshToken, tokenHash, err := s.tokenGen.GenerateRefreshToken()
	if err != nil {
		s.logger.Error("failed to generate refresh token entropy")
		return nil, identityDomain.ErrAuthServiceUnavailable
	}

	var result *AuthResult

	// 5. Execute session persistence, org resolution, and JWT issuance inside a managed transaction
	// This guarantees that if JWT signing or subsequent steps fail, the session is rolled back (no orphan sessions).
	err = s.txManager.WithinTx(ctx, func(txQ database.Querier) error {
		session, err := identityDomain.NewSession(user.ID, tokenHash, meta.IPAddress, meta.UserAgent, SessionLifetime)
		if err != nil {
			s.logger.Error("failed to construct session entity")
			return identityDomain.ErrAuthServiceUnavailable
		}

		if err := s.sessionRepo.Create(ctx, txQ, session); err != nil {
			s.logger.Error("failed to persist user session")
			return identityDomain.ErrAuthServiceUnavailable
		}

		defaultOrgID, err := s.resolveDefaultOrg(ctx, txQ, user.ID)
		if err != nil {
			s.logger.Error("failed to resolve default organization during login")
			return identityDomain.ErrAuthServiceUnavailable
		}

		accessToken, accessExpiry, err := s.jwtService.GenerateAccessToken(user.ID, session.ID, user.IsSystemAdmin)
		if err != nil {
			s.logger.Error("failed to issue access token during login")
			return identityDomain.ErrAuthServiceUnavailable
		}

		result = &AuthResult{
			User: AuthenticatedUser{
				ID:            user.ID,
				Email:         user.Email,
				FullName:      user.FullName,
				IsSystemAdmin: user.IsSystemAdmin,
				Status:        user.Status,
				CreatedAt:     user.CreatedAt,
			},
			SessionID:           session.ID,
			AccessToken:         accessToken,
			AccessTokenExpires:  accessExpiry,
			RawRefreshToken:     rawRefreshToken,
			RefreshTokenExpires: session.ExpiresAt,
			DefaultOrgID:        defaultOrgID,
		}

		return nil
	})

	if err != nil {
		return nil, identityDomain.ErrAuthServiceUnavailable
	}

	return result, nil
}

// Refresh atomically validates and rotates a refresh token, issuing a new access JWT and new refresh token.
func (s *AuthService) Refresh(ctx context.Context, rawRefreshToken string) (*RefreshResult, error) {
	rawRefreshToken = strings.TrimSpace(rawRefreshToken)
	if rawRefreshToken == "" {
		return nil, identityDomain.ErrInvalidRefreshToken
	}

	oldHash := s.tokenGen.HashRefreshToken(rawRefreshToken)
	now := time.Now().UTC()

	var result *RefreshResult

	// Execute rotation inside a transaction with conditional atomic update
	err := s.txManager.WithinTx(ctx, func(q database.Querier) error {
		// 1. Locate session by current refresh token hash
		session, err := s.sessionRepo.FindByRefreshTokenHash(ctx, q, oldHash)
		if err != nil {
			if errors.Is(err, identityDomain.ErrSessionNotFound) {
				return identityDomain.ErrInvalidSession
			}
			s.logger.Error("database query failed during refresh token lookup")
			return identityDomain.ErrAuthServiceUnavailable
		}

		// 2. Validate session state
		if !session.IsActive(now) {
			return identityDomain.ErrInvalidSession
		}

		// 3. Validate that the associated user exists and is active
		user, err := s.userRepo.FindByID(ctx, q, session.UserID)
		if err != nil {
			if errors.Is(err, identityDomain.ErrUserNotFound) {
				return identityDomain.ErrInvalidSession
			}
			s.logger.Error("database query failed during refresh user lookup")
			return identityDomain.ErrAuthServiceUnavailable
		}
		if user.Status != identityDomain.UserStatusActive {
			return identityDomain.ErrInvalidSession
		}

		// 4. Generate new refresh token and hash
		newRawToken, newHash, err := s.tokenGen.GenerateRefreshToken()
		if err != nil {
			s.logger.Error("failed to generate new refresh token during rotation")
			return identityDomain.ErrAuthServiceUnavailable
		}

		// 5. Perform atomic conditional update: oldHash -> newHash
		if err := s.sessionRepo.RotateRefreshToken(ctx, q, session.ID, oldHash, newHash, now); err != nil {
			if errors.Is(err, identityDomain.ErrInvalidSession) || errors.Is(err, identityDomain.ErrSessionNotFound) {
				return identityDomain.ErrInvalidSession
			}
			s.logger.Error("database error during refresh token rotation")
			return identityDomain.ErrAuthServiceUnavailable
		}

		// 6. Issue new short-lived access JWT
		accessToken, accessExpiry, err := s.jwtService.GenerateAccessToken(user.ID, session.ID, user.IsSystemAdmin)
		if err != nil {
			s.logger.Error("failed to generate access token during refresh")
			return identityDomain.ErrAuthServiceUnavailable
		}

		result = &RefreshResult{
			UserID:              user.ID,
			SessionID:           session.ID,
			AccessToken:         accessToken,
			AccessTokenExpires:  accessExpiry,
			RawRefreshToken:     newRawToken,
			RefreshTokenExpires: session.ExpiresAt,
		}

		return nil
	})

	if err != nil {
		if errors.Is(err, identityDomain.ErrInvalidSession) {
			return nil, identityDomain.ErrInvalidSession
		}
		return nil, identityDomain.ErrAuthServiceUnavailable
	}

	return result, nil
}

// RevokeSession explicitly revokes an active session by its ID.
func (s *AuthService) RevokeSession(ctx context.Context, sessionID uuid.UUID) error {
	if sessionID == uuid.Nil {
		return identityDomain.ErrInvalidSession
	}

	if err := s.sessionRepo.RevokeByID(ctx, s.txManager.Querier(), sessionID, time.Now().UTC()); err != nil {
		if errors.Is(err, identityDomain.ErrSessionNotFound) {
			return nil // Idempotent revocation
		}
		s.logger.Error("database error during session revocation")
		return identityDomain.ErrAuthServiceUnavailable
	}

	return nil
}

// RevokeAllUserSessions explicitly revokes all active sessions for a user (e.g. password change).
func (s *AuthService) RevokeAllUserSessions(ctx context.Context, userID uuid.UUID) error {
	if userID == uuid.Nil {
		return identityDomain.ErrInvalidSession
	}

	if err := s.sessionRepo.RevokeAllForUser(ctx, s.txManager.Querier(), userID, time.Now().UTC()); err != nil {
		s.logger.Error("database error during bulk user session revocation")
		return identityDomain.ErrAuthServiceUnavailable
	}

	return nil
}

// ValidateAuthenticatedSession verifies that a session exists, is active, belongs to the specified active user,
// and returns safe authenticated details without leaking PasswordHash.
func (s *AuthService) ValidateAuthenticatedSession(ctx context.Context, userID, sessionID uuid.UUID) (*SessionValidationResult, error) {
	if userID == uuid.Nil || sessionID == uuid.Nil {
		return nil, identityDomain.ErrInvalidSession
	}

	q := s.txManager.Querier()

	// 1. Fetch and validate session
	session, err := s.sessionRepo.FindByID(ctx, q, sessionID)
	if err != nil {
		if errors.Is(err, identityDomain.ErrSessionNotFound) {
			return nil, identityDomain.ErrInvalidSession
		}
		s.logger.Error("database error during session validation query")
		return nil, identityDomain.ErrAuthServiceUnavailable
	}

	if !session.IsActive(time.Now().UTC()) {
		return nil, identityDomain.ErrInvalidSession
	}

	// 2. Ensure session belongs to the claims subject
	if session.UserID != userID {
		return nil, identityDomain.ErrInvalidSession
	}

	// 3. Fetch and validate user status
	user, err := s.userRepo.FindByID(ctx, q, userID)
	if err != nil {
		if errors.Is(err, identityDomain.ErrUserNotFound) {
			return nil, identityDomain.ErrInvalidSession
		}
		s.logger.Error("database error during user validation query")
		return nil, identityDomain.ErrAuthServiceUnavailable
	}

	if user.Status != identityDomain.UserStatusActive {
		return nil, identityDomain.ErrInvalidSession
	}

	return &SessionValidationResult{
		User: AuthenticatedUser{
			ID:            user.ID,
			Email:         user.Email,
			FullName:      user.FullName,
			IsSystemAdmin: user.IsSystemAdmin,
			Status:        user.Status,
			CreatedAt:     user.CreatedAt,
		},
		SessionID: session.ID,
		ExpiresAt: session.ExpiresAt,
	}, nil
}

// resolveDefaultOrg selects the default organization ID based on internal org priority.
// Distinguishes cleanly between database failure (returning error) and user having no orgs (returning nil, nil).
func (s *AuthService) resolveDefaultOrg(ctx context.Context, q database.Querier, userID uuid.UUID) (*uuid.UUID, error) {
	orgs, err := s.memberRepo.ListUserOrganizations(ctx, q, userID)
	if err != nil {
		return nil, err
	}
	if len(orgs) == 0 {
		return nil, nil
	}

	// Priority 1: Default internal organization
	for _, o := range orgs {
		if o.IsDefaultInternal && o.Status == orgDomain.OrgStatusActive {
			id := o.ID
			return &id, nil
		}
	}

	// Priority 2: First active organization
	id := orgs[0].ID
	return &id, nil
}
