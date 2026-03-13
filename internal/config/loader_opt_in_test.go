package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTempConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "sentinel.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func TestLoadConfig_NoImplicitEncryptionDefaults(t *testing.T) {
	t.Setenv("TEST_PG_PASSWORD", "dummy")

	cfgPath := writeTempConfig(t, `version: "1.0"
defaults:
  storage:
    type: local
    local_path: ./backups

databases:
  pg:
    type: postgres
    host: localhost
    port: 5432
    username: sentinel
    password_env: TEST_PG_PASSWORD
    database: sentinel
`)

	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	if cfg.EncryptionKeyEnv != "" {
		t.Fatalf("EncryptionKeyEnv = %q, want empty", cfg.EncryptionKeyEnv)
	}
	if cfg.EncryptionKeyFile != "" {
		t.Fatalf("EncryptionKeyFile = %q, want empty", cfg.EncryptionKeyFile)
	}
}

func TestValidateConfig_EncryptionChecksRemainRuntimeOnly(t *testing.T) {
	t.Setenv("TEST_PG_PASSWORD", "dummy")

	cfgPath := writeTempConfig(t, `version: "1.0"
encryption_key_env: TEST_SENTINEL_MASTER_KEY
defaults:
  storage:
    type: local
    local_path: ./backups

databases:
  pg:
    type: postgres
    host: localhost
    port: 5432
    username: sentinel
    password_env: TEST_PG_PASSWORD
    database: sentinel
`)

	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if cfg.EncryptionKeyEnv != "TEST_SENTINEL_MASTER_KEY" {
		t.Fatalf("EncryptionKeyEnv = %q, want TEST_SENTINEL_MASTER_KEY", cfg.EncryptionKeyEnv)
	}

	if err := ValidateConfig(cfg); err != nil {
		t.Fatalf("ValidateConfig() error = %v, want nil", err)
	}
}
