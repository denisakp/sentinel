package mongo

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/denisakp/sentinel/internal/adapters/storage"
	"github.com/denisakp/sentinel/internal/adapters/storage/local"
	"github.com/denisakp/sentinel/internal/ports"
	"github.com/denisakp/sentinel/internal/sanitize"
)

// backupBackendFactory is overridable in tests.
var backupBackendFactory = storage.NewBackend

// Backup backs up a MongoDB database using mongo_dump. For storage_type=="local"
// the legacy directory-tree dump path is preserved. For remote backends (s3, gcs,
// google-drive, azure) mongodump runs in archive mode against a staging file
// under <backup_path>/.staging/<job-id>/, then the file is streamed to the
// configured StorageBackend and the staging dir is removed (success or failure).
func Backup(ctx context.Context, prober ports.DBProber, da *DumpMongoArgs) (string, error) {
	storageHandler, err := storage.NewStorage(da.Storage)
	if err != nil {
		return "", err
	}

	backupPath, err := storageHandler.GetBackupPath(da.Storage.LocalPath)
	if err != nil {
		return "", err
	}

	remote := da.Storage.StorageType != "" && da.Storage.StorageType != "local"

	// executorOwned: the backup Executor owns hash/encrypt/manifest/upload/
	// cleanup for this remote archive. Backup only stages the
	// archive into the caller-provided dir and returns its plaintext digest.
	executorOwned := remote && strings.TrimSpace(da.RemoteStagingDir) != ""

	var staging *stagingDir
	var stagingArchive string
	switch {
	case executorOwned:
		if err := os.MkdirAll(da.RemoteStagingDir, 0o755); err != nil {
			return "", fmt.Errorf("failed to prepare staging dir: %w", err)
		}
		backupPath = da.RemoteStagingDir
		stagingArchive = filepath.Join(da.RemoteStagingDir, archiveFileName(da.Compress))
	case remote:
		// For remote backends, GetBackupPath returns a remote identifier
		// (bucket, folder id, etc.). Resolve a local filesystem root for the
		// staging dir instead via the local storage handler.
		localHandler := &local.LocalStorage{}
		localRoot, lerr := localHandler.GetBackupPath(da.Storage.LocalPath)
		if lerr != nil {
			return "", fmt.Errorf("failed to resolve local staging root: %w", lerr)
		}
		backupPath = localRoot

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

	if err := prober.Ping(context.Background(), ports.DatabaseConfig{Type: "mongodb", URI: da.Uri}); err != nil {
		return "", err
	}

	cmd := exec.CommandContext(ctx, "mongodump", args...)
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
		// args carries --uri=mongodb://user:PASSWORD@host. Joining it verbatim put
		// the password in the error, and so in every log that captured it (#156).
		// The surrounding code already redacts: the stderr branch above calls
		// RedactStderr. This branch was simply missed.
		return "", fmt.Errorf("failed to run mongo_dump: %w (command: mongodump %s)",
			err, strings.Join(sanitize.RedactArgs(args), " "))
	}

	if executorOwned {
		// Hand the staged archive to the Executor: return its plaintext digest
		// and leave the file in place (no upload, no cleanup here).
		digest, herr := hashFile(stagingArchive)
		if herr != nil {
			return "", fmt.Errorf("failed to hash staged archive: %w", herr)
		}
		fmt.Printf("Backup staged for upload\n")
		return digest, nil
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

	// mongodump wrote the archive itself, via --archive, so hash the file that
	// exists rather than the empty stdout it produced. Hashing stdout recorded
	// sha256("") for every local Mongo backup (#191).
	digest, err := hashFile(da.Storage.OutName)
	if err != nil {
		return "", fmt.Errorf("failed to hash backup archive: %w", err)
	}

	fmt.Printf("Backup complete !\n")
	return digest, nil
}

// hashFile returns the hex-encoded SHA-256 of the file at path, streaming it so
// large archives are not buffered in memory.
func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
