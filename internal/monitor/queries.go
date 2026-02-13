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
		storage_backend, file_path, file_size_bytes, checksum, created_at
		FROM backup_executions ` + whereClause + ` ORDER BY timestamp DESC LIMIT ? OFFSET ?`
	args = append(args, limit, offset)

	rows, err := m.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list executions: %w", err)
	}
	defer rows.Close()

	var executions []Execution
	for rows.Next() {
		exec, err := scanExecution(rows)
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
		storage_backend, file_path, file_size_bytes, checksum, created_at
		FROM backup_executions WHERE id = ?`

	row := m.db.QueryRowContext(ctx, query, id)
	exec, err := scanExecution(row)
	if err != nil {
		return nil, err
	}
	return exec, nil
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
