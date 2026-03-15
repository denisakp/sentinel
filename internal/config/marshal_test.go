package config

import "testing"

func TestBuildStorageParams_MapsGCSFields(t *testing.T) {
	job := BackupJob{
		Output: "backup.sql",
		Storage: StorageConfig{
			Type:               "gcs",
			GCSBucket:          "backup-bucket",
			GCSProjectID:       "project-id",
			GCSCredentialsFile: "/tmp/sa.json",
		},
	}

	params := BuildStorageParams(job)
	if params.StorageType != "gcs" {
		t.Fatalf("StorageType = %q, want gcs", params.StorageType)
	}
	if params.GCSBucket != "backup-bucket" {
		t.Fatalf("GCSBucket = %q", params.GCSBucket)
	}
	if params.GCSProjectID != "project-id" {
		t.Fatalf("GCSProjectID = %q", params.GCSProjectID)
	}
	if params.GCSCredentialsFile != "/tmp/sa.json" {
		t.Fatalf("GCSCredentialsFile = %q", params.GCSCredentialsFile)
	}
}
