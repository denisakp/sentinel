-- Migration: 003_add_consolidated_status_values
-- Description: Extend status CHECK constraint to include V1 consolidated status values
-- Adds: pending, running, completed, failed, interrupted (alongside legacy success, failure, in-progress)

-- SQLite does not support ALTER TABLE ... ALTER COLUMN to modify CHECK constraints
-- Therefore, we must recreate the tables with the new constraint
-- This migration preserves all existing data while updating the CHECK constraint

-- Create temporary backup table with new status constraint
CREATE TABLE backup_executions_new (
	id TEXT PRIMARY KEY,
	backup_name TEXT NOT NULL,
	database_type TEXT NOT NULL,
	timestamp DATETIME NOT NULL,
	status TEXT NOT NULL CHECK (status IN ('pending', 'running', 'completed', 'failed', 'interrupted', 'success', 'failure', 'in-progress')),
	duration_ms INTEGER,
	storage_backend TEXT NOT NULL,
	file_path TEXT NOT NULL,
	file_size_bytes INTEGER,
	error_message TEXT,
	checksum TEXT,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	finished_at DATETIME,
	cleanup_attempted INTEGER DEFAULT 0,
	cleanup_succeeded INTEGER,
	cleanup_error TEXT,
	updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Copy data from old table
INSERT INTO backup_executions_new 
SELECT id, backup_name, database_type, timestamp, status, duration_ms, storage_backend, 
       file_path, file_size_bytes, error_message, checksum, created_at, 
       finished_at, cleanup_attempted, cleanup_succeeded, cleanup_error, updated_at
FROM backup_executions;

-- Drop old table and rename new table
DROP TABLE backup_executions;
ALTER TABLE backup_executions_new RENAME TO backup_executions;

-- Recreate indexes
CREATE INDEX idx_backup_executions_backup_name ON backup_executions(backup_name);
CREATE INDEX idx_backup_executions_timestamp ON backup_executions(timestamp DESC);
CREATE INDEX idx_backup_executions_status ON backup_executions(status);
CREATE INDEX idx_backup_executions_backup_name_timestamp ON backup_executions(backup_name, timestamp DESC);
CREATE INDEX idx_backup_executions_database_type ON backup_executions(database_type);
CREATE INDEX idx_backup_executions_storage_backend ON backup_executions(storage_backend);

-- Create temporary restore table with new status constraint
CREATE TABLE restore_executions_new (
	id TEXT PRIMARY KEY,
	restore_name TEXT NOT NULL,
	database_type TEXT NOT NULL,
	database_name TEXT NOT NULL,
	timestamp DATETIME NOT NULL,
	status TEXT NOT NULL CHECK (status IN ('pending', 'running', 'completed', 'failed', 'interrupted', 'success', 'failure', 'in-progress')),
	duration_ms INTEGER,
	source_backup_path TEXT NOT NULL,
	bytes_restored INTEGER,
	verification_passed BOOLEAN,
	error_message TEXT,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	finished_at DATETIME,
	cleanup_attempted INTEGER DEFAULT 0,
	cleanup_succeeded INTEGER,
	cleanup_error TEXT,
	updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Copy data from old table
INSERT INTO restore_executions_new
SELECT id, restore_name, database_type, database_name, timestamp, status, duration_ms, 
       source_backup_path, bytes_restored, verification_passed, error_message, created_at,
       finished_at, cleanup_attempted, cleanup_succeeded, cleanup_error, updated_at
FROM restore_executions;

-- Drop old table and rename new table
DROP TABLE restore_executions;
ALTER TABLE restore_executions_new RENAME TO restore_executions;

-- Recreate indexes
CREATE INDEX idx_restore_executions_restore_name ON restore_executions(restore_name);
CREATE INDEX idx_restore_executions_timestamp ON restore_executions(timestamp DESC);
CREATE INDEX idx_restore_executions_status ON restore_executions(status);
CREATE INDEX idx_restore_executions_database_type ON restore_executions(database_type);
