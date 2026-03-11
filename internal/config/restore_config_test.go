package config

import (
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
