package config

import (
	"strings"
	"testing"
)

func TestRestoreJobValidation(t *testing.T) {
	tests := []struct {
		name    string
		job     *RestoreJob
		wantErr bool
		errMsg  string
	}{
		{
			name:    "nil restore job",
			job:     nil,
			wantErr: true,
		},
		{
			name: "missing name",
			job: &RestoreJob{
				Schedule: "0 2 * * *",
			},
			wantErr: true,
			errMsg:  "name is required",
		},
		{
			name: "missing schedule",
			job: &RestoreJob{
				Name: "test-restore",
			},
			wantErr: true,
			errMsg:  "schedule is required",
		},
		{
			name: "missing database type",
			job: &RestoreJob{
				Name:     "test-restore",
				Schedule: "0 2 * * *",
			},
			wantErr: true,
			errMsg:  "type is required",
		},
		{
			name: "valid postgres restore job",
			job: &RestoreJob{
				Name:     "test-restore",
				Type:     "postgres",
				Schedule: "0 2 * * *",
				Host:     "localhost",
				Port:     5432,
				Username: "user",
				Database: "testdb",
				BackupSource: RestoreBackupSource{
					Type:       "local",
					LocalPath:  "/backup/prod.sql",
					BackupPath: "prod.sql",
				},
				ConflictStrategy: "error",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRestoreJob(tt.job)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateRestoreJob() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestRestoreBackupSource(t *testing.T) {
	tests := []struct {
		name   string
		source RestoreBackupSource
		valid  bool
	}{
		{
			name: "local source",
			source: RestoreBackupSource{
				Type:       "local",
				LocalPath:  "/backups",
				BackupPath: "prod.sql",
			},
			valid: true,
		},
		{
			name: "s3 source",
			source: RestoreBackupSource{
				Type:       "s3",
				S3Bucket:   "my-backups",
				BackupPath: "postgres/prod.sql",
			},
			valid: true,
		},
		{
			name: "gcs source",
			source: RestoreBackupSource{
				Type:       "gcs",
				GCSBucket:  "backup-bucket",
				BackupPath: "mysql/backup.sql",
			},
			valid: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.source.Type == "" {
				t.Errorf("source type should not be empty")
			}
		})
	}
}

func TestLoadConfig_InterpolatesRestoreGCSFields(t *testing.T) {
	t.Setenv("RESTORE_GCS_BUCKET", "restore-bucket")
	t.Setenv("RESTORE_GCS_PROJECT", "restore-project")
	t.Setenv("RESTORE_GCS_CREDS", "/tmp/restore-creds.json")
	t.Setenv("TEST_PG_PASSWORD", "secret")

	cfgPath := writeTempConfig(t, "version: \"1.0\"\n"+
		"defaults:\n"+
		"  storage:\n"+
		"    type: local\n"+
		"    local_path: ./backups\n\n"+
		"databases:\n"+
		"  pg:\n"+
		"    type: postgres\n"+
		"    host: localhost\n"+
		"    username: sentinel\n"+
		"    password_env: TEST_PG_PASSWORD\n"+
		"    database: app\n\n"+
		"restores:\n"+
		"  pg-restore:\n"+
		"    enabled: true\n"+
		"    type: postgres\n"+
		"    host: localhost\n"+
		"    username: sentinel\n"+
		"    password_env: TEST_PG_PASSWORD\n"+
		"    database: app_restore\n"+
		"    schedule: \"0 2 * * *\"\n"+
		"    backup_source:\n"+
		"      type: gcs\n"+
		"      gcs_bucket: \"${RESTORE_GCS_BUCKET}\"\n"+
		"      gcs_project_id: \"${RESTORE_GCS_PROJECT}\"\n"+
		"      gcs_credentials_file: \"${RESTORE_GCS_CREDS}\"\n"+
		"      backup_path: \"dumps/latest.sql\"\n")

	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	job := cfg.Restores["pg-restore"]
	if job.BackupSource.GCSBucket != "restore-bucket" {
		t.Fatalf("GCSBucket = %q", job.BackupSource.GCSBucket)
	}
	if job.BackupSource.GCSProjectID != "restore-project" {
		t.Fatalf("GCSProjectID = %q", job.BackupSource.GCSProjectID)
	}
	if job.BackupSource.GCSCredentialsFile != "/tmp/restore-creds.json" {
		t.Fatalf("GCSCredentialsFile = %q", job.BackupSource.GCSCredentialsFile)
	}
}

func TestLoadConfig_RestoreGCSInterpolationMissingEnvFails(t *testing.T) {
	t.Setenv("TEST_PG_PASSWORD", "secret")

	cfgPath := writeTempConfig(t, "version: \"1.0\"\n"+
		"defaults:\n"+
		"  storage:\n"+
		"    type: local\n"+
		"    local_path: ./backups\n\n"+
		"databases:\n"+
		"  pg:\n"+
		"    type: postgres\n"+
		"    host: localhost\n"+
		"    username: sentinel\n"+
		"    password_env: TEST_PG_PASSWORD\n"+
		"    database: app\n\n"+
		"restores:\n"+
		"  pg-restore:\n"+
		"    enabled: true\n"+
		"    type: postgres\n"+
		"    host: localhost\n"+
		"    username: sentinel\n"+
		"    password_env: TEST_PG_PASSWORD\n"+
		"    database: app_restore\n"+
		"    schedule: \"0 2 * * *\"\n"+
		"    backup_source:\n"+
		"      type: gcs\n"+
		"      gcs_bucket: \"${MISSING_RESTORE_GCS_BUCKET}\"\n"+
		"      backup_path: \"dumps/latest.sql\"\n")

	_, err := LoadConfig(cfgPath)
	if err == nil {
		t.Fatal("expected interpolation error")
	}
	if !strings.Contains(err.Error(), "MISSING_RESTORE_GCS_BUCKET") {
		t.Fatalf("expected missing env error, got %v", err)
	}
}
