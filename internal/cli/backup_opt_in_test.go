package cli

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/denisakp/sentinel/internal/config"
	"github.com/denisakp/sentinel/internal/manifest"
	"github.com/denisakp/sentinel/internal/storage"
)

func writeBackupFixture(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "backup.sql")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write backup fixture: %v", err)
	}
	return path
}

func validBase64Key() string {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	return base64.StdEncoding.EncodeToString(key)
}

func TestApplyBackupSecurity_PlaintextByDefaultWithAmbientKey(t *testing.T) {
	t.Setenv("SENTINEL_MASTER_KEY", validBase64Key())

	backupPath := writeBackupFixture(t, "-- plaintext backup\n")
	params := &storage.Params{StorageType: "local", OutName: backupPath}
	job := config.BackupJob{Name: "plain-job", Type: "postgres", Database: "app"}

	result, err := applyBackupSecurity(&config.Configuration{}, job, params)
	if err != nil {
		t.Fatalf("applyBackupSecurity() error = %v", err)
	}
	if result == nil {
		t.Fatal("applyBackupSecurity() result = nil")
	}
	if result.encrypted {
		t.Fatalf("result.encrypted = true, want false")
	}
	if result.keyHint != "" {
		t.Fatalf("result.keyHint = %q, want empty", result.keyHint)
	}
	if result.manifestPath == "" {
		t.Fatal("result.manifestPath is empty")
	}

	m, err := manifest.ReadManifest(result.manifestPath)
	if err != nil {
		t.Fatalf("ReadManifest() error = %v", err)
	}
	if m.Encryption != nil {
		t.Fatal("manifest encryption should be nil for plaintext backups")
	}
}

func TestApplyBackupSecurity_ExplicitEncryptionSuccess(t *testing.T) {
	t.Setenv("TEST_SENTINEL_MASTER_KEY", validBase64Key())

	backupPath := writeBackupFixture(t, "-- backup to encrypt\n")
	params := &storage.Params{StorageType: "local", OutName: backupPath}
	job := config.BackupJob{Name: "enc-job", Type: "postgres", Database: "app"}
	cfg := &config.Configuration{EncryptionKeyEnv: "TEST_SENTINEL_MASTER_KEY"}

	result, err := applyBackupSecurity(cfg, job, params)
	if err != nil {
		t.Fatalf("applyBackupSecurity() error = %v", err)
	}
	if result == nil {
		t.Fatal("applyBackupSecurity() result = nil")
	}
	if !result.encrypted {
		t.Fatal("result.encrypted = false, want true")
	}
	if result.keyHint != "TEST_SENTINEL_MASTER_KEY" {
		t.Fatalf("result.keyHint = %q, want TEST_SENTINEL_MASTER_KEY", result.keyHint)
	}
	if result.manifestPath == "" {
		t.Fatal("result.manifestPath is empty")
	}

	m, err := manifest.ReadManifest(result.manifestPath)
	if err != nil {
		t.Fatalf("ReadManifest() error = %v", err)
	}
	if m.Encryption == nil {
		t.Fatal("manifest encryption metadata is nil")
	}
}

func TestApplyBackupSecurity_ExplicitEncryptionFailureIsFatal(t *testing.T) {
	backupPath := writeBackupFixture(t, "-- backup should fail encryption\n")
	params := &storage.Params{StorageType: "local", OutName: backupPath}
	job := config.BackupJob{Name: "enc-fail-job", Type: "postgres", Database: "app"}
	cfg := &config.Configuration{EncryptionKeyEnv: "MISSING_SENTINEL_KEY"}

	result, err := applyBackupSecurity(cfg, job, params)
	if err == nil {
		t.Fatal("applyBackupSecurity() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "failed to encrypt backup") {
		t.Fatalf("error = %q, want to contain %q", err.Error(), "failed to encrypt backup")
	}
	if result != nil {
		t.Fatalf("result = %#v, want nil", result)
	}
	if _, statErr := os.Stat(backupPath + ".manifest.json"); statErr == nil {
		t.Fatal("unexpected manifest created for failed encryption run")
	}
}

func TestApplyBackupSecurity_ImperativeFlowUnaffected(t *testing.T) {
	t.Setenv("SENTINEL_MASTER_KEY", validBase64Key())

	backupPath := writeBackupFixture(t, "-- imperative backup\n")
	params := &storage.Params{StorageType: "local", OutName: backupPath}
	job := config.BackupJob{Name: "imperative-job", Type: "postgres", Database: "app"}

	result, err := applyBackupSecurity(nil, job, params)
	if err != nil {
		t.Fatalf("applyBackupSecurity() error = %v", err)
	}
	if result == nil {
		t.Fatal("applyBackupSecurity() result = nil")
	}
	if result.encrypted {
		t.Fatal("result.encrypted = true, want false")
	}
}
