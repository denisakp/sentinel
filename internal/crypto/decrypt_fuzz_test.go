package crypto_test

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"testing"

	"github.com/denisakp/sentinel/internal/crypto"
	"github.com/denisakp/sentinel/internal/ports"
)

// FuzzChunkDecryptReader feeds arbitrary input to the decrypter and asserts
// the only failure modes are typed errors from a known closed set. No panic,
// no OOM, no hang.
func FuzzChunkDecryptReader(f *testing.F) {
	key := makeTestKey()
	baseNonce := makeTestBaseNonce()
	backupID := "fuzz"

	// Seed 1: valid one-chunk stream (encrypted inline so we don't pull *testing.T helpers).
	{
		var buf bytes.Buffer
		enc, err := crypto.NewChunkEncryptWriter(&buf, key, backupID)
		if err != nil {
			f.Fatalf("seed encrypter: %v", err)
		}
		if _, err := enc.Write([]byte("hello")); err != nil {
			f.Fatalf("seed write: %v", err)
		}
		if err := enc.Flush(); err != nil {
			f.Fatalf("seed flush: %v", err)
		}
		baseNonce = enc.BaseNonce()
		f.Add(buf.Bytes())
	}
	// Seed 2: empty.
	f.Add([]byte{})
	// Seed 3: malformed v2 header (wrong magic).
	f.Add([]byte{'X', 'X', 'X', 'X', 0x02})
	// Seed 4: valid header + oversized length prefix (no body).
	{
		s := []byte{'S', 'E', 'N', 'C', 0x02}
		var lb [4]byte
		binary.LittleEndian.PutUint32(lb[:], 1<<30)
		f.Add(append(s, lb[:]...))
	}
	// Seed 5: valid header + zero-length prefix.
	f.Add([]byte{'S', 'E', 'N', 'C', 0x02, 0, 0, 0, 0})
	// Seed 6: valid header + max-uint32 length prefix + a couple of garbage body bytes.
	f.Add([]byte{'S', 'E', 'N', 'C', 0x02, 0xFF, 0xFF, 0xFF, 0xFF, 'A', 'A'})

	f.Fuzz(func(t *testing.T, input []byte) {
		dec, err := crypto.NewChunkDecryptReader(bytes.NewReader(input), key, baseNonce, backupID)
		if err != nil {
			// Constructor should not fail on arbitrary input — key/nonce are valid here.
			t.Fatalf("NewChunkDecryptReader: %v", err)
		}

		// Bounded sink — io.ReadAll returns once the reader stops producing.
		// Any panic or hang is caught by the fuzz framework.
		_, err = io.ReadAll(dec)
		if err == nil {
			return
		}

		switch {
		case errors.Is(err, ports.ErrChunkTooLarge):
		case errors.Is(err, ports.ErrAuthTagFailed):
		case errors.Is(err, ports.ErrLegacyEnvelope):
		case errors.Is(err, io.EOF):
		case errors.Is(err, io.ErrUnexpectedEOF):
		default:
			var unsupp ports.ErrUnsupportedEnvelopeVersion
			if errors.As(err, &unsupp) {
				return
			}
			t.Fatalf("unexpected error type: %T (%v) — fuzz inputs must only produce known-typed errors", err, err)
		}
	})
}
