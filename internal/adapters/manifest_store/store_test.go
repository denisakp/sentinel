package manifest_store_test

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"testing"
	"time"

	manifest "github.com/denisakp/sentinel/internal/adapters/manifest_store"
	"github.com/denisakp/sentinel/internal/ports"
)

func TestWriteReadManifest_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test.manifest.json"

	orig := &ports.BackupManifest{
		BackupID:     "backup-001",
		Database:     "prod-postgres",
		DatabaseType: "postgres",
		CreatedAt:    time.Now().UTC().Truncate(time.Second),
		SizeBytes:    1024,
		Hash: ports.HashInfo{
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
	if !errors.Is(err, ports.ErrNoManifest) {
		t.Errorf("expected ErrNoManifest, got %v", err)
	}
}

func TestWriteManifest_EmptyPath(t *testing.T) {
	err := manifest.WriteManifest("", &ports.BackupManifest{})
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
	err := manifest.WriteManifest("", &ports.BackupManifest{BackupID: "test"})
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

	orig := &ports.BackupManifest{
		BackupID:     "enc-001",
		Database:     "testdb",
		DatabaseType: "postgres",
		CreatedAt:    time.Now().UTC().Truncate(time.Second),
		SizeBytes:    2048,
		Hash: ports.HashInfo{
			Algorithm: "sha256",
			Value:     "abcdef1234567890",
		},
		Encryption: &ports.EncryptionInfo{
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

func TestWriteReadManifest_WithAdvancedRestoreMetadata(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/advanced.manifest.json"
	start := time.Date(2026, 3, 20, 20, 0, 0, 0, time.UTC)
	end := time.Date(2026, 3, 20, 23, 59, 0, 0, time.UTC)

	orig := &ports.BackupManifest{
		BackupID:     "adv-001",
		Database:     "appdb",
		DatabaseType: "postgres",
		CreatedAt:    time.Now().UTC().Truncate(time.Second),
		SizeBytes:    4096,
		Hash:         ports.HashInfo{Algorithm: "sha256", Value: "feedbeef"},
		AdvancedRestore: &ports.AdvancedRestoreMetadata{
			Capabilities:                  []string{"full", "pitr", "incremental"},
			InitialReleaseSupported:       true,
			RecoverableWindowStartUTC:     &start,
			RecoverableWindowEndUTC:       &end,
			BaseBackupKind:                "physical",
			RequiresIntegrityVerification: true,
			PostgresRecovery: &ports.PostgresRecoveryMetadata{
				TimelineID:         "1",
				WALStartLSN:        "0/1000000",
				WALEndLSN:          "0/2000000",
				BackupStartTimeUTC: start,
				BackupEndTimeUTC:   end,
				WALArchivePrefix:   "wal/archive/prefix",
			},
			IncrementalLineage: &ports.IncrementalLineageMetadata{
				BaselineBackupID:            "base-001",
				RequiredBackupIDs:           []string{"base-001", "delta-001"},
				CompatibleTargetFingerprint: "fp-123",
				ExecutionSupported:          false,
			},
		},
	}

	if err := manifest.WriteManifest(path, orig); err != nil {
		t.Fatalf("WriteManifest() error = %v", err)
	}

	got, err := manifest.ReadManifest(path)
	if err != nil {
		t.Fatalf("ReadManifest() error = %v", err)
	}
	if got.AdvancedRestore == nil {
		t.Fatal("AdvancedRestore is nil")
	}
	if got.AdvancedRestore.PostgresRecovery == nil {
		t.Fatal("PostgresRecovery is nil")
	}
	if got.AdvancedRestore.PostgresRecovery.WALArchivePrefix != "wal/archive/prefix" {
		t.Fatalf("WALArchivePrefix = %q", got.AdvancedRestore.PostgresRecovery.WALArchivePrefix)
	}
	if got.AdvancedRestore.IncrementalLineage == nil {
		t.Fatal("IncrementalLineage is nil")
	}
	if got.AdvancedRestore.IncrementalLineage.BaselineBackupID != "base-001" {
		t.Fatalf("BaselineBackupID = %q", got.AdvancedRestore.IncrementalLineage.BaselineBackupID)
	}
}

func TestVerifyBackupHash(t *testing.T) {
	path := t.TempDir() + "/backup.dump"
	content := []byte("incremental-payload")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("write backup file: %v", err)
	}

	sum := sha256.Sum256(content)
	expected := hex.EncodeToString(sum[:])

	if err := manifest.VerifyBackupHash(path, "sha256", expected); err != nil {
		t.Fatalf("VerifyBackupHash() unexpected error = %v", err)
	}
}

func TestVerifyBackupHash_Mismatch(t *testing.T) {
	path := t.TempDir() + "/backup.dump"
	if err := os.WriteFile(path, []byte("payload"), 0o644); err != nil {
		t.Fatalf("write backup file: %v", err)
	}

	err := manifest.VerifyBackupHash(path, "sha256", "deadbeef")
	if err == nil {
		t.Fatal("expected mismatch error")
	}
}

func TestVerifyBackupHash_UnsupportedAlgorithm(t *testing.T) {
	path := t.TempDir() + "/backup.dump"
	if err := os.WriteFile(path, []byte("payload"), 0o644); err != nil {
		t.Fatalf("write backup file: %v", err)
	}

	err := manifest.VerifyBackupHash(path, "sha1", "deadbeef")
	if err == nil {
		t.Fatal("expected unsupported algorithm error")
	}
}
