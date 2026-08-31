package domain

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"path"
	"strings"
	"unicode/utf8"

	"backup-platform/internal/connector"
)

const (
	// MaxTargetPaths defines the maximum number of source paths in a single website backup job.
	MaxTargetPaths = 50

	// MaxExcludePatterns defines the maximum number of exclude patterns in a single website backup job.
	MaxExcludePatterns = 100

	// MaxTargetPathRunes defines the maximum length of a normalized target path in Unicode characters.
	MaxTargetPathRunes = 255

	// MaxExcludePatternRunes defines the maximum length of an exclude pattern in Unicode characters.
	MaxExcludePatternRunes = 255
)

// TargetSpec defines the typed workload targets for a backup job or plan.
type TargetSpec struct {
	Databases       []string  `json:"databases,omitempty"`
	Paths           []string  `json:"paths,omitempty"`
	ExcludePatterns *[]string `json:"exclude_patterns,omitempty"`
}

// GetExcludePatterns returns the exclude patterns slice or nil if omitted.
func (s TargetSpec) GetExcludePatterns() []string {
	if s.ExcludePatterns == nil {
		return nil
	}
	return *s.ExcludePatterns
}

// MarshalJSON provides clean, workload-specific JSON representation:
// - mysql_database: {"databases": [...]} (empty slice if all databases targeted)
// - website_files:  {"paths": [...], "exclude_patterns": [...]}
func (s TargetSpec) MarshalJSON() ([]byte, error) {
	if s.Paths != nil || s.ExcludePatterns != nil {
		type websiteSpec struct {
			Paths           []string `json:"paths"`
			ExcludePatterns []string `json:"exclude_patterns"`
		}
		excludes := []string{}
		if s.ExcludePatterns != nil {
			excludes = *s.ExcludePatterns
		}
		return json.Marshal(websiteSpec{
			Paths:           s.Paths,
			ExcludePatterns: excludes,
		})
	}

	type dbSpec struct {
		Databases []string `json:"databases"`
	}
	dbs := s.Databases
	if dbs == nil {
		dbs = []string{}
	}
	return json.Marshal(dbSpec{Databases: dbs})
}

// UnmarshalJSON strictly unmarshals and differentiates omitted vs empty exclude_patterns.
func (s *TargetSpec) UnmarshalJSON(data []byte) error {
	type rawSpec struct {
		Databases       *[]string       `json:"databases"`
		Paths           *[]string       `json:"paths"`
		ExcludePatterns json.RawMessage `json:"exclude_patterns"`
	}
	var raw rawSpec
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&raw); err != nil {
		return err
	}

	if raw.Databases != nil {
		s.Databases = *raw.Databases
	}
	if raw.Paths != nil {
		s.Paths = *raw.Paths
	}
	if len(raw.ExcludePatterns) > 0 {
		if bytes.Equal(bytes.TrimSpace(raw.ExcludePatterns), []byte("null")) {
			return errors.New("exclude_patterns cannot be null")
		}
		var ep []string
		if err := json.Unmarshal(raw.ExcludePatterns, &ep); err != nil {
			return err
		}
		s.ExcludePatterns = &ep
	}
	return nil
}

// ValidateAndNormalizePOSIXPath verifies that rawPath is a valid, secure absolute POSIX path
// and returns its canonical normalized representation.
func ValidateAndNormalizePOSIXPath(rawPath string) (string, error) {
	if rawPath == "" {
		return "", errors.New("path cannot be empty")
	}

	if !utf8.ValidString(rawPath) {
		return "", errors.New("path contains invalid UTF-8 characters")
	}

	// Reject NUL byte, control characters (ASCII 0-31 and 127)
	for i := 0; i < len(rawPath); i++ {
		c := rawPath[i]
		if c < 32 || c == 127 {
			return "", errors.New("path contains control characters or NUL byte")
		}
	}

	// Reject backslash to eliminate Windows/POSIX ambiguity
	if strings.Contains(rawPath, "\\") {
		return "", errors.New("backslash characters are forbidden in POSIX paths")
	}

	// Must be an absolute POSIX path starting with '/'
	if !strings.HasPrefix(rawPath, "/") {
		return "", errors.New("path must be an absolute POSIX path starting with '/'")
	}

	// Reject exact root path "/"
	if rawPath == "/" {
		return "", errors.New("root path '/' cannot be targeted for website backup")
	}

	// Reject parent traversal ("..") segments before path.Clean
	segments := strings.Split(rawPath, "/")
	for _, seg := range segments {
		if seg == ".." {
			return "", errors.New("parent traversal segments '..' are strictly forbidden")
		}
	}

	// Canonicalize path using standard POSIX path.Clean
	cleaned := path.Clean(rawPath)
	if cleaned == "/" {
		return "", errors.New("root path '/' cannot be targeted for website backup")
	}

	if utf8.RuneCountInString(cleaned) > MaxTargetPathRunes {
		return "", errors.New("normalized path exceeds maximum length of 255 characters")
	}

	return cleaned, nil
}

// ValidateExcludePattern verifies that pattern satisfies all domain and safety constraints.
func ValidateExcludePattern(pattern string) error {
	if pattern == "" {
		return errors.New("exclude pattern cannot be empty")
	}

	if strings.TrimSpace(pattern) == "" {
		return errors.New("exclude pattern cannot be whitespace-only")
	}

	if !utf8.ValidString(pattern) {
		return errors.New("exclude pattern contains invalid UTF-8 characters")
	}

	for i := 0; i < len(pattern); i++ {
		c := pattern[i]
		if c < 32 || c == 127 {
			return errors.New("exclude pattern contains control characters or NUL byte")
		}
	}

	if utf8.RuneCountInString(pattern) > MaxExcludePatternRunes {
		return errors.New("exclude pattern exceeds maximum length of 255 characters")
	}

	return nil
}

// ValidateTargetSpec verifies that a TargetSpec satisfies all domain constraints for the specified BackupType.
func ValidateTargetSpec(backupType BackupType, spec *TargetSpec) error {
	if spec == nil {
		return errors.New("target specification cannot be nil")
	}

	switch backupType {
	case BackupTypeMySQLDatabase:
		if spec.Paths != nil || spec.ExcludePatterns != nil {
			return errors.New("paths and exclude_patterns are not allowed for mysql_database backup")
		}
		if len(spec.Databases) == 0 {
			// Empty databases slice represents all databases mode
			return nil
		}

		seen := make(map[string]struct{}, len(spec.Databases))
		for _, db := range spec.Databases {
			if err := connector.ValidateDatabaseName(db); err != nil {
				return err
			}
			if connector.IsSystemDatabase(db) {
				return errors.New("system databases cannot be targeted for backup")
			}
			if _, exists := seen[db]; exists {
				return errors.New("duplicate database target in specification")
			}
			seen[db] = struct{}{}
		}
		return nil

	case BackupTypeWebsiteFiles:
		if len(spec.Databases) > 0 {
			return errors.New("databases are not allowed for website_files backup")
		}
		if len(spec.Paths) == 0 {
			return errors.New("at least one path target is required for website_files backup")
		}
		if len(spec.Paths) > MaxTargetPaths {
			return errors.New("maximum number of paths exceeded (limit: 50)")
		}
		if spec.ExcludePatterns == nil {
			return errors.New("exclude_patterns field is required for website_files backup")
		}
		if len(*spec.ExcludePatterns) > MaxExcludePatterns {
			return errors.New("maximum number of exclude patterns exceeded (limit: 100)")
		}

		seenPaths := make(map[string]struct{}, len(spec.Paths))
		for _, rawPath := range spec.Paths {
			cleaned, err := ValidateAndNormalizePOSIXPath(rawPath)
			if err != nil {
				return err
			}
			if _, exists := seenPaths[cleaned]; exists {
				return errors.New("duplicate path target in specification")
			}
			seenPaths[cleaned] = struct{}{}
		}

		seenPatterns := make(map[string]struct{}, len(*spec.ExcludePatterns))
		for _, pattern := range *spec.ExcludePatterns {
			if err := ValidateExcludePattern(pattern); err != nil {
				return err
			}
			if _, exists := seenPatterns[pattern]; exists {
				return errors.New("duplicate exclude pattern in specification")
			}
			seenPatterns[pattern] = struct{}{}
		}
		return nil

	default:
		return errors.New("unsupported backup type for target specification validation")
	}
}

// NormalizeTargetSpec validates and returns a normalized copy of TargetSpec.
// For website_files, paths are converted to canonical normalized POSIX form.
// For mysql_database, database names are preserved without modifications.
func NormalizeTargetSpec(backupType BackupType, spec *TargetSpec) (*TargetSpec, error) {
	if err := ValidateTargetSpec(backupType, spec); err != nil {
		return nil, err
	}

	if backupType == BackupTypeWebsiteFiles {
		normalizedPaths := make([]string, len(spec.Paths))
		for i, p := range spec.Paths {
			cleaned, _ := ValidateAndNormalizePOSIXPath(p)
			normalizedPaths[i] = cleaned
		}
		patterns := make([]string, len(*spec.ExcludePatterns))
		copy(patterns, *spec.ExcludePatterns)

		return &TargetSpec{
			Paths:           normalizedPaths,
			ExcludePatterns: &patterns,
		}, nil
	}

	return spec, nil
}

// DecodeStrictTargetSpec strictly deserializes and validates a JSON byte slice into a TargetSpec.
func DecodeStrictTargetSpec(backupType BackupType, data []byte) (*TargetSpec, error) {
	if len(data) == 0 {
		return nil, ErrInvalidTargetSpec
	}

	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()

	var spec TargetSpec
	if err := dec.Decode(&spec); err != nil {
		return nil, ErrInvalidTargetSpec
	}

	var trailing any
	if err := dec.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, ErrInvalidTargetSpec
	}

	if err := ValidateTargetSpec(backupType, &spec); err != nil {
		return nil, ErrInvalidTargetSpec
	}

	return &spec, nil
}
