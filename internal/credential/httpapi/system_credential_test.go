package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"backup-platform/internal/credential/domain"
	identityDomain "backup-platform/internal/identity/domain"
	identityHttpapi "backup-platform/internal/identity/httpapi"
	orgDomain "backup-platform/internal/organization/domain"
	orgHttpapi "backup-platform/internal/organization/httpapi"
	orgRepo "backup-platform/internal/organization/repository"
	"backup-platform/pkg/uuid"
)

func TestSystemCredential_PublicAPI_SecurityRules(t *testing.T) {
	orgID := uuid.New()
	adminUserID := uuid.New()
	systemCredID := uuid.New()
	userCredID := uuid.New()

	memberRepo := newMockOrgMemberRepo()
	memberRepo.memberships[orgID.String()+":"+adminUserID.String()] = &orgRepo.UserMembershipWithOrg{
		OrganizationID:   orgID,
		OrganizationName: "Security Test Org",
		Slug:             "security-test-org",
		Role:             orgDomain.RoleAdmin,
		Status:           orgDomain.MemberStatusActive,
	}

	testLogger := slog.New(slog.NewTextHandler(io.Discard, nil))
	orgContextMW := orgHttpapi.NewOrganizationContextMiddleware(memberRepo, &mockTxManager{}, testLogger)
	adminGuard := orgHttpapi.RequireOrganizationAdmin(testLogger)

	userMeta := &domain.CredentialMetadata{
		ID:             userCredID,
		OrganizationID: orgID,
		Name:           "User S3 Credentials",
		Type:           domain.TypeS3Credentials,
		ManagedBy:      domain.ManagedByUser,
		KeyVersion:     1,
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
	}

	systemMeta := &domain.CredentialMetadata{
		ID:             systemCredID,
		OrganizationID: orgID,
		Name:           "restic-repo-key",
		Type:           domain.TypeResticRepositoryKey,
		ManagedBy:      domain.ManagedBySystem,
		KeyVersion:     1,
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
	}

	credService := &mockSystemAwareCredentialService{
		userCreds:   map[uuid.UUID]*domain.CredentialMetadata{userCredID: userMeta},
		systemCreds: map[uuid.UUID]*domain.CredentialMetadata{systemCredID: systemMeta},
	}

	handler := NewHandler(credService, testLogger)

	listRoute := orgContextMW(adminGuard(http.HandlerFunc(handler.List)))
	getRoute := orgContextMW(adminGuard(http.HandlerFunc(handler.GetByID)))
	createRoute := orgContextMW(adminGuard(http.HandlerFunc(handler.Create)))
	updateRoute := orgContextMW(adminGuard(http.HandlerFunc(handler.Update)))
	deleteRoute := orgContextMW(adminGuard(http.HandlerFunc(handler.Delete)))

	makeReq := func(method, path string, body []byte) *http.Request {
		var r *http.Request
		if body != nil {
			r = httptest.NewRequest(method, path, bytes.NewReader(body))
			r.Header.Set("Content-Type", "application/json")
		} else {
			r = httptest.NewRequest(method, path, nil)
		}
		r.Header.Set("X-Organization-ID", orgID.String())
		authCtx := &identityHttpapi.AuthContext{
			UserID:        adminUserID,
			Email:         "admin@security-test.org",
			Status:        identityDomain.UserStatusActive,
			IsSystemAdmin: false,
		}
		return r.WithContext(identityHttpapi.WithAuthContext(r.Context(), authCtx))
	}

	t.Run("PUBLIC LIST excludes system credentials and includes user credentials", func(t *testing.T) {
		req := makeReq(http.MethodGet, "/api/v1/credentials", nil)
		rec := httptest.NewRecorder()
		listRoute.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", rec.Code)
		}

		var resp struct {
			Data []CredentialListItemResponse `json:"data"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("failed decoding json: %v", err)
		}

		if len(resp.Data) != 1 {
			t.Fatalf("expected exactly 1 credential in list, got %d", len(resp.Data))
		}
		if resp.Data[0].ID != userCredID.String() {
			t.Errorf("expected user cred ID %s, got %s", userCredID, resp.Data[0].ID)
		}
		for _, item := range resp.Data {
			if item.Type == string(domain.TypeResticRepositoryKey) || item.ID == systemCredID.String() {
				t.Errorf("SECURITY VIOLATION: system credential leaked in public list: %+v", item)
			}
		}
	})

	t.Run("PUBLIC GET by ID on system credential returns 404 not found", func(t *testing.T) {
		req := makeReq(http.MethodGet, "/api/v1/credentials/"+systemCredID.String(), nil)
		req.SetPathValue("id", systemCredID.String())
		rec := httptest.NewRecorder()
		getRoute.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 Not Found for system credential, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("PUBLIC GET by ID on user credential returns 200 with safe metadata", func(t *testing.T) {
		req := makeReq(http.MethodGet, "/api/v1/credentials/"+userCredID.String(), nil)
		req.SetPathValue("id", userCredID.String())
		rec := httptest.NewRecorder()
		getRoute.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK for user credential, got %d", rec.Code)
		}

		bodyStr := rec.Body.String()
		if strings.Contains(bodyStr, "encrypted_secret") || strings.Contains(bodyStr, "nonce") || strings.Contains(bodyStr, "auth_tag") {
			t.Errorf("SECURITY VIOLATION: sensitive field found in response: %s", bodyStr)
		}
	})

	t.Run("PUBLIC CREATE with restic_repository_key is rejected", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"name":   "Malicious Key",
			"type":   "restic_repository_key",
			"secret": "some-secret",
		})
		req := makeReq(http.MethodPost, "/api/v1/credentials", body)
		rec := httptest.NewRecorder()
		createRoute.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden && rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("expected 403 Forbidden or 422 UnprocessableEntity, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("PUBLIC UPDATE on system credential returns 403 Forbidden", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"name": "Tampered System Credential Name",
		})
		req := makeReq(http.MethodPut, "/api/v1/credentials/"+systemCredID.String(), body)
		req.SetPathValue("id", systemCredID.String())
		rec := httptest.NewRecorder()
		updateRoute.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 Forbidden when mutating system credential, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("PUBLIC DELETE on system credential returns 403 Forbidden", func(t *testing.T) {
		req := makeReq(http.MethodDelete, "/api/v1/credentials/"+systemCredID.String(), nil)
		req.SetPathValue("id", systemCredID.String())
		rec := httptest.NewRecorder()
		deleteRoute.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 Forbidden when deleting system credential, got %d: %s", rec.Code, rec.Body.String())
		}
	})
}

type mockSystemAwareCredentialService struct {
	userCreds   map[uuid.UUID]*domain.CredentialMetadata
	systemCreds map[uuid.UUID]*domain.CredentialMetadata
}

func (m *mockSystemAwareCredentialService) CreateCredential(
	ctx context.Context,
	orgID uuid.UUID,
	name string,
	credType domain.Type,
	plaintextPayload []byte,
	fingerprint *string,
) (*domain.CredentialMetadata, error) {
	if !credType.IsUserManaged() {
		return nil, domain.ErrSystemCredentialRestricted
	}
	newMeta := &domain.CredentialMetadata{
		ID:             uuid.New(),
		OrganizationID: orgID,
		Name:           name,
		Type:           credType,
		ManagedBy:      domain.ManagedByUser,
		KeyVersion:     1,
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
	}
	m.userCreds[newMeta.ID] = newMeta
	return newMeta, nil
}

func (m *mockSystemAwareCredentialService) ListCredentialMetadata(
	ctx context.Context,
	orgID uuid.UUID,
) ([]*domain.CredentialMetadata, error) {
	var list []*domain.CredentialMetadata
	for _, c := range m.userCreds {
		if c.OrganizationID == orgID && c.ManagedBy == domain.ManagedByUser {
			list = append(list, c)
		}
	}
	return list, nil
}

func (m *mockSystemAwareCredentialService) GetCredentialMetadata(
	ctx context.Context,
	orgID uuid.UUID,
	credID uuid.UUID,
) (*domain.CredentialMetadata, error) {
	if c, ok := m.systemCreds[credID]; ok && c.OrganizationID == orgID {
		return nil, domain.ErrCredentialNotFound
	}
	if c, ok := m.userCreds[credID]; ok && c.OrganizationID == orgID {
		return c, nil
	}
	return nil, domain.ErrCredentialNotFound
}

func (m *mockSystemAwareCredentialService) GetSystemCredentialMetadata(
	ctx context.Context,
	orgID uuid.UUID,
	credID uuid.UUID,
) (*domain.CredentialMetadata, error) {
	if c, ok := m.systemCreds[credID]; ok && c.OrganizationID == orgID {
		return c, nil
	}
	if c, ok := m.userCreds[credID]; ok && c.OrganizationID == orgID {
		return c, nil
	}
	return nil, domain.ErrCredentialNotFound
}

func (m *mockSystemAwareCredentialService) UpdateCredentialName(
	ctx context.Context,
	orgID uuid.UUID,
	credID uuid.UUID,
	name string,
) (*domain.CredentialMetadata, error) {
	if c, ok := m.systemCreds[credID]; ok && c.OrganizationID == orgID {
		return nil, domain.ErrSystemCredentialRestricted
	}
	if c, ok := m.userCreds[credID]; ok && c.OrganizationID == orgID {
		c.Name = name
		return c, nil
	}
	return nil, domain.ErrCredentialNotFound
}

func (m *mockSystemAwareCredentialService) ReplaceCredentialSecret(
	ctx context.Context,
	orgID uuid.UUID,
	credID uuid.UUID,
	name *string,
	plaintextPayload []byte,
	fingerprint *string,
) (*domain.CredentialMetadata, error) {
	if c, ok := m.systemCreds[credID]; ok && c.OrganizationID == orgID {
		return nil, domain.ErrSystemCredentialRestricted
	}
	if c, ok := m.userCreds[credID]; ok && c.OrganizationID == orgID {
		if name != nil {
			c.Name = *name
		}
		return c, nil
	}
	return nil, domain.ErrCredentialNotFound
}

func (m *mockSystemAwareCredentialService) DeleteCredential(
	ctx context.Context,
	orgID uuid.UUID,
	credID uuid.UUID,
) error {
	if c, ok := m.systemCreds[credID]; ok && c.OrganizationID == orgID {
		return domain.ErrSystemCredentialRestricted
	}
	if _, ok := m.userCreds[credID]; ok {
		delete(m.userCreds, credID)
		return nil
	}
	return domain.ErrCredentialNotFound
}
