package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	dumppg "github.com/denisakp/sentinel/internal/adapters/dump/pg"
	"github.com/denisakp/sentinel/internal/adapters/storage"
	"github.com/denisakp/sentinel/internal/config"
	"github.com/denisakp/sentinel/internal/ports"
)

// TestResolveVerifyAfterUpload exercises the inheritance precedence: a
// per-job override always wins; otherwise the top-level
// integrity.verify_after_upload default applies; a nil Configuration (or an
// unset job pointer with no config) is off (spec 053 / PRD 40, Q5: opt-in,
// default off).
func TestResolveVerifyAfterUpload(t *testing.T) {
	trueVal, falseVal := true, false

	cases := []struct {
		name string
		cfg  *config.Configuration
		job  config.BackupJob
		want bool
	}{
		{"nil config, no override", nil, config.BackupJob{}, false},
		{"default off, no override", &config.Configuration{}, config.BackupJob{}, false},
		{"default on, no override", &config.Configuration{Integrity: config.IntegrityConfig{VerifyAfterUpload: true}}, config.BackupJob{}, true},
		{"default on, job override off", &config.Configuration{Integrity: config.IntegrityConfig{VerifyAfterUpload: true}}, config.BackupJob{VerifyAfterUpload: &falseVal}, false},
		{"default off, job override on", &config.Configuration{}, config.BackupJob{VerifyAfterUpload: &trueVal}, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveVerifyAfterUpload(tc.cfg, tc.job); got != tc.want {
				t.Fatalf("resolveVerifyAfterUpload() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestNewBackupExecutorFromConfigWiresLocalStorageForVerifyAfterUpload proves
// the factory wires a REAL local ports.StorageBackend (not the historical
// nil) when a local-storage job enables verify_after_upload, and that a
// healthy round trip (dump → hash → re-download → re-hash) succeeds through
// the production manifest_store.Adapter — an integration-level complement to
// the domain-level MockBackend tests in internal/domain/backup.
func TestNewBackupExecutorFromConfigWiresLocalStorageForVerifyAfterUpload(t *testing.T) {
	dir := t.TempDir()
	const outName = "job.sql"

	content := []byte("-- real dump content\n")
	sum := sha256.Sum256(content)
	digest := hex.EncodeToString(sum[:])

	cfg := &config.Configuration{Integrity: config.IntegrityConfig{VerifyAfterUpload: true}}
	job := config.BackupJob{Name: "local-verify", Type: "postgres"}
	storageParams := &storage.Params{StorageType: "local", LocalPath: dir, OutName: outName}
	engineOpts := &dumppg.PgDumpArgs{}
	dumps := dumpBuilderFunc(func(_ ports.BuildContext) (ports.BuildResult, error) {
		if err := os.WriteFile(filepath.Join(dir, outName), content, 0o644); err != nil {
			return ports.BuildResult{}, err
		}
		return ports.BuildResult{Digest: digest}, nil
	})

	be, err := NewBackupExecutorFromConfig(cfg, job, storageParams, false, false, engineOpts, dumps)
	if err != nil {
		t.Fatalf("NewBackupExecutorFromConfig() error = %v", err)
	}
	if !be.job.VerifyAfterUpload {
		t.Fatal("expected the resolved domain job to have VerifyAfterUpload = true")
	}

	res, err := be.exec.Run(context.Background(), be.job)
	if err != nil {
		t.Fatalf("Run() error = %v, want success (real local backend wired + hash matches)", err)
	}
	if res.HashValue != digest {
		t.Fatalf("HashValue = %q, want %q", res.HashValue, digest)
	}
}

// TestNewBackupExecutorFromConfigLocalVerifyAfterUploadFailsOnMismatch proves
// the wired verify step surfaces a real failure end-to-end (through the
// production local backend + manifest_store.Adapter, not a test double) when
// the recorded digest does not match what is actually on disk.
func TestNewBackupExecutorFromConfigLocalVerifyAfterUploadFailsOnMismatch(t *testing.T) {
	dir := t.TempDir()
	const outName = "job.sql"

	cfg := &config.Configuration{Integrity: config.IntegrityConfig{VerifyAfterUpload: true}}
	job := config.BackupJob{Name: "local-verify-bad", Type: "postgres"}
	storageParams := &storage.Params{StorageType: "local", LocalPath: dir, OutName: outName}
	engineOpts := &dumppg.PgDumpArgs{}
	dumps := dumpBuilderFunc(func(_ ports.BuildContext) (ports.BuildResult, error) {
		if err := os.WriteFile(filepath.Join(dir, outName), []byte("-- real dump content\n"), 0o644); err != nil {
			return ports.BuildResult{}, err
		}
		// Wrong digest — does not match what was actually written to disk.
		return ports.BuildResult{Digest: strings.Repeat("0", 64)}, nil
	})

	be, err := NewBackupExecutorFromConfig(cfg, job, storageParams, false, false, engineOpts, dumps)
	if err != nil {
		t.Fatalf("NewBackupExecutorFromConfig() error = %v", err)
	}

	if _, err := be.exec.Run(context.Background(), be.job); err == nil {
		t.Fatal("Run() error = nil, want verify_after_upload_failed")
	} else if !strings.Contains(err.Error(), "verify_after_upload_failed") {
		t.Fatalf("Run() error = %v, want it to contain verify_after_upload_failed", err)
	}
}
