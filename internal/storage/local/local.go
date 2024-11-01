package local

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// LocalStorage is a struct that implements the Storage interface.
type LocalStorage struct{}

// GetBackupPath returns the path where the backup will be stored.
// This implementation is part of the LocalStorage struct, which
// manages local backup paths. The function determines the local backup
// directory path and ensures that the directory exists, creating it if necessary.
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
// Returns an error if the path is invalid, if the directory cannot be
// accessed, or if file operations fail.
func (ls *LocalStorage) WriteBackup(data []byte, path string) error {
	if isDirectory(path) {
		tmpDir := path

		files, err := os.ReadDir(tmpDir)
		if err != nil {
			return fmt.Errorf("failed to read backup directory: %w", err)
		}

		for _, file := range files {
			srcPath := filepath.Join(tmpDir, file.Name())
			destPath := filepath.Join(path, file.Name())

			if file.IsDir() {
				err = copyDir(srcPath, destPath)
			} else {
				err = copyFile(srcPath, destPath)
			}

			if err != nil {
				return fmt.Errorf("failed to copy file: %w", err)
			}

		}

		fmt.Printf("Backup successfully written to %s\n", path)

		return nil
	}

	file, err := os.Create(path)
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

// isDirectory checks if a given path points to a directory.
// It returns true if the path is a directory, false otherwise.
// This function is useful to determine whether a path should be
// treated as a directory (for copying multiple files) or as a file.
func isDirectory(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}

// copyFile copies a file from the source to the destination.
// This function is useful to copy backup files for Postgres databases
// when the output format is a directory. Each file is copied individually
// to the destination directory.
func copyFile(src, dest string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open source file: %w", err)
	}
	defer srcFile.Close()

	destFile, err := os.Create(dest)
	if err != nil {
		return fmt.Errorf("failed to create destination file: %w", err)
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, srcFile)
	if err != nil {
		return fmt.Errorf("failed to copy file: %w", err)
	}

	return nil
}

// copyDir recursively copies a directory from the source to the destination.
// This function is useful when performing MongoDump backup of all databases,
// as Mongo exports each database to a separate directory.
func copyDir(srcDir, destDir string) error {
	err := os.MkdirAll(destDir, os.ModePerm)
	if err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return fmt.Errorf("failed to read source directory %s:  %w", srcDir, err)
	}

	for _, entry := range entries {
		srcPath := filepath.Join(srcDir, entry.Name())
		destPath := filepath.Join(destDir, entry.Name())

		if entry.IsDir() {
			if err := copyDir(srcPath, destPath); err != nil {
				return err
			}
		} else {
			if err := copyFile(srcPath, destPath); err != nil {
				return err
			}
		}
	}

	return nil
}
