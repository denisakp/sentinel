package restore

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/denisakp/sentinel/internal/crypto"
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
