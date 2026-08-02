package runtime

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/denisakp/sentinel/internal/adapters/crypto"
	"github.com/denisakp/sentinel/internal/ports"
)

func testMasterKey() []byte {
	k := make([]byte, 32)
	for i := range k {
		k[i] = byte(i + 1)
	}
	return k
}

func writePlainBackup(t *testing.T, content []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "backup.bin")
	if err := os.WriteFile(p, content, 0o644); err != nil {
		t.Fatalf("write backup: %v", err)
	}
	return p
}

func createEncryptedBackup(t *testing.T, filePath string, backupID string, masterKey []byte) *ports.BackupManifest {
	t.Helper()
	in, err := os.Open(filePath)
	if err != nil {
		t.Fatalf("open input: %v", err)
	}

	salt := make([]byte, 32)
	for i := range salt {
		salt[i] = byte(i + 3)
	}
	derived := crypto.DeriveKey(masterKey, salt)

	encPath := filePath + ".enc"
	out, err := os.Create(encPath)
	if err != nil {
		in.Close()
		t.Fatalf("create encrypted file: %v", err)
	}

	hw := crypto.NewHashingWriter(out)
	enc, err := crypto.NewChunkEncryptWriter(hw, derived, backupID)
	if err != nil {
		in.Close()
		out.Close()
		t.Fatalf("NewChunkEncryptWriter(): %v", err)
	}
	if _, err := io.Copy(enc, in); err != nil {
		in.Close()
		out.Close()
		t.Fatalf("encrypt copy: %v", err)
	}
	in.Close()
	if err := enc.Flush(); err != nil {
		out.Close()
		t.Fatalf("encrypt flush: %v", err)
	}
	if err := out.Close(); err != nil {
		t.Fatalf("close encrypted file: %v", err)
	}
	if err := os.Rename(encPath, filePath); err != nil {
		t.Fatalf("replace encrypted file: %v", err)
	}

	info, err := os.Stat(filePath)
	if err != nil {
		t.Fatalf("stat encrypted file: %v", err)
	}

	return &ports.BackupManifest{
		BackupID:     backupID,
		Database:     "db",
		DatabaseType: "postgres",
		CreatedAt:    time.Now().UTC(),
		SizeBytes:    info.Size(),
		Hash: ports.HashInfo{
			Algorithm: "sha256",
			Value:     hw.Sum(),
		},
		Encryption: &ports.EncryptionInfo{
			Algorithm:     "AES-256-GCM",
			KeyDerivation: "PBKDF2-HMAC-SHA256",
			Iterations:    100000,
			Salt:          base64.StdEncoding.EncodeToString(salt),
			IV:            hex.EncodeToString(enc.BaseNonce()),
			AuthTag:       hex.EncodeToString(enc.LastAuthTag()),
		},
	}
}

func TestPreRestoreVerifyAndDecrypt_Plaintext(t *testing.T) {
	content := []byte("plain backup payload")
	filePath := writePlainBackup(t, content)
	hash, err := computeHash(filePath)
	if err != nil {
		t.Fatalf("computeHash() error = %v", err)
	}

	m := &ports.BackupManifest{
		BackupID: "plain-1",
		Hash: ports.HashInfo{
			Algorithm: "sha256",
			Value:     hash,
		},
	}

	r, err := PreRestoreVerifyAndDecrypt(context.Background(), m, filePath, nil)
	if err != nil {
		t.Fatalf("PreRestoreVerifyAndDecrypt() error = %v", err)
	}
	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if string(got) != string(content) {
		t.Fatalf("plaintext restore payload mismatch: got %q want %q", string(got), string(content))
	}
}

func TestPreRestoreVerifyAndDecrypt_EncryptedArtifactStillRestores(t *testing.T) {
	masterKey := testMasterKey()
	t.Setenv("TEST_RESTORE_MASTER_KEY", base64.StdEncoding.EncodeToString(masterKey))

	filePath := writePlainBackup(t, []byte("encrypted backup payload"))
	m := createEncryptedBackup(t, filePath, "enc-restore-1", masterKey)

	kp := &crypto.FileKeyProvider{EnvVar: "TEST_RESTORE_MASTER_KEY"}
	r, err := PreRestoreVerifyAndDecrypt(context.Background(), m, filePath, kp)
	if err != nil {
		t.Fatalf("PreRestoreVerifyAndDecrypt() error = %v", err)
	}
	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if string(got) != "encrypted backup payload" {
		t.Fatalf("decrypted payload mismatch: got %q", string(got))
	}
}

func TestPreRestoreVerifyAndDecrypt_EncryptedWithoutKeyProviderFails(t *testing.T) {
	masterKey := testMasterKey()
	filePath := writePlainBackup(t, []byte("encrypted backup payload"))
	m := createEncryptedBackup(t, filePath, "enc-restore-2", masterKey)

	_, err := PreRestoreVerifyAndDecrypt(context.Background(), m, filePath, nil)
	if err == nil {
		t.Fatal("expected error when key provider is missing for encrypted backup")
	}
}

// runPreflightCapturing runs PreRestoreVerifyAndDecryptWithOptions while
// capturing the default slog output and os.Stderr, so tests can assert the
// --skip-hash-verify WARNING is loud and unmissable (PRD 39).
func runPreflightCapturing(t *testing.T, m *ports.BackupManifest, filePath string, kp ports.KeyProvider, opts ports.DecryptOptions) (io.Reader, error, string, string) {
	t.Helper()

	var logBuf bytes.Buffer
	prevLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(prevLogger)

	oldStderr := os.Stderr
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe(): %v", err)
	}
	os.Stderr = pw

	r, runErr := PreRestoreVerifyAndDecryptWithOptions(context.Background(), m, filePath, kp, opts)

	_ = pw.Close()
	os.Stderr = oldStderr
	stderrBytes, _ := io.ReadAll(pr)

	return r, runErr, logBuf.String(), string(stderrBytes)
}

// mismatchManifest builds a plaintext-artifact manifest whose stored hash is
// deliberately wrong, so the preflight hash compare fails.
func mismatchManifest(backupID string) *ports.BackupManifest {
	return &ports.BackupManifest{
		BackupID: backupID,
		Hash: ports.HashInfo{
			Algorithm: "sha256",
			Value:     strings.Repeat("0", 64), // never equals a real content hash
		},
	}
}

// (a) Default (flag off): a hash mismatch still aborts hard with ErrHashMismatch
// and returns no reader — the pre-PRD-39 behaviour is unchanged.
func TestPreRestoreVerifyAndDecrypt_HashMismatchAbortsByDefault(t *testing.T) {
	filePath := writePlainBackup(t, []byte("payload the manifest disagrees with"))
	m := mismatchManifest("mismatch-off")

	r, err, logs, stderr := runPreflightCapturing(t, m, filePath, nil, ports.DecryptOptions{})
	if err == nil {
		t.Fatal("expected ErrHashMismatch when --skip-hash-verify is off, got nil")
	}
	if !errors.Is(err, ErrHashMismatch) {
		t.Fatalf("err = %v, want ErrHashMismatch", err)
	}
	if r != nil {
		t.Fatalf("expected nil reader on abort, got %T", r)
	}
	if strings.Contains(logs, "skip_hash_verify") {
		t.Fatalf("unexpected skip_hash_verify event when flag off: %s", logs)
	}
	if stderr != "" {
		t.Fatalf("expected no stderr WARNING when flag off, got %q", stderr)
	}
}

// (b) Flag on: a hash mismatch is downgraded to a proceed, emitting the
// structured skip_hash_verify WARNING event AND the stderr WARNING line, and
// the plaintext artifact is still readable.
func TestPreRestoreVerifyAndDecrypt_SkipHashVerifyProceedsWithWarning(t *testing.T) {
	content := []byte("payload the manifest disagrees with")
	filePath := writePlainBackup(t, content)
	m := mismatchManifest("mismatch-on")

	r, err, logs, stderr := runPreflightCapturing(t, m, filePath, nil, ports.DecryptOptions{SkipHashVerify: true})
	if err != nil {
		t.Fatalf("expected proceed with --skip-hash-verify, got err = %v", err)
	}
	if r == nil {
		t.Fatal("expected a reader when bypassing, got nil")
	}
	got, readErr := io.ReadAll(r)
	if readErr != nil {
		t.Fatalf("ReadAll() error = %v", readErr)
	}
	if string(got) != string(content) {
		t.Fatalf("bypassed plaintext mismatch: got %q want %q", string(got), string(content))
	}

	// Structured WARNING event, at WARN level, carrying the bypass marker.
	if !strings.Contains(logs, "skip_hash_verify") {
		t.Fatalf("missing structured skip_hash_verify event: %s", logs)
	}
	if !strings.Contains(logs, `"level":"WARN"`) {
		t.Fatalf("skip_hash_verify event not emitted at WARN level: %s", logs)
	}
	// The match-path info log must NOT claim verification passed on a bypass.
	if strings.Contains(logs, "hash verification passed") {
		t.Fatalf("bypass wrongly logged 'hash verification passed': %s", logs)
	}
	// Prominent stderr line for interactive operators.
	if !strings.Contains(stderr, "skip-hash-verify") || !strings.Contains(stderr, "WARNING") {
		t.Fatalf("missing/weak stderr WARNING line: %q", stderr)
	}
}

// (c) Flag on but hashes match: identical to today — no warning, normal info
// log, and the flag is a no-op.
func TestPreRestoreVerifyAndDecrypt_SkipHashVerifyNoOpOnMatch(t *testing.T) {
	content := []byte("payload that matches its manifest")
	filePath := writePlainBackup(t, content)
	hash, err := computeHash(filePath)
	if err != nil {
		t.Fatalf("computeHash() error = %v", err)
	}
	m := &ports.BackupManifest{
		BackupID: "match-flag-on",
		Hash:     ports.HashInfo{Algorithm: "sha256", Value: hash},
	}

	r, err, logs, stderr := runPreflightCapturing(t, m, filePath, nil, ports.DecryptOptions{SkipHashVerify: true})
	if err != nil {
		t.Fatalf("unexpected error on matching hash: %v", err)
	}
	got, readErr := io.ReadAll(r)
	if readErr != nil {
		t.Fatalf("ReadAll() error = %v", readErr)
	}
	if string(got) != string(content) {
		t.Fatalf("payload mismatch: got %q want %q", string(got), string(content))
	}
	if strings.Contains(logs, "skip_hash_verify") || stderr != "" {
		t.Fatalf("flag should be a no-op on a matching hash; logs=%s stderr=%q", logs, stderr)
	}
	if !strings.Contains(logs, "hash verification passed") {
		t.Fatalf("expected normal 'hash verification passed' info log: %s", logs)
	}
}

// (d) Second gate intact: for an encrypted artifact, --skip-hash-verify silences
// the SHA-256 compare but a corrupt ciphertext still fails AES-256-GCM auth-tag
// verification during decryption.
func TestPreRestoreVerifyAndDecrypt_SkipHashVerifyStillEnforcesAuthTag(t *testing.T) {
	masterKey := testMasterKey()
	t.Setenv("TEST_RESTORE_MASTER_KEY", base64.StdEncoding.EncodeToString(masterKey))

	filePath := writePlainBackup(t, []byte("encrypted backup payload to corrupt"))
	m := createEncryptedBackup(t, filePath, "enc-corrupt-1", masterKey)

	// Corrupt the final byte of the ciphertext (part of the last chunk's auth
	// tag). This also changes the file's SHA-256, so the hash gate would abort
	// were it not bypassed.
	raw, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("read encrypted file: %v", err)
	}
	raw[len(raw)-1] ^= 0xFF
	if err := os.WriteFile(filePath, raw, 0o644); err != nil {
		t.Fatalf("write corrupted file: %v", err)
	}

	kp := &crypto.FileKeyProvider{EnvVar: "TEST_RESTORE_MASTER_KEY"}
	r, err, logs, _ := runPreflightCapturing(t, m, filePath, kp, ports.DecryptOptions{SkipHashVerify: true})
	if err != nil {
		t.Fatalf("hash gate should be bypassed; got err = %v", err)
	}
	if !strings.Contains(logs, "skip_hash_verify") {
		t.Fatalf("expected skip_hash_verify bypass event: %s", logs)
	}
	// The auth-tag failure surfaces while streaming the plaintext out.
	_, readErr := io.ReadAll(r)
	if readErr == nil {
		t.Fatal("expected AES-GCM auth-tag failure on corrupt ciphertext, got nil")
	}
	if !errors.Is(readErr, ports.ErrAuthTagFailed) {
		t.Fatalf("read error = %v, want ErrAuthTagFailed", readErr)
	}
}
