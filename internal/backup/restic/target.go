package restic

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"backup-platform/internal/backup/domain"
	"backup-platform/internal/credential/secretcrypto"
	"backup-platform/internal/storage/s3"
	"backup-platform/pkg/uuid"
)

// RepositoryTarget defines the structured backend abstraction for Restic repositories (ADR-032).
// Note: This is strictly separated from the flat-file storage.StorageProvider abstraction.
type RepositoryTarget interface {
	// Type returns the storage target type ("local", "s3", "s3_compatible")
	Type() string
	// Locator returns the internal non-secret locator stored in the database
	Locator() string
	// ResticRepositoryURL returns the absolute local path or S3 URL formatted for Restic
	ResticRepositoryURL() string
	// Env returns child process environment variables needed for authentication
	Env() []string
	// Cleanup safely clears sensitive secret material from memory
	Cleanup()
}

// LocalRepositoryTarget represents a local filesystem Restic repository.
type LocalRepositoryTarget struct {
	storageRoot string
	orgID       uuid.UUID
	resourceID  uuid.UUID
	locator     string
	fullPath    string
}

// NewLocalRepositoryTarget constructs and validates a LocalRepositoryTarget.
func NewLocalRepositoryTarget(storageRoot string, orgID, resourceID uuid.UUID) (*LocalRepositoryTarget, error) {
	if orgID == uuid.Nil || resourceID == uuid.Nil {
		return nil, errors.New("organization ID and resource ID are required for local repository target")
	}

	cleanRoot := filepath.Clean(storageRoot)
	if cleanRoot == "" || cleanRoot == "." {
		return nil, errors.New("valid storage root is required")
	}

	// Canonical relative locator: repositories/organizations/{orgID}/resources/{resourceID}/restic
	relLocator := filepath.Join("repositories", "organizations", orgID.String(), "resources", resourceID.String(), "restic")
	fullPath := filepath.Join(cleanRoot, relLocator)

	// Ensure no path traversal outside storageRoot
	rel, err := filepath.Rel(cleanRoot, fullPath)
	if err != nil || strings.HasPrefix(rel, "..") {
		return nil, errors.New("repository path traversal detected outside storage root")
	}

	// Ensure directory exists with 0700 permissions (non-world-readable)
	if err := os.MkdirAll(fullPath, 0700); err != nil {
		return nil, fmt.Errorf("failed creating local repository directory: %w", err)
	}

	return &LocalRepositoryTarget{
		storageRoot: cleanRoot,
		orgID:       orgID,
		resourceID:  resourceID,
		locator:     filepath.ToSlash(relLocator),
		fullPath:    fullPath,
	}, nil
}

func (t *LocalRepositoryTarget) Type() string {
	return string(domain.StorageTargetTypeLocal)
}

func (t *LocalRepositoryTarget) Locator() string {
	return t.locator
}

func (t *LocalRepositoryTarget) ResticRepositoryURL() string {
	return t.fullPath
}

func (t *LocalRepositoryTarget) Env() []string {
	return nil
}

func (t *LocalRepositoryTarget) Cleanup() {
	// No in-memory secrets for local repository target
}

// S3RepositoryTarget represents an S3 or S3-compatible Restic repository.
type S3RepositoryTarget struct {
	targetType      string
	orgID           uuid.UUID
	resourceID      uuid.UUID
	locator         string
	resticRepoURL   string
	accessKeyID     string
	secretAccessKey []byte
	sessionToken    []byte
	region          string
}

// NewS3RepositoryTarget constructs and validates an S3RepositoryTarget reusing A.1 S3 security rules.
func NewS3RepositoryTarget(
	targetType string,
	s3Cfg domain.S3TargetConfig,
	orgID, resourceID uuid.UUID,
	accessKeyID, secretAccessKey string,
	sessionToken *string,
	allowInsecure bool,
	privateAllowlist []string,
) (*S3RepositoryTarget, error) {
	if orgID == uuid.Nil || resourceID == uuid.Nil {
		return nil, errors.New("organization ID and resource ID are required for s3 repository target")
	}

	if s3Cfg.Bucket == "" {
		return nil, errors.New("s3 bucket is required")
	}

	if accessKeyID == "" || secretAccessKey == "" {
		return nil, errors.New("s3 credentials are required")
	}

	// 1. Validate endpoint against A.1 EndpointSecurityPolicy (SSRF, link-local, loopback, private IP allowlist)
	policy := &s3.EndpointSecurityPolicy{
		AllowInsecureHTTP: allowInsecure,
		PrivateAllowlist:  privateAllowlist,
	}

	var endpointHostPort string
	var useHTTPS bool = true

	if strings.TrimSpace(s3Cfg.Endpoint) != "" {
		u, err := policy.ValidateEndpointURL(s3Cfg.Endpoint)
		if err != nil {
			return nil, fmt.Errorf("invalid s3 endpoint: %w", err)
		}
		if u.Scheme == "http" {
			useHTTPS = false
		}
		endpointHostPort = u.Host
	}

	// 2. Enforce strictly scoped namespace: organizations/{orgID}/resources/{resourceID}/restic
	// Optional prefix may only act as base prefix if it doesn't escape
	cleanBasePrefix := strings.Trim(strings.TrimSpace(s3Cfg.Prefix), "/")
	for strings.Contains(cleanBasePrefix, "..") {
		return nil, errors.New("invalid s3 prefix traversal detected")
	}

	var relNamespace string
	if cleanBasePrefix != "" {
		relNamespace = path.Join(cleanBasePrefix, "organizations", orgID.String(), "resources", resourceID.String(), "restic")
	} else {
		relNamespace = path.Join("organizations", orgID.String(), "resources", resourceID.String(), "restic")
	}

	// 3. Construct Restic S3 repository URL:
	// If custom endpoint: s3:http(s)://endpoint/bucket/prefix
	// If standard AWS: s3:s3.amazonaws.com/bucket/prefix or s3:https://s3.region.amazonaws.com/bucket/prefix
	var repoURL string
	if endpointHostPort != "" {
		scheme := "https"
		if !useHTTPS {
			scheme = "http"
		}
		repoURL = fmt.Sprintf("s3:%s://%s/%s/%s", scheme, endpointHostPort, s3Cfg.Bucket, relNamespace)
	} else {
		region := s3Cfg.Region
		if region == "" {
			region = "us-east-1"
		}
		repoURL = fmt.Sprintf("s3:https://s3.%s.amazonaws.com/%s/%s", region, s3Cfg.Bucket, relNamespace)
	}

	target := &S3RepositoryTarget{
		targetType:      targetType,
		orgID:           orgID,
		resourceID:      resourceID,
		locator:         relNamespace,
		resticRepoURL:   repoURL,
		accessKeyID:     accessKeyID,
		secretAccessKey: []byte(secretAccessKey),
		region:          s3Cfg.Region,
	}

	if sessionToken != nil && len(*sessionToken) > 0 {
		target.sessionToken = []byte(*sessionToken)
	}

	return target, nil
}

func (t *S3RepositoryTarget) Type() string {
	return t.targetType
}

func (t *S3RepositoryTarget) Locator() string {
	return t.locator
}

func (t *S3RepositoryTarget) ResticRepositoryURL() string {
	return t.resticRepoURL
}

func (t *S3RepositoryTarget) Env() []string {
	env := []string{
		"AWS_ACCESS_KEY_ID=" + t.accessKeyID,
		"AWS_SECRET_ACCESS_KEY=" + string(t.secretAccessKey),
	}
	if t.region != "" {
		env = append(env, "AWS_DEFAULT_REGION="+t.region, "AWS_REGION="+t.region)
	}
	if len(t.sessionToken) > 0 {
		env = append(env, "AWS_SESSION_TOKEN="+string(t.sessionToken))
	}
	return env
}

func (t *S3RepositoryTarget) Cleanup() {
	secretcrypto.ZeroBytes(t.secretAccessKey)
	t.secretAccessKey = nil
	if len(t.sessionToken) > 0 {
		secretcrypto.ZeroBytes(t.sessionToken)
		t.sessionToken = nil
	}
}
