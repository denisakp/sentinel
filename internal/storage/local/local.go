package local

import (
	"fmt"
	"github.com/denisakp/sentinel/internal/utils"
)

// LocalStorage is a struct that implements the Storage interface.
type LocalStorage struct{}

// GetBackupPath returns the path where the backup will be stored.
// This implementation is part of the LocalStorage struct, which
// manages local backup paths. The function determines the local backup
// directory path and ensures that the directory exists, creating it if necessary.
//
// Returns the determined backup path or an error if path determination
// or directory creation fails.
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

// WriteBackup writes the backup data to a specified path.
// This implementation is part of the LocalStorage struct, which contains
// the logic for managing backups on the local file system.
// If the provided path is a directory, the function will copy the
// contents of the backup data (as multiple files) into that directory.
// If the path is a file, it creates the file and writes the backup data to it.
//
// Returns an error if the path is invalid, if the directory cannot be
// accessed, or if file operations fail.
func (ls *LocalStorage) WriteBackup(data []byte, resource string) error {

	if err := utils.WriteData(data, resource); err != nil {
		return err
	}

	fmt.Printf("Backup successfully written to %s\n", resource)

	return nil
}
