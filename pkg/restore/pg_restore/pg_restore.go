package pg_restore

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
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

	var backupData []byte
	var err error

	// Read backup from cloud storage if storage handler exists
	if ra.Storage != nil && ra.Storage.Handler != nil {
		// Type assert to storage.Storage interface
		handler, ok := ra.Storage.Handler.(interface {
			ReadBackup(string) ([]byte, error)
		})
		if !ok {
			return fmt.Errorf("storage handler does not implement ReadBackup method")
		}

		backupData, err = handler.ReadBackup(ra.Storage.OutName)
		if err != nil {
			return fmt.Errorf("failed to read backup from cloud storage - %w", err)
		}
	} else if ra.BackupPath != "" {
		// Fall back to local file
		backupData, err = os.ReadFile(ra.BackupPath)
		if err != nil {
			return fmt.Errorf("failed to read backup file - %w", err)
		}
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

	// Add conflict handling
	if ra.OnConflict != "" {
		args = append(args, fmt.Sprintf("--on-conflict-do=%s", ra.OnConflict))
	}

	// Parse and add additional arguments
	if ra.AdditionalArgs != "" {
		additionalArgs := parseCLIArgs(ra.AdditionalArgs)
		args = append(args, additionalArgs...)
	}

	// Execute pg_restore with stdin piping
	cmd := exec.CommandContext(ctx, "pg_restore", args...)

	// Set up environment with password
	cmd.Env = os.Environ()
	if ra.Password != "" {
		cmd.Env = append(cmd.Env, fmt.Sprintf("PGPASSWORD=%s", ra.Password))
	}

	// Pipe backup data to stdin
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdin pipe - %w", err)
	}

	// Start command
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start pg_restore command - %w", err)
	}

	// Write backup data to stdin
	if _, err := stdin.Write(backupData); err != nil {
		stdin.Close()
		return fmt.Errorf("failed to write backup data to pg_restore stdin - %w", err)
	}
	stdin.Close()

	// Wait for command completion
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("pg_restore command failed - %w", err)
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

// parseCLIArgs parses space-separated CLI arguments
func parseCLIArgs(args string) []string {
	if args == "" {
		return []string{}
	}

	// Simple split on spaces (does not handle quoted arguments)
	// For robust parsing, consider using go-shlex or similar
	return strings.Fields(args)
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
