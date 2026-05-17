// Package restore provides shared pre-restore helpers for integrity verification
// and decryption, used by all four database restore engines.
package restore

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/denisakp/sentinel/internal/crypto"
	"github.com/denisakp/sentinel/internal/manifest"
)

// PreRestoreVerifyAndDecrypt is the single entry point for restore pre-flight:
//
//  1. Reads the backup file at filePath
//  2. Verifies its SHA-256 hash matches manifest.Hash.Value
//  3. If manifest.Encryption != nil, decrypts the content via AES-256-GCM
//  4. Returns an io.Reader of the plaintext backup data
//
// Callers MUST handle the following sentinel errors:
//   - manifest.ErrNoManifest   → pre-v1.1 backup, log WARN and proceed with raw file
//   - ErrHashMismatch          → corruption detected, abort and delete backup
func PreRestoreVerifyAndDecrypt(
	ctx context.Context,
	m *manifest.BackupManifest,
	filePath string,
	keyProvider crypto.KeyProvider,
) (io.Reader, error) {
	return PreRestoreVerifyAndDecryptWithOptions(ctx, m, filePath, keyProvider, crypto.DecryptOptions{})
}

// PreRestoreVerifyAndDecryptWithOptions is the explicit form that lets callers pass
// envelope-version policy (e.g. AllowLegacy for pre-v2 artifacts).
func PreRestoreVerifyAndDecryptWithOptions(
	ctx context.Context,
	m *manifest.BackupManifest,
	filePath string,
	keyProvider crypto.KeyProvider,
	decryptOpts crypto.DecryptOptions,
) (io.Reader, error) {
	// Verify hash
	computed, err := computeHash(filePath)
	if err != nil {
		return nil, fmt.Errorf("restore: failed to compute hash of %q: %w", filePath, err)
	}

	if computed != m.Hash.Value {
		return nil, fmt.Errorf("%w: stored=%s computed=%s file=%q",
			ErrHashMismatch, m.Hash.Value, computed, filePath)
	}

	slog.InfoContext(ctx, "hash verification passed",
		"backup_id", m.BackupID,
		"hash", computed)

	// Open the file for reading (plaintext or ciphertext)
	f, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("restore: failed to open backup file %q: %w", filePath, err)
	}

	// No encryption → return raw file reader
	if m.Encryption == nil {
		return f, nil
	}

	// Encrypted → wire in decryption
	if keyProvider == nil {
		f.Close()
		return nil, fmt.Errorf("restore: backup is encrypted but no key provider was supplied")
	}

	masterKey, err := keyProvider.GetKey()
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("restore: failed to load master key: %w", err)
	}

	// Decode salt/IV from manifest with compatibility for older and newer encodings.
	salt, err := decodeManifestBytes(m.Encryption.Salt)
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("restore: invalid encryption salt in manifest: %w", err)
	}

	iv, err := decodeManifestBytes(m.Encryption.IV)
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("restore: invalid encryption IV in manifest: %w", err)
	}

	// Derive the same per-backup key used during encryption
	derivedKey := crypto.DeriveKey(masterKey, salt)

	decryptOpts.BackupID = m.BackupID
	if decryptOpts.Source == "" {
		decryptOpts.Source = filePath
	}
	dec, err := crypto.NewChunkDecryptReaderWithOptions(f, derivedKey, iv, decryptOpts)
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("restore: failed to initialise decryptor: %w", err)
	}

	return dec, nil
}

// ErrHashMismatch is returned when the computed hash of a backup file does not
// match the stored hash in the manifest.  Callers should abort the restore and
// delete the corrupt backup file.
var ErrHashMismatch = errors.New("hash mismatch")

// computeHash computes the SHA-256 hex digest of the file at path.
func computeHash(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	hw := crypto.NewHashingWriter(io.Discard)
	if _, err := io.Copy(hw, f); err != nil {
		return "", err
	}
	return hw.Sum(), nil
}

func decodeManifestBytes(v string) ([]byte, error) {
	if v == "" {
		return nil, fmt.Errorf("value is empty")
	}
	if b, err := hex.DecodeString(v); err == nil {
		return b, nil
	}
	if b, err := base64.StdEncoding.DecodeString(v); err == nil {
		return b, nil
	}
	b, err := base64.URLEncoding.DecodeString(v)
	if err != nil {
		return nil, fmt.Errorf("unsupported encoding")
	}
	return b, nil
}
