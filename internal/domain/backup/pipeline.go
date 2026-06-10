package backup

// Post-dump artifact pipeline: incremental metadata, side-artifact archival,
// optional in-place encryption, hash verification, manifest persistence.
// Relocated from internal/cli/backup.go (applyBackupSecurity +
// localBackupInfo + resolveBackupPath) by spec 038 Sub-PR K. Manifest I/O
// goes through ports.ManifestStore; adapter-backed steps go through Job
// hooks.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/denisakp/sentinel/internal/ports"
)

// LocalArtifactInfo returns the on-disk path + size of the artifact when the
// job targets local storage; ("", 0) otherwise.
func LocalArtifactInfo(storageType, localPath, outName string) (string, int64) {
	if storageType != "" && storageType != "local" {
		return "", 0
	}

	path := outName
	if path == "" {
		return "", 0
	}

	if localPath != "" && !filepath.IsAbs(path) {
		path = filepath.Join(localPath, path)
	}

	info, err := os.Stat(path)
	if err != nil {
		return path, 0
	}
	if info.IsDir() {
		return path, 0
	}
	return path, info.Size()
}

// ResolveArtifactRef returns the history-row artifact reference + size for
// any storage type ("unknown" when unresolvable).
func ResolveArtifactRef(storageType, localPath, outName, gcsBucket string) (string, int64) {
	if storageType == "" || storageType == "local" {
		path, size := LocalArtifactInfo(storageType, localPath, outName)
		if path == "" {
			return "unknown", size
		}
		return path, size
	}

	if outName != "" {
		if storageType == "gcs" {
			return fmt.Sprintf("gs://%s/%s", gcsBucket, outName), 0
		}
		return outName, 0
	}

	return "unknown", 0
}

// ApplyArtifactSecurity records the dump's plaintext digest in a manifest,
// captures incremental side artifacts, and optionally encrypts the local
// artifact in place. plaintextDigest empty or a non-local artifact yields
// (nil, nil), preserving the pre-carve early return.
//
// Non-fatal manifest-write failures are reported through res.Warnings by
// Run; callers invoking this directly receive them on the outcome's
// ManifestPath being empty.
func (e *Executor) ApplyArtifactSecurity(ctx context.Context, job Job, plaintextDigest string) (*SecurityOutcome, []string, error) {
	filePath, fileSize := LocalArtifactInfo(job.StorageType, job.LocalPath, job.OutName)
	if filePath == "" {
		return nil, nil, nil
	}
	if _, err := os.Stat(filePath); err != nil {
		return nil, nil, nil
	}

	var warnings []string
	plaintextHash := plaintextDigest

	result := &SecurityOutcome{
		HashAlgorithm: "sha256",
		HashValue:     plaintextDigest,
	}

	meta := DeriveIncrementalContext(ctx, e.rec, job, fileSize)
	if meta.BackupType == "incremental" && (job.Engine == "mysql" || job.Engine == "mariadb") {
		if job.ArchiveBinlogs == nil {
			return nil, nil, fmt.Errorf("binlog archival is not configured for incremental %s backup", job.Engine)
		}
		artifacts, archiveErr := job.ArchiveBinlogs(ctx, filePath)
		if archiveErr != nil {
			return nil, nil, archiveErr
		}
		meta.BinlogStartFile = artifacts.BinlogStartFile
		meta.BinlogEndFile = artifacts.BinlogEndFile
		meta.BinlogArtifacts = append([]string{}, artifacts.BinlogArtifacts...)
	}
	if meta.BackupType == "incremental" && job.Engine == "mongodb" {
		if job.ArchiveOplog == nil {
			return nil, nil, fmt.Errorf("oplog archival is not configured for incremental mongodb backup")
		}
		artifacts, archiveErr := job.ArchiveOplog(ctx, filePath)
		if archiveErr != nil {
			return nil, nil, archiveErr
		}
		meta.OplogArtifactPath = artifacts.OplogArtifactPath
	}
	result.BackupType = meta.BackupType
	result.ChainID = meta.ChainID
	result.ChainIndex = meta.ChainIndex
	result.DeltaSize = meta.DeltaSizeBytes
	result.FullSize = meta.FullBackupSizeBytes

	// Encryption is opt-in: the hook is wired only when a key source is
	// configured by the driving adapter.
	var encInfo *ports.EncryptionInfo
	if job.EncryptArtifact != nil {
		encrypted, encMeta, encHash, encErr := job.EncryptArtifact(filePath, job.Name)
		if encErr != nil {
			return nil, nil, fmt.Errorf("failed to encrypt backup: %w", encErr)
		} else if encrypted {
			result.Encrypted = true
			result.HashValue = encHash
			result.KeyHint = job.EncryptionKeyHint
			encInfo = encMeta
		}
	}

	if meta.BackupType == "incremental" {
		verify := job.VerifyArtifactHash
		if verify == nil && e.manifests != nil {
			verify = e.manifests.VerifyHash
		}
		if verify != nil {
			if verifyErr := verify(filePath, result.HashAlgorithm, result.HashValue); verifyErr != nil {
				return nil, nil, fmt.Errorf("failed to verify incremental artifact hash: %w", verifyErr)
			}
		}
	}

	// Write manifest alongside the backup file.
	manifestPath := filePath + ".manifest.json"
	m := &ports.BackupManifest{
		BackupID:     job.Name,
		Database:     job.Database,
		DatabaseType: job.Engine,
		CreatedAt:    time.Now().UTC(),
		SizeBytes:    fileSize,
		Hash: ports.HashInfo{
			Algorithm:      "sha256",
			Value:          result.HashValue,
			PlaintextValue: plaintextHash,
		},
		Encryption: encInfo,
		AdvancedRestore: &ports.AdvancedRestoreMetadata{
			Capabilities: []string{"full", "incremental"},
			IncrementalLineage: &ports.IncrementalLineageMetadata{
				Enabled:             meta.Enabled,
				ChainID:             meta.ChainID,
				ChainIndex:          meta.ChainIndex,
				MaxChainDepth:       meta.MaxChainDepth,
				BaselineBackupID:    meta.BaselineBackupID,
				RequiredBackupIDs:   meta.RequiredBackupIDs,
				DeltaSizeBytes:      meta.DeltaSizeBytes,
				FullBackupSizeBytes: meta.FullBackupSizeBytes,
				CompressionRatio:    meta.CompressionRatio,
				Engine:              job.Engine,
				BinlogStartFile:     meta.BinlogStartFile,
				BinlogEndFile:       meta.BinlogEndFile,
				BinlogArtifacts:     meta.BinlogArtifacts,
				OplogArtifactPath:   meta.OplogArtifactPath,
				ExecutionSupported:  true,
			},
		},
	}
	if e.manifests == nil {
		warnings = append(warnings, fmt.Sprintf("Warning: failed to write manifest for '%s': manifest store not configured", job.Name))
	} else if writeErr := e.manifests.Write(manifestPath, m); writeErr != nil {
		warnings = append(warnings, fmt.Sprintf("Warning: failed to write manifest for '%s': %v", job.Name, writeErr))
	} else {
		result.ManifestPath = manifestPath
	}

	return result, warnings, nil
}
