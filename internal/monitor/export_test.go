package monitor

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestExportHistoryCSV_IncludesIncrementalObservabilityFields(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "history.db")
	mon, err := NewMonitor(dbPath)
	if err != nil {
		t.Fatalf("NewMonitor() error = %v", err)
	}
	defer mon.Close()

	now := time.Now().UTC()
	rec := &Execution{
		BackupName:          "backup-incremental",
		DatabaseType:        "postgres",
		Timestamp:           now,
		Status:              StatusCompleted,
		StorageBackend:      "local",
		FilePath:            "backup.sql",
		BackupType:          "incremental",
		ChainID:             "chain-009",
		ChainIndex:          6,
		DeltaSizeBytes:      512,
		FullBackupSizeBytes: 4096,
		CreatedAt:           now,
	}
	if err := mon.RecordExecution(context.Background(), rec); err != nil {
		t.Fatalf("RecordExecution() error = %v", err)
	}

	data, err := mon.ExportHistory(context.Background(), "csv", &Filter{BackupName: "backup-incremental"})
	if err != nil {
		t.Fatalf("ExportHistory() error = %v", err)
	}
	output := string(data)
	for _, expected := range []string{"backup_type", "chain_id", "delta_size_bytes", "full_backup_size_bytes", "incremental", "chain-009", ",512,", ",4096,"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("csv export missing %q in:\n%s", expected, output)
		}
	}
}
