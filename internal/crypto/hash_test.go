package crypto_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/denisakp/sentinel/internal/crypto"
)

func TestHashingWriter_SingleChunk(t *testing.T) {
	var buf bytes.Buffer
	hw := crypto.NewHashingWriter(&buf)

	input := []byte("hello, sentinel")
	if _, err := hw.Write(input); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	got := hw.Sum()
	want := sha256hex(input)

	if got != want {
		t.Errorf("Sum() = %q, want %q", got, want)
	}

	if !bytes.Equal(buf.Bytes(), input) {
		t.Error("underlying writer did not receive the data")
	}
}

func TestHashingWriter_MultiChunk(t *testing.T) {
	var buf bytes.Buffer
	hw := crypto.NewHashingWriter(&buf)

	chunks := []string{"chunk1", "chunk2", "chunk3"}
	full := strings.Join(chunks, "")

	for _, c := range chunks {
		if _, err := hw.Write([]byte(c)); err != nil {
			t.Fatalf("Write(%q) error = %v", c, err)
		}
	}

	got := hw.Sum()
	want := sha256hex([]byte(full))

	if got != want {
		t.Errorf("MultiChunk Sum() = %q, want %q", got, want)
	}
}

func sha256hex(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}
