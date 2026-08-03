package notifier_test

import (
	"testing"
	"time"

	"github.com/denisakp/sentinel/internal/adapters/notifier"
	"github.com/denisakp/sentinel/internal/ports"
)

func TestFormatMessage(t *testing.T) {
	startTime := time.Date(2026, 2, 12, 10, 0, 0, 0, time.UTC)
	endTime := startTime.Add(5 * time.Minute)

	backup := &ports.BackupContext{
		BackupName:   "daily-backup",
		DatabaseType: "postgres",
		DatabaseName: "mydb",
		Status:       ports.NotifyStatusSuccess,
		StartTime:    startTime,
		EndTime:      endTime,
		FileSize:     1024 * 1024 * 500,
		FilePath:     "/backups/mydb_20260212.sql",
	}

	msg := notifier.FormatMessage(backup)

	if msg.Title == "" {
		t.Fatal("expected non-empty title")
	}

	if msg.MessageText == "" {
		t.Fatal("expected non-empty message")
	}

	if msg.Status != ports.NotifyStatusSuccess {
		t.Fatalf("expected status success, got %s", msg.Status)
	}

	if msg.Details["Database"] != "mydb" {
		t.Fatalf("expected database 'mydb', got %s", msg.Details["Database"])
	}

	if msg.Details["Database Type"] != "postgres" {
		t.Fatalf("expected type 'postgres', got %s", msg.Details["Database Type"])
	}
}

func TestFormatMessage_WithError(t *testing.T) {
	backup := &ports.BackupContext{
		BackupName:   "backup",
		DatabaseType: "mysql",
		DatabaseName: "db",
		Status:       ports.NotifyStatusFailure,
		EndTime:      time.Now(),
		Error:        "connection timeout",
		FileSize:     0,
	}

	msg := notifier.FormatMessage(backup)

	if msg.Details["Error"] != "connection timeout" {
		t.Fatalf("expected error in details, got %s", msg.Details["Error"])
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		duration time.Duration
	}{
		{100 * time.Millisecond},
		{1 * time.Second},
		{30 * time.Second},
		{1 * time.Minute},
		{5*time.Minute + 32*time.Second},
	}

	for _, tt := range tests {
		startTime := time.Now()
		endTime := startTime.Add(tt.duration)

		backup := &ports.BackupContext{
			BackupName:   "test",
			DatabaseType: "postgres",
			DatabaseName: "db",
			Status:       ports.NotifyStatusSuccess,
			StartTime:    startTime,
			EndTime:      endTime,
		}

		msg := notifier.FormatMessage(backup)
		if msg.Details["Duration"] == "" {
			t.Fatalf("expected duration for %v", tt.duration)
		}
	}
}

func TestFormatFileSize(t *testing.T) {
	tests := []struct {
		bytes    int64
		contains string
	}{
		{512, "B"},
		{1024, "KB"},
		{1024 * 1024, "MB"},
		{1024 * 1024 * 1024, "GB"},
	}

	for _, tt := range tests {
		backup := &ports.BackupContext{
			BackupName:   "test",
			DatabaseType: "postgres",
			DatabaseName: "db",
			Status:       ports.NotifyStatusSuccess,
			EndTime:      time.Now(),
			FileSize:     tt.bytes,
		}

		msg := notifier.FormatMessage(backup)
		if !contains(msg.Details["File Size"], tt.contains) {
			t.Fatalf("expected file size to contain '%s', got '%s'", tt.contains, msg.Details["File Size"])
		}
	}
}

func TestSanitizeForSlack(t *testing.T) {
	tests := []struct {
		input    string
		contains string
	}{
		{"**bold text**", "bold text"},
		{"_italic text_", "italic text"},
		{"~strikethrough~", "strikethrough"},
		{"normal text", "normal text"},
	}

	for _, tt := range tests {
		result := notifier.SanitizeForSlack(tt.input)
		if !contains(result, tt.contains) {
			t.Fatalf("expected result to contain '%s', got '%s'", tt.contains, result)
		}
	}
}

func TestStatusEmoji(t *testing.T) {
	tests := []struct {
		status ports.BackupStatus
		label  string
	}{
		{ports.NotifyStatusSuccess, "OK"},
		{ports.NotifyStatusFailure, "FAIL"},
		{ports.NotifyStatusWarning, "WARN"},
	}

	for _, tt := range tests {
		label := notifier.StatusEmoji(tt.status)
		if label != tt.label {
			t.Fatalf("expected label '%s' for status %s, got '%s'", tt.label, tt.status, label)
		}
	}
}

func contains(str, substr string) bool {
	for i := 0; i <= len(str)-len(substr); i++ {
		if str[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
