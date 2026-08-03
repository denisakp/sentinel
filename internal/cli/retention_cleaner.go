package cli

// Storage-side retention DELETE, rewired from the deleted
// internal/retention/cleaner.go. All artifact deletion
// now goes through ports.StorageBackend.Delete; concrete backends are
// obtained exclusively via the storage registry.

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	storage "github.com/denisakp/sentinel/internal/adapters/storage"
	"github.com/denisakp/sentinel/internal/config"
	domainret "github.com/denisakp/sentinel/internal/domain/retention"
	"github.com/denisakp/sentinel/internal/ports"
)

// newRetentionDeleteBackend is a test seam over the storage registry.
var newRetentionDeleteBackend = func(p *storage.BackendParams) (ports.StorageBackend, error) {
	return storage.NewBackend(p)
}

// deleteRetentionCandidates removes backup artifacts from storage for
// supported backends. Candidates carrying the protected-active-baseline
// marker are always skipped.
func deleteRetentionCandidates(ctx context.Context, candidates []domainret.BackupCandidate, storageCfg config.StorageConfig) ([]domainret.DeletedBackup, []error) {
	if len(candidates) == 0 {
		return nil, nil
	}

	deleted := make([]domainret.DeletedBackup, 0, len(candidates))
	var errs []error

	switch storageCfg.Type {
	case "local":
		// Empty LocalPath: candidate FilePaths are absolute, Join keeps them.
		backend, err := newRetentionDeleteBackend(&storage.BackendParams{StorageType: "local"})
		if err != nil {
			return nil, []error{fmt.Errorf("failed to initialize local backend: %w", err)}
		}
		for _, cand := range candidates {
			if isProtectedCandidate(cand) {
				continue
			}
			if err := backend.Delete(ctx, cand.FilePath); err != nil {
				errs = append(errs, fmt.Errorf("failed to delete %s: %w", cand.FilePath, err))
				continue
			}
			deleted = append(deleted, deletedBackupFromCandidate(cand))
		}
		return deleted, errs

	case "s3":
		backend, err := newRetentionDeleteBackend(&storage.BackendParams{
			StorageType:        "s3",
			AWSBucket:          storageCfg.S3Bucket,
			AWSRegion:          storageCfg.S3Region,
			AWSBucketEndpoint:  storageCfg.S3BucketEndpoint,
			AWSAccessKeyID:     storageCfg.S3AccessKeyID,
			AWSSecretAccessKey: storageCfg.S3SecretAccessKey,
		})
		if err != nil {
			return nil, []error{fmt.Errorf("failed to initialize s3 backend: %w", err)}
		}
		for _, cand := range candidates {
			if isProtectedCandidate(cand) {
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
		// Bucket comes from each candidate's gs:// URI, so the backend is
		// constructed per candidate (buckets may differ across history).
		for _, cand := range candidates {
			if isProtectedCandidate(cand) {
				continue
			}
			bucket, object, err := parseBucketObjectRef(cand.FilePath, "gs", "")
			if err != nil {
				errs = append(errs, fmt.Errorf("failed to parse gcs uri %s: %w", cand.FilePath, err))
				continue
			}
			backend, err := newRetentionDeleteBackend(&storage.BackendParams{
				StorageType:        "gcs",
				GCSBucket:          bucket,
				GCSProjectID:       storageCfg.GCSProjectID,
				GCSCredentialsFile: storageCfg.GCSCredentialsFile,
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
		backend, err := newRetentionDeleteBackend(&storage.BackendParams{
			StorageType:         "azure",
			AzureStorageAccount: storageCfg.AzureStorageAccount,
			AzureStorageKey:     storageCfg.AzureStorageKey,
			AzureContainer:      storageCfg.AzureContainer,
		})
		if err != nil {
			return nil, []error{fmt.Errorf("failed to initialize azure backend: %w", err)}
		}
		for _, cand := range candidates {
			if isProtectedCandidate(cand) {
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
		return nil, []error{fmt.Errorf("retention delete not supported for storage type '%s'", storageCfg.Type)}
	}
}

func isProtectedCandidate(cand domainret.BackupCandidate) bool {
	return strings.Contains(cand.ReasonDeleted, domainret.ReasonProtectedActiveBaseline)
}

func deletedBackupFromCandidate(cand domainret.BackupCandidate) domainret.DeletedBackup {
	return domainret.DeletedBackup{
		FilePath:      cand.FilePath,
		FileSize:      cand.FileSize,
		DeletionTime:  time.Now().UTC(),
		ReasonDeleted: cand.ReasonDeleted,
	}
}

// parseBucketObjectRef splits a storage reference into bucket/container and
// object path. Plain (non-URI) paths fall back to defaultBucket.
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

	parsed, err := url.Parse(trimmed)
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
