package cli

import (
	"os"
	"strings"
	"testing"

	dbprobe "github.com/denisakp/sentinel/internal/adapters/db_probe"
	dumppg "github.com/denisakp/sentinel/internal/adapters/dump/pg"
	"github.com/denisakp/sentinel/internal/adapters/storage"
	"github.com/denisakp/sentinel/internal/config"
	"github.com/denisakp/sentinel/internal/ports"
)

// TestNewBackupExecutorFromConfigFailsLoudOnEncryptedRemoteSingle is the
// security guard for spec 047 / FR-008. The auto-discovery "single" dump-all
// path (a dumpBuilderFunc) uploads to remote storage itself and cannot be
// staged in place, so its artifact cannot be encrypted before it leaves the
// host. When encryption is configured AND storage is remote, the factory MUST
// refuse rather than silently leak plaintext to the bucket.
func TestNewBackupExecutorFromConfigFailsLoudOnEncryptedRemoteSingle(t *testing.T) {
	cfg := &config.Configuration{EncryptionKeyEnv: "SENTINEL_TEST_ENC_KEY"}
	job := config.BackupJob{Name: "pg-all", Type: "postgres"}
	storageParams := &storage.Params{
		StorageType: "s3",
		AWSBucket:   "backups",
		OutName:     "pg-all.sql",
	}
	// engineOpts is a real dump-args value, but dumps is a dumpBuilderFunc: that
	// marks the non-stageable auto-discovery "single" strategy (mirrors
	// buildAllDump's postgres branch).
	engineOpts := &dumppg.PgDumpArgs{}
	dumps := dumpBuilderFunc(func(_ ports.BuildContext) (ports.BuildResult, error) {
		return ports.BuildResult{}, nil
	})

	_, err := NewBackupExecutorFromConfig(cfg, job, storageParams, false, false, engineOpts, dumps)
	if err == nil {
		t.Fatal("expected fail-loud error for an encrypted remote 'single' strategy backup, got nil")
	}
	if !strings.Contains(err.Error(), "single") {
		t.Fatalf("error must name the auto-discovery 'single' strategy, got: %v", err)
	}
}

// TestNewBackupExecutorFromConfigStagesEncryptedRemoteSingleDB is the positive
// control: a real single-DB engine Builder (not a dumpBuilderFunc) with the
// same remote + encryption config MUST NOT error — it stages the dump into a
// local dir so the domain pipeline can hash/encrypt/manifest before upload.
func TestNewBackupExecutorFromConfigStagesEncryptedRemoteSingleDB(t *testing.T) {
	cfg := &config.Configuration{EncryptionKeyEnv: "SENTINEL_TEST_ENC_KEY"}
	job := config.BackupJob{Name: "pg-single", Type: "postgres"}
	storageParams := &storage.Params{
		StorageType: "s3",
		AWSBucket:   "backups",
		OutName:     "pg-single.sql",
	}
	engineOpts := &dumppg.PgDumpArgs{}
	dumps := dumppg.NewBuilder(dbprobe.NewAdapter()) // real single-DB engine Builder → stageable

	be, err := NewBackupExecutorFromConfig(cfg, job, storageParams, false, false, engineOpts, dumps)
	if err != nil {
		t.Fatalf("expected staging (no error) for encrypted remote single-DB backup, got: %v", err)
	}
	if be.job.StagingDir == "" {
		t.Fatal("expected a staging dir to be assigned for a stageable encrypted remote backup")
	}
	t.Cleanup(func() { _ = os.RemoveAll(be.job.StagingDir) })

	// The dump's engine options were redirected to write into the staging dir
	// (local storage) rather than straight to the remote backend.
	if engineOpts.Storage == nil {
		t.Fatal("expected dump storage to be redirected to local staging, got nil")
	}
	if engineOpts.Storage.StorageType != "local" || engineOpts.Storage.LocalPath != be.job.StagingDir {
		t.Fatalf("dump was not redirected to local staging: %+v", engineOpts.Storage)
	}
}
