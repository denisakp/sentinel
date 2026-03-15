package retention

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/denisakp/sentinel/internal/config"
	"github.com/denisakp/sentinel/internal/storage/gcs"
)

// DeleteCandidates removes backup files from storage for supported backends.
func DeleteCandidates(ctx context.Context, candidates []BackupCandidate, storageType string, storageCfg config.StorageConfig) ([]DeletedBackup, []error) {
	if len(candidates) == 0 {
		return nil, nil
	}

	deleted := make([]DeletedBackup, 0, len(candidates))
	var errs []error

	switch storageType {
	case "local":
		for _, cand := range candidates {
			if err := os.RemoveAll(cand.FilePath); err != nil {
				errs = append(errs, fmt.Errorf("failed to delete %s: %w", cand.FilePath, err))
				continue
			}
			deleted = append(deleted, DeletedBackup{
				FilePath:      cand.FilePath,
				FileSize:      cand.FileSize,
				DeletionTime:  nowUTC(),
				ReasonDeleted: cand.ReasonDeleted,
			})
		}
		return deleted, errs

	case "gcs":
		for _, cand := range candidates {
			bucket, object, err := parseGCSURI(cand.FilePath)
			if err != nil {
				errs = append(errs, fmt.Errorf("failed to parse gcs uri %s: %w", cand.FilePath, err))
				continue
			}

			backend, err := newGCSDeleteBackend(gcs.Config{
				Bucket:          bucket,
				ProjectID:       storageCfg.GCSProjectID,
				CredentialsFile: storageCfg.GCSCredentialsFile,
			})
			if err != nil {
				errs = append(errs, fmt.Errorf("failed to initialize gcs backend for bucket %s: %w", bucket, err))
				continue
			}

			if err := backend.Delete(ctx, object); err != nil {
				errs = append(errs, fmt.Errorf("failed to delete gcs object %s: %w", cand.FilePath, err))
				continue
			}

			deleted = append(deleted, DeletedBackup{
				FilePath:      cand.FilePath,
				FileSize:      cand.FileSize,
				DeletionTime:  nowUTC(),
				ReasonDeleted: cand.ReasonDeleted,
			})
		}
		return deleted, errs

	default:
		return nil, []error{fmt.Errorf("retention delete not supported for storage type '%s'", storageType)}
	}
}

type gcsDeleteBackend interface {
	Delete(ctx context.Context, path string) error
}

var newGCSDeleteBackend = func(cfg gcs.Config) (gcsDeleteBackend, error) {
	return gcs.NewGCSBackend(cfg)
}

func parseGCSURI(raw string) (string, string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", "", err
	}
	if parsed.Scheme != "gs" {
		return "", "", fmt.Errorf("expected gs:// uri")
	}
	bucket := parsed.Host
	object := strings.TrimPrefix(parsed.Path, "/")
	if bucket == "" {
		return "", "", fmt.Errorf("missing bucket")
	}
	if object == "" {
		return "", "", fmt.Errorf("missing object path")
	}
	return bucket, object, nil
}

func nowUTC() time.Time {
	return time.Now().UTC()
}
