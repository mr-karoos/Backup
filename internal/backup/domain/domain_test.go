package domain

import (
	"strings"
	"testing"
)

func TestValidateTargetSpec_MySQL(t *testing.T) {
	t.Run("Valid single database target", func(t *testing.T) {
		spec := &TargetSpec{
			Databases: []string{"ecommerce_prod"},
		}
		if err := ValidateTargetSpec(BackupTypeMySQLDatabase, spec); err != nil {
			t.Fatalf("unexpected error for valid spec: %v", err)
		}
	})

	t.Run("Valid multiple database targets with exact identity", func(t *testing.T) {
		spec := &TargetSpec{
			Databases: []string{"ecommerce_prod", "analytics_dw", " app_db "},
		}
		if err := ValidateTargetSpec(BackupTypeMySQLDatabase, spec); err != nil {
			t.Fatalf("unexpected error for valid multiple spec: %v", err)
		}
	})

	t.Run("Nil target spec fails", func(t *testing.T) {
		if err := ValidateTargetSpec(BackupTypeMySQLDatabase, nil); err == nil {
			t.Errorf("expected error for nil spec")
		}
	})

	t.Run("Empty databases list is valid (represents all databases)", func(t *testing.T) {
		spec := &TargetSpec{Databases: []string{}}
		if err := ValidateTargetSpec(BackupTypeMySQLDatabase, spec); err != nil {
			t.Errorf("unexpected error for empty database list (mode all): %v", err)
		}
	})

	t.Run("Rejects duplicate database targets", func(t *testing.T) {
		spec := &TargetSpec{
			Databases: []string{"prod_db", "prod_db"},
		}
		if err := ValidateTargetSpec(BackupTypeMySQLDatabase, spec); err == nil {
			t.Errorf("expected error for duplicate databases")
		}
	})

	t.Run("Rejects system databases", func(t *testing.T) {
		sysDBs := []string{"information_schema", "mysql", "performance_schema", "sys", "INFORMATION_SCHEMA", "MySQL"}
		for _, db := range sysDBs {
			spec := &TargetSpec{Databases: []string{db}}
			if err := ValidateTargetSpec(BackupTypeMySQLDatabase, spec); err == nil {
				t.Errorf("expected error for system database %q", db)
			}
		}
	})

	t.Run("Rejects empty or whitespace-only database names", func(t *testing.T) {
		invalidNames := []string{"", "   ", "\t", "\n"}
		for _, db := range invalidNames {
			spec := &TargetSpec{Databases: []string{db}}
			if err := ValidateTargetSpec(BackupTypeMySQLDatabase, spec); err == nil {
				t.Errorf("expected error for invalid name %q", db)
			}
		}
	})

	t.Run("Rejects database names containing control characters", func(t *testing.T) {
		controlNames := []string{"db\x00name", "db\rname", "db\nname"}
		for _, db := range controlNames {
			spec := &TargetSpec{Databases: []string{db}}
			if err := ValidateTargetSpec(BackupTypeMySQLDatabase, spec); err == nil {
				t.Errorf("expected error for control char name %q", db)
			}
		}
	})

	t.Run("Rejects database names exceeding 64 characters", func(t *testing.T) {
		longName := strings.Repeat("a", 65)
		spec := &TargetSpec{Databases: []string{longName}}
		if err := ValidateTargetSpec(BackupTypeMySQLDatabase, spec); err == nil {
			t.Errorf("expected error for name exceeding 64 chars")
		}
	})

	t.Run("DecodeStrictTargetSpec enforces strict decoding", func(t *testing.T) {
		validJSON := []byte(`{"databases":["prod_db"]}`)
		spec, err := DecodeStrictTargetSpec(BackupTypeMySQLDatabase, validJSON)
		if err != nil || len(spec.Databases) != 1 || spec.Databases[0] != "prod_db" {
			t.Fatalf("expected valid decode, got: %v, spec: %+v", err, spec)
		}

		unknownFieldJSON := []byte(`{"databases":["prod_db"],"extra":"field"}`)
		if _, err := DecodeStrictTargetSpec(BackupTypeMySQLDatabase, unknownFieldJSON); err == nil {
			t.Errorf("expected error for unknown field")
		}

		trailingJSON := []byte(`{"databases":["prod_db"]}{"extra":true}`)
		if _, err := DecodeStrictTargetSpec(BackupTypeMySQLDatabase, trailingJSON); err == nil {
			t.Errorf("expected error for trailing JSON value")
		}

		emptyBytes := []byte(``)
		if _, err := DecodeStrictTargetSpec(BackupTypeMySQLDatabase, emptyBytes); err == nil {
			t.Errorf("expected error for empty bytes")
		}
	})

	t.Run("Rejects paths or exclude_patterns in MySQL target spec", func(t *testing.T) {
		spec := &TargetSpec{
			Databases: []string{"prod_db"},
			Paths:     []string{"/var/www/site"},
		}
		if err := ValidateTargetSpec(BackupTypeMySQLDatabase, spec); err == nil {
			t.Errorf("expected error when paths provided for mysql_database")
		}
	})
}

func TestValidateTargetSpec_WebsiteFiles(t *testing.T) {
	emptyExcludes := []string{}
	validExcludes := []string{"*.log", "cache/*", ".git"}

	t.Run("Valid single path and empty excludes", func(t *testing.T) {
		spec := &TargetSpec{
			Paths:           []string{"/var/www/example.com"},
			ExcludePatterns: &emptyExcludes,
		}
		if err := ValidateTargetSpec(BackupTypeWebsiteFiles, spec); err != nil {
			t.Fatalf("unexpected error for valid website spec: %v", err)
		}
	})

	t.Run("Valid multiple paths and patterns", func(t *testing.T) {
		spec := &TargetSpec{
			Paths:           []string{"/var/www/site1", "/home/user/public_html"},
			ExcludePatterns: &validExcludes,
		}
		if err := ValidateTargetSpec(BackupTypeWebsiteFiles, spec); err != nil {
			t.Fatalf("unexpected error for multiple paths: %v", err)
		}
	})

	t.Run("NormalizeTargetSpec canonicalizes paths but preserves order", func(t *testing.T) {
		spec := &TargetSpec{
			Paths:           []string{"/var/www/site1/", "/var//www/./site2/"},
			ExcludePatterns: &emptyExcludes,
		}
		norm, err := NormalizeTargetSpec(BackupTypeWebsiteFiles, spec)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(norm.Paths) != 2 || norm.Paths[0] != "/var/www/site1" || norm.Paths[1] != "/var/www/site2" {
			t.Errorf("expected normalized paths, got: %v", norm.Paths)
		}
	})

	t.Run("Rejects omitted exclude_patterns", func(t *testing.T) {
		spec := &TargetSpec{
			Paths:           []string{"/var/www/site"},
			ExcludePatterns: nil,
		}
		if err := ValidateTargetSpec(BackupTypeWebsiteFiles, spec); err == nil {
			t.Errorf("expected error when exclude_patterns is omitted")
		}
	})

	t.Run("Rejects databases field in website_files spec", func(t *testing.T) {
		spec := &TargetSpec{
			Databases:       []string{"db1"},
			Paths:           []string{"/var/www/site"},
			ExcludePatterns: &emptyExcludes,
		}
		if err := ValidateTargetSpec(BackupTypeWebsiteFiles, spec); err == nil {
			t.Errorf("expected error when databases provided for website_files")
		}
	})

	t.Run("Rejects relative paths", func(t *testing.T) {
		invalidPaths := []string{"public_html", "./site", "var/www/site"}
		for _, p := range invalidPaths {
			spec := &TargetSpec{
				Paths:           []string{p},
				ExcludePatterns: &emptyExcludes,
			}
			if err := ValidateTargetSpec(BackupTypeWebsiteFiles, spec); err == nil {
				t.Errorf("expected error for relative path %q", p)
			}
		}
	})

	t.Run("Rejects root path", func(t *testing.T) {
		spec := &TargetSpec{
			Paths:           []string{"/"},
			ExcludePatterns: &emptyExcludes,
		}
		if err := ValidateTargetSpec(BackupTypeWebsiteFiles, spec); err == nil {
			t.Errorf("expected error for root path '/'")
		}
	})

	t.Run("Rejects parent traversal segments before clean", func(t *testing.T) {
		traversalPaths := []string{"/var/www/../etc", "/var/../www", "/home/user/../../etc/passwd"}
		for _, p := range traversalPaths {
			spec := &TargetSpec{
				Paths:           []string{p},
				ExcludePatterns: &emptyExcludes,
			}
			if err := ValidateTargetSpec(BackupTypeWebsiteFiles, spec); err == nil {
				t.Errorf("expected error for traversal path %q", p)
			}
		}
	})

	t.Run("Rejects backslashes and control characters in paths", func(t *testing.T) {
		invalidPaths := []string{`C:\var\www`, `/var/www/site\name`, "/var/www/site\x00", "/var/www/site\n"}
		for _, p := range invalidPaths {
			spec := &TargetSpec{
				Paths:           []string{p},
				ExcludePatterns: &emptyExcludes,
			}
			if err := ValidateTargetSpec(BackupTypeWebsiteFiles, spec); err == nil {
				t.Errorf("expected error for invalid path %q", p)
			}
		}
	})

	t.Run("Rejects duplicate paths after normalization", func(t *testing.T) {
		spec := &TargetSpec{
			Paths:           []string{"/var/www/site", "/var/www/site/"},
			ExcludePatterns: &emptyExcludes,
		}
		if err := ValidateTargetSpec(BackupTypeWebsiteFiles, spec); err == nil {
			t.Errorf("expected error for duplicate paths after normalization")
		}
	})

	t.Run("Rejects invalid exclude patterns", func(t *testing.T) {
		invalidPatterns := [][]string{
			{""},
			{"   "},
			{"*.log\x00"},
			{"*.log\n"},
			{"*.log", "*.log"}, // duplicate
		}
		for _, pat := range invalidPatterns {
			pCopy := pat
			spec := &TargetSpec{
				Paths:           []string{"/var/www/site"},
				ExcludePatterns: &pCopy,
			}
			if err := ValidateTargetSpec(BackupTypeWebsiteFiles, spec); err == nil {
				t.Errorf("expected error for invalid pattern list: %v", pat)
			}
		}
	})

	t.Run("Preserves exact exclude pattern string identity", func(t *testing.T) {
		patterns := []string{" cache/* ", "*.log"}
		spec := &TargetSpec{
			Paths:           []string{"/var/www/site"},
			ExcludePatterns: &patterns,
		}
		if err := ValidateTargetSpec(BackupTypeWebsiteFiles, spec); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if (*spec.ExcludePatterns)[0] != " cache/* " {
			t.Errorf("expected pattern identity preserved, got %q", (*spec.ExcludePatterns)[0])
		}
	})

	t.Run("JSON round-trip and strict decoding", func(t *testing.T) {
		validJSON := []byte(`{"paths":["/var/www/site"],"exclude_patterns":["*.log"]}`)
		spec, err := DecodeStrictTargetSpec(BackupTypeWebsiteFiles, validJSON)
		if err != nil {
			t.Fatalf("unexpected decode error: %v", err)
		}
		if len(spec.Paths) != 1 || spec.Paths[0] != "/var/www/site" || len(*spec.ExcludePatterns) != 1 {
			t.Errorf("unexpected decoded spec: %+v", spec)
		}

		// Rejects omitted exclude_patterns in JSON
		omittedJSON := []byte(`{"paths":["/var/www/site"]}`)
		if _, err := DecodeStrictTargetSpec(BackupTypeWebsiteFiles, omittedJSON); err == nil {
			t.Errorf("expected error for omitted exclude_patterns")
		}

		// Rejects null exclude_patterns in JSON
		nullJSON := []byte(`{"paths":["/var/www/site"],"exclude_patterns":null}`)
		if _, err := DecodeStrictTargetSpec(BackupTypeWebsiteFiles, nullJSON); err == nil {
			t.Errorf("expected error for null exclude_patterns")
		}
	})
}

func TestValidateS3TargetConfig_RegionSecurity(t *testing.T) {
	validRegions := []string{
		"us-east-1",
		"eu-central-1",
		"us-gov-west-1",
		"auto",
		"ap-southeast-2",
		"a",
		"a1-b2-c3",
	}

	for _, reg := range validRegions {
		cfg := &S3TargetConfig{
			Bucket: "valid-bucket",
			Region: reg,
		}
		if err := ValidateS3TargetConfig(cfg); err != nil {
			t.Errorf("expected valid region %q to be accepted, got error: %v", reg, err)
		}
	}

	invalidRegions := []struct {
		name   string
		region string
	}{
		{"empty", ""},
		{"malicious url authority injection", "us-east-1.amazonaws.com@169.254.169.254/"},
		{"slash delimiter", "us-east-1/test"},
		{"backslash delimiter", `us-east-1\test`},
		{"at sign", "us-east-1@badhost"},
		{"colon port", "us-east-1:8080"},
		{"query param", "us-east-1?param=val"},
		{"fragment", "us-east-1#frag"},
		{"ipv6 bracket", "[::1]"},
		{"url encoding", "us%20east"},
		{"spaces", "us east 1"},
		{"leading space", " us-east-1"},
		{"trailing space", "us-east-1 "},
		{"control character null", "us-east-1\x00"},
		{"control character crlf", "us-east-1\r\n"},
		{"uppercase characters", "US-EAST-1"},
		{"dot in region", "us.east.1"},
		{"starts with hyphen", "-us-east-1"},
		{"too long", strings.Repeat("a", 64)},
	}

	for _, tc := range invalidRegions {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &S3TargetConfig{
				Bucket: "valid-bucket",
				Region: tc.region,
			}
			if err := ValidateS3TargetConfig(cfg); err == nil {
				t.Errorf("SECURITY DEFECT: expected region %q to be rejected, but was accepted", tc.region)
			}
		})
	}
}
