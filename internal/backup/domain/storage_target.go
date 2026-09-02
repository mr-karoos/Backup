package domain

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"backup-platform/pkg/uuid"
)

// StorageTargetType represents the physical storage backend type.
type StorageTargetType string

const (
	StorageTargetTypeLocal        StorageTargetType = "local"
	StorageTargetTypeS3           StorageTargetType = "s3"
	StorageTargetTypeS3Compatible StorageTargetType = "s3_compatible"
	StorageTargetTypeRemoteSSH    StorageTargetType = "remote_ssh"
)

// IsValid checks whether the storage target type is supported.
func (t StorageTargetType) IsValid() bool {
	switch t {
	case StorageTargetTypeLocal, StorageTargetTypeS3, StorageTargetTypeS3Compatible:
		return true
	default:
		return false
	}
}

// StorageTargetStatus represents the availability state of a storage target.
type StorageTargetStatus string

const (
	StorageTargetStatusActive   StorageTargetStatus = "active"
	StorageTargetStatusDisabled StorageTargetStatus = "disabled"
	StorageTargetStatusError    StorageTargetStatus = "error"
	StorageTargetStatusArchived StorageTargetStatus = "archived"
)

// IsValid checks whether the storage target status is valid.
func (s StorageTargetStatus) IsValid() bool {
	switch s {
	case StorageTargetStatusActive, StorageTargetStatusDisabled, StorageTargetStatusError, StorageTargetStatusArchived:
		return true
	default:
		return false
	}
}

// StorageTarget represents a physical or cloud destination for backup artifacts.
type StorageTarget struct {
	ID             uuid.UUID
	OrganizationID uuid.UUID
	Name           string
	Type           StorageTargetType
	Status         StorageTargetStatus
	IsDefault      bool
	CredentialID   *uuid.UUID
	Config         []byte
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// S3TargetConfig encapsulates non-sensitive configuration for AWS S3 and S3-compatible providers.
// Plaintext secrets (access keys, secret keys, session tokens) must never be stored here.
type S3TargetConfig struct {
	Bucket         string `json:"bucket"`
	Region         string `json:"region"`
	Endpoint       string `json:"endpoint,omitempty"`
	ForcePathStyle bool   `json:"force_path_style,omitempty"`
	Prefix         string `json:"prefix,omitempty"`
}

var s3BucketRegex = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$`)

// ValidateS3TargetConfig validates the non-secret S3 target parameters.
func ValidateS3TargetConfig(cfg *S3TargetConfig) error {
	if cfg == nil {
		return errors.New("s3 configuration cannot be empty")
	}

	bucket := strings.TrimSpace(cfg.Bucket)
	if bucket == "" {
		return errors.New("s3 bucket name is required")
	}
	if len(bucket) < 3 || len(bucket) > 63 || !s3BucketRegex.MatchString(bucket) {
		return errors.New("invalid s3 bucket name: must be 3-63 characters, lowercase letters, numbers, hyphens or dots")
	}
	cfg.Bucket = bucket

	region := strings.TrimSpace(cfg.Region)
	if region == "" {
		return errors.New("s3 region is required")
	}
	cfg.Region = region

	endpoint := strings.TrimSpace(cfg.Endpoint)
	if endpoint != "" {
		u, err := url.Parse(endpoint)
		if err != nil || u.Host == "" {
			return errors.New("invalid s3 endpoint URL")
		}
		if u.User != nil {
			return errors.New("s3 endpoint must not contain user credentials")
		}
		if u.Scheme != "https" && u.Scheme != "http" {
			return errors.New("s3 endpoint scheme must be https or http")
		}
		cfg.Endpoint = endpoint
	}

	prefix := strings.TrimSpace(cfg.Prefix)
	if prefix != "" {
		if strings.Contains(prefix, "..") {
			return errors.New("s3 prefix must not contain path traversal elements ('..')")
		}
		prefix = strings.Trim(prefix, "/")
		cfg.Prefix = prefix
	}

	return nil
}

// ParseS3TargetConfig unmarshals JSON configuration into S3TargetConfig and validates it.
func ParseS3TargetConfig(data []byte) (*S3TargetConfig, error) {
	if len(data) == 0 || string(data) == "{}" {
		return nil, errors.New("empty s3 configuration")
	}

	var cfg S3TargetConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("malformed s3 configuration JSON: %w", err)
	}

	if err := ValidateS3TargetConfig(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// ValidateStorageTargetName validates and returns the trimmed target name.
func ValidateStorageTargetName(name string) (string, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return "", ErrInvalidStorageTargetName
	}
	if !utf8.ValidString(trimmed) {
		return "", ErrInvalidStorageTargetName
	}
	for i := 0; i < len(trimmed); i++ {
		c := trimmed[i]
		if c < 32 || c == 127 {
			return "", ErrInvalidStorageTargetName
		}
	}
	if utf8.RuneCountInString(trimmed) > 100 {
		return "", ErrInvalidStorageTargetName
	}
	return trimmed, nil
}

// IsEngineCompatibleWithStorage returns true if the engine can store artifacts to the target type.
func IsEngineCompatibleWithStorage(engine EngineType, storageType StorageTargetType) bool {
	switch engine {
	case EngineTypeDirectStream:
		return storageType == StorageTargetTypeLocal ||
			storageType == StorageTargetTypeS3 ||
			storageType == StorageTargetTypeS3Compatible
	default:
		return false
	}
}
