package cli

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/denisakp/sentinel/internal/config"
	"github.com/denisakp/sentinel/internal/scheduler"
)

func TestScheduleListTableHeadersAndLastStatusRemoval(t *testing.T) {
	next := time.Date(2026, 3, 12, 2, 0, 0, 0, time.UTC)
	rows := buildScheduleListRows([]scheduler.JobInfo{
		{
			Name:          "prod-postgres",
			ScheduleExpr:  "0 2 * * *",
			NextExecution: next,
			LastStatus:    "failed",
		},
	}, map[string]config.RestoreJob{})

	table := renderScheduleListTable(rows)
	lines := strings.Split(strings.TrimSpace(table), "\n")
	if len(lines) < 2 {
		t.Fatalf("expected table output with header and row, got: %q", table)
	}

	headerLine := lines[0]
	for _, col := range []string{"TYPE", "NAME", "SCHEDULE", "NEXT EXECUTION"} {
		if !strings.Contains(headerLine, col) {
			t.Fatalf("schedule list header missing column %q: %q", col, headerLine)
		}
	}
	if strings.Contains(headerLine, "LAST STATUS") {
		t.Fatalf("table header must not include LAST STATUS: %q", headerLine)
	}
}

func TestScheduleListTypeResolutionIncludesRestore(t *testing.T) {
	next := time.Date(2026, 3, 12, 2, 0, 0, 0, time.UTC)
	rows := buildScheduleListRows([]scheduler.JobInfo{
		{Name: "nightly-backup", ScheduleExpr: "0 2 * * *", NextExecution: next},
		{Name: "restore-drill", ScheduleExpr: "0 4 * * 0", NextExecution: next},
	}, map[string]config.RestoreJob{"restore-drill": {Type: "postgres"}})

	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	if rows[0].Type != "backup" {
		t.Fatalf("expected first row to be backup, got %q", rows[0].Type)
	}
	if rows[1].Type != "restore" {
		t.Fatalf("expected second row to be restore, got %q", rows[1].Type)
	}
}

func TestScheduleListJSONCompatibilityIncludesLastStatus(t *testing.T) {
	next := time.Date(2026, 3, 12, 2, 0, 0, 0, time.UTC)
	rows := buildScheduleListRows([]scheduler.JobInfo{
		{
			Name:          "prod-postgres",
			ScheduleExpr:  "0 2 * * *",
			NextExecution: next,
			LastStatus:    "failed",
		},
	}, map[string]config.RestoreJob{})

	data, err := json.Marshal(rows)
	if err != nil {
		t.Fatalf("failed to marshal json rows: %v", err)
	}
	payload := string(data)
	if !strings.Contains(payload, "\"last_status\":\"failed\"") {
		t.Fatalf("expected last_status in json payload, got %s", payload)
	}
}
