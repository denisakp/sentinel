package integration_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestScheduleListAndRetentionPreview(t *testing.T) {
	if os.Getenv("SENTINEL_INTEGRATION_TESTS") == "" {
		t.Skip("SENTINEL_INTEGRATION_TESTS not set")
	}

	binPath := buildSentinel(t)
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "sentinel.yaml")

	yaml := []byte(`version: "1.0"
defaults:
    storage:
        type: local
        local_path: "./backups"
databases:
    sample:
        type: postgres
        schedule: "0 2 * * *"
        host: "localhost"
        port: 5432
        username: "postgres"
        password_env: "PG_PASSWORD"
        database: "postgres"
        retention:
            keep_last: 1
`)

	if err := os.WriteFile(configPath, yaml, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	env := append(os.Environ(), "PG_PASSWORD=dummy")

	scheduleCmd := exec.Command(binPath, "schedule", "list", "--config", configPath)
	scheduleCmd.Env = env
	scheduleCmd.Dir = projectRoot(t)
	if err := scheduleCmd.Run(); err != nil {
		t.Fatalf("schedule list: %v", err)
	}

	retentionCmd := exec.Command(binPath, "retention", "preview", "--config", configPath)
	retentionCmd.Env = env
	retentionCmd.Dir = projectRoot(t)
	if err := retentionCmd.Run(); err != nil {
		t.Fatalf("retention preview: %v", err)
	}
}
