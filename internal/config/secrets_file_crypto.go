package config

import (
	"errors"
	"fmt"
	"os"

	"github.com/denisakp/sentinel/internal/adapters/crypto"
	"github.com/denisakp/sentinel/internal/ports"
)

// readSecretsFileMaybeDecrypt reads a DB-credentials secrets file (the
// MySQL/MariaDB defaults file or the MongoDB secrets file) and, if it carries the
// SSEC container magic, decrypts it in memory before returning the plaintext
// bytes. A plaintext file is returned as-is. The decrypted plaintext is never
// written to disk (spec 059 FR-004).
//
// The decryption key is resolved from the dedicated secrets-file key reference
// when set, falling back to the backup-artifact key reference (FR-014):
//
//	env  = firstNonEmpty(cfg.SecretsKeyEnv,  cfg.EncryptionKeyEnv)
//	file = firstNonEmpty(cfg.SecretsKeyFile, cfg.EncryptionKeyFile)
//
// Every failure (missing key, wrong key, corrupt/truncated data, unsupported
// version) is returned as a single clear error naming the file (FR-006); a wrong
// key never yields a partial parse — the whole file is decrypted in memory first.
// The returned encrypted flag lets callers gate the plaintext-only permission
// warning (D6).
func readSecretsFileMaybeDecrypt(path string, cfg *Configuration) (plaintext []byte, mode os.FileMode, encrypted bool, err error) {
	info, statErr := os.Stat(path)
	if statErr != nil {
		return nil, 0, false, fmt.Errorf("cannot read secrets file '%s': %w", path, statErr)
	}

	raw, readErr := os.ReadFile(path)
	if readErr != nil {
		return nil, info.Mode(), false, fmt.Errorf("cannot read secrets file '%s': %w", path, readErr)
	}

	if !crypto.IsEncryptedSecretsFile(raw) {
		return raw, info.Mode(), false, nil
	}

	env := firstNonEmpty(cfg.SecretsKeyEnv, cfg.EncryptionKeyEnv)
	file := firstNonEmpty(cfg.SecretsKeyFile, cfg.EncryptionKeyFile)
	if env == "" && file == "" {
		return nil, info.Mode(), true, fmt.Errorf(
			"encrypted secrets file '%s' but no decryption key configured (set secrets_key_env/secrets_key_file or encryption_key_env/encryption_key_file)", path)
	}

	kp := &crypto.FileKeyProvider{EnvVar: env, FilePath: file}
	key, keyErr := kp.GetKey()
	if keyErr != nil {
		return nil, info.Mode(), true, fmt.Errorf("cannot decrypt secrets file '%s': %w", path, keyErr)
	}

	decrypted, decErr := crypto.DecryptSecretsFile(raw, key)
	if decErr != nil {
		switch {
		case errors.Is(decErr, ports.ErrAuthTagFailed):
			return nil, info.Mode(), true, fmt.Errorf("cannot decrypt secrets file '%s': wrong key or corrupt data", path)
		case errors.Is(decErr, crypto.ErrSecretsEnvelopeUnsupportedVersion):
			return nil, info.Mode(), true, fmt.Errorf("cannot decrypt secrets file '%s': %w", path, decErr)
		default:
			return nil, info.Mode(), true, fmt.Errorf("cannot decrypt secrets file '%s': corrupt or truncated envelope: %w", path, decErr)
		}
	}

	return decrypted, info.Mode(), true, nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
