package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	manifest "github.com/denisakp/sentinel/internal/adapters/manifest_store"
	"github.com/denisakp/sentinel/internal/adapters/monitor"
	"github.com/denisakp/sentinel/internal/adapters/storage"
	"github.com/denisakp/sentinel/internal/config"
	"github.com/denisakp/sentinel/internal/ports"
	"github.com/spf13/cobra"
)

// newVerifyBackend is a test seam over the storage registry, mirroring
// newRetentionDeleteBackend in retention_cleaner.go. It lets tests inject a
// fake backend for the remote-verify fetch path.
var newVerifyBackend = func(p *storage.BackendParams) (ports.StorageBackend, error) {
	return storage.NewBackend(p)
}

var backupVerifyCmd = &cobra.Command{
	Use:   "verify <backup-id>",
	Short: "Verify the integrity of a stored backup",
	Long:  "Re-compute SHA-256 fingerprint and compare against the stored manifest value.",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		backupID := args[0]
		cfgPath, _ := cmd.Flags().GetString("config")
		if cfgPath == "" {
			cfgPath = os.ExpandEnv("$HOME/.sentinel/config.yaml")
		}
		outputFmt, _ := cmd.Flags().GetString("output")

		cfg, err := config.LoadConfig(cfgPath)
		if err != nil {
			verifyPrintError(outputFmt, backupID, "", fmt.Sprintf("failed to load config: %v", err))
			return fmt.Errorf("load config: %w", ErrVerifyInternal)
		}

		if outputFmt == "" {
			outputFmt = cfg.LogFormat
		}

		mon, err := monitor.NewMonitor(cfg.HistoryDBPath)
		if err != nil {
			verifyPrintError(outputFmt, backupID, "", fmt.Sprintf("failed to open history db: %v", err))
			return fmt.Errorf("open history db: %w", ErrVerifyInternal)
		}
		defer mon.Close()

		ctx := context.Background()

		exec, err := mon.GetExecution(ctx, backupID)
		if err != nil || exec == nil {
			verifyPrintError(outputFmt, backupID, "",
				fmt.Sprintf("backup ID %q not found in history\n  Fix: run 'sentinel monitor list' to see available backup IDs", backupID))
			return fmt.Errorf("backup %q: %w", backupID, ErrVerifyNotFound)
		}

		if exec.FilePath == "" {
			verifyPrintSkipped(outputFmt, backupID)
			return fmt.Errorf("backup %q: %w", backupID, ErrVerifySkipped)
		}

		// Local backups verify against exec.FilePath directly. Remote backups
		// (s3/gcs/azure/gdrive) hold a URI or object key there, unreachable via
		// os.Open, so we download the artifact + its <key>.manifest.json sidecar
		// to a temp dir and verify against those local copies. The remote object
		// reference is still what the report prints (exec.FilePath). A missing
		// sidecar naturally falls through to the existing "skipped" outcome:
		// manifestPath then points at a non-existent local file and ReadManifest
		// returns ports.ErrNoManifest.
		manifestPath := exec.FilePath + ".manifest.json"
		hashTarget := exec.FilePath
		if isRemoteStorageBackend(exec.StorageBackend) {
			tmpDir, tmpErr := os.MkdirTemp("", "sentinel-verify-*")
			if tmpErr != nil {
				verifyPrintError(outputFmt, backupID, exec.BackupName,
					fmt.Sprintf("failed to create temp dir: %v", tmpErr))
				return fmt.Errorf("create temp dir: %w", ErrVerifyInternal)
			}
			defer os.RemoveAll(tmpDir)

			var storageCfg config.StorageConfig
			if job, ok := cfg.Databases[exec.BackupName]; ok {
				storageCfg = job.Storage
			}

			localArtifact, fetchErr := fetchRemoteBackupForVerify(ctx, exec, storageCfg, tmpDir)
			if fetchErr != nil {
				verifyPrintError(outputFmt, backupID, exec.BackupName,
					fmt.Sprintf("failed to fetch remote backup: %v", fetchErr))
				return fmt.Errorf("fetch remote backup: %w", ErrVerifyInternal)
			}
			hashTarget = localArtifact
			manifestPath = localArtifact + ".manifest.json"
		}

		m, err := manifest.ReadManifest(manifestPath)
		if err != nil {
			if errors.Is(err, ports.ErrNoManifest) {
				verifyPrintSkipped(outputFmt, backupID)
				return fmt.Errorf("backup %q: %w", backupID, ErrVerifySkipped)
			}
			verifyPrintError(outputFmt, backupID, exec.BackupName,
				fmt.Sprintf("failed to read manifest: %v", err))
			return fmt.Errorf("read manifest: %w", ErrVerifyInternal)
		}

		computedHash, err := verifyComputeFileHash(hashTarget)
		if err != nil {
			verifyPrintError(outputFmt, backupID, exec.BackupName,
				fmt.Sprintf("failed to compute hash: %v", err))
			return fmt.Errorf("compute hash: %w", ErrVerifyInternal)
		}

		verifiedAt := time.Now().UTC()

		if computedHash != m.Hash.Value {
			if outputFmt == "json" {
				verifyPrintJSON(map[string]interface{}{
					"backup_id":     backupID,
					"database":      exec.BackupName,
					"stored_hash":   m.Hash.Value,
					"computed_hash": computedHash,
					"result":        "fail",
					"error":         "hash mismatch: backup file has been modified or corrupted",
					"verified_at":   verifiedAt.Format(time.RFC3339),
				})
			} else {
				fmt.Printf("FAIL: Backup %s integrity check failed\n", backupID)
				fmt.Printf("  Database:      %s\n", exec.BackupName)
				fmt.Printf("  File:          %s\n", exec.FilePath)
				fmt.Printf("  Stored hash:   %s\n", m.Hash.Value)
				fmt.Printf("  Computed hash: %s\n", computedHash)
				fmt.Println("  Status:        FAIL - hash mismatch")
			}
			return errors.New("hash mismatch")
		}

		if outputFmt == "json" {
			verifyPrintJSON(map[string]interface{}{
				"backup_id":     backupID,
				"database":      exec.BackupName,
				"file_path":     exec.FilePath,
				"stored_hash":   m.Hash.Value,
				"computed_hash": computedHash,
				"result":        "pass",
				"verified_at":   verifiedAt.Format(time.RFC3339),
			})
		} else {
			fmt.Printf("PASS: Backup %s integrity verified\n", backupID)
			fmt.Printf("  Database: %s\n", exec.BackupName)
			fmt.Printf("  File:     %s\n", exec.FilePath)
			fmt.Printf("  Hash:     %s (%s)\n", m.Hash.Value, m.Hash.Algorithm)
			fmt.Println("  Status:   PASS")
		}
		return nil
	},
}

func verifyComputeFileHash(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("failed to open file %q: %w", path, err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("failed to hash file %q: %w", path, err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// isRemoteStorageBackend reports whether a recorded execution's storage
// backend is a non-local (remote) target whose artifact must be downloaded
// before it can be verified.
func isRemoteStorageBackend(storageType string) bool {
	return storageType != "" && storageType != "local"
}

// fetchRemoteBackupForVerify downloads the remote backup artifact referenced by
// exec into tmpDir and, when present, its <key>.manifest.json sidecar alongside
// it (so a subsequent manifest.ReadManifest + hash compare runs on local
// files). It returns the local artifact path. A missing manifest sidecar is not
// an error: the sidecar is simply not downloaded, and the caller's ReadManifest
// then surfaces the existing "no manifest / skipped" outcome. Storage /
// credential failures are returned as errors (never a silent skip).
func fetchRemoteBackupForVerify(ctx context.Context, exec *ports.Execution, storageCfg config.StorageConfig, tmpDir string) (string, error) {
	params, object, err := verifyBackendParamsAndObject(exec.StorageBackend, exec.FilePath, storageCfg)
	if err != nil {
		return "", err
	}

	backend, err := newVerifyBackend(params)
	if err != nil {
		return "", fmt.Errorf("failed to initialize %s backend: %w", exec.StorageBackend, err)
	}

	localArtifact := filepath.Join(tmpDir, filepath.Base(object))
	if err := backend.Download(ctx, object, localArtifact); err != nil {
		return "", fmt.Errorf("failed to download backup artifact %q: %w", object, err)
	}

	// Optional manifest sidecar: tolerate absence (pre-v1.1 backup or a remote
	// upload whose sidecar step failed) by leaving it undownloaded.
	manifestObject := object + ".manifest.json"
	exists, err := backend.Exists(ctx, manifestObject)
	if err != nil {
		return "", fmt.Errorf("failed to check manifest sidecar %q: %w", manifestObject, err)
	}
	if exists {
		if err := backend.Download(ctx, manifestObject, localArtifact+".manifest.json"); err != nil {
			return "", fmt.Errorf("failed to download manifest sidecar %q: %w", manifestObject, err)
		}
	}

	return localArtifact, nil
}

// verifyBackendParamsAndObject maps a recorded remote artifact reference
// (exec.FilePath) plus the job's resolved storage config into the storage
// registry params and the object key to download. It mirrors the per-type
// param construction in retention_cleaner.go and reuses parseBucketObjectRef to
// split bucket/object from either a scheme URI (gs://…) or a plain object key.
func verifyBackendParamsAndObject(storageType, filePath string, cfg config.StorageConfig) (*storage.BackendParams, string, error) {
	switch storageType {
	case "s3":
		_, object, err := parseBucketObjectRef(filePath, "s3", cfg.S3Bucket)
		if err != nil {
			return nil, "", fmt.Errorf("failed to parse s3 path %q: %w", filePath, err)
		}
		return &storage.BackendParams{
			StorageType:        "s3",
			AWSBucket:          cfg.S3Bucket,
			AWSRegion:          cfg.S3Region,
			AWSBucketEndpoint:  cfg.S3BucketEndpoint,
			AWSAccessKeyID:     cfg.S3AccessKeyID,
			AWSSecretAccessKey: cfg.S3SecretAccessKey,
		}, object, nil

	case "gcs":
		bucket, object, err := parseBucketObjectRef(filePath, "gs", cfg.GCSBucket)
		if err != nil {
			return nil, "", fmt.Errorf("failed to parse gcs uri %q: %w", filePath, err)
		}
		return &storage.BackendParams{
			StorageType:        "gcs",
			GCSBucket:          bucket,
			GCSProjectID:       cfg.GCSProjectID,
			GCSCredentialsFile: cfg.GCSCredentialsFile,
		}, object, nil

	case "azure":
		_, object, err := parseBucketObjectRef(filePath, "azure", cfg.AzureContainer)
		if err != nil {
			return nil, "", fmt.Errorf("failed to parse azure path %q: %w", filePath, err)
		}
		return &storage.BackendParams{
			StorageType:         "azure",
			AzureStorageAccount: cfg.AzureStorageAccount,
			AzureStorageKey:     cfg.AzureStorageKey,
			AzureContainer:      cfg.AzureContainer,
		}, object, nil

	case "google-drive":
		// Google Drive addresses files by name/path, not bucket/object, so the
		// recorded reference is the object key as-is.
		return &storage.BackendParams{
			StorageType:          "google-drive",
			GoogleDriveFolderId:  cfg.GDriveFolderID,
			GoogleServiceAccount: cfg.GDriveSAFile,
		}, filePath, nil

	default:
		return nil, "", fmt.Errorf("remote verify not supported for storage type %q", storageType)
	}
}

func verifyPrintError(format, backupID, database, msg string) {
	if format == "json" {
		verifyPrintJSON(map[string]interface{}{
			"backup_id": backupID,
			"database":  database,
			"result":    "error",
			"error":     msg,
		})
	} else {
		fmt.Fprintf(os.Stderr, "Error: %s\n", msg)
	}
}

func verifyPrintSkipped(format, backupID string) {
	if format == "json" {
		verifyPrintJSON(map[string]string{
			"backup_id": backupID,
			"result":    "skipped",
			"warning":   "no manifest found for this backup; integrity cannot be verified (pre-v1.1 backup)",
		})
	} else {
		fmt.Printf("Warning: no manifest found for backup %s (pre-v1.1 backup)\n", backupID)
		fmt.Println("  Integrity cannot be verified.")
	}
}

func verifyPrintJSON(v interface{}) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.Encode(v)
}

func init() {
	BackupCmd.AddCommand(backupVerifyCmd)
	backupVerifyCmd.Flags().String("config", "", "Path to sentinel YAML config")
	backupVerifyCmd.Flags().String("output", "", "Output format: json or text")
	backupVerifyCmd.Flags().Bool("allow-legacy-envelope", legacyEnvelopeEnvDefault(),
		"Decrypt artifacts produced before the v2 envelope fix. UNSAFE: pre-v2 streams used a flawed nonce scheme. Use only to recover plaintext for re-encryption.")
}
