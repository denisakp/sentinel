package cli

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/denisakp/sentinel/internal/config"
	"github.com/denisakp/sentinel/internal/adapters/monitor"
	"github.com/denisakp/sentinel/internal/ports"
	"github.com/denisakp/sentinel/internal/adapters/storage"
	"github.com/denisakp/sentinel/internal/utils"
	"github.com/spf13/cobra"
)

func TestApplyScheduledOutputName(t *testing.T) {
	t.Cleanup(utils.ResetScheduledOutCountersForTest)

	fixed := time.Date(2026, 3, 15, 2, 0, 0, 0, time.UTC)
	job := config.BackupJob{
		Name:   "postgres-job",
		Type:   "postgres",
		Output: "postgres-dev.sql",
		DatabaseOptions: map[string]interface{}{
			"pg_out_format": "p",
		},
	}
	params := &storage.Params{OutName: job.Output}

	applyScheduledOutputName(job, params, executionModeScheduled, fixed)

	if params.OutName != "postgres-dev_2026-03-15T02-00-00.sql" {
		t.Fatalf("scheduled outName = %q", params.OutName)
	}
}

func TestApplyScheduledOutputNameConfigModeUnchanged(t *testing.T) {
	job := config.BackupJob{Name: "mysql-job", Type: "mysql", Output: "mysql-dev"}
	params := &storage.Params{OutName: job.Output}

	applyScheduledOutputName(job, params, executionModeConfig, time.Date(2026, 3, 15, 2, 0, 0, 0, time.UTC))

	if params.OutName != "mysql-dev" {
		t.Fatalf("config mode should not rewrite output, got %q", params.OutName)
	}
}

func TestRunScheduledRetentionWarningNonFatal(t *testing.T) {
	historyPath := filepath.Join(t.TempDir(), "history.db")
	cfg := &config.Configuration{
		HistoryDBPath: historyPath,
		Databases: map[string]config.BackupJob{
			"retention-job": {
				Name: "retention-job",
				Type: "postgres",
				Storage: config.StorageConfig{
					Type: "s3",
				},
				Retention: config.RetentionPolicy{KeepLast: 1},
			},
		},
	}

	mon, err := monitor.NewMonitor(historyPath)
	if err != nil {
		t.Fatalf("new monitor: %v", err)
	}
	t.Cleanup(func() { _ = mon.Close() })

	executions := []*ports.Execution{
		{
			BackupName:    "retention-job",
			DatabaseType:  "postgres",
			Timestamp:     time.Now().Add(-1 * time.Hour).UTC(),
			Status:        "success",
			FilePath:      "s3://bucket/backup-new.sql",
			FileSizeBytes: 123,
		},
		{
			BackupName:    "retention-job",
			DatabaseType:  "postgres",
			Timestamp:     time.Now().Add(-2 * time.Hour).UTC(),
			Status:        "success",
			FilePath:      "s3://bucket/backup-old.sql",
			FileSizeBytes: 123,
		},
	}
	for _, exec := range executions {
		if err := mon.RecordExecution(context.Background(), exec); err != nil {
			t.Fatalf("record execution: %v", err)
		}
	}

	cmd := &cobra.Command{}
	var errBuf bytes.Buffer
	cmd.SetErr(&errBuf)
	cmd.SetOut(&bytes.Buffer{})

	runScheduledRetention(cmd, cfg, cfg.Databases["retention-job"])

	if !strings.Contains(errBuf.String(), "warning: retention apply failed") {
		t.Fatalf("expected warning output, got: %s", errBuf.String())
	}
}

func TestRunScheduledRetentionWarningNonFatalGCS(t *testing.T) {
	historyPath := filepath.Join(t.TempDir(), "history.db")
	cfg := &config.Configuration{
		HistoryDBPath: historyPath,
		Databases: map[string]config.BackupJob{
			"retention-job": {
				Name: "retention-job",
				Type: "postgres",
				Storage: config.StorageConfig{
					Type:      "gcs",
					GCSBucket: "bucket-a",
				},
				Retention: config.RetentionPolicy{KeepLast: 1},
			},
		},
	}

	mon, err := monitor.NewMonitor(historyPath)
	if err != nil {
		t.Fatalf("new monitor: %v", err)
	}
	t.Cleanup(func() { _ = mon.Close() })

	executions := []*ports.Execution{
		{
			BackupName:    "retention-job",
			DatabaseType:  "postgres",
			Timestamp:     time.Now().Add(-1 * time.Hour).UTC(),
			Status:        "success",
			FilePath:      "gs://bucket-a/backup-new.sql",
			FileSizeBytes: 123,
		},
		{
			BackupName:    "retention-job",
			DatabaseType:  "postgres",
			Timestamp:     time.Now().Add(-2 * time.Hour).UTC(),
			Status:        "success",
			FilePath:      "gs://bucket-a/backup-old.sql",
			FileSizeBytes: 123,
		},
	}
	for _, exec := range executions {
		if err := mon.RecordExecution(context.Background(), exec); err != nil {
			t.Fatalf("record execution: %v", err)
		}
	}

	cmd := &cobra.Command{}
	var errBuf bytes.Buffer
	cmd.SetErr(&errBuf)
	cmd.SetOut(&bytes.Buffer{})

	runScheduledRetention(cmd, cfg, cfg.Databases["retention-job"])

	if !strings.Contains(errBuf.String(), "warning: retention apply failed") {
		t.Fatalf("expected warning output, got: %s", errBuf.String())
	}
}

func TestRunScheduledRetentionSkipWhenNoPolicy(t *testing.T) {
	cfg := &config.Configuration{
		HistoryDBPath: filepath.Join(t.TempDir(), "history.db"),
		Databases: map[string]config.BackupJob{
			"job": {
				Name:      "job",
				Type:      "postgres",
				Retention: config.RetentionPolicy{},
			},
		},
	}

	cmd := &cobra.Command{}
	var errBuf bytes.Buffer
	cmd.SetErr(&errBuf)
	cmd.SetOut(&bytes.Buffer{})

	runScheduledRetention(cmd, cfg, cfg.Databases["job"])

	if errBuf.Len() != 0 {
		t.Fatalf("expected no warnings when retention policy disabled, got: %s", errBuf.String())
	}
}

func TestBuildScheduledOutNameCollisionFromBackupFlow(t *testing.T) {
	t.Cleanup(utils.ResetScheduledOutCountersForTest)
	restore := utils.SetNowForTest(func() time.Time {
		return time.Date(2026, 3, 15, 2, 0, 0, 0, time.UTC)
	})
	defer restore()

	job := config.BackupJob{
		Name:   "postgres-job",
		Type:   "postgres",
		Output: "postgres-dev",
		DatabaseOptions: map[string]interface{}{
			"pg_out_format": "p",
		},
	}

	first := &storage.Params{OutName: job.Output}
	second := &storage.Params{OutName: job.Output}

	applyScheduledOutputName(job, first, executionModeScheduled, utils.NowUTC())
	applyScheduledOutputName(job, second, executionModeScheduled, utils.NowUTC())

	if first.OutName != "postgres-dev_2026-03-15T02-00-00.sql" {
		t.Fatalf("first outName = %q", first.OutName)
	}
	if second.OutName != "postgres-dev_2026-03-15T02-00-00-1.sql" {
		t.Fatalf("second outName = %q", second.OutName)
	}
}

func TestResolveBackupPathGCSDoesNotExposeCredentials(t *testing.T) {
	params := &storage.Params{
		StorageType:        "gcs",
		GCSBucket:          "prod-backups",
		OutName:            "db/prod.sql",
		GCSCredentialsFile: "/secrets/prod-sa.json",
		GCSProjectID:       "prod-project",
	}

	path, _ := resolveBackupPath(params)
	if path != "gs://prod-backups/db/prod.sql" {
		t.Fatalf("path = %q", path)
	}
	if strings.Contains(path, params.GCSCredentialsFile) {
		t.Fatalf("path leaked credentials file: %q", path)
	}
	if strings.Contains(path, params.GCSProjectID) {
		t.Fatalf("path leaked project id unexpectedly: %q", path)
	}
}
