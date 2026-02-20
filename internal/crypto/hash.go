package crypto

import (
	"crypto/sha256"
	"encoding/hex"
	"hash"
	"io"
)

// HashingWriter wraps an io.Writer and computes a SHA-256 digest in the same pass.
// This avoids a second read of the data and keeps memory usage minimal.
type HashingWriter struct {
	w io.Writer
	h hash.Hash
}

// NewHashingWriter returns a HashingWriter that writes to w and hashes all written bytes.
func NewHashingWriter(w io.Writer) *HashingWriter {
	return &HashingWriter{
		w: w,
		h: sha256.New(),
	}
}

// Write implements io.Writer. Bytes are written to the underlying writer and
// simultaneously fed into the SHA-256 hash state.
func (hw *HashingWriter) Write(p []byte) (int, error) {
	n, err := hw.w.Write(p)
	if n > 0 {
		// hash.Hash.Write never returns an error
		_, _ = hw.h.Write(p[:n])
	}
	return n, err
}

// Sum returns the hex-encoded SHA-256 digest of all bytes written so far.
func (hw *HashingWriter) Sum() string {
	return hex.EncodeToString(hw.h.Sum(nil))
}
