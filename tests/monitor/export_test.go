package monitor_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/denisakp/sentinel/internal/monitor"
)

func TestExportHistoryJSON(t *testing.T) {
	mon := newTestMonitor(t)
	defer mon.Close()

	insertExecution(t, mon, "job-a", "postgres", "success", time.Now(), 100)

	data, err := mon.ExportHistory(context.Background(), "json", &monitor.Filter{BackupName: "job-a"})
	if err != nil {
		t.Fatalf("export history json failed: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "job-a") {
		t.Fatalf("expected json export to contain job name")
	}
}

func TestExportHistoryCSV(t *testing.T) {
	mon := newTestMonitor(t)
	defer mon.Close()

	insertExecution(t, mon, "job-b", "mysql", "failure", time.Now(), 200)

	data, err := mon.ExportHistory(context.Background(), "csv", &monitor.Filter{BackupName: "job-b"})
	if err != nil {
		t.Fatalf("export history csv failed: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "backup_name") {
		t.Fatalf("expected csv export to include headers")
	}
	if !strings.Contains(content, "job-b") {
		t.Fatalf("expected csv export to contain job name")
	}
}
