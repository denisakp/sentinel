-- Migration: 004_add_security_columns
-- Description: Add integrity verification and encryption metadata columns to backup_executions.
-- Adds: hash_algorithm, hash_value, plaintext_hash_value, encrypted, encryption_key_hint,
--       manifest_path, retry_count

ALTER TABLE backup_executions ADD COLUMN hash_algorithm TEXT;
ALTER TABLE backup_executions ADD COLUMN hash_value TEXT;
ALTER TABLE backup_executions ADD COLUMN plaintext_hash_value TEXT;
ALTER TABLE backup_executions ADD COLUMN encrypted INTEGER DEFAULT 0;
ALTER TABLE backup_executions ADD COLUMN encryption_key_hint TEXT;
ALTER TABLE backup_executions ADD COLUMN manifest_path TEXT;
ALTER TABLE backup_executions ADD COLUMN retry_count INTEGER DEFAULT 0;
