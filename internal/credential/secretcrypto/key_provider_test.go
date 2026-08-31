package secretcrypto

import (
	"bytes"
	"errors"
	"testing"
)

func TestStaticKeyProvider(t *testing.T) {
	validKey := bytes.Repeat([]byte{0x42}, 32)

	t.Run("successfully constructs with 32-byte key and valid version", func(t *testing.T) {
		p, err := NewStaticKeyProvider(validKey, 1)
		if err != nil {
			t.Fatalf("expected success, got error: %v", err)
		}

		key, v, err := p.Current()
		if err != nil {
			t.Fatalf("expected success on Current, got error: %v", err)
		}
		if v != 1 {
			t.Errorf("expected version 1, got %d", v)
		}
		if !bytes.Equal(key, validKey) {
			t.Errorf("expected key to match input key")
		}

		byV, err := p.ByVersion(1)
		if err != nil {
			t.Fatalf("expected success on ByVersion, got error: %v", err)
		}
		if !bytes.Equal(byV, validKey) {
			t.Errorf("expected key to match input key")
		}
	})

	t.Run("rejects invalid key lengths", func(t *testing.T) {
		invalidLengths := []int{0, 16, 24, 31, 33, 64}
		for _, l := range invalidLengths {
			k := bytes.Repeat([]byte{0x01}, l)
			_, err := NewStaticKeyProvider(k, 1)
			if !errors.Is(err, ErrInvalidKeyLength) {
				t.Errorf("expected ErrInvalidKeyLength for length %d, got: %v", l, err)
			}
		}
	})

	t.Run("rejects key version less than 1", func(t *testing.T) {
		invalidVersions := []int{0, -1, -100}
		for _, v := range invalidVersions {
			_, err := NewStaticKeyProvider(validKey, v)
			if !errors.Is(err, ErrInvalidKeyVersion) {
				t.Errorf("expected ErrInvalidKeyVersion for version %d, got: %v", v, err)
			}
		}
	})

	t.Run("returns ErrUnknownKeyVersion for mismatched versions", func(t *testing.T) {
		p, err := NewStaticKeyProvider(validKey, 1)
		if err != nil {
			t.Fatalf("setup failed: %v", err)
		}

		_, err = p.ByVersion(2)
		if !errors.Is(err, ErrUnknownKeyVersion) {
			t.Errorf("expected ErrUnknownKeyVersion for version 2, got: %v", err)
		}

		_, err = p.ByVersion(0)
		if !errors.Is(err, ErrInvalidKeyVersion) {
			t.Errorf("expected ErrInvalidKeyVersion for version 0, got: %v", err)
		}
	})

	t.Run("defensive copy protects against constructor slice mutation", func(t *testing.T) {
		inputKey := bytes.Repeat([]byte{0xAA}, 32)
		p, err := NewStaticKeyProvider(inputKey, 1)
		if err != nil {
			t.Fatalf("setup failed: %v", err)
		}

		// Mutate original slice
		for i := range inputKey {
			inputKey[i] = 0x00
		}

		// Provider internal key must remain 0xAA
		retrievedKey, _, err := p.Current()
		if err != nil {
			t.Fatalf("Current failed: %v", err)
		}
		expectedKey := bytes.Repeat([]byte{0xAA}, 32)
		if !bytes.Equal(retrievedKey, expectedKey) {
			t.Errorf("provider state was corrupted by external slice mutation")
		}
	})

	t.Run("defensive copy protects against getter slice mutation", func(t *testing.T) {
		inputKey := bytes.Repeat([]byte{0xBB}, 32)
		p, err := NewStaticKeyProvider(inputKey, 1)
		if err != nil {
			t.Fatalf("setup failed: %v", err)
		}

		// Get key and mutate it
		retrievedKey, _, _ := p.Current()
		for i := range retrievedKey {
			retrievedKey[i] = 0xFF
		}

		// Subsequent call must return unmutated key
		nextKey, _, _ := p.Current()
		expectedKey := bytes.Repeat([]byte{0xBB}, 32)
		if !bytes.Equal(nextKey, expectedKey) {
			t.Errorf("subsequent Current call returned mutated state")
		}

		byVKey, _ := p.ByVersion(1)
		if !bytes.Equal(byVKey, expectedKey) {
			t.Errorf("subsequent ByVersion call returned mutated state")
		}
	})
}
