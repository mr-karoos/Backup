package s3

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"backup-platform/internal/storage"
	"backup-platform/pkg/uuid"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	s3client "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	smithy "github.com/aws/smithy-go"
)

// S3ProviderConfig encapsulates parameters required to construct an S3StorageProvider.
type S3ProviderConfig struct {
	Bucket           string
	Region           string
	Endpoint         string
	ForcePathStyle   bool
	Prefix           string
	AccessKeyID      string
	SecretAccessKey  string
	SessionToken     *string
	AllowInsecure    bool
	PrivateAllowlist []string
	HTTPClient       *http.Client // Optional override (useful for testing)
}

// S3StorageProvider implements storage.StorageProvider for AWS S3 and S3-compatible backends.
type S3StorageProvider struct {
	client   *s3client.Client
	uploader *manager.Uploader
	bucket   string
	prefix   string
}

// NewS3StorageProvider constructs and validates a new S3StorageProvider.
func NewS3StorageProvider(cfg S3ProviderConfig) (*S3StorageProvider, error) {
	if strings.TrimSpace(cfg.Bucket) == "" {
		return nil, errors.New("s3 bucket is required")
	}
	if strings.TrimSpace(cfg.Region) == "" {
		cfg.Region = "us-east-1"
	}
	if strings.TrimSpace(cfg.AccessKeyID) == "" || strings.TrimSpace(cfg.SecretAccessKey) == "" {
		return nil, errors.New("s3 credentials are required")
	}

	policy := EndpointSecurityPolicy{
		AllowInsecureHTTP: cfg.AllowInsecure,
		PrivateAllowlist:  cfg.PrivateAllowlist,
	}

	var httpClient *http.Client
	if cfg.HTTPClient != nil {
		httpClient = cfg.HTTPClient
	} else {
		httpClient = policy.NewSecureHTTPClient()
	}

	// Validate endpoint URL if provided
	var validatedEndpoint *string
	if strings.TrimSpace(cfg.Endpoint) != "" {
		u, err := policy.ValidateEndpointURL(cfg.Endpoint)
		if err != nil {
			return nil, err
		}
		if u != nil {
			ep := u.String()
			validatedEndpoint = &ep
		}
	}

	var sessionToken string
	if cfg.SessionToken != nil {
		sessionToken = *cfg.SessionToken
	}

	credProvider := credentials.NewStaticCredentialsProvider(
		cfg.AccessKeyID,
		cfg.SecretAccessKey,
		sessionToken,
	)

	// Clean prefix
	cleanPrefix := strings.Trim(strings.TrimSpace(cfg.Prefix), "/")

	clientOptions := func(o *s3client.Options) {
		o.Region = cfg.Region
		o.Credentials = credProvider
		o.HTTPClient = httpClient
		o.UsePathStyle = cfg.ForcePathStyle
		if validatedEndpoint != nil {
			ep := *validatedEndpoint
			o.EndpointResolver = s3client.EndpointResolverFunc(func(region string, options s3client.EndpointResolverOptions) (aws.Endpoint, error) {
				return aws.Endpoint{
					URL:               ep,
					HostnameImmutable: true,
					SigningRegion:     cfg.Region,
				}, nil
			})
		}
	}

	awsClient := s3client.New(s3client.Options{}, clientOptions)

	uploader := manager.NewUploader(awsClient, func(u *manager.Uploader) {
		u.PartSize = 5 * 1024 * 1024 // 5 MB minimum S3 part size
	})

	return &S3StorageProvider{
		client:   awsClient,
		uploader: uploader,
		bucket:   cfg.Bucket,
		prefix:   cleanPrefix,
	}, nil
}

// countingHashingReader wraps an io.Reader, computing SHA-256 and tracking total bytes read.
type countingHashingReader struct {
	reader    io.Reader
	hasher    io.Writer
	bytesRead int64
}

func (r *countingHashingReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	if n > 0 {
		r.bytesRead += int64(n)
		_, _ = r.hasher.Write(p[:n])
	}
	return n, err
}

// BuildObjectKey generates the deterministic tenant-namespaced object key.
func (p *S3StorageProvider) BuildObjectKey(orgID, resID, artifactID uuid.UUID, extension string) string {
	ext := extension
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}

	tenantPath := fmt.Sprintf("organizations/%s/resources/%s/artifacts/%s%s",
		orgID.String(),
		resID.String(),
		artifactID.String(),
		ext,
	)

	if p.prefix != "" {
		return fmt.Sprintf("%s/%s", p.prefix, tenantPath)
	}
	return tenantPath
}

// SaveArtifact streams the artifact payload directly into S3, hashing SHA-256 on the fly.
func (p *S3StorageProvider) SaveArtifact(
	ctx context.Context,
	orgID, resID, runID, artifactID uuid.UUID,
	extension string,
	src io.Reader,
) (*storage.SaveResult, error) {
	if src == nil {
		return nil, errors.New("source stream cannot be nil")
	}

	cleanExt := strings.TrimSpace(extension)
	if !strings.HasPrefix(cleanExt, ".") {
		cleanExt = "." + cleanExt
	}
	if cleanExt != ".sql.gz" && cleanExt != ".tar.gz" {
		return nil, fmt.Errorf("unsupported artifact extension: %s", cleanExt)
	}

	objectKey := p.BuildObjectKey(orgID, resID, artifactID, cleanExt)

	h := sha256.New()
	cr := &countingHashingReader{
		reader: src,
		hasher: h,
	}

	// Stream upload via S3 Uploader (multipart streaming without full in-memory buffering)
	_, err := p.uploader.Upload(ctx, &s3client.PutObjectInput{
		Bucket: aws.String(p.bucket),
		Key:    aws.String(objectKey),
		Body:   cr,
	})
	if err != nil {
		return nil, fmt.Errorf("s3 upload failed: %w", err)
	}

	if cr.bytesRead == 0 {
		// Clean up empty object and fail closed
		_, _ = p.client.DeleteObject(ctx, &s3client.DeleteObjectInput{
			Bucket: aws.String(p.bucket),
			Key:    aws.String(objectKey),
		})
		return nil, errors.New("artifact size must be greater than zero bytes")
	}

	checksumHex := hex.EncodeToString(h.Sum(nil))

	return &storage.SaveResult{
		StorageReference: objectKey,
		SizeBytes:        cr.bytesRead,
		ChecksumSHA256:   checksumHex,
	}, nil
}

// ValidateStorageReference validates that a storage reference conforms strictly to the canonical
// platform-generated structure:
// [prefix/]organizations/{org_uuid}/resources/{resource_uuid}/artifacts/{artifact_uuid}.sql.gz (or .tar.gz)
// Rejects absolute keys, backslashes, NUL bytes, traversal, wrong segment counts, invalid UUIDs, and unsupported extensions.
func (p *S3StorageProvider) ValidateStorageReference(storageReference string) error {
	ref := strings.TrimSpace(storageReference)
	if ref == "" {
		return storage.ErrInvalidStorageReference
	}

	// Reject absolute-style keys, backslashes, NUL bytes, and directory traversal
	if strings.HasPrefix(ref, "/") || strings.Contains(ref, "\\") || strings.Contains(ref, "\x00") || strings.Contains(ref, "..") {
		return storage.ErrInvalidStorageReference
	}

	relRef := ref
	if p.prefix != "" {
		expectedPrefix := p.prefix + "/"
		if !strings.HasPrefix(relRef, expectedPrefix) {
			return storage.ErrInvalidStorageReference
		}
		relRef = strings.TrimPrefix(relRef, expectedPrefix)
	}

	// Canonical structure: organizations/{org_uuid}/resources/{resource_uuid}/artifacts/{artifact_uuid}.(sql.gz|tar.gz)
	segments := strings.Split(relRef, "/")
	if len(segments) != 6 {
		return storage.ErrInvalidStorageReference
	}

	if segments[0] != "organizations" || segments[2] != "resources" || segments[4] != "artifacts" {
		return storage.ErrInvalidStorageReference
	}

	if _, err := uuid.Parse(segments[1]); err != nil {
		return storage.ErrInvalidStorageReference
	}
	if _, err := uuid.Parse(segments[3]); err != nil {
		return storage.ErrInvalidStorageReference
	}

	filename := segments[5]
	var artUUIDStr string
	if strings.HasSuffix(filename, ".sql.gz") {
		artUUIDStr = strings.TrimSuffix(filename, ".sql.gz")
	} else if strings.HasSuffix(filename, ".tar.gz") {
		artUUIDStr = strings.TrimSuffix(filename, ".tar.gz")
	} else {
		return storage.ErrInvalidStorageReference
	}

	if _, err := uuid.Parse(artUUIDStr); err != nil {
		return storage.ErrInvalidStorageReference
	}

	return nil
}

// OpenArtifact streams an artifact directly from S3.
func (p *S3StorageProvider) OpenArtifact(ctx context.Context, storageReference string) (io.ReadCloser, error) {
	if err := p.ValidateStorageReference(storageReference); err != nil {
		return nil, err
	}

	out, err := p.client.GetObject(ctx, &s3client.GetObjectInput{
		Bucket: aws.String(p.bucket),
		Key:    aws.String(strings.TrimSpace(storageReference)),
	})
	if err != nil {
		var nsk *types.NoSuchKey
		if errors.As(err, &nsk) {
			return nil, storage.ErrArtifactNotFound
		}
		var apiErr smithy.APIError
		if errors.As(err, &apiErr) && (apiErr.ErrorCode() == "NoSuchKey" || apiErr.ErrorCode() == "NotFound") {
			return nil, storage.ErrArtifactNotFound
		}
		return nil, fmt.Errorf("s3 get object failed: %w", err)
	}

	return out.Body, nil
}

// DeleteArtifact deletes the specified object from S3. Idempotent on missing objects, fails closed on invalid references.
func (p *S3StorageProvider) DeleteArtifact(ctx context.Context, storageReference string) error {
	if err := p.ValidateStorageReference(storageReference); err != nil {
		return err
	}

	_, err := p.client.DeleteObject(ctx, &s3client.DeleteObjectInput{
		Bucket: aws.String(p.bucket),
		Key:    aws.String(strings.TrimSpace(storageReference)),
	})
	if err != nil {
		var nsk *types.NoSuchKey
		if errors.As(err, &nsk) {
			return nil
		}
		var apiErr smithy.APIError
		if errors.As(err, &apiErr) && (apiErr.ErrorCode() == "NoSuchKey" || apiErr.ErrorCode() == "NotFound") {
			return nil
		}
		return fmt.Errorf("s3 delete object failed: %w", err)
	}

	return nil
}

// EnsureStorageRoot verifies that the target bucket is reachable and accessible.
func (p *S3StorageProvider) EnsureStorageRoot(ctx context.Context) error {
	_, err := p.client.HeadBucket(ctx, &s3client.HeadBucketInput{
		Bucket: aws.String(p.bucket),
	})
	if err != nil {
		return fmt.Errorf("s3 bucket '%s' check failed: %w", p.bucket, err)
	}
	return nil
}
