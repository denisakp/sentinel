package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/denisakp/sentinel/internal/config"
	"github.com/denisakp/sentinel/internal/ports"
	internalrestore "github.com/denisakp/sentinel/internal/adapters/restore/runtime"
)

func TestRestoreExecutorValidation(t *testing.T) {
	tests := []struct {
		name     string
		config   *RestoreExecutionConfig
		wantErr  bool
		errMatch string
	}{
		{
			name:     "nil config",
			config:   nil,
			wantErr:  true,
			errMatch: "configuration cannot be nil",
		},
		{
			name: "missing job name",
			config: &RestoreExecutionConfig{
				DatabaseType: "postgres",
			},
			wantErr:  true,
			errMatch: "job name is required",
		},
		{
			name: "missing database type",
			config: &RestoreExecutionConfig{
				JobName: "test-restore",
			},
			wantErr:  true,
			errMatch: "database type is required",
		},
		{
			name: "invalid database type",
			config: &RestoreExecutionConfig{
				JobName:      "test-restore",
				DatabaseType: "oracle",
			},
			wantErr:  true,
			errMatch: "unsupported database type",
		},
		{
			name: "postgres missing host",
			config: &RestoreExecutionConfig{
				JobName:      "test-restore",
				DatabaseType: "postgres",
				Port:         5432,
				Username:     "user",
				Database:     "testdb",
			},
			wantErr:  true,
			errMatch: "host is required",
		},
		{
			name: "postgres missing username",
			config: &RestoreExecutionConfig{
				JobName:      "test-restore",
				DatabaseType: "postgres",
				Host:         "localhost",
				Port:         5432,
				Database:     "testdb",
			},
			wantErr:  true,
			errMatch: "username is required",
		},
		{
			name: "postgres missing database",
			config: &RestoreExecutionConfig{
				JobName:      "test-restore",
				DatabaseType: "postgres",
				Host:         "localhost",
				Port:         5432,
				Username:     "user",
			},
			wantErr:  true,
			errMatch: "database name is required",
		},
		{
			name: "mongodb missing uri",
			config: &RestoreExecutionConfig{
				JobName:      "test-restore",
				DatabaseType: "mongodb",
			},
			wantErr:  true,
			errMatch: "MongoDB URI is required",
		},
		{
			name: "missing backup source",
			config: &RestoreExecutionConfig{
				JobName:      "test-restore",
				DatabaseType: "postgres",
				Host:         "localhost",
				Port:         5432,
				Username:     "user",
				Database:     "testdb",
			},
			wantErr:  true,
			errMatch: "backup path or backup source is required",
		},
		{
			name: "invalid conflict strategy",
			config: &RestoreExecutionConfig{
				JobName:      "test-restore",
				DatabaseType: "postgres",
				Host:         "localhost",
				Port:         5432,
				Username:     "user",
				Database:     "testdb",
				BackupPath:   "/path/to/backup.sql",
				OnConflict:   "skip",
			},
			wantErr:  true,
			errMatch: "invalid conflict strategy",
		},
		{
			name: "valid postgres restore config",
			config: &RestoreExecutionConfig{
				JobName:      "test-restore",
				DatabaseType: "postgres",
				Host:         "localhost",
				Port:         5432,
				Username:     "user",
				Database:     "testdb",
				BackupPath:   "/path/to/backup.sql",
				OnConflict:   "ignore",
				Timeout:      30 * time.Second,
			},
			wantErr: false,
		},
		{
			name: "valid mongodb restore config",
			config: &RestoreExecutionConfig{
				JobName:      "test-restore",
				DatabaseType: "mongodb",
				URI:          "mongodb://localhost:27017",
				Database:     "testdb",
				BackupPath:   "/path/to/backup.archive",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRestoreConfig(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateRestoreConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && tt.errMatch != "" && err != nil {
				if !contains(err.Error(), tt.errMatch) {
					t.Errorf("validateRestoreConfig() error = %q, want to contain %q", err.Error(), tt.errMatch)
				}
			}
		})
	}
}

func TestRestoreScheduleConfig_Validation(t *testing.T) {
	tests := []struct {
		name    string
		config  *RestoreScheduleConfig
		wantErr bool
	}{
		{
			name:    "nil config",
			config:  nil,
			wantErr: true,
		},
		{
			name: "missing name",
			config: &RestoreScheduleConfig{
				Schedule: "0 2 * * *",
			},
			wantErr: true,
		},
		{
			name: "missing schedule",
			config: &RestoreScheduleConfig{
				Name: "test-restore",
			},
			wantErr: true,
		},
		{
			name: "disabled job",
			config: &RestoreScheduleConfig{
				Name:     "test-restore",
				Schedule: "0 2 * * *",
				Enabled:  false,
			},
			wantErr: true, // Would return early in real implementation
		},
		{
			name: "valid config",
			config: &RestoreScheduleConfig{
				Name:               "test-restore",
				Schedule:           "0 2 * * *",
				Enabled:            true,
				BackupPath:         "/backup/prod.sql",
				BackupSource:       "local",
				VerifyAfterRestore: true,
				RestoreConfig: &RestoreExecutionConfig{
					JobName:      "test-restore",
					DatabaseType: "postgres",
					Host:         "localhost",
					Port:         5432,
					Username:     "user",
					Database:     "testdb",
					OnConflict:   "error",
					BackupPath:   "/backup/prod.sql",
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.config == nil {
				if !tt.wantErr {
					t.Errorf("expected error for nil config")
				}
				return
			}

			err := validateRestoreConfig(tt.config.RestoreConfig)
			if err != nil && !tt.wantErr {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestRestoreResult(t *testing.T) {
	result := &RestoreResult{
		Success:            true,
		DatabaseType:       "postgres",
		BackupFile:         "/backup/prod.sql",
		RestoredDatabase:   "testdb",
		BytesRestored:      1024000,
		Duration:           30 * time.Second,
		StartTime:          time.Now().Add(-30 * time.Second),
		EndTime:            time.Now(),
		ErrorMessage:       "",
		VerificationPassed: true,
	}

	if !result.Success {
		t.Errorf("Expected successful restore result")
	}

	if result.Duration != 30*time.Second {
		t.Errorf("Expected duration 30s, got %v", result.Duration)
	}

	if result.BytesRestored != 1024000 {
		t.Errorf("Expected 1024000 bytes, got %d", result.BytesRestored)
	}
}

func TestRestoreExecutor_Execute(t *testing.T) {
	mockRestoreFn := func(ctx context.Context, config *RestoreExecutionConfig) (*RestoreResult, error) {
		return &RestoreResult{
			Success:       true,
			DatabaseType:  config.DatabaseType,
			BytesRestored: 1024,
			Duration:      1 * time.Second,
			StartTime:     time.Now(),
			EndTime:       time.Now().Add(1 * time.Second),
			ErrorMessage:  "",
		}, nil
	}

	executor := NewRestoreExecutor(mockRestoreFn)

	config := &RestoreExecutionConfig{
		JobName:      "test",
		DatabaseType: "postgres",
		Host:         "localhost",
		Port:         5432,
		Username:     "user",
		Database:     "testdb",
		BackupPath:   "/backup.sql",
	}

	result, err := executor.Execute(context.Background(), config)
	if err != nil {
		t.Fatalf("Execute() error = %v, want nil", err)
	}

	if !result.Success {
		t.Errorf("Execute() result.Success = false, want true")
	}

	if result.DatabaseType != "postgres" {
		t.Errorf("Execute() result.DatabaseType = %v, want postgres", result.DatabaseType)
	}
}

func TestExecuteScheduledRestore_SkipsOnConcurrencyLimit(t *testing.T) {
	limiter := make(chan struct{}, 1)
	limiter <- struct{}{}

	result, err := ExecuteScheduledRestore(context.Background(), &config.Configuration{}, "restore-job", config.RestoreJob{
		Type:     "postgres",
		Database: "db",
		BackupSource: config.RestoreBackupSource{
			Type:       "local",
			BackupPath: "backup.sql",
		},
	}, nil, limiter)
	if err != nil {
		t.Fatalf("ExecuteScheduledRestore() error = %v, want nil", err)
	}
	if result == nil {
		t.Fatal("ExecuteScheduledRestore() result = nil")
	}
	if result.Status != ports.StatusSkipped {
		t.Fatalf("result.Status = %q, want %q", result.Status, ports.StatusSkipped)
	}
	if result.Reason != "concurrency_limit_reached" {
		t.Fatalf("result.Reason = %q, want concurrency_limit_reached", result.Reason)
	}
}

func TestExecuteScheduledRestore_ConvertsLockConflictToSkip(t *testing.T) {
	prevRunner := runSharedRestoreExecution
	t.Cleanup(func() { runSharedRestoreExecution = prevRunner })

	runSharedRestoreExecution = func(_ context.Context, _ *internalrestore.ExecutionRequest) (*internalrestore.ExecutionResult, error) {
		return &internalrestore.ExecutionResult{
			Status: ports.StatusSkipped,
			Reason: "lock_conflict",
		}, internalrestore.ErrRestoreLockConflict
	}

	result, err := ExecuteScheduledRestore(context.Background(), &config.Configuration{}, "restore-job", config.RestoreJob{}, nil, make(chan struct{}, 1))
	if err != nil {
		t.Fatalf("ExecuteScheduledRestore() error = %v, want nil", err)
	}
	if result == nil || result.Status != ports.StatusSkipped || result.Reason != "lock_conflict" {
		t.Fatalf("unexpected skip result: %+v", result)
	}
}

// Helper function to check if string contains substr
func contains(str, substr string) bool {
	return len(str) >= len(substr) && stringContains(str, substr)
}

func stringContains(str, substr string) bool {
	for i := 0; i <= len(str)-len(substr); i++ {
		if str[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
