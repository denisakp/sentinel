package ports

import "io"

// Hasher abstracts *internal/crypto.HashingWriter (current concrete implementation).
//
// A Hasher consumes bytes through its io.Writer interface and exposes the
// running digest via Sum. The concrete implementation streams a SHA-256 hash
// in the same pass as the underlying write — see spec 026 (single-pass
// manifest hashing) for the design driver.
type Hasher interface {
	io.Writer
	Sum() string
}
