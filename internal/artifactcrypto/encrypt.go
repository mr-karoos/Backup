package artifactcrypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"backup-platform/pkg/uuid"
)

// EncryptWriter wraps an io.Writer and produces a valid BPAE V1 encrypted stream.
type EncryptWriter struct {
	dst         io.Writer
	keyProvider KeyProvider
	orgID       uuid.UUID
	artifactID  uuid.UUID
	entropy     io.Reader

	dekGCM              cipher.AEAD
	dek                 [KeySize]byte
	artifactNoncePrefix [ArtifactNoncePrefixSize]byte

	headerWritten bool
	closed        bool

	chunkIndex         uint64
	totalPlaintextSize uint64

	buf       []byte
	bufOffset int
}

// EncryptWriterOption allows overriding defaults (e.g. entropy source for tests).
type EncryptWriterOption func(*EncryptWriter)

// WithEntropySource configures an explicit entropy reader (used for deterministic golden tests).
func WithEntropySource(r io.Reader) EncryptWriterOption {
	return func(w *EncryptWriter) {
		w.entropy = r
	}
}

// NewEncryptWriter creates a new streaming BPAE V1 EncryptWriter.
func NewEncryptWriter(
	dst io.Writer,
	keyProvider KeyProvider,
	orgID, artifactID uuid.UUID,
	opts ...EncryptWriterOption,
) (*EncryptWriter, error) {
	if dst == nil {
		return nil, errors.New("bpae: destination writer cannot be nil")
	}
	if keyProvider == nil {
		return nil, errors.New("bpae: key provider cannot be nil")
	}
	if orgID == uuid.Nil || artifactID == uuid.Nil {
		return nil, errors.New("bpae: valid organization ID and artifact ID are required")
	}

	w := &EncryptWriter{
		dst:         dst,
		keyProvider: keyProvider,
		orgID:       orgID,
		artifactID:  artifactID,
		entropy:     rand.Reader,
		buf:         make([]byte, MaxPlaintextChunkSize),
	}

	for _, opt := range opts {
		opt(w)
	}

	return w, nil
}

// initPrologue generates keys and emits the exact 106-byte fixed prologue.
func (w *EncryptWriter) initPrologue() error {
	if w.headerWritten {
		return nil
	}

	kek, version, err := w.keyProvider.Current()
	if err != nil {
		return fmt.Errorf("bpae: failed acquiring current master key: %w", err)
	}
	defer ZeroBytes(kek)

	if len(kek) != KeySize {
		return ErrInvalidKeyLength
	}
	if version < 1 {
		return ErrInvalidKeyVersion
	}

	// 1. Generate DEK (32 random bytes)
	if _, err := io.ReadFull(w.entropy, w.dek[:]); err != nil {
		return fmt.Errorf("bpae: failed generating random DEK: %w", err)
	}

	// 2. Generate WrapNonce (12 random bytes)
	var wrapNonce [WrapNonceSize]byte
	if _, err := io.ReadFull(w.entropy, wrapNonce[:]); err != nil {
		return fmt.Errorf("bpae: failed generating random wrap nonce: %w", err)
	}

	// 3. Generate ArtifactNoncePrefix (4 random bytes)
	if _, err := io.ReadFull(w.entropy, w.artifactNoncePrefix[:]); err != nil {
		return fmt.Errorf("bpae: failed generating random artifact nonce prefix: %w", err)
	}

	// 4. Build 42-byte Header AAD
	headerAAD := BuildHeaderAAD(uint32(version), w.orgID, w.artifactID)

	// 5. Wrap DEK using KEK
	kekBlock, err := aes.NewCipher(kek)
	if err != nil {
		return fmt.Errorf("bpae: failed creating KEK cipher: %w", err)
	}
	kekGCM, err := cipher.NewGCM(kekBlock)
	if err != nil {
		return fmt.Errorf("bpae: failed creating KEK GCM: %w", err)
	}

	wrappedDEK := kekGCM.Seal(nil, wrapNonce[:], w.dek[:], headerAAD[:])
	if len(wrappedDEK) != WrappedDEKSize {
		return fmt.Errorf("bpae: unexpected wrapped DEK size: %d", len(wrappedDEK))
	}

	// 6. Serialize exact 106-byte Fixed Prologue
	var prologue [PrologueSize]byte
	copy(prologue[0:HeaderAADSize], headerAAD[:])
	copy(prologue[HeaderAADSize:HeaderAADSize+WrapNonceSize], wrapNonce[:])
	copy(prologue[HeaderAADSize+WrapNonceSize:WrappedKeyHeaderSize], wrappedDEK)
	copy(prologue[WrappedKeyHeaderSize:PrologueSize], w.artifactNoncePrefix[:])

	if _, err := w.dst.Write(prologue[:]); err != nil {
		return fmt.Errorf("bpae: failed writing fixed prologue: %w", err)
	}

	// 7. Initialize DEK cipher for subsequent records
	dekBlock, err := aes.NewCipher(w.dek[:])
	if err != nil {
		return fmt.Errorf("bpae: failed creating DEK cipher: %w", err)
	}
	dekGCM, err := cipher.NewGCM(dekBlock)
	if err != nil {
		return fmt.Errorf("bpae: failed creating DEK GCM: %w", err)
	}
	w.dekGCM = dekGCM

	w.headerWritten = true
	return nil
}

// Write buffers incoming plaintext bytes and emits DATA records of at most 64 KiB.
func (w *EncryptWriter) Write(p []byte) (int, error) {
	if w.closed {
		return 0, errors.New("bpae: write to closed EncryptWriter")
	}

	if !w.headerWritten {
		if err := w.initPrologue(); err != nil {
			return 0, err
		}
	}

	totalWritten := 0
	for len(p) > 0 {
		available := MaxPlaintextChunkSize - w.bufOffset
		toCopy := len(p)
		if toCopy > available {
			toCopy = available
		}

		copy(w.buf[w.bufOffset:], p[:toCopy])
		w.bufOffset += toCopy
		p = p[toCopy:]
		totalWritten += toCopy

		if w.bufOffset == MaxPlaintextChunkSize {
			if err := w.flushDataChunk(); err != nil {
				return totalWritten, err
			}
		}
	}

	return totalWritten, nil
}

// flushDataChunk encrypts and writes the currently buffered plaintext chunk.
func (w *EncryptWriter) flushDataChunk() error {
	if w.bufOffset == 0 {
		return nil
	}

	plainChunk := w.buf[:w.bufOffset]
	chunkLen := uint32(w.bufOffset)

	dataAAD := BuildDataAAD(w.orgID, w.artifactID, w.chunkIndex, chunkLen)
	recordNonce := BuildRecordNonce(w.artifactNoncePrefix, w.chunkIndex)

	ciphertext := w.dekGCM.Seal(nil, recordNonce[:], plainChunk, dataAAD[:])

	// Serialize DATA Record Header: Flags (1B) || ChunkIndex (8B BE) || PlaintextLength (4B BE)
	var recHeader [DataRecordHeaderSize]byte
	recHeader[0] = FlagDataRecord
	binary.BigEndian.PutUint64(recHeader[1:9], w.chunkIndex)
	binary.BigEndian.PutUint32(recHeader[9:13], chunkLen)

	if _, err := w.dst.Write(recHeader[:]); err != nil {
		return fmt.Errorf("bpae: failed writing DATA record header: %w", err)
	}
	if _, err := w.dst.Write(ciphertext); err != nil {
		return fmt.Errorf("bpae: failed writing DATA record payload: %w", err)
	}

	w.chunkIndex++
	w.totalPlaintextSize += uint64(chunkLen)
	w.bufOffset = 0
	return nil
}

// Close flushes any pending plaintext chunk, emits the mandatory authenticated FINAL record, and zeroizes keys.
func (w *EncryptWriter) Close() error {
	if w.closed {
		return nil
	}
	w.closed = true

	defer func() {
		ZeroBytes(w.dek[:])
		ZeroBytes(w.buf)
		w.dekGCM = nil
	}()

	if !w.headerWritten {
		if err := w.initPrologue(); err != nil {
			return err
		}
	}

	// Flush remaining partial chunk (if any)
	if w.bufOffset > 0 {
		if err := w.flushDataChunk(); err != nil {
			return err
		}
	}

	// Emit mandatory FINAL record
	nextChunkIndex := w.chunkIndex
	dataChunkCount := w.chunkIndex

	finalAAD := BuildFinalAAD(w.orgID, w.artifactID, nextChunkIndex, w.totalPlaintextSize, dataChunkCount)
	finalNonce := BuildRecordNonce(w.artifactNoncePrefix, nextChunkIndex)

	finalTag := w.dekGCM.Seal(nil, finalNonce[:], nil, finalAAD[:])
	if len(finalTag) != GCMTagSize {
		return fmt.Errorf("bpae: unexpected final tag size: %d", len(finalTag))
	}

	// Serialize FINAL record:
	// Flags (1B) || NextChunkIndex (8B BE) || TotalPlaintextSize (8B BE) || DataChunkCount (8B BE) || GCMTag (16B)
	var finalRec [FinalRecordSize]byte
	finalRec[0] = FlagFinalRecord
	binary.BigEndian.PutUint64(finalRec[1:9], nextChunkIndex)
	binary.BigEndian.PutUint64(finalRec[9:17], w.totalPlaintextSize)
	binary.BigEndian.PutUint64(finalRec[17:25], dataChunkCount)
	copy(finalRec[25:41], finalTag)

	if _, err := w.dst.Write(finalRec[:]); err != nil {
		return fmt.Errorf("bpae: failed writing FINAL record: %w", err)
	}

	return nil
}

// TotalPlaintextSize returns the total plaintext bytes encrypted so far.
func (w *EncryptWriter) TotalPlaintextSize() uint64 {
	return w.totalPlaintextSize + uint64(w.bufOffset)
}
