package monitor

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"strings"
	"github.com/denisakp/sentinel/internal/ports"
)

// ExportHistory exports execution data to JSON or CSV.
func (m *Monitor) ExportHistory(ctx context.Context, format string, filter *ports.Filter) ([]byte, error) {
	format = normalizeFormat(format)

	executions, err := m.ListExecutions(ctx, filter, 100000, 0)
	if err != nil {
		return nil, err
	}

	switch format {
	case "json":
		return exportJSON(executions)
	case "csv":
		return exportCSV(executions)
	default:
		return nil, fmt.Errorf("unsupported export format '%s'", format)
	}
}

func normalizeFormat(format string) string {
	if format == "" {
		return "json"
	}
	return strings.ToLower(format)
}

func exportJSON(executions []ports.Execution) ([]byte, error) {
	data, err := json.MarshalIndent(executions, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal json export: %w", err)
	}
	return data, nil
}

func exportCSV(executions []ports.Execution) ([]byte, error) {
	var buf bytes.Buffer
	writer := csv.NewWriter(&buf)

	headers := []string{
		"id", "backup_name", "database_type", "timestamp", "duration_ms", "status",
		"error_message", "storage_backend", "file_path", "file_size_bytes", "checksum",
		"backup_type", "chain_id", "chain_index", "delta_size_bytes", "full_backup_size_bytes", "created_at",
	}
	if err := writer.Write(headers); err != nil {
		return nil, fmt.Errorf("failed to write csv headers: %w", err)
	}

	for _, exec := range executions {
		record := []string{
			exec.ID,
			exec.BackupName,
			exec.DatabaseType,
			exec.Timestamp.Format("2006-01-02 15:04:05"),
			fmt.Sprintf("%d", exec.DurationMs),
			exec.Status,
			exec.ErrorMessage,
			exec.StorageBackend,
			exec.FilePath,
			fmt.Sprintf("%d", exec.FileSizeBytes),
			exec.Checksum,
			exec.BackupType,
			exec.ChainID,
			fmt.Sprintf("%d", exec.ChainIndex),
			fmt.Sprintf("%d", exec.DeltaSizeBytes),
			fmt.Sprintf("%d", exec.FullBackupSizeBytes),
			exec.CreatedAt.Format("2006-01-02 15:04:05"),
		}
		if err := writer.Write(record); err != nil {
			return nil, fmt.Errorf("failed to write csv record: %w", err)
		}
	}

	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, fmt.Errorf("failed to flush csv export: %w", err)
	}

	return buf.Bytes(), nil
}
