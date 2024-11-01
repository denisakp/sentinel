package local

import (
	"fmt"
	"os"
	"path/filepath"
)

// determineLocalBackupPath returns an absolute path for the local backup directory.
func determineLocalBackupPath(path string) (string, error) {
	if path == "" {
		if envDir := os.Getenv("BACKUP_DIRECTORY"); envDir != "" {
			path = envDir
		} else {
			homeDir, err := os.UserHomeDir()
			if err != nil {
				return "", fmt.Errorf("failed to get user home directory: %w", err)
			}
			path = filepath.Join(homeDir, "sentinel")
		}
	}

	// convert the path to an absolute path
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path: %w", err)
	}

	return absPath, err
}

// createDirIfNotExists checks if the directory exists and creates it if it doesn't.
func createDirIfNotExists(path string) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := os.MkdirAll(path, os.ModePerm); err != nil {
			return fmt.Errorf("unable to create directory: %w", err)
		}
	}
	return nil
}
