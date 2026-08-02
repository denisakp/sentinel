package runtime

// Pre-restore integrity verification + decryption. Relocated verbatim from
// internal/restore/pipeline.go by spec 038 Sub-PR L (crypto-adapter-coupled,
// so it lives driving-side; the domain Executor reaches it through the
// Job.Preflight hook).

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/denisakp/sentinel/internal/adapters/crypto"
	domainrestore "github.com/denisakp/sentinel/internal/domain/restore"
	"github.com/denisakp/sentinel/internal/ports"
)

// ErrHashMismatch is returned when the computed hash of a backup file does
// not match the stored hash in the manifest. Alias over the relocated
// domain sentinel.
var ErrHashMismatch = domainrestore.ErrHashMismatch

// SkipHashVerifyWarnMsg is the prominent, unmissable WARNING line written to
// stderr when a manifest hash mismatch is bypassed via the per-invocation
// --skip-hash-verify restore flag. Mirrors the operator-facing shape of the
// CLI's LegacyEnvelopeOptInWarnMsg, but lives driving-side beside its single
// emission point (preflight): the CLI package imports runtime, not the
// reverse, so the constant cannot live in internal/cli. Takes the backup ID
// and the artifact path.
const SkipHashVerifyWarnMsg = "WARNING: restoring backup %q from %q with --skip-hash-verify — the manifest SHA-256 does NOT match the artifact; integrity is unverified and you are proceeding at your own risk\n"

// PreRestoreVerifyAndDecrypt is the single entry point for restore pre-flight:
//
//  1. Reads the backup file at filePath
//  2. Verifies its SHA-256 hash matches manifest.Hash.Value
//  3. If manifest.Encryption != nil, decrypts the content via AES-256-GCM
//  4. Returns an io.Reader of the plaintext backup data
//
// Callers MUST handle the following sentinel errors:
//   - ports.ErrNoManifest   → pre-v1.1 backup, log WARN and proceed with raw file
//   - ErrHashMismatch          → corruption detected, abort and delete backup
func PreRestoreVerifyAndDecrypt(
	ctx context.Context,
	m *ports.BackupManifest,
	filePath string,
	keyProvider ports.KeyProvider,
) (io.Reader, error) {
	return PreRestoreVerifyAndDecryptWithOptions(ctx, m, filePath, keyProvider, ports.DecryptOptions{})
}

// PreRestoreVerifyAndDecryptWithOptions is the explicit form that lets callers pass
// envelope-version policy (e.g. AllowLegacy for pre-v2 artifacts).
func PreRestoreVerifyAndDecryptWithOptions(
	ctx context.Context,
	m *ports.BackupManifest,
	filePath string,
	keyProvider ports.KeyProvider,
	decryptOpts ports.DecryptOptions,
) (io.Reader, error) {
	// Verify hash
	computed, err := computeHash(filePath)
	if err != nil {
		return nil, fmt.Errorf("restore: failed to compute hash of %q: %w", filePath, err)
	}

	if computed != m.Hash.Value {
		if !decryptOpts.SkipHashVerify {
			return nil, fmt.Errorf("%w: stored=%s computed=%s file=%q",
				ErrHashMismatch, m.Hash.Value, computed, filePath)
		}
		// Explicit, per-invocation operator override via --skip-hash-verify.
		// Never silent: emit both a structured security event and a prominent
		// stderr line. For encrypted artifacts this only silences the SHA-256
		// compare — AES-256-GCM auth-tag verification below is an independent
		// integrity gate this flag does NOT bypass, so a truly corrupt
		// ciphertext still fails to decrypt.
		slog.WarnContext(ctx, "SECURITY WARNING: manifest hash mismatch bypassed via --skip-hash-verify",
			"event", "skip_hash_verify",
			"backup_id", m.BackupID,
			"stored", m.Hash.Value,
			"computed", computed,
			"file", filePath,
			"encrypted", m.Encryption != nil)
		fmt.Fprintf(os.Stderr, SkipHashVerifyWarnMsg, m.BackupID, filePath)
	} else {
		slog.InfoContext(ctx, "hash verification passed",
			"backup_id", m.BackupID,
			"hash", computed)
	}

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
