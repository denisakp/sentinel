package cli

import (
	"github.com/spf13/cobra"
)

var retentionCmd = &cobra.Command{
	Use:   "retention",
	Short: "Manage backup retention policies",
	Long:  "Apply or preview retention policies to clean up old backups.\n\nExamples:\n  sentinel retention preview --config sentinel.yaml\n  sentinel retention apply --config sentinel.yaml --job prod-postgres",
}

var retentionApplyCmd = &cobra.Command{
	Use:   "apply",
	Short: "Apply retention policies to delete old backups",
	Long:  "Delete backups that exceed retention limits. Use --dry-run to preview.",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runRetention(cmd, false)
	},
}

var retentionPreviewCmd = &cobra.Command{
	Use:   "preview",
	Short: "Preview backups that would be deleted (dry-run)",
	Long:  "Show backups that match retention policies without deleting them.",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runRetention(cmd, true)
	},
}

func init() {
	retentionCmd.AddCommand(retentionApplyCmd)
	retentionCmd.AddCommand(retentionPreviewCmd)

	// Add --config flag to retention commands
	retentionApplyCmd.Flags().StringP("config", "c", "", "Path to YAML configuration file")
	retentionPreviewCmd.Flags().StringP("config", "c", "", "Path to YAML configuration file")

	retentionApplyCmd.Flags().String("job", "", "Backup job name to apply retention to")
	retentionApplyCmd.Flags().Bool("dry-run", false, "Preview deletions without deleting")
	retentionPreviewCmd.Flags().String("job", "", "Backup job name to apply retention to")
}
