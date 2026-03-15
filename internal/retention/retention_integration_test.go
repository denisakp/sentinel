package retention

import (
	"context"
	"testing"
	"time"

	"github.com/denisakp/sentinel/internal/config"
	"github.com/denisakp/sentinel/internal/monitor"
	"github.com/denisakp/sentinel/internal/storage/gcs"
)

func TestManagerApplyGCSDeletesRecordsAfterArtifactDelete(t *testing.T) {
	original := newGCSDeleteBackend
	t.Cleanup(func() { newGCSDeleteBackend = original })

	fake := &fakeGCSDeleteBackend{}
	newGCSDeleteBackend = func(_ gcs.Config) (gcsDeleteBackend, error) {
		return fake, nil
	}

	historyPath := t.TempDir() + "/history.db"
	cfg := &config.Configuration{
		Version:       "1.0",
		HistoryDBPath: historyPath,
		Databases: map[string]config.BackupJob{
			"gcs-job": {
				Name: "gcs-job",
				Type: "postgres",
				Storage: config.StorageConfig{
					Type:               "gcs",
					GCSBucket:          "bucket-a",
					GCSCredentialsFile: "/tmp/creds.json",
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
	fixtures := []*monitor.Execution{
		{
			BackupName:    "gcs-job",
			DatabaseType:  "postgres",
			Timestamp:     now.Add(-3 * time.Hour),
			Status:        "success",
			FilePath:      "gs://bucket-a/old.sql",
			FileSizeBytes: 100,
		},
		{
			BackupName:    "gcs-job",
			DatabaseType:  "postgres",
			Timestamp:     now.Add(-2 * time.Hour),
			Status:        "success",
			FilePath:      "gs://bucket-a/mid.sql",
			FileSizeBytes: 100,
		},
		{
			BackupName:    "gcs-job",
			DatabaseType:  "postgres",
			Timestamp:     now.Add(-1 * time.Hour),
			Status:        "success",
			FilePath:      "gs://bucket-a/new.sql",
			FileSizeBytes: 100,
		},
	}
	for _, e := range fixtures {
		if err := mon.RecordExecution(context.Background(), e); err != nil {
			t.Fatalf("RecordExecution() error = %v", err)
		}
	}
	_ = mon.Close()

	manager, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	defer manager.Close()

	deleted, err := manager.Apply(context.Background(), "gcs-job", false)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if len(deleted) != 2 {
		t.Fatalf("deleted = %d, want 2", len(deleted))
	}

	records, err := manager.fetchRecords(context.Background(), "gcs-job")
	if err != nil {
		t.Fatalf("fetchRecords() error = %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("remaining records = %d, want 1", len(records))
	}
	if records[0].FilePath != "gs://bucket-a/new.sql" {
		t.Fatalf("remaining file = %q", records[0].FilePath)
	}
}
