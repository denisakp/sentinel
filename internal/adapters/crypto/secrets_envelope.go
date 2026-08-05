package crypto

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"github.com/denisakp/sentinel/internal/ports"
)

// SSEC is the self-contained container for an encrypted DB-credentials secrets
// file (the MySQL/MariaDB defaults file or the MongoDB secrets file).
//
// Unlike a backup artifact — whose base nonce lives in a manifest sidecar — a
// standalone secrets file has no companion metadata, so the container embeds the
// nonce in a small header ahead of an otherwise-untouched standard SENC v2 chunk
// stream:
//
//	SSEC (4 bytes) || version 0x01 (1 byte) || base nonce (12 bytes) || <SENC v2 stream>
//
// The distinct SSEC magic keeps a secrets file unambiguous from a backup
// artifact (SENC). The payload is produced/consumed by the shipped
// ChunkEncryptWriter / ChunkDecryptReader verbatim — this file adds only framing,
// no new cipher, key format, or generator (spec 059 FR-008).
var secretsMagic = [4]byte{'S', 'S', 'E', 'C'}

const (
	secretsVersionV1 uint8 = 0x01
	secretsNonceSize       = 12 // AES-GCM standard nonce size
	secretsPrefixSize      = 4 + 1 + secretsNonceSize
	// secretsAAD is the fixed Additional Authenticated Data for the secrets-file
	// container. A secrets file has no backup ID; a constant binds the ciphertext
	// to this purpose/version (domain separation) and must match on decrypt.
	secretsAAD = "sentinel-secrets-file/v1"
)

// ErrSecretsEnvelopeCorrupt indicates the SSEC container is truncated or does not
// begin with the SSEC magic.
var ErrSecretsEnvelopeCorrupt = errors.New("crypto: secrets file is not a valid SSEC container (corrupt or truncated)")

// ErrSecretsEnvelopeUnsupportedVersion indicates an SSEC container carries an
// unrecognized version byte.
var ErrSecretsEnvelopeUnsupportedVersion = errors.New("crypto: unsupported secrets-file envelope version")

// IsEncryptedSecretsFile reports whether data begins with the SSEC magic and is
// therefore an encrypted secrets-file container. A plaintext credentials file
// (my.cnf or secrets YAML) never starts with these bytes, so detection is
// content-based and requires no config flag.
func IsEncryptedSecretsFile(data []byte) bool {
	return len(data) >= 4 &&
		data[0] == secretsMagic[0] && data[1] == secretsMagic[1] &&
		data[2] == secretsMagic[2] && data[3] == secretsMagic[3]
}

// EncryptSecretsFile encrypts plaintext under key (32 bytes) into a self-contained
// SSEC container. The key is used directly (no PBKDF2/salt); AAD is a fixed
// constant. The returned bytes are the whole on-disk file.
func EncryptSecretsFile(plaintext, key []byte) ([]byte, error) {
	var payload bytes.Buffer
	w, err := NewChunkEncryptWriter(&payload, key, secretsAAD)
	if err != nil {
		return nil, err
	}
	if _, err := w.Write(plaintext); err != nil {
		return nil, fmt.Errorf("crypto: failed to encrypt secrets file: %w", err)
	}
	if err := w.Flush(); err != nil {
		return nil, fmt.Errorf("crypto: failed to finalize secrets file: %w", err)
	}

	nonce := w.BaseNonce()
	if len(nonce) != secretsNonceSize {
		return nil, fmt.Errorf("crypto: unexpected base nonce size %d (want %d)", len(nonce), secretsNonceSize)
	}

	out := make([]byte, 0, secretsPrefixSize+payload.Len())
	out = append(out, secretsMagic[0], secretsMagic[1], secretsMagic[2], secretsMagic[3])
	out = append(out, secretsVersionV1)
	out = append(out, nonce...)
	out = append(out, payload.Bytes()...)
	return out, nil
}

// DecryptSecretsFile validates the SSEC header, extracts the embedded base nonce,
// and decrypts the SENC payload in memory, returning the plaintext bytes. A wrong
// key or tampered ciphertext surfaces as ports.ErrAuthTagFailed; a bad magic /
// truncation as ErrSecretsEnvelopeCorrupt; an unknown version as
// ErrSecretsEnvelopeUnsupportedVersion. Because the whole (small) file is
// decrypted before returning, a wrong key never yields a partial result.
func DecryptSecretsFile(data, key []byte) ([]byte, error) {
	if len(data) < secretsPrefixSize || !IsEncryptedSecretsFile(data) {
		return nil, ErrSecretsEnvelopeCorrupt
	}
	if data[4] != secretsVersionV1 {
		return nil, fmt.Errorf("%w 0x%02X", ErrSecretsEnvelopeUnsupportedVersion, data[4])
	}

	nonce := data[5:secretsPrefixSize]
	payload := data[secretsPrefixSize:]

	r, err := NewChunkDecryptReaderWithOptions(bytes.NewReader(payload), key, nonce,
		ports.DecryptOptions{BackupID: secretsAAD, Source: "secrets-file"})
	if err != nil {
		return nil, err
	}

	plaintext, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("crypto: failed to decrypt secrets file: %w", err)
	}
	return plaintext, nil
}
