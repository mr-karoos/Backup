package connector

import (
	"context"
	"errors"
	"testing"

	"backup-platform/internal/credential/payload"
	resDomain "backup-platform/internal/resource/domain"
)

type dummyTester struct{}

func (d *dummyTester) TestConnection(ctx context.Context, target Target, credPayload *payload.PayloadV1) (*ProbeResult, error) {
	return &ProbeResult{Success: true}, nil
}

func TestRegistry_RegisterAndGet(t *testing.T) {
	r := NewRegistry()

	// Before registration
	_, err := r.Get(resDomain.TypeUbuntuSSH)
	if !errors.Is(err, ErrNoTesterRegistered) {
		t.Errorf("expected ErrNoTesterRegistered, got: %v", err)
	}

	tester := &dummyTester{}
	r.Register(resDomain.TypeUbuntuSSH, tester)

	got, err := r.Get(resDomain.TypeUbuntuSSH)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != tester {
		t.Errorf("expected registered tester, got: %v", got)
	}

	// Another type still unmapped
	_, err = r.Get(resDomain.TypeCPanel)
	if !errors.Is(err, ErrNoTesterRegistered) {
		t.Errorf("expected ErrNoTesterRegistered for cpanel, got: %v", err)
	}
}
