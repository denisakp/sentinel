package restore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/denisakp/sentinel/internal/config"
	"github.com/denisakp/sentinel/internal/manifest"
	pgrestore "github.com/denisakp/sentinel/pkg/restore/pg_restore"
)

func TestExecuteRestoreReturnsLockConflict(t *testing.T) {
	root := t.TempDir()
	lockDir := filepath.Join(root, "locks")
	if err := os.MkdirAll(lockDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	lockPath := filepath.Join(lockDir, "restore.lock")
	if err := os.WriteFile(lockPath, []byte("locked"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := ExecuteRestore(context.Background(), &ExecutionRequest{
		JobName: "restore",
		LockDir: lockDir,
		Job: config.RestoreJob{
			Type:        "postgres",
			Name:        "restore",
			Host:        "localhost",
			Username:    "postgres",
			PasswordEnv: "PGPASSWORD",
			Database:    "db",
			Schedule:    "0 2 * * *",
			StagingDir:  root,
			BackupSource: config.RestoreBackupSource{
				Type:       "local",
				LocalPath:  root,
				BackupPath: "backup.sql",
			},
		},
	})
	if !errors.Is(err, ErrRestoreLockConflict) {
		t.Fatalf("ExecuteRestore() error = %v, want ErrRestoreLockConflict", err)
	}
}

func TestExecuteEngineRestoreRoutesPITRPath(t *testing.T) {
	t.Setenv("PGPASSWORD", "test-password")

	originalPITR := runPostgresPITR
	originalRestore := runPostgresRestore
	t.Cleanup(func() {
		runPostgresPITR = originalPITR
		runPostgresRestore = originalRestore
	})

	pitrCalled := false
	runPostgresPITR = func(ctx context.Context, job config.RestoreJob, password, stagedPath string) error {
		pitrCalled = true
		return nil
	}
	runPostgresRestore = func(ctx context.Context, args *pgrestore.RestoreArgs) error {
		t.Fatalf("unexpected pg_restore path")
		return nil
	}

	err := executeEngineRestore(context.Background(), config.RestoreJob{
		Type:        "postgres",
		RestoreMode: "pitr",
		Host:        "localhost",
		Port:        5432,
		Username:    "postgres",
		PasswordEnv: "PGPASSWORD",
		Database:    "app",
	}, "staged.sql")
	if err != nil {
		t.Fatalf("executeEngineRestore() error = %v", err)
	}
	if !pitrCalled {
		t.Fatal("expected PITR path to be called")
	}
}

func TestExecuteRestoreRequiresVerificationForPITR(t *testing.T) {
	t.Setenv("PGPASSWORD", "test-password")

	originalStage := stageRestoreSource
	originalPreflight := applyRestorePreflight
	originalEngine := executeRestoreEngine
	originalPITR := runPostgresPITR
	t.Cleanup(func() {
		stageRestoreSource = originalStage
		applyRestorePreflight = originalPreflight
		executeRestoreEngine = originalEngine
		runPostgresPITR = originalPITR
	})

	tmpDir := t.TempDir()
	backupPath := filepath.Join(tmpDir, "backup.sql")
	if err := os.WriteFile(backupPath, []byte("backup"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	target := time.Date(2026, 3, 20, 22, 0, 0, 0, time.UTC)
	start := time.Date(2026, 3, 20, 20, 0, 0, 0, time.UTC)
	end := time.Date(2026, 3, 20, 23, 0, 0, 0, time.UTC)
	manifestPath := filepath.Join(tmpDir, "backup.manifest.json")
	if err := manifest.WriteManifest(manifestPath, &manifest.BackupManifest{
		BackupID:     "base-001",
		Database:     "app",
		DatabaseType: "postgres",
		CreatedAt:    start,
		SizeBytes:    6,
		Hash: manifest.HashInfo{
			Algorithm: "sha256",
			Value:     "d045eb8a208bdba0a4a4dbd2cff08f7a2c12f00339e9d9ed9af08e717dfbd86c",
		},
		AdvancedRestore: &manifest.AdvancedRestoreMetadata{
			Capabilities:                  []string{"full", "pitr"},
			RecoverableWindowStartUTC:     &start,
			RecoverableWindowEndUTC:       &end,
			RequiresIntegrityVerification: true,
		},
	}); err != nil {
		t.Fatalf("WriteManifest() error = %v", err)
	}

	stageRestoreSource = func(ctx context.Context, job config.RestoreJob) (*StagedArtifact, error) {
		return &StagedArtifact{Path: backupPath, ManifestPath: manifestPath, SourcePath: "backup.sql", SizeBytes: 6}, nil
	}
	applyRestorePreflight = func(ctx context.Context, cfg *config.Configuration, artifact *StagedArtifact) (string, error) {
		return artifact.Path, nil
	}
	executeRestoreEngine = func(ctx context.Context, job config.RestoreJob, stagedPath string) error {
		return nil
	}
	runPostgresPITR = func(ctx context.Context, job config.RestoreJob, password, stagedPath string) error {
		return nil
	}

	result, err := ExecuteRestore(context.Background(), &ExecutionRequest{
		JobName: "restore",
		Job: config.RestoreJob{
			Type:          "postgres",
			RestoreMode:   "pitr",
			PITRTimestamp: target.Format(time.RFC3339),
			Host:          "localhost",
			Username:      "postgres",
			PasswordEnv:   "PGPASSWORD",
			Database:      "app",
			Schedule:      "0 2 * * *",
			StagingDir:    tmpDir,
			BackupSource: config.RestoreBackupSource{
				Type:       "local",
				LocalPath:  tmpDir,
				BackupPath: "backup.sql",
			},
		},
	})
	if err == nil {
		t.Fatal("expected verification-required error")
	}
	if !strings.Contains(err.Error(), "verification handler is required") {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil || result.Status != "failed" {
		t.Fatalf("expected failed result, got %#v", result)
	}
}
