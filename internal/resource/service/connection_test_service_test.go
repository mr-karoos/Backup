package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"backup-platform/internal/connector"
	credDomain "backup-platform/internal/credential/domain"
	"backup-platform/internal/credential/payload"
	"backup-platform/internal/credential/secretcrypto"
	"backup-platform/internal/platform/database"
	"backup-platform/internal/resource/domain"
	"backup-platform/pkg/uuid"
)

type fakeVaultReader struct {
	credType       credDomain.Type
	payloadBytes   []byte
	err            error
	callCount      int
	capturedOrgID  uuid.UUID
	capturedCredID uuid.UUID
}

func (f *fakeVaultReader) LoadCredentialForUse(ctx context.Context, orgID, credID uuid.UUID) (credDomain.Type, []byte, error) {
	f.callCount++
	f.capturedOrgID = orgID
	f.capturedCredID = credID
	if f.err != nil {
		return "", nil, f.err
	}
	res := make([]byte, len(f.payloadBytes))
	copy(res, f.payloadBytes)
	return f.credType, res, nil
}

type fakeConnectionTester struct {
	result         *connector.ProbeResult
	err            error
	callCount      int
	lastTarget     connector.Target
	lastPayload    *payload.PayloadV1
	capturedCopy   []byte
	onTestCallback func(target connector.Target, credPayload *payload.PayloadV1)
}

func (f *fakeConnectionTester) TestConnection(ctx context.Context, target connector.Target, credPayload *payload.PayloadV1) (*connector.ProbeResult, error) {
	f.callCount++
	f.lastTarget = target
	f.lastPayload = credPayload
	if credPayload != nil {
		f.capturedCopy = []byte(credPayload.Secret)
	}
	if f.onTestCallback != nil {
		f.onTestCallback(target, credPayload)
	}
	if f.err != nil {
		return nil, f.err
	}
	return f.result, nil
}

type fakeConnectionTestRepo struct {
	resource                *domain.ResourceWithConnector
	findErr                 error
	updateTestStateErr      error
	updateTestStateCalls    int
	lastUpdatedTestAt       time.Time
	lastUpdatedStatus       domain.ConnectionStatus
	lastUpdatedError        *string
	lastUpdatedResourceStat domain.Status
}

func (f *fakeConnectionTestRepo) CreateResource(ctx context.Context, q database.Querier, res *domain.Resource) error {
	return nil
}
func (f *fakeConnectionTestRepo) CreateConnector(ctx context.Context, q database.Querier, conn *domain.ResourceConnector) error {
	return nil
}
func (f *fakeConnectionTestRepo) UpdateResource(ctx context.Context, q database.Querier, res *domain.Resource) error {
	return nil
}
func (f *fakeConnectionTestRepo) UpdateConnector(ctx context.Context, q database.Querier, conn *domain.ResourceConnector) error {
	return nil
}
func (f *fakeConnectionTestRepo) FindByIDForOrganization(ctx context.Context, q database.Querier, orgID, resID uuid.UUID) (*domain.ResourceWithConnector, error) {
	if f.findErr != nil {
		return nil, f.findErr
	}
	if f.resource != nil {
		return f.resource, nil
	}
	return nil, domain.ErrResourceNotFound
}
func (f *fakeConnectionTestRepo) ListForOrganization(ctx context.Context, q database.Querier, orgID uuid.UUID) ([]*domain.ResourceWithConnector, error) {
	return nil, nil
}
func (f *fakeConnectionTestRepo) ArchiveForOrganization(ctx context.Context, q database.Querier, orgID, resID uuid.UUID) error {
	return nil
}
func (f *fakeConnectionTestRepo) UpdateConnectionTestStateForOrganization(
	ctx context.Context,
	q database.Querier,
	orgID, resID uuid.UUID,
	lastTestAt time.Time,
	lastStatus domain.ConnectionStatus,
	lastError *string,
	newResourceStatus domain.Status,
) error {
	f.updateTestStateCalls++
	f.lastUpdatedTestAt = lastTestAt
	f.lastUpdatedStatus = lastStatus
	f.lastUpdatedError = lastError
	f.lastUpdatedResourceStat = newResourceStatus
	return f.updateTestStateErr
}

func sampleUbuntuResource(orgID, resID, credID uuid.UUID, status domain.Status, fingerprint *string) *domain.ResourceWithConnector {
	now := time.Now().UTC()
	return &domain.ResourceWithConnector{
		Resource: &domain.Resource{
			ID:             resID,
			OrganizationID: orgID,
			Name:           "Ubuntu App Server",
			Type:           domain.TypeUbuntuSSH,
			Status:         status,
			CreatedAt:      now,
			UpdatedAt:      now,
		},
		Connector: &domain.ResourceConnector{
			ID:                 uuid.New(),
			OrganizationID:     orgID,
			ResourceID:         resID,
			ConnectorType:      domain.ConnectorTypeUbuntuSSH,
			CredentialID:       credID,
			Host:               "192.168.1.50",
			Port:               22,
			AuthType:           domain.AuthTypeSSHPassword,
			HostKeyFingerprint: fingerprint,
			Config: domain.ConnectorConfig{
				Username: "root",
			},
			CreatedAt: now,
			UpdatedAt: now,
		},
		CredentialName: "SSH Credential",
	}
}

func sampleCPanelResource(orgID, resID, credID uuid.UUID, status domain.Status, username string, useHTTPS *bool) *domain.ResourceWithConnector {
	now := time.Now().UTC()
	return &domain.ResourceWithConnector{
		Resource: &domain.Resource{
			ID:             resID,
			OrganizationID: orgID,
			Name:           "cPanel Server",
			Type:           domain.TypeCPanel,
			Status:         status,
			CreatedAt:      now,
			UpdatedAt:      now,
		},
		Connector: &domain.ResourceConnector{
			ID:             uuid.New(),
			OrganizationID: orgID,
			ResourceID:     resID,
			ConnectorType:  domain.ConnectorTypeCPanel,
			CredentialID:   credID,
			Host:           "cpanel.example.com",
			Port:           2083,
			AuthType:       domain.AuthTypeCPanelAPIToken,
			Config: domain.ConnectorConfig{
				Username: username,
				UseHTTPS: useHTTPS,
			},
			CreatedAt: now,
			UpdatedAt: now,
		},
		CredentialName: "cPanel Token",
	}
}

func TestConnectionTestService_SSH_Success(t *testing.T) {
	ctx := context.Background()
	orgID := uuid.New()
	resID := uuid.New()
	credID := uuid.New()
	fp := "SHA256:validfingerprint"

	payloadBytes, _ := payload.EncodeV1("super-secret-password", nil)
	vault := &fakeVaultReader{
		credType:     credDomain.TypeSSHPassword,
		payloadBytes: payloadBytes,
	}

	repo := &fakeConnectionTestRepo{
		resource: sampleUbuntuResource(orgID, resID, credID, domain.StatusActive, &fp),
	}

	tester := &fakeConnectionTester{
		result: &connector.ProbeResult{
			Success:     true,
			Latency:     45 * time.Millisecond,
			CheckedAt:   time.Now().UTC(),
			FailureKind: connector.FailureKindNone,
			Details: map[string]any{
				"server_banner": "SSH-2.0-OpenSSH_8.9",
			},
		},
	}

	registry := connector.NewRegistry()
	registry.Register(domain.TypeUbuntuSSH, tester)

	txMgr := &fakeTxManager{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	svc := NewConnectionTestService(repo, vault, registry, txMgr, logger)

	resp, err := svc.TestConnection(ctx, orgID, resID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.Status != "success" {
		t.Errorf("expected status 'success', got: %s", resp.Status)
	}
	if resp.LatencyMS != 45 {
		t.Errorf("expected latency 45ms, got: %d", resp.LatencyMS)
	}
	if resp.Details["server_banner"] != "SSH-2.0-OpenSSH_8.9" {
		t.Errorf("unexpected details: %+v", resp.Details)
	}
	if resp.Details["auth_method"] != "password" {
		t.Errorf("expected locally derived auth_method 'password', got: %v", resp.Details["auth_method"])
	}

	// Verify DB state persistence
	if repo.updateTestStateCalls != 1 {
		t.Fatalf("expected 1 update call to repo, got %d", repo.updateTestStateCalls)
	}
	if repo.lastUpdatedStatus != domain.ConnectionStatusSuccess {
		t.Errorf("expected ConnectionStatusSuccess, got: %s", repo.lastUpdatedStatus)
	}
	if repo.lastUpdatedError != nil {
		t.Errorf("expected nil lastUpdatedError, got: %v", *repo.lastUpdatedError)
	}
	if repo.lastUpdatedResourceStat != domain.StatusActive {
		t.Errorf("expected StatusActive, got: %s", repo.lastUpdatedResourceStat)
	}
}

func TestConnectionTestService_SSH_ExpectedRemoteFailure(t *testing.T) {
	ctx := context.Background()
	orgID := uuid.New()
	resID := uuid.New()
	credID := uuid.New()
	fp := "SHA256:validfingerprint"

	payloadBytes, _ := payload.EncodeV1("wrong-password", nil)
	vault := &fakeVaultReader{
		credType:     credDomain.TypeSSHPassword,
		payloadBytes: payloadBytes,
	}

	repo := &fakeConnectionTestRepo{
		resource: sampleUbuntuResource(orgID, resID, credID, domain.StatusActive, &fp),
	}

	tester := &fakeConnectionTester{
		result: &connector.ProbeResult{
			Success:     false,
			Latency:     120 * time.Millisecond,
			CheckedAt:   time.Now().UTC(),
			FailureKind: connector.FailureKindAuthFailed,
			SafeReason:  "authentication failed",
		},
	}

	registry := connector.NewRegistry()
	registry.Register(domain.TypeUbuntuSSH, tester)

	txMgr := &fakeTxManager{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	svc := NewConnectionTestService(repo, vault, registry, txMgr, logger)

	resp, err := svc.TestConnection(ctx, orgID, resID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.Status != "failed" {
		t.Errorf("expected status 'failed', got: %s", resp.Status)
	}
	if resp.Details["reason"] != "authentication failed" {
		t.Errorf("expected details reason 'authentication failed', got: %+v", resp.Details)
	}

	// Verify DB state persistence: status becomes unreachable
	if repo.updateTestStateCalls != 1 {
		t.Fatalf("expected 1 update call to repo, got %d", repo.updateTestStateCalls)
	}
	if repo.lastUpdatedStatus != domain.ConnectionStatusFailed {
		t.Errorf("expected ConnectionStatusFailed, got: %s", repo.lastUpdatedStatus)
	}
	if repo.lastUpdatedError == nil || *repo.lastUpdatedError != "authentication failed" {
		t.Errorf("expected error 'authentication failed', got: %v", repo.lastUpdatedError)
	}
	if repo.lastUpdatedResourceStat != domain.StatusUnreachable {
		t.Errorf("expected StatusUnreachable, got: %s", repo.lastUpdatedResourceStat)
	}
}

func TestConnectionTestService_StatusTransitions_FullMatrix(t *testing.T) {
	ctx := context.Background()
	orgID := uuid.New()
	resID := uuid.New()
	credID := uuid.New()
	fp := "SHA256:validfingerprint"

	payloadBytes, _ := payload.EncodeV1("secret", nil)

	testCases := []struct {
		name           string
		initialStatus  domain.Status
		probeSuccess   bool
		expectedStatus domain.Status
	}{
		{"Active + success -> active", domain.StatusActive, true, domain.StatusActive},
		{"Active + failed -> unreachable", domain.StatusActive, false, domain.StatusUnreachable},
		{"Unreachable + success -> active", domain.StatusUnreachable, true, domain.StatusActive},
		{"Unreachable + failed -> unreachable", domain.StatusUnreachable, false, domain.StatusUnreachable},
		{"Error + success -> active", domain.StatusError, true, domain.StatusActive},
		{"Error + failed -> unreachable", domain.StatusError, false, domain.StatusUnreachable},
		{"Disabled + success -> disabled", domain.StatusDisabled, true, domain.StatusDisabled},
		{"Disabled + failed -> disabled", domain.StatusDisabled, false, domain.StatusDisabled},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakeConnectionTestRepo{
				resource: sampleUbuntuResource(orgID, resID, credID, tc.initialStatus, &fp),
			}
			vault := &fakeVaultReader{credType: credDomain.TypeSSHPassword, payloadBytes: payloadBytes}
			failureKind := connector.FailureKindNone
			if !tc.probeSuccess {
				failureKind = connector.FailureKindAuthFailed
			}
			tester := &fakeConnectionTester{
				result: &connector.ProbeResult{
					Success:     tc.probeSuccess,
					FailureKind: failureKind,
					CheckedAt:   time.Now().UTC(),
				},
			}
			registry := connector.NewRegistry()
			registry.Register(domain.TypeUbuntuSSH, tester)

			svc := NewConnectionTestService(repo, vault, registry, &fakeTxManager{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
			_, err := svc.TestConnection(ctx, orgID, resID)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if repo.lastUpdatedResourceStat != tc.expectedStatus {
				t.Errorf("expected status %s, got: %s", tc.expectedStatus, repo.lastUpdatedResourceStat)
			}
		})
	}
}

func TestConnectionTestService_UnknownFailureKind_503_NoStateMutation_NoLogLeak(t *testing.T) {
	ctx := context.Background()
	orgID := uuid.New()
	resID := uuid.New()
	credID := uuid.New()
	fp := "SHA256:fp"

	payloadBytes, _ := payload.EncodeV1("secret", nil)
	repo := &fakeConnectionTestRepo{
		resource: sampleUbuntuResource(orgID, resID, credID, domain.StatusActive, &fp),
	}
	vault := &fakeVaultReader{credType: credDomain.TypeSSHPassword, payloadBytes: payloadBytes}

	tester := &fakeConnectionTester{
		result: &connector.ProbeResult{
			Success:     false,
			FailureKind: connector.FailureKind("evil_unknown_kind"),
			SafeReason:  "password=SUPERSECRET token=ABC",
			CheckedAt:   time.Now().UTC(),
		},
	}
	registry := connector.NewRegistry()
	registry.Register(domain.TypeUbuntuSSH, tester)

	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))

	svc := NewConnectionTestService(repo, vault, registry, &fakeTxManager{}, logger)
	_, err := svc.TestConnection(ctx, orgID, resID)

	if !errors.Is(err, domain.ErrResourceServiceUnavailable) {
		t.Errorf("expected ErrResourceServiceUnavailable for unknown failure kind, got: %v", err)
	}

	if repo.updateTestStateCalls != 0 {
		t.Errorf("DB state must NOT be updated when unknown FailureKind is returned")
	}

	loggedText := logBuf.String()
	if strings.Contains(loggedText, "SUPERSECRET") || strings.Contains(loggedText, "token=ABC") || strings.Contains(loggedText, "evil_unknown_kind") {
		t.Errorf("SECURITY LEAK: raw safe_reason or unknown kind leaked into logs: %s", loggedText)
	}
}

func TestConnectionTestService_FailureKindNoneOnFailedProbe_503(t *testing.T) {
	ctx := context.Background()
	orgID := uuid.New()
	resID := uuid.New()
	credID := uuid.New()
	fp := "SHA256:fp"

	payloadBytes, _ := payload.EncodeV1("secret", nil)
	repo := &fakeConnectionTestRepo{
		resource: sampleUbuntuResource(orgID, resID, credID, domain.StatusActive, &fp),
	}
	vault := &fakeVaultReader{credType: credDomain.TypeSSHPassword, payloadBytes: payloadBytes}

	tester := &fakeConnectionTester{
		result: &connector.ProbeResult{
			Success:     false,
			FailureKind: connector.FailureKindNone,
			SafeReason:  "authentication failed",
			CheckedAt:   time.Now().UTC(),
		},
	}
	registry := connector.NewRegistry()
	registry.Register(domain.TypeUbuntuSSH, tester)

	svc := NewConnectionTestService(repo, vault, registry, &fakeTxManager{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	_, err := svc.TestConnection(ctx, orgID, resID)

	if !errors.Is(err, domain.ErrResourceServiceUnavailable) {
		t.Errorf("expected ErrResourceServiceUnavailable when failed probe has FailureKindNone, got: %v", err)
	}
	if repo.updateTestStateCalls != 0 {
		t.Errorf("DB state must NOT be updated")
	}
}

func TestConnectionTestService_InconsistentSuccess_503(t *testing.T) {
	ctx := context.Background()
	orgID := uuid.New()
	resID := uuid.New()
	credID := uuid.New()
	fp := "SHA256:fp"

	payloadBytes, _ := payload.EncodeV1("secret", nil)
	repo := &fakeConnectionTestRepo{
		resource: sampleUbuntuResource(orgID, resID, credID, domain.StatusActive, &fp),
	}
	vault := &fakeVaultReader{credType: credDomain.TypeSSHPassword, payloadBytes: payloadBytes}

	tester := &fakeConnectionTester{
		result: &connector.ProbeResult{
			Success:     true,
			FailureKind: connector.FailureKindAuthFailed,
			CheckedAt:   time.Now().UTC(),
		},
	}
	registry := connector.NewRegistry()
	registry.Register(domain.TypeUbuntuSSH, tester)

	svc := NewConnectionTestService(repo, vault, registry, &fakeTxManager{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	_, err := svc.TestConnection(ctx, orgID, resID)

	if !errors.Is(err, domain.ErrResourceServiceUnavailable) {
		t.Errorf("expected ErrResourceServiceUnavailable for success probe with FailureKind, got: %v", err)
	}
	if repo.updateTestStateCalls != 0 {
		t.Errorf("DB state must NOT be updated")
	}
}

func TestConnectionTestService_ZeroCheckedAt_503(t *testing.T) {
	ctx := context.Background()
	orgID := uuid.New()
	resID := uuid.New()
	credID := uuid.New()
	fp := "SHA256:fp"

	payloadBytes, _ := payload.EncodeV1("secret", nil)
	repo := &fakeConnectionTestRepo{
		resource: sampleUbuntuResource(orgID, resID, credID, domain.StatusActive, &fp),
	}
	vault := &fakeVaultReader{credType: credDomain.TypeSSHPassword, payloadBytes: payloadBytes}

	tester := &fakeConnectionTester{
		result: &connector.ProbeResult{
			Success:     true,
			FailureKind: connector.FailureKindNone,
			CheckedAt:   time.Time{}, // Zero timestamp
		},
	}
	registry := connector.NewRegistry()
	registry.Register(domain.TypeUbuntuSSH, tester)

	svc := NewConnectionTestService(repo, vault, registry, &fakeTxManager{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	_, err := svc.TestConnection(ctx, orgID, resID)

	if !errors.Is(err, domain.ErrResourceServiceUnavailable) {
		t.Errorf("expected ErrResourceServiceUnavailable for zero CheckedAt, got: %v", err)
	}
	if repo.updateTestStateCalls != 0 {
		t.Errorf("DB state must NOT be updated")
	}
}

func TestConnectionTestService_NegativeLatency_503(t *testing.T) {
	ctx := context.Background()
	orgID := uuid.New()
	resID := uuid.New()
	credID := uuid.New()
	fp := "SHA256:fp"

	payloadBytes, _ := payload.EncodeV1("secret", nil)
	repo := &fakeConnectionTestRepo{
		resource: sampleUbuntuResource(orgID, resID, credID, domain.StatusActive, &fp),
	}
	vault := &fakeVaultReader{credType: credDomain.TypeSSHPassword, payloadBytes: payloadBytes}

	tester := &fakeConnectionTester{
		result: &connector.ProbeResult{
			Success:     true,
			FailureKind: connector.FailureKindNone,
			CheckedAt:   time.Now().UTC(),
			Latency:     -1 * time.Millisecond,
		},
	}
	registry := connector.NewRegistry()
	registry.Register(domain.TypeUbuntuSSH, tester)

	svc := NewConnectionTestService(repo, vault, registry, &fakeTxManager{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	_, err := svc.TestConnection(ctx, orgID, resID)

	if !errors.Is(err, domain.ErrResourceServiceUnavailable) {
		t.Errorf("expected ErrResourceServiceUnavailable for negative latency, got: %v", err)
	}
	if repo.updateTestStateCalls != 0 {
		t.Errorf("DB state must NOT be updated")
	}
}

func TestConnectionTestService_CanonicalReason_IgnoresSafeReason(t *testing.T) {
	ctx := context.Background()
	orgID := uuid.New()
	resID := uuid.New()
	credID := uuid.New()
	fp := "SHA256:fp"

	payloadBytes, _ := payload.EncodeV1("secret", nil)
	repo := &fakeConnectionTestRepo{
		resource: sampleUbuntuResource(orgID, resID, credID, domain.StatusActive, &fp),
	}
	vault := &fakeVaultReader{credType: credDomain.TypeSSHPassword, payloadBytes: payloadBytes}

	tester := &fakeConnectionTester{
		result: &connector.ProbeResult{
			Success:     false,
			FailureKind: connector.FailureKindAuthFailed,
			SafeReason:  "THIS-MUST-NEVER-LEAK-OVER-API-OR-DB",
			CheckedAt:   time.Now().UTC(),
		},
	}
	registry := connector.NewRegistry()
	registry.Register(domain.TypeUbuntuSSH, tester)

	svc := NewConnectionTestService(repo, vault, registry, &fakeTxManager{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	resp, err := svc.TestConnection(ctx, orgID, resID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.Details["reason"] != "authentication failed" {
		t.Errorf("expected canonical reason 'authentication failed', got: %v", resp.Details["reason"])
	}
	if repo.lastUpdatedError == nil || *repo.lastUpdatedError != "authentication failed" {
		t.Errorf("expected DB last_connection_error 'authentication failed', got: %v", repo.lastUpdatedError)
	}
}

func TestConnectionTestService_ZeroizationBeforeTesterTest(t *testing.T) {
	ctx := context.Background()
	orgID := uuid.New()
	resID := uuid.New()
	credID := uuid.New()
	fp := "SHA256:fp"

	payloadBytes, _ := payload.EncodeV1("my-very-sensitive-password", nil)

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

	var sliceWasZeroedBeforeTester bool
	var secretValueInTester string

	tester := &fakeConnectionTester{
		result: &connector.ProbeResult{
			Success:     true,
			FailureKind: connector.FailureKindNone,
			CheckedAt:   time.Now().UTC(),
		},
		onTestCallback: func(target connector.Target, credPayload *payload.PayloadV1) {
			allZero := make([]byte, len(vaultReturnedSlice))
			sliceWasZeroedBeforeTester = bytes.Equal(vaultReturnedSlice, allZero)
			if credPayload != nil {
				secretValueInTester = credPayload.Secret
			}
		},
	}

	registry := connector.NewRegistry()
	registry.Register(domain.TypeUbuntuSSH, tester)

	svc := NewConnectionTestService(repo, customVault, registry, &fakeTxManager{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	_, err := svc.TestConnection(ctx, orgID, resID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !sliceWasZeroedBeforeTester {
		t.Errorf("SECURITY FLAW: Decrypted JSON bytes buffer was NOT zeroed before tester was invoked!")
	}
	if secretValueInTester != "my-very-sensitive-password" {
		t.Errorf("expected secret to be intact during tester call, got: %q", secretValueInTester)
	}
}

func TestConnectionTestService_CPanel_PreflightUsernameValidation_NoDecrypt(t *testing.T) {
	ctx := context.Background()
	orgID := uuid.New()
	resID := uuid.New()
	credID := uuid.New()
	useHTTPS := true

	invalidUsernames := []string{
		"MyCPanel",     // uppercase
		"user:name",    // colon
		"user\nname",   // newline
		"user\rname",   // CR
		"user\x00name", // NUL
		"",             // empty
	}

	for _, u := range invalidUsernames {
		t.Run("Username "+u, func(t *testing.T) {
			repo := &fakeConnectionTestRepo{
				resource: sampleCPanelResource(orgID, resID, credID, domain.StatusActive, u, &useHTTPS),
			}
			vault := &fakeVaultReader{err: errors.New("vault must not be called")}
			tester := &fakeConnectionTester{}
			registry := connector.NewRegistry()
			registry.Register(domain.TypeCPanel, tester)

			svc := NewConnectionTestService(repo, vault, registry, &fakeTxManager{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
			_, err := svc.TestConnection(ctx, orgID, resID)

			if !errors.Is(err, domain.ErrInvalidConnectorConfig) {
				t.Errorf("expected ErrInvalidConnectorConfig for username %q, got: %v", u, err)
			}
			if vault.callCount != 0 {
				t.Errorf("vault should not be called when username is invalid")
			}
			if tester.callCount != 0 {
				t.Errorf("tester should not be called when username is invalid")
			}
			if repo.updateTestStateCalls != 0 {
				t.Errorf("DB state should not be updated")
			}
		})
	}
}

func TestConnectionTestService_TesterInternalCancellation_503(t *testing.T) {
	ctx := context.Background() // Active parent context
	orgID := uuid.New()
	resID := uuid.New()
	credID := uuid.New()
	fp := "SHA256:fp"

	payloadBytes, _ := payload.EncodeV1("pass", nil)
	repo := &fakeConnectionTestRepo{
		resource: sampleUbuntuResource(orgID, resID, credID, domain.StatusActive, &fp),
	}
	vault := &fakeVaultReader{credType: credDomain.TypeSSHPassword, payloadBytes: payloadBytes}

	tester := &fakeConnectionTester{
		err: context.Canceled, // Tester internally returned Canceled while parent ctx is alive
	}
	registry := connector.NewRegistry()
	registry.Register(domain.TypeUbuntuSSH, tester)

	svc := NewConnectionTestService(repo, vault, registry, &fakeTxManager{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	_, err := svc.TestConnection(ctx, orgID, resID)

	if err == nil {
		t.Fatalf("expected error on internal cancellation")
	}
	if !errors.Is(err, domain.ErrResourceServiceUnavailable) {
		t.Errorf("expected ErrResourceServiceUnavailable for internal cancellation, got: %v", err)
	}
	if repo.updateTestStateCalls != 0 {
		t.Errorf("DB state must NOT be mutated on internal cancellation")
	}
}

func TestConnectionTestService_TesterInternalDeadlineExceeded_503(t *testing.T) {
	ctx := context.Background() // Active parent context
	orgID := uuid.New()
	resID := uuid.New()
	credID := uuid.New()
	fp := "SHA256:fp"

	payloadBytes, _ := payload.EncodeV1("pass", nil)
	repo := &fakeConnectionTestRepo{
		resource: sampleUbuntuResource(orgID, resID, credID, domain.StatusActive, &fp),
	}
	vault := &fakeVaultReader{credType: credDomain.TypeSSHPassword, payloadBytes: payloadBytes}

	tester := &fakeConnectionTester{
		err: context.DeadlineExceeded,
	}
	registry := connector.NewRegistry()
	registry.Register(domain.TypeUbuntuSSH, tester)

	svc := NewConnectionTestService(repo, vault, registry, &fakeTxManager{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	_, err := svc.TestConnection(ctx, orgID, resID)

	if !errors.Is(err, domain.ErrResourceServiceUnavailable) {
		t.Errorf("expected ErrResourceServiceUnavailable for internal deadline exceeded, got: %v", err)
	}
	if repo.updateTestStateCalls != 0 {
		t.Errorf("DB state must NOT be mutated")
	}
}

func TestConnectionTestService_LocallyDerivedAuthMethod_IgnoresTester(t *testing.T) {
	ctx := context.Background()
	orgID := uuid.New()
	resID := uuid.New()
	credID := uuid.New()
	fp := "SHA256:fp"

	payloadBytes, _ := payload.EncodeV1("secret", nil)
	repo := &fakeConnectionTestRepo{
		resource: sampleUbuntuResource(orgID, resID, credID, domain.StatusActive, &fp),
	}
	vault := &fakeVaultReader{credType: credDomain.TypeSSHPassword, payloadBytes: payloadBytes}

	tester := &fakeConnectionTester{
		result: &connector.ProbeResult{
			Success:     true,
			FailureKind: connector.FailureKindNone,
			CheckedAt:   time.Now().UTC(),
			Details: map[string]any{
				"auth_method": "password:SUPERSECRET",
			},
		},
	}
	registry := connector.NewRegistry()
	registry.Register(domain.TypeUbuntuSSH, tester)

	svc := NewConnectionTestService(repo, vault, registry, &fakeTxManager{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	resp, err := svc.TestConnection(ctx, orgID, resID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.Details["auth_method"] != "password" {
		t.Errorf("expected locally derived 'password', got: %v", resp.Details["auth_method"])
	}
}

func TestConnectionTestService_CPanel_APIVersionValidation(t *testing.T) {
	ctx := context.Background()
	orgID := uuid.New()
	resID := uuid.New()
	credID := uuid.New()
	useHTTPS := true

	payloadBytes, _ := payload.EncodeV1("token", nil)

	testCases := []struct {
		name        string
		details     map[string]any
		expectValid bool
	}{
		{"Valid api_version 3", map[string]any{"api_version": 3}, true},
		{"Missing api_version", map[string]any{}, false},
		{"String api_version", map[string]any{"api_version": "3"}, false},
		{"Zero api_version", map[string]any{"api_version": 0}, false},
		{"Negative api_version", map[string]any{"api_version": -1}, false},
		{"Nil details map", nil, false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakeConnectionTestRepo{
				resource: sampleCPanelResource(orgID, resID, credID, domain.StatusActive, "mycpanel", &useHTTPS),
			}
			vault := &fakeVaultReader{credType: credDomain.TypeCPanelAPIToken, payloadBytes: payloadBytes}
			tester := &fakeConnectionTester{
				result: &connector.ProbeResult{
					Success:     true,
					FailureKind: connector.FailureKindNone,
					CheckedAt:   time.Now().UTC(),
					Details:     tc.details,
				},
			}
			registry := connector.NewRegistry()
			registry.Register(domain.TypeCPanel, tester)

			svc := NewConnectionTestService(repo, vault, registry, &fakeTxManager{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
			resp, err := svc.TestConnection(ctx, orgID, resID)

			if tc.expectValid {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if resp.Details["api_version"] != 3 {
					t.Errorf("expected api_version 3, got: %v", resp.Details["api_version"])
				}
				if repo.updateTestStateCalls != 1 {
					t.Errorf("expected 1 DB update call")
				}
			} else {
				if !errors.Is(err, domain.ErrResourceServiceUnavailable) {
					t.Errorf("expected ErrResourceServiceUnavailable, got: %v", err)
				}
				if repo.updateTestStateCalls != 0 {
					t.Errorf("DB state must NOT be updated on invalid api_version")
				}
			}
		})
	}
}

func TestConnectionTestService_SSHOptionalBannerSanitization(t *testing.T) {
	ctx := context.Background()
	orgID := uuid.New()
	resID := uuid.New()
	credID := uuid.New()
	fp := "SHA256:fp"

	payloadBytes, _ := payload.EncodeV1("secret", nil)

	t.Run("Sanitizes banner with control characters", func(t *testing.T) {
		repo := &fakeConnectionTestRepo{
			resource: sampleUbuntuResource(orgID, resID, credID, domain.StatusActive, &fp),
		}
		vault := &fakeVaultReader{credType: credDomain.TypeSSHPassword, payloadBytes: payloadBytes}
		tester := &fakeConnectionTester{
			result: &connector.ProbeResult{
				Success:     true,
				FailureKind: connector.FailureKindNone,
				CheckedAt:   time.Now().UTC(),
				Details: map[string]any{
					"server_banner": "  \x00SSH-2.0-OpenSSH_8.9\r\n  ",
				},
			},
		}
		registry := connector.NewRegistry()
		registry.Register(domain.TypeUbuntuSSH, tester)

		svc := NewConnectionTestService(repo, vault, registry, &fakeTxManager{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
		resp, err := svc.TestConnection(ctx, orgID, resID)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if resp.Details["server_banner"] != "SSH-2.0-OpenSSH_8.9" {
			t.Errorf("expected cleaned banner 'SSH-2.0-OpenSSH_8.9', got: %q", resp.Details["server_banner"])
		}
	})

	t.Run("Non-string banner is omitted without failing probe", func(t *testing.T) {
		repo := &fakeConnectionTestRepo{
			resource: sampleUbuntuResource(orgID, resID, credID, domain.StatusActive, &fp),
		}
		vault := &fakeVaultReader{credType: credDomain.TypeSSHPassword, payloadBytes: payloadBytes}
		tester := &fakeConnectionTester{
			result: &connector.ProbeResult{
				Success:     true,
				FailureKind: connector.FailureKindNone,
				CheckedAt:   time.Now().UTC(),
				Details: map[string]any{
					"server_banner": 12345, // invalid type
				},
			},
		}
		registry := connector.NewRegistry()
		registry.Register(domain.TypeUbuntuSSH, tester)

		svc := NewConnectionTestService(repo, vault, registry, &fakeTxManager{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
		resp, err := svc.TestConnection(ctx, orgID, resID)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if _, exists := resp.Details["server_banner"]; exists {
			t.Errorf("expected server_banner to be omitted when non-string")
		}
	})
}

func TestConnectionTestService_PayloadReferenceCleanup(t *testing.T) {
	ctx := context.Background()
	orgID := uuid.New()
	resID := uuid.New()
	credID := uuid.New()
	fp := "SHA256:fp"

	pass := "my-passphrase"
	payloadBytes, _ := payload.EncodeV1("my-secret", &pass)

	t.Run("Decoded payload string references are cleared after success", func(t *testing.T) {
		repo := &fakeConnectionTestRepo{
			resource: sampleUbuntuResource(orgID, resID, credID, domain.StatusActive, &fp),
		}
		vault := &fakeVaultReader{credType: credDomain.TypeSSHPassword, payloadBytes: payloadBytes}
		tester := &fakeConnectionTester{
			result: &connector.ProbeResult{
				Success:     true,
				FailureKind: connector.FailureKindNone,
				CheckedAt:   time.Now().UTC(),
			},
		}
		registry := connector.NewRegistry()
		registry.Register(domain.TypeUbuntuSSH, tester)

		svc := NewConnectionTestService(repo, vault, registry, &fakeTxManager{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
		_, _ = svc.TestConnection(ctx, orgID, resID)

		if tester.lastPayload == nil {
			t.Fatalf("expected tester to have received payload")
		}
		if tester.lastPayload.Secret != "" {
			t.Errorf("expected secret reference to be cleared, got: %q", tester.lastPayload.Secret)
		}
		if tester.lastPayload.Passphrase != nil {
			t.Errorf("expected passphrase reference to be cleared, got: %v", tester.lastPayload.Passphrase)
		}
	})

	t.Run("Decoded payload string references are cleared after remote failure", func(t *testing.T) {
		repo := &fakeConnectionTestRepo{
			resource: sampleUbuntuResource(orgID, resID, credID, domain.StatusActive, &fp),
		}
		vault := &fakeVaultReader{credType: credDomain.TypeSSHPassword, payloadBytes: payloadBytes}
		tester := &fakeConnectionTester{
			result: &connector.ProbeResult{
				Success:     false,
				FailureKind: connector.FailureKindAuthFailed,
				SafeReason:  "failed",
				CheckedAt:   time.Now().UTC(),
			},
		}
		registry := connector.NewRegistry()
		registry.Register(domain.TypeUbuntuSSH, tester)

		svc := NewConnectionTestService(repo, vault, registry, &fakeTxManager{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
		_, _ = svc.TestConnection(ctx, orgID, resID)

		if tester.lastPayload.Secret != "" {
			t.Errorf("expected secret reference to be cleared after failure, got: %q", tester.lastPayload.Secret)
		}
		if tester.lastPayload.Passphrase != nil {
			t.Errorf("expected passphrase reference to be cleared after failure, got: %v", tester.lastPayload.Passphrase)
		}
	})
}

func TestConnectionTestService_DetailsAllowlisting_DropsInjectedFields(t *testing.T) {
	ctx := context.Background()
	orgID := uuid.New()
	resID := uuid.New()
	credID := uuid.New()
	fp := "SHA256:fp"

	payloadBytes, _ := payload.EncodeV1("secret", nil)
	repo := &fakeConnectionTestRepo{
		resource: sampleUbuntuResource(orgID, resID, credID, domain.StatusActive, &fp),
	}
	vault := &fakeVaultReader{credType: credDomain.TypeSSHPassword, payloadBytes: payloadBytes}

	tester := &fakeConnectionTester{
		result: &connector.ProbeResult{
			Success:     true,
			FailureKind: connector.FailureKindNone,
			CheckedAt:   time.Now().UTC(),
			Details: map[string]any{
				"server_banner": "SSH-2.0-OpenSSH",
				"auth_method":   "password",
				"secret":        "SHOULD-NOT-LEAK",
				"password":      "plaintext",
				"unexpected":    12345,
			},
		},
	}
	registry := connector.NewRegistry()
	registry.Register(domain.TypeUbuntuSSH, tester)

	svc := NewConnectionTestService(repo, vault, registry, &fakeTxManager{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	resp, err := svc.TestConnection(ctx, orgID, resID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.Details["server_banner"] != "SSH-2.0-OpenSSH" {
		t.Errorf("expected server_banner in details")
	}
	if resp.Details["auth_method"] != "password" {
		t.Errorf("expected auth_method in details")
	}
	if _, exists := resp.Details["secret"]; exists {
		t.Errorf("SECURITY LEAK: 'secret' was forwarded in response details")
	}
	if _, exists := resp.Details["password"]; exists {
		t.Errorf("SECURITY LEAK: 'password' was forwarded in response details")
	}
	if _, exists := resp.Details["unexpected"]; exists {
		t.Errorf("unallowlisted field 'unexpected' was forwarded in details")
	}
}

func TestConnectionTestService_CPanel_SuccessAndDetails(t *testing.T) {
	ctx := context.Background()
	orgID := uuid.New()
	resID := uuid.New()
	credID := uuid.New()
	useHTTPS := true

	payloadBytes, _ := payload.EncodeV1("my-token", nil)
	repo := &fakeConnectionTestRepo{
		resource: sampleCPanelResource(orgID, resID, credID, domain.StatusActive, "mycpanel", &useHTTPS),
	}
	vault := &fakeVaultReader{credType: credDomain.TypeCPanelAPIToken, payloadBytes: payloadBytes}

	tester := &fakeConnectionTester{
		result: &connector.ProbeResult{
			Success:     true,
			FailureKind: connector.FailureKindNone,
			Latency:     30 * time.Millisecond,
			CheckedAt:   time.Now().UTC(),
			Details: map[string]any{
				"auth_method": "api_token",
				"api_version": 3,
				"leaked":      "drop-me",
			},
		},
	}
	registry := connector.NewRegistry()
	registry.Register(domain.TypeCPanel, tester)

	svc := NewConnectionTestService(repo, vault, registry, &fakeTxManager{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	resp, err := svc.TestConnection(ctx, orgID, resID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.Status != "success" {
		t.Errorf("expected status 'success', got: %s", resp.Status)
	}
	if resp.Details["auth_method"] != "api_token" {
		t.Errorf("expected auth_method 'api_token', got: %v", resp.Details["auth_method"])
	}
	if resp.Details["api_version"] != 3 {
		t.Errorf("expected api_version 3, got: %v", resp.Details["api_version"])
	}
	if _, exists := resp.Details["leaked"]; exists {
		t.Errorf("unallowlisted field 'leaked' was forwarded in details")
	}
}

func TestConnectionTestService_ParentCancellation_ZeroDBUpdates(t *testing.T) {
	orgID := uuid.New()
	resID := uuid.New()
	credID := uuid.New()
	fp := "SHA256:fp"

	payloadBytes, _ := payload.EncodeV1("pass", nil)
	repo := &fakeConnectionTestRepo{
		resource: sampleUbuntuResource(orgID, resID, credID, domain.StatusActive, &fp),
	}
	vault := &fakeVaultReader{credType: credDomain.TypeSSHPassword, payloadBytes: payloadBytes}

	tester := &fakeConnectionTester{
		err: context.Canceled,
	}
	registry := connector.NewRegistry()
	registry.Register(domain.TypeUbuntuSSH, tester)

	svc := NewConnectionTestService(repo, vault, registry, &fakeTxManager{}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel parent context

	_, err := svc.TestConnection(canceledCtx, orgID, resID)
	if err == nil {
		t.Fatalf("expected error on canceled context")
	}

	if repo.updateTestStateCalls != 0 {
		t.Errorf("DB state must NOT be mutated when parent context is canceled")
	}
}

func TestConnectionTestService_MalformedPayloadDecode_503(t *testing.T) {
	ctx := context.Background()
	orgID := uuid.New()
	resID := uuid.New()
	credID := uuid.New()
	fp := "SHA256:fp"

	malformedBytes := []byte(`{not valid json`)
	repo := &fakeConnectionTestRepo{
		resource: sampleUbuntuResource(orgID, resID, credID, domain.StatusActive, &fp),
	}
	vault := &fakeVaultReader{credType: credDomain.TypeSSHPassword, payloadBytes: malformedBytes}
	tester := &fakeConnectionTester{}
	registry := connector.NewRegistry()
	registry.Register(domain.TypeUbuntuSSH, tester)

	svc := NewConnectionTestService(repo, vault, registry, &fakeTxManager{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	_, err := svc.TestConnection(ctx, orgID, resID)

	if !errors.Is(err, domain.ErrResourceServiceUnavailable) {
		t.Errorf("expected ErrResourceServiceUnavailable for malformed payload, got: %v", err)
	}
	if tester.callCount != 0 {
		t.Errorf("tester should not be called")
	}
	if repo.updateTestStateCalls != 0 {
		t.Errorf("DB state should not be updated")
	}
}

func TestConnectionTestService_UnsupportedPayloadVersion_503(t *testing.T) {
	ctx := context.Background()
	orgID := uuid.New()
	resID := uuid.New()
	credID := uuid.New()
	fp := "SHA256:fp"

	unsupportedVersionBytes := []byte(`{"version":2,"secret":"some-secret"}`)
	repo := &fakeConnectionTestRepo{
		resource: sampleUbuntuResource(orgID, resID, credID, domain.StatusActive, &fp),
	}
	vault := &fakeVaultReader{credType: credDomain.TypeSSHPassword, payloadBytes: unsupportedVersionBytes}
	tester := &fakeConnectionTester{}
	registry := connector.NewRegistry()
	registry.Register(domain.TypeUbuntuSSH, tester)

	svc := NewConnectionTestService(repo, vault, registry, &fakeTxManager{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	_, err := svc.TestConnection(ctx, orgID, resID)

	if !errors.Is(err, domain.ErrResourceServiceUnavailable) {
		t.Errorf("expected ErrResourceServiceUnavailable for version 2, got: %v", err)
	}
	if tester.callCount != 0 {
		t.Errorf("tester should not be called")
	}
	if repo.updateTestStateCalls != 0 {
		t.Errorf("DB state should not be updated")
	}
}

func TestConnectionTestService_RegistryTesterMissing_503(t *testing.T) {
	ctx := context.Background()
	orgID := uuid.New()
	resID := uuid.New()
	credID := uuid.New()
	fp := "SHA256:fp"

	repo := &fakeConnectionTestRepo{
		resource: sampleUbuntuResource(orgID, resID, credID, domain.StatusActive, &fp),
	}
	vault := &fakeVaultReader{err: errors.New("vault should not be called")}
	emptyRegistry := connector.NewRegistry()

	svc := NewConnectionTestService(repo, vault, emptyRegistry, &fakeTxManager{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	_, err := svc.TestConnection(ctx, orgID, resID)

	if !errors.Is(err, domain.ErrResourceServiceUnavailable) {
		t.Errorf("expected ErrResourceServiceUnavailable when tester is missing in registry, got: %v", err)
	}
	if vault.callCount != 0 {
		t.Errorf("vault should not be called")
	}
	if repo.updateTestStateCalls != 0 {
		t.Errorf("DB state should not be updated")
	}
}

func TestConnectionTestService_PersistenceFailure_503(t *testing.T) {
	ctx := context.Background()
	orgID := uuid.New()
	resID := uuid.New()
	credID := uuid.New()
	fp := "SHA256:fp"

	payloadBytes, _ := payload.EncodeV1("secret", nil)
	repo := &fakeConnectionTestRepo{
		resource:           sampleUbuntuResource(orgID, resID, credID, domain.StatusActive, &fp),
		updateTestStateErr: errors.New("database connection lost"),
	}
	vault := &fakeVaultReader{credType: credDomain.TypeSSHPassword, payloadBytes: payloadBytes}
	tester := &fakeConnectionTester{
		result: &connector.ProbeResult{
			Success:     true,
			FailureKind: connector.FailureKindNone,
			CheckedAt:   time.Now().UTC(),
		},
	}
	registry := connector.NewRegistry()
	registry.Register(domain.TypeUbuntuSSH, tester)

	svc := NewConnectionTestService(repo, vault, registry, &fakeTxManager{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	_, err := svc.TestConnection(ctx, orgID, resID)

	if !errors.Is(err, domain.ErrResourceServiceUnavailable) {
		t.Errorf("expected ErrResourceServiceUnavailable on DB update error, got: %v", err)
	}
}

func TestConnectionTestService_PersistenceNotFound_404(t *testing.T) {
	ctx := context.Background()
	orgID := uuid.New()
	resID := uuid.New()
	credID := uuid.New()
	fp := "SHA256:fp"

	payloadBytes, _ := payload.EncodeV1("secret", nil)
	repo := &fakeConnectionTestRepo{
		resource:           sampleUbuntuResource(orgID, resID, credID, domain.StatusActive, &fp),
		updateTestStateErr: domain.ErrResourceNotFound,
	}
	vault := &fakeVaultReader{credType: credDomain.TypeSSHPassword, payloadBytes: payloadBytes}
	tester := &fakeConnectionTester{
		result: &connector.ProbeResult{
			Success:     true,
			FailureKind: connector.FailureKindNone,
			CheckedAt:   time.Now().UTC(),
		},
	}
	registry := connector.NewRegistry()
	registry.Register(domain.TypeUbuntuSSH, tester)

	svc := NewConnectionTestService(repo, vault, registry, &fakeTxManager{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	_, err := svc.TestConnection(ctx, orgID, resID)

	if !errors.Is(err, domain.ErrResourceNotFound) {
		t.Errorf("expected ErrResourceNotFound on DB concurrent deletion, got: %v", err)
	}
}

func TestConnectionTestService_ZeroizationOfDecryptedPayload_OnAllPaths(t *testing.T) {
	ctx := context.Background()
	orgID := uuid.New()
	resID := uuid.New()
	credID := uuid.New()
	fp := "SHA256:fp"

	payloadBytes, _ := payload.EncodeV1("my-very-sensitive-password", nil)

	t.Run("Zeroed on DB update failure", func(t *testing.T) {
		var vaultReturnedSlice []byte
		customVault := &vaultReaderFunc{
			fn: func(ctx context.Context, o, c uuid.UUID) (credDomain.Type, []byte, error) {
				vaultReturnedSlice = make([]byte, len(payloadBytes))
				copy(vaultReturnedSlice, payloadBytes)
				return credDomain.TypeSSHPassword, vaultReturnedSlice, nil
			},
		}

		repo := &fakeConnectionTestRepo{
			resource:           sampleUbuntuResource(orgID, resID, credID, domain.StatusActive, &fp),
			updateTestStateErr: errors.New("db error"),
		}

		tester := &fakeConnectionTester{
			result: &connector.ProbeResult{
				Success:     true,
				FailureKind: connector.FailureKindNone,
				CheckedAt:   time.Now().UTC(),
			},
		}
		registry := connector.NewRegistry()
		registry.Register(domain.TypeUbuntuSSH, tester)

		svc := NewConnectionTestService(repo, customVault, registry, &fakeTxManager{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
		_, _ = svc.TestConnection(ctx, orgID, resID)

		allZero := make([]byte, len(vaultReturnedSlice))
		if !bytes.Equal(vaultReturnedSlice, allZero) {
			t.Errorf("SECURITY FLAW: Service did not zero the decrypted payload buffer after persistence failure")
		}
	})
}

func TestConnectionTestService_RawErrorLoggingRegression(t *testing.T) {
	ctx := context.Background()
	orgID := uuid.New()
	resID := uuid.New()
	credID := uuid.New()
	fp := "SHA256:fp"

	payloadBytes, _ := payload.EncodeV1("secret", nil)
	repo := &fakeConnectionTestRepo{
		resource: sampleUbuntuResource(orgID, resID, credID, domain.StatusActive, &fp),
	}
	vault := &fakeVaultReader{credType: credDomain.TypeSSHPassword, payloadBytes: payloadBytes}

	tester := &fakeConnectionTester{
		err: errors.New("password=SUPERSECRET token=ABC raw-driver-error"),
	}
	registry := connector.NewRegistry()
	registry.Register(domain.TypeUbuntuSSH, tester)

	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))

	svc := NewConnectionTestService(repo, vault, registry, &fakeTxManager{}, logger)
	_, err := svc.TestConnection(ctx, orgID, resID)

	if !errors.Is(err, domain.ErrResourceServiceUnavailable) {
		t.Errorf("expected ErrResourceServiceUnavailable, got: %v", err)
	}

	loggedText := logBuf.String()
	if strings.Contains(loggedText, "SUPERSECRET") || strings.Contains(loggedText, "token=ABC") || strings.Contains(loggedText, "raw-driver-error") {
		t.Errorf("SECURITY FLAW: Raw error details or credentials leaked into logs: %s", loggedText)
	}
}

type vaultReaderFunc struct {
	fn func(ctx context.Context, orgID, credID uuid.UUID) (credDomain.Type, []byte, error)
}

func (v *vaultReaderFunc) LoadCredentialForUse(ctx context.Context, orgID, credID uuid.UUID) (credDomain.Type, []byte, error) {
	return v.fn(ctx, orgID, credID)
}

func TestZeroBytes_RegressionCheck(t *testing.T) {
	b := []byte("secret")
	secretcrypto.ZeroBytes(b)
	for i, v := range b {
		if v != 0 {
			t.Errorf("byte at %d was not zero: %d", i, v)
		}
	}
}
