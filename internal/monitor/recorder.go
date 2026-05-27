package monitor

import (
	"context"
	"crypto/rand"
	"fmt"
	"time"
	"github.com/denisakp/sentinel/internal/ports"
)

// RecordExecution persists a backup execution record.
func (m *Monitor) RecordExecution(ctx context.Context, exec *ports.Execution) error {
	if m == nil || m.db == nil {
		return fmt.Errorf("monitor database is not initialized")
	}
	if exec == nil {
		return fmt.Errorf("execution record is required")
	}
	if exec.ID == "" {
		exec.ID = newUUID()
	}
	if exec.Timestamp.IsZero() {
		exec.Timestamp = time.Now().UTC()
	}
	if exec.CreatedAt.IsZero() {
		exec.CreatedAt = time.Now().UTC()
	}

	query := `INSERT INTO backup_executions
		(id, backup_name, database_type, timestamp, duration_ms, status, error_message, storage_backend, file_path, file_size_bytes, checksum, backup_type, chain_id, chain_index, delta_size_bytes, full_backup_size_bytes, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	_, err := m.db.ExecContext(ctx, query,
		exec.ID,
		exec.BackupName,
		exec.DatabaseType,
		exec.Timestamp,
		exec.DurationMs,
		exec.Status,
		exec.ErrorMessage,
		exec.StorageBackend,
		exec.FilePath,
		exec.FileSizeBytes,
		exec.Checksum,
		exec.BackupType,
		exec.ChainID,
		exec.ChainIndex,
		exec.DeltaSizeBytes,
		exec.FullBackupSizeBytes,
		exec.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to record execution: %w", err)
	}

	return nil
}

// RecordRestoreExecution persists a restore execution record.
func (m *Monitor) RecordRestoreExecution(ctx context.Context, exec *ports.RestoreExecution) error {
	if m == nil || m.db == nil {
		return fmt.Errorf("monitor database is not initialized")
	}
	if exec == nil {
		return fmt.Errorf("restore execution record is required")
	}
	if exec.ID == "" {
		exec.ID = newUUID()
	}
	if exec.Timestamp.IsZero() {
		exec.Timestamp = time.Now().UTC()
	}
	if exec.CreatedAt.IsZero() {
		exec.CreatedAt = time.Now().UTC()
	}

	query := `INSERT INTO restore_executions
		(id, restore_name, database_type, database_name, restore_mode, planning_status, requested_pitr_time_utc, baseline_backup_id, fallback_decision, fallback_reason, fallback_backup_id, chain_depth, chain_id, assembly_duration_ms, recovery_timeline_id, source_type, conflict_strategy, timestamp, duration_ms, status, error_message, error_reason, reason, source_backup_path, staged_file_path, staged_file_retained, bytes_restored, verification_passed, timeout_seconds, created_at, finished_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	_, err := m.db.ExecContext(ctx, query,
		exec.ID,
		exec.RestoreName,
		exec.DatabaseType,
		exec.DatabaseName,
		exec.RestoreMode,
		exec.PlanningStatus,
		exec.RequestedPITRTimeUTC,
		exec.BaselineBackupID,
		exec.FallbackDecision,
		exec.FallbackReason,
		exec.FallbackBackupID,
		exec.ChainDepth,
		exec.ChainID,
		exec.AssemblyDurationMs,
		exec.RecoveryTimelineID,
		exec.SourceType,
		exec.ConflictStrategy,
		exec.Timestamp,
		exec.DurationMs,
		exec.Status,
		exec.ErrorMessage,
		exec.ErrorReason,
		exec.Reason,
		exec.SourceBackupPath,
		exec.StagedFilePath,
		exec.StagedFileRetained,
		exec.BytesRestored,
		exec.VerificationPassed,
		exec.TimeoutSeconds,
		exec.CreatedAt,
		exec.FinishedAt,
	)
	if err != nil {
		// NewMonitor guarantees all required columns exist before this point
		// (see internal/monitor/migrate.go). A missing-column error here is a
		// programmer bug (binary requires a migration it didn't ship); surface
		// it rather than retrying against a legacy column subset.
		return fmt.Errorf("failed to record restore execution: %w", err)
	}

	return nil
}

func newUUID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("uuid-%d", time.Now().UnixNano())
	}

	// Set version (4) and variant bits
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80

	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// RecordFailure finalizes an execution as failed with error message and optional cleanup outcome.
// This enforces terminal state transition semantics for backup/restore failures.
func (m *Monitor) RecordFailure(ctx context.Context, id string, errorMsg string, cleanupAttempted bool, cleanupSucceeded *bool, cleanupError string) error {
	return m.FinalizeExecution(ctx, id, ports.StatusFailed, errorMsg, cleanupAttempted, cleanupSucceeded, cleanupError)
}

// RecordInterrupted marks a stale running execution as interrupted during startup reconciliation.
// This is only called by startup logic for executions that were running when the process stopped.
func (m *Monitor) RecordInterrupted(ctx context.Context, id string, cleanupAttempted bool, cleanupSucceeded *bool, cleanupError string) error {
	errorMsg := "execution interrupted by process restart"
	return m.FinalizeExecution(ctx, id, ports.StatusInterrupted, errorMsg, cleanupAttempted, cleanupSucceeded, cleanupError)
}

// RecordSuccess finalizes an execution as completed successfully.
// This marks the happy path terminal state with no error or cleanup required.
func (m *Monitor) RecordSuccess(ctx context.Context, id string) error {
	return m.FinalizeExecution(ctx, id, ports.StatusCompleted, "", false, nil, "")
}

// RecordRunning persists an initial backup execution record in "running" status.
// Call this at the START of each backup job to enable progress tracking. (T048)
func (m *Monitor) RecordRunning(ctx context.Context, exec *ports.Execution) error {
	if m == nil || m.db == nil {
		return fmt.Errorf("monitor database is not initialized")
	}
	if exec == nil {
		return fmt.Errorf("execution record is required")
	}
	if exec.ID == "" {
		exec.ID = newUUID()
	}
	if exec.Timestamp.IsZero() {
		exec.Timestamp = time.Now().UTC()
	}
	if exec.CreatedAt.IsZero() {
		exec.CreatedAt = time.Now().UTC()
	}
	exec.Status = ports.StatusRunning

	query := `INSERT INTO backup_executions
		(id, backup_name, database_type, timestamp, duration_ms, status, error_message, storage_backend, file_path, file_size_bytes, checksum, backup_type, chain_id, chain_index, delta_size_bytes, full_backup_size_bytes, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	_, err := m.db.ExecContext(ctx, query,
		exec.ID,
		exec.BackupName,
		exec.DatabaseType,
		exec.Timestamp,
		0,
		exec.Status,
		"",
		exec.StorageBackend,
		exec.FilePath,
		exec.FileSizeBytes,
		exec.Checksum,
		exec.BackupType,
		exec.ChainID,
		exec.ChainIndex,
		exec.DeltaSizeBytes,
		exec.FullBackupSizeBytes,
		exec.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to record running execution: %w", err)
	}
	return nil
}

// RecordSecurityInfo updates the security metadata columns for a completed execution.
// Called after backup completes to record hash, encryption, and manifest info. (T026/T035)
func (m *Monitor) RecordSecurityInfo(ctx context.Context, id, hashAlgo, hashValue, plaintextHash, manifestPath string, encrypted bool, keyHint string) error {
	if m == nil || m.db == nil {
		return fmt.Errorf("monitor database is not initialized")
	}

	encryptedInt := 0
	if encrypted {
		encryptedInt = 1
	}

	_, err := m.db.ExecContext(ctx,
		`UPDATE backup_executions SET
			hash_algorithm = ?, hash_value = ?, plaintext_hash_value = ?,
			manifest_path = ?, encrypted = ?, encryption_key_hint = ?
			WHERE id = ?`,
		hashAlgo, hashValue, plaintextHash,
		manifestPath, encryptedInt, keyHint,
		id,
	)
	if err != nil {
		return fmt.Errorf("failed to record security info: %w", err)
	}
	return nil
}

// RecordRetryCount updates the retry_count column for an execution. (T053)
func (m *Monitor) RecordRetryCount(ctx context.Context, id string, retryCount int) error {
	if m == nil || m.db == nil {
		return fmt.Errorf("monitor database is not initialized")
	}
	_, err := m.db.ExecContext(ctx,
		`UPDATE backup_executions SET retry_count = ? WHERE id = ?`,
		retryCount, id)
	if err != nil {
		return fmt.Errorf("failed to record retry count: %w", err)
	}
	return nil
}
