package bootstrap

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	identityDomain "backup-platform/internal/identity/domain"
	identityRepo "backup-platform/internal/identity/repository"
	orgDomain "backup-platform/internal/organization/domain"
	orgRepo "backup-platform/internal/organization/repository"
	"backup-platform/internal/platform/database"
)

// Safe sentinel errors that do not leak internal database details or credentials.
var (
	ErrDatabaseNotReady   = errors.New("database transaction manager is not initialized")
	ErrPasswordHashFailed = errors.New("failed to hash bootstrap password")
	ErrBootstrapFailed    = errors.New("bootstrap execution failed")
)

// Config encapsulates environment-driven credentials for initial admin bootstrap.
type Config struct {
	AdminEmail    string
	AdminPassword string
}

// Service performs safe, idempotent bootstrap of the initial System Admin and Internal Organization.
type Service struct {
	cfg        Config
	txManager  database.TxManager
	userRepo   identityRepo.UserRepository
	orgRepo    orgRepo.OrganizationRepository
	memberRepo orgRepo.MemberRepository
	hasher     identityDomain.PasswordHasher
	logger     *slog.Logger
}

// NewService constructs a new bootstrap Service.
func NewService(
	cfg Config,
	txManager database.TxManager,
	userRepo identityRepo.UserRepository,
	orgRepo orgRepo.OrganizationRepository,
	memberRepo orgRepo.MemberRepository,
	hasher identityDomain.PasswordHasher,
	logger *slog.Logger,
) *Service {
	return &Service{
		cfg:        cfg,
		txManager:  txManager,
		userRepo:   userRepo,
		orgRepo:    orgRepo,
		memberRepo: memberRepo,
		hasher:     hasher,
		logger:     logger,
	}
}

// Run executes the initial bootstrap check and initialization inside a managed transaction.
func (s *Service) Run(ctx context.Context) error {
	if s.txManager == nil {
		return ErrDatabaseNotReady
	}

	// 1. Validate bootstrap credentials. If missing, skip cleanly without error.
	email := strings.ToLower(strings.TrimSpace(s.cfg.AdminEmail))
	password := s.cfg.AdminPassword

	// Immediately clear secret from service struct state to minimize memory lifetime
	s.cfg.AdminPassword = ""

	if email == "" || password == "" {
		s.logger.Info("bootstrap skipped: no bootstrap credentials provided")
		return nil
	}

	// 2. Execute bootstrap operations inside a managed database transaction
	err := s.txManager.WithinTx(ctx, func(q database.Querier) error {
		// Idempotency check: check if an active system administrator already exists
		hasAdmin, err := s.userRepo.HasSystemAdmin(ctx, q)
		if err != nil {
			s.logger.Error("failed to query system admin during bootstrap")
			return err
		}

		if hasAdmin {
			// Best-effort release of local password reference when skipped
			password = ""
			s.logger.Info("bootstrap skipped: system admin already exists")
			return nil
		}

		// Hash password only when bootstrap is actually necessary
		passwordHash, err := s.hasher.Hash(password)
		// Immediately clear local plaintext password reference
		password = ""
		if err != nil {
			s.logger.Error("failed to hash bootstrap admin password")
			return ErrPasswordHashFailed
		}

		// Step A: Create initial System Admin User
		adminUser, err := identityDomain.NewUser(email, passwordHash, "System Administrator", true)
		if err != nil {
			s.logger.Error("failed to build bootstrap admin user entity")
			return err
		}

		if err := s.userRepo.Create(ctx, q, adminUser); err != nil {
			s.logger.Error("failed to persist bootstrap admin user")
			return err
		}

		// Step B: Ensure Default Internal Organization exists
		internalOrg, err := s.orgRepo.FindDefaultInternal(ctx, q)
		if err != nil {
			if errors.Is(err, orgDomain.ErrOrgNotFound) {
				internalOrg, err = orgDomain.NewOrganization("Internal Organization", "internal", true)
				if err != nil {
					s.logger.Error("failed to build internal organization entity")
					return err
				}
				if err := s.orgRepo.Create(ctx, q, internalOrg); err != nil {
					s.logger.Error("failed to persist internal organization")
					return err
				}
			} else {
				s.logger.Error("failed to query default internal organization during bootstrap")
				return err
			}
		}

		// Step C: Link System Admin to Internal Organization as admin member
		membership, err := orgDomain.NewMember(internalOrg.ID, adminUser.ID, orgDomain.RoleAdmin)
		if err != nil {
			s.logger.Error("failed to build bootstrap membership entity")
			return err
		}

		if err := s.memberRepo.Create(ctx, q, membership); err != nil {
			s.logger.Error("failed to persist bootstrap membership")
			return err
		}

		s.logger.Info("bootstrap completed successfully: initial system admin and internal organization created")
		return nil
	})

	if err != nil {
		if errors.Is(err, ErrPasswordHashFailed) {
			return ErrPasswordHashFailed
		}
		return ErrBootstrapFailed
	}

	return nil
}
