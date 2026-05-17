package crypto_test

import (
	"bytes"
	"io"
	"testing"

	"github.com/denisakp/sentinel/internal/crypto"
)

func TestEncryptDecrypt_RoundTrip(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}

	plaintext := []byte("this is a test backup payload for sentinel encryption round-trip")
	backupID := "backup-test-001"

	// Encrypt
	var encBuf bytes.Buffer
	enc, err := crypto.NewChunkEncryptWriter(&encBuf, key, backupID)
	if err != nil {
		t.Fatalf("NewChunkEncryptWriter() error = %v", err)
	}
	if _, err := enc.Write(plaintext); err != nil {
		t.Fatalf("enc.Write() error = %v", err)
	}
	if err := enc.Flush(); err != nil {
		t.Fatalf("enc.Flush() error = %v", err)
	}

	baseNonce := enc.BaseNonce()

	// Decrypt
	dec, err := crypto.NewChunkDecryptReader(&encBuf, key, baseNonce, backupID)
	if err != nil {
		t.Fatalf("NewChunkDecryptReader() error = %v", err)
	}

	got, err := io.ReadAll(dec)
	if err != nil {
		t.Fatalf("ReadAll(dec) error = %v", err)
	}

	if !bytes.Equal(got, plaintext) {
		t.Errorf("decrypt output differs from input\ngot:  %q\nwant: %q", got, plaintext)
	}
}

func TestChunkEncryptWriter_LastAuthTag(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}

	var buf bytes.Buffer
	enc, err := crypto.NewChunkEncryptWriter(&buf, key, "test-auth-tag")
	if err != nil {
		t.Fatalf("NewChunkEncryptWriter() error = %v", err)
	}
	if _, err := enc.Write([]byte("some data to encrypt")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if err := enc.Flush(); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}
	tag := enc.LastAuthTag()
	if len(tag) == 0 {
		t.Error("LastAuthTag() returned empty tag after encryption")
	}
}

func TestChunkEncryptWriter_EmptyFlush(t *testing.T) {
	key := make([]byte, 32)
	var buf bytes.Buffer
	enc, err := crypto.NewChunkEncryptWriter(&buf, key, "test-empty-flush")
	if err != nil {
		t.Fatalf("NewChunkEncryptWriter() error = %v", err)
	}
	// Empty Flush emits the v2 envelope header (5 bytes) and no chunk records.
	if err := enc.Flush(); err != nil {
		t.Errorf("Flush() on empty buffer error = %v", err)
	}
	want := []byte{'S', 'E', 'N', 'C', 0x02}
	if !bytes.Equal(buf.Bytes(), want) {
		t.Errorf("empty Flush() wrote %x, want %x", buf.Bytes(), want)
	}
}

func TestEncryptDecrypt_LargePayload(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	// 200KB payload — spans multiple 64KB chunks
	payload := make([]byte, 200*1024)
	for i := range payload {
		payload[i] = byte(i % 251)
	}
	backupID := "backup-large-001"

	var encBuf bytes.Buffer
	enc, err := crypto.NewChunkEncryptWriter(&encBuf, key, backupID)
	if err != nil {
		t.Fatalf("NewChunkEncryptWriter() error = %v", err)
	}
	if _, err := enc.Write(payload); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if err := enc.Flush(); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}
	baseNonce := enc.BaseNonce()

	dec, err := crypto.NewChunkDecryptReader(&encBuf, key, baseNonce, backupID)
	if err != nil {
		t.Fatalf("NewChunkDecryptReader() error = %v", err)
	}
	got, err := io.ReadAll(dec)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Error("large payload round-trip: decrypted output differs from input")
	}
}

func TestEncryptDecrypt_AuthTagTamper(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}

	plaintext := []byte("tamper test data")
	backupID := "backup-tamper-001"

	var encBuf bytes.Buffer
	enc, err := crypto.NewChunkEncryptWriter(&encBuf, key, backupID)
	if err != nil {
		t.Fatalf("NewChunkEncryptWriter() error = %v", err)
	}
	enc.Write(plaintext)
	enc.Flush()

	baseNonce := enc.BaseNonce()

	// Tamper: flip a byte in the middle of the encrypted data
	data := encBuf.Bytes()
	if len(data) > 10 {
		data[len(data)/2] ^= 0xFF
	}

	dec, err := crypto.NewChunkDecryptReader(bytes.NewReader(data), key, baseNonce, backupID)
	if err != nil {
		t.Fatalf("NewChunkDecryptReader() error = %v", err)
	}

	_, err = io.ReadAll(dec)
	if err == nil {
		t.Error("expected authentication tag verification failure for tampered ciphertext, got nil")
	}
}
