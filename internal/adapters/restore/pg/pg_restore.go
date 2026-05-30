package pg

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	backup "github.com/denisakp/sentinel/internal/domain/backup"
)

// RestoreArgs holds arguments for PostgreSQL restore operations
type RestoreArgs struct {
	// Connection parameters
	Host     string
	Port     int
	Username string
	Password string
	Database string

	// Backup source
	BackupPath string // local file path (optional if Storage is provided)

	// Restore options
	PgRestoreFormat string // c=custom, d=directory, t=tar, p=plain (optional)
	Decompress      bool   // auto-decompress if needed
	OnConflict      string // ignore, replace, error (default: error)
	AllowCascade    bool   // required for replace strategy: permits DROP ... CASCADE
	AdditionalArgs  string // extra arguments for pg_restore

	// Cloud storage
	Storage *struct {
		Type    string      // "s3", "gdrive", "local"
		OutName string      // backup name in storage
		Handler interface{} // storage.Storage interface (injected)
	}
}

// Restore restores a PostgreSQL backup from cloud or local storage
func Restore(ctx context.Context, ra *RestoreArgs) error {
	// Validate required arguments
	if err := ValidateRequiredArgs(ra); err != nil {
		return fmt.Errorf("invalid restore arguments - %w", err)
	}

	// Validate optional arguments
	if err := ValidateRestoreFormat(ra.PgRestoreFormat); err != nil {
		return fmt.Errorf("invalid restore format - %w", err)
	}

	if err := ValidateOnConflict(ra.OnConflict); err != nil {
		return fmt.Errorf("invalid conflict strategy - %w", err)
	}

	// Read backup from cloud storage if storage handler exists
	if ra.Storage != nil && ra.Storage.Handler != nil {
		// Type assert to storage.Storage interface
		handler, ok := ra.Storage.Handler.(interface {
			ReadBackup(string) ([]byte, error)
		})
		if !ok {
			return fmt.Errorf("storage handler does not implement ReadBackup method")
		}

		backupData, err := handler.ReadBackup(ra.Storage.OutName)
		if err != nil {
			return fmt.Errorf("failed to read backup from cloud storage - %w", err)
		}

		tmpFile, err := os.CreateTemp("", "sentinel-pg-restore-*")
		if err != nil {
			return fmt.Errorf("failed to create temp backup file - %w", err)
		}
		defer os.Remove(tmpFile.Name())
		if _, err := tmpFile.Write(backupData); err != nil {
			tmpFile.Close()
			return fmt.Errorf("failed to write temp backup file - %w", err)
		}
		if err := tmpFile.Close(); err != nil {
			return fmt.Errorf("failed to finalize temp backup file - %w", err)
		}
		ra.BackupPath = tmpFile.Name()
	} else if ra.BackupPath != "" {
		// Local restore paths are handed to pg_restore directly.
	} else {
		return fmt.Errorf("backup path or storage handler is required")
	}

	// Check database connectivity before restore
	if err := checkConnectivity(ctx, ra); err != nil {
		return fmt.Errorf("connectivity check failed - %w", err)
	}

	// Build pg_restore command
	args := []string{
		fmt.Sprintf("--host=%s", ra.Host),
		fmt.Sprintf("--port=%d", ra.Port),
		fmt.Sprintf("--username=%s", ra.Username),
		fmt.Sprintf("--dbname=%s", ra.Database),
	}

	// Add format if specified
	if ra.PgRestoreFormat != "" {
		args = append(args, fmt.Sprintf("--format=%s", ra.PgRestoreFormat))
	}

	// Add decompression flag if needed
	if ra.Decompress {
		args = append(args, "--decompression")
	}

	// Map conflict strategy to native pg_restore flags.
	// replace → --clean (drop objects before recreating)
	// ignore  → --if-exists (skip missing objects, suppress errors)
	// error   → default behavior (no flag needed)
	switch ra.OnConflict {
	case "replace":
		args = append(args, "--clean")
		if ra.AllowCascade {
			args = append(args, "--if-exists") // prevents errors on missing deps during clean
		}
	case "ignore":
		args = append(args, "--if-exists")
	}

	// Parse and add additional arguments
	if ra.AdditionalArgs != "" {
		additionalArgs, err := backup.ParseAdditionalArgs(ra.AdditionalArgs)
		if err != nil {
			return fmt.Errorf("failed to parse additional_args: %w", err)
		}
		args = append(args, additionalArgs...)
	}

	if ra.BackupPath != "" {
		args = append(args, ra.BackupPath)
	}

	// Execute pg_restore against the staged backup path.
	cmd := exec.CommandContext(ctx, "pg_restore", args...)

	// Set up environment with password
	cmd.Env = os.Environ()
	if ra.Password != "" {
		cmd.Env = append(cmd.Env, fmt.Sprintf("PGPASSWORD=%s", ra.Password))
	}

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to start pg_restore command - %w", err)
	}

	return nil
}

// RestoreFromFile is a convenience wrapper that restores from a local file
func RestoreFromFile(ctx context.Context, ra *RestoreArgs) error {
	if ra.BackupPath == "" {
		return fmt.Errorf("backup path is required")
	}
	return Restore(ctx, ra)
}

// checkConnectivity verifies database connectivity using pg_dump
func checkConnectivity(ctx context.Context, ra *RestoreArgs) error {
	cmd := exec.CommandContext(ctx, "pg_dump",
		fmt.Sprintf("--host=%s", ra.Host),
		fmt.Sprintf("--port=%d", ra.Port),
		fmt.Sprintf("--username=%s", ra.Username),
		"--list",
		ra.Database,
	)

	cmd.Env = os.Environ()
	if ra.Password != "" {
		cmd.Env = append(cmd.Env, fmt.Sprintf("PGPASSWORD=%s", ra.Password))
	}

	// Discard output, we only care about exit code
	cmd.Stdout = nil
	cmd.Stderr = nil

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("cannot connect to PostgreSQL database %s@%s:%d - %w", ra.Username, ra.Host, ra.Port, err)
	}

	return nil
}

// IsRestoreOptions marks *RestoreArgs as a ports.RestoreOptions (spec 036).
func (*RestoreArgs) IsRestoreOptions() {}
