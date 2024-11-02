package utils

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// IsDirectory checks if a given path points to a directory.
// It returns true if the path is a directory, false otherwise.
// This function is useful to determine whether a path should be
// treated as a directory (for copying multiple files) or as a file.
//
// Returns true if the path is a directory, false otherwise.
func IsDirectory(path string) bool {
	fileInfo, err := os.Stat(path)
	if err != nil {
		return false
	}

	return fileInfo.IsDir()
}

// CopyFile copies a file from the source to the destination.
// This function is useful to copy backup files for Postgres databases
// when the output format is a directory. Each file is copied individually
// to the destination directory.
//
// Returns an error if the file cannot be copied.
func CopyFile(source, destination string) error {
	srcFile, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("failed to open source file: %w", err)
	}
	defer func(srcFile *os.File) {
		err := srcFile.Close()
		if err != nil {
			_ = fmt.Errorf("failed to close source file: %w", err)
		}
	}(srcFile)

	destinationFile, err := os.Create(destination)
	if err != nil {
		return fmt.Errorf("failed to create destination file: %w", err)
	}
	defer func(destFile *os.File) {
		err := destFile.Close()
		if err != nil {

		}
	}(destinationFile)

	_, err = io.Copy(destinationFile, srcFile)
	if err != nil {
		return fmt.Errorf("failed to copy file: %w", err)
	}

	return nil
}

// CopyDir recursively copies a directory from the source to the destination.
// This function is useful when performing MongoDump backup of all databases,
// as Mongo exports each database to a separate directory.
//
// Returns an error if the directory cannot be copied.
func CopyDir(sourceDir, destinationDir string) error {
	err := os.MkdirAll(destinationDir, os.ModePerm)
	if err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	entries, err := os.ReadDir(sourceDir)
	if err != nil {
		return fmt.Errorf("failed to read source directory %s:  %w", sourceDir, err)
	}

	for _, entry := range entries {
		sourcePath := filepath.Join(sourceDir, entry.Name())
		destinationPath := filepath.Join(destinationDir, entry.Name())

		if entry.IsDir() {
			if err := CopyDir(sourcePath, destinationPath); err != nil {
				return err
			}
		} else {
			if err := CopyFile(sourcePath, destinationPath); err != nil {
				return err
			}
		}
	}

	return nil
}

// WriteData writes the data to the specified resource.
// If the resource is a directory, it copies the data to the directory.
// If the resource is a file, it writes the data to the file.
//
// Returns an error if the data cannot be written to the resource.
func WriteData(data []byte, resource string) error {
	if IsDirectory(resource) {
		hasContent, err := hasFilesOrNonEmptySubDir(resource)
		if err != nil {
			return err
		}

		if !hasContent {
			return fmt.Errorf("directory %s is empty", resource)
		}

		fmt.Printf("hasContent: %v\n", hasContent)

		return nil
	}

	file, err := os.Create(resource)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer func(file *os.File) {
		err := file.Close()
		if err != nil {
			_ = fmt.Errorf("failed to close file: %w", err)
		}
	}(file)

	if _, err := file.Write(data); err != nil {
		return fmt.Errorf("failed to write data to file: %w", err)
	}

	return nil
}

// FormatResourceValue formats the given resource path to be used as a file name.
// It extracts the base name of the resource and constructs a path within a temporary
// directory ("sentinel") to avoid using the folderId as a prefix in S3 or Google Drive.
// For example, if the input is "xxxxxx/resource.sql", the function returns
// "/tmp/sentinel/resource.sql", effectively stripping the folderId and ensuring
// the resource is saved in a clean format without unwanted prefixes.
//
// Returns the formatted resource path.
func FormatResourceValue(resource string) string {
	return filepath.Join(os.TempDir(), "sentinel", filepath.Base(resource))
}

// CleanTempDir removes all files sentinel temp directory.
// The function is useful to clean up the temporary directory after the backup
// operation is completed. This ensures that the temporary directory does not
// accumulate files from previous operations.
//
// Returns an error if the files cannot be removed.
func CleanTempDir() error {
	tmpDir := filepath.Join(os.TempDir(), "sentinel")

	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		return fmt.Errorf("failed to read directory: %w", err)
	}

	for _, file := range entries {
		if err := os.RemoveAll(filepath.Join(tmpDir, file.Name())); err != nil {
			return fmt.Errorf("failed to remove file %s: %w", file.Name(), err)
		}
	}

	return nil
}

// PathExists checks if a given path exists.
// The function will be useful when the user specifies an output path that already
// exists. If the path exist, we wll add a suffix to the path to avoid overwriting, or errors.
//
// Returns true if the path exists, false otherwise.
func PathExists(path string) bool {
	_, err := os.Stat(path)

	return !os.IsNotExist(err)
}

// hasFilesOrNonEmptySubDir checks if the specified directory contains any files
// or if it contains subdirectories that have files in them.
// It recursively reads the contents of the directory and its subdirectories.
// If any files are found, or if any subdirectory contains files, it returns true.
// If the directory is empty or only contains empty subdirectories, it returns false.
//
// If an error occurs while reading the directory or its contents, it returns
// false along with the error.
//
// Parameters:
// - directory: the path to the directory to check.
//
// Returns:
// - bool: true if the directory has files or non-empty subdirectories, false otherwise.
// - error: an error if one occurs during the read operation.
func hasFilesOrNonEmptySubDir(directory string) (bool, error) {
	files, err := os.ReadDir(directory)
	if err != nil {
		return false, fmt.Errorf("failed to read directory: %w", err)
	}

	for _, file := range files {
		if !file.IsDir() {
			return true, nil
		}

		subDirPath := filepath.Join(directory, file.Name())
		subDirHasFiles, err := hasFilesOrNonEmptySubDir(subDirPath)
		if err != nil {
			return false, fmt.Errorf("failed to check sub directory: %w", err)
		}

		if subDirHasFiles {
			return true, nil
		}
	}

	return false, nil
}
