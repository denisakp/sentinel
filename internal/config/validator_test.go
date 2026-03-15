package config

import (
	"strings"
	"testing"
)

func TestValidateConfig_GCSStorageRequiresBucket(t *testing.T) {
	enabled := true
	cfg := &Configuration{
		Version:              "1.0",
		MaxConcurrentBackups: 1,
		Databases: map[string]BackupJob{
			"pg": {
				Name:        "pg",
				Type:        "postgres",
				Enabled:     &enabled,
				Host:        "localhost",
				Username:    "sentinel",
				PasswordEnv: "TEST_PG_PASSWORD",
				Database:    "app",
				Storage: StorageConfig{
					Type: "gcs",
				},
			},
		},
	}

	err := ValidateConfig(cfg)
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "bucket") {
		t.Fatalf("expected bucket validation error, got %v", err)
	}
}
