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
	"github.com/denisakp/sentinel/internal/monitor"
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
	applyRestorePreflight = func(ctx context.Context, cfg *config.Configuration, artifact *StagedArtifact, _ bool) (string, error) {
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

func TestExecuteRestoreFallbackConfirmationRequired(t *testing.T) {
	t.Setenv("PGPASSWORD", "test-password")

	originalStage := stageRestoreSource
	originalPreflight := applyRestorePreflight
	originalEngine := executeRestoreEngine
	t.Cleanup(func() {
		stageRestoreSource = originalStage
		applyRestorePreflight = originalPreflight
		executeRestoreEngine = originalEngine
	})

	tmpDir := t.TempDir()
	backupPath := filepath.Join(tmpDir, "backup.sql")
	if err := os.WriteFile(backupPath, []byte("backup"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	manifestPath := filepath.Join(tmpDir, "backup.manifest.json")
	if err := manifest.WriteManifest(manifestPath, &manifest.BackupManifest{
		BackupID:     "incr-003",
		Database:     "app",
		DatabaseType: "postgres",
		CreatedAt:    time.Now().UTC(),
		SizeBytes:    6,
		Hash: manifest.HashInfo{
			Algorithm: "sha256",
			Value:     "d045eb8a208bdba0a4a4dbd2cff08f7a2c12f00339e9d9ed9af08e717dfbd86c",
		},
		AdvancedRestore: &manifest.AdvancedRestoreMetadata{
			Capabilities: []string{"incremental"},
			IncrementalLineage: &manifest.IncrementalLineageMetadata{
				BaselineBackupID:   "base-001",
				ExecutionSupported: false,
			},
		},
	}); err != nil {
		t.Fatalf("WriteManifest() error = %v", err)
	}

	stageRestoreSource = func(ctx context.Context, job config.RestoreJob) (*StagedArtifact, error) {
		return &StagedArtifact{Path: backupPath, ManifestPath: manifestPath, SourcePath: "backup.sql", SizeBytes: 6}, nil
	}
	applyRestorePreflight = func(ctx context.Context, cfg *config.Configuration, artifact *StagedArtifact, _ bool) (string, error) {
		return artifact.Path, nil
	}
	executeRestoreEngine = func(ctx context.Context, job config.RestoreJob, stagedPath string) error {
		t.Fatal("executeRestoreEngine should not run when fallback confirmation is required")
		return nil
	}

	result, err := ExecuteRestore(context.Background(), &ExecutionRequest{
		JobName: "restore",
		Job: config.RestoreJob{
			Type:                  "postgres",
			RestoreMode:           "incremental",
			IncrementalFromBackup: "base-001",
			ConfirmFullFallback:   false,
			Host:                  "localhost",
			Username:              "postgres",
			PasswordEnv:           "PGPASSWORD",
			Database:              "app",
			Schedule:              "0 2 * * *",
			StagingDir:            tmpDir,
			BackupSource: config.RestoreBackupSource{
				Type:       "local",
				LocalPath:  tmpDir,
				BackupPath: "backup.sql",
			},
		},
	})
	if err == nil {
		t.Fatal("expected confirmation-required error")
	}
	if !strings.Contains(err.Error(), "fallback_required_confirmation") {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("result is nil")
	}
	if result.Status != monitor.StatusSkipped {
		t.Fatalf("Status = %q, want %q", result.Status, monitor.StatusSkipped)
	}
	if result.Reason != ReasonCodeFullFallbackConfirmationRequired {
		t.Fatalf("Reason = %q", result.Reason)
	}
}

func TestExecuteRestore_CleansAssembledArtifactsOnEngineFailure(t *testing.T) {
	t.Setenv("PGPASSWORD", "test-password")

	originalStage := stageRestoreSource
	originalPreflight := applyRestorePreflight
	originalStageChain := stageChainArtifacts
	originalAssemble := assemblePostgresChain
	originalEngine := executeRestoreEngine
	t.Cleanup(func() {
		stageRestoreSource = originalStage
		applyRestorePreflight = originalPreflight
		stageChainArtifacts = originalStageChain
		assemblePostgresChain = originalAssemble
		executeRestoreEngine = originalEngine
	})

	tmpDir := t.TempDir()
	backupPath := filepath.Join(tmpDir, "incremental.dump")
	if err := os.WriteFile(backupPath, []byte("backup"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	manifestPath := filepath.Join(tmpDir, "incremental.dump.manifest.json")
	if err := manifest.WriteManifest(manifestPath, &manifest.BackupManifest{
		BackupID:     "incr-002",
		Database:     "app",
		DatabaseType: "postgres",
		CreatedAt:    time.Now().UTC(),
		SizeBytes:    6,
		Hash:         manifest.HashInfo{Algorithm: "sha256", Value: "abc"},
		AdvancedRestore: &manifest.AdvancedRestoreMetadata{
			Capabilities: []string{"incremental"},
			IncrementalLineage: &manifest.IncrementalLineageMetadata{
				BaselineBackupID:   "base-001",
				RequiredBackupIDs:  []string{"incr-001"},
				ExecutionSupported: true,
			},
		},
	}); err != nil {
		t.Fatalf("WriteManifest() error = %v", err)
	}

	stageRestoreSource = func(ctx context.Context, job config.RestoreJob) (*StagedArtifact, error) {
		return &StagedArtifact{Path: backupPath, ManifestPath: manifestPath, SourcePath: "incremental.dump", SizeBytes: 6}, nil
	}
	applyRestorePreflight = func(ctx context.Context, cfg *config.Configuration, artifact *StagedArtifact, _ bool) (string, error) {
		return artifact.Path, nil
	}
	var chainPaths []string
	stageChainArtifacts = func(ctx context.Context, job config.RestoreJob, backupIDs []string) ([]*StagedArtifact, error) {
		artifacts := make([]*StagedArtifact, 0, len(backupIDs))
		for _, id := range backupIDs {
			path := filepath.Join(tmpDir, id+".staged")
			if err := os.WriteFile(path, []byte(id), 0o600); err != nil {
				return nil, err
			}
			chainPaths = append(chainPaths, path)
			artifacts = append(artifacts, &StagedArtifact{Path: path, SourcePath: id, SizeBytes: int64(len(id))})
		}
		return artifacts, nil
	}
	combinedPath := filepath.Join(tmpDir, "combined-dir")
	assemblePostgresChain = func(ctx context.Context, stagingDir string, stagedSources []string, toolsPath string) (string, error) {
		if err := os.MkdirAll(combinedPath, 0o700); err != nil {
			return "", err
		}
		return combinedPath, nil
	}
	executeRestoreEngine = func(ctx context.Context, job config.RestoreJob, stagedPath string) error {
		if stagedPath != combinedPath {
			t.Fatalf("stagedPath = %q, want %q", stagedPath, combinedPath)
		}
		return errors.New("restore engine failed")
	}

	result, err := ExecuteRestore(context.Background(), &ExecutionRequest{
		JobName: "restore",
		Job: config.RestoreJob{
			Type:                  "postgres",
			RestoreMode:           "incremental",
			IncrementalFromBackup: "base-001",
			Host:                  "localhost",
			Username:              "postgres",
			PasswordEnv:           "PGPASSWORD",
			Database:              "app",
			Schedule:              "0 2 * * *",
			StagingDir:            tmpDir,
			BackupSource: config.RestoreBackupSource{
				Type:       "local",
				LocalPath:  tmpDir,
				BackupPath: "incremental.dump",
			},
		},
	})
	if err == nil {
		t.Fatal("expected restore engine failure")
	}
	if result == nil {
		t.Fatal("result is nil")
	}
	if _, statErr := os.Stat(combinedPath); !os.IsNotExist(statErr) {
		t.Fatalf("expected combined artifact cleaned up, stat err = %v", statErr)
	}
	for _, path := range append(chainPaths, backupPath) {
		if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
			t.Fatalf("expected staged artifact %q cleaned up, stat err = %v", path, statErr)
		}
	}
}
