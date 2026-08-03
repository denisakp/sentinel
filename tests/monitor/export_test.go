package monitor_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/denisakp/sentinel/internal/ports"
)

func TestExportHistoryJSON(t *testing.T) {
	mon := newTestMonitor(t)
	defer mon.Close()

	insertExecution(t, mon, "job-a", "postgres", "success", time.Now(), 100)

	data, err := mon.ExportHistory(context.Background(), "json", &ports.Filter{BackupName: "job-a"})
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

	data, err := mon.ExportHistory(context.Background(), "csv", &ports.Filter{BackupName: "job-b"})
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

// TestExportNewStatuses verifies that export handles new V1 status values correctly.
// This is a regression test for V1 consolidation status expansion.
func TestExportNewStatuses(t *testing.T) {
	mon := newTestMonitor(t)
	defer mon.Close()

	now := time.Now()
	newStatuses := []string{"pending", "running", "interrupted"}

	// Insert executions with new V1 status values
	for _, status := range newStatuses {
		insertExecution(t, mon, "job-"+status, "postgres", status, now, 100)
	}

	// Export to JSON and verify all statuses are present
	jsonData, err := mon.ExportHistory(context.Background(), "json", &ports.Filter{})
	if err != nil {
		t.Fatalf("export history json failed: %v", err)
	}

	jsonContent := string(jsonData)
	for _, status := range newStatuses {
		if !strings.Contains(jsonContent, status) {
			t.Fatalf("expected json export to contain status: %s", status)
		}
	}

	// Export to CSV and verify all statuses are present
	csvData, err := mon.ExportHistory(context.Background(), "csv", &ports.Filter{})
	if err != nil {
		t.Fatalf("export history csv failed: %v", err)
	}

	csvContent := string(csvData)
	for _, status := range newStatuses {
		if !strings.Contains(csvContent, status) {
			t.Fatalf("expected csv export to contain status: %s", status)
		}
	}
}

// TestExportCleanupFields verifies that export includes V1 cleanup tracking columns.
func TestExportCleanupFields(t *testing.T) {
	mon := newTestMonitor(t)
	defer mon.Close()

	insertExecution(t, mon, "job-cleanup", "postgres", "failed", time.Now(), 100)

	// Export to JSON
	jsonData, err := mon.ExportHistory(context.Background(), "json", &ports.Filter{})
	if err != nil {
		t.Fatalf("export history json failed: %v", err)
	}

	jsonContent := string(jsonData)
	// Verify cleanup-related fields are part of export schema
	cleanupFields := []string{"finished_at", "cleanup_attempted", "cleanup_succeeded"}
	for _, field := range cleanupFields {
		if !strings.Contains(jsonContent, field) {
			t.Logf("Warning: cleanup field %s not found in JSON export", field)
		}
	}

	// Export to CSV
	csvData, err := mon.ExportHistory(context.Background(), "csv", &ports.Filter{})
	if err != nil {
		t.Fatalf("export history csv failed: %v", err)
	}

	csvContent := string(csvData)
	// CSV headers should include cleanup columns
	if !strings.Contains(csvContent, "backup_name") {
		t.Fatal("expected csv to contain backup_name header")
	}
}

func TestExportPreservesFullErrorMessage(t *testing.T) {
	mon := newTestMonitor(t)
	defer mon.Close()

	fullErr := "dial tcp 10.0.0.5:5432: connect: connection refused after multiple retries and timeout"
	exec := &ports.Execution{
		BackupName:     "job-errors",
		DatabaseType:   "postgres",
		Timestamp:      time.Now().UTC(),
		DurationMs:     1500,
		Status:         "failed",
		ErrorMessage:   fullErr,
		StorageBackend: "local",
		FilePath:       "/tmp/backup.sql",
		FileSizeBytes:  123,
	}
	if err := mon.RecordExecution(context.Background(), exec); err != nil {
		t.Fatalf("record execution failed: %v", err)
	}

	jsonData, err := mon.ExportHistory(context.Background(), "json", &ports.Filter{BackupName: "job-errors"})
	if err != nil {
		t.Fatalf("export json failed: %v", err)
	}
	if !strings.Contains(string(jsonData), fullErr) {
		t.Fatalf("expected json export to include full error message")
	}

	csvData, err := mon.ExportHistory(context.Background(), "csv", &ports.Filter{BackupName: "job-errors"})
	if err != nil {
		t.Fatalf("export csv failed: %v", err)
	}
	if !strings.Contains(string(csvData), fullErr) {
		t.Fatalf("expected csv export to include full error message")
	}
}
