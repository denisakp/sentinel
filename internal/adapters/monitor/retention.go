package monitor

// Retention-side history DELETEs, implementing the two ports.Recorder
// retention methods. SQL bodies ported verbatim
// from internal/retention/retention.go::Manager.deleteRecords and
// Manager.ApplyRestoreRetention.

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/denisakp/sentinel/internal/domain/retention"
)

// RetentionDeleteRecords deletes backup_executions rows matching the given
// candidate file paths for jobName. Empty candidates is a no-op success.
// Idempotent: deleting a non-existent (jobName, filePath) pair returns nil.
func (m *Monitor) RetentionDeleteRecords(ctx context.Context, jobName string, candidates []retention.BackupCandidate) error {
	if len(candidates) == 0 {
		return nil
	}
	if m == nil || m.db == nil {
		return fmt.Errorf("retention delete: monitor database is not initialized")
	}

	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("retention delete: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	stmt, err := tx.PrepareContext(ctx, `
		DELETE FROM backup_executions WHERE backup_name = ? AND file_path = ?
	`)
	if err != nil {
		return fmt.Errorf("retention delete: %w", err)
	}
	defer stmt.Close()

	for _, cand := range candidates {
		if _, err := stmt.ExecContext(ctx, jobName, cand.FilePath); err != nil {
			return fmt.Errorf("retention delete: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("retention delete: %w", err)
	}
	return nil
}

// DeleteRestoreExecutions deletes restore_executions rows for jobName
// according to the retention policy. With policy.DryRun it only counts and
// returns nil without deleting anything.
func (m *Monitor) DeleteRestoreExecutions(ctx context.Context, jobName string, policy retention.Policy) error {
	if m == nil || m.db == nil {
		return fmt.Errorf("delete restore executions: monitor database is not initialized")
	}
	now := time.Now().UTC()

	args := []interface{}{jobName, "success"}
	query := `DELETE FROM restore_executions WHERE restore_name = ? AND status = ?`

	if policy.KeepLast > 0 {
		// Keep only the latest N successful restores
		query = `DELETE FROM restore_executions WHERE restore_name = ? AND status = ? AND id NOT IN (
			SELECT id FROM restore_executions
			WHERE restore_name = ? AND status = ?
			ORDER BY timestamp DESC LIMIT ?
		)`
		args = []interface{}{jobName, "success", jobName, "success", policy.KeepLast}
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
			return fmt.Errorf("delete restore executions: %w", err)
		}
		return nil
	}

	if _, err := m.db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("delete restore executions: %w", err)
	}
	return nil
}
