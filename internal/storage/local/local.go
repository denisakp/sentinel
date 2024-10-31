package local

import (
	"fmt"
	"os"
)

// LocalStorage is a struct that implements the Storage interface
type LocalStorage struct{}

// GetBackupPath returns the path to the local backup directory
func (ls *LocalStorage) GetBackupPath(path string) (string, error) {
	// Determine the local backup directory path, using default if necessary
	localBackupPath, err := determineLocalBackupPath(path)
	if err != nil {
		return "", err
	}

	// Ensure the directory exists or create it
	if err := createDirIfNotExists(localBackupPath); err != nil {
		return "", err
	}

	return localBackupPath, err
}

func (ls *LocalStorage) WriteBackup(data []byte, path string) error {
	file, err := os.Create(path) // create the file
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	// write the data to the file
	if _, err := file.Write(data); err != nil {
		return fmt.Errorf("failed to write data: %w", err)
	}

	fmt.Printf("Backup successfully written to %s\n", path)

	return nil
}
