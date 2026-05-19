package monitor

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestRecordExecution_ErrorMessageIsIdentity proves the monitor sink does not
// transform the error string: whatever the dump-adapter passes lands in the
// SQLite row byte-for-byte. This closes the FR-008 monitor-sink assumption
// that redaction must happen upstream (in the adapter, via sanitize.RedactStderr).
func TestRecordExecution_ErrorMessageIsIdentity(t *testing.T) {
	cases := []struct {
		name string
		msg  string
	}{
		{
			name: "redacted",
			msg:  "failed to execute pg_dump command - exit 1, password=*****",
		},
		{
			name: "control_non_redacted",
			msg:  "failed to execute pg_dump command - exit 1, password=hunter2",
		},
	}

	dbPath := filepath.Join(t.TempDir(), "history.db")
	mon, err := NewMonitor(dbPath)
	if err != nil {
		t.Fatalf("NewMonitor: %v", err)
	}
	defer mon.Close()

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Now().UTC()
			exec := &Execution{
				BackupName:     "leak-probe",
				DatabaseType:   "postgres",
				Timestamp:      now,
				DurationMs:     1,
				Status:         StatusFailed,
				StorageBackend: "local",
				FilePath:       "x.sql",
				ErrorMessage:   tc.msg,
				CreatedAt:      now,
			}
			if err := mon.RecordExecution(context.Background(), exec); err != nil {
				t.Fatalf("RecordExecution: %v", err)
			}
			got, err := mon.GetExecution(context.Background(), exec.ID)
			if err != nil {
				t.Fatalf("GetExecution: %v", err)
			}
			if got.ErrorMessage != tc.msg {
				t.Fatalf("identity violated: got %q, want %q", got.ErrorMessage, tc.msg)
			}
			if tc.name == "control_non_redacted" && !strings.Contains(got.ErrorMessage, "hunter2") {
				t.Fatalf("control sanity check failed — sink unexpectedly redacted")
			}
		})
	}
}
