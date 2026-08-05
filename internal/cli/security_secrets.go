package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/denisakp/sentinel/internal/adapters/crypto"
	"github.com/spf13/cobra"
)

var securityEncryptSecretsFileCmd = &cobra.Command{
	Use:   "encrypt-secrets-file <plaintext-path>",
	Short: "Encrypt a DB-credentials secrets file at rest",
	Long: `Encrypt a plaintext DB-credentials secrets file (a MySQL/MariaDB defaults
file or a MongoDB secrets file) into Sentinel's self-contained encrypted form,
so it can be stored encrypted at rest and decrypted in memory at config-load
time.

The encrypted output is written to a NEW path (--out); the plaintext input is
never modified or deleted — remove it yourself once you have verified the
encrypted file loads. Point the job's defaults_file / mongo_secrets_file at the
encrypted output and configure the key via secrets_key_env / secrets_key_file
(falling back to encryption_key_env / encryption_key_file).

The key is the same 256-bit AES key used elsewhere; generate one with
'sentinel security init-key'.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		inPath := args[0]
		outPath, _ := cmd.Flags().GetString("out")
		keyEnv, _ := cmd.Flags().GetString("key-env")
		keyFile, _ := cmd.Flags().GetString("key-file")
		force, _ := cmd.Flags().GetBool("force")

		if outPath == "" {
			return fmt.Errorf("--out is required (the encrypted output path; the plaintext input is left untouched)")
		}
		if keyEnv == "" && keyFile == "" {
			return fmt.Errorf("a key is required: pass --key-env or --key-file")
		}

		absIn, err := filepath.Abs(inPath)
		if err != nil {
			return fmt.Errorf("resolve input path: %w", err)
		}
		absOut, err := filepath.Abs(outPath)
		if err != nil {
			return fmt.Errorf("resolve output path: %w", err)
		}
		if absIn == absOut {
			return fmt.Errorf("--out must differ from the input path (refusing to overwrite the plaintext secrets file '%s')", inPath)
		}
		if _, err := os.Stat(absOut); err == nil && !force {
			return fmt.Errorf("output file '%s' already exists (pass --force to overwrite)", outPath)
		}

		plaintext, err := os.ReadFile(absIn)
		if err != nil {
			return fmt.Errorf("cannot read plaintext secrets file '%s': %w", inPath, err)
		}

		kp := &crypto.FileKeyProvider{EnvVar: keyEnv, FilePath: keyFile}
		key, err := kp.GetKey()
		if err != nil {
			return fmt.Errorf("cannot resolve encryption key: %w", err)
		}

		enc, err := crypto.EncryptSecretsFile(plaintext, key)
		if err != nil {
			return fmt.Errorf("failed to encrypt secrets file: %w", err)
		}

		// Write atomically via a temp file in the same directory, then rename, so a
		// failure never leaves a partial, valid-looking output — and never touches
		// the plaintext input.
		tmp, err := os.CreateTemp(filepath.Dir(absOut), ".sentinel-secrets-*.tmp")
		if err != nil {
			return fmt.Errorf("cannot create temporary output: %w", err)
		}
		tmpName := tmp.Name()
		cleanup := true
		defer func() {
			if cleanup {
				_ = os.Remove(tmpName)
			}
		}()
		if _, err := tmp.Write(enc); err != nil {
			_ = tmp.Close()
			return fmt.Errorf("cannot write encrypted output: %w", err)
		}
		if err := tmp.Chmod(0o600); err != nil {
			_ = tmp.Close()
			return fmt.Errorf("cannot set output permissions: %w", err)
		}
		if err := tmp.Close(); err != nil {
			return fmt.Errorf("cannot finalize encrypted output: %w", err)
		}
		if err := os.Rename(tmpName, absOut); err != nil {
			return fmt.Errorf("cannot move encrypted output into place: %w", err)
		}
		cleanup = false

		fmt.Printf("Encrypted secrets file written to %s\n", outPath)
		fmt.Printf("The plaintext input '%s' was left untouched.\n", inPath)
		fmt.Println("Once you have verified the encrypted file loads, remove the plaintext original (e.g. shred -u).")
		return nil
	},
}

func init() {
	SecurityCmd.AddCommand(securityEncryptSecretsFileCmd)
	securityEncryptSecretsFileCmd.Flags().String("out", "", "Destination path for the encrypted secrets file (required)")
	securityEncryptSecretsFileCmd.Flags().String("key-env", "", "Env var holding the base64-encoded 256-bit key")
	securityEncryptSecretsFileCmd.Flags().String("key-file", "", "Path to a file holding the base64-encoded key")
	securityEncryptSecretsFileCmd.Flags().Bool("force", false, "Overwrite the output file if it already exists")
}
