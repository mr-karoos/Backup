package s3

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"backup-platform/internal/storage"
	"backup-platform/pkg/uuid"
)

func TestEndpointSecurityPolicy_ValidateEndpointURL(t *testing.T) {
	policyStrict := EndpointSecurityPolicy{AllowInsecureHTTP: false}
	policyInsecure := EndpointSecurityPolicy{AllowInsecureHTTP: true}

	tests := []struct {
		name        string
		policy      EndpointSecurityPolicy
		url         string
		expectError bool
	}{
		{"valid https", policyStrict, "https://s3.amazonaws.com", false},
		{"valid https custom port", policyStrict, "https://my-minio.example.com:9000", false},
		{"empty url default", policyStrict, "", false},
		{"insecure http rejected in strict", policyStrict, "http://my-minio.example.com:9000", true},
		{"insecure http allowed when permitted", policyInsecure, "http://localhost:9000", false},
		{"user credentials rejected", policyStrict, "https://user:pass@s3.amazonaws.com", true},
		{"unsupported scheme ftp", policyStrict, "ftp://s3.amazonaws.com", true},
		{"url with path rejected", policyStrict, "https://s3.amazonaws.com/some/path", true},
		{"url with query string rejected", policyStrict, "https://s3.amazonaws.com?foo=bar", true},
		{"url with fragment rejected", policyStrict, "https://s3.amazonaws.com#frag", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.policy.ValidateEndpointURL(tt.url)
			if (err != nil) != tt.expectError {
				t.Errorf("ValidateEndpointURL(%q) err = %v, expected error: %v", tt.url, err, tt.expectError)
			}
		})
	}
}

func TestEndpointSecurityPolicy_SSRFProtection(t *testing.T) {
	policyNoAllowlistStrict := EndpointSecurityPolicy{
		AllowInsecureHTTP: false,
		PrivateAllowlist:  nil,
	}

	policyWithAllowlistStrict := EndpointSecurityPolicy{
		AllowInsecureHTTP: false,
		PrivateAllowlist:  []string{"minio.internal", "192.168.1.100", "10.50.0.0/16", "localhost", "127.0.0.1"},
	}

	policyWithAllowlistInsecure := EndpointSecurityPolicy{
		AllowInsecureHTTP: true,
		PrivateAllowlist:  []string{"minio.internal", "192.168.1.100", "10.50.0.0/16", "localhost", "127.0.0.1"},
	}

	tests := []struct {
		name       string
		policy     EndpointSecurityPolicy
		host       string
		ipStr      string
		expectPass bool
	}{
		// Default strict (no allowlist)
		{"loopback ipv4 blocked by default", policyNoAllowlistStrict, "localhost", "127.0.0.1", false},
		{"loopback ipv6 blocked by default", policyNoAllowlistStrict, "localhost", "::1", false},
		{"cloud metadata blocked", policyNoAllowlistStrict, "169.254.169.254", "169.254.169.254", false},
		{"link-local blocked", policyNoAllowlistStrict, "link-local", "169.254.10.20", false},
		{"multicast blocked", policyNoAllowlistStrict, "multicast", "224.0.0.1", false},
		{"private 10.x blocked by default", policyNoAllowlistStrict, "internal.db", "10.0.0.5", false},
		{"private 172.16.x blocked by default", policyNoAllowlistStrict, "internal.db", "172.16.0.10", false},
		{"private 192.168.x blocked by default", policyNoAllowlistStrict, "internal.db", "192.168.1.50", false},
		{"public ip allowed", policyNoAllowlistStrict, "s3.amazonaws.com", "52.216.1.1", true},

		// Strict mode with allowlist: loopback remains blocked when AllowInsecureHTTP=false
		{"loopback blocked in strict even if allowlisted", policyWithAllowlistStrict, "localhost", "127.0.0.1", false},
		{"cloud metadata blocked even if allowlisted", policyWithAllowlistStrict, "169.254.169.254", "169.254.169.254", false},
		{"allowlisted private IP allowed in strict", policyWithAllowlistStrict, "192.168.1.100", "192.168.1.100", true},

		// Insecure development mode with allowlist: local MinIO loopback allowed
		{"loopback allowed in insecure mode when explicitly allowlisted", policyWithAllowlistInsecure, "localhost", "127.0.0.1", true},
		{"allowlisted hostname allowed in insecure mode", policyWithAllowlistInsecure, "minio.internal", "10.0.0.5", true},
		{"allowlisted CIDR allowed in insecure mode", policyWithAllowlistInsecure, "other-host", "10.50.2.3", true},
		{"non-allowlisted private IP still blocked in insecure mode", policyWithAllowlistInsecure, "other-host", "192.168.1.101", false},
		{"cloud metadata still blocked even in insecure mode with allowlist", policyWithAllowlistInsecure, "169.254.169.254", "169.254.169.254", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip := net.ParseIP(tt.ipStr)
			if ip == nil {
				t.Fatalf("failed to parse test IP %s", tt.ipStr)
			}
			allowed := tt.policy.IsIPAllowed(tt.host, ip)
			if allowed != tt.expectPass {
				t.Errorf("IsIPAllowed(%s, %s) = %v, expected %v", tt.host, tt.ipStr, allowed, tt.expectPass)
			}
		})
	}
}

func TestEndpointSecurityPolicy_EnvironmentProxyCannotBypass(t *testing.T) {
	// Set environment proxies that might attempt to hijack traffic
	t.Setenv("HTTP_PROXY", "http://malicious-proxy.internal:8080")
	t.Setenv("HTTPS_PROXY", "http://malicious-proxy.internal:8080")
	t.Setenv("ALL_PROXY", "socks5://malicious-proxy.internal:1080")

	policy := &EndpointSecurityPolicy{
		AllowInsecureHTTP: false,
	}

	client := policy.NewSecureHTTPClient()
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected *http.Transport, got %T", client.Transport)
	}

	// Transport.Proxy must be explicitly nil to guarantee direct transport
	if transport.Proxy != nil {
		t.Fatalf("transport.Proxy must be nil, got non-nil function")
	}
}

func TestEndpointSecurityPolicy_RedirectRejected(t *testing.T) {
	// Server that attempts an HTTP 302 redirect
	redirectTarget := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer redirectTarget.Close()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, redirectTarget.URL, http.StatusFound)
	}))
	defer srv.Close()

	policy := &EndpointSecurityPolicy{
		AllowInsecureHTTP: true,
		PrivateAllowlist:  []string{"127.0.0.1"},
	}

	client := policy.NewSecureHTTPClient()
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	_, err := client.Do(req)
	if err == nil {
		t.Fatalf("expected redirect to be rejected with ErrRedirectNotAllowed, got nil error")
	}
	if !errors.Is(err, ErrRedirectNotAllowed) && !strings.Contains(err.Error(), "http redirects are not allowed") {
		t.Errorf("expected ErrRedirectNotAllowed, got: %v", err)
	}
}

func TestS3StorageProvider_BuildObjectKey(t *testing.T) {
	orgID := uuid.New()
	resID := uuid.New()
	artID := uuid.New()

	pNoPrefix := &S3StorageProvider{prefix: ""}
	key1 := pNoPrefix.BuildObjectKey(orgID, resID, artID, ".sql.gz")
	expected1 := fmt.Sprintf("organizations/%s/resources/%s/artifacts/%s.sql.gz", orgID, resID, artID)
	if key1 != expected1 {
		t.Errorf("expected key %s, got %s", expected1, key1)
	}

	pWithPrefix := &S3StorageProvider{prefix: "custom-prefix"}
	key2 := pWithPrefix.BuildObjectKey(orgID, resID, artID, "tar.gz")
	expected2 := fmt.Sprintf("custom-prefix/organizations/%s/resources/%s/artifacts/%s.tar.gz", orgID, resID, artID)
	if key2 != expected2 {
		t.Errorf("expected key %s, got %s", expected2, key2)
	}
}

// Mock S3 server for lifecycle testing
type mockS3Server struct {
	mu      sync.Mutex
	objects map[string][]byte
}

func newMockS3Server() (*httptest.Server, *mockS3Server) {
	m := &mockS3Server{
		objects: make(map[string][]byte),
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		defer m.mu.Unlock()

		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if len(parts) == 0 {
			w.WriteHeader(http.StatusOK)
			return
		}

		bucket := parts[0]
		_ = bucket

		if r.Method == http.MethodHead && len(parts) == 1 {
			// HeadBucket
			w.WriteHeader(http.StatusOK)
			return
		}

		key := strings.Join(parts[1:], "/")

		switch r.Method {
		case http.MethodPut:
			data, err := io.ReadAll(r.Body)
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			m.objects[key] = data
			w.Header().Set("ETag", `"mock-etag"`)
			w.WriteHeader(http.StatusOK)

		case http.MethodGet:
			data, ok := m.objects[key]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(data)

		case http.MethodDelete:
			delete(m.objects, key)
			w.WriteHeader(http.StatusNoContent)

		default:
			w.WriteHeader(http.StatusOK)
		}
	}))

	return srv, m
}

func TestS3StorageProvider_EndToEnd(t *testing.T) {
	srv, mockS3 := newMockS3Server()
	defer srv.Close()

	cfg := S3ProviderConfig{
		Bucket:          "test-backup-bucket",
		Region:          "us-east-1",
		Endpoint:        srv.URL,
		ForcePathStyle:  true,
		AccessKeyID:     "TESTKEYID",
		SecretAccessKey: "TESTSECRETKEY",
		AllowInsecure:   true,
		HTTPClient:      srv.Client(),
	}

	provider, err := NewS3StorageProvider(cfg)
	if err != nil {
		t.Fatalf("failed creating S3StorageProvider: %v", err)
	}

	ctx := context.Background()

	// 1. EnsureStorageRoot
	if err := provider.EnsureStorageRoot(ctx); err != nil {
		t.Fatalf("EnsureStorageRoot failed: %v", err)
	}

	// 2. SaveArtifact
	orgID := uuid.New()
	resID := uuid.New()
	runID := uuid.New()
	artID := uuid.New()

	testData := []byte("hello s3 backup world test stream")
	res, err := provider.SaveArtifact(ctx, orgID, resID, runID, artID, ".sql.gz", bytes.NewReader(testData))
	if err != nil {
		t.Fatalf("SaveArtifact failed: %v", err)
	}

	if res.SizeBytes != int64(len(testData)) {
		t.Errorf("expected size %d, got %d", len(testData), res.SizeBytes)
	}

	expectedHash := sha256.Sum256(testData)
	expectedHex := hex.EncodeToString(expectedHash[:])
	if res.ChecksumSHA256 != expectedHex {
		t.Errorf("expected checksum %s, got %s", expectedHex, res.ChecksumSHA256)
	}

	// Check object stored in mock S3
	mockS3.mu.Lock()
	stored, ok := mockS3.objects[res.StorageReference]
	mockS3.mu.Unlock()
	if !ok || !bytes.Equal(stored, testData) {
		t.Errorf("stored content mismatch in mock S3")
	}

	// 3. OpenArtifact
	readCloser, err := provider.OpenArtifact(ctx, res.StorageReference)
	if err != nil {
		t.Fatalf("OpenArtifact failed: %v", err)
	}
	fetchedBytes, err := io.ReadAll(readCloser)
	_ = readCloser.Close()
	if err != nil {
		t.Fatalf("failed reading artifact stream: %v", err)
	}
	if !bytes.Equal(fetchedBytes, testData) {
		t.Errorf("fetched bytes mismatch: %s vs %s", string(fetchedBytes), string(testData))
	}

	// 4. OpenArtifact Not Found (valid canonical format, missing object)
	missingKey := provider.BuildObjectKey(uuid.New(), uuid.New(), uuid.New(), ".sql.gz")
	_, err = provider.OpenArtifact(ctx, missingKey)
	if err != storage.ErrArtifactNotFound {
		t.Errorf("expected ErrArtifactNotFound, got %v", err)
	}

	// 5. DeleteArtifact
	if err := provider.DeleteArtifact(ctx, res.StorageReference); err != nil {
		t.Fatalf("DeleteArtifact failed: %v", err)
	}

	mockS3.mu.Lock()
	_, stillExists := mockS3.objects[res.StorageReference]
	mockS3.mu.Unlock()
	if stillExists {
		t.Errorf("expected object to be deleted")
	}

	// 6. DeleteArtifact Idempotence (missing object returns nil)
	if err := provider.DeleteArtifact(ctx, res.StorageReference); err != nil {
		t.Errorf("expected idempotent delete, got error: %v", err)
	}
}

func TestS3StorageProvider_CanonicalReferenceValidation(t *testing.T) {
	orgID := uuid.New()
	resID := uuid.New()
	artID := uuid.New()

	pNoPrefix := &S3StorageProvider{prefix: ""}
	pWithPrefix := &S3StorageProvider{prefix: "custom-prefix"}

	validSQL := fmt.Sprintf("organizations/%s/resources/%s/artifacts/%s.sql.gz", orgID, resID, artID)
	validTar := fmt.Sprintf("organizations/%s/resources/%s/artifacts/%s.tar.gz", orgID, resID, artID)
	validWithPrefix := fmt.Sprintf("custom-prefix/organizations/%s/resources/%s/artifacts/%s.sql.gz", orgID, resID, artID)

	tests := []struct {
		name        string
		provider    *S3StorageProvider
		ref         string
		expectError bool
	}{
		{"valid sql.gz", pNoPrefix, validSQL, false},
		{"valid tar.gz", pNoPrefix, validTar, false},
		{"valid with prefix", pWithPrefix, validWithPrefix, false},
		{"empty reference", pNoPrefix, "", true},
		{"absolute key rejected", pNoPrefix, "/" + validSQL, true},
		{"backslash rejected", pNoPrefix, strings.ReplaceAll(validSQL, "/", "\\"), true},
		{"NUL byte rejected", pNoPrefix, validSQL + "\x00", true},
		{"path traversal rejected", pNoPrefix, fmt.Sprintf("organizations/%s/resources/%s/artifacts/../%s.sql.gz", orgID, resID, artID), true},
		{"wrong segment count short", pNoPrefix, fmt.Sprintf("organizations/%s/artifacts/%s.sql.gz", orgID, artID), true},
		{"wrong segment count long", pNoPrefix, fmt.Sprintf("organizations/%s/resources/%s/extra/artifacts/%s.sql.gz", orgID, resID, artID), true},
		{"invalid org uuid", pNoPrefix, fmt.Sprintf("organizations/not-a-uuid/resources/%s/artifacts/%s.sql.gz", resID, artID), true},
		{"invalid res uuid", pNoPrefix, fmt.Sprintf("organizations/%s/resources/not-a-uuid/artifacts/%s.sql.gz", orgID, artID), true},
		{"invalid art uuid", pNoPrefix, fmt.Sprintf("organizations/%s/resources/%s/artifacts/not-a-uuid.sql.gz", orgID, resID), true},
		{"unsupported extension txt", pNoPrefix, fmt.Sprintf("organizations/%s/resources/%s/artifacts/%s.txt", orgID, resID, artID), true},
		{"unsupported extension gz without tar/sql", pNoPrefix, fmt.Sprintf("organizations/%s/resources/%s/artifacts/%s.gz", orgID, resID, artID), true},
		{"missing expected prefix", pWithPrefix, validSQL, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.provider.ValidateStorageReference(tt.ref)
			if (err != nil) != tt.expectError {
				t.Errorf("ValidateStorageReference(%q) err = %v, expectError = %v", tt.ref, err, tt.expectError)
			}
			if tt.expectError && !errors.Is(err, storage.ErrInvalidStorageReference) {
				t.Errorf("expected ErrInvalidStorageReference, got %v", err)
			}
		})
	}
}

func TestS3StorageProvider_OpenAndDelete_MalformedReference(t *testing.T) {
	p := &S3StorageProvider{bucket: "test-bucket"}
	ctx := context.Background()

	malformedRefs := []string{
		"",
		"../traversal",
		"/etc/passwd",
		"organizations/bad/resources/bad/artifacts/bad.sql.gz",
	}

	for _, ref := range malformedRefs {
		_, err := p.OpenArtifact(ctx, ref)
		if !errors.Is(err, storage.ErrInvalidStorageReference) {
			t.Errorf("OpenArtifact(%q) expected ErrInvalidStorageReference, got %v", ref, err)
		}

		err = p.DeleteArtifact(ctx, ref)
		if !errors.Is(err, storage.ErrInvalidStorageReference) {
			t.Errorf("DeleteArtifact(%q) expected ErrInvalidStorageReference, got %v", ref, err)
		}
	}
}

func TestS3StorageProvider_UploadFailure_NoSaveResult(t *testing.T) {
	// Server that fails on Put
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := S3ProviderConfig{
		Bucket:          "failing-bucket",
		Region:          "us-east-1",
		Endpoint:        srv.URL,
		ForcePathStyle:  true,
		AccessKeyID:     "KEY",
		SecretAccessKey: "SECRET",
		AllowInsecure:   true,
		HTTPClient:      srv.Client(),
	}

	p, err := NewS3StorageProvider(cfg)
	if err != nil {
		t.Fatalf("failed creating provider: %v", err)
	}

	ctx := context.Background()
	res, err := p.SaveArtifact(ctx, uuid.New(), uuid.New(), uuid.New(), uuid.New(), ".sql.gz", bytes.NewReader([]byte("test data")))
	if err == nil {
		t.Fatalf("expected upload to fail, got nil error")
	}
	if res != nil {
		t.Fatalf("expected nil SaveResult on upload failure, got: %+v", res)
	}
}

func TestS3StorageProvider_ContextCancellation(t *testing.T) {
	srv, _ := newMockS3Server()
	defer srv.Close()

	cfg := S3ProviderConfig{
		Bucket:          "cancel-bucket",
		Region:          "us-east-1",
		Endpoint:        srv.URL,
		ForcePathStyle:  true,
		AccessKeyID:     "KEY",
		SecretAccessKey: "SECRET",
		AllowInsecure:   true,
		HTTPClient:      srv.Client(),
	}

	p, err := NewS3StorageProvider(cfg)
	if err != nil {
		t.Fatalf("failed creating provider: %v", err)
	}

	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err = p.SaveArtifact(cancelledCtx, uuid.New(), uuid.New(), uuid.New(), uuid.New(), ".sql.gz", bytes.NewReader([]byte("test data")))
	if err == nil {
		t.Fatalf("expected cancelled context error, got nil")
	}
}
