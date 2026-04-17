package monitor

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Monitor provides access to backup history records.
type Monitor struct {
	db *sql.DB
}

// NewMonitor opens (or creates) the history database and ensures schema exists.
func NewMonitor(dbPath string) (*Monitor, error) {
	if dbPath == "" {
		return nil, fmt.Errorf("history_db_path is required")
	}

	path, err := expandHome(dbPath)
	if err != nil {
		return nil, err
	}
	if err := ensureDir(filepath.Dir(path)); err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("failed to open history database: %w", err)
	}

	// Ensure schema_migrations table exists first
	if _, err := db.Exec(SchemaMigrationsTable); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to ensure schema_migrations table: %w", err)
	}

	// Run pending migrations before applying legacy schemas
	if err := runMigrations(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	// Legacy schema application (for backwards compatibility)
	if _, err := db.Exec(BackupExecutionsSchema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to ensure backup history schema: %w", err)
	}
	if _, err := db.Exec(RestoreExecutionsSchema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to ensure restore history schema: %w", err)
	}
	if err := ensureSchemaColumns(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to ensure history columns: %w", err)
	}

	return &Monitor{db: db}, nil
}

// ReconcileStaleExecutions detects and marks stale running executions as interrupted.
// This should be called during application startup to finalize any executions that were
// running when the process was stopped or crashed.
func (m *Monitor) ReconcileStaleExecutions(ctx context.Context) (int, error) {
	if m == nil || m.db == nil {
		return 0, fmt.Errorf("monitor database is not initialized")
	}

	// Get all stale running executions (status='running' or legacy 'in-progress')
	staleExecutions, err := m.GetStaleRunningExecutions(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to query stale executions: %w", err)
	}

	if len(staleExecutions) == 0 {
		return 0, nil // No stale executions to reconcile
	}

	reconciledCount := 0
	for _, exec := range staleExecutions {
		// Mark as interrupted with no cleanup attempt (artifacts may or may not exist)
		// The next backup/restore operation will overwrite any partial artifacts
		if err := m.RecordInterrupted(ctx, exec.ID, false, nil, ""); err != nil {
			return reconciledCount, fmt.Errorf("failed to mark execution %s as interrupted: %w", exec.ID, err)
		}
		reconciledCount++
	}

	return reconciledCount, nil
}

// Close releases database resources.
func (m *Monitor) Close() error {
	if m == nil || m.db == nil {
		return nil
	}
	return m.db.Close()
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

func ensureSchemaColumns(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(backup_executions)`)
	if err != nil {
		return fmt.Errorf("failed to read schema info: %w", err)
	}
	defer rows.Close()

	columns := map[string]struct{}{}
	for rows.Next() {
		var cid int
		var name string
		var ctype string
		var notnull int
		var dflt sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return fmt.Errorf("failed to scan schema info: %w", err)
		}
		columns[name] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("failed to read schema info: %w", err)
	}

	addColumn := func(name, definition string) error {
		if _, ok := columns[name]; ok {
			return nil
		}
		stmt := fmt.Sprintf("ALTER TABLE backup_executions ADD COLUMN %s %s", name, definition)
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("failed to add column %s: %w", name, err)
		}
		return nil
	}

	if err := addColumn("database_type", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := addColumn("storage_backend", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := addColumn("file_path", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := addColumn("file_size_bytes", "INTEGER"); err != nil {
		return err
	}
	if err := addColumn("checksum", "TEXT"); err != nil {
		return err
	}
	if err := addColumn("backup_type", "TEXT"); err != nil {
		return err
	}
	if err := addColumn("chain_id", "TEXT"); err != nil {
		return err
	}
	if err := addColumn("chain_index", "INTEGER"); err != nil {
		return err
	}
	if err := addColumn("delta_size_bytes", "INTEGER"); err != nil {
		return err
	}
	if err := addColumn("full_backup_size_bytes", "INTEGER"); err != nil {
		return err
	}

	// V1 Consolidation: Add cleanup and interruption tracking columns
	if err := addColumn("finished_at", "DATETIME"); err != nil {
		return err
	}
	if err := addColumn("cleanup_attempted", "INTEGER DEFAULT 0"); err != nil {
		return err
	}
	if err := addColumn("cleanup_succeeded", "INTEGER"); err != nil {
		return err
	}
	if err := addColumn("cleanup_error", "TEXT"); err != nil {
		return err
	}
	if err := addColumn("updated_at", "DATETIME DEFAULT CURRENT_TIMESTAMP"); err != nil {
		return err
	}

	restoreRows, err := db.Query(`PRAGMA table_info(restore_executions)`)
	if err != nil {
		return fmt.Errorf("failed to read restore schema info: %w", err)
	}
	defer restoreRows.Close()

	restoreColumns := map[string]struct{}{}
	for restoreRows.Next() {
		var cid int
		var name string
		var ctype string
		var notnull int
		var dflt sql.NullString
		var pk int
		if err := restoreRows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return fmt.Errorf("failed to scan restore schema info: %w", err)
		}
		restoreColumns[name] = struct{}{}
	}
	if err := restoreRows.Err(); err != nil {
		return fmt.Errorf("failed to read restore schema info: %w", err)
	}

	addRestoreColumn := func(name, definition string) error {
		if _, ok := restoreColumns[name]; ok {
			return nil
		}
		stmt := fmt.Sprintf("ALTER TABLE restore_executions ADD COLUMN %s %s", name, definition)
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("failed to add restore column %s: %w", name, err)
		}
		return nil
	}

	if err := addRestoreColumn("source_type", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := addRestoreColumn("conflict_strategy", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := addRestoreColumn("staged_file_path", "TEXT"); err != nil {
		return err
	}
	if err := addRestoreColumn("staged_file_retained", "INTEGER DEFAULT 0"); err != nil {
		return err
	}
	if err := addRestoreColumn("error_reason", "TEXT"); err != nil {
		return err
	}
	if err := addRestoreColumn("reason", "TEXT"); err != nil {
		return err
	}
	if err := addRestoreColumn("timeout_seconds", "INTEGER"); err != nil {
		return err
	}
	if err := addRestoreColumn("restore_mode", "TEXT"); err != nil {
		return err
	}
	if err := addRestoreColumn("planning_status", "TEXT"); err != nil {
		return err
	}
	if err := addRestoreColumn("requested_pitr_time_utc", "DATETIME"); err != nil {
		return err
	}
	if err := addRestoreColumn("baseline_backup_id", "TEXT"); err != nil {
		return err
	}
	if err := addRestoreColumn("fallback_decision", "TEXT"); err != nil {
		return err
	}
	if err := addRestoreColumn("fallback_reason", "TEXT"); err != nil {
		return err
	}
	if err := addRestoreColumn("fallback_backup_id", "TEXT"); err != nil {
		return err
	}
	if err := addRestoreColumn("chain_depth", "INTEGER"); err != nil {
		return err
	}
	if err := addRestoreColumn("chain_id", "TEXT"); err != nil {
		return err
	}
	if err := addRestoreColumn("assembly_duration_ms", "INTEGER"); err != nil {
		return err
	}
	if err := addRestoreColumn("recovery_timeline_id", "TEXT"); err != nil {
		return err
	}

	if err := ensureRestoreStatusConstraint(db); err != nil {
		return err
	}

	return nil
}

func ensureRestoreStatusConstraint(db *sql.DB) error {
	var tableSQL sql.NullString
	if err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'restore_executions'`).Scan(&tableSQL); err != nil {
		return fmt.Errorf("failed to inspect restore_executions schema: %w", err)
	}
	if !tableSQL.Valid {
		return nil
	}

	schemaLower := strings.ToLower(tableSQL.String)
	if strings.Contains(schemaLower, "'timeout'") && strings.Contains(schemaLower, "'skipped'") {
		return nil
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin restore schema repair transaction: %w", err)
	}

	repairErr := func() error {
		if _, err := tx.Exec(`
CREATE TABLE restore_executions_new (
	id TEXT PRIMARY KEY,
	restore_name TEXT NOT NULL,
	database_type TEXT NOT NULL,
	database_name TEXT NOT NULL,
	restore_mode TEXT,
	planning_status TEXT,
	requested_pitr_time_utc DATETIME,
	baseline_backup_id TEXT,
	fallback_decision TEXT,
	fallback_reason TEXT,
	fallback_backup_id TEXT,
	chain_depth INTEGER,
	chain_id TEXT,
	assembly_duration_ms INTEGER,
	recovery_timeline_id TEXT,
	source_type TEXT NOT NULL DEFAULT '',
	conflict_strategy TEXT NOT NULL DEFAULT '',
	timestamp DATETIME NOT NULL,
	status TEXT NOT NULL CHECK (status IN ('pending', 'running', 'completed', 'failed', 'interrupted', 'success', 'failure', 'in-progress', 'timeout', 'skipped')),
	duration_ms INTEGER,
	source_backup_path TEXT NOT NULL,
	staged_file_path TEXT,
	staged_file_retained INTEGER DEFAULT 0,
	bytes_restored INTEGER,
	verification_passed BOOLEAN,
	error_message TEXT,
	error_reason TEXT,
	reason TEXT,
	timeout_seconds INTEGER,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	finished_at DATETIME,
	cleanup_attempted INTEGER DEFAULT 0,
	cleanup_succeeded INTEGER,
	cleanup_error TEXT,
	updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
`); err != nil {
			return fmt.Errorf("failed to create restore_executions_new table: %w", err)
		}

		if _, err := tx.Exec(`
INSERT INTO restore_executions_new (
	id, restore_name, database_type, database_name, restore_mode, planning_status,
	requested_pitr_time_utc, baseline_backup_id, fallback_decision, fallback_reason, fallback_backup_id, recovery_timeline_id,
	chain_depth, chain_id, assembly_duration_ms,
	source_type, conflict_strategy,
	timestamp, status, duration_ms, source_backup_path, staged_file_path, staged_file_retained,
	bytes_restored, verification_passed, error_message, error_reason, reason, timeout_seconds,
	created_at, finished_at, cleanup_attempted, cleanup_succeeded, cleanup_error, updated_at
)
SELECT
	id, restore_name, database_type, database_name, NULL, NULL,
	NULL, NULL, NULL, NULL, NULL, NULL,
	NULL, NULL, NULL,
	source_type, conflict_strategy,
	timestamp, status, duration_ms, source_backup_path, staged_file_path, staged_file_retained,
	bytes_restored, verification_passed, error_message, error_reason, reason, timeout_seconds,
	created_at, finished_at, cleanup_attempted, cleanup_succeeded, cleanup_error, updated_at
FROM restore_executions;
`); err != nil {
			return fmt.Errorf("failed to copy restore execution rows: %w", err)
		}

		if _, err := tx.Exec(`DROP TABLE restore_executions;`); err != nil {
			return fmt.Errorf("failed to drop old restore_executions table: %w", err)
		}
		if _, err := tx.Exec(`ALTER TABLE restore_executions_new RENAME TO restore_executions;`); err != nil {
			return fmt.Errorf("failed to rename restore_executions_new: %w", err)
		}

		indexStatements := []string{
			`CREATE INDEX IF NOT EXISTS idx_restore_executions_restore_name ON restore_executions(restore_name);`,
			`CREATE INDEX IF NOT EXISTS idx_restore_executions_timestamp ON restore_executions(timestamp DESC);`,
			`CREATE INDEX IF NOT EXISTS idx_restore_executions_status ON restore_executions(status);`,
			`CREATE INDEX IF NOT EXISTS idx_restore_executions_restore_name_timestamp ON restore_executions(restore_name, timestamp DESC);`,
			`CREATE INDEX IF NOT EXISTS idx_restore_executions_database_type ON restore_executions(database_type);`,
			`CREATE INDEX IF NOT EXISTS idx_restore_executions_database_name ON restore_executions(database_name);`,
		}
		for _, stmt := range indexStatements {
			if _, err := tx.Exec(stmt); err != nil {
				return fmt.Errorf("failed to recreate restore index: %w", err)
			}
		}

		return nil
	}()

	if repairErr != nil {
		_ = tx.Rollback()
		return repairErr
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit restore schema repair transaction: %w", err)
	}

	return nil
}

// runMigrations applies all pending migrations in sequential order.
// It reads migration files from embedded FS, checks which are already applied,
// and executes pending ones within a transaction for atomicity.
func runMigrations(db *sql.DB) error {
	// Get list of migration files
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("failed to read migrations directory: %w", err)
	}

	// Parse migration files and sort by version
	var migrations []migrationFile
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		version, name, err := parseMigrationFileName(entry.Name())
		if err != nil {
			return fmt.Errorf("invalid migration file name %s: %w", entry.Name(), err)
		}
		migrations = append(migrations, migrationFile{
			version:  version,
			name:     name,
			fileName: entry.Name(),
		})
	}
	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].version < migrations[j].version
	})

	// Get applied migrations
	appliedVersions, err := getAppliedMigrations(db)
	if err != nil {
		return fmt.Errorf("failed to get applied migrations: %w", err)
	}

	// Apply pending migrations
	for _, mig := range migrations {
		if appliedVersions[mig.version] {
			continue // Already applied
		}

		// Read migration content
		content, err := migrationsFS.ReadFile("migrations/" + mig.fileName)
		if err != nil {
			return fmt.Errorf("failed to read migration %s: %w", mig.fileName, err)
		}

		// Apply migration in transaction
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("failed to begin transaction for migration %s: %w", mig.fileName, err)
		}

		// Execute migration SQL
		if _, err := tx.Exec(string(content)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("failed to execute migration %s: %w", mig.fileName, err)
		}

		// Record migration
		if _, err := tx.Exec(`INSERT INTO schema_migrations (version, name) VALUES (?, ?)`, mig.version, mig.name); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("failed to record migration %s: %w", mig.fileName, err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("failed to commit migration %s: %w", mig.fileName, err)
		}
	}

	return nil
}

// migrationFile represents a parsed migration file.
type migrationFile struct {
	version  int
	name     string
	fileName string
}

// parseMigrationFileName parses migration file name in format "NNN_description.sql"
// and returns version number and description.
func parseMigrationFileName(fileName string) (version int, name string, err error) {
	// Remove .sql extension
	baseName := strings.TrimSuffix(fileName, ".sql")

	// Split on first underscore
	parts := strings.SplitN(baseName, "_", 2)
	if len(parts) != 2 {
		return 0, "", fmt.Errorf("migration file must be in format NNN_description.sql")
	}

	// Parse version
	v, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, "", fmt.Errorf("invalid version number: %w", err)
	}

	return v, parts[1], nil
}

// getAppliedMigrations returns a set of already applied migration versions.
func getAppliedMigrations(db *sql.DB) (map[int]bool, error) {
	rows, err := db.Query(`SELECT version FROM schema_migrations ORDER BY version`)
	if err != nil {
		return nil, fmt.Errorf("failed to query applied migrations: %w", err)
	}
	defer rows.Close()

	applied := make(map[int]bool)
	for rows.Next() {
		var version int
		if err := rows.Scan(&version); err != nil {
			return nil, fmt.Errorf("failed to scan migration version: %w", err)
		}
		applied[version] = true
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to read migration rows: %w", err)
	}

	return applied, nil
}
