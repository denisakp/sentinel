package integration_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"

	_ "modernc.org/sqlite"
)

var (
	buildOnce sync.Once
	binPath   string
	buildErr  error
)

func buildSentinel(t *testing.T) string {
	t.Helper()
	buildOnce.Do(func() {
		tmpDir, err := os.MkdirTemp("", "sentinel-bin-*")
		if err != nil {
			buildErr = err
			return
		}
		binPath = filepath.Join(tmpDir, "sentinel")

		cmd := exec.Command("go", "build", "-o", binPath, "./")
		cmd.Env = os.Environ()
		cmd.Dir = projectRoot(t)
		buildErr = cmd.Run()
	})

	if buildErr != nil {
		t.Fatalf("failed to build sentinel: %v", buildErr)
	}

	return binPath
}

func projectRoot(t *testing.T) string {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	// tests/integration -> project root is two levels up
	return filepath.Dir(filepath.Dir(cwd))
}

// TestDB represents a temporary SQLite database for integration testing.
type TestDB struct {
	Path string
	DB   *sql.DB
}

// NewTestDB creates a temporary SQLite database for testing.
// The database is created in a temporary directory and should be cleaned up with Close().
func NewTestDB(t *testing.T) *TestDB {
	t.Helper()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_sentinel.db")

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}

	// Enable foreign keys and set pragmas for test performance
	pragmas := []string{
		"PRAGMA foreign_keys = ON",
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous = NORMAL",
	}
	for _, pragma := range pragmas {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			t.Fatalf("failed to set pragma: %v", err)
		}
	}

	return &TestDB{
		Path: dbPath,
		DB:   db,
	}
}

// Close closes the database connection and removes the temporary file.
func (tdb *TestDB) Close() error {
	if tdb.DB != nil {
		if err := tdb.DB.Close(); err != nil {
			return fmt.Errorf("failed to close test database: %w", err)
		}
	}
	if tdb.Path != "" {
		if err := os.Remove(tdb.Path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove test database file: %w", err)
		}
	}
	return nil
}

// InitSchema initializes the database schema for testing.
// This applies the baseline schema without migration tracking for simple test scenarios.
func (tdb *TestDB) InitSchema(t *testing.T) {
	t.Helper()

	schema := `
	CREATE TABLE IF NOT EXISTS backup_executions (
		id TEXT PRIMARY KEY,
		backup_name TEXT NOT NULL,
		database_type TEXT NOT NULL,
		timestamp DATETIME NOT NULL,
		duration_ms INTEGER,
		status TEXT NOT NULL,
		error_message TEXT,
		storage_backend TEXT NOT NULL,
		file_path TEXT NOT NULL,
		file_size_bytes INTEGER,
		checksum TEXT,
		created_at DATETIME NOT NULL,
		finished_at DATETIME,
		cleanup_attempted INTEGER DEFAULT 0,
		cleanup_succeeded INTEGER,
		cleanup_error TEXT,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_backup_name ON backup_executions(backup_name);
	CREATE INDEX IF NOT EXISTS idx_status ON backup_executions(status);
	CREATE INDEX IF NOT EXISTS idx_timestamp ON backup_executions(timestamp DESC);

	CREATE TABLE IF NOT EXISTS restore_executions (
		id TEXT PRIMARY KEY,
		restore_name TEXT NOT NULL,
		database_type TEXT NOT NULL,
		database_name TEXT NOT NULL,
		timestamp DATETIME NOT NULL,
		duration_ms INTEGER,
		status TEXT NOT NULL,
		error_message TEXT,
		source_backup_path TEXT NOT NULL,
		bytes_restored INTEGER,
		verification_passed INTEGER DEFAULT 0,
		created_at DATETIME NOT NULL,
		finished_at DATETIME,
		cleanup_attempted INTEGER DEFAULT 0,
		cleanup_succeeded INTEGER,
		cleanup_error TEXT,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_restore_name ON restore_executions(restore_name);
	CREATE INDEX IF NOT EXISTS idx_restore_timestamp ON restore_executions(timestamp DESC);
	`

	if _, err := tdb.DB.Exec(schema); err != nil {
		t.Fatalf("failed to initialize test schema: %v", err)
	}
}

// InsertExecution inserts a test execution record.
func (tdb *TestDB) InsertExecution(t *testing.T, id, backupName, status string) {
	t.Helper()

	query := `INSERT INTO backup_executions 
		(id, backup_name, database_type, timestamp, status, storage_backend, file_path, created_at)
		VALUES (?, ?, 'postgres', datetime('now'), ?, 'local', '/tmp/test.backup', datetime('now'))`

	_, err := tdb.DB.Exec(query, id, backupName, status)
	if err != nil {
		t.Fatalf("failed to insert test execution: %v", err)
	}
}

// CountExecutions returns the count of executions matching the given status.
func (tdb *TestDB) CountExecutions(t *testing.T, status string) int {
	t.Helper()

	var count int
	query := `SELECT COUNT(*) FROM backup_executions WHERE status = ?`
	err := tdb.DB.QueryRow(query, status).Scan(&count)
	if err != nil {
		t.Fatalf("failed to count executions: %v", err)
	}
	return count
}

// GetExecution retrieves an execution record by ID for assertion.
func (tdb *TestDB) GetExecution(t *testing.T, id string) map[string]interface{} {
	t.Helper()

	query := `SELECT id, backup_name, status, error_message, cleanup_attempted, cleanup_succeeded, cleanup_error 
		FROM backup_executions WHERE id = ?`

	var execID, backupName, status string
	var errorMsg, cleanupError sql.NullString
	var cleanupAttempted int
	var cleanupSucceeded sql.NullInt64

	err := tdb.DB.QueryRow(query, id).Scan(&execID, &backupName, &status, &errorMsg, &cleanupAttempted, &cleanupSucceeded, &cleanupError)
	if err != nil {
		t.Fatalf("failed to get execution %s: %v", id, err)
	}

	result := map[string]interface{}{
		"id":                execID,
		"backup_name":       backupName,
		"status":            status,
		"cleanup_attempted": cleanupAttempted == 1,
	}

	if errorMsg.Valid {
		result["error_message"] = errorMsg.String
	}
	if cleanupSucceeded.Valid {
		result["cleanup_succeeded"] = cleanupSucceeded.Int64 == 1
	}
	if cleanupError.Valid {
		result["cleanup_error"] = cleanupError.String
	}

	return result
}

// ExecContext executes a query with context for testing transaction scenarios.
func (tdb *TestDB) ExecContext(ctx context.Context, query string, args ...interface{}) error {
	_, err := tdb.DB.ExecContext(ctx, query, args...)
	return err
}
