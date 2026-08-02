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
// any storage type ("unknown" when unresolvable). For remote storage the
// reference is the remote object key (or gs:// URL) and the size is read from
// the staged artifact at stagedPath when present (spec 047; previously always
// 0 for remote).
func ResolveArtifactRef(storageType, localPath, outName, gcsBucket, stagedPath string) (string, int64) {
	if storageType == "" || storageType == "local" {
		path, size := LocalArtifactInfo(storageType, localPath, outName)
		if path == "" {
			return "unknown", size
		}
		return path, size
	}

	size := stagedArtifactSize(stagedPath)

	if outName != "" {
		if storageType == "gcs" {
			return fmt.Sprintf("gs://%s/%s", gcsBucket, outName), size
		}
		return outName, size
	}

	return "unknown", size
}

// stagedArtifactSize returns the on-disk size of the staged remote artifact,
// or 0 when the path is empty, missing, or a directory.
func stagedArtifactSize(stagedPath string) int64 {
	if stagedPath == "" {
		return 0
	}
	info, err := os.Stat(stagedPath)
	if err != nil || info.IsDir() {
		return 0
	}
	return info.Size()
}

// ApplyArtifactSecurity records the dump's plaintext digest in a manifest,
// captures incremental side artifacts, and optionally encrypts the artifact
// in place.
//
// For LOCAL storage the artifact path is resolved from the job's on-disk
// output (unchanged pre-carve behaviour: a missing/non-local artifact yields
// (nil, nil)). For REMOTE storage the artifact is the staged file at
// stagedPath (spec 047): security runs there so the Executor can upload the
// encrypted artifact + manifest sidecar, closing the plaintext-remote-upload
// bypass. stagedPath may be empty in unit tests / non-stageable dump-all
// paths; the encryption hook is still invoked (hooks own the file I/O).
//
// Non-fatal manifest-write failures are reported through res.Warnings by
// Run; callers invoking this directly receive them on the outcome's
// ManifestPath being empty.
func (e *Executor) ApplyArtifactSecurity(ctx context.Context, job Job, plaintextDigest, stagedPath string) (*SecurityOutcome, []string, error) {
	remote := job.StorageType != "" && job.StorageType != "local"

	var filePath string
	var fileSize int64
	if remote {
		// Nothing to secure: no staged artifact AND no encryption configured
		// (the standalone post-dump wrapper, or a non-stageable dump-all with
		// no key). Preserve the historical no-op. A configured encryption hook
		// forces the security path even without a staged file so a key that
		// could not be applied fails loud upstream rather than silently
		// uploading plaintext.
		if stagedPath == "" && job.EncryptArtifact == nil {
			return nil, nil, nil
		}
		filePath = stagedPath
		fileSize = stagedArtifactSize(filePath)
	} else {
		filePath, fileSize = LocalArtifactInfo(job.StorageType, job.LocalPath, job.OutName)
		if filePath == "" {
			return nil, nil, nil
		}
		if _, err := os.Stat(filePath); err != nil {
			return nil, nil, nil
		}
	}

	var warnings []string
	plaintextHash := plaintextDigest

	result := &SecurityOutcome{
		HashAlgorithm: "sha256",
		HashValue:     plaintextDigest,
	}

	// Compression is opt-in and runs first — before hash/encrypt (pipeline
	// order: dump → compress → hash → encrypt). The artifact is compressed in
	// place so every downstream step (incremental size math, encryption,
	// manifest SizeBytes) operates on the compressed bytes. The compressed
	// digest becomes the stored-artifact hash (Q3); encryption, when
	// configured, overrides it with the ciphertext digest below. Spec 049 /
	// PRD 33.
	var compInfo *ports.CompressionInfo
	if job.CompressArtifact != nil && filePath != "" {
		compressed, cmeta, compHash, compErr := job.CompressArtifact(filePath)
		if compErr != nil {
			return nil, nil, fmt.Errorf("failed to compress backup: %w", compErr)
		}
		if compressed {
			compInfo = cmeta
			result.HashValue = compHash
			if info, statErr := os.Stat(filePath); statErr == nil && !info.IsDir() {
				fileSize = info.Size()
			}
		}
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
		Compression: compInfo,
		Encryption:  encInfo,
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
	if filePath == "" {
		// Degenerate case (unit test mock builder / non-stageable dump-all with
		// no staged file): skip manifest I/O rather than writing to a bogus path.
	} else if e.manifests == nil {
		warnings = append(warnings, fmt.Sprintf("Warning: failed to write manifest for '%s': manifest store not configured", job.Name))
	} else if writeErr := e.manifests.Write(manifestPath, m); writeErr != nil {
		warnings = append(warnings, fmt.Sprintf("Warning: failed to write manifest for '%s': %v", job.Name, writeErr))
	} else {
		result.ManifestPath = manifestPath
	}

	return result, warnings, nil
}
