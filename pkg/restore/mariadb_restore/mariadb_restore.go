package mariadb_restore

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"github.com/denisakp/sentinel/internal/backup"
)

// RestoreArgs holds arguments for MariaDB restore operations
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
	OnConflict     string // ignore, replace, error (default: error)
	AdditionalArgs string // extra arguments for mariadb

	// Cloud storage
	Storage *struct {
		Type    string      // "s3", "gdrive", "local"
		OutName string      // backup name in storage
		Handler interface{} // storage.Storage interface (injected)
	}
}

// Restore restores a MariaDB backup from cloud or local storage
func Restore(ctx context.Context, ra *RestoreArgs) error {
	// Validate required arguments
	if err := ValidateRequiredArgs(ra); err != nil {
		return fmt.Errorf("invalid restore arguments - %w", err)
	}

	if err := ValidateOnConflict(ra.OnConflict); err != nil {
		return fmt.Errorf("invalid conflict strategy - %w", err)
	}

	// Read backup from cloud storage if storage handler exists
	if ra.Storage != nil && ra.Storage.Handler != nil {
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
		tmpFile, err := os.CreateTemp("", "sentinel-mariadb-restore-*")
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
		// Local restore paths are streamed directly to mariadb.
	} else {
		return fmt.Errorf("backup path or storage handler is required")
	}

	// Check database connectivity before restore
	if err := checkConnectivity(ctx, ra); err != nil {
		return fmt.Errorf("connectivity check failed - %w", err)
	}

	// Build mariadb command
	args := []string{
		fmt.Sprintf("--host=%s", ra.Host),
		fmt.Sprintf("--port=%d", ra.Port),
		fmt.Sprintf("--user=%s", ra.Username),
	}

	// Map conflict strategy to native mariadb CLI flags.
	// ignore  → --force (continue on duplicate key errors instead of aborting)
	// replace → --force (mariadb CLI has no native REPLACE INTO flag; --force
	//           continues past duplicate errors, which is the closest safe option)
	// error   → default behavior (abort on first error)
	if ra.OnConflict == "ignore" || ra.OnConflict == "replace" {
		args = append(args, "--force")
	}

	// Parse and add additional arguments
	if ra.AdditionalArgs != "" {
		additionalArgs, err := backup.ParseAdditionalArgs(ra.AdditionalArgs)
		if err != nil {
			return fmt.Errorf("failed to parse additional_args: %w", err)
		}
		args = append(args, additionalArgs...)
	}

	// Always select the target database
	args = append(args, ra.Database)

	// Execute mariadb with stdin piping
	cmd := exec.CommandContext(ctx, "mariadb", args...)

	// Set up environment with password
	cmd.Env = os.Environ()
	if ra.Password != "" {
		cmd.Env = append(cmd.Env, fmt.Sprintf("MYSQL_PWD=%s", ra.Password))
	}

	in, err := os.Open(ra.BackupPath)
	if err != nil {
		return fmt.Errorf("failed to open staged backup file - %w", err)
	}
	defer in.Close()
	cmd.Stdin = in

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to start mariadb command - %w", err)
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

// checkConnectivity verifies database connectivity using mariadb
func checkConnectivity(ctx context.Context, ra *RestoreArgs) error {
	cmd := exec.CommandContext(ctx, "mariadb",
		fmt.Sprintf("--host=%s", ra.Host),
		fmt.Sprintf("--port=%d", ra.Port),
		fmt.Sprintf("--user=%s", ra.Username),
		"-e", "SELECT 1",
	)

	cmd.Env = os.Environ()
	if ra.Password != "" {
		cmd.Env = append(cmd.Env, fmt.Sprintf("MYSQL_PWD=%s", ra.Password))
	}

	// Discard output, we only care about exit code
	cmd.Stdout = nil
	cmd.Stderr = nil

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("cannot connect to MariaDB database %s@%s:%d - %w", ra.Username, ra.Host, ra.Port, err)
	}

	return nil
}
