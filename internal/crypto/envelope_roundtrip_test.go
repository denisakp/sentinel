package crypto_test

import (
	"bytes"
	"errors"
	"io"
	"math/rand"
	"testing"

	"github.com/denisakp/sentinel/internal/crypto"
	"github.com/denisakp/sentinel/internal/ports"
)

func TestEnvelopeV2RoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping large round-trip under -short")
	}
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	const N = 100 * 1024 * 1024
	src := make([]byte, N)
	r := rand.New(rand.NewSource(42))
	r.Read(src)

	var encBuf bytes.Buffer
	enc, err := crypto.NewChunkEncryptWriter(&encBuf, key, "rt-100mb")
	if err != nil {
		t.Fatalf("NewChunkEncryptWriter() error = %v", err)
	}
	if _, err := enc.Write(src); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if err := enc.Flush(); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}

	encBytes := encBuf.Bytes()
	if len(encBytes) < 5 || !bytes.Equal(encBytes[:5], []byte{'S', 'E', 'N', 'C', 0x02}) {
		t.Fatalf("missing v2 header: first5=%x", encBytes[:5])
	}

	dec, err := crypto.NewChunkDecryptReaderWithOptions(&encBuf, key, enc.BaseNonce(),
		ports.DecryptOptions{BackupID: "rt-100mb"})
	if err != nil {
		t.Fatalf("NewChunkDecryptReaderWithOptions() error = %v", err)
	}
	got, err := io.ReadAll(dec)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if !bytes.Equal(got, src) {
		t.Errorf("round-trip mismatch (got=%d, want=%d)", len(got), len(src))
	}
}

func TestEnvelopeV2_EmptyStream(t *testing.T) {
	key := make([]byte, 32)
	var encBuf bytes.Buffer
	enc, err := crypto.NewChunkEncryptWriter(&encBuf, key, "empty")
	if err != nil {
		t.Fatalf("NewChunkEncryptWriter() error = %v", err)
	}
	if err := enc.Flush(); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}
	if !bytes.Equal(encBuf.Bytes(), []byte{'S', 'E', 'N', 'C', 0x02}) {
		t.Fatalf("empty stream output = %x", encBuf.Bytes())
	}

	dec, err := crypto.NewChunkDecryptReaderWithOptions(&encBuf, key, enc.BaseNonce(),
		ports.DecryptOptions{BackupID: "empty"})
	if err != nil {
		t.Fatalf("NewChunkDecryptReaderWithOptions() error = %v", err)
	}
	out, err := io.ReadAll(dec)
	if err != nil && !errors.Is(err, io.EOF) {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if len(out) != 0 {
		t.Errorf("got %d bytes, want 0", len(out))
	}
}

func TestDecryptEnvelope_RejectsUnknownVersion(t *testing.T) {
	key := make([]byte, 32)
	stream := append([]byte{'S', 'E', 'N', 'C', 0xFF}, []byte("garbage")...)
	dec, err := crypto.NewChunkDecryptReaderWithOptions(bytes.NewReader(stream), key, make([]byte, 12),
		ports.DecryptOptions{BackupID: "unk", Source: "/tmp/unk.enc"})
	if err != nil {
		t.Fatalf("NewChunkDecryptReaderWithOptions() error = %v", err)
	}
	var sink bytes.Buffer
	_, err = io.Copy(&sink, dec)
	var unsupp ports.ErrUnsupportedEnvelopeVersion
	if !errors.As(err, &unsupp) {
		t.Fatalf("err = %v, want ErrUnsupportedEnvelopeVersion", err)
	}
	if unsupp.Version != 0xFF {
		t.Errorf("Version=0x%02X want 0xFF", unsupp.Version)
	}
	if sink.Len() != 0 {
		t.Errorf("plaintext written = %d, want 0", sink.Len())
	}
}

// writeLegacyV1 produces a stream without the v2 header — only chunk records.
// We can't reproduce v1 from the current writer (it always writes the header),
// so we strip the header bytes off after encrypt.
func writeLegacyV1(t *testing.T, key, baseNonce []byte, backupID string, plaintext []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	enc, err := crypto.NewChunkEncryptWriter(&buf, key, backupID)
	if err != nil {
		t.Fatalf("NewChunkEncryptWriter() error = %v", err)
	}
	if _, err := enc.Write(plaintext); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if err := enc.Flush(); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}
	// Encrypt path uses a random base nonce; we must mirror it back to baseNonce out-param.
	copy(baseNonce, enc.BaseNonce())
	// Strip the 5-byte v2 header to simulate a legacy v1 stream.
	out := buf.Bytes()
	if len(out) < 5 {
		t.Fatalf("encrypt produced %d bytes, want >= 5", len(out))
	}
	return append([]byte{}, out[5:]...)
}

func TestDecryptEnvelope_RefusesLegacyByDefault(t *testing.T) {
	key := make([]byte, 32)
	baseNonce := make([]byte, 12)
	plaintext := []byte("legacy payload")
	legacy := writeLegacyV1(t, key, baseNonce, "legacy-1", plaintext)

	dec, err := crypto.NewChunkDecryptReaderWithOptions(bytes.NewReader(legacy), key, baseNonce,
		ports.DecryptOptions{AllowLegacy: false, BackupID: "legacy-1", Source: "/tmp/legacy.enc"})
	if err != nil {
		t.Fatalf("NewChunkDecryptReaderWithOptions() error = %v", err)
	}
	var sink bytes.Buffer
	_, err = io.Copy(&sink, dec)
	if !errors.Is(err, ports.ErrLegacyEnvelope) {
		t.Fatalf("err = %v, want ErrLegacyEnvelope", err)
	}
	if sink.Len() != 0 {
		t.Errorf("plaintext written = %d, want 0", sink.Len())
	}
}

func TestDecryptEnvelope_LegacyOptInProceeds(t *testing.T) {
	key := make([]byte, 32)
	baseNonce := make([]byte, 12)
	plaintext := []byte("legacy payload to recover")
	legacy := writeLegacyV1(t, key, baseNonce, "legacy-2", plaintext)

	dec, err := crypto.NewChunkDecryptReaderWithOptions(bytes.NewReader(legacy), key, baseNonce,
		ports.DecryptOptions{AllowLegacy: true, BackupID: "legacy-2", Source: "/tmp/legacy.enc"})
	if err != nil {
		t.Fatalf("NewChunkDecryptReaderWithOptions() error = %v", err)
	}
	got, err := io.ReadAll(dec)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Errorf("round-trip mismatch: got=%q want=%q", got, plaintext)
	}
}
