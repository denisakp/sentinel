package crypto

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"os"
	"strings"

	"golang.org/x/crypto/pbkdf2"
)

// KeyProvider abstracts master key loading. Enables future KMS integration.
type KeyProvider interface {
	GetKey() ([]byte, error)
}

// FileKeyProvider reads the master encryption key from an environment variable
// or a file. The key must be a base64-encoded 32-byte value.
type FileKeyProvider struct {
	EnvVar   string
	FilePath string
}

// GetKey returns the 32-byte AES-256 master key.
// It prefers the environment variable over the file path.
func (p *FileKeyProvider) GetKey() ([]byte, error) {
	var raw string

	if p.EnvVar != "" {
		raw = os.Getenv(p.EnvVar)
	}

	if raw == "" && p.FilePath != "" {
		data, err := os.ReadFile(p.FilePath)
		if err != nil {
			return nil, fmt.Errorf("crypto: failed to read key file %q: %w", p.FilePath, err)
		}
		raw = strings.TrimSpace(string(data))
	}

	if raw == "" {
		if p.EnvVar != "" {
			return nil, fmt.Errorf("crypto: no master key found (set %s or configure encryption_key_file)", p.EnvVar)
		}
		return nil, fmt.Errorf("crypto: no master key found (configure encryption_key_env or encryption_key_file)")
	}

	keyBytes, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		// Try URL-safe base64 as fallback
		keyBytes, err = base64.URLEncoding.DecodeString(raw)
		if err != nil {
			return nil, fmt.Errorf("crypto: master key is not valid base64: %w", err)
		}
	}

	if len(keyBytes) != 32 {
		return nil, fmt.Errorf("crypto: master key must be 32 bytes (got %d after base64 decode)", len(keyBytes))
	}

	return keyBytes, nil
}

// GenerateKey generates a new random 32-byte key and returns it base64-encoded.
func GenerateKey() (string, error) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return "", fmt.Errorf("crypto: failed to generate random key: %w", err)
	}
	return base64.StdEncoding.EncodeToString(key), nil
}

// DeriveKey derives a 32-byte encryption key from masterKey and salt using
// PBKDF2-HMAC-SHA256 with 100,000 iterations.
func DeriveKey(masterKey, salt []byte) []byte {
	return pbkdf2.Key(masterKey, salt, 100_000, 32, sha256.New)
}

// GenerateSalt generates a cryptographically random 32-byte salt.
func GenerateSalt() ([]byte, error) {
	salt := make([]byte, 32)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("crypto: failed to generate salt: %w", err)
	}
	return salt, nil
}
