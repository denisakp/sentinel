package config

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/denisakp/sentinel/internal/adapters/crypto"
)

// newSecretsKey returns a fresh base64 key (for env/config) and its raw 32 bytes
// (for EncryptSecretsFile).
func newSecretsKey(t *testing.T) (b64 string, raw []byte) {
	t.Helper()
	b64, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	raw, err = base64.StdEncoding.DecodeString(b64)
	if err != nil {
		t.Fatalf("decode key: %v", err)
	}
	return b64, raw
}

// writeEncryptedSecrets encrypts plaintext under raw and writes it to dir/name.
func writeEncryptedSecrets(t *testing.T, dir, name string, plaintext, raw []byte) string {
	t.Helper()
	enc, err := crypto.EncryptSecretsFile(plaintext, raw)
	if err != nil {
		t.Fatalf("EncryptSecretsFile: %v", err)
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, enc, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	return p
}

func TestReadSecretsFileMaybeDecrypt_Plaintext(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "plain.cnf")
	body := []byte("[client]\nuser=root\n")
	if err := os.WriteFile(p, body, 0o600); err != nil {
		t.Fatal(err)
	}

	got, _, encrypted, err := readSecretsFileMaybeDecrypt(p, &Configuration{})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if encrypted {
		t.Fatal("plaintext file reported as encrypted")
	}
	if string(got) != string(body) {
		t.Fatalf("got %q want %q", got, body)
	}
}

func TestReadSecretsFileMaybeDecrypt_DedicatedKey(t *testing.T) {
	dir := t.TempDir()
	b64, raw := newSecretsKey(t)
	body := []byte("[client]\nuser=root\npassword=s3cr3t\n")
	p := writeEncryptedSecrets(t, dir, "db.cnf.enc", body, raw)

	t.Setenv("SENTINEL_SECRETS_KEY", b64)
	cfg := &Configuration{SecretsKeyEnv: "SENTINEL_SECRETS_KEY"}

	got, _, encrypted, err := readSecretsFileMaybeDecrypt(p, cfg)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if !encrypted {
		t.Fatal("encrypted file reported as plaintext")
	}
	if string(got) != string(body) {
		t.Fatalf("got %q want %q", got, body)
	}
}

func TestReadSecretsFileMaybeDecrypt_ArtifactKeyFallback(t *testing.T) {
	dir := t.TempDir()
	b64, raw := newSecretsKey(t)
	body := []byte("uri: mongodb://user@host:27017\npassword: p\n")
	p := writeEncryptedSecrets(t, dir, "mongo.enc", body, raw)

	// Only the artifact key is configured; the secrets path must fall back to it.
	t.Setenv("SENTINEL_ENC_KEY", b64)
	cfg := &Configuration{EncryptionKeyEnv: "SENTINEL_ENC_KEY"}

	got, _, _, err := readSecretsFileMaybeDecrypt(p, cfg)
	if err != nil {
		t.Fatalf("decrypt via fallback: %v", err)
	}
	if string(got) != string(body) {
		t.Fatalf("got %q want %q", got, body)
	}
}

func TestReadSecretsFileMaybeDecrypt_NoDecryptedFileOnDisk(t *testing.T) {
	dir := t.TempDir()
	b64, raw := newSecretsKey(t)
	body := []byte("[client]\nuser=root\npassword=s3cr3t\n")
	p := writeEncryptedSecrets(t, dir, "db.cnf.enc", body, raw)

	t.Setenv("SENTINEL_SECRETS_KEY", b64)
	cfg := &Configuration{SecretsKeyEnv: "SENTINEL_SECRETS_KEY"}
	if _, _, _, err := readSecretsFileMaybeDecrypt(p, cfg); err != nil {
		t.Fatalf("decrypt: %v", err)
	}

	// The only file in dir must be the encrypted input; no plaintext copy written.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		content, _ := os.ReadFile(filepath.Join(dir, e.Name()))
		if strings.Contains(string(content), "s3cr3t") {
			t.Fatalf("decrypted plaintext leaked to disk in %s", e.Name())
		}
	}
	if len(entries) != 1 {
		t.Fatalf("expected exactly 1 file on disk, got %d", len(entries))
	}
}

func TestApplyDefaultsFile_Encrypted(t *testing.T) {
	dir := t.TempDir()
	b64, raw := newSecretsKey(t)
	body := []byte("[client]\nhost=db.example\nuser=root\npassword=s3cr3t\nport=3307\n")
	p := writeEncryptedSecrets(t, dir, "db.cnf.enc", body, raw)

	t.Setenv("SENTINEL_SECRETS_KEY", b64)
	cfg := &Configuration{SecretsKeyEnv: "SENTINEL_SECRETS_KEY"}
	job := BackupJob{Name: "mysql-prod", Type: "mysql", DefaultsFile: p}

	if err := applyDefaultsFile(&job, cfg); err != nil {
		t.Fatalf("applyDefaultsFile: %v", err)
	}
	if job.Host != "db.example" || job.Username != "root" || job.Port != 3307 {
		t.Fatalf("creds not applied from encrypted file: %+v", job)
	}
	if job.myCnfPassword != "s3cr3t" {
		t.Fatalf("password not applied: %q", job.myCnfPassword)
	}
}

func TestApplyMongoSecrets_Encrypted(t *testing.T) {
	dir := t.TempDir()
	b64, raw := newSecretsKey(t)
	body := []byte("uri: mongodb://alice@mongo.example:27017\npassword: s3cr3t\n")
	p := writeEncryptedSecrets(t, dir, "mongo.enc", body, raw)

	t.Setenv("SENTINEL_SECRETS_KEY", b64)
	cfg := &Configuration{SecretsKeyEnv: "SENTINEL_SECRETS_KEY"}
	job := BackupJob{Name: "mongo-prod", Type: "mongodb", MongoSecretsFile: p}

	if err := applyMongoSecrets(&job, cfg); err != nil {
		t.Fatalf("applyMongoSecrets: %v", err)
	}
	if !strings.Contains(job.URI, "alice:s3cr3t@mongo.example") {
		t.Fatalf("mongo uri not composed from encrypted file: %q", job.URI)
	}
}

// --- US3: failure contract ---

func TestReadSecretsFileMaybeDecrypt_WrongKey(t *testing.T) {
	dir := t.TempDir()
	_, raw := newSecretsKey(t)
	p := writeEncryptedSecrets(t, dir, "db.cnf.enc", []byte("[client]\nuser=root\n"), raw)

	otherB64, _ := newSecretsKey(t)
	t.Setenv("SENTINEL_SECRETS_KEY", otherB64)
	cfg := &Configuration{SecretsKeyEnv: "SENTINEL_SECRETS_KEY"}

	_, _, _, err := readSecretsFileMaybeDecrypt(p, cfg)
	if err == nil || !strings.Contains(err.Error(), p) {
		t.Fatalf("expected error naming file %q, got %v", p, err)
	}
	if !strings.Contains(err.Error(), "wrong key or corrupt") {
		t.Fatalf("expected wrong-key message, got %v", err)
	}
}

func TestReadSecretsFileMaybeDecrypt_NoKeyConfigured(t *testing.T) {
	dir := t.TempDir()
	_, raw := newSecretsKey(t)
	p := writeEncryptedSecrets(t, dir, "db.cnf.enc", []byte("[client]\nuser=root\n"), raw)

	_, _, encrypted, err := readSecretsFileMaybeDecrypt(p, &Configuration{})
	if err == nil {
		t.Fatal("expected error when no key configured")
	}
	if !encrypted {
		t.Fatal("expected encrypted=true even on no-key error")
	}
	if !strings.Contains(err.Error(), p) || !strings.Contains(err.Error(), "no decryption key") {
		t.Fatalf("expected clear no-key error naming file, got %v", err)
	}
}

func TestReadSecretsFileMaybeDecrypt_Corrupt(t *testing.T) {
	dir := t.TempDir()
	b64, raw := newSecretsKey(t)
	p := writeEncryptedSecrets(t, dir, "db.cnf.enc", []byte("[client]\nuser=root\n"), raw)

	// Corrupt a byte inside the payload.
	data, _ := os.ReadFile(p)
	data[len(data)-1] ^= 0xFF
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("SENTINEL_SECRETS_KEY", b64)
	cfg := &Configuration{SecretsKeyEnv: "SENTINEL_SECRETS_KEY"}
	_, _, _, err := readSecretsFileMaybeDecrypt(p, cfg)
	if err == nil || !strings.Contains(err.Error(), p) {
		t.Fatalf("expected error naming file on corrupt data, got %v", err)
	}
}

func TestReadSecretsFileMaybeDecrypt_UnsupportedVersion(t *testing.T) {
	dir := t.TempDir()
	b64, raw := newSecretsKey(t)
	p := writeEncryptedSecrets(t, dir, "db.cnf.enc", []byte("[client]\nuser=root\n"), raw)

	data, _ := os.ReadFile(p)
	data[4] = 0x02 // bump SSEC version byte
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("SENTINEL_SECRETS_KEY", b64)
	cfg := &Configuration{SecretsKeyEnv: "SENTINEL_SECRETS_KEY"}
	_, _, _, err := readSecretsFileMaybeDecrypt(p, cfg)
	if err == nil || !strings.Contains(err.Error(), p) {
		t.Fatalf("expected error naming file on unsupported version, got %v", err)
	}
}
