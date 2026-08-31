package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"backup-platform/internal/connector"
	credDomain "backup-platform/internal/credential/domain"
	"backup-platform/internal/credential/payload"
	"backup-platform/internal/resource/domain"
	"backup-platform/pkg/uuid"
)

type fakeDatabaseDiscoverer struct {
	result         []connector.DatabaseInfo
	err            error
	callCount      int
	lastTarget     connector.Target
	lastPayload    *payload.PayloadV1
	onDiscCallback func(target connector.Target, credPayload *payload.PayloadV1)
}

func (f *fakeDatabaseDiscoverer) DiscoverDatabases(ctx context.Context, target connector.Target, credPayload *payload.PayloadV1) ([]connector.DatabaseInfo, error) {
	f.callCount++
	f.lastTarget = target
	f.lastPayload = credPayload
	if f.onDiscCallback != nil {
		f.onDiscCallback(target, credPayload)
	}
	if f.err != nil {
		return nil, f.err
	}
	return f.result, nil
}

func TestDatabaseDiscoveryService_Ubuntu_Success(t *testing.T) {
	ctx := context.Background()
	orgID := uuid.New()
	resID := uuid.New()
	credID := uuid.New()
	fp := "SHA256:fingerprint"

	payloadBytes, _ := payload.EncodeV1("ssh-secret-pass", nil)
	repo := &fakeConnectionTestRepo{
		resource: sampleUbuntuResource(orgID, resID, credID, domain.StatusActive, &fp),
	}
	vault := &fakeVaultReader{credType: credDomain.TypeSSHPassword, payloadBytes: payloadBytes}

	tables48 := int64(48)
	tables12 := int64(12)
	discoverer := &fakeDatabaseDiscoverer{
		result: []connector.DatabaseInfo{
			{Name: "zeta_dw", SizeBytes: 524288000, TablesCount: &tables12, Status: connector.DatabaseStatusAccessible},
			{Name: "mysql", SizeBytes: 1000, TablesCount: nil, Status: connector.DatabaseStatusAccessible}, // System DB to filter
			{Name: "alpha_prod", SizeBytes: 104857600, TablesCount: &tables48, Status: connector.DatabaseStatusAccessible},
		},
	}
	registry := connector.NewDiscoveryRegistry()
	registry.Register(domain.TypeUbuntuSSH, discoverer)

	svc := NewDatabaseDiscoveryService(repo, vault, registry, &fakeTxManager{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	dbs, err := svc.DiscoverDatabases(ctx, orgID, resID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(dbs) != 2 {
		t.Fatalf("expected 2 databases after filtering, got: %d", len(dbs))
	}

	// Deterministic alphabetical sort
	if dbs[0].Name != "alpha_prod" || dbs[0].SizeBytes != 104857600 || *dbs[0].TablesCount != 48 || dbs[0].Status != connector.DatabaseStatusAccessible {
		t.Errorf("unexpected database 0: %+v", dbs[0])
	}
	if dbs[1].Name != "zeta_dw" || dbs[1].SizeBytes != 524288000 || *dbs[1].TablesCount != 12 || dbs[1].Status != connector.DatabaseStatusAccessible {
		t.Errorf("unexpected database 1: %+v", dbs[1])
	}

	// Invariant: GET discovery must NEVER persist state
	if repo.updateTestStateCalls != 0 {
		t.Errorf("expected 0 DB state mutation calls, got: %d", repo.updateTestStateCalls)
	}
}

func TestDatabaseDiscoveryService_CPanel_Success(t *testing.T) {
	ctx := context.Background()
	orgID := uuid.New()
	resID := uuid.New()
	credID := uuid.New()
	useHTTPS := true

	payloadBytes, _ := payload.EncodeV1("cpanel-token", nil)
	repo := &fakeConnectionTestRepo{
		resource: sampleCPanelResource(orgID, resID, credID, domain.StatusActive, "mycpanel", &useHTTPS),
	}
	vault := &fakeVaultReader{credType: credDomain.TypeCPanelAPIToken, payloadBytes: payloadBytes}

	discoverer := &fakeDatabaseDiscoverer{
		result: []connector.DatabaseInfo{
			{Name: "mycpanel_shop", SizeBytes: 4161, TablesCount: nil, Status: connector.DatabaseStatusAccessible},
		},
	}
	registry := connector.NewDiscoveryRegistry()
	registry.Register(domain.TypeCPanel, discoverer)

	svc := NewDatabaseDiscoveryService(repo, vault, registry, &fakeTxManager{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	dbs, err := svc.DiscoverDatabases(ctx, orgID, resID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(dbs) != 1 {
		t.Fatalf("expected 1 database, got: %d", len(dbs))
	}
	if dbs[0].Name != "mycpanel_shop" || dbs[0].SizeBytes != 4161 || dbs[0].TablesCount != nil || dbs[0].Status != connector.DatabaseStatusAccessible {
		t.Errorf("unexpected cpanel database: %+v", dbs[0])
	}
	if repo.updateTestStateCalls != 0 {
		t.Errorf("expected 0 DB state mutations")
	}
}

func TestDatabaseDiscoveryService_Preflight_UbuntuMissingFingerprint(t *testing.T) {
	ctx := context.Background()
	orgID := uuid.New()
	resID := uuid.New()
	credID := uuid.New()

	repo := &fakeConnectionTestRepo{
		resource: sampleUbuntuResource(orgID, resID, credID, domain.StatusActive, nil), // Missing fingerprint
	}
	vault := &fakeVaultReader{err: errors.New("vault must not be called")}
	discoverer := &fakeDatabaseDiscoverer{}
	registry := connector.NewDiscoveryRegistry()
	registry.Register(domain.TypeUbuntuSSH, discoverer)

	svc := NewDatabaseDiscoveryService(repo, vault, registry, &fakeTxManager{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	_, err := svc.DiscoverDatabases(ctx, orgID, resID)

	if !errors.Is(err, domain.ErrInvalidHostKeyFingerprint) {
		t.Errorf("expected ErrInvalidHostKeyFingerprint, got: %v", err)
	}
	if vault.callCount != 0 {
		t.Errorf("vault must not be called on preflight failure")
	}
	if discoverer.callCount != 0 {
		t.Errorf("discoverer must not be called on preflight failure")
	}
}

func TestDatabaseDiscoveryService_Preflight_CPanelInvalidUsername(t *testing.T) {
	ctx := context.Background()
	orgID := uuid.New()
	resID := uuid.New()
	credID := uuid.New()
	useHTTPS := true

	invalidUsernames := []string{"MyCPanel", "user:name", "user\nname", "user\rname", "user\x00name"}
	for _, u := range invalidUsernames {
		t.Run("Username "+u, func(t *testing.T) {
			repo := &fakeConnectionTestRepo{
				resource: sampleCPanelResource(orgID, resID, credID, domain.StatusActive, u, &useHTTPS),
			}
			vault := &fakeVaultReader{err: errors.New("vault must not be called")}
			discoverer := &fakeDatabaseDiscoverer{}
			registry := connector.NewDiscoveryRegistry()
			registry.Register(domain.TypeCPanel, discoverer)

			svc := NewDatabaseDiscoveryService(repo, vault, registry, &fakeTxManager{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
			_, err := svc.DiscoverDatabases(ctx, orgID, resID)

			if !errors.Is(err, domain.ErrInvalidConnectorConfig) {
				t.Errorf("expected ErrInvalidConnectorConfig, got: %v", err)
			}
			if vault.callCount != 0 {
				t.Errorf("vault must not be called")
			}
			if discoverer.callCount != 0 {
				t.Errorf("discoverer must not be called")
			}
		})
	}
}

func TestDatabaseDiscoveryService_Preflight_CPanelHTTPSFalse(t *testing.T) {
	ctx := context.Background()
	orgID := uuid.New()
	resID := uuid.New()
	credID := uuid.New()
	useHTTPS := false

	repo := &fakeConnectionTestRepo{
		resource: sampleCPanelResource(orgID, resID, credID, domain.StatusActive, "mycpanel", &useHTTPS),
	}
	vault := &fakeVaultReader{err: errors.New("vault must not be called")}
	discoverer := &fakeDatabaseDiscoverer{}
	registry := connector.NewDiscoveryRegistry()
	registry.Register(domain.TypeCPanel, discoverer)

	svc := NewDatabaseDiscoveryService(repo, vault, registry, &fakeTxManager{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	_, err := svc.DiscoverDatabases(ctx, orgID, resID)

	if !errors.Is(err, domain.ErrInvalidConnectorConfig) {
		t.Errorf("expected ErrInvalidConnectorConfig for use_https=false, got: %v", err)
	}
	if vault.callCount != 0 {
		t.Errorf("vault must not be called")
	}
	if discoverer.callCount != 0 {
		t.Errorf("discoverer must not be called")
	}
}

func TestDatabaseDiscoveryService_RegistryMissingDiscoverer(t *testing.T) {
	ctx := context.Background()
	orgID := uuid.New()
	resID := uuid.New()
	credID := uuid.New()
	fp := "SHA256:fp"

	repo := &fakeConnectionTestRepo{
		resource: sampleUbuntuResource(orgID, resID, credID, domain.StatusActive, &fp),
	}
	vault := &fakeVaultReader{err: errors.New("vault must not be called")}
	emptyRegistry := connector.NewDiscoveryRegistry()

	svc := NewDatabaseDiscoveryService(repo, vault, emptyRegistry, &fakeTxManager{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	_, err := svc.DiscoverDatabases(ctx, orgID, resID)

	if !errors.Is(err, domain.ErrResourceServiceUnavailable) {
		t.Errorf("expected ErrResourceServiceUnavailable when discoverer missing, got: %v", err)
	}
	if vault.callCount != 0 {
		t.Errorf("vault must not be called")
	}
}

func TestDatabaseDiscoveryService_CredentialTypeMismatch(t *testing.T) {
	ctx := context.Background()
	orgID := uuid.New()
	resID := uuid.New()
	credID := uuid.New()
	fp := "SHA256:fp"

	payloadBytes, _ := payload.EncodeV1("token", nil)
	repo := &fakeConnectionTestRepo{
		resource: sampleUbuntuResource(orgID, resID, credID, domain.StatusActive, &fp),
	}
	// Vault returns cpanel token for Ubuntu SSH resource
	vault := &fakeVaultReader{credType: credDomain.TypeCPanelAPIToken, payloadBytes: payloadBytes}

	discoverer := &fakeDatabaseDiscoverer{}
	registry := connector.NewDiscoveryRegistry()
	registry.Register(domain.TypeUbuntuSSH, discoverer)

	svc := NewDatabaseDiscoveryService(repo, vault, registry, &fakeTxManager{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	_, err := svc.DiscoverDatabases(ctx, orgID, resID)

	if !errors.Is(err, domain.ErrResourceServiceUnavailable) {
		t.Errorf("expected ErrResourceServiceUnavailable on type mismatch, got: %v", err)
	}
	if discoverer.callCount != 0 {
		t.Errorf("discoverer must not be called on type mismatch")
	}
}

func TestDatabaseDiscoveryService_PayloadDecodeError(t *testing.T) {
	ctx := context.Background()
	orgID := uuid.New()
	resID := uuid.New()
	credID := uuid.New()
	fp := "SHA256:fp"

	repo := &fakeConnectionTestRepo{
		resource: sampleUbuntuResource(orgID, resID, credID, domain.StatusActive, &fp),
	}
	vault := &fakeVaultReader{credType: credDomain.TypeSSHPassword, payloadBytes: []byte(`{not valid json`)}

	discoverer := &fakeDatabaseDiscoverer{}
	registry := connector.NewDiscoveryRegistry()
	registry.Register(domain.TypeUbuntuSSH, discoverer)

	svc := NewDatabaseDiscoveryService(repo, vault, registry, &fakeTxManager{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	_, err := svc.DiscoverDatabases(ctx, orgID, resID)

	if !errors.Is(err, domain.ErrResourceServiceUnavailable) {
		t.Errorf("expected ErrResourceServiceUnavailable on decode error, got: %v", err)
	}
	if discoverer.callCount != 0 {
		t.Errorf("discoverer must not be called")
	}
}

func TestDatabaseDiscoveryService_ZeroizationBeforeDiscovery(t *testing.T) {
	ctx := context.Background()
	orgID := uuid.New()
	resID := uuid.New()
	credID := uuid.New()
	fp := "SHA256:fp"

	payloadBytes, _ := payload.EncodeV1("discovery-secret", nil)

	var vaultReturnedSlice []byte
	customVault := &vaultReaderFunc{
		fn: func(ctx context.Context, o, c uuid.UUID) (credDomain.Type, []byte, error) {
			vaultReturnedSlice = make([]byte, len(payloadBytes))
			copy(vaultReturnedSlice, payloadBytes)
			return credDomain.TypeSSHPassword, vaultReturnedSlice, nil
		},
	}

	repo := &fakeConnectionTestRepo{
		resource: sampleUbuntuResource(orgID, resID, credID, domain.StatusActive, &fp),
	}

	var sliceWasZeroedBeforeDiscovery bool
	var secretValueInDiscoverer string

	discoverer := &fakeDatabaseDiscoverer{
		result: []connector.DatabaseInfo{{Name: "test_db", SizeBytes: 100, Status: connector.DatabaseStatusAccessible}},
		onDiscCallback: func(target connector.Target, credPayload *payload.PayloadV1) {
			allZero := make([]byte, len(vaultReturnedSlice))
			sliceWasZeroedBeforeDiscovery = bytes.Equal(vaultReturnedSlice, allZero)
			if credPayload != nil {
				secretValueInDiscoverer = credPayload.Secret
			}
		},
	}

	registry := connector.NewDiscoveryRegistry()
	registry.Register(domain.TypeUbuntuSSH, discoverer)

	svc := NewDatabaseDiscoveryService(repo, customVault, registry, &fakeTxManager{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	_, err := svc.DiscoverDatabases(ctx, orgID, resID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !sliceWasZeroedBeforeDiscovery {
		t.Errorf("SECURITY FLAW: Decrypted JSON bytes buffer was NOT zeroed before discoverer was invoked!")
	}
	if secretValueInDiscoverer != "discovery-secret" {
		t.Errorf("expected secret intact during discoverer, got: %q", secretValueInDiscoverer)
	}
}

func TestDatabaseDiscoveryService_PayloadCleanupAfterDiscovery(t *testing.T) {
	ctx := context.Background()
	orgID := uuid.New()
	resID := uuid.New()
	credID := uuid.New()
	fp := "SHA256:fp"

	pass := "my-passphrase"
	payloadBytes, _ := payload.EncodeV1("my-secret", &pass)

	t.Run("Cleaned on discovery success", func(t *testing.T) {
		repo := &fakeConnectionTestRepo{
			resource: sampleUbuntuResource(orgID, resID, credID, domain.StatusActive, &fp),
		}
		vault := &fakeVaultReader{credType: credDomain.TypeSSHPassword, payloadBytes: payloadBytes}
		discoverer := &fakeDatabaseDiscoverer{
			result: []connector.DatabaseInfo{{Name: "test_db", SizeBytes: 100, Status: connector.DatabaseStatusAccessible}},
		}
		registry := connector.NewDiscoveryRegistry()
		registry.Register(domain.TypeUbuntuSSH, discoverer)

		svc := NewDatabaseDiscoveryService(repo, vault, registry, &fakeTxManager{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
		_, _ = svc.DiscoverDatabases(ctx, orgID, resID)

		if discoverer.lastPayload.Secret != "" {
			t.Errorf("expected secret reference to be cleared, got: %q", discoverer.lastPayload.Secret)
		}
		if discoverer.lastPayload.Passphrase != nil {
			t.Errorf("expected passphrase reference to be cleared, got: %v", discoverer.lastPayload.Passphrase)
		}
	})

	t.Run("Cleaned on discovery error", func(t *testing.T) {
		repo := &fakeConnectionTestRepo{
			resource: sampleUbuntuResource(orgID, resID, credID, domain.StatusActive, &fp),
		}
		vault := &fakeVaultReader{credType: credDomain.TypeSSHPassword, payloadBytes: payloadBytes}
		discoverer := &fakeDatabaseDiscoverer{
			err: errors.New("remote error"),
		}
		registry := connector.NewDiscoveryRegistry()
		registry.Register(domain.TypeUbuntuSSH, discoverer)

		svc := NewDatabaseDiscoveryService(repo, vault, registry, &fakeTxManager{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
		_, _ = svc.DiscoverDatabases(ctx, orgID, resID)

		if discoverer.lastPayload.Secret != "" {
			t.Errorf("expected secret reference to be cleared, got: %q", discoverer.lastPayload.Secret)
		}
		if discoverer.lastPayload.Passphrase != nil {
			t.Errorf("expected passphrase reference to be cleared, got: %v", discoverer.lastPayload.Passphrase)
		}
	})
}

func TestDatabaseDiscoveryService_ContractIntegrityCheck(t *testing.T) {
	ctx := context.Background()
	orgID := uuid.New()
	resID := uuid.New()
	credID := uuid.New()
	fp := "SHA256:fp"

	payloadBytes, _ := payload.EncodeV1("pass", nil)

	t.Run("Rejects duplicate database names from discoverer", func(t *testing.T) {
		repo := &fakeConnectionTestRepo{
			resource: sampleUbuntuResource(orgID, resID, credID, domain.StatusActive, &fp),
		}
		vault := &fakeVaultReader{credType: credDomain.TypeSSHPassword, payloadBytes: payloadBytes}
		discoverer := &fakeDatabaseDiscoverer{
			result: []connector.DatabaseInfo{
				{Name: "app_db", SizeBytes: 100, Status: connector.DatabaseStatusAccessible},
				{Name: "app_db", SizeBytes: 200, Status: connector.DatabaseStatusAccessible},
			},
		}
		registry := connector.NewDiscoveryRegistry()
		registry.Register(domain.TypeUbuntuSSH, discoverer)

		svc := NewDatabaseDiscoveryService(repo, vault, registry, &fakeTxManager{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
		_, err := svc.DiscoverDatabases(ctx, orgID, resID)

		if !errors.Is(err, domain.ErrResourceServiceUnavailable) {
			t.Errorf("expected ErrResourceServiceUnavailable for duplicates, got: %v", err)
		}
	})
}

func TestDatabaseDiscoveryService_DisabledResourceCanBeDiscovered(t *testing.T) {
	ctx := context.Background()
	orgID := uuid.New()
	resID := uuid.New()
	credID := uuid.New()
	fp := "SHA256:fp"

	payloadBytes, _ := payload.EncodeV1("pass", nil)
	repo := &fakeConnectionTestRepo{
		resource: sampleUbuntuResource(orgID, resID, credID, domain.StatusDisabled, &fp), // Disabled status
	}
	vault := &fakeVaultReader{credType: credDomain.TypeSSHPassword, payloadBytes: payloadBytes}
	discoverer := &fakeDatabaseDiscoverer{
		result: []connector.DatabaseInfo{{Name: "app_db", SizeBytes: 100, Status: connector.DatabaseStatusAccessible}},
	}
	registry := connector.NewDiscoveryRegistry()
	registry.Register(domain.TypeUbuntuSSH, discoverer)

	svc := NewDatabaseDiscoveryService(repo, vault, registry, &fakeTxManager{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	dbs, err := svc.DiscoverDatabases(ctx, orgID, resID)
	if err != nil {
		t.Fatalf("unexpected error on disabled resource discovery: %v", err)
	}

	if len(dbs) != 1 {
		t.Fatalf("expected 1 database")
	}
}

func TestDatabaseDiscoveryService_RemoteErrorLoggingSafety(t *testing.T) {
	ctx := context.Background()
	orgID := uuid.New()
	resID := uuid.New()
	credID := uuid.New()
	fp := "SHA256:fp"

	payloadBytes, _ := payload.EncodeV1("pass", nil)
	repo := &fakeConnectionTestRepo{
		resource: sampleUbuntuResource(orgID, resID, credID, domain.StatusActive, &fp),
	}
	vault := &fakeVaultReader{credType: credDomain.TypeSSHPassword, payloadBytes: payloadBytes}
	discoverer := &fakeDatabaseDiscoverer{
		err: errors.New("remote error with password=SUPERSECRET_LEAK and /var/lib/mysql"),
	}
	registry := connector.NewDiscoveryRegistry()
	registry.Register(domain.TypeUbuntuSSH, discoverer)

	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))

	svc := NewDatabaseDiscoveryService(repo, vault, registry, &fakeTxManager{}, logger)
	_, err := svc.DiscoverDatabases(ctx, orgID, resID)

	if !errors.Is(err, domain.ErrResourceServiceUnavailable) {
		t.Errorf("expected ErrResourceServiceUnavailable, got: %v", err)
	}

	logText := logBuf.String()
	if strings.Contains(logText, "SUPERSECRET_LEAK") || strings.Contains(logText, "/var/lib/mysql") {
		t.Errorf("SECURITY FLAW: sensitive strings leaked in service log: %s", logText)
	}
}
