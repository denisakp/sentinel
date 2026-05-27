package retention_test

import (
	"context"
	"testing"
	"time"

	"github.com/denisakp/sentinel/internal/config"
	"github.com/denisakp/sentinel/internal/adapters/monitor"
	"github.com/denisakp/sentinel/internal/ports"
	"github.com/denisakp/sentinel/internal/retention"
)

func TestManagerApplyGCSFailureKeepsRecordsConsistent(t *testing.T) {
	historyPath := t.TempDir() + "/history.db"
	cfg := &config.Configuration{
		Version:       "1.0",
		HistoryDBPath: historyPath,
		Databases: map[string]config.BackupJob{
			"gcs-job": {
				Name: "gcs-job",
				Type: "postgres",
				Storage: config.StorageConfig{
					Type: "gcs",
				},
				Retention: config.RetentionPolicy{KeepLast: 1},
			},
		},
	}

	mon, err := monitor.NewMonitor(historyPath)
	if err != nil {
		t.Fatalf("NewMonitor() error = %v", err)
	}

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
	_ = mon.Close()

	manager, err := retention.NewManager(cfg)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	defer manager.Close()

	if _, err := manager.Apply(context.Background(), "gcs-job", false); err == nil {
		t.Fatal("expected retention error for unconfigured gcs backend in test env")
	}

	candidates, err := manager.ListCandidates(context.Background(), "gcs-job")
	if err != nil {
		t.Fatalf("ListCandidates() error = %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("candidates = %d, want 1", len(candidates))
	}
	if candidates[0].FilePath != "gs://bucket-a/old.sql" {
		t.Fatalf("candidate = %q", candidates[0].FilePath)
	}
}
