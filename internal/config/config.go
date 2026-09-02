package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"math"
	"net/url"
	"os"
	"strconv"
	"strings"
)

// Standard environment constants.
const (
	EnvDevelopment = "development"
	EnvTest        = "test"
	EnvStaging     = "staging"
	EnvProduction  = "production"
)

// Default configuration values for development.
const (
	DefaultAppEnv      = EnvDevelopment
	DefaultHTTPAddr    = ":8080"
	DefaultLogLevel    = "info"
	DefaultStorageRoot = "/srv/backup-platform"
	DefaultDevDatabase = "postgres://postgres:postgres@localhost:5432/backup_platform?sslmode=disable"
	DefaultMasterKeyV  = 1
)

// Config represents the application startup configuration loaded from environment variables.
type Config struct {
	AppEnv                      string
	HTTPAddr                    string
	DatabaseURL                 string
	LogLevel                    string
	StorageRoot                 string
	BootstrapAdminEmail         string
	BootstrapAdminPassword      string
	JWTSigningKey               string
	AuthCookieSecure            bool
	EncryptionMasterKey         []byte
	EncryptionMasterKeyVersion  int
	S3PrivateEndpointsAllowlist []string
	S3AllowInsecureEndpoints    bool
}

// Load reads configuration from environment variables and validates all constraints.
func Load() (*Config, error) {
	appEnv := strings.TrimSpace(getEnv("APP_ENV", DefaultAppEnv))
	httpAddr := strings.TrimSpace(getEnv("HTTP_ADDR", DefaultHTTPAddr))
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	logLevel := strings.ToLower(strings.TrimSpace(getEnv("LOG_LEVEL", DefaultLogLevel)))
	storageRoot := strings.TrimSpace(getEnv("STORAGE_ROOT", DefaultStorageRoot))
	bootstrapAdminEmail := strings.TrimSpace(os.Getenv("BOOTSTRAP_ADMIN_EMAIL"))
	bootstrapAdminPassword := os.Getenv("BOOTSTRAP_ADMIN_PASSWORD")
	jwtSigningKey := os.Getenv("JWT_SIGNING_KEY")

	// 1. Parse and decode ENCRYPTION_MASTER_KEY (Required in all environments)
	rawMasterKey := strings.TrimSpace(os.Getenv("ENCRYPTION_MASTER_KEY"))
	if rawMasterKey == "" {
		return nil, errors.New("ENCRYPTION_MASTER_KEY is required")
	}

	decodedMasterKey, err := base64.StdEncoding.DecodeString(rawMasterKey)
	if err != nil {
		return nil, errors.New("invalid ENCRYPTION_MASTER_KEY: must be valid base64")
	}

	if len(decodedMasterKey) != 32 {
		return nil, errors.New("invalid ENCRYPTION_MASTER_KEY: must decode to exactly 32 bytes")
	}

	// 2. Parse ENCRYPTION_MASTER_KEY_VERSION (Defaults to 1)
	masterKeyVersion := DefaultMasterKeyV
	if rawKeyVersion := strings.TrimSpace(os.Getenv("ENCRYPTION_MASTER_KEY_VERSION")); rawKeyVersion != "" {
		v, err := strconv.Atoi(rawKeyVersion)
		if err != nil || v < 1 || v > math.MaxInt32 {
			return nil, errors.New("invalid ENCRYPTION_MASTER_KEY_VERSION: must be an integer between 1 and 2147483647")
		}
		masterKeyVersion = v
	}

	// Determine AuthCookieSecure default based on environment
	defaultCookieSecure := (appEnv == EnvProduction || appEnv == EnvStaging)
	authCookieSecure := defaultCookieSecure
	if cookieSecureStr := strings.TrimSpace(os.Getenv("AUTH_COOKIE_SECURE")); cookieSecureStr != "" {
		parsed, err := strconv.ParseBool(cookieSecureStr)
		if err != nil {
			return nil, errors.New("invalid AUTH_COOKIE_SECURE: must be a valid boolean")
		}
		authCookieSecure = parsed
	}

	// In development/test, provide a non-sensitive default if DATABASE_URL is not set
	if databaseURL == "" {
		if appEnv == EnvProduction || appEnv == EnvStaging {
			return nil, errors.New("DATABASE_URL environment variable is required in production and staging environments")
		}
		databaseURL = DefaultDevDatabase
	}

	// S3 Insecure Endpoints policy (default false)
	s3AllowInsecure := false
	if s3InsecureStr := strings.TrimSpace(os.Getenv("S3_ALLOW_INSECURE_ENDPOINTS")); s3InsecureStr != "" {
		parsed, err := strconv.ParseBool(s3InsecureStr)
		if err != nil {
			return nil, errors.New("invalid S3_ALLOW_INSECURE_ENDPOINTS: must be a valid boolean")
		}
		s3AllowInsecure = parsed
	}

	// S3 Private Endpoints Allowlist (comma-separated list of IP CIDRs or hostnames)
	var s3Allowlist []string
	if rawAllowlist := strings.TrimSpace(os.Getenv("S3_PRIVATE_ENDPOINTS_ALLOWLIST")); rawAllowlist != "" {
		parts := strings.Split(rawAllowlist, ",")
		for _, p := range parts {
			trimmed := strings.TrimSpace(p)
			if trimmed != "" {
				s3Allowlist = append(s3Allowlist, trimmed)
			}
		}
	}

	cfg := &Config{
		AppEnv:                      appEnv,
		HTTPAddr:                    httpAddr,
		DatabaseURL:                 databaseURL,
		LogLevel:                    logLevel,
		StorageRoot:                 storageRoot,
		BootstrapAdminEmail:         bootstrapAdminEmail,
		BootstrapAdminPassword:      bootstrapAdminPassword,
		JWTSigningKey:               jwtSigningKey,
		AuthCookieSecure:            authCookieSecure,
		EncryptionMasterKey:         decodedMasterKey,
		EncryptionMasterKeyVersion:  masterKeyVersion,
		S3PrivateEndpointsAllowlist: s3Allowlist,
		S3AllowInsecureEndpoints:    s3AllowInsecure,
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return cfg, nil
}

// Validate ensures all configuration parameters satisfy startup invariants.
func (c *Config) Validate() error {
	switch c.AppEnv {
	case EnvDevelopment, EnvTest, EnvStaging, EnvProduction:
		// valid
	default:
		return fmt.Errorf("invalid APP_ENV '%s': must be one of [development, test, staging, production]", c.AppEnv)
	}

	if c.HTTPAddr == "" {
		return errors.New("HTTP_ADDR cannot be empty")
	}

	if c.DatabaseURL == "" {
		return errors.New("DATABASE_URL cannot be empty")
	}

	// Validate DATABASE_URL structure without leaking credentials on error
	u, err := url.Parse(c.DatabaseURL)
	if err != nil {
		return errors.New("invalid DATABASE_URL: malformed URL format")
	}

	scheme := strings.ToLower(u.Scheme)
	if scheme != "postgres" && scheme != "postgresql" {
		return errors.New("invalid DATABASE_URL: scheme must be 'postgres' or 'postgresql'")
	}

	if u.Host == "" {
		return errors.New("invalid DATABASE_URL: host cannot be empty")
	}

	switch c.LogLevel {
	case "debug", "info", "warn", "error":
		// valid
	default:
		return fmt.Errorf("invalid LOG_LEVEL '%s': must be one of [debug, info, warn, error]", c.LogLevel)
	}

	if c.StorageRoot == "" {
		return errors.New("STORAGE_ROOT cannot be empty")
	}

	if !isPosixAbsolutePath(c.StorageRoot) {
		return errors.New("STORAGE_ROOT must be an absolute POSIX path starting with '/'")
	}

	// JWT signing key validation (minimum 32 bytes required for HS256)
	if len(c.JWTSigningKey) < 32 {
		return errors.New("JWT_SIGNING_KEY is required and must be at least 32 bytes")
	}

	// Encryption master key validation (must be exactly 32 bytes for AES-256)
	if len(c.EncryptionMasterKey) != 32 {
		return errors.New("ENCRYPTION_MASTER_KEY is required and must decode to exactly 32 bytes")
	}

	// Encryption master key version validation (must be between 1 and MaxInt32)
	if c.EncryptionMasterKeyVersion < 1 || c.EncryptionMasterKeyVersion > math.MaxInt32 {
		return errors.New("ENCRYPTION_MASTER_KEY_VERSION must be an integer between 1 and 2147483647")
	}

	// Cookie security policy validation: Staging and Production MUST have Secure=true
	if (c.AppEnv == EnvProduction || c.AppEnv == EnvStaging) && !c.AuthCookieSecure {
		return errors.New("AUTH_COOKIE_SECURE must be true in production and staging environments")
	}

	// S3 insecure endpoints policy validation: Staging and Production MUST NOT allow insecure HTTP endpoints
	if (c.AppEnv == EnvProduction || c.AppEnv == EnvStaging) && c.S3AllowInsecureEndpoints {
		return errors.New("S3_ALLOW_INSECURE_ENDPOINTS cannot be true in production and staging environments")
	}

	// Bootstrap credentials validation: if one is set, both must be provided
	if (c.BootstrapAdminEmail != "" && c.BootstrapAdminPassword == "") ||
		(c.BootstrapAdminEmail == "" && c.BootstrapAdminPassword != "") {
		return errors.New("both BOOTSTRAP_ADMIN_EMAIL and BOOTSTRAP_ADMIN_PASSWORD must be provided together")
	}

	if c.BootstrapAdminEmail != "" && !strings.Contains(c.BootstrapAdminEmail, "@") {
		return errors.New("invalid BOOTSTRAP_ADMIN_EMAIL: must contain '@'")
	}

	return nil
}

func isPosixAbsolutePath(p string) bool {
	return strings.HasPrefix(p, "/")
}

func getEnv(key, defaultValue string) string {
	val := os.Getenv(key)
	if val == "" {
		return defaultValue
	}
	return val
}
