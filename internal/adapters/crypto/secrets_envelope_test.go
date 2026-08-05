package crypto

import (
	"bytes"
	"errors"
	"testing"

	"github.com/denisakp/sentinel/internal/ports"
)

func testKey(t *testing.T) []byte {
	t.Helper()
	k := make([]byte, 32)
	for i := range k {
		k[i] = byte(i + 1)
	}
	return k
}

func TestEncryptSecretsFile_RoundTrip(t *testing.T) {
	key := testKey(t)
	plaintext := []byte("[client]\nuser=root\npassword=s3cr3t\n")

	enc, err := EncryptSecretsFile(plaintext, key)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if !IsEncryptedSecretsFile(enc) {
		t.Fatal("produced file not detected as SSEC")
	}

	got, err := DecryptSecretsFile(enc, key)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Fatalf("round-trip mismatch: got %q want %q", got, plaintext)
	}
}

func TestEncryptSecretsFile_EmptyPlaintext(t *testing.T) {
	key := testKey(t)
	enc, err := EncryptSecretsFile(nil, key)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	got, err := DecryptSecretsFile(enc, key)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty plaintext, got %q", got)
	}
}

func TestDecryptSecretsFile_WrongKey(t *testing.T) {
	key := testKey(t)
	enc, err := EncryptSecretsFile([]byte("password=hunter2"), key)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	wrong := make([]byte, 32)
	copy(wrong, key)
	wrong[0] ^= 0xFF

	_, err = DecryptSecretsFile(enc, wrong)
	if !errors.Is(err, ports.ErrAuthTagFailed) {
		t.Fatalf("expected ErrAuthTagFailed, got %v", err)
	}
}

func TestDecryptSecretsFile_CorruptPayload(t *testing.T) {
	key := testKey(t)
	enc, err := EncryptSecretsFile([]byte("password=hunter2"), key)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	// Flip a byte inside the SENC payload (past the 17-byte prefix + SENC header).
	enc[secretsPrefixSize+8] ^= 0xFF

	if _, err := DecryptSecretsFile(enc, key); err == nil {
		t.Fatal("expected error on corrupt payload, got nil")
	}
}

func TestDecryptSecretsFile_Truncated(t *testing.T) {
	key := testKey(t)
	enc, err := EncryptSecretsFile([]byte("password=hunter2"), key)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	truncated := enc[:len(enc)-4]
	if _, err := DecryptSecretsFile(truncated, key); err == nil {
		t.Fatal("expected error on truncated container, got nil")
	}
}

func TestDecryptSecretsFile_BadMagic(t *testing.T) {
	key := testKey(t)
	// A backup-artifact SENC stream must NOT be treated as an SSEC secrets file.
	var artifact bytes.Buffer
	w, err := NewChunkEncryptWriter(&artifact, key, "some-backup-id")
	if err != nil {
		t.Fatalf("writer: %v", err)
	}
	_, _ = w.Write([]byte("dump data"))
	_ = w.Flush()

	if IsEncryptedSecretsFile(artifact.Bytes()) {
		t.Fatal("SENC backup artifact wrongly detected as SSEC secrets file")
	}
	if _, err := DecryptSecretsFile(artifact.Bytes(), key); !errors.Is(err, ErrSecretsEnvelopeCorrupt) {
		t.Fatalf("expected ErrSecretsEnvelopeCorrupt for non-SSEC input, got %v", err)
	}
}

func TestDecryptSecretsFile_UnsupportedVersion(t *testing.T) {
	key := testKey(t)
	enc, err := EncryptSecretsFile([]byte("password=hunter2"), key)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	enc[4] = 0x02 // bump version byte

	if _, err := DecryptSecretsFile(enc, key); !errors.Is(err, ErrSecretsEnvelopeUnsupportedVersion) {
		t.Fatalf("expected ErrSecretsEnvelopeUnsupportedVersion, got %v", err)
	}
}

func TestIsEncryptedSecretsFile_Plaintext(t *testing.T) {
	if IsEncryptedSecretsFile([]byte("[client]\nuser=root\n")) {
		t.Fatal("plaintext my.cnf wrongly detected as encrypted")
	}
	if IsEncryptedSecretsFile([]byte("SS")) {
		t.Fatal("too-short input wrongly detected as encrypted")
	}
	if IsEncryptedSecretsFile(nil) {
		t.Fatal("nil wrongly detected as encrypted")
	}
}
