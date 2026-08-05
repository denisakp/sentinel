package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTestDefaultsFile(t *testing.T, content string, mode os.FileMode) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, ".my.cnf")
	if err := os.WriteFile(p, []byte(content), mode); err != nil {
		t.Fatalf("write defaults file: %v", err)
	}
	if err := os.Chmod(p, mode); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	return p
}

// T010 (US1): credentials only in defaults_file — Host/Username resolved,
// ResolveJobPassword returns the file's password.
func TestLoadConfig_DefaultsFileOnlySource(t *testing.T) {
	dfPath := writeTestDefaultsFile(t, "[client]\nhost=127.0.0.1\nuser=sentinel_backup\npassword=s3cr3t\nport=3306\n", 0o600)

	cfgPath := writeTempConfig(t, "version: \"1.0\"\n"+
		"defaults:\n  storage:\n    type: local\n    local_path: ./backups\n\n"+
		"databases:\n  mydb:\n    type: mysql\n    database: app\n"+
		"    defaults_file: \""+dfPath+"\"\n")

	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	job := cfg.Databases["mydb"]
	if job.Host != "127.0.0.1" {
		t.Fatalf("Host = %q, want 127.0.0.1", job.Host)
	}
	if job.Username != "sentinel_backup" {
		t.Fatalf("Username = %q, want sentinel_backup", job.Username)
	}
	if job.Port != 3306 {
		t.Fatalf("Port = %d, want 3306", job.Port)
	}
	pw, err := ResolveJobPassword(job)
	if err != nil {
		t.Fatalf("ResolveJobPassword error = %v", err)
	}
	if pw != "s3cr3t" {
		t.Fatalf("password = %q, want s3cr3t", pw)
	}

	if err := ValidateConfig(cfg); err != nil {
		t.Fatalf("ValidateConfig() error = %v (defaults_file-only config should validate)", err)
	}
}

// T012 (US2): explicit host wins over the file's host; other fields still
// come from the file.
func TestLoadConfig_DefaultsFileExplicitFieldWins(t *testing.T) {
	dfPath := writeTestDefaultsFile(t, "[client]\nhost=file-host\nuser=file-user\npassword=file-pass\n", 0o600)

	cfgPath := writeTempConfig(t, "version: \"1.0\"\n"+
		"defaults:\n  storage:\n    type: local\n    local_path: ./backups\n\n"+
		"databases:\n  mydb:\n    type: mysql\n    database: app\n"+
		"    host: explicit-host\n"+
		"    defaults_file: \""+dfPath+"\"\n")

	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	job := cfg.Databases["mydb"]
	if job.Host != "explicit-host" {
		t.Fatalf("Host = %q, want explicit-host (explicit must win)", job.Host)
	}
	if job.Username != "file-user" {
		t.Fatalf("Username = %q, want file-user (fallback should still apply)", job.Username)
	}
	pw, err := ResolveJobPassword(job)
	if err != nil || pw != "file-pass" {
		t.Fatalf("password = %q err=%v, want file-pass", pw, err)
	}
}

// T013 (US2): explicit credentials cover every field — file present but
// unused, no error from its mere presence.
func TestLoadConfig_DefaultsFilePresentButFullyOverridden(t *testing.T) {
	dfPath := writeTestDefaultsFile(t, "[client]\nhost=file-host\nuser=file-user\npassword=file-pass\n", 0o600)

	t.Setenv("MYDB_PW", "explicit-secret")
	cfgPath := writeTempConfig(t, "version: \"1.0\"\n"+
		"defaults:\n  storage:\n    type: local\n    local_path: ./backups\n\n"+
		"databases:\n  mydb:\n    type: mysql\n    database: app\n"+
		"    host: explicit-host\n    username: explicit-user\n    password_env: MYDB_PW\n"+
		"    defaults_file: \""+dfPath+"\"\n")

	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	job := cfg.Databases["mydb"]
	if job.Host != "explicit-host" || job.Username != "explicit-user" {
		t.Fatalf("got Host=%q Username=%q, want explicit values unchanged", job.Host, job.Username)
	}
	pw, err := ResolveJobPassword(job)
	if err != nil || pw != "explicit-secret" {
		t.Fatalf("password = %q err=%v, want explicit-secret (password_env must win)", pw, err)
	}
	if err := ValidateConfig(cfg); err != nil {
		t.Fatalf("ValidateConfig() error = %v", err)
	}
}

// T015 (US3): nonexistent defaults_file path fails fast at load time.
func TestLoadConfig_DefaultsFileMissingPathFailsFast(t *testing.T) {
	cfgPath := writeTempConfig(t, "version: \"1.0\"\n"+
		"defaults:\n  storage:\n    type: local\n    local_path: ./backups\n\n"+
		"databases:\n  mydb:\n    type: mysql\n    database: app\n"+
		"    defaults_file: /nonexistent/path/.my.cnf\n")

	_, err := LoadConfig(cfgPath)
	if err == nil {
		t.Fatal("LoadConfig() expected error for missing defaults_file, got nil")
	}
	if !strings.Contains(err.Error(), "/nonexistent/path/.my.cnf") {
		t.Fatalf("error %q does not name the file", err.Error())
	}
}

// T016 (US3): malformed defaults_file fails fast at load time.
func TestLoadConfig_DefaultsFileMalformedFailsFast(t *testing.T) {
	dfPath := writeTestDefaultsFile(t, "[client\nhost=127.0.0.1\n", 0o600)

	cfgPath := writeTempConfig(t, "version: \"1.0\"\n"+
		"defaults:\n  storage:\n    type: local\n    local_path: ./backups\n\n"+
		"databases:\n  mydb:\n    type: mysql\n    database: app\n"+
		"    defaults_file: \""+dfPath+"\"\n")

	_, err := LoadConfig(cfgPath)
	if err == nil {
		t.Fatal("LoadConfig() expected error for malformed defaults_file, got nil")
	}
}

// T016 (US3): a defaults_file with no [client] section at all, and no other
// credential source, still surfaces the standard missing-password error at
// validation time (not silently accepted, not a parse failure either).
func TestLoadConfig_DefaultsFileNoClientSectionFallsThroughToMissingError(t *testing.T) {
	dfPath := writeTestDefaultsFile(t, "[mysqldump]\nquick\n", 0o600)

	cfgPath := writeTempConfig(t, "version: \"1.0\"\n"+
		"defaults:\n  storage:\n    type: local\n    local_path: ./backups\n\n"+
		"databases:\n  mydb:\n    type: mysql\n    database: app\n"+
		"    defaults_file: \""+dfPath+"\"\n")

	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v (no [client] section should not be a load error)", err)
	}
	if err := ValidateConfig(cfg); err == nil {
		t.Fatal("ValidateConfig() expected error: no credential source supplied host/username/password")
	}
}

// T018: a mysql job with NO defaults_file at all — confirm zero regression.
func TestLoadConfig_NoDefaultsFileUnchangedBehavior(t *testing.T) {
	t.Setenv("MYDB_PW_NOREGRESSION", "secret")
	cfgPath := writeTempConfig(t, "version: \"1.0\"\n"+
		"defaults:\n  storage:\n    type: local\n    local_path: ./backups\n\n"+
		"databases:\n  mydb:\n    type: mysql\n    database: app\n"+
		"    host: localhost\n    username: sentinel\n    password_env: MYDB_PW_NOREGRESSION\n")

	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	job := cfg.Databases["mydb"]
	if job.Host != "localhost" || job.Username != "sentinel" {
		t.Fatalf("got Host=%q Username=%q", job.Host, job.Username)
	}
	pw, err := ResolveJobPassword(job)
	if err != nil || pw != "secret" {
		t.Fatalf("password = %q err=%v, want secret", pw, err)
	}
	if err := ValidateConfig(cfg); err != nil {
		t.Fatalf("ValidateConfig() error = %v", err)
	}

	// Missing password_env entirely (and no defaults_file) must still be
	// rejected at ValidateConfig, exactly as before this feature (loader.go's
	// own requireEnvValue is a no-op on an empty env name; the real
	// enforcement lives in validateConnection).
	cfgPath2 := writeTempConfig(t, "version: \"1.0\"\n"+
		"defaults:\n  storage:\n    type: local\n    local_path: ./backups\n\n"+
		"databases:\n  mydb:\n    type: mysql\n    database: app\n"+
		"    host: localhost\n    username: sentinel\n")
	cfg2, err := LoadConfig(cfgPath2)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v (should load; validation happens separately)", err)
	}
	if err := ValidateConfig(cfg2); err == nil || !strings.Contains(err.Error(), "password_env") {
		t.Fatalf("got err=%v, want a password_env-required error (unchanged behavior)", err)
	}
}

// T017: defaults_file on a non-mysql/mariadb job is rejected by ValidateConfig.
func TestValidateConfig_DefaultsFileRejectedForWrongType(t *testing.T) {
	t.Setenv("PG_PW_REJECT_TEST", "secret")
	cfgPath := writeTempConfig(t, "version: \"1.0\"\n"+
		"defaults:\n  storage:\n    type: local\n    local_path: ./backups\n\n"+
		"databases:\n  pgjob:\n    type: postgres\n    database: app\n"+
		"    host: localhost\n    username: sentinel\n    password_env: PG_PW_REJECT_TEST\n"+
		"    defaults_file: /etc/sentinel/.my.cnf\n")

	// LoadConfig itself must not error just from the presence of an
	// unreadable/irrelevant defaults_file on a postgres job (loader.go skips
	// resolution entirely for non-mysql/mariadb types) — the rejection is
	// ValidateConfig's job.
	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v (loader must skip defaults_file for non-mysql/mariadb types)", err)
	}
	err = ValidateConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "defaults_file is only valid for mysql or mariadb backup jobs") {
		t.Fatalf("got err=%v, want the defaults_file type-rejection error", err)
	}
}

// T020 (Polish): group/world-readable defaults_file triggers a warning,
// without blocking config load.
func TestLoadConfig_DefaultsFilePermissionWarning(t *testing.T) {
	dfPath := writeTestDefaultsFile(t, "[client]\nhost=127.0.0.1\nuser=sentinel\npassword=s3cr3t\n", 0o644)

	cfgPath := writeTempConfig(t, "version: \"1.0\"\n"+
		"defaults:\n  storage:\n    type: local\n    local_path: ./backups\n\n"+
		"databases:\n  mydb:\n    type: mysql\n    database: app\n"+
		"    defaults_file: \""+dfPath+"\"\n")

	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	_, err := LoadConfig(cfgPath)
	w.Close()
	os.Stderr = oldStderr
	if err != nil {
		t.Fatalf("LoadConfig() error = %v (permission issue must warn, not fail)", err)
	}

	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	captured := string(buf[:n])
	if !strings.Contains(captured, "group- or world-readable") {
		t.Fatalf("expected a permission warning on stderr, got: %q", captured)
	}
}
