package payload

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

// S3PayloadV1 represents version 1 of the structured S3 credential plaintext payload.
// Contains secrets required to authenticate against AWS S3 and S3-compatible object storage.
type S3PayloadV1 struct {
	Version         int     `json:"version"`
	AccessKeyID     string  `json:"access_key_id"`
	SecretAccessKey string  `json:"secret_access_key"`
	SessionToken    *string `json:"session_token,omitempty"`
}

// ClearS3 releases references to sensitive secret strings in an S3PayloadV1 instance.
func ClearS3(p *S3PayloadV1) {
	if p == nil {
		return
	}
	p.AccessKeyID = ""
	p.SecretAccessKey = ""
	p.SessionToken = nil
}

// EncodeS3V1 serializes S3 access credentials into standard S3PayloadV1 JSON bytes.
// Enforces that accessKeyID and secretAccessKey are strictly non-empty.
func EncodeS3V1(accessKeyID, secretAccessKey string, sessionToken *string) ([]byte, error) {
	if len(accessKeyID) == 0 || len(secretAccessKey) == 0 {
		return nil, ErrInvalidPayload
	}

	p := S3PayloadV1{
		Version:         1,
		AccessKeyID:     accessKeyID,
		SecretAccessKey: secretAccessKey,
		SessionToken:    sessionToken,
	}

	data, err := json.Marshal(p)
	ClearS3(&p)
	return data, err
}

// DecodeS3 strictly deserializes and validates an S3 credential plaintext JSON payload.
// Enforces:
// 1. Strict DisallowUnknownFields
// 2. Exactly one JSON value (second decode must yield io.EOF)
// 3. Version must equal 1
// 4. AccessKeyID and SecretAccessKey must be non-empty
func DecodeS3(data []byte) (*S3PayloadV1, error) {
	if len(data) == 0 {
		return nil, ErrInvalidPayload
	}

	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()

	var p S3PayloadV1
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

	if len(p.AccessKeyID) == 0 || len(p.SecretAccessKey) == 0 {
		return nil, ErrInvalidPayload
	}

	return &p, nil
}
