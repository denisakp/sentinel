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

// scheduledCheckConfig builds a minimal valid config with one postgres backup
// job and the given integrity.scheduled_check block.
func scheduledCheckConfig(sc IntegrityScheduledCheck) *Configuration {
	enabled := true
	return &Configuration{
		Version:              "1.0",
		MaxConcurrentBackups: 1,
		Integrity:            IntegrityConfig{ScheduledCheck: sc},
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

// TestValidateScheduledIntegrityCheck_Matrix asserts the config-validation
// matrix for integrity.scheduled_check.
func TestValidateScheduledIntegrityCheck_Matrix(t *testing.T) {
	cases := []struct {
		name    string
		sc      IntegrityScheduledCheck
		wantErr string // "" = expect success
	}{
		{"disabled ignores empty cron", IntegrityScheduledCheck{Enabled: false}, ""},
		{"valid failure mode", IntegrityScheduledCheck{Enabled: true, Cron: "0 3 * * 0", NotifyOn: "failure"}, ""},
		{"valid always + since + job", IntegrityScheduledCheck{Enabled: true, Cron: "*/30 * * * *", Since: "30d", NotifyOn: "always", Job: "pg"}, ""},
		{"valid empty notify_on defaults", IntegrityScheduledCheck{Enabled: true, Cron: "0 3 * * 0"}, ""},
		{"valid never", IntegrityScheduledCheck{Enabled: true, Cron: "0 3 * * 0", NotifyOn: "never"}, ""},
		{"enabled without cron", IntegrityScheduledCheck{Enabled: true}, "cron is required"},
		{"enabled with blank cron", IntegrityScheduledCheck{Enabled: true, Cron: "   "}, "cron is required"},
		{"invalid cron", IntegrityScheduledCheck{Enabled: true, Cron: "not a cron"}, "invalid cron expression"},
		{"invalid since", IntegrityScheduledCheck{Enabled: true, Cron: "0 3 * * 0", Since: "5x"}, "since"},
		{"invalid notify_on", IntegrityScheduledCheck{Enabled: true, Cron: "0 3 * * 0", NotifyOn: "page"}, "notify_on"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateConfig(scheduledCheckConfig(tc.sc))
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("ValidateConfig() unexpected error = %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("ValidateConfig() error = nil, want substring %q", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("ValidateConfig() error = %v, want substring %q", err, tc.wantErr)
			}
		})
	}
}

// TestValidateScheduledIntegrityCheck_ReservedName asserts a user backup or
// restore job may not take the reserved __integrity_check name,
// independent of whether the scheduled check is enabled.
func TestValidateScheduledIntegrityCheck_ReservedName(t *testing.T) {
	enabled := true

	dbCfg := scheduledCheckConfig(IntegrityScheduledCheck{})
	dbCfg.Databases[IntegrityCheckJobName] = BackupJob{
		Name: IntegrityCheckJobName, Type: "postgres", Enabled: &enabled,
		Host: "localhost", Username: "sentinel", PasswordEnv: "TEST_PG_PASSWORD",
		Database: "app", Storage: StorageConfig{Type: "local", LocalPath: "/tmp/backups"},
	}
	err := ValidateConfig(dbCfg)
	if err == nil || !strings.Contains(err.Error(), "reserved") {
		t.Fatalf("backup job named %q must be rejected as reserved; got %v", IntegrityCheckJobName, err)
	}

	restoreCfg := scheduledCheckConfig(IntegrityScheduledCheck{})
	restoreCfg.Restores = map[string]RestoreJob{
		IntegrityCheckJobName: {Type: "postgres", Database: "app"},
	}
	err = ValidateConfig(restoreCfg)
	if err == nil || !strings.Contains(err.Error(), "reserved") {
		t.Fatalf("restore job named %q must be rejected as reserved; got %v", IntegrityCheckJobName, err)
	}
}
