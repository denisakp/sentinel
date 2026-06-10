package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/denisakp/sentinel/internal/adapters/crypto"
	"github.com/spf13/cobra"
)

// SecurityCmd is the root command for security key management.
var SecurityCmd = &cobra.Command{
	Use:   "security",
	Short: "Security key management",
	Long:  "Commands for managing Sentinel encryption keys.",
}

var securityInitKeyCmd = &cobra.Command{
	Use:   "init-key",
	Short: "Generate a new master encryption key",
	Long:  "Generate a cryptographically strong 256-bit master encryption key for backup encryption.",
	RunE: func(cmd *cobra.Command, args []string) error {
		outputFmt, _ := cmd.Flags().GetString("output")
		force, _ := cmd.Flags().GetBool("force")

		// Check if key already configured
		existing := os.Getenv("SENTINEL_MASTER_KEY")
		if existing != "" && !force {
			fmt.Fprintln(os.Stderr, "Warning: SENTINEL_MASTER_KEY is already configured.")
			fmt.Fprintln(os.Stderr, "  Use --force to generate a new key (this will invalidate existing encrypted backups).")
			return nil
		}

		key, err := crypto.GenerateKey()
		if err != nil {
			return fmt.Errorf("failed to generate secure random key: %w", err)
		}

		if outputFmt == "json" {
			out := map[string]interface{}{
				"key":             key,
				"algorithm":       "aes-256-gcm",
				"key_length_bits": 256,
				"generated_at":    time.Now().UTC().Format(time.RFC3339),
				"instructions": map[string]string{
					"env_var":      "SENTINEL_MASTER_KEY",
					"config_field": "encryption_key_file",
				},
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(out)
		}

		// Text output
		fmt.Println("Sentinel Master Encryption Key")
		fmt.Println("===============================================================")
		fmt.Println()
		fmt.Printf("  Key: %s\n", key)
		fmt.Println()
		fmt.Println("===============================================================")
		fmt.Println()
		fmt.Println("  IMPORTANT - Store this key securely. It cannot be recovered.")
		fmt.Println()
		fmt.Println("  1. Set as environment variable (recommended):")
		fmt.Printf("     export SENTINEL_MASTER_KEY=\"%s\"\n", key)
		fmt.Println()
		fmt.Println("  2. Or write to a key file and reference in config:")
		fmt.Printf("     echo \"%s\" > /etc/sentinel/master.key\n", key)
		fmt.Println("     chmod 600 /etc/sentinel/master.key")
		fmt.Println()
		fmt.Println("     In your sentinel config:")
		fmt.Println("       encryption_key_file: /etc/sentinel/master.key")
		fmt.Println()
		fmt.Println("  3. For production: store in a secrets manager")
		fmt.Println("     (AWS Secrets Manager, HashiCorp Vault, etc.) and inject")
		fmt.Println("     via SENTINEL_MASTER_KEY at runtime.")
		fmt.Println()
		fmt.Println("  If this key is lost, encrypted backups CANNOT be recovered.")
		return nil
	},
}

func init() {
	SecurityCmd.AddCommand(securityInitKeyCmd)
	securityInitKeyCmd.Flags().String("output", "text", "Output format: json or text")
	securityInitKeyCmd.Flags().Bool("force", false, "Generate a new key even if SENTINEL_MASTER_KEY is already set")
}
