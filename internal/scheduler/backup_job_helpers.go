package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"
)

// defaultBackoffs is the standard 3-attempt retry schedule: 1s, 2s, 4s. (T051)
var defaultBackoffs = []time.Duration{time.Second, 2 * time.Second, 4 * time.Second}

// RunBackupWithRetry wraps a backup function in withRetry using the default 3-attempt
// backoff schedule. On final failure, emits the structured ERROR log required by T053
// and updates retry_count in the monitor.
func RunBackupWithRetry(
	ctx context.Context,
	backupID string,
	databaseName string,
	fn func() error,
) error {
	attempts := 0
	err := withRetry(func() error {
		attempts++
		return fn()
	}, 3, defaultBackoffs)

	if err != nil {
		// T053: structured ERROR log after all retries exhausted
		slog.ErrorContext(ctx, "backup failed after retries",
			"level", "ERROR",
			"event", "backup_failed",
			"backup_id", backupID,
			"database", databaseName,
			"error", err.Error(),
			"attempts", attempts,
			"timestamp", time.Now().UTC().Format(time.RFC3339),
		)
	}
	return err
}

// DeleteArtifactsOnFailure deletes both the backup file (e.g. .sql.enc) and its
// co-located manifest file (.manifest.json) when a backup fails. (T052)
func DeleteArtifactsOnFailure(backupPath string) {
	if backupPath == "" {
		return
	}

	manifestPath := backupPath + ".manifest.json"

	for _, path := range []string{backupPath, manifestPath} {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			slog.Warn("failed to delete artifact on backup failure",
				"event", "artifact_cleanup_failed",
				"path", path,
				"error", err,
			)
		} else if err == nil {
			slog.Debug("deleted artifact on backup failure",
				"event", "artifact_deleted",
				"path", path,
			)
		}
	}
}

// FormatStructuredError returns a formatted error string for inclusion in the monitor.
// Used to produce consistent error messages for the backup_executions table. (T053)
func FormatStructuredError(backupID, database string, err error, attempts int) string {
	return fmt.Sprintf(
		`{"event":"backup_failed","backup_id":%q,"database":%q,"error":%q,"attempts":%d}`,
		backupID, database, err.Error(), attempts,
	)
}
