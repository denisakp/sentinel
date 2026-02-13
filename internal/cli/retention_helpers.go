package cli

import (
	"context"
	"fmt"

	"github.com/denisakp/sentinel/internal/config"
	"github.com/denisakp/sentinel/internal/retention"
	"github.com/spf13/cobra"
)

func runRetention(cmd *cobra.Command, preview bool) error {
	path, _ := cmd.Flags().GetString("config")
	if path == "" {
		return fmt.Errorf("--config is required")
	}
	jobName, _ := cmd.Flags().GetString("job")
	dryRun := preview
	if cmd.Flags().Changed("dry-run") {
		value, _ := cmd.Flags().GetBool("dry-run")
		dryRun = value
	}

	cfg, err := config.LoadConfig(path)
	if err != nil {
		return err
	}
	if err := config.ValidateConfig(cfg); err != nil {
		return err
	}

	manager, err := retention.NewManager(cfg)
	if err != nil {
		return err
	}
	defer manager.Close()

	ctx := context.Background()
	if jobName != "" {
		deleted, err := manager.Apply(ctx, jobName, dryRun)
		if err != nil {
			return err
		}
		printRetentionSummary(cmd, jobName, deleted, dryRun)
		return nil
	}

	summary, err := manager.ApplyAll(ctx, dryRun)
	if err != nil && len(summary.Errors) > 0 {
		cmd.PrintErrln("retention completed with errors")
	}
	for job, count := range summary.ByBackupJob {
		cmd.Printf("%s: deleted %d backups\n", job, count)
	}
	cmd.Printf("total deleted: %d backups\n", summary.TotalDeleted)
	return nil
}

func printRetentionSummary(cmd *cobra.Command, jobName string, deleted []retention.DeletedBackup, dryRun bool) {
	mode := "apply"
	if dryRun {
		mode = "preview"
	}
	cmd.Printf("retention %s for %s\n", mode, jobName)
	for _, item := range deleted {
		cmd.Printf("- %s (%d bytes) - %s\n", item.FilePath, item.FileSize, item.ReasonDeleted)
	}
}
