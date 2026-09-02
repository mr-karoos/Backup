package httpapi

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"backup-platform/internal/credential/domain"
	"backup-platform/internal/credential/payload"
	orgDomain "backup-platform/internal/organization/domain"
	orgHttpapi "backup-platform/internal/organization/httpapi"
	"backup-platform/internal/platform/httpapi"
	"backup-platform/pkg/uuid"

	"golang.org/x/crypto/ssh"
)

func generateTestED25519Key(t *testing.T) (string, string) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate ed25519 key: %v", err)
	}
	privBlock, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		t.Fatalf("failed to marshal private key: %v", err)
	}
	pemBytes := pem.EncodeToMemory(privBlock)

	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatalf("failed to convert public key: %v", err)
	}
	fp := ssh.FingerprintSHA256(sshPub)
	return string(pemBytes), fp
}

func generateTestEncryptedED25519Key(t *testing.T, passphrase string) (string, string) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate ed25519 key: %v", err)
	}
	privBlock, err := ssh.MarshalPrivateKeyWithPassphrase(priv, "", []byte(passphrase))
	if err != nil {
		t.Fatalf("failed to marshal encrypted private key: %v", err)
	}
	pemBytes := pem.EncodeToMemory(privBlock)

	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatalf("failed to convert public key: %v", err)
	}
	fp := ssh.FingerprintSHA256(sshPub)
	return string(pemBytes), fp
}

type fakeCredentialService struct {
	createErr              error
	listErr                error
	getMetaErr             error
	updateNameErr          error
	replaceSecretErr       error
	deleteErr              error
	createdMeta            *domain.CredentialMetadata
	listItems              []*domain.CredentialMetadata
	currentMeta            *domain.CredentialMetadata
	capturedOrgID          uuid.UUID
	capturedCredID         uuid.UUID
	capturedName           string
	capturedType           domain.Type
	capturedFingerprint    *string
	capturedPayload        []byte
	bufferSnapshotOnCreate []byte
	bufferSnapshotOnUpdate []byte
	createCalls            int
	updateNameCalls        int
	replaceSecretCalls     int
	deleteCalls            int
}

func (f *fakeCredentialService) CreateCredential(
	ctx context.Context,
	orgID uuid.UUID,
	name string,
	credType domain.Type,
	plaintextPayload []byte,
	fingerprint *string,
) (*domain.CredentialMetadata, error) {
	f.createCalls++
	f.capturedOrgID = orgID
	f.capturedName = name
	f.capturedType = credType
	f.capturedFingerprint = fingerprint
	f.capturedPayload = plaintextPayload

	if len(plaintextPayload) > 0 {
		snap := make([]byte, len(plaintextPayload))
		copy(snap, plaintextPayload)
		f.bufferSnapshotOnCreate = snap
	}

	if f.createErr != nil {
		return nil, f.createErr
	}

	if f.createdMeta != nil {
		return f.createdMeta, nil
	}

	return &domain.CredentialMetadata{
		ID:             uuid.New(),
		OrganizationID: orgID,
		Name:           name,
		Type:           credType,
		Fingerprint:    fingerprint,
		KeyVersion:     1,
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
	}, nil
}

func (f *fakeCredentialService) ListCredentialMetadata(
	ctx context.Context,
	orgID uuid.UUID,
) ([]*domain.CredentialMetadata, error) {
	f.capturedOrgID = orgID
	if f.listErr != nil {
		return nil, f.listErr
	}
	if f.listItems != nil {
		return f.listItems, nil
	}
	return []*domain.CredentialMetadata{}, nil
}

func (f *fakeCredentialService) GetCredentialMetadata(
	ctx context.Context,
	orgID uuid.UUID,
	credID uuid.UUID,
) (*domain.CredentialMetadata, error) {
	f.capturedOrgID = orgID
	f.capturedCredID = credID
	if f.getMetaErr != nil {
		return nil, f.getMetaErr
	}
	if f.currentMeta != nil {
		if f.currentMeta.OrganizationID != orgID || f.currentMeta.ID != credID {
			return nil, domain.ErrCredentialNotFound
		}
		return f.currentMeta, nil
	}
	return nil, domain.ErrCredentialNotFound
}

func (f *fakeCredentialService) UpdateCredentialName(
	ctx context.Context,
	orgID uuid.UUID,
	credID uuid.UUID,
	name string,
) (*domain.CredentialMetadata, error) {
	f.updateNameCalls++
	f.capturedOrgID = orgID
	f.capturedCredID = credID
	f.capturedName = name
	if f.updateNameErr != nil {
		return nil, f.updateNameErr
	}
	if f.currentMeta != nil {
		if f.currentMeta.OrganizationID != orgID || f.currentMeta.ID != credID {
			return nil, domain.ErrCredentialNotFound
		}
		f.currentMeta.Name = name
		f.currentMeta.UpdatedAt = time.Now().UTC()
		return f.currentMeta, nil
	}
	return nil, domain.ErrCredentialNotFound
}

func (f *fakeCredentialService) ReplaceCredentialSecret(
	ctx context.Context,
	orgID uuid.UUID,
	credID uuid.UUID,
	name *string,
	plaintextPayload []byte,
	fingerprint *string,
) (*domain.CredentialMetadata, error) {
	f.replaceSecretCalls++
	f.capturedOrgID = orgID
	f.capturedCredID = credID
	f.capturedFingerprint = fingerprint
	f.capturedPayload = plaintextPayload
	if name != nil {
		f.capturedName = *name
	}

	if len(plaintextPayload) > 0 {
		snap := make([]byte, len(plaintextPayload))
		copy(snap, plaintextPayload)
		f.bufferSnapshotOnUpdate = snap
	}

	if f.replaceSecretErr != nil {
		return nil, f.replaceSecretErr
	}

	if f.currentMeta != nil {
		if f.currentMeta.OrganizationID != orgID || f.currentMeta.ID != credID {
			return nil, domain.ErrCredentialNotFound
		}
		if name != nil {
			f.currentMeta.Name = *name
		}
		f.currentMeta.Fingerprint = fingerprint
		f.currentMeta.UpdatedAt = time.Now().UTC()
		return f.currentMeta, nil
	}

	return nil, domain.ErrCredentialNotFound
}

func (f *fakeCredentialService) DeleteCredential(
	ctx context.Context,
	orgID uuid.UUID,
	credID uuid.UUID,
) error {
	f.deleteCalls++
	f.capturedOrgID = orgID
	f.capturedCredID = credID
	if f.deleteErr != nil {
		return f.deleteErr
	}
	if f.currentMeta != nil {
		if f.currentMeta.OrganizationID != orgID || f.currentMeta.ID != credID {
			return domain.ErrCredentialNotFound
		}
		f.currentMeta = nil
		return nil
	}
	return domain.ErrCredentialNotFound
}

func attachAdminTenantContext(r *http.Request, orgID uuid.UUID) *http.Request {
	tenantCtx := &orgHttpapi.TenantContext{
		OrganizationID:    orgID,
		UserID:            uuid.New(),
		OrganizationName:  "Acme Corp",
		OrganizationSlug:  "acme-corp",
		IsDefaultInternal: false,
		Role:              orgDomain.RoleAdmin,
		MembershipStatus:  orgDomain.MemberStatusActive,
	}
	return r.WithContext(orgHttpapi.WithTenantContext(r.Context(), tenantCtx))
}

func TestCredentialHandler_Create(t *testing.T) {
	orgID := uuid.New()

	t.Run("successfully creates ssh_password credential with 201 Created", func(t *testing.T) {
		svc := &fakeCredentialService{}
		h := NewHandler(svc, nil)

		body := map[string]any{
			"name":   "Production SSH Password",
			"type":   "ssh_password",
			"secret": "super-secret-password-123",
		}
		jsonBytes, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/credentials", bytes.NewReader(jsonBytes))
		req.Header.Set("Content-Type", "application/json")
		req = attachAdminTenantContext(req, orgID)

		rec := httptest.NewRecorder()
		h.Create(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created, got %d. Body: %s", rec.Code, rec.Body.String())
		}
		if rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get("Pragma") != "no-cache" {
			t.Errorf("missing non-cacheable headers")
		}

		var resp httpapi.ResponseEnvelope
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to decode response envelope: %v", err)
		}

		dataMap, ok := resp.Data.(map[string]any)
		if !ok {
			t.Fatalf("expected data map in envelope")
		}

		if dataMap["name"] != "Production SSH Password" || dataMap["type"] != "ssh_password" {
			t.Errorf("unexpected response data: %+v", dataMap)
		}
		if dataMap["fingerprint"] != nil {
			t.Errorf("expected nil fingerprint for ssh_password")
		}

		// Verify service received structured version 1 JSON intermediate payload
		intermediate, err := payload.Decode(svc.bufferSnapshotOnCreate)
		if err != nil {
			t.Fatalf("service did not receive valid intermediate JSON payload: %v", err)
		}
		if intermediate.Version != 1 || intermediate.Secret != "super-secret-password-123" {
			t.Errorf("unexpected intermediate payload: %+v", intermediate)
		}
	})

	t.Run("successfully creates cpanel_api_token credential", func(t *testing.T) {
		svc := &fakeCredentialService{}
		h := NewHandler(svc, nil)

		body := map[string]any{
			"name":   "cPanel Backup Token",
			"type":   "cpanel_api_token",
			"secret": "CPANEL_API_TOKEN_XYZ987",
		}
		jsonBytes, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/credentials", bytes.NewReader(jsonBytes))
		req.Header.Set("Content-Type", "application/json")
		req = attachAdminTenantContext(req, orgID)

		rec := httptest.NewRecorder()
		h.Create(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created, got %d", rec.Code)
		}
	})

	t.Run("successfully creates valid unencrypted ssh_private_key and computes SHA256 fingerprint", func(t *testing.T) {
		svc := &fakeCredentialService{}
		h := NewHandler(svc, nil)

		privPEM, expectedFP := generateTestED25519Key(t)

		body := map[string]any{
			"name":       "Server ED25519 Key",
			"type":       "ssh_private_key",
			"secret":     privPEM,
			"passphrase": nil,
		}
		jsonBytes, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/credentials", bytes.NewReader(jsonBytes))
		req.Header.Set("Content-Type", "application/json")
		req = attachAdminTenantContext(req, orgID)

		rec := httptest.NewRecorder()
		h.Create(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		var resp httpapi.ResponseEnvelope
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		dataMap := resp.Data.(map[string]any)

		if dataMap["fingerprint"] != expectedFP {
			t.Errorf("expected fingerprint %s, got %v", expectedFP, dataMap["fingerprint"])
		}
		if svc.capturedFingerprint == nil || *svc.capturedFingerprint != expectedFP {
			t.Errorf("expected service to receive fingerprint %s", expectedFP)
		}
	})

	t.Run("successfully creates encrypted ssh_private_key with valid passphrase", func(t *testing.T) {
		svc := &fakeCredentialService{}
		h := NewHandler(svc, nil)

		passphrase := "my-strong-ssh-passphrase"
		privPEM, expectedFP := generateTestEncryptedED25519Key(t, passphrase)

		body := map[string]any{
			"name":       "Encrypted SSH Key",
			"type":       "ssh_private_key",
			"secret":     privPEM,
			"passphrase": passphrase,
		}
		jsonBytes, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/credentials", bytes.NewReader(jsonBytes))
		req.Header.Set("Content-Type", "application/json")
		req = attachAdminTenantContext(req, orgID)

		rec := httptest.NewRecorder()
		h.Create(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		var resp httpapi.ResponseEnvelope
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		dataMap := resp.Data.(map[string]any)

		if dataMap["fingerprint"] != expectedFP {
			t.Errorf("expected fingerprint %s, got %v", expectedFP, dataMap["fingerprint"])
		}
	})

	t.Run("rejects invalid SSH private key format with 422 VALIDATION_FAILED", func(t *testing.T) {
		svc := &fakeCredentialService{}
		h := NewHandler(svc, nil)

		body := map[string]any{
			"name":   "Broken SSH Key",
			"type":   "ssh_private_key",
			"secret": "-----BEGIN RSA PRIVATE KEY-----\nNOT_A_VALID_KEY\n-----END RSA PRIVATE KEY-----",
		}
		jsonBytes, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/credentials", bytes.NewReader(jsonBytes))
		req.Header.Set("Content-Type", "application/json")
		req = attachAdminTenantContext(req, orgID)

		rec := httptest.NewRecorder()
		h.Create(rec, req)

		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("expected 422, got %d", rec.Code)
		}

		var errEnv httpapi.ErrorEnvelope
		_ = json.Unmarshal(rec.Body.Bytes(), &errEnv)
		if errEnv.Error.Code != "VALIDATION_FAILED" {
			t.Errorf("expected code VALIDATION_FAILED, got %s", errEnv.Error.Code)
		}
	})

	t.Run("rejects encrypted SSH key when passphrase is wrong with 422 VALIDATION_FAILED", func(t *testing.T) {
		svc := &fakeCredentialService{}
		h := NewHandler(svc, nil)

		privPEM, _ := generateTestEncryptedED25519Key(t, "correct-passphrase")

		body := map[string]any{
			"name":       "Encrypted SSH Key",
			"type":       "ssh_private_key",
			"secret":     privPEM,
			"passphrase": "wrong-passphrase",
		}
		jsonBytes, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/credentials", bytes.NewReader(jsonBytes))
		req.Header.Set("Content-Type", "application/json")
		req = attachAdminTenantContext(req, orgID)

		rec := httptest.NewRecorder()
		h.Create(rec, req)

		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("expected 422 for wrong passphrase, got %d", rec.Code)
		}
	})

	t.Run("rejects passphrase on non-ssh_private_key types with 422", func(t *testing.T) {
		nonSSHTypes := []string{"ssh_password", "cpanel_api_token", "cpanel_password"}
		for _, credType := range nonSSHTypes {
			t.Run(credType, func(t *testing.T) {
				svc := &fakeCredentialService{}
				h := NewHandler(svc, nil)

				body := map[string]any{
					"name":       "Test Cred",
					"type":       credType,
					"secret":     "secret123",
					"passphrase": "unsupported-passphrase",
				}
				jsonBytes, _ := json.Marshal(body)

				req := httptest.NewRequest(http.MethodPost, "/api/v1/credentials", bytes.NewReader(jsonBytes))
				req.Header.Set("Content-Type", "application/json")
				req = attachAdminTenantContext(req, orgID)

				rec := httptest.NewRecorder()
				h.Create(rec, req)

				if rec.Code != http.StatusUnprocessableEntity {
					t.Fatalf("expected 422 for passphrase on %s, got %d", credType, rec.Code)
				}
			})
		}
	})

	t.Run("rejects unknown credential type with 422", func(t *testing.T) {
		svc := &fakeCredentialService{}
		h := NewHandler(svc, nil)

		body := map[string]any{
			"name":   "AWS Secret",
			"type":   "aws_iam_key",
			"secret": "secret",
		}
		jsonBytes, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/credentials", bytes.NewReader(jsonBytes))
		req.Header.Set("Content-Type", "application/json")
		req = attachAdminTenantContext(req, orgID)

		rec := httptest.NewRecorder()
		h.Create(rec, req)

		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("expected 422 for unknown type, got %d", rec.Code)
		}
	})

	t.Run("rejects empty secret with 422", func(t *testing.T) {
		svc := &fakeCredentialService{}
		h := NewHandler(svc, nil)

		body := map[string]any{
			"name":   "Empty",
			"type":   "ssh_password",
			"secret": "",
		}
		jsonBytes, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/credentials", bytes.NewReader(jsonBytes))
		req.Header.Set("Content-Type", "application/json")
		req = attachAdminTenantContext(req, orgID)

		rec := httptest.NewRecorder()
		h.Create(rec, req)

		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("expected 422 for empty secret, got %d", rec.Code)
		}
	})

	t.Run("rejects invalid or empty name with 422", func(t *testing.T) {
		svc := &fakeCredentialService{}
		h := NewHandler(svc, nil)

		body := map[string]any{
			"name":   "   ",
			"type":   "ssh_password",
			"secret": "valid-secret",
		}
		jsonBytes, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/credentials", bytes.NewReader(jsonBytes))
		req.Header.Set("Content-Type", "application/json")
		req = attachAdminTenantContext(req, orgID)

		rec := httptest.NewRecorder()
		h.Create(rec, req)

		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("expected 422 for empty name, got %d", rec.Code)
		}
	})

	t.Run("rejects unknown JSON fields with 400 BAD_REQUEST", func(t *testing.T) {
		svc := &fakeCredentialService{}
		h := NewHandler(svc, nil)

		body := map[string]any{
			"name":            "Test",
			"type":            "ssh_password",
			"secret":          "secret",
			"unknown_payload": "injected",
		}
		jsonBytes, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/credentials", bytes.NewReader(jsonBytes))
		req.Header.Set("Content-Type", "application/json")
		req = attachAdminTenantContext(req, orgID)

		rec := httptest.NewRecorder()
		h.Create(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for unknown JSON fields, got %d", rec.Code)
		}
	})

	t.Run("rejects wrong Content-Type with 400", func(t *testing.T) {
		svc := &fakeCredentialService{}
		h := NewHandler(svc, nil)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/credentials", strings.NewReader("name=test"))
		req.Header.Set("Content-Type", "text/plain")
		req = attachAdminTenantContext(req, orgID)

		rec := httptest.NewRecorder()
		h.Create(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for wrong Content-Type, got %d", rec.Code)
		}
	})

	t.Run("secret non-disclosure guarantee in response", func(t *testing.T) {
		svc := &fakeCredentialService{}
		h := NewHandler(svc, nil)

		sensitiveSecret := "VERY-SENSITIVE-CREDENTIAL-TEST-VALUE"
		body := map[string]any{
			"name":   "Sensitive Cred",
			"type":   "ssh_password",
			"secret": sensitiveSecret,
		}
		jsonBytes, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/credentials", bytes.NewReader(jsonBytes))
		req.Header.Set("Content-Type", "application/json")
		req = attachAdminTenantContext(req, orgID)

		rec := httptest.NewRecorder()
		h.Create(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created, got %d", rec.Code)
		}

		responseBody := rec.Body.String()
		if strings.Contains(responseBody, sensitiveSecret) {
			t.Errorf("SECURITY DISASTER: plaintext secret was echoed in response: %s", responseBody)
		}

		forbiddenKeys := []string{"encrypted_secret", "nonce", "auth_tag", "passphrase", "organization_id"}
		for _, key := range forbiddenKeys {
			if strings.Contains(responseBody, `"`+key+`"`) {
				t.Errorf("SECURITY FLAW: forbidden key '%s' found in Create response: %s", key, responseBody)
			}
		}
	})

	t.Run("converts service unavailable to 503", func(t *testing.T) {
		svc := &fakeCredentialService{
			createErr: domain.ErrCredentialServiceUnavailable,
		}
		h := NewHandler(svc, nil)

		body := map[string]any{
			"name":   "Test",
			"type":   "ssh_password",
			"secret": "secret",
		}
		jsonBytes, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/credentials", bytes.NewReader(jsonBytes))
		req.Header.Set("Content-Type", "application/json")
		req = attachAdminTenantContext(req, orgID)

		rec := httptest.NewRecorder()
		h.Create(rec, req)

		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("expected 503 for service unavailable, got %d", rec.Code)
		}
	})
}

func TestCredentialHandler_List(t *testing.T) {
	orgID := uuid.New()
	now := time.Now().UTC()
	fp := "SHA256:testlistfp"

	t.Run("successfully lists credentials with safe metadata only", func(t *testing.T) {
		credID1 := uuid.New()
		credID2 := uuid.New()

		svc := &fakeCredentialService{
			listItems: []*domain.CredentialMetadata{
				{
					ID:             credID1,
					OrganizationID: orgID,
					Name:           "Server SSH Key",
					Type:           domain.TypeSSHPrivateKey,
					Fingerprint:    &fp,
					KeyVersion:     1,
					CreatedAt:      now,
					UpdatedAt:      now,
				},
				{
					ID:             credID2,
					OrganizationID: orgID,
					Name:           "cPanel Password",
					Type:           domain.TypeCPanelPassword,
					Fingerprint:    nil,
					KeyVersion:     1,
					CreatedAt:      now.Add(-time.Hour),
					UpdatedAt:      now.Add(-time.Hour),
				},
			},
		}
		h := NewHandler(svc, nil)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/credentials", nil)
		req = attachAdminTenantContext(req, orgID)

		rec := httptest.NewRecorder()
		h.List(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d. Body: %s", rec.Code, rec.Body.String())
		}
		if rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get("Pragma") != "no-cache" {
			t.Errorf("missing non-cacheable headers")
		}

		var resp httpapi.ResponseEnvelope
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to decode list response: %v", err)
		}

		items, ok := resp.Data.([]any)
		if !ok || len(items) != 2 {
			t.Fatalf("expected 2 items, got %v", items)
		}

		item0 := items[0].(map[string]any)
		if item0["id"] != credID1.String() || item0["name"] != "Server SSH Key" || item0["fingerprint"] != fp {
			t.Errorf("unexpected item 0: %+v", item0)
		}

		forbiddenKeys := []string{"encrypted_secret", "nonce", "auth_tag", "secret", "passphrase", "organization_id"}
		respStr := rec.Body.String()
		for _, key := range forbiddenKeys {
			if strings.Contains(respStr, `"`+key+`"`) {
				t.Errorf("SECURITY FLAW: forbidden key '%s' found in List response: %s", key, respStr)
			}
		}
	})

	t.Run("returns empty array when no credentials exist", func(t *testing.T) {
		svc := &fakeCredentialService{
			listItems: []*domain.CredentialMetadata{},
		}
		h := NewHandler(svc, nil)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/credentials", nil)
		req = attachAdminTenantContext(req, orgID)

		rec := httptest.NewRecorder()
		h.List(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", rec.Code)
		}

		var resp httpapi.ResponseEnvelope
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		items, ok := resp.Data.([]any)
		if !ok || len(items) != 0 {
			t.Errorf("expected empty array, got: %v", resp.Data)
		}
	})

	t.Run("converts service error to 503", func(t *testing.T) {
		svc := &fakeCredentialService{
			listErr: domain.ErrCredentialServiceUnavailable,
		}
		h := NewHandler(svc, nil)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/credentials", nil)
		req = attachAdminTenantContext(req, orgID)

		rec := httptest.NewRecorder()
		h.List(rec, req)

		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("expected 503 for list failure, got %d", rec.Code)
		}
	})
}

func TestCredentialHandler_AuthorizationGuards(t *testing.T) {
	orgID := uuid.New()
	credID := uuid.New()
	svc := &fakeCredentialService{
		currentMeta: &domain.CredentialMetadata{
			ID:             credID,
			OrganizationID: orgID,
			Name:           "Key",
			Type:           domain.TypeSSHPassword,
		},
	}
	h := NewHandler(svc, nil)

	// Build handlers wrapped with RequireOrganizationAdmin
	listHandler := orgHttpapi.RequireOrganizationAdmin(nil)(http.HandlerFunc(h.List))
	createHandler := orgHttpapi.RequireOrganizationAdmin(nil)(http.HandlerFunc(h.Create))
	updateHandler := orgHttpapi.RequireOrganizationAdmin(nil)(http.HandlerFunc(h.Update))
	deleteHandler := orgHttpapi.RequireOrganizationAdmin(nil)(http.HandlerFunc(h.Delete))

	t.Run("admin role is allowed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/credentials", nil)
		req = attachAdminTenantContext(req, orgID)
		rec := httptest.NewRecorder()
		listHandler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("expected 200 for admin, got %d", rec.Code)
		}
	})

	t.Run("member role receives 403 Forbidden on all endpoints", func(t *testing.T) {
		tenantCtx := &orgHttpapi.TenantContext{
			OrganizationID:   orgID,
			UserID:           uuid.New(),
			Role:             orgDomain.RoleMember,
			MembershipStatus: orgDomain.MemberStatusActive,
		}
		req := httptest.NewRequest(http.MethodGet, "/api/v1/credentials", nil)
		req.SetPathValue("id", credID.String())
		req = req.WithContext(orgHttpapi.WithTenantContext(req.Context(), tenantCtx))

		rec := httptest.NewRecorder()
		listHandler.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("expected 403 for member role, got %d", rec.Code)
		}

		recCreate := httptest.NewRecorder()
		createHandler.ServeHTTP(recCreate, req)
		if recCreate.Code != http.StatusForbidden {
			t.Errorf("expected 403 on create for member role, got %d", recCreate.Code)
		}

		recUpdate := httptest.NewRecorder()
		updateHandler.ServeHTTP(recUpdate, req)
		if recUpdate.Code != http.StatusForbidden {
			t.Errorf("expected 403 on update for member role, got %d", recUpdate.Code)
		}

		recDelete := httptest.NewRecorder()
		deleteHandler.ServeHTTP(recDelete, req)
		if recDelete.Code != http.StatusForbidden {
			t.Errorf("expected 403 on delete for member role, got %d", recDelete.Code)
		}
	})

	t.Run("viewer role receives 403 Forbidden on all endpoints", func(t *testing.T) {
		tenantCtx := &orgHttpapi.TenantContext{
			OrganizationID:   orgID,
			UserID:           uuid.New(),
			Role:             orgDomain.RoleViewer,
			MembershipStatus: orgDomain.MemberStatusActive,
		}
		req := httptest.NewRequest(http.MethodGet, "/api/v1/credentials", nil)
		req.SetPathValue("id", credID.String())
		req = req.WithContext(orgHttpapi.WithTenantContext(req.Context(), tenantCtx))

		rec := httptest.NewRecorder()
		listHandler.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("expected 403 for viewer role, got %d", rec.Code)
		}

		recUpdate := httptest.NewRecorder()
		updateHandler.ServeHTTP(recUpdate, req)
		if recUpdate.Code != http.StatusForbidden {
			t.Errorf("expected 403 on update for viewer role, got %d", recUpdate.Code)
		}

		recDelete := httptest.NewRecorder()
		deleteHandler.ServeHTTP(recDelete, req)
		if recDelete.Code != http.StatusForbidden {
			t.Errorf("expected 403 on delete for viewer role, got %d", recDelete.Code)
		}
	})

	t.Run("missing TenantContext fails closed with 403 Forbidden", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/credentials", nil)
		rec := httptest.NewRecorder()
		listHandler.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("expected 403 when tenant context is missing, got %d", rec.Code)
		}
	})
}

func TestClearCreateCredentialRequest_Direct(t *testing.T) {
	pass := "secret-passphrase"
	req := &CreateCredentialRequest{
		Name:       "Test",
		Type:       "ssh_private_key",
		Secret:     "secret-value",
		Passphrase: &pass,
	}

	clearCreateCredentialRequest(req)

	if req.Secret != "" {
		t.Errorf("expected secret to be empty string")
	}
	if req.Passphrase != nil {
		t.Errorf("expected passphrase pointer to be nil")
	}
}

func TestCredentialHandler_Create_PayloadZeroization_Success(t *testing.T) {
	orgID := uuid.New()
	svc := &fakeCredentialService{}
	h := NewHandler(svc, nil)

	body := map[string]any{
		"name":   "Production SSH Password",
		"type":   "ssh_password",
		"secret": "super-secret-password-123",
	}
	jsonBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/credentials", bytes.NewReader(jsonBytes))
	req.Header.Set("Content-Type", "application/json")
	req = attachAdminTenantContext(req, orgID)

	rec := httptest.NewRecorder()
	h.Create(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d", rec.Code)
	}

	// 1. Snapshot taken during CreateCredential call must contain valid JSON payload
	snap, err := payload.Decode(svc.bufferSnapshotOnCreate)
	if err != nil {
		t.Fatalf("failed to decode snapshot payload: %v", err)
	}
	if snap.Secret != "super-secret-password-123" {
		t.Errorf("unexpected secret in snapshot")
	}

	// 2. The captured payload slice reference in the service must be all zeroes after handler returns
	allZero := make([]byte, len(svc.capturedPayload))
	if !bytes.Equal(svc.capturedPayload, allZero) {
		t.Errorf("SECURITY FLAW: handler did not zero intermediate payload buffer after service call")
	}
}

func TestCredentialHandler_Create_PayloadZeroization_Failure(t *testing.T) {
	orgID := uuid.New()
	svc := &fakeCredentialService{
		createErr: domain.ErrCredentialServiceUnavailable,
	}
	h := NewHandler(svc, nil)

	body := map[string]any{
		"name":   "Production SSH Password",
		"type":   "ssh_password",
		"secret": "super-secret-password-123",
	}
	jsonBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/credentials", bytes.NewReader(jsonBytes))
	req.Header.Set("Content-Type", "application/json")
	req = attachAdminTenantContext(req, orgID)

	rec := httptest.NewRecorder()
	h.Create(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 Service Unavailable, got %d", rec.Code)
	}

	// The captured payload slice reference in the service must be all zeroes after handler returns
	allZero := make([]byte, len(svc.capturedPayload))
	if !bytes.Equal(svc.capturedPayload, allZero) {
		t.Errorf("SECURITY FLAW: handler did not zero intermediate payload buffer on failure")
	}
}

func TestCredentialHandler_Create_MalformedJSON(t *testing.T) {
	orgID := uuid.New()
	svc := &fakeCredentialService{}
	h := NewHandler(svc, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/credentials", strings.NewReader(`{"name":`))
	req.Header.Set("Content-Type", "application/json")
	req = attachAdminTenantContext(req, orgID)

	rec := httptest.NewRecorder()
	h.Create(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request, got %d", rec.Code)
	}
	var errEnv httpapi.ErrorEnvelope
	_ = json.Unmarshal(rec.Body.Bytes(), &errEnv)
	if errEnv.Error.Code != "BAD_REQUEST" {
		t.Errorf("expected code BAD_REQUEST, got %s", errEnv.Error.Code)
	}
	if svc.bufferSnapshotOnCreate != nil {
		t.Errorf("service should not have been called on malformed JSON")
	}
}

func TestCredentialHandler_Create_OversizedBody(t *testing.T) {
	orgID := uuid.New()
	svc := &fakeCredentialService{}
	h := NewHandler(svc, nil)

	largeSecret := strings.Repeat("A", httpapi.MaxRequestBodyBytes+1024)
	largeBody := `{"name":"Large","type":"ssh_password","secret":"` + largeSecret + `"}`

	req := httptest.NewRequest(http.MethodPost, "/api/v1/credentials", strings.NewReader(largeBody))
	req.Header.Set("Content-Type", "application/json")
	req = attachAdminTenantContext(req, orgID)

	rec := httptest.NewRecorder()
	h.Create(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request on oversized body, got %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), largeSecret[:64]) {
		t.Errorf("SECURITY FLAW: request body content echoed in error response")
	}
	if svc.bufferSnapshotOnCreate != nil {
		t.Errorf("service should not have been called on oversized request")
	}
}

func TestCredentialHandler_Create_CanonicalInternalServerError(t *testing.T) {
	orgID := uuid.New()
	rawInternalErr := errors.New("unhandled internal crash at /sys/kernel/panic")
	svc := &fakeCredentialService{
		createErr: rawInternalErr,
	}
	h := NewHandler(svc, nil)

	body := map[string]any{
		"name":   "Test",
		"type":   "ssh_password",
		"secret": "secret",
	}
	jsonBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/credentials", bytes.NewReader(jsonBytes))
	req.Header.Set("Content-Type", "application/json")
	req = attachAdminTenantContext(req, orgID)

	rec := httptest.NewRecorder()
	h.Create(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 Internal Server Error, got %d", rec.Code)
	}

	var errEnv httpapi.ErrorEnvelope
	_ = json.Unmarshal(rec.Body.Bytes(), &errEnv)
	if errEnv.Error.Code != "INTERNAL_SERVER_ERROR" {
		t.Errorf("expected code INTERNAL_SERVER_ERROR, got %s", errEnv.Error.Code)
	}
	if strings.Contains(rec.Body.String(), "/sys/kernel/panic") {
		t.Errorf("SECURITY FLAW: raw internal error detail leaked in response: %s", rec.Body.String())
	}
}

func TestClearUpdateCredentialRequest_Direct(t *testing.T) {
	sec := "secret-val"
	pass := "passphrase-val"
	name := "Name"
	req := &UpdateCredentialRequest{
		Name:       &name,
		Secret:     &sec,
		Passphrase: &pass,
	}

	clearUpdateCredentialRequest(req)

	if req.Secret != nil {
		t.Errorf("expected secret pointer to be nil")
	}
	if req.Passphrase != nil {
		t.Errorf("expected passphrase pointer to be nil")
	}
}

func TestCredentialHandler_Update(t *testing.T) {
	orgID := uuid.New()
	credID := uuid.New()
	now := time.Now().UTC()
	fp := "SHA256:orig-fp"

	setupExistingKey := func() *fakeCredentialService {
		return &fakeCredentialService{
			currentMeta: &domain.CredentialMetadata{
				ID:             credID,
				OrganizationID: orgID,
				Name:           "Original Key",
				Type:           domain.TypeSSHPrivateKey,
				Fingerprint:    &fp,
				KeyVersion:     1,
				CreatedAt:      now,
				UpdatedAt:      now,
			},
		}
	}

	setupExistingPassword := func() *fakeCredentialService {
		return &fakeCredentialService{
			currentMeta: &domain.CredentialMetadata{
				ID:             credID,
				OrganizationID: orgID,
				Name:           "Original Password",
				Type:           domain.TypeSSHPassword,
				KeyVersion:     1,
				CreatedAt:      now,
				UpdatedAt:      now,
			},
		}
	}

	t.Run("successfully updates name only with 200 OK (no crypto, fingerprint unchanged)", func(t *testing.T) {
		svc := setupExistingKey()
		h := NewHandler(svc, nil)

		body := map[string]any{"name": "New Key Name"}
		jsonBytes, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPut, "/api/v1/credentials/"+credID.String(), bytes.NewReader(jsonBytes))
		req.Header.Set("Content-Type", "application/json")
		req.SetPathValue("id", credID.String())
		req = attachAdminTenantContext(req, orgID)

		rec := httptest.NewRecorder()
		h.Update(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d. Body: %s", rec.Code, rec.Body.String())
		}
		if svc.updateNameCalls != 1 {
			t.Errorf("expected 1 updateName call, got %d", svc.updateNameCalls)
		}
		if svc.replaceSecretCalls != 0 {
			t.Errorf("expected 0 replaceSecret calls on name-only update, got %d", svc.replaceSecretCalls)
		}

		var resp struct {
			Data CredentialUpdateResponse `json:"data"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.Data.Name != "New Key Name" {
			t.Errorf("expected name 'New Key Name', got %s", resp.Data.Name)
		}
		if resp.Data.Fingerprint == nil || *resp.Data.Fingerprint != fp {
			t.Errorf("expected fingerprint preserved as %s, got %v", fp, resp.Data.Fingerprint)
		}
	})

	t.Run("successfully updates secret for ssh_password with 200 OK", func(t *testing.T) {
		svc := setupExistingPassword()
		h := NewHandler(svc, nil)

		body := map[string]any{"secret": "new-ssh-password-999"}
		jsonBytes, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPut, "/api/v1/credentials/"+credID.String(), bytes.NewReader(jsonBytes))
		req.Header.Set("Content-Type", "application/json")
		req.SetPathValue("id", credID.String())
		req = attachAdminTenantContext(req, orgID)

		rec := httptest.NewRecorder()
		h.Update(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d. Body: %s", rec.Code, rec.Body.String())
		}
		if svc.replaceSecretCalls != 1 {
			t.Errorf("expected 1 replaceSecret call")
		}
	})

	t.Run("successfully updates secret for ssh_private_key and recomputes SHA256 fingerprint", func(t *testing.T) {
		svc := setupExistingKey()
		h := NewHandler(svc, nil)

		newPrivKey, expectedFP := generateTestED25519Key(t)
		body := map[string]any{"secret": newPrivKey}
		jsonBytes, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPut, "/api/v1/credentials/"+credID.String(), bytes.NewReader(jsonBytes))
		req.Header.Set("Content-Type", "application/json")
		req.SetPathValue("id", credID.String())
		req = attachAdminTenantContext(req, orgID)

		rec := httptest.NewRecorder()
		h.Update(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d. Body: %s", rec.Code, rec.Body.String())
		}
		if svc.capturedFingerprint == nil || *svc.capturedFingerprint != expectedFP {
			t.Errorf("expected recomputed fingerprint %s, got %v", expectedFP, svc.capturedFingerprint)
		}
	})

	t.Run("successfully updates encrypted ssh_private_key with valid passphrase", func(t *testing.T) {
		svc := setupExistingKey()
		h := NewHandler(svc, nil)

		pass := "valid-passphrase-123"
		encPrivKey, expectedFP := generateTestEncryptedED25519Key(t, pass)
		body := map[string]any{
			"secret":     encPrivKey,
			"passphrase": pass,
		}
		jsonBytes, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPut, "/api/v1/credentials/"+credID.String(), bytes.NewReader(jsonBytes))
		req.Header.Set("Content-Type", "application/json")
		req.SetPathValue("id", credID.String())
		req = attachAdminTenantContext(req, orgID)

		rec := httptest.NewRecorder()
		h.Update(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d. Body: %s", rec.Code, rec.Body.String())
		}
		if svc.capturedFingerprint == nil || *svc.capturedFingerprint != expectedFP {
			t.Errorf("expected fingerprint %s, got %v", expectedFP, svc.capturedFingerprint)
		}
	})

	t.Run("rejects invalid SSH private key format with 422 VALIDATION_FAILED", func(t *testing.T) {
		svc := setupExistingKey()
		h := NewHandler(svc, nil)

		body := map[string]any{"secret": "not-a-valid-ssh-key"}
		jsonBytes, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPut, "/api/v1/credentials/"+credID.String(), bytes.NewReader(jsonBytes))
		req.Header.Set("Content-Type", "application/json")
		req.SetPathValue("id", credID.String())
		req = attachAdminTenantContext(req, orgID)

		rec := httptest.NewRecorder()
		h.Update(rec, req)

		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("expected 422 for invalid SSH key, got %d", rec.Code)
		}
	})

	t.Run("rejects encrypted SSH key when passphrase is wrong with 422 VALIDATION_FAILED", func(t *testing.T) {
		svc := setupExistingKey()
		h := NewHandler(svc, nil)

		encPrivKey, _ := generateTestEncryptedED25519Key(t, "correct-passphrase")
		body := map[string]any{
			"secret":     encPrivKey,
			"passphrase": "wrong-passphrase",
		}
		jsonBytes, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPut, "/api/v1/credentials/"+credID.String(), bytes.NewReader(jsonBytes))
		req.Header.Set("Content-Type", "application/json")
		req.SetPathValue("id", credID.String())
		req = attachAdminTenantContext(req, orgID)

		rec := httptest.NewRecorder()
		h.Update(rec, req)

		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("expected 422 for wrong passphrase, got %d", rec.Code)
		}
	})

	t.Run("rejects passphrase when secret is omitted with 422 VALIDATION_FAILED", func(t *testing.T) {
		svc := setupExistingKey()
		h := NewHandler(svc, nil)

		body := map[string]any{
			"name":       "New Name",
			"passphrase": "some-passphrase",
		}
		jsonBytes, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPut, "/api/v1/credentials/"+credID.String(), bytes.NewReader(jsonBytes))
		req.Header.Set("Content-Type", "application/json")
		req.SetPathValue("id", credID.String())
		req = attachAdminTenantContext(req, orgID)

		rec := httptest.NewRecorder()
		h.Update(rec, req)

		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("expected 422 when passphrase given without secret, got %d", rec.Code)
		}
	})

	t.Run("rejects passphrase on non-ssh_private_key types with 422 VALIDATION_FAILED", func(t *testing.T) {
		svc := setupExistingPassword()
		h := NewHandler(svc, nil)

		body := map[string]any{
			"secret":     "new-password",
			"passphrase": "invalid-passphrase-usage",
		}
		jsonBytes, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPut, "/api/v1/credentials/"+credID.String(), bytes.NewReader(jsonBytes))
		req.Header.Set("Content-Type", "application/json")
		req.SetPathValue("id", credID.String())
		req = attachAdminTenantContext(req, orgID)

		rec := httptest.NewRecorder()
		h.Update(rec, req)

		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("expected 422 for passphrase on password type, got %d", rec.Code)
		}
	})

	t.Run("rejects empty PUT payload {} with 422 VALIDATION_FAILED", func(t *testing.T) {
		svc := setupExistingKey()
		h := NewHandler(svc, nil)

		req := httptest.NewRequest(http.MethodPut, "/api/v1/credentials/"+credID.String(), strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		req.SetPathValue("id", credID.String())
		req = attachAdminTenantContext(req, orgID)

		rec := httptest.NewRecorder()
		h.Update(rec, req)

		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("expected 422 on empty JSON object {}, got %d", rec.Code)
		}
		if svc.updateNameCalls != 0 || svc.replaceSecretCalls != 0 {
			t.Errorf("service should not have been called on empty update")
		}
	})

	t.Run("rejects empty name with 422 VALIDATION_FAILED", func(t *testing.T) {
		svc := setupExistingKey()
		h := NewHandler(svc, nil)

		body := map[string]any{"name": "   "}
		jsonBytes, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPut, "/api/v1/credentials/"+credID.String(), bytes.NewReader(jsonBytes))
		req.Header.Set("Content-Type", "application/json")
		req.SetPathValue("id", credID.String())
		req = attachAdminTenantContext(req, orgID)

		rec := httptest.NewRecorder()
		h.Update(rec, req)

		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("expected 422 for empty name, got %d", rec.Code)
		}
	})

	t.Run("rejects name longer than 100 chars with 422 VALIDATION_FAILED", func(t *testing.T) {
		svc := setupExistingKey()
		h := NewHandler(svc, nil)

		body := map[string]any{"name": strings.Repeat("A", 101)}
		jsonBytes, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPut, "/api/v1/credentials/"+credID.String(), bytes.NewReader(jsonBytes))
		req.Header.Set("Content-Type", "application/json")
		req.SetPathValue("id", credID.String())
		req = attachAdminTenantContext(req, orgID)

		rec := httptest.NewRecorder()
		h.Update(rec, req)

		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("expected 422 for too long name, got %d", rec.Code)
		}
	})

	t.Run("rejects empty secret with 422 VALIDATION_FAILED", func(t *testing.T) {
		svc := setupExistingKey()
		h := NewHandler(svc, nil)

		body := map[string]any{"secret": ""}
		jsonBytes, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPut, "/api/v1/credentials/"+credID.String(), bytes.NewReader(jsonBytes))
		req.Header.Set("Content-Type", "application/json")
		req.SetPathValue("id", credID.String())
		req = attachAdminTenantContext(req, orgID)

		rec := httptest.NewRecorder()
		h.Update(rec, req)

		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("expected 422 for empty secret, got %d", rec.Code)
		}
	})

	t.Run("rejects immutable/unknown JSON fields with 400 BAD_REQUEST", func(t *testing.T) {
		svc := setupExistingKey()
		h := NewHandler(svc, nil)

		immutableFields := []string{"type", "organization_id", "id", "fingerprint", "key_version", "encrypted_secret", "nonce", "auth_tag"}
		for _, field := range immutableFields {
			t.Run(field, func(t *testing.T) {
				body := map[string]any{
					"name": "Valid Name",
					field:  "immutable-value",
				}
				jsonBytes, _ := json.Marshal(body)

				req := httptest.NewRequest(http.MethodPut, "/api/v1/credentials/"+credID.String(), bytes.NewReader(jsonBytes))
				req.Header.Set("Content-Type", "application/json")
				req.SetPathValue("id", credID.String())
				req = attachAdminTenantContext(req, orgID)

				rec := httptest.NewRecorder()
				h.Update(rec, req)

				if rec.Code != http.StatusBadRequest {
					t.Errorf("expected 400 Bad Request for immutable field %s, got %d", field, rec.Code)
				}
			})
		}
	})

	t.Run("rejects invalid UUID in path with 400 BAD_REQUEST", func(t *testing.T) {
		svc := setupExistingKey()
		h := NewHandler(svc, nil)

		body := map[string]any{"name": "Valid Name"}
		jsonBytes, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPut, "/api/v1/credentials/invalid-uuid", bytes.NewReader(jsonBytes))
		req.Header.Set("Content-Type", "application/json")
		req.SetPathValue("id", "invalid-uuid")
		req = attachAdminTenantContext(req, orgID)

		rec := httptest.NewRecorder()
		h.Update(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request for invalid UUID, got %d", rec.Code)
		}
	})

	t.Run("returns 404 CREDENTIAL_NOT_FOUND when credential does not exist or cross-tenant", func(t *testing.T) {
		svc := &fakeCredentialService{
			getMetaErr: domain.ErrCredentialNotFound,
		}
		h := NewHandler(svc, nil)

		body := map[string]any{"secret": "new-secret"}
		jsonBytes, _ := json.Marshal(body)

		nonExistentID := uuid.New()
		req := httptest.NewRequest(http.MethodPut, "/api/v1/credentials/"+nonExistentID.String(), bytes.NewReader(jsonBytes))
		req.Header.Set("Content-Type", "application/json")
		req.SetPathValue("id", nonExistentID.String())
		req = attachAdminTenantContext(req, orgID)

		rec := httptest.NewRecorder()
		h.Update(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 Not Found, got %d", rec.Code)
		}
		var errEnv httpapi.ErrorEnvelope
		_ = json.Unmarshal(rec.Body.Bytes(), &errEnv)
		if errEnv.Error.Code != "CREDENTIAL_NOT_FOUND" {
			t.Errorf("expected code CREDENTIAL_NOT_FOUND, got %s", errEnv.Error.Code)
		}
	})

	t.Run("secret non-disclosure guarantee in update response", func(t *testing.T) {
		svc := setupExistingPassword()
		h := NewHandler(svc, nil)

		topSecretVal := "SUPER_CONFIDENTIAL_ROTATED_PASSWORD_999"
		body := map[string]any{
			"secret": topSecretVal,
		}
		jsonBytes, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPut, "/api/v1/credentials/"+credID.String(), bytes.NewReader(jsonBytes))
		req.Header.Set("Content-Type", "application/json")
		req.SetPathValue("id", credID.String())
		req = attachAdminTenantContext(req, orgID)

		rec := httptest.NewRecorder()
		h.Update(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", rec.Code)
		}
		respBody := rec.Body.String()
		if strings.Contains(respBody, topSecretVal) {
			t.Errorf("SECURITY FLAW: plaintext secret leaked in update response")
		}
		if strings.Contains(respBody, "encrypted_secret") || strings.Contains(respBody, "nonce") || strings.Contains(respBody, "auth_tag") {
			t.Errorf("SECURITY FLAW: encrypted internal fields leaked in update response")
		}
	})

	t.Run("payload zeroization on update success and failure", func(t *testing.T) {
		t.Run("success zeroes intermediate buffer", func(t *testing.T) {
			svc := setupExistingPassword()
			h := NewHandler(svc, nil)

			body := map[string]any{"secret": "rot-secret-1"}
			jsonBytes, _ := json.Marshal(body)

			req := httptest.NewRequest(http.MethodPut, "/api/v1/credentials/"+credID.String(), bytes.NewReader(jsonBytes))
			req.Header.Set("Content-Type", "application/json")
			req.SetPathValue("id", credID.String())
			req = attachAdminTenantContext(req, orgID)

			rec := httptest.NewRecorder()
			h.Update(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("expected 200 OK, got %d", rec.Code)
			}
			allZero := make([]byte, len(svc.capturedPayload))
			if !bytes.Equal(svc.capturedPayload, allZero) {
				t.Errorf("SECURITY FLAW: payload buffer was not zeroed after successful update")
			}
		})

		t.Run("failure zeroes intermediate buffer", func(t *testing.T) {
			svc := setupExistingPassword()
			svc.replaceSecretErr = domain.ErrCredentialServiceUnavailable
			h := NewHandler(svc, nil)

			body := map[string]any{"secret": "rot-secret-2"}
			jsonBytes, _ := json.Marshal(body)

			req := httptest.NewRequest(http.MethodPut, "/api/v1/credentials/"+credID.String(), bytes.NewReader(jsonBytes))
			req.Header.Set("Content-Type", "application/json")
			req.SetPathValue("id", credID.String())
			req = attachAdminTenantContext(req, orgID)

			rec := httptest.NewRecorder()
			h.Update(rec, req)

			if rec.Code != http.StatusServiceUnavailable {
				t.Fatalf("expected 503, got %d", rec.Code)
			}
			allZero := make([]byte, len(svc.capturedPayload))
			if !bytes.Equal(svc.capturedPayload, allZero) {
				t.Errorf("SECURITY FLAW: payload buffer was not zeroed after failed update")
			}
		})
	})
}

func TestCredentialHandler_Delete(t *testing.T) {
	orgID := uuid.New()
	credID := uuid.New()

	t.Run("successfully hard-deletes unreferenced credential with 204 No Content and empty body", func(t *testing.T) {
		svc := &fakeCredentialService{
			currentMeta: &domain.CredentialMetadata{
				ID:             credID,
				OrganizationID: orgID,
			},
		}
		h := NewHandler(svc, nil)

		req := httptest.NewRequest(http.MethodDelete, "/api/v1/credentials/"+credID.String(), nil)
		req.SetPathValue("id", credID.String())
		req = attachAdminTenantContext(req, orgID)

		rec := httptest.NewRecorder()
		h.Delete(rec, req)

		if rec.Code != http.StatusNoContent {
			t.Fatalf("expected 204 No Content, got %d", rec.Code)
		}
		if rec.Body.Len() != 0 {
			t.Errorf("expected completely empty body for 204, got: %s", rec.Body.String())
		}
		if rec.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("expected Cache-Control: no-store header on 204 response")
		}
		if svc.deleteCalls != 1 {
			t.Errorf("expected 1 delete call")
		}
	})

	t.Run("returns 404 CREDENTIAL_NOT_FOUND when deleting non-existent or cross-tenant credential", func(t *testing.T) {
		svc := &fakeCredentialService{
			deleteErr: domain.ErrCredentialNotFound,
		}
		h := NewHandler(svc, nil)

		req := httptest.NewRequest(http.MethodDelete, "/api/v1/credentials/"+credID.String(), nil)
		req.SetPathValue("id", credID.String())
		req = attachAdminTenantContext(req, orgID)

		rec := httptest.NewRecorder()
		h.Delete(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 Not Found, got %d", rec.Code)
		}
		var errEnv httpapi.ErrorEnvelope
		_ = json.Unmarshal(rec.Body.Bytes(), &errEnv)
		if errEnv.Error.Code != "CREDENTIAL_NOT_FOUND" {
			t.Errorf("expected code CREDENTIAL_NOT_FOUND, got %s", errEnv.Error.Code)
		}
	})

	t.Run("returns 409 Conflict with CREDENTIAL_IN_USE when credential is referenced by resource connector", func(t *testing.T) {
		svc := &fakeCredentialService{
			deleteErr: domain.ErrCredentialInUse,
		}
		h := NewHandler(svc, nil)

		req := httptest.NewRequest(http.MethodDelete, "/api/v1/credentials/"+credID.String(), nil)
		req.SetPathValue("id", credID.String())
		req = attachAdminTenantContext(req, orgID)

		rec := httptest.NewRecorder()
		h.Delete(rec, req)

		if rec.Code != http.StatusConflict {
			t.Fatalf("expected 409 Conflict, got %d", rec.Code)
		}
		var errEnv httpapi.ErrorEnvelope
		_ = json.Unmarshal(rec.Body.Bytes(), &errEnv)
		if errEnv.Error.Code != "CREDENTIAL_IN_USE" {
			t.Errorf("expected code CREDENTIAL_IN_USE, got %s", errEnv.Error.Code)
		}
	})

	t.Run("returns 400 BAD_REQUEST on invalid UUID in path", func(t *testing.T) {
		svc := &fakeCredentialService{}
		h := NewHandler(svc, nil)

		req := httptest.NewRequest(http.MethodDelete, "/api/v1/credentials/bad-id", nil)
		req.SetPathValue("id", "bad-id")
		req = attachAdminTenantContext(req, orgID)

		rec := httptest.NewRecorder()
		h.Delete(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request, got %d", rec.Code)
		}
	})

	t.Run("returns 503 SERVICE_UNAVAILABLE on database/infrastructure failure", func(t *testing.T) {
		svc := &fakeCredentialService{
			deleteErr: domain.ErrCredentialServiceUnavailable,
		}
		h := NewHandler(svc, nil)

		req := httptest.NewRequest(http.MethodDelete, "/api/v1/credentials/"+credID.String(), nil)
		req.SetPathValue("id", credID.String())
		req = attachAdminTenantContext(req, orgID)

		rec := httptest.NewRecorder()
		h.Delete(rec, req)

		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("expected 503 Service Unavailable, got %d", rec.Code)
		}
	})
}

func TestHandler_S3Credentials(t *testing.T) {
	orgID := uuid.New()
	credID := uuid.New()

	t.Run("Create s3_credentials successfully encodes S3PayloadV1", func(t *testing.T) {
		svc := &fakeCredentialService{
			createdMeta: &domain.CredentialMetadata{
				ID:             credID,
				OrganizationID: orgID,
				Name:           "Production S3 Key",
				Type:           domain.TypeS3Credentials,
				CreatedAt:      time.Now(),
			},
		}
		h := NewHandler(svc, nil)

		body := map[string]any{
			"name":              "Production S3 Key",
			"type":              "s3_credentials",
			"access_key_id":     "AKIAIOSFODNN7EXAMPLE",
			"secret_access_key": "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		}
		raw, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/credentials", bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		req = attachAdminTenantContext(req, orgID)

		rec := httptest.NewRecorder()
		h.Create(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created, got %d: %s", rec.Code, rec.Body.String())
		}

		if svc.capturedType != domain.TypeS3Credentials {
			t.Fatalf("expected capturedType %s, got %s", domain.TypeS3Credentials, svc.capturedType)
		}

		s3Payload, err := payload.DecodeS3(svc.bufferSnapshotOnCreate)
		if err != nil {
			t.Fatalf("failed decoding captured S3 payload: %v", err)
		}
		if s3Payload.AccessKeyID != "AKIAIOSFODNN7EXAMPLE" {
			t.Fatalf("expected access key AKIAIOSFODNN7EXAMPLE, got %s", s3Payload.AccessKeyID)
		}
		if s3Payload.SecretAccessKey != "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY" {
			t.Fatalf("expected secret key wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY, got %s", s3Payload.SecretAccessKey)
		}
	})

	t.Run("Create s3_credentials missing secret_access_key fails with 422", func(t *testing.T) {
		svc := &fakeCredentialService{}
		h := NewHandler(svc, nil)

		body := map[string]any{
			"name":          "Incomplete S3 Key",
			"type":          "s3_credentials",
			"access_key_id": "AKIAIOSFODNN7EXAMPLE",
		}
		raw, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/credentials", bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		req = attachAdminTenantContext(req, orgID)

		rec := httptest.NewRecorder()
		h.Create(rec, req)

		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("expected 422 Unprocessable Entity, got %d", rec.Code)
		}
	})

	t.Run("Update s3_credentials successfully replaces S3PayloadV1", func(t *testing.T) {
		svc := &fakeCredentialService{
			currentMeta: &domain.CredentialMetadata{
				ID:             credID,
				OrganizationID: orgID,
				Name:           "Old S3 Key",
				Type:           domain.TypeS3Credentials,
			},
		}
		h := NewHandler(svc, nil)

		body := map[string]any{
			"name":              "Updated S3 Key",
			"access_key_id":     "NEWAKIAIOSFODNN7EXAMPLE",
			"secret_access_key": "NEWsecretkey1234567890",
		}
		raw, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPut, "/api/v1/credentials/"+credID.String(), bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		req.SetPathValue("id", credID.String())
		req = attachAdminTenantContext(req, orgID)

		rec := httptest.NewRecorder()
		h.Update(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
		}

		s3Payload, err := payload.DecodeS3(svc.bufferSnapshotOnUpdate)
		if err != nil {
			t.Fatalf("failed decoding captured updated S3 payload: %v", err)
		}
		if s3Payload.AccessKeyID != "NEWAKIAIOSFODNN7EXAMPLE" {
			t.Fatalf("expected new access key, got %s", s3Payload.AccessKeyID)
		}
	})
}
