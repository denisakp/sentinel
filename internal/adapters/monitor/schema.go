package monitor

const BackupExecutionsSchema = `
CREATE TABLE IF NOT EXISTS backup_executions (
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
	backup_type TEXT,
	chain_id TEXT,
	chain_index INTEGER,
	delta_size_bytes INTEGER,
	full_backup_size_bytes INTEGER,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	finished_at DATETIME,
	cleanup_attempted INTEGER DEFAULT 0,
	cleanup_succeeded INTEGER,
	cleanup_error TEXT,
	updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Indexes for efficient querying
CREATE INDEX IF NOT EXISTS idx_backup_executions_backup_name ON backup_executions(backup_name);
CREATE INDEX IF NOT EXISTS idx_backup_executions_timestamp ON backup_executions(timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_backup_executions_status ON backup_executions(status);
CREATE INDEX IF NOT EXISTS idx_backup_executions_backup_name_timestamp ON backup_executions(backup_name, timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_backup_executions_database_type ON backup_executions(database_type);
CREATE INDEX IF NOT EXISTS idx_backup_executions_storage_backend ON backup_executions(storage_backend);
`

const RestoreExecutionsSchema = `
CREATE TABLE IF NOT EXISTS restore_executions (
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

-- Indexes for efficient querying
CREATE INDEX IF NOT EXISTS idx_restore_executions_restore_name ON restore_executions(restore_name);
CREATE INDEX IF NOT EXISTS idx_restore_executions_timestamp ON restore_executions(timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_restore_executions_status ON restore_executions(status);
CREATE INDEX IF NOT EXISTS idx_restore_executions_restore_name_timestamp ON restore_executions(restore_name, timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_restore_executions_database_type ON restore_executions(database_type);
CREATE INDEX IF NOT EXISTS idx_restore_executions_database_name ON restore_executions(database_name);
`

// IntegrityChecksSchema mirrors migration 005_add_integrity_checks.sql as a
// defensive CREATE TABLE IF NOT EXISTS, matching the per-table const pattern of
// BackupExecutionsSchema / RestoreExecutionsSchema (spec 052 / PRD 35). The
// result and trigger CHECK constraints enforce the outcome/trigger vocabularies
// at the store boundary.
const IntegrityChecksSchema = `
CREATE TABLE IF NOT EXISTS integrity_checks (
	id TEXT PRIMARY KEY,
	run_id TEXT NOT NULL,
	backup_id TEXT,
	job_name TEXT,
	result TEXT NOT NULL CHECK (result IN ('ok', 'corrupted', 'missing_artifact', 'missing_manifest')),
	stored_hash TEXT,
	computed_hash TEXT,
	storage_backend TEXT,
	artifact_path TEXT,
	checked_at DATETIME NOT NULL,
	trigger TEXT NOT NULL DEFAULT 'manual' CHECK (trigger IN ('manual', 'scheduled')),
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_integrity_checks_run_id ON integrity_checks(run_id);
CREATE INDEX IF NOT EXISTS idx_integrity_checks_result ON integrity_checks(result);
CREATE INDEX IF NOT EXISTS idx_integrity_checks_checked_at ON integrity_checks(checked_at DESC);
`

// SchemaMigrationsTable tracks applied schema migrations for version control.
const SchemaMigrationsTable = `
CREATE TABLE IF NOT EXISTS schema_migrations (
	version INTEGER PRIMARY KEY,
	name TEXT NOT NULL,
	checksum TEXT,
	applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_schema_migrations_applied_at ON schema_migrations(applied_at DESC);
`
