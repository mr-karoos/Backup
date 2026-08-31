package config

import (
	"bytes"
	"encoding/base64"
	"math"
	"strings"
	"testing"
)

const validTestJWTKey = "test-jwt-secret-key-must-be-at-least-32-bytes!"

var (
	validTestMasterKeyBytes  = bytes.Repeat([]byte{0x42}, 32)
	validTestMasterKeyBase64 = base64.StdEncoding.EncodeToString(validTestMasterKeyBytes)
)

// setValidRequiredSecrets sets all unconditionally required secrets using t.Setenv.
func setValidRequiredSecrets(t *testing.T) {
	t.Helper()
	t.Setenv("JWT_SIGNING_KEY", validTestJWTKey)
	t.Setenv("ENCRYPTION_MASTER_KEY", validTestMasterKeyBase64)
}

func TestConfig_LoadDefaults(t *testing.T) {
	t.Setenv("APP_ENV", "")
	t.Setenv("HTTP_ADDR", "")
	t.Setenv("DATABASE_URL", "")
	t.Setenv("LOG_LEVEL", "")
	t.Setenv("STORAGE_ROOT", "")
	t.Setenv("BOOTSTRAP_ADMIN_EMAIL", "")
	t.Setenv("BOOTSTRAP_ADMIN_PASSWORD", "")
	t.Setenv("AUTH_COOKIE_SECURE", "")
	t.Setenv("ENCRYPTION_MASTER_KEY_VERSION", "")
	setValidRequiredSecrets(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("expected load to succeed with defaults, got error: %v", err)
	}

	if cfg.AppEnv != DefaultAppEnv {
		t.Errorf("expected AppEnv %s, got %s", DefaultAppEnv, cfg.AppEnv)
	}
	if cfg.HTTPAddr != DefaultHTTPAddr {
		t.Errorf("expected HTTPAddr %s, got %s", DefaultHTTPAddr, cfg.HTTPAddr)
	}
	if cfg.LogLevel != DefaultLogLevel {
		t.Errorf("expected LogLevel %s, got %s", DefaultLogLevel, cfg.LogLevel)
	}
	if cfg.StorageRoot != DefaultStorageRoot {
		t.Errorf("expected StorageRoot %s, got %s", DefaultStorageRoot, cfg.StorageRoot)
	}
	if cfg.JWTSigningKey != validTestJWTKey {
		t.Errorf("expected JWTSigningKey match")
	}
	if !bytes.Equal(cfg.EncryptionMasterKey, validTestMasterKeyBytes) {
		t.Errorf("expected EncryptionMasterKey match")
	}
	if cfg.EncryptionMasterKeyVersion != 1 {
		t.Errorf("expected default EncryptionMasterKeyVersion 1, got %d", cfg.EncryptionMasterKeyVersion)
	}
	if cfg.AuthCookieSecure != false {
		t.Errorf("expected AuthCookieSecure = false in development default")
	}
}

func TestConfig_EncryptionMasterKeyValidation(t *testing.T) {
	t.Run("missing in all environments rejected without default fallback", func(t *testing.T) {
		envs := []string{EnvDevelopment, EnvTest, EnvStaging, EnvProduction}
		for _, env := range envs {
			t.Run(env, func(t *testing.T) {
				t.Setenv("APP_ENV", env)
				t.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/db")
				t.Setenv("JWT_SIGNING_KEY", validTestJWTKey)
				t.Setenv("ENCRYPTION_MASTER_KEY", "")

				_, err := Load()
				if err == nil {
					t.Fatalf("expected failure for missing ENCRYPTION_MASTER_KEY in %s", env)
				}
				if !strings.Contains(err.Error(), "ENCRYPTION_MASTER_KEY is required") {
					t.Errorf("unexpected error message: %v", err)
				}
			})
		}
	})

	t.Run("invalid base64 encoding rejected safely", func(t *testing.T) {
		t.Setenv("APP_ENV", "development")
		t.Setenv("JWT_SIGNING_KEY", validTestJWTKey)
		t.Setenv("ENCRYPTION_MASTER_KEY", "not-valid-base64-@@@")

		_, err := Load()
		if err == nil {
			t.Fatal("expected failure for invalid base64 ENCRYPTION_MASTER_KEY, got nil")
		}
		if !strings.Contains(err.Error(), "invalid ENCRYPTION_MASTER_KEY: must be valid base64") {
			t.Errorf("unexpected error message: %v", err)
		}
		if strings.Contains(err.Error(), "not-valid-base64") {
			t.Errorf("SECURITY FLAW: raw invalid master key leaked in error: %v", err)
		}
	})

	t.Run("invalid decoded key lengths rejected", func(t *testing.T) {
		invalidLengths := []int{0, 16, 24, 31, 33, 64}
		for _, l := range invalidLengths {
			raw := bytes.Repeat([]byte{0x01}, l)
			encoded := base64.StdEncoding.EncodeToString(raw)

			t.Run(string(rune(l)), func(t *testing.T) {
				t.Setenv("APP_ENV", "development")
				t.Setenv("JWT_SIGNING_KEY", validTestJWTKey)
				t.Setenv("ENCRYPTION_MASTER_KEY", encoded)

				_, err := Load()
				if err == nil {
					t.Fatalf("expected error for key length %d, got nil", l)
				}
				if l == 0 {
					if !strings.Contains(err.Error(), "ENCRYPTION_MASTER_KEY is required") &&
						!strings.Contains(err.Error(), "must decode to exactly 32 bytes") {
						t.Errorf("unexpected error for length 0: %v", err)
					}
				} else {
					if !strings.Contains(err.Error(), "invalid ENCRYPTION_MASTER_KEY: must decode to exactly 32 bytes") {
						t.Errorf("unexpected error for length %d: %v", l, err)
					}
				}
			})
		}
	})

	t.Run("valid 32-byte master key accepted", func(t *testing.T) {
		t.Setenv("APP_ENV", "development")
		setValidRequiredSecrets(t)

		cfg, err := Load()
		if err != nil {
			t.Fatalf("expected valid 32-byte key to succeed, got: %v", err)
		}
		if len(cfg.EncryptionMasterKey) != 32 {
			t.Fatalf("expected 32 bytes length, got %d", len(cfg.EncryptionMasterKey))
		}
		if !bytes.Equal(cfg.EncryptionMasterKey, validTestMasterKeyBytes) {
			t.Errorf("decoded bytes do not match expected")
		}
	})
}

func TestConfig_EncryptionMasterKeyVersionValidation(t *testing.T) {
	t.Run("missing version defaults to 1", func(t *testing.T) {
		t.Setenv("APP_ENV", "development")
		t.Setenv("ENCRYPTION_MASTER_KEY_VERSION", "")
		setValidRequiredSecrets(t)

		cfg, err := Load()
		if err != nil {
			t.Fatalf("expected success, got: %v", err)
		}
		if cfg.EncryptionMasterKeyVersion != 1 {
			t.Errorf("expected default version 1, got %d", cfg.EncryptionMasterKeyVersion)
		}
	})

	t.Run("valid positive versions accepted", func(t *testing.T) {
		validVersions := []struct {
			input    string
			expected int
		}{
			{"1", 1},
			{"2", 2},
			{"42", 42},
			{"2147483647", math.MaxInt32},
		}

		for _, tc := range validVersions {
			t.Run(tc.input, func(t *testing.T) {
				t.Setenv("APP_ENV", "development")
				t.Setenv("ENCRYPTION_MASTER_KEY_VERSION", tc.input)
				setValidRequiredSecrets(t)

				cfg, err := Load()
				if err != nil {
					t.Fatalf("expected version %s to be accepted, got error: %v", tc.input, err)
				}
				if cfg.EncryptionMasterKeyVersion != tc.expected {
					t.Errorf("expected version %d, got %d", tc.expected, cfg.EncryptionMasterKeyVersion)
				}
			})
		}
	})

	t.Run("invalid versions rejected", func(t *testing.T) {
		invalidVersions := []string{
			"0",
			"-1",
			"-100",
			"abc",
			"1.5",
			"2147483648", // Overflows 32-bit signed integer
		}

		for _, iv := range invalidVersions {
			t.Run(iv, func(t *testing.T) {
				t.Setenv("APP_ENV", "development")
				t.Setenv("ENCRYPTION_MASTER_KEY_VERSION", iv)
				setValidRequiredSecrets(t)

				_, err := Load()
				if err == nil {
					t.Fatalf("expected version %s to be rejected, got nil", iv)
				}
				if !strings.Contains(err.Error(), "invalid ENCRYPTION_MASTER_KEY_VERSION: must be an integer between 1 and 2147483647") {
					t.Errorf("unexpected error for version %s: %v", iv, err)
				}
			})
		}
	})
}

func TestConfig_JWTSigningKeyValidation(t *testing.T) {
	t.Run("missing JWT key", func(t *testing.T) {
		t.Setenv("APP_ENV", "development")
		t.Setenv("JWT_SIGNING_KEY", "")
		t.Setenv("ENCRYPTION_MASTER_KEY", validTestMasterKeyBase64)

		_, err := Load()
		if err == nil {
			t.Fatal("expected error when JWT_SIGNING_KEY is missing, got nil")
		}
		if !strings.Contains(err.Error(), "JWT_SIGNING_KEY is required and must be at least 32 bytes") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("short JWT key less than 32 bytes", func(t *testing.T) {
		t.Setenv("APP_ENV", "development")
		t.Setenv("JWT_SIGNING_KEY", "short-key-under-32-bytes")
		t.Setenv("ENCRYPTION_MASTER_KEY", validTestMasterKeyBase64)

		_, err := Load()
		if err == nil {
			t.Fatal("expected error when JWT_SIGNING_KEY < 32 bytes, got nil")
		}
	})

	t.Run("valid JWT key exactly 32 bytes", func(t *testing.T) {
		t.Setenv("APP_ENV", "development")
		t.Setenv("JWT_SIGNING_KEY", "12345678901234567890123456789012")
		t.Setenv("ENCRYPTION_MASTER_KEY", validTestMasterKeyBase64)

		cfg, err := Load()
		if err != nil {
			t.Fatalf("expected success with 32 bytes key, got: %v", err)
		}
		if len(cfg.JWTSigningKey) != 32 {
			t.Errorf("expected 32 bytes length")
		}
	})
}

func TestConfig_CookieSecurityPolicy(t *testing.T) {
	t.Run("production with insecure cookie rejected", func(t *testing.T) {
		t.Setenv("APP_ENV", "production")
		t.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/prod")
		t.Setenv("AUTH_COOKIE_SECURE", "false")
		setValidRequiredSecrets(t)

		_, err := Load()
		if err == nil {
			t.Fatal("expected error in production with AUTH_COOKIE_SECURE=false, got nil")
		}
		if !strings.Contains(err.Error(), "AUTH_COOKIE_SECURE must be true in production") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("production default secure=true accepted", func(t *testing.T) {
		t.Setenv("APP_ENV", "production")
		t.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/prod")
		t.Setenv("AUTH_COOKIE_SECURE", "")
		setValidRequiredSecrets(t)

		cfg, err := Load()
		if err != nil {
			t.Fatalf("expected production to default to secure=true, got error: %v", err)
		}
		if !cfg.AuthCookieSecure {
			t.Errorf("expected AuthCookieSecure = true in production default")
		}
	})

	t.Run("development with secure=false accepted", func(t *testing.T) {
		t.Setenv("APP_ENV", "development")
		t.Setenv("AUTH_COOKIE_SECURE", "false")
		setValidRequiredSecrets(t)

		cfg, err := Load()
		if err != nil {
			t.Fatalf("expected development with secure=false to succeed, got: %v", err)
		}
		if cfg.AuthCookieSecure != false {
			t.Errorf("expected AuthCookieSecure = false")
		}
	})

	t.Run("invalid non-boolean AUTH_COOKIE_SECURE rejected", func(t *testing.T) {
		t.Setenv("APP_ENV", "development")
		t.Setenv("AUTH_COOKIE_SECURE", "not-a-valid-boolean")
		setValidRequiredSecrets(t)

		_, err := Load()
		if err == nil {
			t.Fatal("expected error for non-boolean AUTH_COOKIE_SECURE, got nil")
		}
		if !strings.Contains(err.Error(), "invalid AUTH_COOKIE_SECURE: must be a valid boolean") {
			t.Errorf("unexpected error message: %v", err)
		}
	})
}

func TestConfig_ProductionRequiresDatabaseURL(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	t.Setenv("DATABASE_URL", "")
	setValidRequiredSecrets(t)

	_, err := Load()
	if err == nil {
		t.Fatal("expected error in production when DATABASE_URL is missing, got nil")
	}
}

func TestConfig_InvalidLogLevel(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	t.Setenv("LOG_LEVEL", "invalid_level")
	setValidRequiredSecrets(t)

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for invalid LOG_LEVEL, got nil")
	}
}

func TestConfig_InvalidDatabaseScheme(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	t.Setenv("DATABASE_URL", "mysql://root:pass@localhost:3306/mydb")
	setValidRequiredSecrets(t)

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for non-postgres database scheme, got nil")
	}
	if !strings.Contains(err.Error(), "scheme must be 'postgres' or 'postgresql'") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestConfig_ValidPostgreSQLURL(t *testing.T) {
	testCases := []string{
		"postgres://user:pass@localhost:5432/backup_db?sslmode=disable",
		"postgresql://user:pass@127.0.0.1:5432/backup_db",
	}

	for _, dsn := range testCases {
		t.Run(dsn, func(t *testing.T) {
			t.Setenv("APP_ENV", "development")
			t.Setenv("DATABASE_URL", dsn)
			setValidRequiredSecrets(t)

			cfg, err := Load()
			if err != nil {
				t.Fatalf("expected valid postgres URL to succeed, got: %v", err)
			}
			if cfg.DatabaseURL != dsn {
				t.Errorf("expected DatabaseURL '%s', got '%s'", dsn, cfg.DatabaseURL)
			}
		})
	}
}

func TestConfig_StorageRootValidation(t *testing.T) {
	rejectedPaths := []string{
		"./storage",
		"../storage",
		"storage/data",
		`\storage`,
		`\srv\backup-platform`,
		`C:\backup-platform`,
	}

	for _, p := range rejectedPaths {
		t.Run("rejected:"+p, func(t *testing.T) {
			t.Setenv("APP_ENV", "development")
			t.Setenv("STORAGE_ROOT", p)
			setValidRequiredSecrets(t)

			_, err := Load()
			if err == nil {
				t.Fatalf("expected invalid STORAGE_ROOT '%s' to be rejected, got nil", p)
			}
			if !strings.Contains(err.Error(), "STORAGE_ROOT must be an absolute POSIX path starting with '/'") {
				t.Errorf("unexpected error message: %v", err)
			}
		})
	}

	acceptedPaths := []string{
		"/srv/backup-platform",
		"/data/backups",
	}

	for _, p := range acceptedPaths {
		t.Run("accepted:"+p, func(t *testing.T) {
			t.Setenv("APP_ENV", "development")
			t.Setenv("STORAGE_ROOT", p)
			setValidRequiredSecrets(t)

			cfg, err := Load()
			if err != nil {
				t.Fatalf("expected valid POSIX STORAGE_ROOT '%s' to be accepted, got error: %v", p, err)
			}
			if cfg.StorageRoot != p {
				t.Errorf("expected StorageRoot '%s', got '%s'", p, cfg.StorageRoot)
			}
		})
	}
}

func TestConfig_ValidateDirect(t *testing.T) {
	validCfg := Config{
		AppEnv:                     EnvDevelopment,
		HTTPAddr:                   ":8080",
		DatabaseURL:                "postgres://localhost:5432/db",
		LogLevel:                   "info",
		StorageRoot:                "/srv/backup-platform",
		JWTSigningKey:              validTestJWTKey,
		AuthCookieSecure:           false,
		EncryptionMasterKey:        validTestMasterKeyBytes,
		EncryptionMasterKeyVersion: 1,
	}

	t.Run("valid config succeeds", func(t *testing.T) {
		c := validCfg
		if err := c.Validate(); err != nil {
			t.Fatalf("expected valid config to succeed, got: %v", err)
		}
	})

	t.Run("invalid master key length in Validate", func(t *testing.T) {
		c := validCfg
		c.EncryptionMasterKey = make([]byte, 16)
		if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "ENCRYPTION_MASTER_KEY is required and must decode to exactly 32 bytes") {
			t.Errorf("expected 32-byte master key validation error, got: %v", err)
		}
	})

	t.Run("invalid master key version in Validate", func(t *testing.T) {
		c := validCfg
		c.EncryptionMasterKeyVersion = 0
		if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "ENCRYPTION_MASTER_KEY_VERSION must be an integer between 1 and 2147483647") {
			t.Errorf("expected version validation error, got: %v", err)
		}
	})
}
