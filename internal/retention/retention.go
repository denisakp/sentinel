package retention

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/denisakp/sentinel/internal/config"
	"github.com/denisakp/sentinel/internal/adapters/monitor"
	domainret "github.com/denisakp/sentinel/internal/domain/retention"
	_ "modernc.org/sqlite"
)

// Manager manages retention operations.
type Manager struct {
	cfg *config.Configuration
	db  *sql.DB
}

// NewManager creates a retention manager with database access.
func NewManager(cfg *config.Configuration) (*Manager, error) {
	path, err := expandHome(cfg.HistoryDBPath)
	if err != nil {
		return nil, err
	}
	if err := ensureDir(filepath.Dir(path)); err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("failed to open retention database: %w", err)
	}
	if _, err := db.Exec(monitor.BackupExecutionsSchema); err != nil {
		return nil, fmt.Errorf("failed to ensure retention schema: %w", err)
	}

	return &Manager{cfg: cfg, db: db}, nil
}

// Close releases database resources.
func (m *Manager) Close() error {
	if m.db == nil {
		return nil
	}
	return m.db.Close()
}

// Apply executes retention policy cleanup for a backup job.
func (m *Manager) Apply(ctx context.Context, backupName string, dryRun bool) ([]DeletedBackup, error) {
	job, ok := m.cfg.Databases[backupName]
	if !ok {
		return nil, fmt.Errorf("backup '%s' not found", backupName)
	}
	policy := Policy{
		KeepLast: job.Retention.KeepLast,
		KeepDays: job.Retention.KeepDays,
		DryRun:   dryRun,
	}

	records, err := m.fetchRecords(ctx, backupName)
	if err != nil {
		return nil, err
	}
	candidates := CalculateCandidates(records, policy, time.Now().UTC())
	candidates = domainret.ProtectActiveBaseline(candidates, records)
	if dryRun {
		return candidatesToDeleted(candidates), nil
	}

	deleted, errs := DeleteCandidates(ctx, candidates, job.Storage.Type, job.Storage)
	if len(errs) > 0 {
		return deleted, fmt.Errorf("retention delete failed: %v", errs[0])
	}

	if err := m.deleteRecords(ctx, backupName, candidates); err != nil {
		return deleted, err
	}
	return deleted, nil
}

// ApplyRestoreRetention cleans up old restore execution records based on retention policy.
// This keeps the restore_executions table clean while respecting configurable retention rules.
func (m *Manager) ApplyRestoreRetention(ctx context.Context, restoreName string, policy Policy) error {
	now := time.Now().UTC()

	// Build deletion query based on retention policy
	args := []interface{}{restoreName, "success"}

	query := `DELETE FROM restore_executions WHERE restore_name = ? AND status = ?`

	if policy.KeepLast > 0 {
		// Keep only the latest N successful restores
		query = `DELETE FROM restore_executions WHERE restore_name = ? AND status = ? AND id NOT IN (
			SELECT id FROM restore_executions 
			WHERE restore_name = ? AND status = ? 
			ORDER BY timestamp DESC LIMIT ?
		)`
		args = []interface{}{restoreName, "success", restoreName, "success", policy.KeepLast}
	} else if policy.KeepDays > 0 {
		// Keep restores from last N days
		cutoffTime := now.AddDate(0, 0, -policy.KeepDays)
		query += ` AND timestamp < ?`
		args = append(args, cutoffTime)
	}

	if policy.DryRun {
		// Just count what would be deleted
		countQuery := strings.ReplaceAll(query, "DELETE FROM restore_executions", "SELECT COUNT(*) FROM restore_executions")
		var count int
		if err := m.db.QueryRowContext(ctx, countQuery, args...).Scan(&count); err != nil {
			return fmt.Errorf("failed to count restore deletion candidates: %w", err)
		}
		return nil
	}

	_, err := m.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("failed to apply restore retention: %w", err)
	}

	return nil
}

// ApplyAll executes retention policies for all jobs.
func (m *Manager) ApplyAll(ctx context.Context, dryRun bool) (ApplySummary, error) {
	summary := ApplySummary{ByBackupJob: make(map[string]int)}

	for name, job := range m.cfg.Databases {
		if job.Retention.KeepLast == 0 && job.Retention.KeepDays == 0 {
			continue
		}
		deleted, err := m.Apply(ctx, name, dryRun)
		if err != nil {
			summary.Errors = append(summary.Errors, err.Error())
			continue
		}
		summary.ByBackupJob[name] = len(deleted)
		summary.TotalDeleted += int64(len(deleted))
		for _, item := range deleted {
			summary.TotalSize += item.FileSize
		}
	}

	if len(summary.Errors) > 0 {
		return summary, fmt.Errorf("retention apply completed with errors")
	}
	return summary, nil
}

// ListCandidates returns backups matching retention criteria without deleting.
func (m *Manager) ListCandidates(ctx context.Context, backupName string) ([]BackupCandidate, error) {
	job, ok := m.cfg.Databases[backupName]
	if !ok {
		return nil, fmt.Errorf("backup '%s' not found", backupName)
	}
	policy := Policy{KeepLast: job.Retention.KeepLast, KeepDays: job.Retention.KeepDays, DryRun: true}
	records, err := m.fetchRecords(ctx, backupName)
	if err != nil {
		return nil, err
	}
	return CalculateCandidates(records, policy, time.Now().UTC()), nil
}

func (m *Manager) fetchRecords(ctx context.Context, backupName string) ([]BackupRecord, error) {
	rows, err := m.db.QueryContext(ctx, `
		SELECT file_path, file_size_bytes, timestamp, status, backup_type, chain_id, chain_index
		FROM backup_executions
		WHERE backup_name = ? AND status = 'success'
		ORDER BY timestamp DESC
	`, backupName)
	if err != nil {
		return nil, fmt.Errorf("failed to query backup records: %w", err)
	}
	defer rows.Close()

	var records []BackupRecord
	for rows.Next() {
		var filePath sql.NullString
		var fileSize sql.NullInt64
		var timestamp string
		var status string
		var backupType sql.NullString
		var chainID sql.NullString
		var chainIndex sql.NullInt64
		if err := rows.Scan(&filePath, &fileSize, &timestamp, &status, &backupType, &chainID, &chainIndex); err != nil {
			return nil, fmt.Errorf("failed to scan backup record: %w", err)
		}
		parsed, err := parseTimestamp(timestamp)
		if err != nil {
			return nil, err
		}
		records = append(records, BackupRecord{
			FilePath:   filePath.String,
			FileSize:   fileSize.Int64,
			Timestamp:  parsed,
			Status:     status,
			BackupType: backupType.String,
			ChainID:    chainID.String,
			ChainIndex: int(chainIndex.Int64),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to read backup records: %w", err)
	}

	return records, nil
}

func (m *Manager) deleteRecords(ctx context.Context, backupName string, candidates []BackupCandidate) error {
	if len(candidates) == 0 {
		return nil
	}

	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin retention transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	stmt, err := tx.PrepareContext(ctx, `
		DELETE FROM backup_executions WHERE backup_name = ? AND file_path = ?
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare retention delete: %w", err)
	}
	defer stmt.Close()

	for _, cand := range candidates {
		if _, err := stmt.ExecContext(ctx, backupName, cand.FilePath); err != nil {
			return fmt.Errorf("failed to delete backup record: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit retention transaction: %w", err)
	}
	return nil
}

func candidatesToDeleted(candidates []BackupCandidate) []DeletedBackup {
	deleted := make([]DeletedBackup, 0, len(candidates))
	for _, cand := range candidates {
		deleted = append(deleted, DeletedBackup{
			FilePath:      cand.FilePath,
			FileSize:      cand.FileSize,
			DeletionTime:  time.Now().UTC(),
			ReasonDeleted: cand.ReasonDeleted,
		})
	}
	return deleted
}

func parseTimestamp(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, fmt.Errorf("timestamp is required")
	}
	formats := []string{time.RFC3339, "2006-01-02 15:04:05", time.RFC3339Nano}
	for _, format := range formats {
		if parsed, err := time.Parse(format, value); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("unsupported timestamp format '%s'", value)
}

func expandHome(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("history_db_path is required")
	}
	if !strings.HasPrefix(path, "~") {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to resolve home directory: %w", err)
	}
	return filepath.Join(home, strings.TrimPrefix(path, "~/")), nil
}

func ensureDir(path string) error {
	if path == "" {
		return nil
	}
	return os.MkdirAll(path, 0o755)
}
