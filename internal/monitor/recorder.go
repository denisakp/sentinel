package monitor

import (
	"context"
	"crypto/rand"
	"fmt"
	"time"
)

// RecordExecution persists a backup execution record.
func (m *Monitor) RecordExecution(ctx context.Context, exec *Execution) error {
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
		(id, backup_name, database_type, timestamp, duration_ms, status, error_message, storage_backend, file_path, file_size_bytes, checksum, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

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
		exec.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to record execution: %w", err)
	}

	return nil
}

// RecordRestoreExecution persists a restore execution record.
func (m *Monitor) RecordRestoreExecution(ctx context.Context, exec *RestoreExecution) error {
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
		(id, restore_name, database_type, database_name, timestamp, duration_ms, status, error_message, source_backup_path, bytes_restored, verification_passed, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	_, err := m.db.ExecContext(ctx, query,
		exec.ID,
		exec.RestoreName,
		exec.DatabaseType,
		exec.DatabaseName,
		exec.Timestamp,
		exec.DurationMs,
		exec.Status,
		exec.ErrorMessage,
		exec.SourceBackupPath,
		exec.BytesRestored,
		exec.VerificationPassed,
		exec.CreatedAt,
	)
	if err != nil {
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
	return m.FinalizeExecution(ctx, id, StatusFailed, errorMsg, cleanupAttempted, cleanupSucceeded, cleanupError)
}

// RecordInterrupted marks a stale running execution as interrupted during startup reconciliation.
// This is only called by startup logic for executions that were running when the process stopped.
func (m *Monitor) RecordInterrupted(ctx context.Context, id string, cleanupAttempted bool, cleanupSucceeded *bool, cleanupError string) error {
	errorMsg := "execution interrupted by process restart"
	return m.FinalizeExecution(ctx, id, StatusInterrupted, errorMsg, cleanupAttempted, cleanupSucceeded, cleanupError)
}

// RecordSuccess finalizes an execution as completed successfully.
// This marks the happy path terminal state with no error or cleanup required.
func (m *Monitor) RecordSuccess(ctx context.Context, id string) error {
	return m.FinalizeExecution(ctx, id, StatusCompleted, "", false, nil, "")
}
