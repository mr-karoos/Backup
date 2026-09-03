package artifactcrypto

import (
	"encoding/binary"
	"errors"

	"backup-platform/pkg/uuid"
)

// Frozen BPAE V1 Constants
const (
	MagicString            = "BPAE"
	FormatVersionV1   byte = 0x01
	CipherSuiteAESGCM byte = 0x01
	FlagDataRecord    byte = 0x00
	FlagFinalRecord   byte = 0x01

	KeySize      = 32 // 256-bit AES
	GCMNonceSize = 12 // Standard 96-bit AES-GCM nonce
	GCMTagSize   = 16 // Standard 128-bit AES-GCM tag

	HeaderAADSize           = 42  // Magic(4) + Ver(1) + Suite(1) + KeyVer(4) + OrgID(16) + ArtID(16)
	WrapNonceSize           = 12  // Random 12-byte wrap nonce
	WrappedDEKSize          = 48  // 32-byte DEK + 16-byte GCM tag
	WrappedKeyHeaderSize    = 102 // HeaderAAD(42) + WrapNonce(12) + WrappedDEK(48)
	ArtifactNoncePrefixSize = 4   // Random 4-byte prefix
	PrologueSize            = 106 // WrappedKeyHeader(102) + ArtifactNoncePrefix(4)

	DataAADSize  = 46 // Ver(1) + OrgID(16) + ArtID(16) + Flag(1) + ChunkIndex(8) + PlaintextLength(4)
	FinalAADSize = 58 // Ver(1) + OrgID(16) + ArtID(16) + Flag(1) + NextChunk(8) + TotalSize(8) + ChunkCount(8)

	DataRecordHeaderSize = 13 // Flag(1) + ChunkIndex(8) + PlaintextLength(4)
	FinalRecordSize      = 41 // Flag(1) + NextChunk(8) + TotalSize(8) + ChunkCount(8) + GCMTag(16)

	MaxPlaintextChunkSize = 65536 // 64 KiB
)

var (
	MagicBytes = [4]byte{0x42, 0x50, 0x41, 0x45} // ASCII "BPAE"
)

// Standard Typed Errors (Safe for caller classification without leaking secret material)
var (
	ErrMalformedBPAE          = errors.New("bpae: malformed stream or unexpected header format")
	ErrUnsupportedVersion     = errors.New("bpae: unsupported format version")
	ErrUnsupportedCipherSuite = errors.New("bpae: unsupported cipher suite")
	ErrAuthFailed             = errors.New("bpae: cryptographic authentication failed (tampering or corrupted data)")
	ErrMissingFinal           = errors.New("bpae: truncated stream or missing mandatory FINAL record")
	ErrCorruptedFinal         = errors.New("bpae: invalid FINAL record counter or authentication tag")
	ErrTrailingBytes          = errors.New("bpae: unexpected trailing bytes after authenticated FINAL record")
	ErrIdentityMismatch       = errors.New("bpae: organization or artifact identity mismatch")
	ErrInvalidPlaintextLength = errors.New("bpae: invalid plaintext chunk length")
	ErrOutOfOrderChunk        = errors.New("bpae: out-of-order, skipped, or duplicate chunk index")
	ErrDuplicateFinal         = errors.New("bpae: duplicate FINAL record detected")
)

// BuildHeaderAAD constructs the exact 42-byte Header AAD:
// Magic (4B) || FormatVersion (1B) || CipherSuite (1B) || MasterKeyVersion (4B BE) || OrganizationID (16B) || ArtifactID (16B)
func BuildHeaderAAD(masterKeyVersion uint32, orgID, artifactID uuid.UUID) [HeaderAADSize]byte {
	var aad [HeaderAADSize]byte
	copy(aad[0:4], MagicBytes[:])
	aad[4] = FormatVersionV1
	aad[5] = CipherSuiteAESGCM
	binary.BigEndian.PutUint32(aad[6:10], masterKeyVersion)
	copy(aad[10:26], orgID[:])
	copy(aad[26:42], artifactID[:])
	return aad
}

// BuildDataAAD constructs the exact 46-byte DATA record AAD:
// FormatVersion (1B) || OrganizationID (16B) || ArtifactID (16B) || Flags (1B = 0x00) || ChunkIndex (8B BE) || PlaintextLength (4B BE)
func BuildDataAAD(orgID, artifactID uuid.UUID, chunkIndex uint64, plainLength uint32) [DataAADSize]byte {
	var aad [DataAADSize]byte
	aad[0] = FormatVersionV1
	copy(aad[1:17], orgID[:])
	copy(aad[17:33], artifactID[:])
	aad[33] = FlagDataRecord
	binary.BigEndian.PutUint64(aad[34:42], chunkIndex)
	binary.BigEndian.PutUint32(aad[42:46], plainLength)
	return aad
}

// BuildFinalAAD constructs the exact 58-byte FINAL record AAD:
// FormatVersion (1B) || OrganizationID (16B) || ArtifactID (16B) || Flags (1B = 0x01) || NextChunkIndex (8B BE) || TotalPlaintextSize (8B BE) || DataChunkCount (8B BE)
func BuildFinalAAD(orgID, artifactID uuid.UUID, nextChunkIndex, totalPlaintextSize, dataChunkCount uint64) [FinalAADSize]byte {
	var aad [FinalAADSize]byte
	aad[0] = FormatVersionV1
	copy(aad[1:17], orgID[:])
	copy(aad[17:33], artifactID[:])
	aad[33] = FlagFinalRecord
	binary.BigEndian.PutUint64(aad[34:42], nextChunkIndex)
	binary.BigEndian.PutUint64(aad[42:50], totalPlaintextSize)
	binary.BigEndian.PutUint64(aad[50:58], dataChunkCount)
	return aad
}

// BuildRecordNonce constructs the exact 12-byte nonce:
// ArtifactNoncePrefix (4B) || SequenceNumber (8B BE)
func BuildRecordNonce(prefix [ArtifactNoncePrefixSize]byte, seq uint64) [GCMNonceSize]byte {
	var nonce [GCMNonceSize]byte
	copy(nonce[0:4], prefix[:])
	binary.BigEndian.PutUint64(nonce[4:12], seq)
	return nonce
}
