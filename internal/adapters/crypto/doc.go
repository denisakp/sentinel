// Package crypto holds Sentinel's concrete cryptographic adapters: AES-256-GCM
// streaming encryption ([ChunkEncryptWriter] / [ChunkDecryptReader]), the
// single-pass SHA-256 [HashingWriter] used by the backup manifest, and the
// file/env-backed [FileKeyProvider] used to load the master key. The free
// helpers [GenerateKey], [DeriveKey], and [GenerateSalt] remain at package
// scope because they are stateless utilities.
//
// This package satisfies the ports declared in internal/ports/:
//
//   - ports.EncryptWriter ← *ChunkEncryptWriter
//   - ports.DecryptReader ← *ChunkDecryptReader
//   - ports.Hasher        ← *HashingWriter
//   - ports.KeyProvider   ← *FileKeyProvider
//
// Compile-time assertions live in conformance.go.
//
// See:
//   - docs/adr/0001-adopt-hexagonal-architecture.md (hexagonal layering)
//   - docs/adr/0006-encryption-envelope-v1.md       (envelope v1/v2 contract)
//   - specs/030-crypto-adapter-migration/           (this package's relocation)
package crypto
