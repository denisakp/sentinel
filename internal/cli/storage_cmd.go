package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/denisakp/sentinel/internal/storage/azure"
	"github.com/denisakp/sentinel/internal/storage/gcs"
	"github.com/denisakp/sentinel/internal/storage/gdrive"
	"github.com/denisakp/sentinel/internal/storage/local"
	"github.com/denisakp/sentinel/internal/storage/sentinel_s3"
)

// StorageCmd is the root command for storage backend management.
var StorageCmd = &cobra.Command{
	Use:   "storage",
	Short: "Storage backend management",
	Long:  "Commands for managing Sentinel storage backends.",
}

var storageStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show status of all configured storage backends",
	Long:  "Connect to each configured storage backend and report reachability, backup count, and total size.",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfgPath, _ := cmd.Flags().GetString("config")
		outputFmt, _ := cmd.Flags().GetString("output")

		cfg, err := LoadAndValidateConfig(cfgPath)
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}
		if outputFmt == "" {
			outputFmt = cfg.LogFormat
		}

		ctx := context.Background()

		type statusEntry struct {
			Name        string  `json:"name"`
			Type        string  `json:"type"`
			Reachable   bool    `json:"reachable"`
			BackupCount int     `json:"backup_count"`
			TotalSizeMB float64 `json:"total_size_mb"`
			LastBackup  string  `json:"last_backup,omitempty"`
			Error       string  `json:"error,omitempty"`
		}

		var entries []statusEntry

		for name, storageCfg := range cfg.Storages {
			entry := statusEntry{Name: name, Type: storageCfg.Type}

			switch storageCfg.Type {
			case "local":
				backend := local.NewLocalBackend(storageCfg.LocalPath)
				status, _ := backend.Status(ctx)
				entry.Reachable = status.Reachable
				entry.BackupCount = status.BackupCount
				entry.TotalSizeMB = float64(status.TotalSizeBytes) / (1024 * 1024)
				entry.Error = status.Error
				if status.LastBackup != nil {
					entry.LastBackup = status.LastBackup.Format(time.RFC3339)
				}

			case "s3":
				s3Storage := &sentinel_s3.AmazonS3Storage{
					Bucket:    storageCfg.S3Bucket,
					Region:    storageCfg.S3Region,
					EndPoint:  storageCfg.S3BucketEndpoint,
					AccessKey: storageCfg.S3AccessKeyID,
					SecretKey: storageCfg.S3SecretAccessKey,
				}
				client, err := sentinel_s3.NewS3Storage(s3Storage)
				if err != nil {
					entry.Reachable = false
					entry.Error = err.Error()
				} else {
					backend := sentinel_s3.NewS3Backend(client)
					status, _ := backend.Status(ctx)
					entry.Reachable = status.Reachable
					entry.BackupCount = status.BackupCount
					entry.TotalSizeMB = float64(status.TotalSizeBytes) / (1024 * 1024)
					entry.Error = status.Error
					if status.LastBackup != nil {
						entry.LastBackup = status.LastBackup.Format(time.RFC3339)
					}
				}

			case "google-drive":
				gds := &gdrive.GoogleDriveStorage{
					FolderId:           storageCfg.GDriveFolderID,
					ServiceAccountFile: storageCfg.GDriveSAFile,
				}
				client, err := gdrive.NewGoogleDriveStorage(gds)
				if err != nil {
					entry.Reachable = false
					entry.Error = err.Error()
				} else {
					backend := gdrive.NewGDriveBackend(client)
					status, _ := backend.Status(ctx)
					entry.Reachable = status.Reachable
					entry.BackupCount = status.BackupCount
					entry.TotalSizeMB = float64(status.TotalSizeBytes) / (1024 * 1024)
					entry.Error = status.Error
					if status.LastBackup != nil {
						entry.LastBackup = status.LastBackup.Format(time.RFC3339)
					}
				}

			case "gcs":
				backend, err := gcs.NewGCSBackend(gcs.Config{
					Bucket:          storageCfg.GCSBucket,
					ProjectID:       storageCfg.GCSProjectID,
					CredentialsFile: storageCfg.GCSCredentialsFile,
				})
				if err != nil {
					entry.Reachable = false
					entry.Error = err.Error()
				} else {
					status, _ := backend.Status(ctx)
					entry.Reachable = status.Reachable
					entry.BackupCount = status.BackupCount
					entry.TotalSizeMB = float64(status.TotalSizeBytes) / (1024 * 1024)
					entry.Error = status.Error
					if status.LastBackup != nil {
						entry.LastBackup = status.LastBackup.Format(time.RFC3339)
					}
				}

			case "azure":
				azCfg := azure.Config{
					AccountName: storageCfg.AzureStorageAccount,
					Container:   storageCfg.AzureContainer,
				}
				backend, err := azure.NewAzureBlobBackend(azCfg)
				if err != nil {
					entry.Reachable = false
					entry.Error = err.Error()
				} else {
					status, _ := backend.Status(ctx)
					entry.Reachable = status.Reachable
					entry.BackupCount = status.BackupCount
					entry.TotalSizeMB = float64(status.TotalSizeBytes) / (1024 * 1024)
					entry.Error = status.Error
					if status.LastBackup != nil {
						entry.LastBackup = status.LastBackup.Format(time.RFC3339)
					}
				}

			default:
				entry.Reachable = false
				entry.Error = fmt.Sprintf("unsupported storage type: %s", storageCfg.Type)
			}

			entries = append(entries, entry)
		}

		if len(entries) == 0 {
			if outputFmt == "json" {
				fmt.Println("[]")
			} else {
				fmt.Println("No named storage backends configured.")
				fmt.Println("  Add storage backends under the 'storages:' key in your config.")
			}
			return nil
		}

		if outputFmt == "json" {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(entries)
		}

		fmt.Printf("Storage Backend Status (%d configured)\n", len(entries))
		fmt.Println("─────────────────────────────────────────")
		for _, e := range entries {
			statusStr := "OK"
			if !e.Reachable {
				statusStr = "UNREACHABLE"
			}
			fmt.Printf("  %-20s %-12s %s\n", e.Name, e.Type, statusStr)
			if e.Reachable {
				fmt.Printf("    Backups: %d  Size: %.1f MB\n", e.BackupCount, e.TotalSizeMB)
				if e.LastBackup != "" {
					fmt.Printf("    Last backup: %s\n", e.LastBackup)
				}
			} else if e.Error != "" {
				fmt.Printf("    Error: %s\n", e.Error)
			}
		}
		return nil
	},
}

func init() {
	StorageCmd.AddCommand(storageStatusCmd)
	storageStatusCmd.Flags().String("config", "", "Path to sentinel YAML config")
	storageStatusCmd.Flags().String("output", "", "Output format: json or text")
}
