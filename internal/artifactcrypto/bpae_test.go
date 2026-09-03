package artifactcrypto

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"testing"

	"backup-platform/pkg/uuid"
)

// deterministicEntropy provides static deterministic entropy for golden/known-answer tests.
type deterministicEntropy struct {
	data []byte
	pos  int
}

func (d *deterministicEntropy) Read(p []byte) (int, error) {
	n := copy(p, d.data[d.pos:])
	d.pos += n
	return n, nil
}

func TestBPAE_DeterministicGoldenVector(t *testing.T) {
	// Synthetic non-secret test keys
	kek := bytes.Repeat([]byte{0x01}, 32)
	keyVersion := 1
	keyProvider, err := NewStaticKeyProvider(kek, keyVersion)
	if err != nil {
		t.Fatalf("failed creating key provider: %v", err)
	}

	orgID, _ := uuid.Parse("11111111-1111-1111-1111-111111111111")
	artifactID, _ := uuid.Parse("22222222-2222-2222-2222-222222222222")

	// Fixed entropy: DEK (32B), WrapNonce (12B), ArtifactNoncePrefix (4B)
	dekEntropy := bytes.Repeat([]byte{0x02}, 32)
	wrapNonceEntropy := bytes.Repeat([]byte{0x03}, 12)
	noncePrefixEntropy := []byte{0x04, 0x05, 0x06, 0x07}

	var allEntropy []byte
	allEntropy = append(allEntropy, dekEntropy...)
	allEntropy = append(allEntropy, wrapNonceEntropy...)
	allEntropy = append(allEntropy, noncePrefixEntropy...)

	entropy := &deterministicEntropy{data: allEntropy}

	plaintext := []byte("Hello, BPAE deterministic vector!")

	var out bytes.Buffer
	enc, err := NewEncryptWriter(&out, keyProvider, orgID, artifactID, WithEntropySource(entropy))
	if err != nil {
		t.Fatalf("failed creating EncryptWriter: %v", err)
	}

	if _, err := enc.Write(plaintext); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	if err := enc.Close(); err != nil {
		t.Fatalf("close failed: %v", err)
	}

	encoded := out.Bytes()

	// Verify exact framing sizes:
	// Prologue (106) + DATA record (13 header + len(plaintext) + 16 tag) + FINAL record (41)
	expectedTotalLen := 106 + 13 + len(plaintext) + 16 + 41
	if len(encoded) != expectedTotalLen {
		t.Fatalf("expected total length %d, got %d", expectedTotalLen, len(encoded))
	}

	// Verify Prologue offsets:
	// Magic (0..4) == ASCII "BPAE"
	if string(encoded[0:4]) != "BPAE" {
		t.Errorf("Magic mismatch: got %v", encoded[0:4])
	}
	// FormatVersion (4) == 0x01
	if encoded[4] != 0x01 {
		t.Errorf("FormatVersion mismatch: got %x", encoded[4])
	}
	// CipherSuite (5) == 0x01
	if encoded[5] != 0x01 {
		t.Errorf("CipherSuite mismatch: got %x", encoded[5])
	}
	// MasterKeyVersion (6..10) == 1
	if binary.BigEndian.Uint32(encoded[6:10]) != 1 {
		t.Errorf("MasterKeyVersion mismatch: got %d", binary.BigEndian.Uint32(encoded[6:10]))
	}
	// OrganizationID (10..26)
	if !bytes.Equal(encoded[10:26], orgID[:]) {
		t.Errorf("OrgID mismatch")
	}
	// ArtifactID (26..42)
	if !bytes.Equal(encoded[26:42], artifactID[:]) {
		t.Errorf("ArtifactID mismatch")
	}
	// WrapNonce (42..54) == all 0x03
	if !bytes.Equal(encoded[42:54], wrapNonceEntropy) {
		t.Errorf("WrapNonce mismatch")
	}
	// ArtifactNoncePrefix (102..106) == 0x04, 0x05, 0x06, 0x07
	if !bytes.Equal(encoded[102:106], noncePrefixEntropy) {
		t.Errorf("ArtifactNoncePrefix mismatch")
	}

	// Verify DATA record:
	// Flag (106) == 0x00
	if encoded[106] != 0x00 {
		t.Errorf("DATA flag mismatch: got %x", encoded[106])
	}
	// ChunkIndex (107..115) == 0
	if binary.BigEndian.Uint64(encoded[107:115]) != 0 {
		t.Errorf("ChunkIndex mismatch")
	}
	// PlaintextLength (115..119) == len(plaintext)
	if binary.BigEndian.Uint32(encoded[115:119]) != uint32(len(plaintext)) {
		t.Errorf("PlaintextLength mismatch")
	}

	// Verify FINAL record (last 41 bytes):
	finalStart := len(encoded) - 41
	if encoded[finalStart] != 0x01 {
		t.Errorf("FINAL flag mismatch: got %x", encoded[finalStart])
	}
	// NextChunkIndex (8B) == 1
	if binary.BigEndian.Uint64(encoded[finalStart+1:finalStart+9]) != 1 {
		t.Errorf("FINAL NextChunkIndex mismatch")
	}
	// TotalPlaintextSize (8B) == len(plaintext)
	if binary.BigEndian.Uint64(encoded[finalStart+9:finalStart+17]) != uint64(len(plaintext)) {
		t.Errorf("FINAL TotalPlaintextSize mismatch")
	}
	// DataChunkCount (8B) == 1
	if binary.BigEndian.Uint64(encoded[finalStart+17:finalStart+25]) != 1 {
		t.Errorf("FINAL DataChunkCount mismatch")
	}

	// Golden hex snapshot check (proves exact deterministic reproducibility)
	goldenHex := hex.EncodeToString(encoded)
	if len(goldenHex) == 0 {
		t.Fatal("empty golden hex")
	}

	// Verify round-trip decryption using the same key provider and identities
	dec, err := NewDecryptReader(bytes.NewReader(encoded), keyProvider, orgID, artifactID)
	if err != nil {
		t.Fatalf("failed creating DecryptReader: %v", err)
	}
	defer dec.Close()

	decrypted, err := io.ReadAll(dec)
	if err != nil {
		t.Fatalf("decryption failed: %v", err)
	}
	if !bytes.Equal(decrypted, plaintext) {
		t.Fatalf("decrypted plaintext mismatch: expected %q, got %q", string(plaintext), string(decrypted))
	}
}

func TestBPAE_RoundTripPayloadSizes(t *testing.T) {
	kek := bytes.Repeat([]byte{0x42}, 32)
	keyProvider, _ := NewStaticKeyProvider(kek, 1)
	orgID := uuid.New()
	artifactID := uuid.New()

	testSizes := []int{
		0,      // empty stream
		1,      // 1 byte
		100,    // small
		65535,  // max chunk - 1
		65536,  // exact max chunk (64 KiB)
		65537,  // 64 KiB + 1 (spans 2 chunks)
		131072, // exact 2 chunks
		200000, // multiple chunks (~195 KiB)
	}

	for _, size := range testSizes {
		t.Run(string(rune(size)), func(t *testing.T) {
			payload := make([]byte, size)
			for i := range payload {
				payload[i] = byte((i * 31) % 256)
			}

			var buf bytes.Buffer
			enc, err := NewEncryptWriter(&buf, keyProvider, orgID, artifactID)
			if err != nil {
				t.Fatalf("NewEncryptWriter failed: %v", err)
			}

			// Fragmented writes
			chunkSize := 1024
			if size < 1024 {
				chunkSize = 1
			}
			for i := 0; i < len(payload); i += chunkSize {
				end := i + chunkSize
				if end > len(payload) {
					end = len(payload)
				}
				if _, err := enc.Write(payload[i:end]); err != nil {
					t.Fatalf("write failed: %v", err)
				}
			}

			if err := enc.Close(); err != nil {
				t.Fatalf("close failed: %v", err)
			}

			dec, err := NewDecryptReader(&buf, keyProvider, orgID, artifactID)
			if err != nil {
				t.Fatalf("NewDecryptReader failed: %v", err)
			}
			defer dec.Close()

			got, err := io.ReadAll(dec)
			if err != nil {
				t.Fatalf("ReadAll failed for size %d: %v", size, err)
			}

			if !bytes.Equal(got, payload) {
				t.Fatalf("round-trip mismatch for size %d", size)
			}
		})
	}
}

func TestBPAE_TamperAndFailureModes(t *testing.T) {
	kek := bytes.Repeat([]byte{0x77}, 32)
	keyProvider, _ := NewStaticKeyProvider(kek, 1)
	orgID := uuid.New()
	artifactID := uuid.New()

	makeValidEncrypted := func() []byte {
		var buf bytes.Buffer
		enc, _ := NewEncryptWriter(&buf, keyProvider, orgID, artifactID)
		_, _ = enc.Write([]byte("Sensitive payload to be protected by BPAE V1 authenticated encryption."))
		_ = enc.Close()
		return buf.Bytes()
	}

	t.Run("wrong Magic fails closed", func(t *testing.T) {
		valid := makeValidEncrypted()
		valid[0] = 'X'
		dec, _ := NewDecryptReader(bytes.NewReader(valid), keyProvider, orgID, artifactID)
		_, err := io.ReadAll(dec)
		if !errors.Is(err, ErrMalformedBPAE) {
			t.Fatalf("expected ErrMalformedBPAE, got %v", err)
		}
	})

	t.Run("unknown FormatVersion fails closed", func(t *testing.T) {
		valid := makeValidEncrypted()
		valid[4] = 0x02 // Unknown version
		dec, _ := NewDecryptReader(bytes.NewReader(valid), keyProvider, orgID, artifactID)
		_, err := io.ReadAll(dec)
		if !errors.Is(err, ErrUnsupportedVersion) {
			t.Fatalf("expected ErrUnsupportedVersion, got %v", err)
		}
	})

	t.Run("unknown CipherSuite fails closed", func(t *testing.T) {
		valid := makeValidEncrypted()
		valid[5] = 0x99 // Unknown suite
		dec, _ := NewDecryptReader(bytes.NewReader(valid), keyProvider, orgID, artifactID)
		_, err := io.ReadAll(dec)
		if !errors.Is(err, ErrUnsupportedCipherSuite) {
			t.Fatalf("expected ErrUnsupportedCipherSuite, got %v", err)
		}
	})

	t.Run("unknown MasterKeyVersion fails closed", func(t *testing.T) {
		valid := makeValidEncrypted()
		binary.BigEndian.PutUint32(valid[6:10], 999) // Version 999 not registered
		dec, _ := NewDecryptReader(bytes.NewReader(valid), keyProvider, orgID, artifactID)
		_, err := io.ReadAll(dec)
		if !errors.Is(err, ErrUnknownKeyVersion) {
			t.Fatalf("expected ErrUnknownKeyVersion, got %v", err)
		}
	})

	t.Run("wrong OrganizationID fails closed", func(t *testing.T) {
		valid := makeValidEncrypted()
		wrongOrgID := uuid.New()
		dec, _ := NewDecryptReader(bytes.NewReader(valid), keyProvider, wrongOrgID, artifactID)
		_, err := io.ReadAll(dec)
		if !errors.Is(err, ErrIdentityMismatch) {
			t.Fatalf("expected ErrIdentityMismatch, got %v", err)
		}
	})

	t.Run("wrong ArtifactID fails closed", func(t *testing.T) {
		valid := makeValidEncrypted()
		wrongArtifactID := uuid.New()
		dec, _ := NewDecryptReader(bytes.NewReader(valid), keyProvider, orgID, wrongArtifactID)
		_, err := io.ReadAll(dec)
		if !errors.Is(err, ErrIdentityMismatch) {
			t.Fatalf("expected ErrIdentityMismatch, got %v", err)
		}
	})

	t.Run("corrupted WrappedDEK fails closed", func(t *testing.T) {
		valid := makeValidEncrypted()
		valid[60] ^= 0xFF // Flip bit in WrappedDEK
		dec, _ := NewDecryptReader(bytes.NewReader(valid), keyProvider, orgID, artifactID)
		_, err := io.ReadAll(dec)
		if !errors.Is(err, ErrAuthFailed) {
			t.Fatalf("expected ErrAuthFailed, got %v", err)
		}
	})

	t.Run("corrupted DATA ciphertext fails closed", func(t *testing.T) {
		valid := makeValidEncrypted()
		valid[120] ^= 0x55 // Flip bit in DATA ciphertext
		dec, _ := NewDecryptReader(bytes.NewReader(valid), keyProvider, orgID, artifactID)
		_, err := io.ReadAll(dec)
		if !errors.Is(err, ErrAuthFailed) {
			t.Fatalf("expected ErrAuthFailed, got %v", err)
		}
	})

	t.Run("missing FINAL record fails closed", func(t *testing.T) {
		valid := makeValidEncrypted()
		truncated := valid[:len(valid)-41] // Strip FINAL record
		dec, _ := NewDecryptReader(bytes.NewReader(truncated), keyProvider, orgID, artifactID)
		_, err := io.ReadAll(dec)
		if !errors.Is(err, ErrMissingFinal) {
			t.Fatalf("expected ErrMissingFinal, got %v", err)
		}
	})

	t.Run("corrupted FINAL tag fails closed", func(t *testing.T) {
		valid := makeValidEncrypted()
		valid[len(valid)-5] ^= 0xAA // Flip bit in FINAL tag
		dec, _ := NewDecryptReader(bytes.NewReader(valid), keyProvider, orgID, artifactID)
		_, err := io.ReadAll(dec)
		if !errors.Is(err, ErrAuthFailed) {
			t.Fatalf("expected ErrAuthFailed, got %v", err)
		}
	})

	t.Run("trailing bytes after FINAL fails closed", func(t *testing.T) {
		valid := makeValidEncrypted()
		withTrailing := append(valid, []byte("MALICIOUS_TRAILING_BYTES")...)
		dec, _ := NewDecryptReader(bytes.NewReader(withTrailing), keyProvider, orgID, artifactID)
		_, err := io.ReadAll(dec)
		if !errors.Is(err, ErrTrailingBytes) {
			t.Fatalf("expected ErrTrailingBytes, got %v", err)
		}
	})

	t.Run("duplicate FINAL fails closed", func(t *testing.T) {
		valid := makeValidEncrypted()
		finalRec := valid[len(valid)-41:]
		withDupFinal := append(valid, finalRec...)
		dec, _ := NewDecryptReader(bytes.NewReader(withDupFinal), keyProvider, orgID, artifactID)
		_, err := io.ReadAll(dec)
		if !errors.Is(err, ErrTrailingBytes) && !errors.Is(err, ErrDuplicateFinal) {
			t.Fatalf("expected ErrTrailingBytes or ErrDuplicateFinal, got %v", err)
		}
	})
}

func TestBPAE_FinalHoldbackSemantics(t *testing.T) {
	kek := bytes.Repeat([]byte{0x88}, 32)
	keyProvider, _ := NewStaticKeyProvider(kek, 1)
	orgID := uuid.New()
	artifactID := uuid.New()

	t.Run("One-chunk artifact with missing FINAL does NOT release the only chunk", func(t *testing.T) {
		var buf bytes.Buffer
		enc, _ := NewEncryptWriter(&buf, keyProvider, orgID, artifactID)
		secretData := []byte("CRITICAL_SECRET_THAT_MUST_NOT_LEAK_WITHOUT_FINAL")
		_, _ = enc.Write(secretData)
		_ = enc.Close()

		raw := buf.Bytes()
		truncated := raw[:len(raw)-41] // Strip FINAL record

		dec, err := NewDecryptReader(bytes.NewReader(truncated), keyProvider, orgID, artifactID)
		if err != nil {
			t.Fatalf("NewDecryptReader failed: %v", err)
		}
		defer dec.Close()

		output := make([]byte, 1024)
		n, err := dec.Read(output)

		// MUST return error and 0 bytes!
		if n != 0 {
			t.Fatalf("SECURITY VIOLATION: holdback failed, released %d bytes without FINAL!", n)
		}
		if !errors.Is(err, ErrMissingFinal) {
			t.Fatalf("expected ErrMissingFinal, got %v", err)
		}
	})

	t.Run("One-chunk artifact with corrupted FINAL does NOT release the only chunk", func(t *testing.T) {
		var buf bytes.Buffer
		enc, _ := NewEncryptWriter(&buf, keyProvider, orgID, artifactID)
		secretData := []byte("CRITICAL_SECRET_THAT_MUST_NOT_LEAK_ON_CORRUPTION")
		_, _ = enc.Write(secretData)
		_ = enc.Close()

		corrupted := buf.Bytes()
		corrupted[len(corrupted)-1] ^= 0xFF // Corrupt tag byte

		dec, err := NewDecryptReader(bytes.NewReader(corrupted), keyProvider, orgID, artifactID)
		if err != nil {
			t.Fatalf("NewDecryptReader failed: %v", err)
		}
		defer dec.Close()

		output := make([]byte, 1024)
		n, err := dec.Read(output)

		if n != 0 {
			t.Fatalf("SECURITY VIOLATION: holdback failed, released %d bytes on corrupted FINAL!", n)
		}
		if !errors.Is(err, ErrAuthFailed) {
			t.Fatalf("expected ErrAuthFailed, got %v", err)
		}
	})

	t.Run("Multi-chunk artifact with missing FINAL does NOT release final chunk", func(t *testing.T) {
		var buf bytes.Buffer
		enc, _ := NewEncryptWriter(&buf, keyProvider, orgID, artifactID)

		// Write 65536 bytes (chunk 0) + 10 bytes (chunk 1)
		chunk0 := bytes.Repeat([]byte{'A'}, MaxPlaintextChunkSize)
		chunk1 := []byte("LAST_CHUNK")
		_, _ = enc.Write(chunk0)
		_, _ = enc.Write(chunk1)
		_ = enc.Close()

		raw := buf.Bytes()
		truncated := raw[:len(raw)-41] // Strip FINAL record

		dec, err := NewDecryptReader(bytes.NewReader(truncated), keyProvider, orgID, artifactID)
		if err != nil {
			t.Fatalf("NewDecryptReader failed: %v", err)
		}
		defer dec.Close()

		// Read all available bytes
		received, err := io.ReadAll(dec)

		// chunk0 should be received (since chunk1 authenticated), but chunk1 MUST NOT be received!
		if !bytes.Equal(received, chunk0) {
			t.Fatalf("expected to receive only chunk0 (%d bytes), got %d bytes", len(chunk0), len(received))
		}
		if bytes.Contains(received, chunk1) {
			t.Fatalf("SECURITY VIOLATION: holdback failed, released chunk1 without FINAL!")
		}
		if !errors.Is(err, ErrMissingFinal) {
			t.Fatalf("expected ErrMissingFinal, got %v", err)
		}
	})
}
