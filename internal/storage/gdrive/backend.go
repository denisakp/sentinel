package gdrive

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	storagetypes "github.com/denisakp/sentinel/internal/storage/types"
	"github.com/denisakp/sentinel/internal/ports"
)

// GDriveBackend implements StorageBackend for Google Drive storage.
type GDriveBackend struct {
	client *MyGoogleDriveClient
}

// NewGDriveBackend wraps an existing MyGoogleDriveClient as a StorageBackend.
func NewGDriveBackend(client *MyGoogleDriveClient) *GDriveBackend {
	return &GDriveBackend{client: client}
}

// Upload uploads the local file at src to the Google Drive folder with name dest.
func (b *GDriveBackend) Upload(_ context.Context, src, dest string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("gdrive: failed to read source file %q: %w", src, err)
	}
	return b.client.uploadFile(data, filepath.Base(dest), b.client.folderId)
}

// Download downloads the Google Drive file matching src filename to local dest.
func (b *GDriveBackend) Download(ctx context.Context, src, dest string) error {
	fileName := filepath.Base(src)
	query := fmt.Sprintf("'%s' in parents and name='%s' and trashed=false", b.client.folderId, fileName)
	fileList, err := b.client.service.Files.List().Q(query).Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("gdrive: failed to find file %q: %w", fileName, err)
	}
	if len(fileList.Files) == 0 {
		return fmt.Errorf("gdrive: file %q not found", fileName)
	}

	resp, err := b.client.service.Files.Get(fileList.Files[0].Id).Context(ctx).Download()
	if err != nil {
		return fmt.Errorf("gdrive: failed to download file %q: %w", fileName, err)
	}
	defer resp.Body.Close()

	f, err := os.Create(dest)
	if err != nil {
		return fmt.Errorf("gdrive: failed to create destination file %q: %w", dest, err)
	}
	defer f.Close()

	if _, err := io.Copy(f, resp.Body); err != nil {
		return fmt.Errorf("gdrive: failed to write downloaded content: %w", err)
	}
	return nil
}

// Delete removes the file at path from Google Drive.
func (b *GDriveBackend) Delete(ctx context.Context, path string) error {
	return b.client.DeleteBackup(ctx, path)
}

// List returns all files in the Google Drive folder matching the given prefix.
func (b *GDriveBackend) List(ctx context.Context, prefix string) ([]ports.StorageObject, error) {
	query := fmt.Sprintf("'%s' in parents and trashed=false", b.client.folderId)
	fileList, err := b.client.service.Files.List().
		Q(query).
		Fields("files(id,name,size,modifiedTime)").
		Context(ctx).
		Do()
	if err != nil {
		return nil, fmt.Errorf("gdrive: failed to list files: %w", err)
	}

	var objects []ports.StorageObject
	for _, f := range fileList.Files {
		if prefix != "" && !strings.HasPrefix(f.Name, prefix) {
			continue
		}
		obj := ports.StorageObject{
			Path:      f.Name,
			SizeBytes: f.Size,
		}
		if f.ModifiedTime != "" {
			if t, err := time.Parse(time.RFC3339, f.ModifiedTime); err == nil {
				obj.LastModified = t
			}
		}
		objects = append(objects, obj)
	}
	return objects, nil
}

// Exists returns true if a file with the given name exists in the Drive folder.
func (b *GDriveBackend) Exists(ctx context.Context, path string) (bool, error) {
	fileName := filepath.Base(path)
	query := fmt.Sprintf("'%s' in parents and name='%s' and trashed=false", b.client.folderId, fileName)
	fileList, err := b.client.service.Files.List().Q(query).Context(ctx).Do()
	if err != nil {
		return false, fmt.Errorf("gdrive: failed to check existence of %q: %w", path, err)
	}
	return len(fileList.Files) > 0, nil
}

// Status returns the repository status for this Google Drive backend.
func (b *GDriveBackend) Status(ctx context.Context) (storagetypes.RepoStatus, error) {
	objects, err := b.List(ctx, "")
	if err != nil {
		return storagetypes.RepoStatus{Reachable: false, Error: err.Error()}, nil
	}
	var total int64
	var lastMod *time.Time
	for _, obj := range objects {
		total += obj.SizeBytes
		t := obj.LastModified
		if lastMod == nil || t.After(*lastMod) {
			lastMod = &t
		}
	}
	return storagetypes.RepoStatus{
		Reachable:      true,
		BackupCount:    len(objects),
		TotalSizeBytes: total,
		LastBackup:     lastMod,
	}, nil
}
