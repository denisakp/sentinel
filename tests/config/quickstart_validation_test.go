package config_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/denisakp/sentinel/internal/config"
)

func buildBinary(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	binName := "sentinel"
	if runtime.GOOS == "windows" {
		binName += ".exe"
	}
	out := filepath.Join(dir, binName)
	cmd := exec.Command("go", "build", "-o", out, ".")
	cmd.Dir = repoRoot(t)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to build sentinel binary: %v\n%s", err, stderr.String())
	}
	return out
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed")
	}
	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find repository root (go.mod not found)")
		}
		dir = parent
	}
}

func loadFixture(t *testing.T, yamlContent string) {
	t.Helper()
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgFile, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("write temp config: %v", err)
	}
	cfg, err := config.LoadConfig(cfgFile)
	if err != nil {
		t.Fatalf("config.LoadConfig unexpected error: %v", err)
	}
	if cfg == nil {
		t.Fatal("config.LoadConfig returned nil")
	}
}

func runCmd(t *testing.T, bin string, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	code := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		} else {
			code = -1
		}
	}
	return buf.String(), code
}

func TestQuickstart_RootHelp(t *testing.T) {
	bin := buildBinary(t)
	out, code := runCmd(t, bin, "--help")
	if code != 0 {
		t.Fatalf("sentinel --help exited %d:\n%s", code, out)
	}
	for _, want := range []string{"backup", "schedule", "security", "storage", "monitor", "retention"} {
		if !strings.Contains(out, want) {
			t.Errorf("--help output missing %q subcommand", want)
		}
	}
}

func TestQuickstart_ScheduleHelp(t *testing.T) {
	bin := buildBinary(t)
	out, _ := runCmd(t, bin, "schedule", "--help")
	if !strings.Contains(out, "start") {
		t.Errorf("schedule --help missing 'start' subcommand:\n%s", out)
	}
}

func TestQuickstart_SecurityInitKey(t *testing.T) {
	bin := buildBinary(t)
	out, code := runCmd(t, bin, "security", "init-key")
	if code != 0 {
		t.Fatalf("security init-key exited %d:\n%s", code, out)
	}
	if !strings.Contains(out, "Key:") {
		t.Errorf("security init-key output missing 'Key:' field:\n%s", out)
	}
	if !strings.Contains(out, "SENTINEL_MASTER_KEY") {
		t.Errorf("security init-key output missing SENTINEL_MASTER_KEY usage hint:\n%s", out)
	}
}

func TestQuickstart_TLSConfig_ParsesWithoutError(t *testing.T) {
	// Set dummy env vars required by the quickstart YAML pattern.
	t.Setenv("DB_HOST", "localhost")
	t.Setenv("DB_USER", "sentinel")
	t.Setenv("DB_PASSWORD", "dummy")
	loadFixture(t, "version: \"1.0\"\n"+
		"defaults:\n"+
		"  storage:\n"+
		"    type: local\n"+
		"databases:\n"+
		"  prod-postgres:\n"+
		"    type: postgres\n"+
		"    host_env: DB_HOST\n"+
		"    port: 5432\n"+
		"    username_env: DB_USER\n"+
		"    password_env: DB_PASSWORD\n"+
		"    database: myapp\n"+
		"    schedule: \"0 2 * * *\"\n"+
		"    tls:\n"+
		"      enabled: true\n"+
		"      mode: verify-full\n"+
		"      ca_cert: /tmp/ca.crt\n")
}

func TestQuickstart_BackupVerify_SubcommandRegistered(t *testing.T) {
	bin := buildBinary(t)
	out, _ := runCmd(t, bin, "backup", "--help")
	if !strings.Contains(out, "verify") {
		t.Errorf("backup --help missing 'verify' subcommand:\n%s", out)
	}
}

func TestQuickstart_BackupVerify_MissingConfigExitsCleanly(t *testing.T) {
	bin := buildBinary(t)
	out, _ := runCmd(t, bin, "backup", "verify", "nonexistent-backup-id")
	if strings.Contains(out, "panic:") {
		t.Fatalf("backup verify panicked:\n%s", out)
	}
}

func TestQuickstart_StorageStatus_SubcommandRegistered(t *testing.T) {
	bin := buildBinary(t)
	out, _ := runCmd(t, bin, "storage", "--help")
	if !strings.Contains(out, "status") {
		t.Errorf("storage --help missing 'status' subcommand:\n%s", out)
	}
}

func TestQuickstart_StorageStatus_WithoutConfigExitsCleanly(t *testing.T) {
	bin := buildBinary(t)
	out, _ := runCmd(t, bin, "storage", "status")
	if strings.Contains(out, "panic:") {
		t.Fatalf("storage status panicked:\n%s", out)
	}
}

func TestQuickstart_MonitorList_SubcommandRegistered(t *testing.T) {
	bin := buildBinary(t)
	out, _ := runCmd(t, bin, "monitor", "--help")
	if !strings.Contains(out, "list") {
		t.Errorf("monitor --help missing 'list' subcommand:\n%s", out)
	}
}

func TestQuickstart_MonitorList_WithoutConfigExitsCleanly(t *testing.T) {
	bin := buildBinary(t)
	out, _ := runCmd(t, bin, "monitor", "list", "--limit", "5")
	if strings.Contains(out, "panic:") {
		t.Fatalf("monitor list panicked:\n%s", out)
	}
}

func TestQuickstart_AzureConfig_ParsesWithoutError(t *testing.T) {
	t.Setenv("PG_PASSWORD", "dummy")
	loadFixture(t, "version: \"1.0\"\n"+
		"storages:\n"+
		"  azure-production:\n"+
		"    type: azure\n"+
		"    account_name: myaccount\n"+
		"    container: sentinel-backups\n"+
		"    tier: Cool\n"+
		"    auth:\n"+
		"      type: managed_identity\n"+
		"databases:\n"+
		"  prod-postgres:\n"+
		"    type: postgres\n"+
		"    host: localhost\n"+
		"    username: sentinel\n"+
		"    password_env: PG_PASSWORD\n"+
		"    database: myapp\n"+
		"    schedule: \"0 2 * * *\"\n"+
		"    storage:\n"+
		"      type: azure\n"+
		"      name: azure-production\n")
}

func TestQuickstart_CredentialProtection_EnvVarConfig_ParsesWithoutError(t *testing.T) {
	t.Setenv("MY_DB_PASSWORD", "dummy")
	loadFixture(t, "version: \"1.0\"\n"+
		"defaults:\n"+
		"  storage:\n"+
		"    type: local\n"+
		"databases:\n"+
		"  my-db:\n"+
		"    type: mysql\n"+
		"    host: localhost\n"+
		"    username: root\n"+
		"    password_env: MY_DB_PASSWORD\n"+
		"    database: myapp\n")
}
