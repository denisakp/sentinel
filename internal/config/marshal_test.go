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

func TestBuildAdvancedRestoreRequest_MapsMySQLReplayTargets(t *testing.T) {
	job := RestoreJob{
		RestoreMode:           "incremental",
		IncrementalFromBackup: "base-1",
		MySQL: MySQLRestoreConfig{
			BinlogTargetTime: "2026-03-20T23:59:00+00:00",
		},
	}

	request, err := BuildAdvancedRestoreRequest(job)
	if err != nil {
		t.Fatalf("BuildAdvancedRestoreRequest() error = %v", err)
	}
	if request.BinlogTargetTime != "2026-03-20T23:59:00+00:00" {
		t.Fatalf("BinlogTargetTime = %q", request.BinlogTargetTime)
	}
}

func TestBuildMySQLBinlogReplayArgs_MapsTargetTime(t *testing.T) {
	job := RestoreJob{
		Type:     "mysql",
		Host:     "127.0.0.1",
		Port:     3306,
		Username: "root",
		Database: "app",
		MySQL: MySQLRestoreConfig{
			BinlogTargetTime: "2026-03-20T23:59:00+00:00",
		},
	}

	args, err := BuildMySQLBinlogReplayArgs(job, "secret", []string{"/tmp/a.tar"})
	if err != nil {
		t.Fatalf("BuildMySQLBinlogReplayArgs() error = %v", err)
	}
	if args.Engine != "mysql" {
		t.Fatalf("Engine = %q", args.Engine)
	}
	if args.TargetTime != "2026-03-20T23:59:00+00:00" {
		t.Fatalf("TargetTime = %q", args.TargetTime)
	}
	if args.TargetPosition != nil {
		t.Fatalf("TargetPosition = %#v, want nil", args.TargetPosition)
	}
}

func TestBuildMySQLBinlogReplayArgs_MapsTargetPosition(t *testing.T) {
	job := RestoreJob{
		Type:     "mariadb",
		Host:     "127.0.0.1",
		Port:     3306,
		Username: "root",
		Database: "app",
		MySQL: MySQLRestoreConfig{
			BinlogTargetPosition: &BinlogTargetPosition{File: "mariadb-bin.000123", Pos: 2048},
		},
	}

	args, err := BuildMySQLBinlogReplayArgs(job, "secret", []string{"/tmp/a.tar", "/tmp/b.tar"})
	if err != nil {
		t.Fatalf("BuildMySQLBinlogReplayArgs() error = %v", err)
	}
	if args.Engine != "mariadb" {
		t.Fatalf("Engine = %q", args.Engine)
	}
	if args.TargetPosition == nil {
		t.Fatal("TargetPosition is nil")
	}
	if args.TargetPosition.File != "mariadb-bin.000123" || args.TargetPosition.Pos != 2048 {
		t.Fatalf("TargetPosition = %#v", args.TargetPosition)
	}
}

func TestNormalizeIncrementalBackupConfig_Defaults(t *testing.T) {
	job := BackupJob{
		IncrementalBackup: &IncrementalBackupConfig{Enabled: true},
	}

	normalized := NormalizeIncrementalBackupConfig(job)
	if normalized.IncrementalBackup == nil {
		t.Fatal("IncrementalBackup is nil")
	}
	if normalized.IncrementalBackup.MaxChainDepth != 6 {
		t.Fatalf("MaxChainDepth = %d, want 6", normalized.IncrementalBackup.MaxChainDepth)
	}
	if normalized.IncrementalBackup.OplogWindowWarnHours != 24 {
		t.Fatalf("OplogWindowWarnHours = %d, want 24", normalized.IncrementalBackup.OplogWindowWarnHours)
	}
}
