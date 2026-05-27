package mongo_dump

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os/exec"
	"strings"

	"github.com/denisakp/sentinel/internal/backup/mongo"
	"github.com/denisakp/sentinel/internal/sanitize"
	"github.com/denisakp/sentinel/internal/adapters/storage"
	"github.com/denisakp/sentinel/internal/adapters/storage/local"
)

// backupBackendFactory and checkConnectivity are overridable in tests.
var (
	backupBackendFactory = storage.NewBackend
	checkConnectivity    = mongo.CheckConnectivity
)

// Backup backs up a MongoDB database using mongo_dump. For storage_type=="local"
// the legacy directory-tree dump path is preserved. For remote backends (s3, gcs,
// google-drive, azure) mongodump runs in archive mode against a staging file
// under <backup_path>/.staging/<job-id>/, then the file is streamed to the
// configured StorageBackend and the staging dir is removed (success or failure).
func Backup(da *DumpMongoArgs) (string, error) {
	storageHandler, err := storage.NewStorage(da.Storage)
	if err != nil {
		return "", err
	}

	backupPath, err := storageHandler.GetBackupPath(da.Storage.LocalPath)
	if err != nil {
		return "", err
	}

	remote := da.Storage.StorageType != "" && da.Storage.StorageType != "local"

	// For remote backends, GetBackupPath returns a remote identifier (bucket,
	// folder id, etc.). Resolve a local filesystem root for the staging dir
	// instead via the local storage handler.
	if remote {
		localHandler := &local.LocalStorage{}
		localRoot, lerr := localHandler.GetBackupPath(da.Storage.LocalPath)
		if lerr != nil {
			return "", fmt.Errorf("failed to resolve local staging root: %w", lerr)
		}
		backupPath = localRoot
	}

	var staging *stagingDir
	var stagingArchive string
	if remote {
		staging, err = newStagingDir(backupPath, "")
		if err != nil {
			return "", fmt.Errorf("failed to create staging dir: %w", err)
		}
		defer staging.Cleanup()
		stagingArchive = staging.ArchivePath(da.Compress)
	}

	args, material, err := argsBuilder(da, backupPath, stagingArchive)
	if err != nil {
		return "", fmt.Errorf("failed to build mongo_dump arguments: %w", err)
	}
	defer func() {
		if cerr := material.Close(); cerr != nil {
			_ = cerr
		}
	}()

	if err := checkConnectivity(da.Uri); err != nil {
		return "", err
	}

	cmd := exec.Command("mongodump", args...)
	var stdErr bytes.Buffer
	cmd.Stderr = &stdErr
	var stdOut bytes.Buffer
	cmd.Stdout = &stdOut

	if err := cmd.Run(); err != nil {
		redacted, _ := sanitize.RedactStderr(stdErr.Bytes())
		stderr := strings.TrimSpace(redacted)
		if stderr != "" {
			return "", fmt.Errorf("failed to run mongo_dump: %w: %s", err, stderr)
		}
		stdout := strings.TrimSpace(stdOut.String())
		if stdout != "" {
			return "", fmt.Errorf("failed to run mongo_dump: %w: %s", err, stdout)
		}
		return "", fmt.Errorf("failed to run mongo_dump: %w (command: mongodump %s)", err, strings.Join(args, " "))
	}

	if remote {
		backend, err := backupBackendFactory(da.Storage)
		if err != nil {
			return "", fmt.Errorf("failed to initialize backup backend: %w", err)
		}
		if err := backend.Upload(context.Background(), stagingArchive, da.Storage.OutName); err != nil {
			return "", fmt.Errorf("failed to upload backup to %s: %w", da.Storage.StorageType, err)
		}
		fmt.Printf("Backup complete !\n")
		return "", nil
	}

	sum := sha256.Sum256(stdOut.Bytes())
	digest := hex.EncodeToString(sum[:])

	if err := storageHandler.WriteBackup(stdOut.Bytes(), da.Storage.OutName); err != nil {
		return "", fmt.Errorf("failed to write backup to storage: %w", err)
	}

	fmt.Printf("Backup complete !\n")
	return digest, nil
}
