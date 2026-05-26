package crypto_test

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/denisakp/sentinel/internal/crypto"
	"github.com/denisakp/sentinel/internal/ports"
)

const (
	testChunkSize    = 64 * 1024
	testMaxChunkSize = testChunkSize + 16
	testGCMOverhead  = 16
)

func makeTestKey() []byte {
	k := make([]byte, 32)
	for i := range k {
		k[i] = byte(i + 1)
	}
	return k
}

func makeTestBaseNonce() []byte {
	n := make([]byte, 12)
	for i := range n {
		n[i] = byte(0xAA ^ i)
	}
	return n
}

// forgeHeader returns a v2 envelope header (5 bytes).
func forgeHeader() []byte {
	return []byte{'S', 'E', 'N', 'C', 0x02}
}

// forgeStreamWithLen returns a 9-byte stream: v2 header + uint32-LE length prefix
// (no chunk body — by design, so a missing bound check would either block or OOM).
func forgeStreamWithLen(chunkLen uint32) []byte {
	out := append([]byte{}, forgeHeader()...)
	var lenBuf [4]byte
	binary.LittleEndian.PutUint32(lenBuf[:], chunkLen)
	return append(out, lenBuf[:]...)
}

// forgeValidOneChunk encrypts plaintext into a v2 envelope stream and returns
// the raw bytes plus the base nonce used (so the test can construct a reader).
func forgeValidOneChunk(t *testing.T, key []byte, backupID string, plaintext []byte) ([]byte, []byte) {
	t.Helper()
	var buf bytes.Buffer
	enc, err := crypto.NewChunkEncryptWriter(&buf, key, backupID)
	if err != nil {
		t.Fatalf("NewChunkEncryptWriter: %v", err)
	}
	if _, err := enc.Write(plaintext); err != nil {
		t.Fatalf("enc.Write: %v", err)
	}
	if err := enc.Flush(); err != nil {
		t.Fatalf("enc.Flush: %v", err)
	}
	return buf.Bytes(), enc.BaseNonce()
}

func TestChunkBoundEnforced(t *testing.T) {
	key := makeTestKey()
	backupID := "bound-test"

	cases := []struct {
		name     string
		chunkLen uint32
		wantErr  error
	}{
		{"zero_length", 0, ports.ErrChunkTooLarge},
		{"one_over_max", uint32(testMaxChunkSize + 1), ports.ErrChunkTooLarge},
		{"one_gib", 1 << 30, ports.ErrChunkTooLarge},
		{"max_uint32", ^uint32(0), ports.ErrChunkTooLarge},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stream := forgeStreamWithLen(tc.chunkLen)
			dec, err := crypto.NewChunkDecryptReader(bytes.NewReader(stream), key, makeTestBaseNonce(), backupID)
			if err != nil {
				t.Fatalf("NewChunkDecryptReader: %v", err)
			}

			start := time.Now()
			_, err = io.ReadAll(dec)
			elapsed := time.Since(start)

			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want errors.Is(_, %v)", err, tc.wantErr)
			}
			if elapsed > 10*time.Millisecond {
				t.Fatalf("rejection took %v, want < 10ms (a missing bound check would block or OOM)", elapsed)
			}
		})
	}
}

func TestChunkBound_MaxChunkSize_BoundaryInclusive(t *testing.T) {
	key := makeTestKey()
	backupID := "boundary"

	// Plaintext sized exactly to chunkSize so the encoded chunk is chunkSize+16 = maxChunkSize.
	plaintext := bytes.Repeat([]byte("A"), testChunkSize)
	stream, baseNonce := forgeValidOneChunk(t, key, backupID, plaintext)

	dec, err := crypto.NewChunkDecryptReader(bytes.NewReader(stream), key, baseNonce, backupID)
	if err != nil {
		t.Fatalf("NewChunkDecryptReader: %v", err)
	}
	got, err := io.ReadAll(dec)
	if err != nil {
		t.Fatalf("ReadAll: %v, want nil (chunkLen=maxChunkSize must be accepted)", err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Fatalf("plaintext mismatch (boundary chunk decrypt)")
	}
}

func TestChunkBound_WrongKey_AuthTagFailed(t *testing.T) {
	key := makeTestKey()
	backupID := "wrongkey"
	plaintext := []byte("hello sentinel")

	stream, baseNonce := forgeValidOneChunk(t, key, backupID, plaintext)

	// Decrypt with a different key.
	wrongKey := make([]byte, 32)
	for i := range wrongKey {
		wrongKey[i] = byte(0xFF ^ i)
	}

	dec, err := crypto.NewChunkDecryptReader(bytes.NewReader(stream), wrongKey, baseNonce, backupID)
	if err != nil {
		t.Fatalf("NewChunkDecryptReader: %v", err)
	}
	_, err = io.ReadAll(dec)
	if !errors.Is(err, ports.ErrAuthTagFailed) {
		t.Fatalf("err = %v, want errors.Is(_, ErrAuthTagFailed)", err)
	}
}

// TestMaxChunkSize_MatchesGCMOverhead verifies the package-level invariant that
// the decoder's compile-time bound matches what AES-GCM actually produces at
// runtime. If a future change to the AEAD construction alters Overhead(), this
// catches the drift loudly.
func TestMaxChunkSize_MatchesGCMOverhead(t *testing.T) {
	block, err := aes.NewCipher(makeTestKey())
	if err != nil {
		t.Fatalf("aes.NewCipher: %v", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatalf("cipher.NewGCM: %v", err)
	}
	if gcm.Overhead() != testGCMOverhead {
		t.Fatalf("gcm.Overhead() = %d, want %d (testMaxChunkSize assumption broken)", gcm.Overhead(), testGCMOverhead)
	}
}
