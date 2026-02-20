package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"fmt"
	"io"
)

// ChunkDecryptReader reads AES-256-GCM encrypted data from r and decrypts it.
// It expects the format written by ChunkEncryptWriter:
// repeated blocks of [4-byte-len][ciphertext+tag].
type ChunkDecryptReader struct {
	r         io.Reader
	gcm       cipher.AEAD
	baseNonce []byte
	aad       []byte
	chunkIdx  uint64
	plainBuf  []byte
	eof       bool
}

// NewChunkDecryptReader returns a ChunkDecryptReader that decrypts data from r.
// key must be 32 bytes. baseNonce must match the value from the backup manifest.
// backupID must match the AAD used during encryption.
func NewChunkDecryptReader(r io.Reader, key, baseNonce []byte, backupID string) (*ChunkDecryptReader, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("crypto: failed to create AES cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("crypto: failed to create GCM: %w", err)
	}

	return &ChunkDecryptReader{
		r:         r,
		gcm:       gcm,
		baseNonce: baseNonce,
		aad:       []byte(backupID),
	}, nil
}

// Read implements io.Reader. It decrypts chunks on demand.
func (d *ChunkDecryptReader) Read(p []byte) (int, error) {
	// Return buffered plaintext first
	if len(d.plainBuf) > 0 {
		n := copy(p, d.plainBuf)
		d.plainBuf = d.plainBuf[n:]
		return n, nil
	}

	if d.eof {
		return 0, io.EOF
	}

	// Read next chunk
	if err := d.readChunk(); err != nil {
		if err == io.EOF {
			d.eof = true
			return 0, io.EOF
		}
		return 0, err
	}

	n := copy(p, d.plainBuf)
	d.plainBuf = d.plainBuf[n:]
	return n, nil
}

func (d *ChunkDecryptReader) readChunk() error {
	// Read 4-byte length prefix
	var lenBuf [4]byte
	if _, err := io.ReadFull(d.r, lenBuf[:]); err != nil {
		return err // io.EOF or partial read
	}

	chunkLen := binary.LittleEndian.Uint32(lenBuf[:])
	if chunkLen == 0 {
		return io.EOF
	}

	// Read ciphertext+tag
	ciphertext := make([]byte, chunkLen)
	if _, err := io.ReadFull(d.r, ciphertext); err != nil {
		return fmt.Errorf("crypto: failed to read encrypted chunk: %w", err)
	}

	// Decrypt with auth tag verification
	nonce := d.chunkNonce(d.chunkIdx)
	plaintext, err := d.gcm.Open(nil, nonce, ciphertext, d.aad)
	if err != nil {
		return fmt.Errorf("crypto: authentication tag verification failed (chunk %d): %w", d.chunkIdx, err)
	}

	d.chunkIdx++
	d.plainBuf = plaintext
	return nil
}

// chunkNonce mirrors the nonce derivation used by ChunkEncryptWriter.
func (d *ChunkDecryptReader) chunkNonce(idx uint64) []byte {
	nonce := make([]byte, len(d.baseNonce))
	copy(nonce, d.baseNonce)

	var idxBuf [8]byte
	binary.LittleEndian.PutUint64(idxBuf[:], idx)
	offset := len(nonce) - 8
	for i := 0; i < 8; i++ {
		nonce[offset+i] ^= idxBuf[i]
	}
	return nonce
}
