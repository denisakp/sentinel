package scheduler

import (
	"context"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/denisakp/sentinel/internal/adapters/monitor"
	"github.com/denisakp/sentinel/internal/ports"
)

func newTestMonitor(t *testing.T) *monitor.Monitor {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "history.db")
	mon, err := monitor.NewMonitor(dbPath)
	if err != nil {
		t.Fatalf("NewMonitor: %v", err)
	}
	t.Cleanup(func() { _ = mon.Close() })
	return mon
}

func seedRunning(t *testing.T, mon *monitor.Monitor, execID, jobName, dbType string) {
	t.Helper()
	exec := &ports.Execution{
		ID:           execID,
		BackupName:   jobName,
		DatabaseType: dbType,
	}
	if err := mon.RecordRunning(context.Background(), exec); err != nil {
		t.Fatalf("RecordRunning: %v", err)
	}
}

func isFailureStatus(s string) bool {
	return s == ports.StatusFailed || s == monitor.LegacyStatusFailure
}

// --- T015 ---

func TestExecuteBackupWithCleanup_RecordsPanic(t *testing.T) {
	mon := newTestMonitor(t)
	const execID = "exec-test-001"
	seedRunning(t, mon, execID, "panicjob", "postgres")
	fs := &fakeStorage{}

	result := ExecuteBackupWithCleanup(
		context.Background(),
		execID,
		"/tmp/some/backup.sql",
		fs,
		mon,
		func(ctx context.Context) error { panic("boom") },
	)

	if result.Error == nil {
		t.Fatalf("expected result.Error set by recover, got nil")
	}
	if !strings.HasPrefix(result.Error.Error(), "worker panic: ") {
		t.Errorf("expected 'worker panic:' prefix, got %q", result.Error.Error())
	}

	exec, err := mon.GetExecution(context.Background(), execID)
	if err != nil {
		t.Fatalf("GetExecution: %v", err)
	}
	if !isFailureStatus(exec.Status) {
		t.Errorf("expected failure status, got %q", exec.Status)
	}
	if exec.ErrorMessage != "worker panic: boom" {
		t.Errorf("expected error_message 'worker panic: boom', got %q", exec.ErrorMessage)
	}
}

// --- T016 ---

func TestExecuteBackupWithCleanup_PanicBeforeStart(t *testing.T) {
	mon := newTestMonitor(t)
	fs := &fakeStorage{}

	result := ExecuteBackupWithCleanupContext(
		context.Background(),
		"",
		"",
		"jobX",
		"postgres",
		fs,
		mon,
		func(ctx context.Context) error { panic("pre-start boom") },
	)

	if result.ExecutionID == "" {
		t.Fatalf("expected synthesized executionID, got empty")
	}

	exec, err := mon.GetExecution(context.Background(), result.ExecutionID)
	if err != nil {
		t.Fatalf("GetExecution synthesized: %v", err)
	}
	if !isFailureStatus(exec.Status) {
		t.Errorf("expected failure status, got %q", exec.Status)
	}
	if !strings.HasPrefix(exec.ErrorMessage, "worker panic: ") {
		t.Errorf("expected 'worker panic:' prefix, got %q", exec.ErrorMessage)
	}
}

// --- T017 ---

func TestExecuteBackupWithCleanup_PanicMessageTruncated(t *testing.T) {
	mon := newTestMonitor(t)
	const execID = "exec-trunc-001"
	seedRunning(t, mon, execID, "panicjob", "postgres")
	fs := &fakeStorage{}

	huge := strings.Repeat("世", 1000)
	ExecuteBackupWithCleanup(
		context.Background(),
		execID,
		"",
		fs,
		mon,
		func(ctx context.Context) error { panic(huge) },
	)

	exec, err := mon.GetExecution(context.Background(), execID)
	if err != nil {
		t.Fatalf("GetExecution: %v", err)
	}
	if !strings.HasPrefix(exec.ErrorMessage, "worker panic: ") {
		t.Fatalf("missing prefix: %q", exec.ErrorMessage[:min(40, len(exec.ErrorMessage))])
	}
	body := strings.TrimPrefix(exec.ErrorMessage, "worker panic: ")
	if len(body) > maxPanicMsgBytes {
		t.Errorf("body length %d exceeds %d", len(body), maxPanicMsgBytes)
	}
	if !utf8.ValidString(body) {
		t.Errorf("truncated body is not valid UTF-8")
	}
}

// --- T018 ---

func TestExecuteRestoreWithCleanup_RecordsPanic(t *testing.T) {
	mon := newTestMonitor(t)
	const execID = "restore-exec-001"
	seedRunning(t, mon, execID, "restore-panicjob", "postgres")
	fs := &fakeStorage{}

	result := ExecuteRestoreWithCleanup(
		context.Background(),
		execID,
		"/tmp/restore.sql",
		fs,
		mon,
		func(ctx context.Context) error { panic("restore-boom") },
	)
	if result.Error == nil {
		t.Fatalf("expected result.Error set")
	}

	exec, err := mon.GetExecution(context.Background(), execID)
	if err != nil {
		t.Fatalf("GetExecution: %v", err)
	}
	if !isFailureStatus(exec.Status) {
		t.Errorf("expected failure status, got %q", exec.Status)
	}
}

// --- T019: panic feeds retry ---

func TestRunBackupWithRetry_PanicFeedsRetry(t *testing.T) {
	original := defaultBackoffs
	defaultBackoffs = []time.Duration{0, 0}
	defer func() { defaultBackoffs = original }()

	var attempts atomic.Int64
	err := RunBackupWithRetry(context.Background(), "job", "db", func() (err error) {
		defer func() {
			if pErr, _ := HandlePanic(recover()); pErr != nil {
				err = pErr
			}
		}()
		n := attempts.Add(1)
		if n < 3 {
			panic("transient boom")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("expected success on third attempt, got %v", err)
	}
	if got := attempts.Load(); got != 3 {
		t.Errorf("expected 3 attempts (2 panics + 1 success), got %d", got)
	}
}
