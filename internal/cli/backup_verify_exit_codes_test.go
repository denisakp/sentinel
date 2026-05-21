package cli

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
)

var _ = os.DevNull

// runVerify invokes backupVerifyCmd's RunE with the supplied args/flags and
// returns its error. Stdout is silenced.
func runVerify(t *testing.T, configPath, backupID string) error {
	t.Helper()
	cmd := &cobra.Command{Use: "verify-test"}
	cmd.Flags().String("config", configPath, "")
	cmd.Flags().String("output", "text", "")
	cmd.Flags().Bool("allow-legacy-envelope", false, "")
	return backupVerifyCmd.RunE(cmd, []string{backupID})
}

func writeYAML(t *testing.T, dir, contents string) string {
	t.Helper()
	p := filepath.Join(dir, "sentinel.yaml")
	if err := os.WriteFile(p, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestBackupVerifyExitCodes_ConfigLoadFailureIs4(t *testing.T) {
	err := runVerify(t, "/nonexistent/path/sentinel.yaml", "any")
	if got := Code(err); got != 4 {
		t.Fatalf("config load failure: want code 4, got %d (err=%v)", got, err)
	}
	if !errors.Is(err, ErrVerifyInternal) {
		t.Fatalf("want ErrVerifyInternal, got %v", err)
	}
}

func TestBackupVerifyExitCodes_NotFoundIs2(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeYAML(t, dir, "history_db_path: "+filepath.Join(dir, "history.db")+`
databases:
  dummy:
    type: postgres
    host: localhost
    port: 5432
    user: u
    database: d
    schedule: "0 0 * * *"
`)

	err := runVerify(t, cfgPath, "definitely-not-an-id")
	if got := Code(err); got != 2 {
		t.Fatalf("missing backup: want code 2, got %d (err=%v)", got, err)
	}
	if !errors.Is(err, ErrVerifyNotFound) {
		t.Fatalf("want ErrVerifyNotFound, got %v", err)
	}
}
