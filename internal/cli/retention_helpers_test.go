package cli

// End-to-end coverage of the rewired retention flow.
// Ports the Manager.Apply scenarios from the deleted
// internal/retention/retention_integration_test.go and
// tests/retention/retention_integration_test.go onto applyJobRetention:
// real *monitor.Monitor for history + MockBackend for storage.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/denisakp/sentinel/internal/adapters/monitor"
	storage "github.com/denisakp/sentinel/internal/adapters/storage"
	"github.com/denisakp/sentinel/internal/config"
	"github.com/denisakp/sentinel/internal/ports"
	"github.com/denisakp/sentinel/internal/ports/storagetesting"
)

func newRetentionTestMonitor(t *testing.T, historyPath string) *monitor.Monitor {
	t.Helper()
	mon, err := monitor.NewMonitor(historyPath)
	if err != nil {
		t.Fatalf("NewMonitor() error = %v", err)
	}
	t.Cleanup(func() { _ = mon.Close() })
	return mon
}

func TestRetentionEnabledConsidersGFS(t *testing.T) {
	cases := []struct {
		name string
		rp   config.RetentionPolicy
		want bool
	}{
		{"empty", config.RetentionPolicy{}, false},
		{"flat", config.RetentionPolicy{KeepLast: 1}, true},
		{"gfs-only", config.RetentionPolicy{GFS: &config.GFSPolicy{KeepMonthly: 12}}, true},
		{"gfs-empty", config.RetentionPolicy{GFS: &config.GFSPolicy{}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := retentionEnabled(tc.rp); got != tc.want {
				t.Fatalf("retentionEnabled(%s) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}

func TestApplyJobRetentionGFSOnlyJobProcessed(t *testing.T) {
	historyPath := filepath.Join(t.TempDir(), "history.db")
	cfg := &config.Configuration{
		Version:       "1.0",
		HistoryDBPath: historyPath,
		Databases: map[string]config.BackupJob{
			"pg-job": {
				Name:      "pg-job",
				Type:      "postgres",
				Storage:   config.StorageConfig{Type: "local"},
				Retention: config.RetentionPolicy{GFS: &config.GFSPolicy{KeepDaily: 1}},
			},
		},
	}

	mon := newRetentionTestMonitor(t, historyPath)
	base := time.Date(2026, 8, 2, 6, 0, 0, 0, time.UTC)
	// Three distinct days; keep_daily:1 keeps the newest day's newest backup.
	fixtures := []*ports.Execution{
		{BackupName: "pg-job", DatabaseType: "postgres", Timestamp: base, Status: "success", FilePath: "d0.sql", FileSizeBytes: 10},
		{BackupName: "pg-job", DatabaseType: "postgres", Timestamp: base.Add(-24 * time.Hour), Status: "success", FilePath: "d1.sql", FileSizeBytes: 10},
		{BackupName: "pg-job", DatabaseType: "postgres", Timestamp: base.Add(-48 * time.Hour), Status: "success", FilePath: "d2.sql", FileSizeBytes: 10},
	}
	for _, e := range fixtures {
		if err := mon.RecordExecution(context.Background(), e); err != nil {
			t.Fatalf("RecordExecution() error = %v", err)
		}
	}

	// Dry-run: a GFS-only job must be processed (not skipped) and report the
	// two non-anchor backups with the GFS reason.
	deleted, err := applyJobRetention(context.Background(), cfg, mon, "pg-job", true)
	if err != nil {
		t.Fatalf("applyJobRetention() error = %v", err)
	}
	if len(deleted) != 2 {
		t.Fatalf("candidates = %d, want 2 (%#v)", len(deleted), deleted)
	}
	for _, d := range deleted {
		if d.FilePath == "d0.sql" {
			t.Fatalf("newest daily anchor d0.sql must be kept, got it as candidate")
		}
		if d.ReasonDeleted != "not retained by gfs" {
			t.Fatalf("candidate %s reason = %q, want gfs reason", d.FilePath, d.ReasonDeleted)
		}
	}
}

func TestApplyJobRetentionGCSDeletesRecordsAfterArtifactDelete(t *testing.T) {
	mock := storagetesting.NewMockBackend()
	mock.PutBytes("old.sql", []byte("x"))
	mock.PutBytes("mid.sql", []byte("x"))
	mock.PutBytes("new.sql", []byte("x"))
	stubRetentionBackend(t, func(_ *storage.BackendParams) (ports.StorageBackend, error) {
		return mock, nil
	})

	historyPath := filepath.Join(t.TempDir(), "history.db")
	cfg := &config.Configuration{
		Version:       "1.0",
		HistoryDBPath: historyPath,
		Databases: map[string]config.BackupJob{
			"gcs-job": {
				Name: "gcs-job",
				Type: "postgres",
				Storage: config.StorageConfig{
					Type:      "gcs",
					GCSBucket: "bucket-a",
				},
				Retention: config.RetentionPolicy{KeepLast: 1},
			},
		},
	}

	mon := newRetentionTestMonitor(t, historyPath)
	now := time.Now().UTC()
	for i, name := range []string{"old.sql", "mid.sql", "new.sql"} {
		exec := &ports.Execution{
			BackupName:    "gcs-job",
			DatabaseType:  "postgres",
			Timestamp:     now.Add(-time.Duration(3-i) * time.Hour),
			Status:        "success",
			FilePath:      "gs://bucket-a/" + name,
			FileSizeBytes: 100,
		}
		if err := mon.RecordExecution(context.Background(), exec); err != nil {
			t.Fatalf("RecordExecution() error = %v", err)
		}
	}

	deleted, err := applyJobRetention(context.Background(), cfg, mon, "gcs-job", false)
	if err != nil {
		t.Fatalf("applyJobRetention() error = %v", err)
	}
	if len(deleted) != 2 {
		t.Fatalf("deleted = %d, want 2", len(deleted))
	}

	records, err := fetchRetentionRecords(context.Background(), mon, "gcs-job")
	if err != nil {
		t.Fatalf("fetchRetentionRecords() error = %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("remaining records = %d, want 1", len(records))
	}
	if records[0].FilePath != "gs://bucket-a/new.sql" {
		t.Fatalf("remaining file = %q", records[0].FilePath)
	}
}

func TestApplyJobRetentionPreservesActiveChainBaseline(t *testing.T) {
	historyPath := filepath.Join(t.TempDir(), "history.db")
	backupDir := t.TempDir()
	fullPath := filepath.Join(backupDir, "full.sql")
	incrementalPath := filepath.Join(backupDir, "incremental.sql")
	if err := os.WriteFile(fullPath, []byte("full"), 0o644); err != nil {
		t.Fatalf("WriteFile(full) error = %v", err)
	}
	if err := os.WriteFile(incrementalPath, []byte("incremental"), 0o644); err != nil {
		t.Fatalf("WriteFile(incremental) error = %v", err)
	}

	cfg := &config.Configuration{
		Version:       "1.0",
		HistoryDBPath: historyPath,
		Databases: map[string]config.BackupJob{
			"pg-job": {
				Name:      "pg-job",
				Type:      "postgres",
				Storage:   config.StorageConfig{Type: "local"},
				Retention: config.RetentionPolicy{KeepLast: 1},
			},
		},
	}

	mon := newRetentionTestMonitor(t, historyPath)
	now := time.Now().UTC()
	fixtures := []*ports.Execution{
		{
			BackupName:    "pg-job",
			DatabaseType:  "postgres",
			Timestamp:     now.Add(-2 * time.Hour),
			Status:        "success",
			FilePath:      fullPath,
			FileSizeBytes: 4,
			BackupType:    "full",
			ChainID:       "chain-a",
			ChainIndex:    0,
		},
		{
			BackupName:    "pg-job",
			DatabaseType:  "postgres",
			Timestamp:     now.Add(-1 * time.Hour),
			Status:        "success",
			FilePath:      incrementalPath,
			FileSizeBytes: 11,
			BackupType:    "incremental",
			ChainID:       "chain-a",
			ChainIndex:    1,
		},
	}
	for _, e := range fixtures {
		if err := mon.RecordExecution(context.Background(), e); err != nil {
			t.Fatalf("RecordExecution() error = %v", err)
		}
	}

	deleted, err := applyJobRetention(context.Background(), cfg, mon, "pg-job", false)
	if err != nil {
		t.Fatalf("applyJobRetention() error = %v", err)
	}
	if len(deleted) != 0 {
		t.Fatalf("deleted = %d, want 0", len(deleted))
	}

	if _, err := os.Stat(fullPath); err != nil {
		t.Fatalf("expected baseline to remain, stat err = %v", err)
	}
}

func TestApplyJobRetentionStorageFailureKeepsRecordsConsistent(t *testing.T) {
	stubRetentionBackend(t, func(_ *storage.BackendParams) (ports.StorageBackend, error) {
		return nil, errors.New("unconfigured gcs backend")
	})

	historyPath := filepath.Join(t.TempDir(), "history.db")
	cfg := &config.Configuration{
		Version:       "1.0",
		HistoryDBPath: historyPath,
		Databases: map[string]config.BackupJob{
			"gcs-job": {
				Name:      "gcs-job",
				Type:      "postgres",
				Storage:   config.StorageConfig{Type: "gcs"},
				Retention: config.RetentionPolicy{KeepLast: 1},
			},
		},
	}

	mon := newRetentionTestMonitor(t, historyPath)
	now := time.Now().UTC()
	fixtures := []*ports.Execution{
		{BackupName: "gcs-job", DatabaseType: "postgres", Timestamp: now.Add(-2 * time.Hour), Status: "success", FilePath: "gs://bucket-a/old.sql", FileSizeBytes: 10},
		{BackupName: "gcs-job", DatabaseType: "postgres", Timestamp: now.Add(-1 * time.Hour), Status: "success", FilePath: "gs://bucket-a/new.sql", FileSizeBytes: 10},
	}
	for _, e := range fixtures {
		if err := mon.RecordExecution(context.Background(), e); err != nil {
			t.Fatalf("RecordExecution() error = %v", err)
		}
	}

	if _, err := applyJobRetention(context.Background(), cfg, mon, "gcs-job", false); err == nil {
		t.Fatal("expected retention error when storage backend init fails")
	}

	// History rows must remain untouched after the storage-side failure.
	records, err := fetchRetentionRecords(context.Background(), mon, "gcs-job")
	if err != nil {
		t.Fatalf("fetchRetentionRecords() error = %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("remaining records = %d, want 2", len(records))
	}

	// Dry-run candidate listing still reports the stale artifact.
	deleted, err := applyJobRetention(context.Background(), cfg, mon, "gcs-job", true)
	if err != nil {
		t.Fatalf("applyJobRetention(dry-run) error = %v", err)
	}
	if len(deleted) != 1 {
		t.Fatalf("candidates = %d, want 1", len(deleted))
	}
	if deleted[0].FilePath != "gs://bucket-a/old.sql" {
		t.Fatalf("candidate = %q", deleted[0].FilePath)
	}
}

// TestApplyJobRetentionHonoursConfiguredDryRun is the regression guard for #157.
//
// retention.dry_run parsed, validated and took part in defaults inheritance,
// and then no delete path consulted it. The automatic sweep that runs after a
// scheduled backup called applyJobRetention with a hard-coded false, so it
// deleted artifacts for real while the operator had explicitly asked for
// nothing to be deleted. Silent data loss with the safety switch engaged.
//
// The assertion that matters is the storage one. Counting returned candidates
// would pass against the bug, because a dry run and a real run report the same
// candidates; only whether the bytes are still there tells them apart.
func TestApplyJobRetentionHonoursConfiguredDryRun(t *testing.T) {
	mock := storagetesting.NewMockBackend()
	for _, k := range []string{"old.sql", "mid.sql", "new.sql"} {
		mock.PutBytes(k, []byte("x"))
	}
	stubRetentionBackend(t, func(_ *storage.BackendParams) (ports.StorageBackend, error) {
		return mock, nil
	})

	historyPath := filepath.Join(t.TempDir(), "history.db")
	cfg := &config.Configuration{
		Version:       "1.0",
		HistoryDBPath: historyPath,
		Databases: map[string]config.BackupJob{
			"gcs-job": {
				Name:    "gcs-job",
				Type:    "postgres",
				Storage: config.StorageConfig{Type: "gcs", GCSBucket: "bucket-a"},
				// The safety switch, set in YAML rather than on the command line.
				Retention: config.RetentionPolicy{KeepLast: 1, DryRun: true},
			},
		},
	}

	mon := newRetentionTestMonitor(t, historyPath)
	now := time.Now().UTC()
	for i, name := range []string{"old.sql", "mid.sql", "new.sql"} {
		exec := &ports.Execution{
			BackupName:    "gcs-job",
			DatabaseType:  "postgres",
			Timestamp:     now.Add(-time.Duration(3-i) * time.Hour),
			Status:        "success",
			FilePath:      "gs://bucket-a/" + name,
			FileSizeBytes: 100,
		}
		if err := mon.RecordExecution(context.Background(), exec); err != nil {
			t.Fatalf("RecordExecution() error = %v", err)
		}
	}

	// false is what the post-backup sweep passes. Before the fix this deleted.
	deleted, err := applyJobRetention(context.Background(), cfg, mon, "gcs-job", false)
	if err != nil {
		t.Fatalf("applyJobRetention() error = %v", err)
	}
	if len(deleted) != 2 {
		t.Fatalf("candidates = %d, want 2; a dry run still reports what it would remove", len(deleted))
	}

	for _, k := range []string{"old.sql", "mid.sql", "new.sql"} {
		if _, ok := mock.GetBytes(k); !ok {
			t.Errorf("retention.dry_run: true was ignored: %s was deleted from storage", k)
		}
	}

	records, err := fetchRetentionRecords(context.Background(), mon, "gcs-job")
	if err != nil {
		t.Fatalf("fetchRetentionRecords() error = %v", err)
	}
	if len(records) != 3 {
		t.Errorf("history rows = %d, want 3; a dry run must not delete history either", len(records))
	}
}

// TestApplyJobRetentionCLIFlagCannotCancelConfiguredDryRun pins the direction of
// the override. dry_run is a safety switch, so the flag and the configuration OR
// together: either can request a dry run, neither can cancel the other's. Turning
// off a configured dry_run is an edit to the file, not a flag.
func TestApplyJobRetentionCLIFlagCannotCancelConfiguredDryRun(t *testing.T) {
	mock := storagetesting.NewMockBackend()
	mock.PutBytes("old.sql", []byte("x"))
	mock.PutBytes("new.sql", []byte("x"))
	stubRetentionBackend(t, func(_ *storage.BackendParams) (ports.StorageBackend, error) {
		return mock, nil
	})

	historyPath := filepath.Join(t.TempDir(), "history.db")
	cfg := &config.Configuration{
		Version:       "1.0",
		HistoryDBPath: historyPath,
		Databases: map[string]config.BackupJob{
			"job": {
				Name:      "job",
				Type:      "postgres",
				Storage:   config.StorageConfig{Type: "gcs", GCSBucket: "bucket-a"},
				Retention: config.RetentionPolicy{KeepLast: 1, DryRun: true},
			},
		},
	}

	mon := newRetentionTestMonitor(t, historyPath)
	now := time.Now().UTC()
	for i, name := range []string{"old.sql", "new.sql"} {
		if err := mon.RecordExecution(context.Background(), &ports.Execution{
			BackupName: "job", DatabaseType: "postgres",
			Timestamp: now.Add(-time.Duration(2-i) * time.Hour),
			Status:    "success", FilePath: "gs://bucket-a/" + name, FileSizeBytes: 100,
		}); err != nil {
			t.Fatalf("RecordExecution() error = %v", err)
		}
	}

	if _, err := applyJobRetention(context.Background(), cfg, mon, "job", false); err != nil {
		t.Fatalf("applyJobRetention() error = %v", err)
	}
	if _, ok := mock.GetBytes("old.sql"); !ok {
		t.Error("the absent --dry-run flag cancelled a configured dry_run: true and deleted an artifact")
	}
}
