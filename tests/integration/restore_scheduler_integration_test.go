package integration

import (
	"context"
	"testing"
	"time"

	"github.com/denisakp/sentinel/internal/scheduler"
)

func TestRestoreSchedulerBasicFlow(t *testing.T) {
	mockRestoreFn := func(ctx context.Context, config *scheduler.RestoreExecutionConfig) (*scheduler.RestoreResult, error) {
		return &scheduler.RestoreResult{
			Success:       true,
			DatabaseType:  config.DatabaseType,
			BytesRestored: 1024,
			Duration:      100 * time.Millisecond,
			StartTime:     time.Now(),
			EndTime:       time.Now().Add(100 * time.Millisecond),
		}, nil
	}

	executor := scheduler.NewRestoreExecutor(mockRestoreFn)

	config := &scheduler.RestoreExecutionConfig{
		JobName:      "test-restore",
		DatabaseType: "postgres",
		Host:         "localhost",
		Port:         5432,
		Username:     "user",
		Database:     "testdb",
		BackupPath:   "/tmp/backup.sql",
	}

	result, err := executor.Execute(context.Background(), config)
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	if !result.Success {
		t.Errorf("Execute() success = false, want true")
	}

	if result.DatabaseType != "postgres" {
		t.Errorf("Execute() DatabaseType = %q, want postgres", result.DatabaseType)
	}
}

func TestRestoreSchedulerMultipleCalls(t *testing.T) {
	callCount := 0
	mockRestoreFn := func(ctx context.Context, config *scheduler.RestoreExecutionConfig) (*scheduler.RestoreResult, error) {
		callCount++
		return &scheduler.RestoreResult{
			Success:       true,
			BytesRestored: 1024,
			Duration:      10 * time.Millisecond,
			StartTime:     time.Now(),
			EndTime:       time.Now().Add(10 * time.Millisecond),
		}, nil
	}

	executor := scheduler.NewRestoreExecutor(mockRestoreFn)

	for i := 0; i < 3; i++ {
		config := &scheduler.RestoreExecutionConfig{
			JobName:      "test",
			DatabaseType: "postgres",
			Host:         "localhost",
			Port:         5432,
			Username:     "user",
			Database:     "db",
			BackupPath:   "/tmp/backup.sql",
		}

		result, err := executor.Execute(context.Background(), config)
		if err != nil {
			t.Errorf("Call %d failed: %v", i+1, err)
		}
		if !result.Success {
			t.Errorf("Call %d: success = false", i+1)
		}
	}

	if callCount != 3 {
		t.Errorf("Expected 3 calls, got %d", callCount)
	}
}
