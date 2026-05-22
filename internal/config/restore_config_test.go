package config

import (
	"errors"
	"strings"
	"testing"

	"github.com/denisakp/sentinel/internal/backup"
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
				Name:       "test-restore",
				Type:       "postgres",
				StagingDir: "/tmp/sentinel",
				Host:       "localhost",
				Username:   "user",
				Database:   "testdb",
				BackupSource: RestoreBackupSource{
					Type:       "local",
					LocalPath:  "/backup",
					BackupPath: "prod.sql",
				},
			},
			wantErr: true,
			errMsg:  "restore schedule (cron) is required",
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
				Name:       "test-restore",
				Type:       "postgres",
				Schedule:   "0 2 * * *",
				StagingDir: "/tmp/sentinel",
				Host:       "localhost",
				Port:       5432,
				Username:   "user",
				Database:   "testdb",
				BackupSource: RestoreBackupSource{
					Type:       "local",
					LocalPath:  "/backup/prod.sql",
					BackupPath: "prod.sql",
				},
				ConflictStrategy: "error",
			},
			wantErr: false,
		},
		{
			name: "missing staging dir",
			job: &RestoreJob{
				Name:     "test-restore",
				Type:     "postgres",
				Schedule: "0 2 * * *",
				Host:     "localhost",
				Username: "user",
				Database: "testdb",
				BackupSource: RestoreBackupSource{
					Type:       "local",
					LocalPath:  "/backup/prod.sql",
					BackupPath: "prod.sql",
				},
			},
			wantErr: true,
			errMsg:  "staging_dir is required",
		},
		{
			name: "unsupported restore source",
			job: &RestoreJob{
				Name:       "test-restore",
				Type:       "postgres",
				Schedule:   "0 2 * * *",
				StagingDir: "/tmp/sentinel",
				Host:       "localhost",
				Username:   "user",
				Database:   "testdb",
				BackupSource: RestoreBackupSource{
					Type:       "azure",
					BackupPath: "prod.sql",
				},
			},
			wantErr: true,
			errMsg:  "unsupported backup_source.type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRestoreJob(tt.job)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateRestoreJob() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && tt.errMsg != "" && err != nil && !strings.Contains(err.Error(), tt.errMsg) {
				t.Fatalf("ValidateRestoreJob() error = %q, want to contain %q", err.Error(), tt.errMsg)
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

func TestRestoreBackupSource_S3_RequiresBucket(t *testing.T) {
	job := &RestoreJob{
		Name:       "test-s3-no-bucket",
		Type:       "postgres",
		Schedule:   "0 2 * * *",
		StagingDir: "/tmp/sentinel",
		Host:       "localhost",
		Username:   "user",
		Database:   "testdb",
		BackupSource: RestoreBackupSource{
			Type:       "s3",
			BackupPath: "postgres/prod.sql",
			// S3Bucket intentionally omitted
		},
	}
	err := ValidateRestoreJob(job)
	if err == nil {
		t.Fatal("expected error when s3_bucket is missing for s3 source type")
	}
	if !strings.Contains(err.Error(), "s3_bucket") {
		t.Errorf("expected error mentioning s3_bucket, got %q", err.Error())
	}
}

func TestRestoreBackupSource_GCS_RequiresBucket(t *testing.T) {
	job := &RestoreJob{
		Name:       "test-gcs-no-bucket",
		Type:       "postgres",
		Schedule:   "0 2 * * *",
		StagingDir: "/tmp/sentinel",
		Host:       "localhost",
		Username:   "user",
		Database:   "testdb",
		BackupSource: RestoreBackupSource{
			Type:       "gcs",
			BackupPath: "mysql/backup.sql",
			// GCSBucket intentionally omitted
		},
	}
	err := ValidateRestoreJob(job)
	if err == nil {
		t.Fatal("expected error when gcs_bucket is missing for gcs source type")
	}
	if !strings.Contains(err.Error(), "gcs_bucket") {
		t.Errorf("expected error mentioning gcs_bucket, got %q", err.Error())
	}
}

func TestRestoreBackupSource_Local_RequiresLocalPath(t *testing.T) {
	job := &RestoreJob{
		Name:       "test-local-no-path",
		Type:       "postgres",
		Schedule:   "0 2 * * *",
		StagingDir: "/tmp/sentinel",
		Host:       "localhost",
		Username:   "user",
		Database:   "testdb",
		BackupSource: RestoreBackupSource{
			Type:       "local",
			BackupPath: "prod.sql",
			// LocalPath intentionally omitted
		},
	}
	err := ValidateRestoreJob(job)
	if err == nil {
		t.Fatal("expected error when local_path is missing for local source type")
	}
	if !strings.Contains(err.Error(), "local_path") {
		t.Errorf("expected error mentioning local_path, got %q", err.Error())
	}
}

func TestRestoreBackupSource_UnsupportedType_RejectsAzure(t *testing.T) {
	job := &RestoreJob{
		Name:       "test-azure",
		Type:       "postgres",
		Schedule:   "0 2 * * *",
		StagingDir: "/tmp/sentinel",
		Host:       "localhost",
		Username:   "user",
		Database:   "testdb",
		BackupSource: RestoreBackupSource{
			Type:       "azure",
			BackupPath: "prod.sql",
		},
	}
	err := ValidateRestoreJob(job)
	if err == nil {
		t.Fatal("expected error for unsupported backup_source.type=azure")
	}
	if !strings.Contains(err.Error(), "unsupported backup_source.type") {
		t.Errorf("expected unsupported type error, got %q", err.Error())
	}
}

func TestRestoreBackupSource_UnsupportedType_RejectsFTP(t *testing.T) {
	job := &RestoreJob{
		Name:       "test-ftp",
		Type:       "postgres",
		Schedule:   "0 2 * * *",
		StagingDir: "/tmp/sentinel",
		Host:       "localhost",
		Username:   "user",
		Database:   "testdb",
		BackupSource: RestoreBackupSource{
			Type:       "ftp",
			BackupPath: "prod.sql",
		},
	}
	err := ValidateRestoreJob(job)
	if err == nil {
		t.Fatal("expected error for unsupported backup_source.type=ftp")
	}
}

func TestRestoreBackupSource_MissingBackupPath_Fails(t *testing.T) {
	job := &RestoreJob{
		Name:       "test-no-backup-path",
		Type:       "postgres",
		Schedule:   "0 2 * * *",
		StagingDir: "/tmp/sentinel",
		Host:       "localhost",
		Username:   "user",
		Database:   "testdb",
		BackupSource: RestoreBackupSource{
			Type:      "local",
			LocalPath: "/backups",
			// BackupPath intentionally omitted
		},
	}
	err := ValidateRestoreJob(job)
	if err == nil {
		t.Fatal("expected error when backup_path is missing")
	}
	if !strings.Contains(err.Error(), "backup_path") {
		t.Errorf("expected error mentioning backup_path, got %q", err.Error())
	}
}

func TestRestoreBackupSource_S3_ValidWithBucket(t *testing.T) {
	job := &RestoreJob{
		Name:       "test-s3-valid",
		Type:       "postgres",
		Schedule:   "0 2 * * *",
		StagingDir: "/tmp/sentinel",
		Host:       "localhost",
		Username:   "user",
		Database:   "testdb",
		BackupSource: RestoreBackupSource{
			Type:       "s3",
			S3Bucket:   "my-backup-bucket",
			BackupPath: "postgres/prod.sql",
		},
	}
	if err := ValidateRestoreJob(job); err != nil {
		t.Errorf("expected valid s3 source to pass, got error: %v", err)
	}
}

func TestRestoreBackupSource_GCS_ValidWithBucket(t *testing.T) {
	job := &RestoreJob{
		Name:       "test-gcs-valid",
		Type:       "postgres",
		Schedule:   "0 2 * * *",
		StagingDir: "/tmp/sentinel",
		Host:       "localhost",
		Username:   "user",
		Database:   "testdb",
		BackupSource: RestoreBackupSource{
			Type:       "gcs",
			GCSBucket:  "backup-bucket",
			BackupPath: "postgres/prod.sql",
		},
	}
	if err := ValidateRestoreJob(job); err != nil {
		t.Errorf("expected valid gcs source to pass, got error: %v", err)
	}
}

func TestLoadConfig_AppliesRestoreStagingDefaults(t *testing.T) {
	t.Setenv("TEST_PG_PASSWORD", "secret")

	cfgPath := writeTempConfig(t, "version: \"1.0\"\n"+
		"restore:\n"+
		"  staging_dir: /tmp/sentinel\n"+
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
		"      type: local\n"+
		"      local_path: ./backups\n"+
		"      backup_path: dumps/latest.sql\n")

	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	job := cfg.Restores["pg-restore"]
	if job.StagingDir != "/tmp/sentinel" {
		t.Fatalf("StagingDir = %q, want /tmp/sentinel", job.StagingDir)
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

func TestRestoreJobValidation_AdditionalArgs(t *testing.T) {
	validJob := func(opts map[string]interface{}) *RestoreJob {
		return &RestoreJob{
			Name:       "rj",
			Schedule:   "0 2 * * *",
			Type:       "postgres",
			StagingDir: "/tmp/sentinel",
			Host:       "localhost",
			Username:   "user",
			Database:   "testdb",
			BackupSource: RestoreBackupSource{
				Type:       "local",
				LocalPath:  "/backup",
				BackupPath: "prod.sql",
			},
			RestoreOptions: opts,
		}
	}

	tests := []struct {
		name      string
		opts      map[string]interface{}
		wantErrIs error
		errSub    string
	}{
		{
			name: "valid additional_args passes",
			opts: map[string]interface{}{"additional_args": `--exclude-table-data="audit logs"`},
		},
		{
			name: "no additional_args field passes",
			opts: nil,
		},
		{
			name:      "unterminated quote rejected",
			opts:      map[string]interface{}{"additional_args": `--where="x > 1`},
			wantErrIs: backup.ErrUnterminatedQuote,
			errSub:    "restore_options.additional_args",
		},
		{
			name:   "non-string additional_args rejected",
			opts:   map[string]interface{}{"additional_args": 42},
			errSub: "must be a string",
		},
		{
			name:      "NUL byte rejected",
			opts:      map[string]interface{}{"additional_args": "--foo\x00bar"},
			wantErrIs: backup.ErrNULByte,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRestoreJob(validJob(tt.opts))
			if tt.wantErrIs == nil && tt.errSub == "" {
				if err != nil {
					t.Fatalf("expected no error, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			if tt.wantErrIs != nil && !errors.Is(err, tt.wantErrIs) {
				t.Fatalf("expected errors.Is(err, %v), got %v", tt.wantErrIs, err)
			}
			if tt.errSub != "" && !strings.Contains(err.Error(), tt.errSub) {
				t.Fatalf("expected error to contain %q, got %q", tt.errSub, err.Error())
			}
		})
	}
}
