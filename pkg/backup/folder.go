package backup

import (
	"fmt"
	"os"
	"path/filepath"
)

// CreateBackupDirectory creates a backup directory
func CreateBackupDirectory() (string, error) {
	backupDirectory := os.Getenv("BACKUP_DIRECTORY")
	if backupDirectory == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to get user home directory: %w", err)
		}
		backupDirectory = filepath.Join(homeDir, "sentinel")
	}

	absolutePath, err := filepath.Abs(backupDirectory)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path for backup directory: %w", err)
	}

	if err := os.MkdirAll(absolutePath, os.ModePerm); err != nil {
		return "", fmt.Errorf("failed to create backup directory: %v", err)
	}

	return absolutePath, nil
}
