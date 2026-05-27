package ports

import (
	"errors"
	"fmt"
	"io"
)

// EncryptWriter abstracts *internal/adapters/crypto.ChunkEncryptWriter (current concrete implementation).
//
// Implementations consume bytes via io.Writer and emit Sentinel's chunked
// AES-256-GCM envelope. BaseNonce returns the IV stored in the backup
// manifest; LastAuthTag returns the per-stream tag captured during the
// final chunk flush (used for envelope verification at restore time).
type EncryptWriter interface {
	io.Writer
	Flush() error
	BaseNonce() []byte
	LastAuthTag() []byte
}

// DecryptReader abstracts *internal/adapters/crypto.ChunkDecryptReader (current concrete implementation).
type DecryptReader interface {
	io.Reader
}

// DecryptOptions controls how the decrypt path handles envelope versioning.
//
// Relocated to internal/ports (single source of truth per spec 028 FR-003a);
// concrete adapter lives in internal/adapters/crypto/ (spec 030).
type DecryptOptions struct {
	// AllowLegacy permits decrypting pre-v2 (unversioned) artifacts. Off by default.
	AllowLegacy bool
	// Source is a human-readable path/URI identifying the artifact (for log lines).
	Source string
	// BackupID is the AAD used during encryption.
	BackupID string
}

// Encryption envelope sentinels — single source of truth per spec 028
// FR-003a. Callers MUST import these from this package; aliasing back into
// internal/adapters/crypto/ is forbidden.

// ErrShortNonce indicates the AEAD's nonce size is too small to host the 8-byte counter region.
var ErrShortNonce = errors.New("crypto: AEAD nonce shorter than 8-byte counter region")

// ErrChunkCounterOverflow indicates the per-stream chunk counter would overflow uint64.
var ErrChunkCounterOverflow = errors.New("crypto: chunk counter would overflow uint64 — rotate the encryption key and re-encrypt from source")

// ErrLegacyEnvelope indicates a stream lacks the v2 magic header and the caller did not opt in.
var ErrLegacyEnvelope = errors.New("crypto: legacy (pre-v2) envelope detected — re-encrypt from source, or pass --allow-legacy-envelope to proceed at your own risk")

// ErrChunkTooLarge indicates a chunk-length prefix is outside the legal range [1, maxChunkSize].
var ErrChunkTooLarge = errors.New("crypto: chunk length out of bounds")

// ErrAuthTagFailed indicates AES-GCM authentication-tag verification failed for a chunk (wrong key, tampered ciphertext, or tampered tag).
var ErrAuthTagFailed = errors.New("crypto: authentication tag verification failed")

// ErrUnsupportedEnvelopeVersion indicates the stream header carries an unknown version byte.
type ErrUnsupportedEnvelopeVersion struct {
	Version uint8
}

func (e ErrUnsupportedEnvelopeVersion) Error() string {
	return fmt.Sprintf("crypto: unsupported envelope version 0x%02X — upgrade Sentinel", e.Version)
}
