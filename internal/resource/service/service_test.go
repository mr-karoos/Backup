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

	credDomain "backup-platform/internal/credential/domain"
	"backup-platform/internal/platform/database"
	"backup-platform/internal/resource/domain"
	"backup-platform/pkg/uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type fakeQuerier struct{}

func (f *fakeQuerier) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}

func (f *fakeQuerier) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return nil, nil
}

func (f *fakeQuerier) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return nil
}

type fakeTxManager struct {
	repoSnapshot *fakeRepo
}

func (f *fakeTxManager) Querier() database.Querier {
	return &fakeQuerier{}
}

func (f *fakeTxManager) WithinTx(ctx context.Context, fn func(tx database.Querier) error) error {
	// Take snapshot before transaction
	if f.repoSnapshot != nil {
		f.repoSnapshot.snapshot()
	}
	err := fn(&fakeQuerier{})
	if err != nil {
		// Rollback snapshot
		if f.repoSnapshot != nil {
			f.repoSnapshot.rollback()
		}
		return err
	}
	return nil
}

type fakeRepo struct {
	resources       map[string]*domain.Resource
	connectors      map[string]*domain.ResourceConnector
	credentials     map[string]*credDomain.CredentialMetadata
	createConnErr   error
	updateConnErr   error
	createResErr    error
	updateResErr    error
	findErr         error
	listErr         error
	archiveErr      error
	savedResources  map[string]*domain.Resource
	savedConnectors map[string]*domain.ResourceConnector
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		resources:   make(map[string]*domain.Resource),
		connectors:  make(map[string]*domain.ResourceConnector),
		credentials: make(map[string]*credDomain.CredentialMetadata),
	}
}

func (r *fakeRepo) snapshot() {
	r.savedResources = make(map[string]*domain.Resource)
	for k, v := range r.resources {
		cp := *v
		r.savedResources[k] = &cp
	}
	r.savedConnectors = make(map[string]*domain.ResourceConnector)
	for k, v := range r.connectors {
		cp := *v
		r.savedConnectors[k] = &cp
	}
}

func (r *fakeRepo) rollback() {
	r.resources = r.savedResources
	r.connectors = r.savedConnectors
}

func (r *fakeRepo) CreateResource(ctx context.Context, q database.Querier, res *domain.Resource) error {
	if r.createResErr != nil {
		return r.createResErr
	}
	r.resources[res.ID.String()] = res
	return nil
}

func (r *fakeRepo) CreateConnector(ctx context.Context, q database.Querier, conn *domain.ResourceConnector) error {
	if r.createConnErr != nil {
		return r.createConnErr
	}
	r.connectors[conn.ResourceID.String()] = conn
	return nil
}

func (r *fakeRepo) UpdateResource(ctx context.Context, q database.Querier, res *domain.Resource) error {
	if r.updateResErr != nil {
		return r.updateResErr
	}
	existing, ok := r.resources[res.ID.String()]
	if !ok || existing.OrganizationID != res.OrganizationID || existing.Status == domain.StatusArchived {
		return domain.ErrResourceNotFound
	}
	r.resources[res.ID.String()] = res
	return nil
}

func (r *fakeRepo) UpdateConnector(ctx context.Context, q database.Querier, conn *domain.ResourceConnector) error {
	if r.updateConnErr != nil {
		return r.updateConnErr
	}
	existing, ok := r.connectors[conn.ResourceID.String()]
	if !ok || existing.OrganizationID != conn.OrganizationID {
		return domain.ErrResourceNotFound
	}
	r.connectors[conn.ResourceID.String()] = conn
	return nil
}

func (r *fakeRepo) FindByIDForOrganization(ctx context.Context, q database.Querier, orgID, resID uuid.UUID) (*domain.ResourceWithConnector, error) {
	if r.findErr != nil {
		return nil, r.findErr
	}
	res, ok := r.resources[resID.String()]
	if !ok || res.OrganizationID != orgID || res.Status == domain.StatusArchived {
		return nil, domain.ErrResourceNotFound
	}
	conn, ok := r.connectors[resID.String()]
	if !ok {
		return nil, domain.ErrResourceNotFound
	}
	cred, ok := r.credentials[conn.CredentialID.String()]
	credName := "Mock Credential"
	var credFP *string
	if ok {
		credName = cred.Name
		credFP = cred.Fingerprint
	}
	return &domain.ResourceWithConnector{
		Resource:              res,
		Connector:             conn,
		CredentialName:        credName,
		CredentialFingerprint: credFP,
	}, nil
}

func (r *fakeRepo) ListForOrganization(ctx context.Context, q database.Querier, orgID uuid.UUID) ([]*domain.ResourceWithConnector, error) {
	if r.listErr != nil {
		return nil, r.listErr
	}
	resList := make([]*domain.ResourceWithConnector, 0)
	for _, res := range r.resources {
		if res.OrganizationID == orgID && res.Status != domain.StatusArchived {
			conn := r.connectors[res.ID.String()]
			cred := r.credentials[conn.CredentialID.String()]
			credName := "Mock Credential"
			var credFP *string
			if cred != nil {
				credName = cred.Name
				credFP = cred.Fingerprint
			}
			resList = append(resList, &domain.ResourceWithConnector{
				Resource:              res,
				Connector:             conn,
				CredentialName:        credName,
				CredentialFingerprint: credFP,
			})
		}
	}
	return resList, nil
}

func (r *fakeRepo) ArchiveForOrganization(ctx context.Context, q database.Querier, orgID, resID uuid.UUID) error {
	if r.archiveErr != nil {
		return r.archiveErr
	}
	res, ok := r.resources[resID.String()]
	if !ok || res.OrganizationID != orgID {
		return domain.ErrResourceNotFound
	}
	res.Status = domain.StatusArchived
	return nil
}

func (r *fakeRepo) UpdateConnectionTestStateForOrganization(
	ctx context.Context,
	q database.Querier,
	orgID, resID uuid.UUID,
	lastTestAt time.Time,
	lastStatus domain.ConnectionStatus,
	lastError *string,
	newResourceStatus domain.Status,
) error {
	res, ok := r.resources[resID.String()]
	if !ok || res.OrganizationID != orgID || res.Status == domain.StatusArchived {
		return domain.ErrResourceNotFound
	}
	res.LastConnectionTestAt = &lastTestAt
	res.LastConnectionStatus = &lastStatus
	res.LastConnectionError = lastError
	res.Status = newResourceStatus
	return nil
}

func (r *fakeRepo) FindMetadataForOrganization(ctx context.Context, q database.Querier, orgID, credID uuid.UUID) (*credDomain.CredentialMetadata, error) {
	cred, ok := r.credentials[credID.String()]
	if !ok || cred.OrganizationID != orgID {
		return nil, credDomain.ErrCredentialNotFound
	}
	return cred, nil
}

func TestResourceService_CreateResource(t *testing.T) {
	ctx := context.Background()
	orgA := uuid.New()
	orgB := uuid.New()
	credSSH := uuid.New()
	credPass := uuid.New()
	credToken := uuid.New()
	credCPPass := uuid.New()
	credCrossTenant := uuid.New()

	repo := newFakeRepo()
	repo.credentials[credSSH.String()] = &credDomain.CredentialMetadata{
		ID:             credSSH,
		OrganizationID: orgA,
		Name:           "Org A SSH Key",
		Type:           credDomain.TypeSSHPrivateKey,
	}
	repo.credentials[credPass.String()] = &credDomain.CredentialMetadata{
		ID:             credPass,
		OrganizationID: orgA,
		Name:           "Org A SSH Password",
		Type:           credDomain.TypeSSHPassword,
	}
	repo.credentials[credToken.String()] = &credDomain.CredentialMetadata{
		ID:             credToken,
		OrganizationID: orgA,
		Name:           "Org A cPanel Token",
		Type:           credDomain.TypeCPanelAPIToken,
	}
	repo.credentials[credCPPass.String()] = &credDomain.CredentialMetadata{
		ID:             credCPPass,
		OrganizationID: orgA,
		Name:           "Org A cPanel Password",
		Type:           credDomain.TypeCPanelPassword,
	}
	repo.credentials[credCrossTenant.String()] = &credDomain.CredentialMetadata{
		ID:             credCrossTenant,
		OrganizationID: orgB,
		Name:           "Org B Secret",
		Type:           credDomain.TypeSSHPrivateKey,
	}

	txMgr := &fakeTxManager{repoSnapshot: repo}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := NewService(repo, repo, txMgr, logger)

	t.Run("successfully creates Ubuntu SSH resource with SSH key", func(t *testing.T) {
		fp := "SHA256:abc1234567890"
		timeout := 15
		input := CreateResourceInput{
			Name: "Production Web 1",
			Type: domain.TypeUbuntuSSH,
			Connector: CreateConnectorInput{
				Host:               "192.168.1.100",
				Port:               22,
				AuthType:           domain.AuthTypeSSHKey,
				Username:           "ubuntu",
				CredentialID:       credSSH,
				HostKeyFingerprint: &fp,
				ConnectionTimeout:  &timeout,
			},
		}

		resWithConn, err := svc.CreateResource(ctx, orgA, input)
		if err != nil {
			t.Fatalf("expected success, got: %v", err)
		}
		if resWithConn.Resource.Status != domain.StatusActive {
			t.Errorf("expected status active, got %s", resWithConn.Resource.Status)
		}
		if resWithConn.Resource.Type != domain.TypeUbuntuSSH {
			t.Errorf("expected type ubuntu_ssh, got %s", resWithConn.Resource.Type)
		}
		if resWithConn.Connector.ConnectorType != domain.ConnectorTypeUbuntuSSH {
			t.Errorf("expected connector type ubuntu_ssh, got %s", resWithConn.Connector.ConnectorType)
		}
		if resWithConn.Connector.Config.Username != "ubuntu" {
			t.Errorf("expected config username 'ubuntu', got %s", resWithConn.Connector.Config.Username)
		}
	})

	t.Run("successfully creates Ubuntu SSH resource with SSH password", func(t *testing.T) {
		input := CreateResourceInput{
			Name: "Ubuntu Server Password Auth",
			Type: domain.TypeUbuntuSSH,
			Connector: CreateConnectorInput{
				Host:         "192.168.1.101",
				Port:         22,
				AuthType:     domain.AuthTypeSSHPassword,
				Username:     "ubuntu",
				CredentialID: credPass,
			},
		}

		resWithConn, err := svc.CreateResource(ctx, orgA, input)
		if err != nil {
			t.Fatalf("expected success, got: %v", err)
		}
		if resWithConn.Connector.AuthType != domain.AuthTypeSSHPassword {
			t.Errorf("expected auth_type ssh_password, got %s", resWithConn.Connector.AuthType)
		}
	})

	t.Run("Ubuntu SSH strictly rejects use_https whether true or false", func(t *testing.T) {
		trueVal := true
		falseVal := false

		input1 := CreateResourceInput{
			Name: "Ubuntu With HTTPS True",
			Type: domain.TypeUbuntuSSH,
			Connector: CreateConnectorInput{
				Host:         "192.168.1.102",
				Port:         22,
				AuthType:     domain.AuthTypeSSHKey,
				Username:     "ubuntu",
				CredentialID: credSSH,
				UseHTTPS:     &trueVal,
			},
		}
		_, err := svc.CreateResource(ctx, orgA, input1)
		if !errors.Is(err, domain.ErrInvalidConnectorConfig) {
			t.Errorf("expected ErrInvalidConnectorConfig for Ubuntu with use_https=true, got: %v", err)
		}

		input2 := CreateResourceInput{
			Name: "Ubuntu With HTTPS False",
			Type: domain.TypeUbuntuSSH,
			Connector: CreateConnectorInput{
				Host:         "192.168.1.103",
				Port:         22,
				AuthType:     domain.AuthTypeSSHKey,
				Username:     "ubuntu",
				CredentialID: credSSH,
				UseHTTPS:     &falseVal,
			},
		}
		_, err = svc.CreateResource(ctx, orgA, input2)
		if !errors.Is(err, domain.ErrInvalidConnectorConfig) {
			t.Errorf("expected ErrInvalidConnectorConfig for Ubuntu with use_https=false, got: %v", err)
		}
	})

	t.Run("successfully creates cPanel resource with API Token", func(t *testing.T) {
		useHTTPS := true
		input := CreateResourceInput{
			Name: "Shared Hosting Account Token",
			Type: domain.TypeCPanel,
			Connector: CreateConnectorInput{
				Host:         "cpanel.example.com",
				Port:         2083,
				AuthType:     domain.AuthTypeCPanelAPIToken,
				Username:     "mycpanel",
				CredentialID: credToken,
				UseHTTPS:     &useHTTPS,
			},
		}

		resWithConn, err := svc.CreateResource(ctx, orgA, input)
		if err != nil {
			t.Fatalf("expected success, got: %v", err)
		}
		if resWithConn.Connector.Config.UseHTTPS == nil || !*resWithConn.Connector.Config.UseHTTPS {
			t.Errorf("expected config use_https = true")
		}
	})

	t.Run("successfully creates cPanel resource with Password", func(t *testing.T) {
		useHTTPS := false
		input := CreateResourceInput{
			Name: "Shared Hosting Account Password",
			Type: domain.TypeCPanel,
			Connector: CreateConnectorInput{
				Host:         "cpanel.example.com",
				Port:         2082,
				AuthType:     domain.AuthTypeCPanelPassword,
				Username:     "mycpanel",
				CredentialID: credCPPass,
				UseHTTPS:     &useHTTPS,
			},
		}

		resWithConn, err := svc.CreateResource(ctx, orgA, input)
		if err != nil {
			t.Fatalf("expected success, got: %v", err)
		}
		if resWithConn.Connector.Config.UseHTTPS == nil || *resWithConn.Connector.Config.UseHTTPS {
			t.Errorf("expected config use_https = false")
		}
	})

	t.Run("rejects cross-tenant credential with ErrInvalidCredentialReference", func(t *testing.T) {
		input := CreateResourceInput{
			Name: "Cross Tenant Attack",
			Type: domain.TypeUbuntuSSH,
			Connector: CreateConnectorInput{
				Host:         "10.0.0.1",
				Port:         22,
				AuthType:     domain.AuthTypeSSHKey,
				Username:     "root",
				CredentialID: credCrossTenant,
			},
		}

		_, err := svc.CreateResource(ctx, orgA, input)
		if !errors.Is(err, domain.ErrInvalidCredentialReference) {
			t.Errorf("expected ErrInvalidCredentialReference, got: %v", err)
		}
	})

	t.Run("rejects credential type mismatch with ErrInvalidCredentialReference", func(t *testing.T) {
		// Ubuntu with cPanel Token credential
		input := CreateResourceInput{
			Name: "Type Mismatch",
			Type: domain.TypeUbuntuSSH,
			Connector: CreateConnectorInput{
				Host:         "10.0.0.1",
				Port:         22,
				AuthType:     domain.AuthTypeSSHKey,
				Username:     "root",
				CredentialID: credToken, // cPanel token
			},
		}

		_, err := svc.CreateResource(ctx, orgA, input)
		if !errors.Is(err, domain.ErrInvalidCredentialReference) {
			t.Errorf("expected ErrInvalidCredentialReference, got: %v", err)
		}
	})

	t.Run("atomic rollback on connector insert failure", func(t *testing.T) {
		initialResCount := len(repo.resources)
		initialConnCount := len(repo.connectors)

		repo.createConnErr = errors.New("db error on connector insert")
		defer func() { repo.createConnErr = nil }()

		input := CreateResourceInput{
			Name: "Rollback Test",
			Type: domain.TypeUbuntuSSH,
			Connector: CreateConnectorInput{
				Host:         "10.0.0.2",
				Port:         22,
				AuthType:     domain.AuthTypeSSHKey,
				Username:     "root",
				CredentialID: credSSH,
			},
		}

		_, err := svc.CreateResource(ctx, orgA, input)
		if !errors.Is(err, domain.ErrResourceServiceUnavailable) {
			t.Errorf("expected ErrResourceServiceUnavailable, got: %v", err)
		}

		if len(repo.resources) != initialResCount {
			t.Errorf("SECURITY FLAW: resource remained persisted after connector insert failure")
		}
		if len(repo.connectors) != initialConnCount {
			t.Errorf("SECURITY FLAW: connector remained persisted after transaction failure")
		}
	})
}

func TestResourceService_UpdateResource(t *testing.T) {
	ctx := context.Background()
	orgA := uuid.New()
	credSSH1 := uuid.New()
	credSSH2 := uuid.New()

	repo := newFakeRepo()
	repo.credentials[credSSH1.String()] = &credDomain.CredentialMetadata{
		ID:             credSSH1,
		OrganizationID: orgA,
		Name:           "SSH Key 1",
		Type:           credDomain.TypeSSHPrivateKey,
	}
	repo.credentials[credSSH2.String()] = &credDomain.CredentialMetadata{
		ID:             credSSH2,
		OrganizationID: orgA,
		Name:           "SSH Key 2",
		Type:           credDomain.TypeSSHPrivateKey,
	}

	txMgr := &fakeTxManager{repoSnapshot: repo}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := NewService(repo, repo, txMgr, logger)

	// Create initial resource
	inputCreate := CreateResourceInput{
		Name: "Original Resource Name",
		Type: domain.TypeUbuntuSSH,
		Connector: CreateConnectorInput{
			Host:         "10.0.0.10",
			Port:         22,
			AuthType:     domain.AuthTypeSSHKey,
			Username:     "root",
			CredentialID: credSSH1,
		},
	}
	created, err := svc.CreateResource(ctx, orgA, inputCreate)
	if err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	t.Run("successfully updates name and connector network parameters", func(t *testing.T) {
		inputUpdate := UpdateResourceInput{
			Name: "Updated Resource Name",
			Connector: CreateConnectorInput{
				Host:         "10.0.0.20",
				Port:         2222,
				AuthType:     domain.AuthTypeSSHKey,
				Username:     "deploy",
				CredentialID: credSSH2,
			},
		}

		updated, err := svc.UpdateResource(ctx, orgA, created.Resource.ID, inputUpdate)
		if err != nil {
			t.Fatalf("expected success, got: %v", err)
		}
		if updated.Resource.Name != "Updated Resource Name" {
			t.Errorf("expected updated name, got %s", updated.Resource.Name)
		}
		if updated.Connector.Host != "10.0.0.20" || updated.Connector.Port != 2222 {
			t.Errorf("expected updated host/port, got %s:%d", updated.Connector.Host, updated.Connector.Port)
		}
		if updated.Connector.Config.Username != "deploy" {
			t.Errorf("expected updated username 'deploy', got %s", updated.Connector.Config.Username)
		}
		if updated.Connector.CredentialID != credSSH2 {
			t.Errorf("expected updated credential ID")
		}
	})

	t.Run("Ubuntu SSH rejects use_https on update", func(t *testing.T) {
		trueVal := true
		inputUpdate := UpdateResourceInput{
			Name: "Invalid Ubuntu Update",
			Connector: CreateConnectorInput{
				Host:         "10.0.0.20",
				Port:         22,
				AuthType:     domain.AuthTypeSSHKey,
				Username:     "deploy",
				CredentialID: credSSH1,
				UseHTTPS:     &trueVal,
			},
		}

		_, err := svc.UpdateResource(ctx, orgA, created.Resource.ID, inputUpdate)
		if !errors.Is(err, domain.ErrInvalidConnectorConfig) {
			t.Errorf("expected ErrInvalidConnectorConfig on update, got: %v", err)
		}
	})

	t.Run("rejects auth type incompatible with original resource type", func(t *testing.T) {
		inputUpdate := UpdateResourceInput{
			Name: "Invalid Auth Type",
			Connector: CreateConnectorInput{
				Host:         "10.0.0.20",
				Port:         2083,
				AuthType:     domain.AuthTypeCPanelAPIToken, // cPanel on Ubuntu resource
				Username:     "root",
				CredentialID: credSSH1,
			},
		}

		_, err := svc.UpdateResource(ctx, orgA, created.Resource.ID, inputUpdate)
		if !errors.Is(err, domain.ErrInvalidAuthType) {
			t.Errorf("expected ErrInvalidAuthType, got: %v", err)
		}
	})

	t.Run("atomic rollback when connector update fails", func(t *testing.T) {
		repo.updateConnErr = errors.New("db error on connector update")
		defer func() { repo.updateConnErr = nil }()

		inputUpdate := UpdateResourceInput{
			Name: "Rollback Name",
			Connector: CreateConnectorInput{
				Host:         "10.0.0.99",
				Port:         22,
				AuthType:     domain.AuthTypeSSHKey,
				Username:     "root",
				CredentialID: credSSH1,
			},
		}

		_, err := svc.UpdateResource(ctx, orgA, created.Resource.ID, inputUpdate)
		if !errors.Is(err, domain.ErrResourceServiceUnavailable) {
			t.Errorf("expected ErrResourceServiceUnavailable, got: %v", err)
		}

		// Verify resource name in repo was rolled back to previous state
		current := repo.resources[created.Resource.ID.String()]
		if current.Name == "Rollback Name" {
			t.Errorf("SECURITY FLAW: resource name remained modified after connector update failure")
		}
	})
}

func TestResourceService_RawErrorLeakageLogging(t *testing.T) {
	ctx := context.Background()
	orgA := uuid.New()
	credSSH := uuid.New()

	repo := newFakeRepo()
	repo.credentials[credSSH.String()] = &credDomain.CredentialMetadata{
		ID:             credSSH,
		OrganizationID: orgA,
		Name:           "SSH Key",
		Type:           credDomain.TypeSSHPrivateKey,
	}

	// Fake database error containing highly sensitive connection details / password
	repo.createResErr = errors.New("pq: password=SUPERSECRET connection failed on internal-db host:5432")

	logBuf := &bytes.Buffer{}
	logger := slog.New(slog.NewTextHandler(logBuf, nil))
	txMgr := &fakeTxManager{repoSnapshot: repo}
	svc := NewService(repo, repo, txMgr, logger)

	input := CreateResourceInput{
		Name: "Logging Test Server",
		Type: domain.TypeUbuntuSSH,
		Connector: CreateConnectorInput{
			Host:         "10.0.0.1",
			Port:         22,
			AuthType:     domain.AuthTypeSSHKey,
			Username:     "root",
			CredentialID: credSSH,
		},
	}

	_, err := svc.CreateResource(ctx, orgA, input)
	if !errors.Is(err, domain.ErrResourceServiceUnavailable) {
		t.Errorf("expected ErrResourceServiceUnavailable, got: %v", err)
	}

	logOutput := logBuf.String()

	// 1. Must contain generic operation message
	if !strings.Contains(logOutput, "resource creation transaction failed") {
		t.Errorf("expected log to contain generic failure message, got: %s", logOutput)
	}

	// 2. Must NOT contain sensitive raw DB error strings
	forbiddenStrings := []string{
		"SUPERSECRET",
		"password=",
		"pq:",
		"internal-db",
	}
	for _, forbidden := range forbiddenStrings {
		if strings.Contains(logOutput, forbidden) {
			t.Errorf("SECURITY FLAW: raw infrastructure error leaked into logs: %q found in %s", forbidden, logOutput)
		}
	}
}

func TestResourceService_ArchiveResource(t *testing.T) {
	ctx := context.Background()
	orgA := uuid.New()
	credSSH := uuid.New()

	repo := newFakeRepo()
	repo.credentials[credSSH.String()] = &credDomain.CredentialMetadata{
		ID:             credSSH,
		OrganizationID: orgA,
		Name:           "SSH Key",
		Type:           credDomain.TypeSSHPrivateKey,
	}

	txMgr := &fakeTxManager{repoSnapshot: repo}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := NewService(repo, repo, txMgr, logger)

	created, err := svc.CreateResource(ctx, orgA, CreateResourceInput{
		Name: "To Archive",
		Type: domain.TypeUbuntuSSH,
		Connector: CreateConnectorInput{
			Host:         "10.0.0.1",
			Port:         22,
			AuthType:     domain.AuthTypeSSHKey,
			Username:     "root",
			CredentialID: credSSH,
		},
	})
	if err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	t.Run("successfully archives resource", func(t *testing.T) {
		err := svc.ArchiveResource(ctx, orgA, created.Resource.ID)
		if err != nil {
			t.Fatalf("expected success, got: %v", err)
		}

		// Connector should still be present in repository
		if repo.connectors[created.Resource.ID.String()] == nil {
			t.Errorf("SECURITY FLAW: connector was deleted during archive")
		}

		// Subsequent Get should return ErrResourceNotFound
		_, err = svc.GetResource(ctx, orgA, created.Resource.ID)
		if !errors.Is(err, domain.ErrResourceNotFound) {
			t.Errorf("expected ErrResourceNotFound for archived resource, got: %v", err)
		}
	})
}

func TestResourceService_CorruptDataMapping(t *testing.T) {
	ctx := context.Background()
	orgA := uuid.New()
	resID := uuid.New()

	repo := newFakeRepo()
	repo.findErr = domain.ErrCorruptResourceData
	repo.listErr = domain.ErrCorruptResourceData

	logBuf := &bytes.Buffer{}
	logger := slog.New(slog.NewTextHandler(logBuf, nil))
	txMgr := &fakeTxManager{repoSnapshot: repo}
	svc := NewService(repo, repo, txMgr, logger)

	t.Run("GetResource maps ErrCorruptResourceData to ErrResourceServiceUnavailable", func(t *testing.T) {
		_, err := svc.GetResource(ctx, orgA, resID)
		if !errors.Is(err, domain.ErrResourceServiceUnavailable) {
			t.Errorf("expected ErrResourceServiceUnavailable, got: %v", err)
		}
		if errors.Is(err, domain.ErrCorruptResourceData) {
			t.Errorf("ErrCorruptResourceData should not leak directly from service")
		}
	})

	t.Run("ListResources maps ErrCorruptResourceData to ErrResourceServiceUnavailable", func(t *testing.T) {
		_, err := svc.ListResources(ctx, orgA)
		if !errors.Is(err, domain.ErrResourceServiceUnavailable) {
			t.Errorf("expected ErrResourceServiceUnavailable, got: %v", err)
		}
	})
}
