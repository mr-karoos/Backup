package artifactcrypto

import (
	"bufio"
	"crypto/aes"
	"crypto/cipher"
	"crypto/subtle"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"backup-platform/pkg/uuid"
)

// DecryptReader implements an authenticated streaming BPAE V1 reader with 1-chunk holdback.
type DecryptReader struct {
	bufReader          *bufio.Reader
	closer             io.Closer
	keyProvider        KeyProvider
	expectedOrgID      uuid.UUID
	expectedArtifactID uuid.UUID

	dekGCM              cipher.AEAD
	dek                 [KeySize]byte
	artifactNoncePrefix [ArtifactNoncePrefixSize]byte

	prologueParsed bool
	finalValidated bool
	closed         bool

	nextChunkIndex     uint64
	totalPlaintextSize uint64

	// Holdback buffers
	activeChunk  []byte
	activeOffset int
	heldChunk    []byte
}

// NewDecryptReader constructs a DecryptReader enforcing identity binding and 1-chunk holdback.
func NewDecryptReader(
	src io.Reader,
	keyProvider KeyProvider,
	expectedOrgID, expectedArtifactID uuid.UUID,
) (*DecryptReader, error) {
	if src == nil {
		return nil, errors.New("bpae: source reader cannot be nil")
	}
	if keyProvider == nil {
		return nil, errors.New("bpae: key provider cannot be nil")
	}
	if expectedOrgID == uuid.Nil || expectedArtifactID == uuid.Nil {
		return nil, errors.New("bpae: valid expected organization ID and artifact ID are required")
	}

	var closer io.Closer
	if c, ok := src.(io.Closer); ok {
		closer = c
	}
	bufReader := bufio.NewReaderSize(src, 32*1024)

	return &DecryptReader{
		bufReader:          bufReader,
		closer:             closer,
		keyProvider:        keyProvider,
		expectedOrgID:      expectedOrgID,
		expectedArtifactID: expectedArtifactID,
	}, nil
}

// ParsePrologue reads and validates the exact 106-byte fixed prologue immediately if not already parsed.
func (r *DecryptReader) ParsePrologue() error {
	return r.parsePrologue()
}

// parsePrologue reads and validates the exact 106-byte fixed prologue.
func (r *DecryptReader) parsePrologue() error {
	if r.prologueParsed {
		return nil
	}

	var prologue [PrologueSize]byte
	if _, err := io.ReadFull(r.bufReader, prologue[:]); err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return ErrMalformedBPAE
		}
		return fmt.Errorf("bpae: failed reading fixed prologue: %w", err)
	}

	// 1. Verify Magic: ASCII "BPAE"
	if subtle.ConstantTimeCompare(prologue[0:4], MagicBytes[:]) != 1 {
		return ErrMalformedBPAE
	}

	// 2. Verify FormatVersion: 0x01
	if prologue[4] != FormatVersionV1 {
		return ErrUnsupportedVersion
	}

	// 3. Verify CipherSuite: 0x01 (AES-256-GCM)
	if prologue[5] != CipherSuiteAESGCM {
		return ErrUnsupportedCipherSuite
	}

	// 4. Extract MasterKeyVersion (4B BE uint32)
	masterKeyVersion := binary.BigEndian.Uint32(prologue[6:10])
	if masterKeyVersion < 1 {
		return ErrInvalidKeyVersion
	}

	// 5. Verify OrganizationID and ArtifactID
	var embeddedOrgID, embeddedArtifactID uuid.UUID
	copy(embeddedOrgID[:], prologue[10:26])
	copy(embeddedArtifactID[:], prologue[26:42])

	if embeddedOrgID != r.expectedOrgID || embeddedArtifactID != r.expectedArtifactID {
		return ErrIdentityMismatch
	}

	// 6. Extract WrapNonce, WrappedDEK, and ArtifactNoncePrefix
	var wrapNonce [WrapNonceSize]byte
	copy(wrapNonce[:], prologue[HeaderAADSize:HeaderAADSize+WrapNonceSize])

	wrappedDEK := prologue[HeaderAADSize+WrapNonceSize : WrappedKeyHeaderSize]
	copy(r.artifactNoncePrefix[:], prologue[WrappedKeyHeaderSize:PrologueSize])

	// 7. Resolve Master Key (KEK) by Version
	kek, err := r.keyProvider.ByVersion(int(masterKeyVersion))
	if err != nil {
		return fmt.Errorf("%w: version %d", ErrUnknownKeyVersion, masterKeyVersion)
	}
	defer ZeroBytes(kek)

	if len(kek) != KeySize {
		return ErrInvalidKeyLength
	}

	// 8. Unwrap DEK using KEK and Header AAD (first 42 bytes of prologue)
	kekBlock, err := aes.NewCipher(kek)
	if err != nil {
		return fmt.Errorf("bpae: failed initializing KEK cipher: %w", err)
	}
	kekGCM, err := cipher.NewGCM(kekBlock)
	if err != nil {
		return fmt.Errorf("bpae: failed initializing KEK GCM: %w", err)
	}

	headerAAD := prologue[0:HeaderAADSize]
	unwrappedDEK, err := kekGCM.Open(nil, wrapNonce[:], wrappedDEK, headerAAD)
	if err != nil {
		return ErrAuthFailed
	}
	if len(unwrappedDEK) != KeySize {
		ZeroBytes(unwrappedDEK)
		return ErrAuthFailed
	}

	copy(r.dek[:], unwrappedDEK)
	ZeroBytes(unwrappedDEK)

	// 9. Initialize DEK GCM cipher for subsequent records
	dekBlock, err := aes.NewCipher(r.dek[:])
	if err != nil {
		return fmt.Errorf("bpae: failed initializing DEK cipher: %w", err)
	}
	dekGCM, err := cipher.NewGCM(dekBlock)
	if err != nil {
		return fmt.Errorf("bpae: failed initializing DEK GCM: %w", err)
	}
	r.dekGCM = dekGCM

	r.prologueParsed = true
	return nil
}

// fetchNextRecord reads and authenticates the next record from the underlying stream.
// Returns (isFinal bool, plaintext []byte, err error).
func (r *DecryptReader) fetchNextRecord() (bool, []byte, error) {
	var flagBuf [1]byte
	if _, err := io.ReadFull(r.bufReader, flagBuf[:]); err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return false, nil, ErrMissingFinal
		}
		return false, nil, fmt.Errorf("bpae: failed reading record flag: %w", err)
	}

	flag := flagBuf[0]
	switch flag {
	case FlagDataRecord:
		if r.finalValidated {
			return false, nil, ErrTrailingBytes
		}

		// Read remainder of DATA header: ChunkIndex (8B) + PlaintextLength (4B)
		var dataHeader [12]byte
		if _, err := io.ReadFull(r.bufReader, dataHeader[:]); err != nil {
			return false, nil, ErrMalformedBPAE
		}

		chunkIndex := binary.BigEndian.Uint64(dataHeader[0:8])
		plainLength := binary.BigEndian.Uint32(dataHeader[8:12])

		// Reject out-of-order, duplicate, or skipped indexes
		if chunkIndex != r.nextChunkIndex {
			return false, nil, ErrOutOfOrderChunk
		}

		// Reject invalid plaintext length BEFORE allocating
		if plainLength == 0 || plainLength > MaxPlaintextChunkSize {
			return false, nil, ErrInvalidPlaintextLength
		}

		// Read ciphertext + GCM tag
		ciphertextLen := int(plainLength) + GCMTagSize
		buf := make([]byte, ciphertextLen)
		if _, err := io.ReadFull(r.bufReader, buf); err != nil {
			return false, nil, ErrMalformedBPAE
		}

		// Authenticate and decrypt
		dataAAD := BuildDataAAD(r.expectedOrgID, r.expectedArtifactID, chunkIndex, plainLength)
		recordNonce := BuildRecordNonce(r.artifactNoncePrefix, chunkIndex)

		plaintext, err := r.dekGCM.Open(nil, recordNonce[:], buf, dataAAD[:])
		if err != nil {
			return false, nil, ErrAuthFailed
		}

		r.nextChunkIndex++
		r.totalPlaintextSize += uint64(plainLength)
		return false, plaintext, nil

	case FlagFinalRecord:
		if r.finalValidated {
			return true, nil, ErrDuplicateFinal
		}

		// Read remainder of FINAL record:
		// NextChunkIndex (8B) + TotalPlaintextSize (8B) + DataChunkCount (8B) + GCMTag (16B) = 40 bytes
		var finalBuf [40]byte
		if _, err := io.ReadFull(r.bufReader, finalBuf[:]); err != nil {
			return true, nil, ErrMalformedBPAE
		}

		nextChunkIndex := binary.BigEndian.Uint64(finalBuf[0:8])
		totalPlaintextSize := binary.BigEndian.Uint64(finalBuf[8:16])
		dataChunkCount := binary.BigEndian.Uint64(finalBuf[16:24])
		finalTag := finalBuf[24:40]

		// Counter and size verification
		if nextChunkIndex != dataChunkCount || nextChunkIndex != r.nextChunkIndex {
			return true, nil, ErrCorruptedFinal
		}
		if totalPlaintextSize != r.totalPlaintextSize {
			return true, nil, ErrCorruptedFinal
		}

		// Authenticate FINAL record tag
		finalAAD := BuildFinalAAD(r.expectedOrgID, r.expectedArtifactID, nextChunkIndex, totalPlaintextSize, dataChunkCount)
		finalNonce := BuildRecordNonce(r.artifactNoncePrefix, nextChunkIndex)

		if _, err := r.dekGCM.Open(nil, finalNonce[:], finalTag, finalAAD[:]); err != nil {
			return true, nil, ErrAuthFailed
		}

		// Assert NO trailing bytes exist in the underlying stream using Peek(1).
		// Peek returns data (err == nil): ErrTrailingBytes
		// Peek returns io.EOF: stream ended cleanly, FINAL accepted
		// Any other error: propagate safe I/O error
		// (0, nil) is never accepted as proof of EOF.
		_, peekErr := r.bufReader.Peek(1)
		if peekErr == nil {
			return true, nil, ErrTrailingBytes
		}
		if !errors.Is(peekErr, io.EOF) {
			return true, nil, fmt.Errorf("bpae: error checking trailing stream: %w", peekErr)
		}

		r.finalValidated = true
		return true, nil, nil

	default:
		return false, nil, ErrMalformedBPAE
	}
}

// Read implements fail-closed streaming decryption with 1-chunk holdback.
func (r *DecryptReader) Read(p []byte) (int, error) {
	if r.closed {
		return 0, errors.New("bpae: read from closed DecryptReader")
	}

	if !r.prologueParsed {
		if err := r.parsePrologue(); err != nil {
			return 0, err
		}
	}

	// 1. If active chunk has remaining bytes, serve them first
	if r.activeOffset < len(r.activeChunk) {
		n := copy(p, r.activeChunk[r.activeOffset:])
		r.activeOffset += n
		return n, nil
	}

	// Active chunk is exhausted.
	r.activeChunk = nil
	r.activeOffset = 0

	// 2. If FINAL has already been validated and no held chunk remains, we are at EOF.
	if r.finalValidated && r.heldChunk == nil {
		return 0, io.EOF
	}

	// 3. We must advance the stream to maintain holdback.
	for {
		// If we do not currently have a held chunk and FINAL is not yet seen, fetch one.
		if r.heldChunk == nil && !r.finalValidated {
			isFinal, chunk, err := r.fetchNextRecord()
			if err != nil {
				return 0, err
			}
			if isFinal {
				// Empty stream case (0 DATA records). FINAL validated immediately.
				return 0, io.EOF
			}
			r.heldChunk = chunk
		}

		// At this point we have a heldChunk.
		// Peek/fetch the subsequent record:
		isFinal, nextChunk, err := r.fetchNextRecord()
		if err != nil {
			// If fetching the subsequent record fails (corrupted, missing FINAL, auth failed),
			// heldChunk is NEVER released to the caller!
			return 0, err
		}

		if isFinal {
			// FINAL is now authenticated! The heldChunk is the final chunk and is now SAFE to release.
			r.activeChunk = r.heldChunk
			r.heldChunk = nil
			r.activeOffset = 0

			n := copy(p, r.activeChunk[r.activeOffset:])
			r.activeOffset += n
			return n, nil
		}

		// Another DATA record authenticated!
		// The previous heldChunk is now safe to promote to activeChunk.
		// nextChunk becomes the new heldChunk.
		r.activeChunk = r.heldChunk
		r.heldChunk = nextChunk
		r.activeOffset = 0

		n := copy(p, r.activeChunk[r.activeOffset:])
		r.activeOffset += n
		return n, nil
	}
}

// Close closes the underlying reader and zeroizes all sensitive key material.
func (r *DecryptReader) Close() error {
	if r.closed {
		return nil
	}
	r.closed = true

	defer func() {
		ZeroBytes(r.dek[:])
		ZeroBytes(r.activeChunk)
		ZeroBytes(r.heldChunk)
		r.dekGCM = nil
	}()

	if r.closer != nil {
		return r.closer.Close()
	}
	return nil
}
