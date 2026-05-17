// Package crypto implements Sentinel's streaming AES-256-GCM encryption envelope.
//
// On-disk format (envelope v2): a fixed 5-byte header `"SENC" || 0x02` precedes
// the existing v1 chunk stream of `[uint32-le length][ciphertext+16B tag]` records.
// Each chunk's nonce is derived by XOR-ing the base nonce with a per-chunk uint64
// counter into the trailing 8 bytes. Per-key safe stream length is bounded by the
// uint64 counter (2^64 chunks ≈ 2^80 bytes at 64 KB chunks — operationally
// unreachable). The writer errors on counter overflow rather than wrapping. See
// ADR docs/adr/0006-encryption-envelope-v1.md for the governing contract.
package crypto

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
	"math"
)

const chunkSize = 64 * 1024 // 64KB chunks

// ChunkEncryptWriter wraps an io.Writer and encrypts data using AES-256-GCM
// in 64KB chunks. Each chunk is prefixed with a 4-byte little-endian length.
//
// The nonce for each chunk is derived by XOR-ing the base nonce with the
// chunk index (counter mode), making each chunk's nonce unique.
//
// The backup ID is used as Additional Authenticated Data (AAD), binding
// the ciphertext to the specific backup record.
type ChunkEncryptWriter struct {
	w             io.Writer
	gcm           cipher.AEAD
	baseNonce     []byte
	aad           []byte
	chunkIdx      uint64
	buf           []byte
	lastTag       []byte
	headerWritten bool
}

// NewChunkEncryptWriter returns a ChunkEncryptWriter that encrypts data to w.
// key must be 32 bytes. backupID is used as AAD.
func NewChunkEncryptWriter(w io.Writer, key []byte, backupID string) (*ChunkEncryptWriter, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("crypto: failed to create AES cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("crypto: failed to create GCM: %w", err)
	}

	if gcm.NonceSize() < 8 {
		return nil, fmt.Errorf("crypto: AEAD nonce size %d insufficient: %w", gcm.NonceSize(), ErrShortNonce)
	}

	baseNonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(baseNonce); err != nil {
		return nil, fmt.Errorf("crypto: failed to generate base nonce: %w", err)
	}

	return &ChunkEncryptWriter{
		w:         w,
		gcm:       gcm,
		baseNonce: baseNonce,
		aad:       []byte(backupID),
		buf:       make([]byte, 0, chunkSize),
	}, nil
}

// BaseNonce returns the base nonce (IV) used for this stream.
// Store this in the backup manifest for decryption.
func (e *ChunkEncryptWriter) BaseNonce() []byte {
	return e.baseNonce
}

// LastAuthTag returns the authentication tag of the last encrypted chunk.
func (e *ChunkEncryptWriter) LastAuthTag() []byte {
	return e.lastTag
}

// Write buffers plaintext data and encrypts full 64KB chunks.
func (e *ChunkEncryptWriter) Write(p []byte) (int, error) {
	total := len(p)
	for len(p) > 0 {
		space := chunkSize - len(e.buf)
		if len(p) < space {
			e.buf = append(e.buf, p...)
			p = nil
		} else {
			e.buf = append(e.buf, p[:space]...)
			p = p[space:]
			if err := e.flushChunk(); err != nil {
				return 0, err
			}
		}
	}
	return total, nil
}

// Flush encrypts and writes any remaining buffered data. The v2 envelope header
// is emitted on the first flush even when the buffer is empty, so a zero-byte
// stream still produces a valid (header-only) artifact.
// Must be called after all Write calls complete.
func (e *ChunkEncryptWriter) Flush() error {
	if err := e.ensureHeader(); err != nil {
		return err
	}
	if len(e.buf) > 0 {
		return e.flushChunk()
	}
	return nil
}

func (e *ChunkEncryptWriter) ensureHeader() error {
	if e.headerWritten {
		return nil
	}
	if err := writeHeader(e.w); err != nil {
		return err
	}
	e.headerWritten = true
	return nil
}

func (e *ChunkEncryptWriter) flushChunk() error {
	if err := e.ensureHeader(); err != nil {
		return err
	}
	if e.chunkIdx == math.MaxUint64 {
		logCryptoEvent(context.Background(), EventNonceCounterOverflow,
			slog.String("backup_id", string(e.aad)),
			slog.Uint64("chunk_count", e.chunkIdx),
		)
		return ErrChunkCounterOverflow
	}

	nonce := e.chunkNonce(e.chunkIdx)
	ciphertext := e.gcm.Seal(nil, nonce, e.buf, e.aad)

	// Extract auth tag (last 16 bytes of ciphertext)
	tagSize := e.gcm.Overhead()
	if len(ciphertext) >= tagSize {
		e.lastTag = ciphertext[len(ciphertext)-tagSize:]
	}

	// Write 4-byte length prefix + ciphertext+tag
	var lenBuf [4]byte
	binary.LittleEndian.PutUint32(lenBuf[:], uint32(len(ciphertext)))
	if _, err := e.w.Write(lenBuf[:]); err != nil {
		return fmt.Errorf("crypto: failed to write chunk length: %w", err)
	}
	if _, err := e.w.Write(ciphertext); err != nil {
		return fmt.Errorf("crypto: failed to write encrypted chunk: %w", err)
	}

	e.chunkIdx++
	e.buf = e.buf[:0]
	return nil
}

// chunkNonce returns the nonce for chunkIdx by XOR-ing the base nonce with the index.
func (e *ChunkEncryptWriter) chunkNonce(idx uint64) []byte {
	nonce := make([]byte, len(e.baseNonce))
	copy(nonce, e.baseNonce)

	// XOR the last 8 bytes of the nonce with idx (little-endian)
	var idxBuf [8]byte
	binary.LittleEndian.PutUint64(idxBuf[:], idx)
	offset := len(nonce) - 8
	for i := range 8 {
		nonce[offset+i] ^= idxBuf[i]
	}
	return nonce
}
