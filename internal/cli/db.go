package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/denisakp/sentinel/internal/monitor"
	"github.com/spf13/cobra"
)

var dbCmd = &cobra.Command{
	Use:   "db",
	Short: "Database schema and migration management",
	Long:  "Manage database schema migrations and view migration status.",
}

var dbMigrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Database migration commands",
	Long:  "Run or inspect database schema migrations.",
}

var dbMigrateStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show database migration status",
	Long:  "Display the current database schema version and list of applied/pending migrations.",
	RunE: func(cmd *cobra.Command, args []string) error {
		// Get config path
		configPath, _ := cmd.Flags().GetString("config")

		// Load configuration to get history_db_path (skip full validation for db commands)
		cfg, err := LoadConfigMinimal(configPath)
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		// Validate only that history_db_path is set
		if cfg.HistoryDBPath == "" {
			return fmt.Errorf("history_db_path must be set in configuration")
		}

		// Initialize monitor
		mon, err := monitor.NewMonitor(cfg.HistoryDBPath)
		if err != nil {
			return fmt.Errorf("failed to initialize monitor: %w", err)
		}
		defer mon.Close()

		// Get migration status
		ctx := context.Background()
		status, err := mon.GetMigrationStatus(ctx)
		if err != nil {
			return fmt.Errorf("failed to get migration status: %w", err)
		}

		// Display status
		cmd.Println("Database Migration Status")
		cmd.Println("=========================")
		cmd.Printf("Current Version:          %d\n", status.CurrentVersion)
		cmd.Printf("Latest Available Version: %d\n", status.LatestAvailableVersion)
		cmd.Printf("Status:                   %s\n", formatMigrationStatus(status.IsUpToDate))

		if len(status.PendingVersions) > 0 {
			cmd.Printf("\nPending Migrations: %v\n", status.PendingVersions)
		}

		if len(status.AppliedMigrations) > 0 {
			cmd.Println("\nApplied Migrations:")
			cmd.Println("-------------------")
			for _, mig := range status.AppliedMigrations {
				cmd.Printf("  [%03d] %s (applied at %s)\n",
					mig.Version,
					mig.Name,
					mig.AppliedAt.Format(time.RFC3339))
			}
		}

		return nil
	},
}

func formatMigrationStatus(isUpToDate bool) string {
	if isUpToDate {
		return "Up-to-date ✓"
	}
	return "Pending migrations"
}

func init() {
	// Build command tree: db -> migrate -> status
	dbMigrateCmd.AddCommand(dbMigrateStatusCmd)
	dbCmd.AddCommand(dbMigrateCmd)

	// Add --config flag to status command
	dbMigrateStatusCmd.Flags().StringP("config", "c", "", "Path to YAML configuration file")
}
