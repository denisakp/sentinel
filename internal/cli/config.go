package cli

import (
	"github.com/denisakp/sentinel/internal/config"
	"github.com/spf13/cobra"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage configuration files",
	Long:  "Validate or test YAML configuration files",
}

var configValidateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate YAML configuration file",
	RunE: func(cmd *cobra.Command, args []string) error {
		path, _ := cmd.Flags().GetString("config")
		cfg, err := config.LoadConfig(path)
		if err != nil {
			return err
		}
		if err := config.ValidateConfig(cfg); err != nil {
			return err
		}
		cmd.Println("configuration is valid")
		return nil
	},
}

func init() {
	configCmd.AddCommand(configValidateCmd)

	// Add --config flag to config commands
	configValidateCmd.Flags().StringP("config", "c", "", "Path to YAML configuration file (required)")
	configValidateCmd.MarkFlagRequired("config")
}
