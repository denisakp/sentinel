package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/denisakp/sentinel/internal/config"
	"github.com/denisakp/sentinel/internal/storage/gcs"
)

type fakeRestoreGCSDownloader struct {
	downloadErr error
	lastSrc     string
	lastDest    string
}

func (f *fakeRestoreGCSDownloader) Download(_ context.Context, src, dest string) error {
	f.lastSrc = src
	f.lastDest = dest
	if f.downloadErr != nil {
		return f.downloadErr
	}
	return os.WriteFile(dest, []byte("backup-bytes"), 0o644)
}

func TestRestoreRunCmd_GCSFlagsRegistered(t *testing.T) {
	if restoreRunCmd.Flags().Lookup("gcs-bucket") == nil {
		t.Fatal("expected --gcs-bucket flag on restore run command")
	}
	if restoreRunCmd.Flags().Lookup("gcs-credentials-file") == nil {
		t.Fatal("expected --gcs-credentials-file flag on restore run command")
	}
	if restoreRunCmd.Flags().Lookup("gcs-project-id") == nil {
		t.Fatal("expected --gcs-project-id flag on restore run command")
	}
}

func TestHandleRestoreRun_DeletesStagedFileOnSuccess(t *testing.T) {
	cfgPath := writeRestoreRunConfig(t, false)

	prevCfg := restoreConfigFile
	prevKeep := restoreKeepFile
	prevFactory := newRestoreGCSBackend
	prevExec := restoreFromLocalStagedFile
	t.Cleanup(func() {
		restoreConfigFile = prevCfg
		restoreKeepFile = prevKeep
		newRestoreGCSBackend = prevFactory
		restoreFromLocalStagedFile = prevExec
	})

	restoreConfigFile = cfgPath
	restoreKeepFile = false

	fake := &fakeRestoreGCSDownloader{}
	newRestoreGCSBackend = func(cfg gcs.Config) (restoreGCSDownloader, error) {
		if cfg.Bucket != "backup-bucket" {
			return nil, fmt.Errorf("unexpected bucket: %s", cfg.Bucket)
		}
		return fake, nil
	}
	restoreFromLocalStagedFile = func(_ context.Context, _ config.RestoreJob, stagedPath string) error {
		if _, err := os.Stat(stagedPath); err != nil {
			return err
		}
		return nil
	}

	err := handleRestoreRun(nil, []string{"pg-restore"})
	if err != nil {
		t.Fatalf("handleRestoreRun() error = %v", err)
	}
	if fake.lastDest == "" {
		t.Fatal("expected downloader destination path to be captured")
	}
	if _, err := os.Stat(fake.lastDest); !os.IsNotExist(err) {
		t.Fatalf("expected staged file to be deleted, stat err = %v", err)
	}
}

func TestHandleRestoreRun_DeletesStagedFileOnFailure(t *testing.T) {
	cfgPath := writeRestoreRunConfig(t, false)

	prevCfg := restoreConfigFile
	prevKeep := restoreKeepFile
	prevFactory := newRestoreGCSBackend
	prevExec := restoreFromLocalStagedFile
	t.Cleanup(func() {
		restoreConfigFile = prevCfg
		restoreKeepFile = prevKeep
		newRestoreGCSBackend = prevFactory
		restoreFromLocalStagedFile = prevExec
	})

	restoreConfigFile = cfgPath
	restoreKeepFile = false

	fake := &fakeRestoreGCSDownloader{}
	newRestoreGCSBackend = func(_ gcs.Config) (restoreGCSDownloader, error) { return fake, nil }
	restoreFromLocalStagedFile = func(_ context.Context, _ config.RestoreJob, _ string) error {
		return errors.New("restore failed")
	}

	err := handleRestoreRun(nil, []string{"pg-restore"})
	if err == nil {
		t.Fatal("expected restore failure error")
	}
	if fake.lastDest == "" {
		t.Fatal("expected downloader destination path to be captured")
	}
	if _, statErr := os.Stat(fake.lastDest); !os.IsNotExist(statErr) {
		t.Fatalf("expected staged file to be deleted after failure, stat err = %v", statErr)
	}
}

func TestHandleRestoreRun_KeepFilePreservesStagedFile(t *testing.T) {
	cfgPath := writeRestoreRunConfig(t, true)

	prevCfg := restoreConfigFile
	prevKeep := restoreKeepFile
	prevFactory := newRestoreGCSBackend
	prevExec := restoreFromLocalStagedFile
	t.Cleanup(func() {
		restoreConfigFile = prevCfg
		restoreKeepFile = prevKeep
		newRestoreGCSBackend = prevFactory
		restoreFromLocalStagedFile = prevExec
	})

	restoreConfigFile = cfgPath
	restoreKeepFile = false

	fake := &fakeRestoreGCSDownloader{}
	newRestoreGCSBackend = func(_ gcs.Config) (restoreGCSDownloader, error) { return fake, nil }
	restoreFromLocalStagedFile = func(_ context.Context, _ config.RestoreJob, _ string) error { return nil }

	err := handleRestoreRun(nil, []string{"pg-restore"})
	if err != nil {
		t.Fatalf("handleRestoreRun() error = %v", err)
	}
	if fake.lastDest == "" {
		t.Fatal("expected downloader destination path to be captured")
	}
	if _, statErr := os.Stat(fake.lastDest); statErr != nil {
		t.Fatalf("expected staged file to be preserved with keep_file=true, stat err = %v", statErr)
	}
	_ = os.Remove(fake.lastDest)
}

func TestHandleRestoreRun_NotFoundErrorIsActionable(t *testing.T) {
	cfgPath := writeRestoreRunConfig(t, false)

	prevCfg := restoreConfigFile
	prevFactory := newRestoreGCSBackend
	t.Cleanup(func() {
		restoreConfigFile = prevCfg
		newRestoreGCSBackend = prevFactory
	})

	restoreConfigFile = cfgPath
	newRestoreGCSBackend = func(_ gcs.Config) (restoreGCSDownloader, error) {
		return &fakeRestoreGCSDownloader{downloadErr: fmt.Errorf("%w: %q", gcs.ErrObjectNotFound, "missing.sql")}, nil
	}

	err := handleRestoreRun(nil, []string{"pg-restore"})
	if err == nil {
		t.Fatal("expected error for missing object")
	}
	if !strings.Contains(err.Error(), "restore backup object not found") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func writeRestoreRunConfig(t *testing.T, keepFile bool) string {
	t.Helper()
	t.Setenv("TEST_PG_PASSWORD", "secret")

	cfg := "version: \"1.0\"\n" +
		"defaults:\n" +
		"  storage:\n" +
		"    type: local\n" +
		"    local_path: ./backups\n" +
		"databases:\n" +
		"  pg:\n" +
		"    type: postgres\n" +
		"    host: localhost\n" +
		"    username: sentinel\n" +
		"    password_env: TEST_PG_PASSWORD\n" +
		"    database: app\n" +
		"restores:\n" +
		"  pg-restore:\n" +
		"    enabled: true\n" +
		"    type: postgres\n" +
		"    host: localhost\n" +
		"    username: sentinel\n" +
		"    password_env: TEST_PG_PASSWORD\n" +
		"    database: app_restore\n" +
		"    schedule: \"0 2 * * *\"\n" +
		"    keep_file: "
	if keepFile {
		cfg += "true\n"
	} else {
		cfg += "false\n"
	}
	cfg += "    backup_source:\n" +
		"      type: gcs\n" +
		"      gcs_bucket: backup-bucket\n" +
		"      backup_path: dumps/latest.sql\n"

	path := t.TempDir() + "/restore.yaml"
	if err := os.WriteFile(path, []byte(cfg), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	return path
}
