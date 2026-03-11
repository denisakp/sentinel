package integration_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestConfigToBackupIntegration(t *testing.T) {
	if os.Getenv("SENTINEL_INTEGRATION_TESTS") == "" {
		t.Skip("set SENTINEL_INTEGRATION_TESTS=1 to run integration tests")
	}

	host := os.Getenv("SENTINEL_TEST_PG_HOST")
	user := os.Getenv("SENTINEL_TEST_PG_USER")
	password := os.Getenv("SENTINEL_TEST_PG_PASSWORD")
	database := os.Getenv("SENTINEL_TEST_PG_DB")

	if host == "" || user == "" || password == "" || database == "" {
		t.Skip("set SENTINEL_TEST_PG_HOST/USER/PASSWORD/DB to run this test")
	}

	configPath := writeTempConfig(t, "sentinel.yaml", `version: "1.0"
log_format: json
history_db_path: "`+filepath.Join(t.TempDir(), "history.db")+`"

defaults:
  storage:
    type: local
    local_path: "`+t.TempDir()+`"

databases:
  test-postgres:
    type: postgres
    host_env: SENTINEL_TEST_PG_HOST
    username_env: SENTINEL_TEST_PG_USER
    password_env: SENTINEL_TEST_PG_PASSWORD
    database: "`+database+`"
    schedule: "0 2 * * *"
    notifications: []
`)

	bin := buildSentinel(t)
	cmd := exec.Command(bin, "backup", "--config", configPath)
	cmd.Env = append(os.Environ(),
		"SENTINEL_TEST_PG_HOST="+host,
		"SENTINEL_TEST_PG_USER="+user,
		"SENTINEL_TEST_PG_PASSWORD="+password,
	)
	cmd.Dir = projectRoot(t)

	if err := cmd.Run(); err != nil {
		t.Fatalf("backup command failed: %v", err)
	}
}

func writeTempConfig(t *testing.T, name, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}
	return path
}
