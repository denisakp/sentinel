-- Migration: 002_add_cleanup_columns
-- Description: Add V1 consolidation columns for fail-safe execution recovery
-- Adds: finished_at, cleanup_attempted, cleanup_succeeded, cleanup_error, updated_at

-- Add columns to backup_executions
ALTER TABLE backup_executions ADD COLUMN finished_at DATETIME;
ALTER TABLE backup_executions ADD COLUMN cleanup_attempted INTEGER DEFAULT 0;
ALTER TABLE backup_executions ADD COLUMN cleanup_succeeded INTEGER;
ALTER TABLE backup_executions ADD COLUMN cleanup_error TEXT;
ALTER TABLE backup_executions ADD COLUMN updated_at DATETIME DEFAULT CURRENT_TIMESTAMP;

-- Add columns to restore_executions
ALTER TABLE restore_executions ADD COLUMN finished_at DATETIME;
ALTER TABLE restore_executions ADD COLUMN cleanup_attempted INTEGER DEFAULT 0;
ALTER TABLE restore_executions ADD COLUMN cleanup_succeeded INTEGER;
ALTER TABLE restore_executions ADD COLUMN cleanup_error TEXT;
ALTER TABLE restore_executions ADD COLUMN updated_at DATETIME DEFAULT CURRENT_TIMESTAMP;
