package scheduler

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/denisakp/sentinel/internal/config"
	"github.com/denisakp/sentinel/internal/crypto"
	"github.com/denisakp/sentinel/internal/manifest"
	"github.com/denisakp/sentinel/internal/ports"
	"github.com/denisakp/sentinel/internal/monitor"
)

type mockBackupStorage struct{}

func (m *mockBackupStorage) ReadBackup(ctx context.Context, source string, path string) ([]byte, error) {
	return []byte("mock-backup-data"), nil
}

func schedulerTestMasterKey() []byte {
	k := make([]byte, 32)
	for i := range k {
		k[i] = byte(i + 9)
	}
	return k
}

func createEncryptedBackupForScheduler(t *testing.T, plain []byte, backupID string, masterKey []byte) (string, *ports.BackupManifest) {
	t.Helper()
	filePath := filepath.Join(t.TempDir(), "restore-target.bin")
	if err := os.WriteFile(filePath, plain, 0o644); err != nil {
		t.Fatalf("write plaintext backup: %v", err)
	}

	in, err := os.Open(filePath)
	if err != nil {
		t.Fatalf("open plaintext backup: %v", err)
	}

	salt := make([]byte, 32)
	for i := range salt {
		salt[i] = byte(i + 5)
	}
	derived := crypto.DeriveKey(masterKey, salt)

	encPath := filePath + ".enc"
	out, err := os.Create(encPath)
	if err != nil {
		in.Close()
		t.Fatalf("create encrypted output: %v", err)
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
		t.Fatalf("encrypt backup copy: %v", err)
	}
	in.Close()
	if err := enc.Flush(); err != nil {
		out.Close()
		t.Fatalf("flush encrypted backup: %v", err)
	}
	if err := out.Close(); err != nil {
		t.Fatalf("close encrypted output: %v", err)
	}
	if err := os.Rename(encPath, filePath); err != nil {
		t.Fatalf("replace encrypted output: %v", err)
	}

	info, err := os.Stat(filePath)
	if err != nil {
		t.Fatalf("stat encrypted backup: %v", err)
	}

	m := &ports.BackupManifest{
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

	if err := manifest.WriteManifest(filePath+".manifest.json", m); err != nil {
		t.Fatalf("WriteManifest() error = %v", err)
	}

	return filePath, m
}

func TestExecuteRestore_EncryptedPreflightStillPassesWithConfiguredKey(t *testing.T) {
	masterKey := schedulerTestMasterKey()
	t.Setenv("TEST_SCHED_RESTORE_KEY", base64.StdEncoding.EncodeToString(masterKey))

	backupPath, _ := createEncryptedBackupForScheduler(t, []byte("restore payload"), "sched-enc-1", masterKey)

	rsm := &RestoreScheduleManager{
		logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
		backupStorage: &mockBackupStorage{},
		cfg: &config.Configuration{
			EncryptionKeyEnv: "TEST_SCHED_RESTORE_KEY",
		},
	}

	restoreCfg := &RestoreScheduleConfig{
		Name:         "restore-opt-in-test",
		BackupPath:   backupPath,
		BackupSource: "local",
		RestoreConfig: &RestoreExecutionConfig{
			JobName:      "restore-opt-in-test",
			DatabaseType: "unsupported-db",
		},
	}

	err := rsm.executeRestore(context.Background(), restoreCfg)
	if err == nil {
		t.Fatal("executeRestore() error = nil, want unsupported database type error")
	}
	if !strings.Contains(err.Error(), "unsupported database type") {
		t.Fatalf("executeRestore() error = %q, want unsupported database type", err.Error())
	}
	if strings.Contains(err.Error(), "failed to load master key") {
		t.Fatalf("executeRestore() unexpectedly failed during encrypted preflight: %v", err)
	}
}

func TestRecordRestoreExecution_PropagatesAdvancedDefaults(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "history.db")
	mon, err := monitor.NewMonitor(dbPath)
	if err != nil {
		t.Fatalf("NewMonitor() error = %v", err)
	}
	defer mon.Close()

	rsm := &RestoreScheduleManager{
		logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		monitor: mon,
	}

	rsm.recordRestoreExecution(context.Background(), &RestoreScheduleConfig{
		Name: "restore-scheduled",
		RestoreConfig: &RestoreExecutionConfig{
			DatabaseType: "postgres",
			Database:     "app",
		},
		BackupPath: "backup.sql",
	}, time.Now().UTC(), true, 100, true, "")

	items, err := mon.ListRestoreExecutions(context.Background(), &ports.RestoreFilter{RestoreName: "restore-scheduled"}, 10, 0)
	if err != nil {
		t.Fatalf("ListRestoreExecutions() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 restore execution, got %d", len(items))
	}
	if items[0].RestoreMode != "full" {
		t.Fatalf("RestoreMode = %q, want full", items[0].RestoreMode)
	}
	if items[0].PlanningStatus != "ready" {
		t.Fatalf("PlanningStatus = %q, want ready", items[0].PlanningStatus)
	}
	if items[0].FallbackDecision != "none" {
		t.Fatalf("FallbackDecision = %q, want none", items[0].FallbackDecision)
	}
}
