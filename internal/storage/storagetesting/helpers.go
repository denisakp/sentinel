package storagetesting

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"testing"
)

// TruncatingReader wraps an io.Reader and returns io.ErrUnexpectedEOF after
// at most N bytes have been read from the underlying reader. It is used by
// the partial_failure_visibility contract case to simulate a caller stream
// dying mid-upload.
type TruncatingReader struct {
	R    io.Reader
	N    int64
	read int64
}

func (t *TruncatingReader) Read(p []byte) (int, error) {
	if t.read >= t.N {
		return 0, io.ErrUnexpectedEOF
	}
	remaining := t.N - t.read
	if int64(len(p)) > remaining {
		p = p[:remaining]
	}
	n, err := t.R.Read(p)
	t.read += int64(n)
	if err == nil && t.read >= t.N {
		err = io.ErrUnexpectedEOF
	}
	return n, err
}

// StreamingSHA256 returns the SHA-256 of r consumed in a single pass with
// no full buffering.
func StreamingSHA256(r io.Reader) ([]byte, error) {
	h := sha256.New()
	if _, err := io.Copy(h, r); err != nil {
		return nil, err
	}
	return h.Sum(nil), nil
}

// RandKey returns a unique key per subtest with the given prefix.
func RandKey(t *testing.T, prefix string) string {
	t.Helper()
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatalf("RandKey: %v", err)
	}
	return fmt.Sprintf("%s-%s", prefix, hex.EncodeToString(b[:]))
}
