package gdrive

import (
	"bytes"
	"context"
	"fmt"
	"github.com/denisakp/sentinel/internal/utils"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"
	"os"
	"path/filepath"
)

type GoogleDriveStorage struct {
	folderId string
	service  *drive.Service
}

// NewGoogleDriveStorage creates a new GoogleDriveStorage instance
func NewGoogleDriveStorage(folderId, serviceAccountFile string) (*GoogleDriveStorage, error) {
	ctx := context.Background()
	b, err := os.ReadFile(serviceAccountFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read service account file: %w", err)
	}

	srv, err := drive.NewService(ctx, option.WithCredentialsJSON(b))
	if err != nil {
		return nil, fmt.Errorf("failed to create Google Drive service: %w", err)
	}

	// Code to create a new GoogleDriveStorage instance
	return &GoogleDriveStorage{service: srv, folderId: folderId}, nil
}

// GetBackupPath returns the backup path for Google Drive
func (g *GoogleDriveStorage) GetBackupPath(outName string) (string, error) {
	return g.folderId, nil
}

// WriteBackup writes the backup data to Google Drive
func (g *GoogleDriveStorage) WriteBackup(data []byte, resource string) error {

	resource = utils.FormatResourceValue(resource)

	fmt.Printf("writing backup to %s \n", resource)

	if err := utils.WriteData(data, resource); err != nil {
		return err
	}

	if err := g.uploadData(resource); err != nil {
		return err
	}

	if err := utils.CleanTempDir(); err != nil {
		return err
	}

	return nil
}

// createGoogleDriveFolder creates a new folder in Google Drive with the specified name
// under the specified parent folder identified by parentId.
// It returns the ID of the newly created folder or an error if the folder creation fails.
//
// Parameters:
// - name: the name of the folder to create.
// - parentId: the ID of the parent folder under which the new folder will be created.
//
// Returns:
// - string: the ID of the created folder.
// - error: an error if one occurs during the folder creation process.
func (g *GoogleDriveStorage) createGoogleDriveFolder(name, parentId string) (string, error) {
	folderMetaData := &drive.File{
		Name:     name,
		MimeType: "application/vnd.google-apps.folder",
		Parents:  []string{parentId},
	}

	file, err := g.service.Files.Create(folderMetaData).Do()
	if err != nil {
		return "", fmt.Errorf("failed to create folder: %w", err)
	}

	return file.Id, nil
}

// uploadFile uploads a file to Google Drive using the specified data and name,
// placing it in the folder identified by parentId.
// The name is extracted from the provided path to ensure only the base name is used.
// It returns an error if the upload fails.
//
// Parameters:
// - data: the byte slice containing the file data to upload.
// - name: the full path or name of the file being uploaded.
// - parentId: the ID of the parent folder where the file will be uploaded.
//
// Returns:
// - error: an error if the upload fails.
func (g *GoogleDriveStorage) uploadFile(data []byte, name, parentId string) error {
	name = filepath.Base(name)

	fmt.Printf("uploading file: %s \n", name)

	fileMetadata := &drive.File{
		Name:     name,
		Parents:  []string{parentId},
		MimeType: "application/octet-stream",
	}

	file := bytes.NewReader(data)
	_, err := g.service.Files.Create(fileMetadata).Media(file).Do()
	if err != nil {
		return fmt.Errorf("failed to upload file: %w", err)
	}

	return nil
}

// uploadDirectory uploads an entire local directory to Google Drive.
// It creates a new folder in Google Drive corresponding to the local directory,
// and recursively uploads all files and subdirectories within it.
// The new folder is created under the folder identified by parentDir.
// It returns an error if any part of the upload process fails.
//
// Parameters:
// - localDir: the path to the local directory to upload.
// - parentDir: the ID of the Google Drive folder where the new folder will be created.
//
// Returns:
// - error: an error if the upload process fails.
func (g *GoogleDriveStorage) uploadDirectory(localDir, parentDir string) error {
	dirName := filepath.Base(localDir)
	folderId, err := g.createGoogleDriveFolder(dirName, parentDir)
	if err != nil {
		return err
	}

	entries, err := os.ReadDir(localDir)
	if err != nil {
		return fmt.Errorf("failed to read local directory: %w", err)
	}

	for _, entry := range entries {
		localPath := filepath.Join(localDir, entry.Name())

		if entry.IsDir() {
			if err := g.uploadDirectory(localPath, folderId); err != nil {
				return err
			}
		} else {
			fileData, err := os.ReadFile(localPath)
			if err != nil {
				return fmt.Errorf("failed to read file data: %w", err)
			}

			if err := g.uploadFile(fileData, entry.Name(), folderId); err != nil {
				return err
			}
		}
	}

	return nil
}

// uploadData uploads a resource to Google Drive, which can be either a file or a directory.
// It checks if the resource exists; if it's a directory, it calls uploadDirectory,
// otherwise, it reads the file data and calls uploadFile.
// It returns an error if the resource does not exist or if the upload fails.
//
// Parameters:
// - resource: the path to the resource (file or directory) to upload.
//
// Returns:
// - error: an error if the resource does not exist or if the upload fails.
func (g *GoogleDriveStorage) uploadData(resource string) error {
	if !utils.PathExists(resource) {
		return fmt.Errorf("resource %s does not exist", resource)
	}

	if utils.IsDirectory(resource) {
		return g.uploadDirectory(resource, g.folderId)
	}

	data, err := os.ReadFile(resource)
	if err != nil {
		return fmt.Errorf("failed to read resource data: %w", err)
	}

	return g.uploadFile(data, resource, g.folderId)
}
