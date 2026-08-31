package payload

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

var (
	// ErrInvalidPayload indicates that the payload JSON is malformed, missing required fields, or has unexpected trailing data.
	ErrInvalidPayload = errors.New("invalid credential payload")

	// ErrUnsupportedVersion indicates that the payload version is not supported.
	ErrUnsupportedVersion = errors.New("unsupported credential payload version")
)

// PayloadV1 represents version 1 of the structured credential plaintext payload.
type PayloadV1 struct {
	Version    int     `json:"version"`
	Secret     string  `json:"secret"`
	Passphrase *string `json:"passphrase,omitempty"`
}

// Clear releases references to sensitive string fields in a PayloadV1 instance on a best-effort basis.
func Clear(p *PayloadV1) {
	if p == nil {
		return
	}
	p.Secret = ""
	p.Passphrase = nil
}

// EncodeV1 serializes a secret and optional passphrase into standard PayloadV1 JSON bytes.
// It enforces that secret is non-empty (len(secret) > 0) without trimming whitespace.
func EncodeV1(secret string, passphrase *string) ([]byte, error) {
	if len(secret) == 0 {
		return nil, ErrInvalidPayload
	}

	p := PayloadV1{
		Version:    1,
		Secret:     secret,
		Passphrase: passphrase,
	}

	data, err := json.Marshal(p)
	// Best-effort release of intermediate struct references
	p.Secret = ""
	p.Passphrase = nil

	return data, err
}

// Decode strictly deserializes and validates a credential plaintext JSON payload.
// It enforces:
// 1. Strict DisallowUnknownFields
// 2. Exactly one JSON value (second decode must yield io.EOF)
// 3. Version must equal 1
// 4. Secret must be non-empty
func Decode(data []byte) (*PayloadV1, error) {
	if len(data) == 0 {
		return nil, ErrInvalidPayload
	}

	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()

	var p PayloadV1
	if err := dec.Decode(&p); err != nil {
		return nil, ErrInvalidPayload
	}

	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, ErrInvalidPayload
	}

	if p.Version != 1 {
		return nil, ErrUnsupportedVersion
	}

	if len(p.Secret) == 0 {
		return nil, ErrInvalidPayload
	}

	return &p, nil
}
