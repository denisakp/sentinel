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

func TestBuildAdvancedRestoreRequest_DefaultMode(t *testing.T) {
	request, err := BuildAdvancedRestoreRequest(RestoreJob{})
	if err != nil {
		t.Fatalf("BuildAdvancedRestoreRequest() error = %v", err)
	}
	if request.RestoreMode != "full" {
		t.Fatalf("RestoreMode = %q, want full", request.RestoreMode)
	}
	if request.PITRTimestampUTC != nil {
		t.Fatalf("PITRTimestampUTC = %v, want nil", request.PITRTimestampUTC)
	}
}

func TestBuildAdvancedRestoreRequest_PITRNormalizesUTC(t *testing.T) {
	job := RestoreJob{
		RestoreMode:        "pitr",
		PITRTimestamp:      "2026-03-20T23:59:00+02:00",
		PITRTargetTimeline: "5",
	}

	request, err := BuildAdvancedRestoreRequest(job)
	if err != nil {
		t.Fatalf("BuildAdvancedRestoreRequest() error = %v", err)
	}
	if request.PITRTimestampUTC == nil {
		t.Fatal("PITRTimestampUTC is nil")
	}
	if got := request.PITRTimestampUTC.Format("2006-01-02T15:04:05Z07:00"); got != "2026-03-20T21:59:00Z" {
		t.Fatalf("PITRTimestampUTC = %q, want 2026-03-20T21:59:00Z", got)
	}
}

func TestBuildAdvancedRestoreRequest_InvalidPITRTimestamp(t *testing.T) {
	_, err := BuildAdvancedRestoreRequest(RestoreJob{RestoreMode: "pitr", PITRTimestamp: "2026-03-20 23:59:00"})
	if err == nil {
		t.Fatal("expected parse error for invalid pitr timestamp")
	}
}
