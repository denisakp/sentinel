package config

import (
	"strings"
	"testing"
)

func TestValidateIntegrity(t *testing.T) {
	cases := []struct {
		algorithm string
		wantErr   bool
	}{
		{"", false},       // default
		{"sha256", false}, // the only supported algorithm
		{"md5", true},
		{"sha512", true},
		{"SHA256", true}, // case-sensitive: only lowercase "sha256"
	}
	for _, c := range cases {
		err := validateIntegrity(IntegrityConfig{Algorithm: c.algorithm})
		if c.wantErr && err == nil {
			t.Errorf("validateIntegrity(%q): want error, got nil", c.algorithm)
		}
		if !c.wantErr && err != nil {
			t.Errorf("validateIntegrity(%q): unexpected error: %v", c.algorithm, err)
		}
	}
}

// TestValidateConfig_IntegrityAlgorithm asserts the integrity block is wired
// into ValidateConfig.
func TestValidateConfig_IntegrityAlgorithm(t *testing.T) {
	enabled := true
	newCfg := func(algo string) *Configuration {
		return &Configuration{
			Version:              "1.0",
			MaxConcurrentBackups: 1,
			Integrity:            IntegrityConfig{Algorithm: algo},
			Databases: map[string]BackupJob{
				"pg": {
					Name:        "pg",
					Type:        "postgres",
					Enabled:     &enabled,
					Host:        "localhost",
					Username:    "sentinel",
					PasswordEnv: "TEST_PG_PASSWORD",
					Database:    "app",
					Storage:     StorageConfig{Type: "local", LocalPath: "/tmp/backups"},
				},
			},
		}
	}

	if err := ValidateConfig(newCfg("sha256")); err != nil {
		t.Fatalf("sha256 should be valid, got %v", err)
	}

	err := ValidateConfig(newCfg("crc32"))
	if err == nil {
		t.Fatal("expected an integrity.algorithm validation error")
	}
	if !strings.Contains(err.Error(), "integrity.algorithm") {
		t.Fatalf("expected integrity.algorithm error, got %v", err)
	}
}
