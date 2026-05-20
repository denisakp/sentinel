package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/denisakp/sentinel/internal/config"
	"github.com/spf13/cobra"
)

// buildBackupCmd reproduces the relevant subset of BackupCmd flag registration
// in an isolated *cobra.Command so tests don't mutate the package-global
// BackupCmd flag state across cases.
func buildBackupCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "backup"}
	cmd.Flags().StringP("password", "p", "", "Database password")
	cmd.Flags().String("password-env", "", "Name of environment variable holding the database password")
	cmd.Flags().String("password-file", "", "Path to a file whose first line is the database password")
	_ = cmd.Flags().MarkDeprecated("password", passwordFlagDeprecationSuffix)
	return cmd
}

// runResolvePassword sets up a fresh command + buffers and invokes the
// resolvePassword helper. Returns (password, err, stderr).
func runResolvePassword(t *testing.T, argv []string, job config.BackupJob) (string, error, string) {
	t.Helper()
	passwordFilePermWarnOnce = sync.Once{}
	cmd := buildBackupCmd()
	var stderr bytes.Buffer
	cmd.SetErr(&stderr)
	cmd.SetOut(&bytes.Buffer{})
	cmd.Flags().SetOutput(&stderr)
	if err := cmd.ParseFlags(argv); err != nil {
		return "", err, stderr.String()
	}
	pw, err := resolvePassword(cmd, job)
	return pw, err, stderr.String()
}

func writeTempPwFile(t *testing.T, body string, mode os.FileMode) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "pw")
	if err := os.WriteFile(p, []byte(body), mode); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.Chmod(p, mode); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	return p
}

func TestResolvePassword_PasswordEnvFlag(t *testing.T) {
	t.Setenv("BTEST_PWD_OK", "hunter2")
	pw, err, _ := runResolvePassword(t, []string{"--password-env", "BTEST_PWD_OK"}, config.BackupJob{Type: "postgres"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if pw != "hunter2" {
		t.Fatalf("pw=%q", pw)
	}
}

func TestResolvePassword_PasswordFileFlag_600NoWarning(t *testing.T) {
	p := writeTempPwFile(t, "hunter2\n", 0o600)
	pw, err, stderr := runResolvePassword(t, []string{"--password-file", p}, config.BackupJob{Type: "postgres"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if pw != "hunter2" {
		t.Fatalf("pw=%q", pw)
	}
	if strings.Contains(stderr, "warning") {
		t.Fatalf("did not expect warning, got: %q", stderr)
	}
}

func TestResolvePassword_PasswordFileFlag_644EmitsWarningOnce(t *testing.T) {
	p := writeTempPwFile(t, "hunter2\n", 0o644)
	pw, err, stderr := runResolvePassword(t, []string{"--password-file", p}, config.BackupJob{Type: "postgres"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if pw != "hunter2" {
		t.Fatalf("pw=%q", pw)
	}
	if !strings.Contains(stderr, "warning: password file") || !strings.Contains(stderr, "0o644") || !strings.Contains(stderr, "recommend chmod 0600") {
		t.Fatalf("warning wording missing: %q", stderr)
	}
	if count := strings.Count(stderr, "warning: password file"); count != 1 {
		t.Fatalf("warning emitted %d times, want 1", count)
	}

	// Re-call: sync.Once should suppress repeat warnings within a process.
	// Reset is intentionally not called here.
	cmd := buildBackupCmd()
	var stderr2 bytes.Buffer
	cmd.SetErr(&stderr2)
	if err := cmd.ParseFlags([]string{"--password-file", p}); err != nil {
		t.Fatal(err)
	}
	if _, err := resolvePassword(cmd, config.BackupJob{Type: "postgres"}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stderr2.String(), "warning") {
		t.Fatalf("second call should not re-emit warning, got: %q", stderr2.String())
	}
}

func TestResolvePassword_ConflictFlags(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want []string
	}{
		{"password+env", []string{"--password", "x", "--password-env", "FOO"}, []string{"--password", "--password-env"}},
		{"password+file", []string{"--password", "x", "--password-file", "/tmp/x"}, []string{"--password", "--password-file"}},
		{"env+file", []string{"--password-env", "FOO", "--password-file", "/tmp/x"}, []string{"--password-env", "--password-file"}},
		{"all three", []string{"--password", "x", "--password-env", "FOO", "--password-file", "/tmp/x"}, []string{"--password", "--password-env", "--password-file"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err, _ := runResolvePassword(t, tc.args, config.BackupJob{Type: "postgres"})
			if !errors.Is(err, config.ErrMultipleFlags) {
				t.Fatalf("want ErrMultipleFlags, got %v", err)
			}
			for _, w := range tc.want {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("err must name %q: %v", w, err)
				}
			}
			if !strings.Contains(err.Error(), "use exactly one of --password, --password-env, --password-file") {
				t.Errorf("err must contain remediation: %v", err)
			}
		})
	}
}

func TestResolvePassword_EnvUnset(t *testing.T) {
	t.Setenv("BTEST_PWD_UNSET", "")
	_, err, _ := runResolvePassword(t, []string{"--password-env", "BTEST_PWD_UNSET"}, config.BackupJob{Type: "postgres"})
	if !errors.Is(err, config.ErrEnvVarUnset) {
		t.Fatalf("want ErrEnvVarUnset, got %v", err)
	}
	if !strings.Contains(err.Error(), "BTEST_PWD_UNSET") || !strings.Contains(err.Error(), "--password-env") {
		t.Fatalf("error must name var and flag: %v", err)
	}
}

func TestResolvePassword_FlagOverridesConfigSilently(t *testing.T) {
	t.Setenv("BTEST_FLAG_OVERRIDE", "from-flag")
	t.Setenv("BTEST_CONFIG_DEFAULT", "from-config")
	pw, err, stderr := runResolvePassword(t,
		[]string{"--password-env", "BTEST_FLAG_OVERRIDE"},
		config.BackupJob{Type: "postgres", PasswordEnv: "BTEST_CONFIG_DEFAULT"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if pw != "from-flag" {
		t.Fatalf("pw=%q", pw)
	}
	if strings.Contains(stderr, "warning") {
		t.Fatalf("override must be silent, got: %q", stderr)
	}
}

func TestResolvePassword_DeprecationNoticeOnPasswordFlag(t *testing.T) {
	passwordFilePermWarnOnce = sync.Once{}
	cmd := buildBackupCmd()
	var stderr bytes.Buffer
	cmd.SetErr(&stderr)
	cmd.SetOut(&bytes.Buffer{})
	cmd.Flags().SetOutput(&stderr)
	if err := cmd.ParseFlags([]string{"--password", "x"}); err != nil {
		t.Fatalf("parse: %v", err)
	}
	// pflag emits the deprecation notice during ParseFlags.
	out := stderr.String()
	wantPrefix := "Flag --password has been deprecated, "
	if !strings.HasPrefix(out, wantPrefix) {
		t.Fatalf("missing pflag deprecation prefix; got: %q", out)
	}
	if !strings.Contains(out, passwordFlagDeprecationSuffix) {
		t.Fatalf("missing suffix; got: %q", out)
	}
	if count := strings.Count(out, "has been deprecated"); count != 1 {
		t.Fatalf("deprecation emitted %d times, want 1", count)
	}

	if _, err := resolvePassword(cmd, config.BackupJob{Type: "postgres"}); err != nil {
		t.Fatalf("resolve err: %v", err)
	}
}

func TestResolvePassword_NoFlagNoDeprecationNotice(t *testing.T) {
	t.Setenv("BTEST_NO_FLAG", "x")
	_, err, stderr := runResolvePassword(t,
		[]string{"--password-env", "BTEST_NO_FLAG"},
		config.BackupJob{Type: "postgres"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if strings.Contains(stderr, "deprecated") {
		t.Fatalf("must not emit deprecation when --password absent: %q", stderr)
	}
}
