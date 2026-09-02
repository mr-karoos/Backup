package payload

import (
	"errors"
	"testing"
)

func TestEncodeDecodeS3V1_Success(t *testing.T) {
	ak := "AKIAIOSFODNN7EXAMPLE"
	sk := "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"
	st := "session-token-xyz"

	data, err := EncodeS3V1(ak, sk, &st)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	decoded, err := DecodeS3(data)
	if err != nil {
		t.Fatalf("unexpected decode error: %v", err)
	}

	if decoded.Version != 1 {
		t.Errorf("expected version 1, got %d", decoded.Version)
	}
	if decoded.AccessKeyID != ak {
		t.Errorf("expected access key %s, got %s", ak, decoded.AccessKeyID)
	}
	if decoded.SecretAccessKey != sk {
		t.Errorf("expected secret key %s, got %s", sk, decoded.SecretAccessKey)
	}
	if decoded.SessionToken == nil || *decoded.SessionToken != st {
		t.Errorf("expected session token %s, got %v", st, decoded.SessionToken)
	}

	ClearS3(decoded)
	if decoded.AccessKeyID != "" || decoded.SecretAccessKey != "" || decoded.SessionToken != nil {
		t.Errorf("expected cleared fields, got %+v", decoded)
	}
}

func TestEncodeS3V1_Validation(t *testing.T) {
	tests := []struct {
		name string
		ak   string
		sk   string
	}{
		{"empty ak", "", "secret"},
		{"empty sk", "key", ""},
		{"both empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := EncodeS3V1(tt.ak, tt.sk, nil)
			if !errors.Is(err, ErrInvalidPayload) {
				t.Errorf("expected ErrInvalidPayload, got %v", err)
			}
		})
	}
}

func TestDecodeS3_Rejections(t *testing.T) {
	tests := []struct {
		name        string
		data        string
		expectedErr error
	}{
		{"empty payload", "", ErrInvalidPayload},
		{"malformed json", "{not-json}", ErrInvalidPayload},
		{"unknown field", `{"version":1,"access_key_id":"a","secret_access_key":"b","extra":"c"}`, ErrInvalidPayload},
		{"multiple json objects", `{"version":1,"access_key_id":"a","secret_access_key":"b"}{"extra":true}`, ErrInvalidPayload},
		{"unsupported version", `{"version":2,"access_key_id":"a","secret_access_key":"b"}`, ErrUnsupportedVersion},
		{"missing access key", `{"version":1,"access_key_id":"","secret_access_key":"b"}`, ErrInvalidPayload},
		{"missing secret key", `{"version":1,"access_key_id":"a","secret_access_key":""}`, ErrInvalidPayload},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := DecodeS3([]byte(tt.data))
			if !errors.Is(err, tt.expectedErr) {
				t.Errorf("expected %v, got %v", tt.expectedErr, err)
			}
		})
	}
}
