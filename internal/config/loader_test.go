package config

import (
	"strings"
	"testing"
)

func TestLoadConfig_InterpolatesBackupGCSFields(t *testing.T) {
	t.Setenv("TEST_PG_PASSWORD", "secret")
	t.Setenv("BACKUP_GCS_BUCKET", "backup-bucket")
	t.Setenv("BACKUP_GCS_PROJECT", "backup-project")
	t.Setenv("BACKUP_GCS_CREDS", "/tmp/backup-creds.json")

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
		"    database: app\n"+
		"    storage:\n"+
		"      type: gcs\n"+
		"      gcs_bucket: \"${BACKUP_GCS_BUCKET}\"\n"+
		"      gcs_project_id: \"${BACKUP_GCS_PROJECT}\"\n"+
		"      gcs_credentials_file: \"${BACKUP_GCS_CREDS}\"\n")

	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	job := cfg.Databases["pg"]
	if job.Storage.GCSBucket != "backup-bucket" {
		t.Fatalf("GCSBucket = %q", job.Storage.GCSBucket)
	}
	if job.Storage.GCSProjectID != "backup-project" {
		t.Fatalf("GCSProjectID = %q", job.Storage.GCSProjectID)
	}
	if job.Storage.GCSCredentialsFile != "/tmp/backup-creds.json" {
		t.Fatalf("GCSCredentialsFile = %q", job.Storage.GCSCredentialsFile)
	}
}

func TestLoadConfig_BackupGCSInterpolationMissingEnvFails(t *testing.T) {
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
		"    database: app\n"+
		"    storage:\n"+
		"      type: gcs\n"+
		"      gcs_bucket: \"${MISSING_BACKUP_GCS_BUCKET}\"\n")

	_, err := LoadConfig(cfgPath)
	if err == nil {
		t.Fatal("expected interpolation error")
	}
	if !strings.Contains(err.Error(), "MISSING_BACKUP_GCS_BUCKET") {
		t.Fatalf("expected missing env error, got %v", err)
	}
}

func TestLoadConfig_NamedStorageNotOverriddenByDefaults(t *testing.T) {
	t.Setenv("TEST_MONGO_URI", "mongodb://localhost:27017")
	t.Setenv("AWS_BUCKET", "sentinel")
	t.Setenv("AWS_ENDPOINT", "http://localhost:9000")
	t.Setenv("AWS_REGION", "us-east-1")
	t.Setenv("AWS_ACCESS_KEY", "key")
	t.Setenv("AWS_SECRET_KEY", "secret")

	cfgPath := writeTempConfig(t, "version: \"1.0\"\n"+
		"defaults:\n"+
		"  storage:\n"+
		"    type: local\n"+
		"    local_path: ./backups\n\n"+
		"storages:\n"+
		"  rustfs:\n"+
		"    type: s3\n"+
		"    s3_bucket: \"${AWS_BUCKET}\"\n"+
		"    s3_bucket_endpoint: \"${AWS_ENDPOINT}\"\n"+
		"    s3_region: \"${AWS_REGION}\"\n"+
		"    s3_access_key_id_env: AWS_ACCESS_KEY\n"+
		"    s3_secret_access_key_env: AWS_SECRET_KEY\n\n"+
		"databases:\n"+
		"  mongo-main:\n"+
		"    type: mongodb\n"+
		"    uri_env: TEST_MONGO_URI\n"+
		"    database: app\n"+
		"    storage:\n"+
		"      name: rustfs\n")

	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	job := cfg.Databases["mongo-main"]
	if job.Storage.Type != "s3" {
		t.Fatalf("Storage.Type = %q, want s3", job.Storage.Type)
	}
	if job.Storage.Name != "rustfs" {
		t.Fatalf("Storage.Name = %q, want rustfs", job.Storage.Name)
	}
	if job.Storage.S3Bucket != "sentinel" {
		t.Fatalf("S3Bucket = %q, want sentinel", job.Storage.S3Bucket)
	}
}
