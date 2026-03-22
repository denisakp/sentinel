package config

import (
	"strings"
	"testing"
)

func TestValidateConfig_GCSStorageRequiresBucket(t *testing.T) {
	enabled := true
	cfg := &Configuration{
		Version:              "1.0",
		MaxConcurrentBackups: 1,
		Databases: map[string]BackupJob{
			"pg": {
				Name:        "pg",
				Type:        "postgres",
				Enabled:     &enabled,
				Host:        "localhost",
				Username:    "sentinel",
				PasswordEnv: "TEST_PG_PASSWORD",
				Database:    "app",
				Storage: StorageConfig{
					Type: "gcs",
				},
			},
		},
	}

	err := ValidateConfig(cfg)
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "bucket") {
		t.Fatalf("expected bucket validation error, got %v", err)
	}
}

func TestValidateConfig_AdvancedRestoreRules(t *testing.T) {
	tests := []struct {
		name        string
		mutate      func(*RestoreJob)
		wantErrPart string
	}{
		{
			name: "pitr requires postgres",
			mutate: func(job *RestoreJob) {
				job.Type = "mysql"
				job.RestoreMode = "pitr"
				job.PITRTimestamp = "2026-03-20T23:59:00Z"
			},
			wantErrPart: "supported only for postgres",
		},
		{
			name: "pitr requires timezone timestamp",
			mutate: func(job *RestoreJob) {
				job.RestoreMode = "pitr"
				job.PITRTimestamp = "2026-03-20T23:59:00"
			},
			wantErrPart: "RFC3339 with timezone",
		},
		{
			name: "incremental requires baseline",
			mutate: func(job *RestoreJob) {
				job.RestoreMode = "incremental"
				job.IncrementalFromBackup = ""
			},
			wantErrPart: "incremental_from_backup is required",
		},
		{
			name: "invalid mode rejected",
			mutate: func(job *RestoreJob) {
				job.RestoreMode = "snapshot"
			},
			wantErrPart: "restore_mode must be one of",
		},
		{
			name: "confirm fallback only with incremental",
			mutate: func(job *RestoreJob) {
				job.RestoreMode = "full"
				job.ConfirmFullFallback = true
			},
			wantErrPart: "confirm_full_fallback is only valid",
		},
		{
			name: "valid pitr accepted",
			mutate: func(job *RestoreJob) {
				job.RestoreMode = "pitr"
				job.PITRTimestamp = "2026-03-20T23:59:00+00:00"
			},
			wantErrPart: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := baseConfigWithRestore()
			job := cfg.Restores["restore_job"]
			tt.mutate(&job)
			cfg.Restores["restore_job"] = job

			err := ValidateConfig(cfg)
			if tt.wantErrPart == "" {
				if err != nil {
					t.Fatalf("ValidateConfig() unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q", tt.wantErrPart)
			}
			if !strings.Contains(err.Error(), tt.wantErrPart) {
				t.Fatalf("expected error containing %q, got %v", tt.wantErrPart, err)
			}
		})
	}
}

func baseConfigWithRestore() *Configuration {
	enabled := true
	restoreEnabled := true

	return &Configuration{
		Version:              "1.0",
		MaxConcurrentBackups: 1,
		Databases: map[string]BackupJob{
			"backup_job": {
				Name:        "backup_job",
				Type:        "postgres",
				Enabled:     &enabled,
				Host:        "localhost",
				Username:    "sentinel",
				PasswordEnv: "PG_PASSWORD",
				Database:    "appdb",
				Storage:     StorageConfig{Type: "local", LocalPath: "/tmp"},
				Schedule:    "0 1 * * *",
			},
		},
		Restores: map[string]RestoreJob{
			"restore_job": {
				Enabled:    &restoreEnabled,
				Type:       "postgres",
				Host:       "localhost",
				Username:   "sentinel",
				Database:   "appdb",
				Schedule:   "0 2 * * *",
				StagingDir: "/tmp",
				BackupSource: RestoreBackupSource{
					Type:       "local",
					LocalPath:  "/tmp",
					BackupPath: "backup.sql",
				},
			},
		},
	}
}
