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
	"github.com/denisakp/sentinel/internal/manifest"
	"github.com/denisakp/sentinel/internal/monitor"
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
			os.Exit(4)
		}

		if outputFmt == "" {
			outputFmt = cfg.LogFormat
		}

		mon, err := monitor.NewMonitor(cfg.HistoryDBPath)
		if err != nil {
			verifyPrintError(outputFmt, backupID, "", fmt.Sprintf("failed to open history db: %v", err))
			os.Exit(4)
		}
		defer mon.Close()

		ctx := context.Background()

		exec, err := mon.GetExecution(ctx, backupID)
		if err != nil || exec == nil {
			verifyPrintError(outputFmt, backupID, "",
				fmt.Sprintf("backup ID %q not found in history\n  Fix: run 'sentinel monitor list' to see available backup IDs", backupID))
			os.Exit(2)
		}

		if exec.FilePath == "" {
			verifyPrintSkipped(outputFmt, backupID)
			os.Exit(3)
		}

		manifestPath := exec.FilePath + ".manifest.json"
		m, err := manifest.ReadManifest(manifestPath)
		if err != nil {
			if errors.Is(err, manifest.ErrNoManifest) {
				verifyPrintSkipped(outputFmt, backupID)
				os.Exit(3)
			}
			verifyPrintError(outputFmt, backupID, exec.BackupName,
				fmt.Sprintf("failed to read manifest: %v", err))
			os.Exit(4)
		}

		computedHash, err := verifyComputeFileHash(exec.FilePath)
		if err != nil {
			verifyPrintError(outputFmt, backupID, exec.BackupName,
				fmt.Sprintf("failed to compute hash: %v", err))
			os.Exit(4)
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
			os.Exit(1)
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
}
