package uuid

import (
	"crypto/rand"
	"database/sql/driver"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// UUID represents an RFC 4122 Version 4 universally unique identifier.
type UUID [16]byte

// Nil represents a blank/zero UUID.
var Nil UUID

// New generates a new random RFC 4122 Version 4 UUID.
func New() UUID {
	var u UUID
	_, err := rand.Read(u[:])
	if err != nil {
		panic(fmt.Sprintf("crypto/rand read failed: %v", err))
	}
	u[6] = (u[6] & 0x0f) | 0x40 // Version 4
	u[8] = (u[8] & 0x3f) | 0x80 // Variant RFC 4122
	return u
}

// Parse parses a 36-character hexadecimal UUID string with hyphens into a UUID.
func Parse(s string) (UUID, error) {
	s = strings.TrimSpace(s)
	if len(s) != 36 {
		return Nil, errors.New("invalid UUID string length: must be 36 characters")
	}

	if s[8] != '-' || s[13] != '-' || s[18] != '-' || s[23] != '-' {
		return Nil, errors.New("invalid UUID hyphen format: expected 8-4-4-4-12")
	}

	clean := s[0:8] + s[9:13] + s[14:18] + s[19:23] + s[24:36]
	b, err := hex.DecodeString(clean)
	if err != nil {
		return Nil, fmt.Errorf("invalid UUID hex characters: %w", err)
	}

	var u UUID
	copy(u[:], b)
	return u, nil
}

// String returns the standard 36-character hyphenated UUID string representation.
func (u UUID) String() string {
	buf := make([]byte, 36)
	hex.Encode(buf[0:8], u[0:4])
	buf[8] = '-'
	hex.Encode(buf[9:13], u[4:6])
	buf[13] = '-'
	hex.Encode(buf[14:18], u[6:8])
	buf[18] = '-'
	hex.Encode(buf[19:23], u[8:10])
	buf[23] = '-'
	hex.Encode(buf[24:36], u[10:16])
	return string(buf)
}

// Value implements the database/sql driver.Valuer interface for PostgreSQL UUID mapping.
func (u UUID) Value() (driver.Value, error) {
	return u.String(), nil
}

// Scan implements the database/sql.Scanner interface for PostgreSQL UUID scanning.
func (u *UUID) Scan(src any) error {
	if src == nil {
		*u = Nil
		return nil
	}

	switch v := src.(type) {
	case string:
		parsed, err := Parse(v)
		if err != nil {
			return err
		}
		*u = parsed
		return nil
	case []byte:
		if len(v) == 16 {
			copy(u[:], v)
			return nil
		}
		parsed, err := Parse(string(v))
		if err != nil {
			return err
		}
		*u = parsed
		return nil
	default:
		return fmt.Errorf("unsupported UUID scan source type: %T", src)
	}
}

// MarshalText implements encoding.TextMarshaler for JSON serialization.
func (u UUID) MarshalText() ([]byte, error) {
	return []byte(u.String()), nil
}

// UnmarshalText implements encoding.TextUnmarshaler for JSON deserialization.
func (u *UUID) UnmarshalText(data []byte) error {
	parsed, err := Parse(string(data))
	if err != nil {
		return err
	}
	*u = parsed
	return nil
}
