package monitor

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"time"

	"github.com/denisakp/sentinel/internal/lock"
)

const (
	migrationLockJob     = "monitor.migrate"
	migrationLockTimeout = 30 * time.Second
	migrationStaleAge    = 5 * time.Minute
)

// runMigrationsLocked is the version-gated, cross-process-serialized
// migration entry point used by NewMonitor.
//
// Behaviour:
//   - Ensures schema_version exists.
//   - If current > BinarySchemaVersion → returns ErrForwardIncompatible.
//   - If current < BinarySchemaVersion → acquires a file lock next to the DB,
//     re-reads the version under the lock (handles racing peers), runs all
//     pending SQL migrations, and stamps schema_version to BinarySchemaVersion.
//   - If current == BinarySchemaVersion → no-op, no lock acquired.
//
// dbPath MUST be the absolute path of the SQLite file; the lock file lives
// in the same directory as a sibling named "<basename>.migrate.lock".
func runMigrationsLocked(dbPath string, db *sql.DB) error {
	if _, err := db.Exec(SchemaVersionTable); err != nil {
		return fmt.Errorf("failed to ensure schema_version table: %w", err)
	}

	current, err := readSchemaVersion(db)
	if err != nil {
		return err
	}

	if current > BinarySchemaVersion {
		slog.Error("monitor schema forward-incompatible",
			"event", "monitor_schema_forward_incompatible",
			"db_path", dbPath,
			"current_version", current,
			"required_version", BinarySchemaVersion,
		)
		return newForwardIncompatible(current, BinarySchemaVersion)
	}

	if current == BinarySchemaVersion {
		return nil
	}

	if current < 0 {
		return fmt.Errorf("%w: version %d", ErrUnsupportedSourceVersion, current)
	}

	dir := filepath.Dir(dbPath)
	mgr := lock.NewManager(dir)
	if _, err := mgr.AcquireWithTimeout(context.Background(), migrationLockJob, migrationStaleAge, migrationLockTimeout); err != nil {
		return fmt.Errorf("failed to acquire migration lock at %s: %w", filepath.Join(dir, migrationLockJob+".lock"), err)
	}
	defer func() { _ = mgr.Release(migrationLockJob) }()

	// Re-read under the lock — a peer may have completed the migration while we
	// waited.
	current, err = readSchemaVersion(db)
	if err != nil {
		return err
	}
	if current >= BinarySchemaVersion {
		if current > BinarySchemaVersion {
			return newForwardIncompatible(current, BinarySchemaVersion)
		}
		return nil
	}

	slog.Info("monitor schema migration starting",
		"event", "monitor_schema_migration_starting",
		"db_path", dbPath,
		"current_version", current,
		"required_version", BinarySchemaVersion,
	)

	// Run the embedded SQL migrations. The existing runMigrations applies
	// each pending file in its own transaction and inserts into
	// schema_migrations. After it returns successfully, stamp schema_version.
	if err := runMigrations(db); err != nil {
		slog.Error("monitor schema migration failed",
			"event", "monitor_schema_migration_failed",
			"db_path", dbPath,
			"current_version", current,
			"required_version", BinarySchemaVersion,
			"error", err.Error(),
		)
		return fmt.Errorf("monitor migration failed: %w", err)
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin schema_version commit tx: %w", err)
	}
	if err := writeSchemaVersion(tx, BinarySchemaVersion); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit schema_version bump: %w", err)
	}

	slog.Info("monitor schema migration complete",
		"event", "monitor_schema_migration_complete",
		"db_path", dbPath,
		"applied_version", BinarySchemaVersion,
	)
	return nil
}

// IsForwardIncompatible reports whether err originated from a
// forward-incompatible monitor database. Convenience for CLI exit-code wiring.
func IsForwardIncompatible(err error) bool { return errors.Is(err, ErrForwardIncompatible) }
