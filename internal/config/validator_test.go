package config

import (
	"os"
	"path/filepath"
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
			// Was "incremental mysql allowed", asserting this validated cleanly.
			// That encoded #186: the planner rejects any engine but postgres with
			// ReasonCodeUnsupportedDatabaseType, so a clean validation here meant
			// the failure arrived at `restore run` instead. A test that pins a
			// defect makes it look deliberate to the next reader.
			name: "incremental rejected for mysql, the planner cannot run it",
			mutate: func(job *RestoreJob) {
				job.Type = "mysql"
				job.RestoreMode = "incremental"
				job.IncrementalFromBackup = "baseline-1"
			},
			wantErrPart: "supported only for postgres",
		},
		{
			name: "binlog selectors mutually exclusive",
			mutate: func(job *RestoreJob) {
				job.Type = "mysql"
				job.RestoreMode = "incremental"
				job.IncrementalFromBackup = "baseline-1"
				job.MySQL.BinlogTargetTime = "2026-03-20T23:59:00+00:00"
				job.MySQL.BinlogTargetPosition = &BinlogTargetPosition{File: "mysql-bin.000001", Pos: 100}
			},
			wantErrPart: "mutually exclusive",
		},
		{
			name: "binlog selector only for mysql",
			mutate: func(job *RestoreJob) {
				job.Type = "postgres"
				job.MySQL.BinlogTargetTime = "2026-03-20T23:59:00+00:00"
			},
			wantErrPart: "only valid for mysql or mariadb",
		},
		{
			// Was "mariadb binlog target position accepted", expecting no error.
			// The position itself is well formed and passes its own validation;
			// what fails is the mode, because only postgres can be planned
			// incrementally (#186).
			name: "incremental rejected for mariadb even with a valid binlog position",
			mutate: func(job *RestoreJob) {
				job.Type = "mariadb"
				job.RestoreMode = "incremental"
				job.IncrementalFromBackup = "baseline-1"
				job.MySQL.BinlogTargetPosition = &BinlogTargetPosition{File: "mariadb-bin.000101", Pos: 1234}
			},
			wantErrPart: "supported only for postgres",
		},
		{
			name: "binlog target position requires file and pos",
			mutate: func(job *RestoreJob) {
				job.Type = "mysql"
				job.RestoreMode = "incremental"
				job.IncrementalFromBackup = "baseline-1"
				job.MySQL.BinlogTargetPosition = &BinlogTargetPosition{File: "", Pos: 0}
			},
			wantErrPart: "requires both file and pos > 0",
		},
		{
			name: "binlog target time requires RFC3339 timezone",
			mutate: func(job *RestoreJob) {
				job.Type = "mysql"
				job.RestoreMode = "incremental"
				job.IncrementalFromBackup = "baseline-1"
				job.MySQL.BinlogTargetTime = "2026-03-20 23:59:00"
			},
			wantErrPart: "RFC3339 with timezone",
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

func TestValidateConfig_IncrementalBackupPolicyRules(t *testing.T) {
	enabled := true
	cfg := &Configuration{
		Version:              "1.0",
		MaxConcurrentBackups: 1,
		Databases: map[string]BackupJob{
			"mysql_job": {
				Name:        "mysql_job",
				Type:        "mysql",
				Enabled:     &enabled,
				Host:        "localhost",
				Username:    "sentinel",
				PasswordEnv: "MYSQL_PASSWORD",
				Database:    "appdb",
				Storage:     StorageConfig{Type: "local", LocalPath: "/tmp"},
				Schedule:    "0 1 * * *",
				IncrementalBackup: &IncrementalBackupConfig{
					Enabled: true,
				},
			},
		},
	}

	err := ValidateConfig(cfg)
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "mysql.binlog_path is required") {
		t.Fatalf("expected binlog_path validation error, got %v", err)
	}
}

func TestValidateConfig_IncrementalBackupPolicyRequiresMountedBinlogPath(t *testing.T) {
	enabled := true
	cfg := &Configuration{
		Version:              "1.0",
		MaxConcurrentBackups: 1,
		Databases: map[string]BackupJob{
			"mysql_job": {
				Name:        "mysql_job",
				Type:        "mysql",
				Enabled:     &enabled,
				Host:        "localhost",
				Username:    "sentinel",
				PasswordEnv: "MYSQL_PASSWORD",
				Database:    "appdb",
				Storage:     StorageConfig{Type: "local", LocalPath: "/tmp"},
				Schedule:    "0 1 * * *",
				MySQL:       MySQLConfig{BinlogPath: filepath.Join(t.TempDir(), "missing-binlogs")},
				IncrementalBackup: &IncrementalBackupConfig{
					Enabled: true,
				},
			},
		},
	}

	err := ValidateConfig(cfg)
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "must exist and be locally mounted") {
		t.Fatalf("expected mounted binlog_path validation error, got %v", err)
	}
}

func TestValidateConfig_IncrementalBackupPolicyAcceptsMountedBinlogPath(t *testing.T) {
	enabled := true
	binlogDir := filepath.Join(t.TempDir(), "binlogs")
	if err := os.MkdirAll(binlogDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	cfg := &Configuration{
		Version:              "1.0",
		MaxConcurrentBackups: 1,
		Databases: map[string]BackupJob{
			"mysql_job": {
				Name:        "mysql_job",
				Type:        "mysql",
				Enabled:     &enabled,
				Host:        "localhost",
				Username:    "sentinel",
				PasswordEnv: "MYSQL_PASSWORD",
				Database:    "appdb",
				Storage:     StorageConfig{Type: "local", LocalPath: "/tmp"},
				Schedule:    "0 1 * * *",
				MySQL:       MySQLConfig{BinlogPath: binlogDir},
				IncrementalBackup: &IncrementalBackupConfig{
					Enabled: true,
				},
			},
		},
	}

	err := ValidateConfig(cfg)
	if err != nil {
		t.Fatalf("ValidateConfig() unexpected error: %v", err)
	}
}

func TestValidateConfig_GFSRetention(t *testing.T) {
	enabled := true
	base := func(rp RetentionPolicy) *Configuration {
		return &Configuration{
			Version:              "1.0",
			MaxConcurrentBackups: 1,
			Databases: map[string]BackupJob{
				"pg": {
					Name:        "pg",
					Type:        "postgres",
					Enabled:     &enabled,
					Host:        "localhost",
					Username:    "sentinel",
					PasswordEnv: "PG_PASSWORD",
					Database:    "appdb",
					Schedule:    "0 1 * * *",
					Storage:     StorageConfig{Type: "local", LocalPath: "/tmp"},
					Retention:   rp,
				},
			},
		}
	}

	t.Run("negative tier rejected", func(t *testing.T) {
		err := ValidateConfig(base(RetentionPolicy{GFS: &GFSPolicy{KeepDaily: -1}}))
		if err == nil || !strings.Contains(err.Error(), "must be >= 0") {
			t.Fatalf("expected negative GFS tier rejected, got %v", err)
		}
	})

	t.Run("gfs-only policy valid", func(t *testing.T) {
		if err := ValidateConfig(base(RetentionPolicy{GFS: &GFSPolicy{KeepMonthly: 12}})); err != nil {
			t.Fatalf("GFS-only policy must be valid, got %v", err)
		}
	})

	t.Run("flat-zero plus gfs valid", func(t *testing.T) {
		rp := RetentionPolicy{DryRun: true, GFS: &GFSPolicy{KeepDaily: 7}}
		if err := ValidateConfig(base(rp)); err != nil {
			t.Fatalf("dry-run + GFS (flat zero) must be valid, got %v", err)
		}
	})

	t.Run("all-zero gfs with flat-zero dry-run still errors", func(t *testing.T) {
		rp := RetentionPolicy{DryRun: true, GFS: &GFSPolicy{}}
		if err := ValidateConfig(base(rp)); err == nil {
			t.Fatal("expected error: dry-run with no flat rule and empty GFS")
		}
	})
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

func TestValidateConfig_MaxConcurrentRestoresRange(t *testing.T) {
	base := func(v int) *Configuration {
		return &Configuration{
			Version:               "1.0",
			MaxConcurrentBackups:  1,
			MaxConcurrentRestores: v,
			Databases: map[string]BackupJob{
				"db": {Type: "postgres", Host: "h", Username: "u", PasswordEnv: "PW", Database: "d", Schedule: "0 2 * * *", Storage: StorageConfig{Type: "local", LocalPath: "/tmp"}},
			},
		}
	}
	if err := ValidateConfig(base(-1)); err == nil {
		t.Error("negative max_concurrent_restores must be rejected")
	}
	if err := ValidateConfig(base(101)); err == nil {
		t.Error("max_concurrent_restores > 100 must be rejected")
	}
	if err := ValidateConfig(base(4)); err != nil {
		t.Errorf("valid max_concurrent_restores rejected: %v", err)
	}
	if err := ValidateConfig(base(0)); err != nil {
		t.Errorf("unset (0) max_concurrent_restores must pass (loader defaults it): %v", err)
	}
}
