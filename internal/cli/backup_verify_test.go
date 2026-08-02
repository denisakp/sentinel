package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/denisakp/sentinel/internal/adapters/monitor"
	"github.com/denisakp/sentinel/internal/adapters/storage"
	"github.com/denisakp/sentinel/internal/ports"
	"github.com/denisakp/sentinel/internal/ports/storagetesting"
)

// swapVerifyBackend points newVerifyBackend at a fixed fake backend for the
// duration of a test, restoring the registry seam afterwards.
func swapVerifyBackend(t *testing.T, backend ports.StorageBackend) {
	t.Helper()
	prev := newVerifyBackend
	newVerifyBackend = func(_ *storage.BackendParams) (ports.StorageBackend, error) {
		return backend, nil
	}
	t.Cleanup(func() { newVerifyBackend = prev })
}

// seedRemoteExecution writes a config + history DB with one recorded s3 backup
// execution and returns the config path and the generated backup ID.
func seedRemoteExecution(t *testing.T, filePath string, size int64) (string, string) {
	t.Helper()
	dir := t.TempDir()
	historyPath := filepath.Join(dir, "history.db")
	cfgPath := writeYAML(t, dir, "history_db_path: "+historyPath+`
databases:
  remote-job:
    type: postgres
    storage:
      type: s3
      s3_bucket: test-bucket
`)

	mon, err := monitor.NewMonitor(historyPath)
	if err != nil {
		t.Fatalf("new monitor: %v", err)
	}
	exec := &ports.Execution{
		BackupName:     "remote-job",
		DatabaseType:   "postgres",
		Timestamp:      time.Now().UTC(),
		Status:         "success",
		StorageBackend: "s3",
		FilePath:       filePath,
		FileSizeBytes:  size,
	}
	if err := mon.RecordExecution(context.Background(), exec); err != nil {
		_ = mon.Close()
		t.Fatalf("record execution: %v", err)
	}
	_ = mon.Close()
	return cfgPath, exec.ID
}

// TestBackupVerifyRemoteFetchValidates asserts that verify DOWNLOADS a remote
// backup + its .manifest.json sidecar and validates the integrity hash, instead
// of failing to os.Open the remote key and reporting "no manifest".
func TestBackupVerifyRemoteFetchValidates(t *testing.T) {
	artifact := []byte("-- remote dump payload\n")
	sum := sha256.Sum256(artifact)
	hashValue := hex.EncodeToString(sum[:])

	manifestJSON, err := json.Marshal(ports.BackupManifest{
		BackupID: "remote-job",
		Hash: ports.HashInfo{
			Algorithm: "sha256",
			Value:     hashValue,
		},
	})
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}

	mb := storagetesting.NewMockBackend()
	mb.PutBytes("backup.sql", artifact)
	mb.PutBytes("backup.sql.manifest.json", manifestJSON)
	swapVerifyBackend(t, mb)

	cfgPath, backupID := seedRemoteExecution(t, "backup.sql", int64(len(artifact)))

	if err := runVerify(t, cfgPath, backupID); err != nil {
		t.Fatalf("verify of a valid remote backup should PASS, got: %v", err)
	}
}

// TestBackupVerifyRemoteFetchMissingManifestSkips asserts that a remote backup
// WITHOUT a manifest sidecar is tolerated: the artifact is downloaded, the
// sidecar is absent, and verify reports the existing "skipped" outcome rather
// than hard-failing.
func TestBackupVerifyRemoteFetchMissingManifestSkips(t *testing.T) {
	artifact := []byte("-- remote dump payload, no sidecar\n")

	mb := storagetesting.NewMockBackend()
	mb.PutBytes("backup.sql", artifact) // no .manifest.json seeded
	swapVerifyBackend(t, mb)

	cfgPath, backupID := seedRemoteExecution(t, "backup.sql", int64(len(artifact)))

	err := runVerify(t, cfgPath, backupID)
	if !errors.Is(err, ErrVerifySkipped) {
		t.Fatalf("remote backup with no manifest should be skipped, got: %v", err)
	}
}

// TestBackupVerifyRemoteFetchDetectsTampering asserts the integrity guarantee:
// when the downloaded remote artifact does not match the manifest hash, verify
// FAILS (does not silently pass).
func TestBackupVerifyRemoteFetchDetectsTampering(t *testing.T) {
	artifact := []byte("-- tampered remote payload\n")

	manifestJSON, err := json.Marshal(ports.BackupManifest{
		BackupID: "remote-job",
		Hash: ports.HashInfo{
			Algorithm: "sha256",
			Value:     "0000000000000000000000000000000000000000000000000000000000000000",
		},
	})
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}

	mb := storagetesting.NewMockBackend()
	mb.PutBytes("backup.sql", artifact)
	mb.PutBytes("backup.sql.manifest.json", manifestJSON)
	swapVerifyBackend(t, mb)

	cfgPath, backupID := seedRemoteExecution(t, "backup.sql", int64(len(artifact)))

	if err := runVerify(t, cfgPath, backupID); err == nil {
		t.Fatal("verify must FAIL on a hash mismatch for a remote backup, got nil")
	}
}
