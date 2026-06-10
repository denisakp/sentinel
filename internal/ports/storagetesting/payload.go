package storagetesting

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"math/rand"
	"sync"
)

// LargePayloadSize is the fixed "large-object" contract case size (64 MiB).
const LargePayloadSize = 64 * 1024 * 1024

var (
	largeOnce   sync.Once
	largeBytes  []byte
	largeSHA256 string
)

func ensureLargePayload() {
	largeOnce.Do(func() {
		buf := make([]byte, LargePayloadSize)
		r := rand.New(rand.NewSource(1))
		if _, err := io.ReadFull(r, buf); err != nil {
			panic(err)
		}
		largeBytes = buf
		sum := sha256.Sum256(buf)
		largeSHA256 = hex.EncodeToString(sum[:])
	})
}

// LargePayload returns a fresh io.Reader yielding the deterministic 64 MiB
// contract payload. The underlying bytes are generated once per process and
// shared across calls; only the Reader cursor is per-call.
func LargePayload() io.Reader {
	ensureLargePayload()
	return bytes.NewReader(largeBytes)
}

// LargePayloadBytes returns the underlying bytes for callers that need to
// write the payload to a file. Do not mutate the returned slice.
func LargePayloadBytes() []byte {
	ensureLargePayload()
	return largeBytes
}

// LargePayloadSHA256 is the hex-encoded SHA-256 of the canonical payload.
func LargePayloadSHA256() string {
	ensureLargePayload()
	return largeSHA256
}
