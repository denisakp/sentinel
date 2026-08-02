package s3

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"

	"github.com/denisakp/sentinel/internal/ports"
)

// ErrObjectNotFound is returned by Download when the requested object does not
// exist, so callers can treat a missing (optional) object as absence rather
// than a hard failure — parity with the local/GCS backends (spec 047).
var ErrObjectNotFound = errors.New("s3: object not found")

// S3Backend implements StorageBackend for Amazon S3 and S3-compatible storage.
type S3Backend struct {
	client *MyS3Client
}

// NewS3Backend wraps an existing MyS3Client as a StorageBackend.
func NewS3Backend(client *MyS3Client) *S3Backend {
	return &S3Backend{client: client}
}

// Upload uploads the local file at src to the S3 object key dest.
func (b *S3Backend) Upload(ctx context.Context, src, dest string) error {
	f, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("s3: failed to open source file %q: %w", src, err)
	}
	defer f.Close()

	_, err = b.client.Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(b.client.Bucket),
		Key:    aws.String(dest),
		Body:   f,
	})
	if err != nil {
		return fmt.Errorf("s3: failed to upload %q to %q: %w", src, dest, err)
	}
	return nil
}

// Download downloads the S3 object at src to local file dest.
func (b *S3Backend) Download(ctx context.Context, src, dest string) error {
	resp, err := b.client.Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(b.client.Bucket),
		Key:    aws.String(src),
	})
	if err != nil {
		var apiErr smithy.APIError
		if errors.As(err, &apiErr) {
			switch apiErr.ErrorCode() {
			case "NoSuchKey", "NotFound":
				return fmt.Errorf("%w: %s", ErrObjectNotFound, src)
			}
		}
		return fmt.Errorf("s3: failed to get object %q: %w", src, err)
	}
	defer resp.Body.Close()

	f, err := os.Create(dest)
	if err != nil {
		return fmt.Errorf("s3: failed to create destination file %q: %w", dest, err)
	}
	defer f.Close()

	if _, err := io.Copy(f, resp.Body); err != nil {
		return fmt.Errorf("s3: failed to write downloaded content: %w", err)
	}
	return nil
}

// Delete removes the S3 object at path.
func (b *S3Backend) Delete(ctx context.Context, path string) error {
	_, err := b.client.Client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(b.client.Bucket),
		Key:    aws.String(path),
	})
	if err != nil {
		return fmt.Errorf("s3: failed to delete %q: %w", path, err)
	}
	return nil
}

// List returns all S3 objects with the given key prefix.
func (b *S3Backend) List(ctx context.Context, prefix string) ([]ports.StorageObject, error) {
	var objects []ports.StorageObject
	paginator := s3.NewListObjectsV2Paginator(b.client.Client, &s3.ListObjectsV2Input{
		Bucket: aws.String(b.client.Bucket),
		Prefix: aws.String(prefix),
	})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("s3: failed to list objects: %w", err)
		}
		for _, obj := range page.Contents {
			o := ports.StorageObject{
				Path:      aws.ToString(obj.Key),
				SizeBytes: aws.ToInt64(obj.Size),
			}
			if obj.LastModified != nil {
				o.LastModified = *obj.LastModified
			}
			if obj.ETag != nil {
				o.ETag = aws.ToString(obj.ETag)
			}
			objects = append(objects, o)
		}
	}
	return objects, nil
}

// Exists returns true if the S3 object at path exists.
func (b *S3Backend) Exists(ctx context.Context, path string) (bool, error) {
	_, err := b.client.Client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(b.client.Bucket),
		Key:    aws.String(path),
	})
	if err != nil {
		return false, nil
	}
	return true, nil
}

// Status returns the repository status for this S3 backend.
func (b *S3Backend) Status(ctx context.Context) (ports.RepoStatus, error) {
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
