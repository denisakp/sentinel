package monitor

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// ListExecutions retrieves backup history with filtering.
func (m *Monitor) ListExecutions(ctx context.Context, filter *Filter, limit int, offset int) ([]Execution, error) {
	if m == nil || m.db == nil {
		return nil, fmt.Errorf("monitor database is not initialized")
	}
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	whereClause, args := buildFilter(filter)
	query := `SELECT id, backup_name, database_type, timestamp, duration_ms, status, error_message,
		storage_backend, file_path, file_size_bytes, checksum, backup_type, chain_id, chain_index, delta_size_bytes, full_backup_size_bytes, created_at,
		finished_at, cleanup_attempted, cleanup_succeeded, cleanup_error, updated_at
		FROM backup_executions ` + whereClause + ` ORDER BY timestamp DESC LIMIT ? OFFSET ?`
	args = append(args, limit, offset)

	rows, err := m.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list executions: %w", err)
	}
	defer rows.Close()

	var executions []Execution
	for rows.Next() {
		exec, err := scanFullExecution(rows)
		if err != nil {
			return nil, err
		}
		executions = append(executions, *exec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to read execution rows: %w", err)
	}

	return executions, nil
}

// GetExecution retrieves detailed info for a single execution.
func (m *Monitor) GetExecution(ctx context.Context, id string) (*Execution, error) {
	if m == nil || m.db == nil {
		return nil, fmt.Errorf("monitor database is not initialized")
	}
	if id == "" {
		return nil, fmt.Errorf("execution id is required")
	}

	query := `SELECT id, backup_name, database_type, timestamp, duration_ms, status, error_message,
		storage_backend, file_path, file_size_bytes, checksum, backup_type, chain_id, chain_index, delta_size_bytes, full_backup_size_bytes, created_at,
		finished_at, cleanup_attempted, cleanup_succeeded, cleanup_error, updated_at
		FROM backup_executions WHERE id = ?`

	row := m.db.QueryRowContext(ctx, query, id)
	exec, err := scanFullExecution(row)
	if err != nil {
		return nil, err
	}
	return exec, nil
}

// ListRestoreExecutions retrieves restore history with filtering.
func (m *Monitor) ListRestoreExecutions(ctx context.Context, filter *RestoreFilter, limit int, offset int) ([]RestoreExecution, error) {
	if m == nil || m.db == nil {
		return nil, fmt.Errorf("monitor database is not initialized")
	}
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	whereClause, args := buildRestoreFilter(filter)
	query := `SELECT id, restore_name, database_type, database_name, source_type, conflict_strategy,
		restore_mode, planning_status, requested_pitr_time_utc, baseline_backup_id, fallback_decision, fallback_reason, fallback_backup_id, chain_depth, chain_id, assembly_duration_ms, recovery_timeline_id,
		timestamp, duration_ms, status, error_message, error_reason, reason, source_backup_path,
		staged_file_path, staged_file_retained, bytes_restored, verification_passed, timeout_seconds,
		created_at, finished_at
		FROM restore_executions ` + whereClause + ` ORDER BY timestamp DESC LIMIT ? OFFSET ?`
	args = append(args, limit, offset)

	rows, err := m.db.QueryContext(ctx, query, args...)
	if err != nil {
		if strings.Contains(err.Error(), "restore_mode") || strings.Contains(err.Error(), "planning_status") {
			legacyQuery := `SELECT id, restore_name, database_type, database_name, source_type, conflict_strategy,
				timestamp, duration_ms, status, error_message, error_reason, reason, source_backup_path,
				staged_file_path, staged_file_retained, bytes_restored, verification_passed, timeout_seconds,
				created_at, finished_at
				FROM restore_executions ` + whereClause + ` ORDER BY timestamp DESC LIMIT ? OFFSET ?`
			legacyRows, legacyErr := m.db.QueryContext(ctx, legacyQuery, args...)
			if legacyErr != nil {
				return nil, fmt.Errorf("failed to list restore executions with legacy fallback: %w", legacyErr)
			}
			defer legacyRows.Close()

			var legacyExecutions []RestoreExecution
			for legacyRows.Next() {
				exec, scanErr := scanRestoreExecutionLegacy(legacyRows)
				if scanErr != nil {
					return nil, scanErr
				}
				legacyExecutions = append(legacyExecutions, *exec)
			}
			if err := legacyRows.Err(); err != nil {
				return nil, fmt.Errorf("failed to read legacy restore execution rows: %w", err)
			}
			return legacyExecutions, nil
		}
		return nil, fmt.Errorf("failed to list restore executions: %w", err)
	}
	defer rows.Close()

	var executions []RestoreExecution
	for rows.Next() {
		exec, err := scanRestoreExecution(rows)
		if err != nil {
			return nil, err
		}
		executions = append(executions, *exec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to read restore execution rows: %w", err)
	}

	return executions, nil
}

func buildFilter(filter *Filter) (string, []interface{}) {
	if filter == nil {
		return "", nil
	}

	var clauses []string
	var args []interface{}

	if filter.BackupName != "" {
		clauses = append(clauses, "backup_name = ?")
		args = append(args, filter.BackupName)
	}
	if filter.Status != "" {
		clauses = append(clauses, "status = ?")
		args = append(args, filter.Status)
	}
	if filter.DatabaseType != "" {
		clauses = append(clauses, "database_type = ?")
		args = append(args, filter.DatabaseType)
	}
	if filter.StorageBackend != "" {
		clauses = append(clauses, "storage_backend = ?")
		args = append(args, filter.StorageBackend)
	}
	if !filter.StartDate.IsZero() {
		clauses = append(clauses, "timestamp >= ?")
		args = append(args, filter.StartDate)
	}
	if !filter.EndDate.IsZero() {
		clauses = append(clauses, "timestamp <= ?")
		args = append(args, filter.EndDate)
	}

	if len(clauses) == 0 {
		return "", args
	}

	return "WHERE " + strings.Join(clauses, " AND "), args
}

func buildRestoreFilter(filter *RestoreFilter) (string, []interface{}) {
	if filter == nil {
		return "", nil
	}

	var clauses []string
	var args []interface{}

	if filter.RestoreName != "" {
		clauses = append(clauses, "restore_name = ?")
		args = append(args, filter.RestoreName)
	}
	if filter.Status != "" {
		clauses = append(clauses, "status = ?")
		args = append(args, filter.Status)
	}
	if filter.DatabaseType != "" {
		clauses = append(clauses, "database_type = ?")
		args = append(args, filter.DatabaseType)
	}
	if !filter.StartDate.IsZero() {
		clauses = append(clauses, "timestamp >= ?")
		args = append(args, filter.StartDate)
	}
	if !filter.EndDate.IsZero() {
		clauses = append(clauses, "timestamp <= ?")
		args = append(args, filter.EndDate)
	}

	if len(clauses) == 0 {
		return "", args
	}

	return "WHERE " + strings.Join(clauses, " AND "), args
}

type rowScanner interface {
	Scan(dest ...interface{}) error
}

func scanExecution(row rowScanner) (*Execution, error) {
	var exec Execution
	var timestamp time.Time
	var createdAt time.Time
	var durationMs sql.NullInt64
	var fileSize sql.NullInt64
	var errorMessage sql.NullString
	var checksum sql.NullString

	if err := row.Scan(
		&exec.ID,
		&exec.BackupName,
		&exec.DatabaseType,
		&timestamp,
		&durationMs,
		&exec.Status,
		&errorMessage,
		&exec.StorageBackend,
		&exec.FilePath,
		&fileSize,
		&checksum,
		&createdAt,
	); err != nil {
		return nil, fmt.Errorf("failed to scan execution: %w", err)
	}

	exec.Timestamp = timestamp
	exec.CreatedAt = createdAt
	if durationMs.Valid {
		exec.DurationMs = durationMs.Int64
	}
	if fileSize.Valid {
		exec.FileSizeBytes = fileSize.Int64
	}
	if errorMessage.Valid {
		exec.ErrorMessage = errorMessage.String
	}
	if checksum.Valid {
		exec.Checksum = checksum.String
	}

	return &exec, nil
}

// FinalizeExecution marks an execution as completed with final status, timestamp, and optional cleanup outcome.
// This is used for terminal state transitions (running -> completed/failed/interrupted).
func (m *Monitor) FinalizeExecution(ctx context.Context, id string, status string, errorMsg string, cleanupAttempted bool, cleanupSucceeded *bool, cleanupError string) error {
	if m == nil || m.db == nil {
		return fmt.Errorf("monitor database is not initialized")
	}
	if id == "" {
		return fmt.Errorf("execution id is required")
	}

	now := time.Now()
	query := `UPDATE backup_executions SET 
		status = ?,
		error_message = ?,
		finished_at = ?,
		cleanup_attempted = ?,
		cleanup_succeeded = ?,
		cleanup_error = ?,
		updated_at = ?
		WHERE id = ?`

	_, err := m.db.ExecContext(ctx, query, status, errorMsg, now, cleanupAttempted, cleanupSucceeded, cleanupError, now, id)
	if err != nil {
		return fmt.Errorf("failed to finalize execution %s: %w", id, err)
	}

	return nil
}

// GetStaleRunningExecutions retrieves executions with status 'running' or legacy 'in-progress'
// for startup reconciliation. These are candidates for marking as interrupted.
func (m *Monitor) GetStaleRunningExecutions(ctx context.Context) ([]Execution, error) {
	if m == nil || m.db == nil {
		return nil, fmt.Errorf("monitor database is not initialized")
	}

	query := `SELECT id, backup_name, database_type, timestamp, duration_ms, status, error_message,
		storage_backend, file_path, file_size_bytes, checksum, backup_type, chain_id, chain_index, delta_size_bytes, full_backup_size_bytes, created_at,
		finished_at, cleanup_attempted, cleanup_succeeded, cleanup_error, updated_at
		FROM backup_executions 
		WHERE status IN (?, ?) 
		ORDER BY timestamp DESC`

	rows, err := m.db.QueryContext(ctx, query, StatusRunning, LegacyStatusInProgress)
	if err != nil {
		return nil, fmt.Errorf("failed to query stale running executions: %w", err)
	}
	defer rows.Close()

	var executions []Execution
	for rows.Next() {
		exec, err := scanFullExecution(rows)
		if err != nil {
			return nil, err
		}
		executions = append(executions, *exec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to read stale execution rows: %w", err)
	}

	return executions, nil
}

// scanFullExecution scans execution with all V1 consolidation fields (cleanup, finished_at, updated_at).
func scanFullExecution(row rowScanner) (*Execution, error) {
	var exec Execution
	var timestamp time.Time
	var createdAt time.Time
	var updatedAt time.Time
	var durationMs sql.NullInt64
	var fileSize sql.NullInt64
	var errorMessage sql.NullString
	var checksum sql.NullString
	var backupType sql.NullString
	var chainID sql.NullString
	var chainIndex sql.NullInt64
	var deltaSize sql.NullInt64
	var fullBackupSize sql.NullInt64
	var finishedAt sql.NullTime
	var cleanupSucceeded sql.NullBool
	var cleanupError sql.NullString

	if err := row.Scan(
		&exec.ID,
		&exec.BackupName,
		&exec.DatabaseType,
		&timestamp,
		&durationMs,
		&exec.Status,
		&errorMessage,
		&exec.StorageBackend,
		&exec.FilePath,
		&fileSize,
		&checksum,
		&backupType,
		&chainID,
		&chainIndex,
		&deltaSize,
		&fullBackupSize,
		&createdAt,
		&finishedAt,
		&exec.CleanupAttempted,
		&cleanupSucceeded,
		&cleanupError,
		&updatedAt,
	); err != nil {
		return nil, fmt.Errorf("failed to scan full execution: %w", err)
	}

	exec.Timestamp = timestamp
	exec.CreatedAt = createdAt
	exec.UpdatedAt = updatedAt

	if durationMs.Valid {
		exec.DurationMs = durationMs.Int64
	}
	if fileSize.Valid {
		exec.FileSizeBytes = fileSize.Int64
	}
	if errorMessage.Valid {
		exec.ErrorMessage = errorMessage.String
	}
	if checksum.Valid {
		exec.Checksum = checksum.String
	}
	if backupType.Valid {
		exec.BackupType = backupType.String
	}
	if chainID.Valid {
		exec.ChainID = chainID.String
	}
	if chainIndex.Valid {
		exec.ChainIndex = int(chainIndex.Int64)
	}
	if deltaSize.Valid {
		exec.DeltaSizeBytes = deltaSize.Int64
	}
	if fullBackupSize.Valid {
		exec.FullBackupSizeBytes = fullBackupSize.Int64
	}
	if finishedAt.Valid {
		exec.FinishedAt = &finishedAt.Time
	}
	if cleanupSucceeded.Valid {
		val := cleanupSucceeded.Bool
		exec.CleanupSucceeded = &val
	}
	if cleanupError.Valid {
		exec.CleanupError = cleanupError.String
	}

	return &exec, nil
}

func scanRestoreExecution(row rowScanner) (*RestoreExecution, error) {
	var exec RestoreExecution
	var timestamp time.Time
	var createdAt time.Time
	var durationMs sql.NullInt64
	var errorMessage sql.NullString
	var errorReason sql.NullString
	var reason sql.NullString
	var restoreMode sql.NullString
	var planningStatus sql.NullString
	var requestedPITRTime sql.NullTime
	var baselineBackupID sql.NullString
	var fallbackDecision sql.NullString
	var fallbackReason sql.NullString
	var fallbackBackupID sql.NullString
	var chainDepth sql.NullInt64
	var chainID sql.NullString
	var assemblyDurationMs sql.NullInt64
	var recoveryTimelineID sql.NullString
	var stagedFilePath sql.NullString
	var stagedRetained sql.NullBool
	var bytesRestored sql.NullInt64
	var verificationPassed sql.NullBool
	var timeoutSeconds sql.NullInt64
	var finishedAt sql.NullTime

	if err := row.Scan(
		&exec.ID,
		&exec.RestoreName,
		&exec.DatabaseType,
		&exec.DatabaseName,
		&exec.SourceType,
		&exec.ConflictStrategy,
		&restoreMode,
		&planningStatus,
		&requestedPITRTime,
		&baselineBackupID,
		&fallbackDecision,
		&fallbackReason,
		&fallbackBackupID,
		&chainDepth,
		&chainID,
		&assemblyDurationMs,
		&recoveryTimelineID,
		&timestamp,
		&durationMs,
		&exec.Status,
		&errorMessage,
		&errorReason,
		&reason,
		&exec.SourceBackupPath,
		&stagedFilePath,
		&stagedRetained,
		&bytesRestored,
		&verificationPassed,
		&timeoutSeconds,
		&createdAt,
		&finishedAt,
	); err != nil {
		return nil, fmt.Errorf("failed to scan restore execution: %w", err)
	}

	exec.Timestamp = timestamp
	exec.CreatedAt = createdAt
	if restoreMode.Valid {
		exec.RestoreMode = restoreMode.String
	}
	if planningStatus.Valid {
		exec.PlanningStatus = planningStatus.String
	}
	if requestedPITRTime.Valid {
		t := requestedPITRTime.Time
		exec.RequestedPITRTimeUTC = &t
	}
	if baselineBackupID.Valid {
		exec.BaselineBackupID = baselineBackupID.String
	}
	if fallbackDecision.Valid {
		exec.FallbackDecision = fallbackDecision.String
	}
	if fallbackReason.Valid {
		exec.FallbackReason = fallbackReason.String
	}
	if fallbackBackupID.Valid {
		exec.FallbackBackupID = fallbackBackupID.String
	}
	if chainDepth.Valid {
		exec.ChainDepth = int(chainDepth.Int64)
	}
	if chainID.Valid {
		exec.ChainID = chainID.String
	}
	if assemblyDurationMs.Valid {
		exec.AssemblyDurationMs = assemblyDurationMs.Int64
	}
	if recoveryTimelineID.Valid {
		exec.RecoveryTimelineID = recoveryTimelineID.String
	}
	if durationMs.Valid {
		exec.DurationMs = durationMs.Int64
	}
	if errorMessage.Valid {
		exec.ErrorMessage = errorMessage.String
	}
	if errorReason.Valid {
		exec.ErrorReason = errorReason.String
	}
	if reason.Valid {
		exec.Reason = reason.String
	}
	if stagedFilePath.Valid {
		exec.StagedFilePath = stagedFilePath.String
	}
	if stagedRetained.Valid {
		exec.StagedFileRetained = stagedRetained.Bool
	}
	if bytesRestored.Valid {
		exec.BytesRestored = bytesRestored.Int64
	}
	if verificationPassed.Valid {
		exec.VerificationPassed = verificationPassed.Bool
	}
	if timeoutSeconds.Valid {
		exec.TimeoutSeconds = int(timeoutSeconds.Int64)
	}
	if finishedAt.Valid {
		exec.FinishedAt = &finishedAt.Time
	}

	return &exec, nil
}

func scanRestoreExecutionLegacy(row rowScanner) (*RestoreExecution, error) {
	var exec RestoreExecution
	var timestamp time.Time
	var createdAt time.Time
	var durationMs sql.NullInt64
	var errorMessage sql.NullString
	var errorReason sql.NullString
	var reason sql.NullString
	var stagedFilePath sql.NullString
	var stagedRetained sql.NullBool
	var bytesRestored sql.NullInt64
	var verificationPassed sql.NullBool
	var timeoutSeconds sql.NullInt64
	var finishedAt sql.NullTime

	if err := row.Scan(
		&exec.ID,
		&exec.RestoreName,
		&exec.DatabaseType,
		&exec.DatabaseName,
		&exec.SourceType,
		&exec.ConflictStrategy,
		&timestamp,
		&durationMs,
		&exec.Status,
		&errorMessage,
		&errorReason,
		&reason,
		&exec.SourceBackupPath,
		&stagedFilePath,
		&stagedRetained,
		&bytesRestored,
		&verificationPassed,
		&timeoutSeconds,
		&createdAt,
		&finishedAt,
	); err != nil {
		return nil, fmt.Errorf("failed to scan legacy restore execution: %w", err)
	}

	exec.Timestamp = timestamp
	exec.CreatedAt = createdAt
	if durationMs.Valid {
		exec.DurationMs = durationMs.Int64
	}
	if errorMessage.Valid {
		exec.ErrorMessage = errorMessage.String
	}
	if errorReason.Valid {
		exec.ErrorReason = errorReason.String
	}
	if reason.Valid {
		exec.Reason = reason.String
	}
	if stagedFilePath.Valid {
		exec.StagedFilePath = stagedFilePath.String
	}
	if stagedRetained.Valid {
		exec.StagedFileRetained = stagedRetained.Bool
	}
	if bytesRestored.Valid {
		exec.BytesRestored = bytesRestored.Int64
	}
	if verificationPassed.Valid {
		exec.VerificationPassed = verificationPassed.Bool
	}
	if timeoutSeconds.Valid {
		exec.TimeoutSeconds = int(timeoutSeconds.Int64)
	}
	if finishedAt.Valid {
		exec.FinishedAt = &finishedAt.Time
	}

	return &exec, nil
}

// GetMigrationStatus returns the current migration state including applied and pending versions.
func (m *Monitor) GetMigrationStatus(ctx context.Context) (*MigrationStatus, error) {
	if m == nil || m.db == nil {
		return nil, fmt.Errorf("monitor database is not initialized")
	}

	// Get all applied migrations
	rows, err := m.db.QueryContext(ctx, `
		SELECT version, name, COALESCE(checksum, ''), applied_at 
		FROM schema_migrations 
		ORDER BY version`)
	if err != nil {
		return nil, fmt.Errorf("failed to query applied migrations: %w", err)
	}
	defer rows.Close()

	var appliedMigrations []Migration
	currentVersion := 0
	for rows.Next() {
		var mig Migration
		if err := rows.Scan(&mig.Version, &mig.Name, &mig.Checksum, &mig.AppliedAt); err != nil {
			return nil, fmt.Errorf("failed to scan migration: %w", err)
		}
		appliedMigrations = append(appliedMigrations, mig)
		if mig.Version > currentVersion {
			currentVersion = mig.Version
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to read migration rows: %w", err)
	}

	// Get latest available version from embedded migrations
	latestAvailable, err := getLatestAvailableMigrationVersion()
	if err != nil {
		return nil, fmt.Errorf("failed to get latest available version: %w", err)
	}

	// Calculate pending versions
	appliedVersions := make(map[int]bool)
	for _, mig := range appliedMigrations {
		appliedVersions[mig.Version] = true
	}

	var pendingVersions []int
	for v := 1; v <= latestAvailable; v++ {
		if !appliedVersions[v] {
			pendingVersions = append(pendingVersions, v)
		}
	}

	return &MigrationStatus{
		CurrentVersion:         currentVersion,
		LatestAvailableVersion: latestAvailable,
		PendingVersions:        pendingVersions,
		AppliedMigrations:      appliedMigrations,
		IsUpToDate:             currentVersion == latestAvailable && len(pendingVersions) == 0,
	}, nil
}

// getLatestAvailableMigrationVersion scans embedded migrations to find the highest version.
func getLatestAvailableMigrationVersion() (int, error) {
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return 0, fmt.Errorf("failed to read migrations directory: %w", err)
	}

	latestVersion := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		version, _, err := parseMigrationFileName(entry.Name())
		if err != nil {
			continue // Skip invalid files
		}
		if version > latestVersion {
			latestVersion = version
		}
	}

	return latestVersion, nil
}
