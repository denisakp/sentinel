package ports

// KeyProvider abstracts *internal/crypto.FileKeyProvider (current concrete implementation).
//
// Implementations return the 32-byte AES-256 master key used by the
// encryption ports. The concrete file-based implementation reads from an
// environment variable or a key file; a future KMS-backed implementation
// would satisfy the same contract.
//
// The free helpers GenerateKey, DeriveKey, GenerateSalt live in
// internal/adapters/crypto/. They are stateless utility
// functions that callers can import directly from the adapter without
// violating the dependency rule, so they do not need a port surface.
type KeyProvider interface {
	GetKey() ([]byte, error)
}
