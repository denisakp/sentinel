package mongo

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	backup "github.com/denisakp/sentinel/internal/domain/backup"
	"github.com/denisakp/sentinel/internal/sanitize"
)

// RestoreArgs holds arguments for MongoDB restore operations
type RestoreArgs struct {
	// Connection parameters
	URI      string // MongoDB connection URI (connection string)
	Database string // Database name

	// Backup source
	BackupPath string // local file path (optional if Storage is provided)

	// Restore options
	OnConflict     string // ignore, replace, error (default: error)
	Gzip           bool   // decompress gzip
	Archive        bool   // backup is a tar archive
	AdditionalArgs string // extra arguments for mongorestore

	// Cloud storage
	Storage *struct {
		Type    string      // "s3", "gdrive", "local"
		OutName string      // backup name in storage
		Handler interface{} // storage.Storage interface (injected)
	}
}

// Restore restores a MongoDB backup from cloud or local storage
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
		tmpFile, err := os.CreateTemp("", "sentinel-mongo-restore-*")
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
		// Local restore paths are handed to mongorestore directly.
	} else {
		return fmt.Errorf("backup path or storage handler is required")
	}

	// Check database connectivity before restore
	if err := checkConnectivity(ctx, ra); err != nil {
		return fmt.Errorf("connectivity check failed - %w", err)
	}

	// Build mongorestore command
	args := []string{
		fmt.Sprintf("--uri=%s", ra.URI),
	}

	// Add database if specified
	if ra.Database != "" {
		args = append(args, fmt.Sprintf("--db=%s", ra.Database))
	}

	// Add gzip decompression flag if needed
	if ra.Gzip {
		args = append(args, "--gzip")
	}

	// Add archive flag if backup is tar archive
	if ra.Archive {
		args = append(args, fmt.Sprintf("--archive=%s", ra.BackupPath))
	}

	// Add conflict handling
	if ra.OnConflict == "ignore" {
		args = append(args, "--stopOnError=false")
	} else if ra.OnConflict == "replace" {
		args = append(args, "--drop")
	}

	// Parse and add additional arguments
	if ra.AdditionalArgs != "" {
		additionalArgs, err := backup.ParseAdditionalArgs(ra.AdditionalArgs)
		if err != nil {
			return fmt.Errorf("failed to parse additional_args: %w", err)
		}
		args = append(args, additionalArgs...)
	}

	if !ra.Archive && ra.Gzip && filepath.Ext(ra.BackupPath) == ".gz" {
		args = append(args, "--gzip")
	}

	if !ra.Archive {
		args = append(args, ra.BackupPath)
	}

	// Execute mongorestore against the staged backup path.
	cmd := exec.CommandContext(ctx, "mongorestore", args...)

	// Set up environment (no special password handling for MongoDB URI)
	cmd.Env = os.Environ()

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to start mongorestore command - %w", err)
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

// checkConnectivity verifies database connectivity using mongosh
func checkConnectivity(ctx context.Context, ra *RestoreArgs) error {
	cmd := exec.CommandContext(ctx, "mongosh",
		ra.URI,
		"--eval", "db.version()",
	)

	cmd.Env = os.Environ()

	// Discard output, we only care about exit code
	cmd.Stdout = nil
	cmd.Stderr = nil

	if err := cmd.Run(); err != nil {
		// ra.URI carries userinfo, so printing it verbatim leaked the password
		// into the error and into logs (#156). The host and scheme stay visible,
		// which is what makes the message useful; only the secret goes.
		return fmt.Errorf("cannot connect to MongoDB at URI %s - %w", sanitize.RedactLog(ra.URI), err)
	}

	return nil
}

// IsRestoreOptions marks *RestoreArgs as a ports.RestoreOptions.
func (*RestoreArgs) IsRestoreOptions() {}
