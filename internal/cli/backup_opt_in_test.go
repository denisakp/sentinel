package cli

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/denisakp/sentinel/internal/config"
	manifest "github.com/denisakp/sentinel/internal/adapters/manifest_store"
	"github.com/denisakp/sentinel/internal/adapters/monitor"
	"github.com/denisakp/sentinel/internal/ports"
	"github.com/denisakp/sentinel/internal/adapters/storage"
)

func writeBackupFixture(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "backup.sql")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write backup fixture: %v", err)
	}
	return path
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
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

	content := "-- plaintext backup\n"
	backupPath := writeBackupFixture(t, content)
	params := &storage.Params{StorageType: "local", OutName: backupPath}
	job := config.BackupJob{Name: "plain-job", Type: "postgres", Database: "app"}

	result, err := applyBackupSecurity(&config.Configuration{}, job, params, false, sha256Hex([]byte(content)))
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

	content := "-- backup to encrypt\n"
	backupPath := writeBackupFixture(t, content)
	params := &storage.Params{StorageType: "local", OutName: backupPath}
	job := config.BackupJob{Name: "enc-job", Type: "postgres", Database: "app"}
	cfg := &config.Configuration{EncryptionKeyEnv: "TEST_SENTINEL_MASTER_KEY"}

	result, err := applyBackupSecurity(cfg, job, params, false, sha256Hex([]byte(content)))
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
	content := "-- backup should fail encryption\n"
	backupPath := writeBackupFixture(t, content)
	params := &storage.Params{StorageType: "local", OutName: backupPath}
	job := config.BackupJob{Name: "enc-fail-job", Type: "postgres", Database: "app"}
	cfg := &config.Configuration{EncryptionKeyEnv: "MISSING_SENTINEL_KEY"}

	result, err := applyBackupSecurity(cfg, job, params, false, sha256Hex([]byte(content)))
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

	content := "-- imperative backup\n"
	backupPath := writeBackupFixture(t, content)
	params := &storage.Params{StorageType: "local", OutName: backupPath}
	job := config.BackupJob{Name: "imperative-job", Type: "postgres", Database: "app"}

	result, err := applyBackupSecurity(nil, job, params, false, sha256Hex([]byte(content)))
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

func TestApplyBackupSecurity_IncrementalHashVerificationFailureIsFatal(t *testing.T) {
	prevVerify := verifyIncrementalArtifactHash
	verifyIncrementalArtifactHash = func(path, algorithm, expected string) error {
		return fmt.Errorf("hash mismatch")
	}
	t.Cleanup(func() {
		verifyIncrementalArtifactHash = prevVerify
	})

	content := "-- incremental backup payload\n"
	backupPath := writeBackupFixture(t, content)
	params := &storage.Params{StorageType: "local", OutName: backupPath}

	historyPath := filepath.Join(t.TempDir(), "history.db")
	mon, err := monitor.NewMonitor(historyPath)
	if err != nil {
		t.Fatalf("NewMonitor() error = %v", err)
	}
	t.Cleanup(func() { _ = mon.Close() })

	now := time.Now().UTC()
	if err := mon.RecordExecution(context.Background(), &ports.Execution{
		ID:           "prev-full-1",
		BackupName:   "inc-job",
		DatabaseType: "postgres",
		Timestamp:    now,
		DurationMs:   1,
		Status:       ports.StatusCompleted,
		BackupType:   "full",
		ChainID:      "chain-1",
		ChainIndex:   0,
		FilePath:     "baseline.dump",
		CreatedAt:    now,
	}); err != nil {
		t.Fatalf("RecordExecution() error = %v", err)
	}

	job := config.BackupJob{
		Name:     "inc-job",
		Type:     "postgres",
		Database: "app",
		IncrementalBackup: &config.IncrementalBackupConfig{
			Enabled: true,
		},
	}

	result, err := applyBackupSecurity(&config.Configuration{HistoryDBPath: historyPath}, job, params, false, sha256Hex([]byte(content)))
	if err == nil {
		t.Fatal("applyBackupSecurity() error = nil, want hash verification error")
	}
	if !strings.Contains(err.Error(), "failed to verify incremental artifact hash") {
		t.Fatalf("error = %q, want to contain incremental hash verification context", err.Error())
	}
	if result != nil {
		t.Fatalf("result = %#v, want nil", result)
	}
	if _, statErr := os.Stat(backupPath + ".manifest.json"); statErr == nil {
		t.Fatal("unexpected manifest created after failed incremental hash verification")
	}
}
