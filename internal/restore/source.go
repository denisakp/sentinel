package restore

import (
	"context"
	crand "crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/denisakp/sentinel/internal/config"
	"github.com/denisakp/sentinel/internal/storage"
	"github.com/denisakp/sentinel/internal/storage/gcs"
	"github.com/denisakp/sentinel/internal/storage/local"
)

var (
	ErrUnsupportedRestoreSource = errors.New("unsupported restore source")
	ErrSourceObjectNotFound     = errors.New("restore source object not found")
	ErrInsufficientStagingSpace = errors.New("insufficient staging space")
)

type StagedArtifact struct {
	Path         string
	ManifestPath string
	SourcePath   string
	SizeBytes    int64
	Retained     bool
}

var newS3RestoreBackend = func(src config.RestoreBackupSource) (storage.StorageBackend, error) {
	return storage.NewRestoreBackend(storage.RestoreBackendParams{
		Type:              "s3",
		S3Bucket:          src.S3Bucket,
		S3Region:          src.S3Region,
		S3BucketEndpoint:  src.S3BucketEndpoint,
		S3AccessKeyID:     src.S3AccessKeyID,
		S3SecretAccessKey: src.S3SecretAccessKey,
	})
}

var newGCSRestoreBackend = func(src config.RestoreBackupSource) (storage.StorageBackend, error) {
	return storage.NewRestoreBackend(storage.RestoreBackendParams{
		Type:               "gcs",
		GCSBucket:          src.GCSBucket,
		GCSProjectID:       src.GCSProjectID,
		GCSCredentialsFile: src.GCSCredentialsFile,
	})
}

var downloadRestoreSourceObject = downloadSourceObject

func StageRestoreSource(ctx context.Context, job config.RestoreJob) (*StagedArtifact, error) {
	if job.StagingDir == "" {
		return nil, fmt.Errorf("staging_dir is required")
	}
	if err := os.MkdirAll(job.StagingDir, 0o700); err != nil {
		return nil, fmt.Errorf("failed to create staging dir: %w", err)
	}
	if err := os.Chmod(job.StagingDir, 0o700); err != nil {
		return nil, fmt.Errorf("failed to secure staging dir: %w", err)
	}

	backupObject, backupSize, err := resolveSourceObject(ctx, job.BackupSource)
	if err != nil {
		return nil, err
	}
	if err := ensureStagingCapacity(job.StagingDir, backupSize); err != nil {
		return nil, err
	}

	stagedPath := filepath.Join(job.StagingDir, stagedFileName(job.Name, backupObject))
	if err := downloadRestoreSourceObject(ctx, job.BackupSource, backupObject, stagedPath); err != nil {
		return nil, err
	}
	artifact := &StagedArtifact{
		Path:       stagedPath,
		SourcePath: backupObject,
		SizeBytes:  backupSize,
	}
	if err := os.Chmod(stagedPath, 0o600); err != nil {
		_ = CleanupStagedArtifact(artifact)
		return nil, fmt.Errorf("failed to secure staged file: %w", err)
	}

	manifestObject := backupObject + ".manifest.json"
	manifestPath := stagedPath + ".manifest.json"
	if err := downloadOptionalSourceObject(ctx, job.BackupSource, manifestObject, manifestPath); err == nil {
		artifact.ManifestPath = manifestPath
		if chmodErr := os.Chmod(manifestPath, 0o600); chmodErr != nil {
			_ = CleanupStagedArtifact(artifact)
			return nil, fmt.Errorf("failed to secure staged manifest: %w", chmodErr)
		}
	} else if !errors.Is(err, ErrSourceObjectNotFound) {
		artifact.ManifestPath = manifestPath
		_ = CleanupStagedArtifact(artifact)
		return nil, fmt.Errorf("failed to stage restore manifest: %w", err)
	}

	return artifact, nil
}

// StageChainArtifacts stages all resolved backup IDs into the restore staging directory.
func StageChainArtifacts(ctx context.Context, job config.RestoreJob, backupIDs []string) ([]*StagedArtifact, error) {
	if len(backupIDs) == 0 {
		return nil, nil
	}
	if job.StagingDir == "" {
		return nil, fmt.Errorf("staging_dir is required")
	}
	if err := os.MkdirAll(job.StagingDir, 0o700); err != nil {
		return nil, fmt.Errorf("failed to create staging dir: %w", err)
	}

	objects, err := listSourceObjects(ctx, job.BackupSource)
	if err != nil {
		return nil, err
	}

	selected := make([]storage.StorageObject, 0, len(backupIDs))
	sizes := make([]int64, 0, len(backupIDs))
	for _, backupID := range backupIDs {
		obj, err := resolveChainObject(backupID, objects)
		if err != nil {
			return nil, err
		}
		selected = append(selected, obj)
		sizes = append(sizes, obj.SizeBytes)
	}
	if err := EnsureStagingCapacityForArtifacts(job.StagingDir, sizes); err != nil {
		return nil, err
	}

	artifacts := make([]*StagedArtifact, 0, len(selected))
	for _, obj := range selected {
		stagedPath := filepath.Join(job.StagingDir, stagedFileName(job.Name, obj.Path))
		if err := downloadRestoreSourceObject(ctx, job.BackupSource, obj.Path, stagedPath); err != nil {
			_ = CleanupStagedArtifacts(artifacts)
			return nil, err
		}
		artifact := &StagedArtifact{
			Path:       stagedPath,
			SourcePath: obj.Path,
			SizeBytes:  obj.SizeBytes,
		}
		if err := os.Chmod(stagedPath, 0o600); err != nil {
			_ = CleanupStagedArtifact(artifact)
			_ = CleanupStagedArtifacts(artifacts)
			return nil, fmt.Errorf("failed to secure staged file: %w", err)
		}

		manifestObject := obj.Path + ".manifest.json"
		manifestPath := stagedPath + ".manifest.json"
		if err := downloadOptionalSourceObject(ctx, job.BackupSource, manifestObject, manifestPath); err == nil {
			artifact.ManifestPath = manifestPath
			if chmodErr := os.Chmod(manifestPath, 0o600); chmodErr != nil {
				_ = CleanupStagedArtifact(artifact)
				_ = CleanupStagedArtifacts(artifacts)
				return nil, fmt.Errorf("failed to secure staged manifest: %w", chmodErr)
			}
		} else if !errors.Is(err, ErrSourceObjectNotFound) {
			artifact.ManifestPath = manifestPath
			_ = CleanupStagedArtifact(artifact)
			_ = CleanupStagedArtifacts(artifacts)
			return nil, fmt.Errorf("failed to stage chain manifest: %w", err)
		}

		artifacts = append(artifacts, artifact)
	}

	return artifacts, nil
}

func CleanupStagedArtifact(artifact *StagedArtifact) error {
	if artifact == nil {
		return nil
	}
	var cleanupErr error
	if artifact.ManifestPath != "" {
		if err := os.Remove(artifact.ManifestPath); err != nil && !os.IsNotExist(err) {
			cleanupErr = err
		}
	}
	if err := os.Remove(artifact.Path); err != nil && !os.IsNotExist(err) && cleanupErr == nil {
		cleanupErr = err
	}
	return cleanupErr
}

func CleanupStagedArtifacts(artifacts []*StagedArtifact) error {
	var cleanupErr error
	for _, artifact := range artifacts {
		if err := CleanupStagedArtifact(artifact); err != nil {
			cleanupErr = errors.Join(cleanupErr, err)
		}
	}
	return cleanupErr
}

func resolveSourceObject(ctx context.Context, source config.RestoreBackupSource) (string, int64, error) {
	if !source.UseLatestMatch {
		size, err := sourceObjectSize(ctx, source, source.BackupPath)
		return source.BackupPath, size, err
	}

	objects, err := listSourceObjects(ctx, source)
	if err != nil {
		return "", 0, err
	}
	var matches []storage.StorageObject
	for _, object := range objects {
		matched, matchErr := filepath.Match(source.BackupPath, object.Path)
		if matchErr == nil && matched {
			matches = append(matches, object)
		}
	}
	if len(matches) == 0 {
		return "", 0, fmt.Errorf("%w: %s", ErrSourceObjectNotFound, source.BackupPath)
	}
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].LastModified.After(matches[j].LastModified)
	})
	return matches[0].Path, matches[0].SizeBytes, nil
}

func listSourceObjects(ctx context.Context, source config.RestoreBackupSource) ([]storage.StorageObject, error) {
	switch source.Type {
	case "local":
		backend := local.NewLocalBackend(source.LocalPath)
		return backend.List(ctx, "")
	case "s3":
		backend, err := newS3RestoreBackend(source)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize s3 restore backend: %w", err)
		}
		return backend.List(ctx, "")
	case "gcs":
		backend, err := newGCSRestoreBackend(source)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize gcs restore backend: %w", err)
		}
		return backend.List(ctx, "")
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedRestoreSource, source.Type)
	}
}

func sourceObjectSize(ctx context.Context, source config.RestoreBackupSource, object string) (int64, error) {
	objects, err := listSourceObjects(ctx, source)
	if err != nil {
		return 0, err
	}
	for _, candidate := range objects {
		if candidate.Path == object {
			return candidate.SizeBytes, nil
		}
	}
	return 0, fmt.Errorf("%w: %s", ErrSourceObjectNotFound, object)
}

func downloadSourceObject(ctx context.Context, source config.RestoreBackupSource, object, dest string) error {
	switch source.Type {
	case "local":
		if err := local.NewLocalBackend(source.LocalPath).Download(ctx, object, dest); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("%w: %s", ErrSourceObjectNotFound, object)
			}
			return err
		}
		return nil
	case "s3":
		backend, err := newS3RestoreBackend(source)
		if err != nil {
			return fmt.Errorf("failed to initialize s3 restore backend: %w", err)
		}
		return backend.Download(ctx, object, dest)
	case "gcs":
		backend, err := newGCSRestoreBackend(source)
		if err != nil {
			return fmt.Errorf("failed to initialize gcs restore backend: %w", err)
		}
		if err := backend.Download(ctx, object, dest); err != nil {
			if strings.Contains(err.Error(), gcs.ErrObjectNotFound.Error()) {
				return fmt.Errorf("%w: %s", ErrSourceObjectNotFound, object)
			}
			return err
		}
		return nil
	default:
		return fmt.Errorf("%w: %s", ErrUnsupportedRestoreSource, source.Type)
	}
}

func downloadOptionalSourceObject(ctx context.Context, source config.RestoreBackupSource, object, dest string) error {
	if err := downloadRestoreSourceObject(ctx, source, object, dest); err != nil {
		if errors.Is(err, ErrSourceObjectNotFound) {
			return err
		}
		return err
	}
	return nil
}

func ensureStagingCapacity(dir string, required int64) error {
	if required <= 0 {
		return nil
	}
	var stats syscall.Statfs_t
	if err := syscall.Statfs(dir, &stats); err != nil {
		return fmt.Errorf("failed to inspect staging dir capacity: %w", err)
	}
	available := int64(stats.Bavail) * int64(stats.Bsize)
	if available < required {
		return fmt.Errorf("%w: need=%d available=%d", ErrInsufficientStagingSpace, required, available)
	}
	return nil
}

// EnsureStagingCapacityForArtifacts performs one capacity check for a list of artifact sizes.
func EnsureStagingCapacityForArtifacts(dir string, artifactSizes []int64) error {
	var total int64
	for _, size := range artifactSizes {
		if size > 0 {
			total += size
		}
	}
	return ensureStagingCapacity(dir, total)
}

func resolveChainObject(backupID string, objects []storage.StorageObject) (storage.StorageObject, error) {
	for _, obj := range objects {
		if obj.Path == backupID {
			return obj, nil
		}
	}
	for _, obj := range objects {
		base := filepath.Base(obj.Path)
		if strings.Contains(base, backupID) {
			return obj, nil
		}
	}
	return storage.StorageObject{}, fmt.Errorf("%w: %s", ErrSourceObjectNotFound, backupID)
}

func stagedFileName(jobName, sourcePath string) string {
	base := filepath.Base(sourcePath)
	if base == "" || base == "." || base == string(filepath.Separator) {
		base = "restore.bin"
	}
	return fmt.Sprintf("%s_%d_%s_%s", jobName, time.Now().UTC().Unix(), randomSuffix(), base)
}

func randomSuffix() string {
	buf := make([]byte, 4)
	if _, err := crand.Read(buf); err != nil {
		return fmt.Sprintf("%d", time.Now().UTC().UnixNano())
	}
	return hex.EncodeToString(buf)
}
