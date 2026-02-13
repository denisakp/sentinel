package monitor

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

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

	return nil
}
