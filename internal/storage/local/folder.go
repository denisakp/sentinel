package local

import (
	"fmt"
	"os"
	"path/filepath"
)

// determineLocalBackupPath determines the local backup directory path.
// If the provided path is empty, it checks the environment variable
// BACKUP_DIRECTORY. If that is also not set, it defaults to a directory
// named "sentinel" in the user's home directory. The function returns
// the absolute path of the determined backup directory.
// Returns an error if the home directory cannot be retrieved
// or if converting to an absolute path fails.
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

	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path: %w", err)
	}

	return absPath, err
}

// createDirIfNotExists checks if the specified directory exists and creates it
// if it does not. This function ensures that the necessary directory structure
// is available for storing backups. Returns an error if there is an issue
// checking the directory's existence or if creating the directory fails.
func createDirIfNotExists(path string) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := os.MkdirAll(path, os.ModePerm); err != nil {
			return fmt.Errorf("unable to create directory: %w", err)
		}
	}
	return nil
}
