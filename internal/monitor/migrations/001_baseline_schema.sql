-- Migration: 001_baseline_schema
-- Description: Initial backup and restore execution history tables
-- Applied: Auto-applied for new installations

CREATE TABLE IF NOT EXISTS backup_executions (
	id TEXT PRIMARY KEY,
	backup_name TEXT NOT NULL,
	database_type TEXT NOT NULL,
	timestamp DATETIME NOT NULL,
	status TEXT NOT NULL CHECK (status IN ('success', 'failure', 'in-progress')),
	duration_ms INTEGER,
	storage_backend TEXT NOT NULL,
	file_path TEXT NOT NULL,
	file_size_bytes INTEGER,
	error_message TEXT,
	checksum TEXT,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_backup_executions_backup_name ON backup_executions(backup_name);
CREATE INDEX IF NOT EXISTS idx_backup_executions_timestamp ON backup_executions(timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_backup_executions_status ON backup_executions(status);
CREATE INDEX IF NOT EXISTS idx_backup_executions_backup_name_timestamp ON backup_executions(backup_name, timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_backup_executions_database_type ON backup_executions(database_type);
CREATE INDEX IF NOT EXISTS idx_backup_executions_storage_backend ON backup_executions(storage_backend);

CREATE TABLE IF NOT EXISTS restore_executions (
	id TEXT PRIMARY KEY,
	restore_name TEXT NOT NULL,
	database_type TEXT NOT NULL,
	database_name TEXT NOT NULL,
	timestamp DATETIME NOT NULL,
	status TEXT NOT NULL CHECK (status IN ('success', 'failure', 'in-progress')),
	duration_ms INTEGER,
	source_backup_path TEXT NOT NULL,
	bytes_restored INTEGER,
	verification_passed BOOLEAN,
	error_message TEXT,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_restore_executions_restore_name ON restore_executions(restore_name);
CREATE INDEX IF NOT EXISTS idx_restore_executions_timestamp ON restore_executions(timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_restore_executions_status ON restore_executions(status);
CREATE INDEX IF NOT EXISTS idx_restore_executions_database_type ON restore_executions(database_type);
