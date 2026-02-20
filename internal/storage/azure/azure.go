package azure

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blob"

	"github.com/denisakp/sentinel/internal/config"
	internalstorage "github.com/denisakp/sentinel/internal/storage"
)

// AzureBlobBackend implements StorageBackend for Azure Blob Storage.
type AzureBlobBackend struct {
	client    *azblob.Client
	container string
	tier      blob.AccessTier
}

// NewAzureBlobBackend creates an AzureBlobBackend from the given config.
func NewAzureBlobBackend(cfg config.AzureConfig) (*AzureBlobBackend, error) {
	client, err := NewAzureClient(cfg)
	if err != nil {
		return nil, err
	}

	tier := blob.AccessTierHot
	switch cfg.Tier {
	case "Cool":
		tier = blob.AccessTierCool
	case "Archive":
		tier = blob.AccessTierArchive
	}

	return &AzureBlobBackend{
		client:    client,
		container: cfg.Container,
		tier:      tier,
	}, nil
}

// Upload uploads a local file at src to blob path dest.
func (b *AzureBlobBackend) Upload(ctx context.Context, src, dest string) error {
	f, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("azure: failed to open source file %q: %w", src, err)
	}
	defer f.Close()

	opts := &azblob.UploadStreamOptions{
		BlockSize:   4 * 1024 * 1024,
		Concurrency: 4,
		AccessTier:  &b.tier,
	}

	if _, err := b.client.UploadStream(ctx, b.container, dest, f, opts); err != nil {
		return fmt.Errorf("azure: failed to upload %q to %q: %w", src, dest, err)
	}
	return nil
}

// Download downloads a blob at src to local path dest.
func (b *AzureBlobBackend) Download(ctx context.Context, src, dest string) error {
	f, err := os.Create(dest)
	if err != nil {
		return fmt.Errorf("azure: failed to create destination file %q: %w", dest, err)
	}
	defer f.Close()

	resp, err := b.client.DownloadStream(ctx, b.container, src, nil)
	if err != nil {
		return fmt.Errorf("azure: failed to download %q: %w", src, err)
	}
	defer resp.Body.Close()

	if _, err := io.Copy(f, resp.Body); err != nil {
		return fmt.Errorf("azure: failed to write downloaded content to %q: %w", dest, err)
	}
	return nil
}

// Delete removes a blob at path.
func (b *AzureBlobBackend) Delete(ctx context.Context, path string) error {
	_, err := b.client.DeleteBlob(ctx, b.container, path, nil)
	if err != nil {
		return fmt.Errorf("azure: failed to delete blob %q: %w", path, err)
	}
	return nil
}

// List returns all blobs with the given prefix.
func (b *AzureBlobBackend) List(ctx context.Context, prefix string) ([]internalstorage.StorageObject, error) {
	pager := b.client.NewListBlobsFlatPager(b.container, &azblob.ListBlobsFlatOptions{
		Prefix: &prefix,
	})

	var objects []internalstorage.StorageObject
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("azure: failed to list blobs: %w", err)
		}
		for _, item := range page.Segment.BlobItems {
			obj := internalstorage.StorageObject{
				Path: *item.Name,
			}
			if item.Properties != nil {
				if item.Properties.ContentLength != nil {
					obj.SizeBytes = *item.Properties.ContentLength
				}
				if item.Properties.LastModified != nil {
					obj.LastModified = *item.Properties.LastModified
				}
				if item.Properties.ETag != nil {
					obj.ETag = string(*item.Properties.ETag)
				}
			}
			objects = append(objects, obj)
		}
	}
	return objects, nil
}

// Exists returns true if the blob at path exists.
func (b *AzureBlobBackend) Exists(ctx context.Context, path string) (bool, error) {
	_, err := b.client.ServiceClient().NewContainerClient(b.container).NewBlobClient(path).GetProperties(ctx, nil)
	if err != nil {
		if isNotFoundError(err) {
			return false, nil
		}
		return false, fmt.Errorf("azure: failed to check existence of %q: %w", path, err)
	}
	return true, nil
}

// Status returns the repository status for this Azure backend.
func (b *AzureBlobBackend) Status(ctx context.Context) (internalstorage.RepoStatus, error) {
	objects, err := b.List(ctx, "")
	if err != nil {
		return internalstorage.RepoStatus{
			Reachable: false,
			Error:     err.Error(),
		}, nil
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

	return internalstorage.RepoStatus{
		Reachable:      true,
		BackupCount:    len(objects),
		TotalSizeBytes: total,
		LastBackup:     lastMod,
	}, nil
}

func isNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	// Azure SDK wraps errors; check for 404 in the message
	errStr := err.Error()
	return contains(errStr, "404") || contains(errStr, "BlobNotFound") || contains(errStr, "ContainerNotFound")
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (func() bool {
		for i := 0; i <= len(s)-len(substr); i++ {
			if s[i:i+len(substr)] == substr {
				return true
			}
		}
		return false
	})()
}
