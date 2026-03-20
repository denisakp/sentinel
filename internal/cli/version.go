package cli

import (
	"fmt"

	"github.com/denisakp/sentinel/internal/version"
	"github.com/spf13/cobra"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print Sentinel build metadata",
	Long: `Print detailed Sentinel build metadata including version, commit, build date, and Go runtime version.

Use --format json for machine-readable output suitable for CI pipelines and diagnostics tooling.
Use --tools to additionally inspect the versions of supported external database client binaries
(pg_dump, mysqldump, mariadb-dump, mongodump) available in the current environment.`,
	RunE: runVersion,
}

var (
	versionFormat string
	versionTools  bool
)

func init() {
	versionCmd.Flags().StringVar(&versionFormat, "format", "text", "Output format: text or json")
	versionCmd.Flags().BoolVar(&versionTools, "tools", false, "Inspect supported external database client tool versions")
}

func runVersion(cmd *cobra.Command, _ []string) error {
	switch versionFormat {
	case "text":
		// handled below
	case "json":
		// handled below
	default:
		return fmt.Errorf("unsupported format %q: must be one of: text, json", versionFormat)
	}

	meta := version.Get()

	var tools []version.ToolResult
	if versionTools {
		tools = version.ProbeTools()
	}

	switch versionFormat {
	case "json":
		out, err := version.RenderJSON(meta, tools, versionTools)
		if err != nil {
			return fmt.Errorf("version: failed to render JSON: %w", err)
		}
		fmt.Fprintln(cmd.OutOrStdout(), string(out))
	default:
		if versionTools {
			fmt.Fprint(cmd.OutOrStdout(), version.RenderTextWithTools(meta, tools))
		} else {
			fmt.Fprint(cmd.OutOrStdout(), version.RenderText(meta))
		}
	}

	return nil
}
