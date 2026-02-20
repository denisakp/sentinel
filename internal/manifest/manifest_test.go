package manifest_test

import (
	"errors"
	"os"
	"testing"
	"time"

	"github.com/denisakp/sentinel/internal/manifest"
)

func TestWriteReadManifest_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test.manifest.json"

	orig := &manifest.BackupManifest{
		BackupID:     "backup-001",
		Database:     "prod-postgres",
		DatabaseType: "postgres",
		CreatedAt:    time.Now().UTC().Truncate(time.Second),
		SizeBytes:    1024,
		Hash: manifest.HashInfo{
			Algorithm: "sha256",
			Value:     "abc123def456",
		},
	}

	if err := manifest.WriteManifest(path, orig); err != nil {
		t.Fatalf("WriteManifest() error = %v", err)
	}

	got, err := manifest.ReadManifest(path)
	if err != nil {
		t.Fatalf("ReadManifest() error = %v", err)
	}

	if got.BackupID != orig.BackupID {
		t.Errorf("BackupID = %q, want %q", got.BackupID, orig.BackupID)
	}
	if got.Hash.Value != orig.Hash.Value {
		t.Errorf("Hash.Value = %q, want %q", got.Hash.Value, orig.Hash.Value)
	}
}

func TestReadManifest_NotFound(t *testing.T) {
	_, err := manifest.ReadManifest("/nonexistent/path.manifest.json")
	if err == nil {
		t.Fatal("expected error for missing manifest")
	}
	if !errors.Is(err, manifest.ErrNoManifest) {
		t.Errorf("expected ErrNoManifest, got %v", err)
	}
}

func TestWriteManifest_EmptyPath(t *testing.T) {
	err := manifest.WriteManifest("", &manifest.BackupManifest{})
	if err == nil {
		t.Error("expected error for empty path")
	}
}

func TestReadManifest_InvalidJSON(t *testing.T) {
	f, err := os.CreateTemp("", "bad-manifest-*.json")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(f.Name()) })
	f.WriteString("not valid json")
	f.Close()

	_, err = manifest.ReadManifest(f.Name())
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestReadManifest_MissingFields(t *testing.T) {
	f, err := os.CreateTemp("", "incomplete-manifest-*.json")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(f.Name()) })
	f.WriteString(`{"backup_id":"","database":"test"}`)
	f.Close()

	_, err = manifest.ReadManifest(f.Name())
	if err == nil {
		t.Error("expected error for missing backup_id")
	}
}

func TestWriteManifest_NilManifest(t *testing.T) {
	err := manifest.WriteManifest("/tmp/nil-manifest.json", nil)
	if err == nil {
		t.Error("expected error for nil manifest")
	}
}

func TestWriteManifest_EmptyManifestPath(t *testing.T) {
	err := manifest.WriteManifest("", &manifest.BackupManifest{BackupID: "test"})
	if err == nil {
		t.Error("expected error for empty path")
	}
}

func TestReadManifest_MissingHashValue(t *testing.T) {
	f, err := os.CreateTemp("", "missing-hash-*.json")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(f.Name()) })
	f.WriteString(`{"backup_id":"abc-001","hash":{"algorithm":"sha256","value":""}}`)
	f.Close()

	_, err = manifest.ReadManifest(f.Name())
	if err == nil {
		t.Error("expected error for empty hash value")
	}
}

func TestReadManifest_MissingHashAlgorithm(t *testing.T) {
	f, err := os.CreateTemp("", "missing-algo-*.json")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(f.Name()) })
	f.WriteString(`{"backup_id":"abc-001","hash":{"algorithm":"","value":"deadbeef"}}`)
	f.Close()

	_, err = manifest.ReadManifest(f.Name())
	if err == nil {
		t.Error("expected error for empty hash algorithm")
	}
}

func TestWriteReadManifest_WithEncryption(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/encrypted.manifest.json"

	orig := &manifest.BackupManifest{
		BackupID:     "enc-001",
		Database:     "testdb",
		DatabaseType: "postgres",
		CreatedAt:    time.Now().UTC().Truncate(time.Second),
		SizeBytes:    2048,
		Hash: manifest.HashInfo{
			Algorithm: "sha256",
			Value:     "abcdef1234567890",
		},
		Encryption: &manifest.EncryptionInfo{
			Algorithm:     "AES-256-GCM",
			KeyDerivation: "PBKDF2-HMAC-SHA256",
			Iterations:    100000,
			Salt:          "c2FsdHZhbHVlMTIzNDU2Nzg5MDEyMzQ1Ng==",
			IV:            "aXZieXRlczEyMzQ=",
			AuthTag:       "dGFnYnl0ZXMxMjM0NTY3OA==",
		},
	}

	if err := manifest.WriteManifest(path, orig); err != nil {
		t.Fatalf("WriteManifest() error = %v", err)
	}

	got, err := manifest.ReadManifest(path)
	if err != nil {
		t.Fatalf("ReadManifest() error = %v", err)
	}
	if got.Encryption == nil {
		t.Fatal("ReadManifest() encryption info is nil")
	}
	if got.Encryption.Algorithm != "AES-256-GCM" {
		t.Errorf("Encryption.Algorithm = %q, want %q", got.Encryption.Algorithm, "AES-256-GCM")
	}
	if got.Encryption.Iterations != 100000 {
		t.Errorf("Encryption.Iterations = %d, want %d", got.Encryption.Iterations, 100000)
	}
}
