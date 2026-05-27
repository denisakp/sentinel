package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/denisakp/sentinel/internal/config"
	"github.com/denisakp/sentinel/internal/manifest"
	"github.com/denisakp/sentinel/internal/adapters/storage"
)

// US1 (T025): applyBackupSecurity records the supplied plaintext digest in the
// manifest without re-reading the artefact bytes.
func TestApplyBackupSecurity_UsesProvidedPlaintextDigest(t *testing.T) {
	content := "-- some payload bytes\n"
	backupPath := writeBackupFixture(t, content)
	params := &storage.Params{StorageType: "local", OutName: backupPath}
	job := config.BackupJob{Name: "digest-job", Type: "postgres", Database: "app"}

	// Pass a sentinel digest that is NOT the SHA-256 of the file content: this
	// proves the orchestrator stores the supplied digest verbatim and does NOT
	// re-hash the file.
	const sentinel = "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"

	result, err := applyBackupSecurity(&config.Configuration{}, job, params, false, sentinel)
	if err != nil {
		t.Fatalf("applyBackupSecurity: %v", err)
	}
	if result == nil || result.hashValue != sentinel {
		t.Fatalf("hashValue=%q want %q", result.hashValue, sentinel)
	}

	m, err := manifest.ReadManifest(result.manifestPath)
	if err != nil {
		t.Fatalf("ReadManifest: %v", err)
	}
	if m.Hash.Value != sentinel {
		t.Fatalf("manifest hash.value=%q want %q", m.Hash.Value, sentinel)
	}
	if m.Hash.PlaintextValue != sentinel {
		t.Fatalf("manifest hash.plaintext_value=%q want %q", m.Hash.PlaintextValue, sentinel)
	}
}

// US1 (T026): empty digest + non-local artefact yields (nil, nil) (pre-existing
// early-return preserved).
func TestApplyBackupSecurity_EmptyDigestNonLocalNoOp(t *testing.T) {
	params := &storage.Params{StorageType: "s3", OutName: "remote.archive"}
	job := config.BackupJob{Name: "remote-job", Type: "mongodb"}

	result, err := applyBackupSecurity(&config.Configuration{}, job, params, false, "")
	if err != nil {
		t.Fatalf("applyBackupSecurity: %v", err)
	}
	if result != nil {
		t.Fatalf("result=%#v want nil", result)
	}
}

// US2 (T029): encrypted path overwrites hash.value with ciphertext digest while
// hash.plaintext_value retains the supplied plaintextDigest.
func TestApplyBackupSecurity_EncryptedPreservesPlaintextDigest(t *testing.T) {
	t.Setenv("TEST_SENTINEL_MASTER_KEY", validBase64Key())

	content := "-- plaintext to be encrypted\n"
	backupPath := writeBackupFixture(t, content)
	params := &storage.Params{StorageType: "local", OutName: backupPath}
	job := config.BackupJob{Name: "enc-digest-job", Type: "postgres", Database: "app"}
	cfg := &config.Configuration{EncryptionKeyEnv: "TEST_SENTINEL_MASTER_KEY"}

	plaintextDigest := sha256Hex([]byte(content))
	result, err := applyBackupSecurity(cfg, job, params, false, plaintextDigest)
	if err != nil {
		t.Fatalf("applyBackupSecurity: %v", err)
	}
	if !result.encrypted {
		t.Fatalf("result.encrypted=false want true")
	}
	if result.hashValue == plaintextDigest {
		t.Fatalf("ciphertext hash should not equal plaintext digest")
	}

	// The on-disk artefact is now ciphertext; its SHA-256 must match
	// result.hashValue (encryption branch's inline HashingWriter result).
	encBytes, err := os.ReadFile(backupPath)
	if err != nil {
		t.Fatalf("read encrypted: %v", err)
	}
	sum := sha256.Sum256(encBytes)
	if got := hex.EncodeToString(sum[:]); got != result.hashValue {
		t.Fatalf("ciphertext digest mismatch: file=%s result=%s", got, result.hashValue)
	}

	m, err := manifest.ReadManifest(result.manifestPath)
	if err != nil {
		t.Fatalf("ReadManifest: %v", err)
	}
	if m.Hash.Value != result.hashValue {
		t.Fatalf("manifest hash.value=%q want %q", m.Hash.Value, result.hashValue)
	}
	if m.Hash.PlaintextValue != plaintextDigest {
		t.Fatalf("manifest hash.plaintext_value=%q want %q", m.Hash.PlaintextValue, plaintextDigest)
	}
}

// US3 (T033): verify-time round trip — the digest the producer emits matches
// what manifest.VerifyBackupHash recomputes from on-disk bytes.
func TestVerifyBackupHash_RoundTripsProducerDigest(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "artefact.bin")
	payload := []byte("payload that the producer hashed inline\n")
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	digest := sha256Hex(payload)
	if err := manifest.VerifyBackupHash(path, "sha256", digest); err != nil {
		t.Fatalf("VerifyBackupHash: %v", err)
	}
}
