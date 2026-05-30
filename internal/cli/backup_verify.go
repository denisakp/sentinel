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
	"time"

	"github.com/denisakp/sentinel/internal/config"
	manifest "github.com/denisakp/sentinel/internal/adapters/manifest_store"
	"github.com/denisakp/sentinel/internal/adapters/monitor"
	"github.com/denisakp/sentinel/internal/ports"
	"github.com/spf13/cobra"
)

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

		manifestPath := exec.FilePath + ".manifest.json"
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

		computedHash, err := verifyComputeFileHash(exec.FilePath)
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
