package crypto

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
)

// DecryptOptions controls how the decrypt path handles envelope versioning.
type DecryptOptions struct {
	// AllowLegacy permits decrypting pre-v2 (unversioned) artifacts. Off by default.
	AllowLegacy bool
	// Source is a human-readable path/URI identifying the artifact (for log lines).
	Source string
	// BackupID is the AAD used during encryption.
	BackupID string
}

// ChunkDecryptReader reads AES-256-GCM encrypted data from r and decrypts it.
// It expects the v2 envelope format: a 5-byte header `"SENC" || 0x02` followed
// by repeated `[4-byte-len][ciphertext+tag]` chunk records.
type ChunkDecryptReader struct {
	r             io.Reader
	gcm           cipher.AEAD
	baseNonce     []byte
	aad           []byte
	opts          DecryptOptions
	chunkIdx      uint64
	plainBuf      []byte
	eof           bool
	headerChecked bool
}

// NewChunkDecryptReader returns a ChunkDecryptReader that decrypts data from r.
// key must be 32 bytes. baseNonce must match the value from the backup manifest.
// backupID must match the AAD used during encryption.
//
// Callers SHOULD prefer NewChunkDecryptReaderWithOptions so log lines carry a
// meaningful source.
func NewChunkDecryptReader(r io.Reader, key, baseNonce []byte, backupID string) (*ChunkDecryptReader, error) {
	return NewChunkDecryptReaderWithOptions(r, key, baseNonce, DecryptOptions{BackupID: backupID})
}

// NewChunkDecryptReaderWithOptions is the explicit constructor. AllowLegacy=false
// refuses pre-v2 artifacts; AllowLegacy=true emits a loud warning and proceeds.
func NewChunkDecryptReaderWithOptions(r io.Reader, key, baseNonce []byte, opts DecryptOptions) (*ChunkDecryptReader, error) {
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
		aad:       []byte(opts.BackupID),
		opts:      opts,
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
	if !d.headerChecked {
		if err := d.consumeHeader(); err != nil {
			return err
		}
	}

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

func (d *ChunkDecryptReader) consumeHeader() error {
	_, isLegacy, leadBytes, err := readAndClassifyHeader(d.r)
	if err != nil {
		if unsupp, ok := err.(ErrUnsupportedEnvelopeVersion); ok {
			logCryptoEvent(context.Background(), EventEnvelopeUnknownVersion,
				slog.String("backup_id", d.opts.BackupID),
				slog.String("source", d.opts.Source),
				slog.Uint64("version_byte", uint64(unsupp.Version)),
			)
			return fmt.Errorf("crypto: refusing unknown envelope version for %q from %q: %w",
				d.opts.BackupID, d.opts.Source, err)
		}
		return err
	}

	if isLegacy {
		if !d.opts.AllowLegacy {
			return fmt.Errorf("crypto: legacy envelope for %q from %q: %w",
				d.opts.BackupID, d.opts.Source, ErrLegacyEnvelope)
		}
		logCryptoEvent(context.Background(), EventLegacyEnvelopeDecrypt,
			slog.String("backup_id", d.opts.BackupID),
			slog.String("source", d.opts.Source),
		)
		d.r = io.MultiReader(bytes.NewReader(leadBytes), d.r)
		d.headerChecked = true
		return nil
	}

	d.headerChecked = true
	return nil
}

// chunkNonce mirrors the nonce derivation used by ChunkEncryptWriter.
func (d *ChunkDecryptReader) chunkNonce(idx uint64) []byte {
	nonce := make([]byte, len(d.baseNonce))
	copy(nonce, d.baseNonce)

	var idxBuf [8]byte
	binary.LittleEndian.PutUint64(idxBuf[:], idx)
	offset := len(nonce) - 8
	for i := range 8 {
		nonce[offset+i] ^= idxBuf[i]
	}
	return nonce
}
