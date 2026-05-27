package local

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/denisakp/sentinel/internal/ports"
)

// LocalBackend implements StorageBackend for local filesystem storage.
type LocalBackend struct {
	basePath string
}

// NewLocalBackend creates a LocalBackend rooted at basePath.
func NewLocalBackend(basePath string) *LocalBackend {
	return &LocalBackend{basePath: basePath}
}

func (b *LocalBackend) fullPath(p string) string {
	return filepath.Join(b.basePath, p)
}

// Upload copies the local file at src to dest within the base path.
func (b *LocalBackend) Upload(_ context.Context, src, dest string) error {
	destPath := b.fullPath(dest)
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return fmt.Errorf("local: failed to create directory: %w", err)
	}
	return copyFile(src, destPath)
}

// Download copies the file at src (within base path) to local dest.
func (b *LocalBackend) Download(_ context.Context, src, dest string) error {
	return copyFile(b.fullPath(src), dest)
}

// Delete removes the file at path within the base path.
func (b *LocalBackend) Delete(_ context.Context, path string) error {
	if err := os.Remove(b.fullPath(path)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("local: failed to delete %q: %w", path, err)
	}
	return nil
}

// List returns all files under the base path matching the given prefix.
func (b *LocalBackend) List(_ context.Context, prefix string) ([]ports.StorageObject, error) {
	var objects []ports.StorageObject
	err := filepath.Walk(b.basePath, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(b.basePath, path)
		if prefix != "" && !strings.HasPrefix(rel, prefix) {
			return nil
		}
		objects = append(objects, ports.StorageObject{
			Path:         rel,
			SizeBytes:    info.Size(),
			LastModified: info.ModTime().UTC(),
		})
		return nil
	})
	return objects, err
}

// Exists returns true if the file at path exists within the base path.
func (b *LocalBackend) Exists(_ context.Context, path string) (bool, error) {
	_, err := os.Stat(b.fullPath(path))
	if os.IsNotExist(err) {
		return false, nil
	}
	return err == nil, err
}

// Status returns the repository status for this local backend.
func (b *LocalBackend) Status(ctx context.Context) (ports.RepoStatus, error) {
	objects, err := b.List(ctx, "")
	if err != nil {
		return ports.RepoStatus{Reachable: false, Error: err.Error()}, nil
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
	return ports.RepoStatus{
		Reachable:      true,
		BackupCount:    len(objects),
		TotalSizeBytes: total,
		LastBackup:     lastMod,
	}, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("local: failed to open source %q: %w", src, err)
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("local: failed to create destination %q: %w", dst, err)
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("local: failed to copy data: %w", err)
	}
	return out.Sync()
}
