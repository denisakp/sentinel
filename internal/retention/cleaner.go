package retention

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/denisakp/sentinel/internal/config"
	"github.com/denisakp/sentinel/internal/adapters/storage/azure"
	"github.com/denisakp/sentinel/internal/adapters/storage/gcs"
	"github.com/denisakp/sentinel/internal/adapters/storage/s3"
	domainret "github.com/denisakp/sentinel/internal/domain/retention"
)

const reasonProtectedActiveBaseline = domainret.ReasonProtectedActiveBaseline

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
			if strings.Contains(cand.ReasonDeleted, reasonProtectedActiveBaseline) {
				continue
			}
			if err := os.RemoveAll(cand.FilePath); err != nil {
				errs = append(errs, fmt.Errorf("failed to delete %s: %w", cand.FilePath, err))
				continue
			}
			deleted = append(deleted, deletedBackupFromCandidate(cand))
		}
		return deleted, errs

	case "s3":
		backend, err := newS3DeleteBackend(storageCfg)
		if err != nil {
			return nil, []error{fmt.Errorf("failed to initialize s3 backend: %w", err)}
		}

		for _, cand := range candidates {
			if strings.Contains(cand.ReasonDeleted, reasonProtectedActiveBaseline) {
				continue
			}
			_, object, err := parseBucketObjectRef(cand.FilePath, "s3", storageCfg.S3Bucket)
			if err != nil {
				errs = append(errs, fmt.Errorf("failed to parse s3 path %s: %w", cand.FilePath, err))
				continue
			}

			if err := backend.Delete(ctx, object); err != nil {
				errs = append(errs, fmt.Errorf("failed to delete s3 object %s: %w", cand.FilePath, err))
				continue
			}

			deleted = append(deleted, deletedBackupFromCandidate(cand))
		}
		return deleted, errs

	case "gcs":
		for _, cand := range candidates {
			if strings.Contains(cand.ReasonDeleted, reasonProtectedActiveBaseline) {
				continue
			}
			bucket, object, err := parseBucketObjectRef(cand.FilePath, "gs", "")
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

			deleted = append(deleted, deletedBackupFromCandidate(cand))
		}
		return deleted, errs

	case "azure":
		backend, err := newAzureDeleteBackend(storageCfg)
		if err != nil {
			return nil, []error{fmt.Errorf("failed to initialize azure backend: %w", err)}
		}

		for _, cand := range candidates {
			if strings.Contains(cand.ReasonDeleted, reasonProtectedActiveBaseline) {
				continue
			}
			_, object, err := parseBucketObjectRef(cand.FilePath, "azure", storageCfg.AzureContainer)
			if err != nil {
				errs = append(errs, fmt.Errorf("failed to parse azure path %s: %w", cand.FilePath, err))
				continue
			}

			if err := backend.Delete(ctx, object); err != nil {
				errs = append(errs, fmt.Errorf("failed to delete azure blob %s: %w", cand.FilePath, err))
				continue
			}

			deleted = append(deleted, deletedBackupFromCandidate(cand))
		}
		return deleted, errs

	default:
		return nil, []error{fmt.Errorf("retention delete not supported for storage type '%s'", storageType)}
	}
}

func deletedBackupFromCandidate(cand BackupCandidate) DeletedBackup {
	return DeletedBackup{
		FilePath:      cand.FilePath,
		FileSize:      cand.FileSize,
		DeletionTime:  nowUTC(),
		ReasonDeleted: cand.ReasonDeleted,
	}
}

type s3DeleteBackend interface {
	Delete(ctx context.Context, path string) error
}

var newS3DeleteBackend = func(cfg config.StorageConfig) (s3DeleteBackend, error) {
	client, err := s3.NewS3Storage(&s3.AmazonS3Storage{
		Bucket:    cfg.S3Bucket,
		Region:    cfg.S3Region,
		EndPoint:  cfg.S3BucketEndpoint,
		AccessKey: cfg.S3AccessKeyID,
		SecretKey: cfg.S3SecretAccessKey,
	})
	if err != nil {
		return nil, err
	}
	return s3.NewS3Backend(client), nil
}

type gcsDeleteBackend interface {
	Delete(ctx context.Context, path string) error
}

var newGCSDeleteBackend = func(cfg gcs.Config) (gcsDeleteBackend, error) {
	return gcs.NewGCSBackend(cfg)
}

type azureDeleteBackend interface {
	Delete(ctx context.Context, path string) error
}

var newAzureDeleteBackend = func(cfg config.StorageConfig) (azureDeleteBackend, error) {
	azCfg := azure.Config{
		AccountName: cfg.AzureStorageAccount,
		Container:   cfg.AzureContainer,
	}
	if strings.TrimSpace(cfg.AzureStorageKey) != "" {
		azCfg.Auth = azure.AuthConfig{
			Type:             "connection_string",
			ConnectionString: fmt.Sprintf("DefaultEndpointsProtocol=https;AccountName=%s;AccountKey=%s;EndpointSuffix=core.windows.net", cfg.AzureStorageAccount, cfg.AzureStorageKey),
		}
	} else {
		azCfg.Auth = azure.AuthConfig{Type: "managed_identity"}
	}
	return azure.NewAzureBlobBackend(azCfg)
}

func parseGCSURI(raw string) (string, string, error) {
	return parseBucketObjectRef(raw, "gs", "")
}

func parseBucketObjectRef(raw, scheme, defaultBucket string) (string, string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", "", fmt.Errorf("empty path")
	}
	if !strings.Contains(trimmed, "://") {
		if strings.TrimSpace(defaultBucket) == "" {
			return "", "", fmt.Errorf("missing container/bucket for non-canonical path")
		}
		return defaultBucket, trimmed, nil
	}

	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", "", err
	}
	if parsed.Scheme != scheme {
		return "", "", fmt.Errorf("expected %s:// uri", scheme)
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
