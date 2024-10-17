package backup

import (
	"fmt"
	"os"
	"path/filepath"
)

// CreateBackupDirectory creates a backup directory
func CreateBackupDirectory() (string, error) {
	backupDirectory := os.Getenv("BACKUP_DIRECTORY") // get backup directory from environment variable

	if backupDirectory == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to get user home directory: %w", err) // return an error if the user's home directory cannot be retrieved
		}

		backupDirectory = filepath.Join(homeDir, "sentinel") // create a <sentinel> directory in the user's home directory
	}

	absolutePath, err := filepath.Abs(backupDirectory) // get the absolute path of the backup directory
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path for backup directory: %w", err) // return an error if the absolute path cannot be retrieved
	}

	if err := os.MkdirAll(absolutePath, os.ModePerm); err != nil {
		return "", fmt.Errorf("failed to create backup directory: %v", err) // return an error if the backup directory cannot be created
	}

	return absolutePath, nil
}
