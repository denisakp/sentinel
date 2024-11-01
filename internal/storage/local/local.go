package local

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
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
	if isDirectory(path) {
		tmpDir := path

		files, err := os.ReadDir(tmpDir)
		if err != nil {
			return fmt.Errorf("failed to read backup directory: %w", err)
		}

		for _, file := range files {
			srcPath := filepath.Join(tmpDir, file.Name())
			destPath := filepath.Join(path, file.Name())

			if err := copyFile(srcPath, destPath); err != nil {
				return fmt.Errorf("failed to copy file %s: %w", file.Name(), err)
			}
		}

		fmt.Printf("Backup successfully written to %s\n", path)

		return nil
	}

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

func isDirectory(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}

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
