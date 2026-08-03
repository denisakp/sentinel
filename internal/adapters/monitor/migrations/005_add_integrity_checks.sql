-- Migration: 005_add_integrity_checks
-- Description: Add the integrity_checks audit table for scheduled/manual
--              repository integrity sweeps (spec 052 / PRD 35). Each sweep
--              writes one row per verified artifact, grouped by run_id and
--              labelled by trigger. The result and trigger CHECK constraints
--              enforce the outcome/trigger vocabularies at the store boundary.

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
