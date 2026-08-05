package config

import (
	"os"
	"strings"
	"testing"
)

// T006 (US1): password-only secrets file fills a username-bearing URI.
func TestLoadConfig_MongoSecretsFilePasswordFills(t *testing.T) {
	sfPath := writeTestMongoSecretsFile(t, "password: s3cr3t\n", 0o600)

	cfgPath := writeTempConfig(t, "version: \"1.0\"\n"+
		"defaults:\n  storage:\n    type: local\n    local_path: ./backups\n\n"+
		"databases:\n  mongo1:\n    type: mongodb\n    database: app\n"+
		"    uri: \"mongodb://appuser@host:27017/\"\n"+
		"    mongo_secrets_file: \""+sfPath+"\"\n")

	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	job := cfg.Databases["mongo1"]
	if job.URI != "mongodb://appuser:s3cr3t@host:27017/" {
		t.Fatalf("URI = %q", job.URI)
	}
	if err := ValidateConfig(cfg); err != nil {
		t.Fatalf("ValidateConfig() error = %v", err)
	}
}

// T007 (US1): full-URI secrets file used when no inline uri/uri_env at all.
func TestLoadConfig_MongoSecretsFileFullURI(t *testing.T) {
	sfPath := writeTestMongoSecretsFile(t, "uri: \"mongodb://appuser:s3cr3t@host:27017/\"\n", 0o600)

	cfgPath := writeTempConfig(t, "version: \"1.0\"\n"+
		"defaults:\n  storage:\n    type: local\n    local_path: ./backups\n\n"+
		"databases:\n  mongo1:\n    type: mongodb\n    database: app\n"+
		"    mongo_secrets_file: \""+sfPath+"\"\n")

	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	job := cfg.Databases["mongo1"]
	if job.URI != "mongodb://appuser:s3cr3t@host:27017/" {
		t.Fatalf("URI = %q", job.URI)
	}
	if err := ValidateConfig(cfg); err != nil {
		t.Fatalf("ValidateConfig() error = %v", err)
	}
}

// T008 (US2): conflicting password sources -> config-load error.
func TestLoadConfig_MongoSecretsFileConflict(t *testing.T) {
	sfPath := writeTestMongoSecretsFile(t, "password: fromfile\n", 0o600)

	cfgPath := writeTempConfig(t, "version: \"1.0\"\n"+
		"defaults:\n  storage:\n    type: local\n    local_path: ./backups\n\n"+
		"databases:\n  mongo1:\n    type: mongodb\n    database: app\n"+
		"    uri: \"mongodb://u:already-set@host:27017/\"\n"+
		"    mongo_secrets_file: \""+sfPath+"\"\n")

	_, err := LoadConfig(cfgPath)
	if err == nil || !strings.Contains(err.Error(), "conflicting password sources") {
		t.Fatalf("got err=%v, want a conflicting-password-sources error", err)
	}
}

// T009 (US2): job.URI already set -> file's uri key silently unused, no error.
func TestLoadConfig_MongoSecretsFileURIUnusedWhenSet(t *testing.T) {
	sfPath := writeTestMongoSecretsFile(t, "uri: \"mongodb://other@host2:27017/\"\n", 0o600)

	cfgPath := writeTempConfig(t, "version: \"1.0\"\n"+
		"defaults:\n  storage:\n    type: local\n    local_path: ./backups\n\n"+
		"databases:\n  mongo1:\n    type: mongodb\n    database: app\n"+
		"    uri: \"mongodb://u:existing@host:27017/\"\n"+
		"    mongo_secrets_file: \""+sfPath+"\"\n")

	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	job := cfg.Databases["mongo1"]
	if job.URI != "mongodb://u:existing@host:27017/" {
		t.Fatalf("URI = %q, want unchanged from explicit value", job.URI)
	}
}

// T011 (US3): nonexistent secrets file path fails fast at load time.
func TestLoadConfig_MongoSecretsFileMissingPathFailsFast(t *testing.T) {
	cfgPath := writeTempConfig(t, "version: \"1.0\"\n"+
		"defaults:\n  storage:\n    type: local\n    local_path: ./backups\n\n"+
		"databases:\n  mongo1:\n    type: mongodb\n    database: app\n"+
		"    uri: \"mongodb://u@host:27017/\"\n"+
		"    mongo_secrets_file: /nonexistent/path/mongo.yaml\n")

	_, err := LoadConfig(cfgPath)
	if err == nil || !strings.Contains(err.Error(), "/nonexistent/path/mongo.yaml") {
		t.Fatalf("got err=%v, want an error naming the file", err)
	}
}

// T012 (US3): malformed secrets file fails fast at load time.
func TestLoadConfig_MongoSecretsFileMalformedFailsFast(t *testing.T) {
	sfPath := writeTestMongoSecretsFile(t, "uri: [unterminated\n", 0o600)

	cfgPath := writeTempConfig(t, "version: \"1.0\"\n"+
		"defaults:\n  storage:\n    type: local\n    local_path: ./backups\n\n"+
		"databases:\n  mongo1:\n    type: mongodb\n    database: app\n"+
		"    uri: \"mongodb://u@host:27017/\"\n"+
		"    mongo_secrets_file: \""+sfPath+"\"\n")

	_, err := LoadConfig(cfgPath)
	if err == nil {
		t.Fatal("LoadConfig() expected error for malformed secrets file, got nil")
	}
}

// T013 (US3): password with no username resolvable anywhere -> missing-username error.
func TestLoadConfig_MongoSecretsFileMissingUsername(t *testing.T) {
	sfPath := writeTestMongoSecretsFile(t, "password: fromfile\n", 0o600)

	cfgPath := writeTempConfig(t, "version: \"1.0\"\n"+
		"defaults:\n  storage:\n    type: local\n    local_path: ./backups\n\n"+
		"databases:\n  mongo1:\n    type: mongodb\n    database: app\n"+
		"    mongo_secrets_file: \""+sfPath+"\"\n")

	_, err := LoadConfig(cfgPath)
	if err == nil || !strings.Contains(err.Error(), "no username is known") {
		t.Fatalf("got err=%v, want a missing-username error", err)
	}
}

// T015: a mongodb job with NO mongo_secrets_file at all -- zero regression.
func TestLoadConfig_NoMongoSecretsFileUnchangedBehavior(t *testing.T) {
	cfgPath := writeTempConfig(t, "version: \"1.0\"\n"+
		"defaults:\n  storage:\n    type: local\n    local_path: ./backups\n\n"+
		"databases:\n  mongo1:\n    type: mongodb\n    database: app\n"+
		"    uri: \"mongodb://u:pw@host:27017/\"\n")

	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	job := cfg.Databases["mongo1"]
	if job.URI != "mongodb://u:pw@host:27017/" {
		t.Fatalf("URI = %q, want unchanged", job.URI)
	}
	if err := ValidateConfig(cfg); err != nil {
		t.Fatalf("ValidateConfig() error = %v", err)
	}

	// Missing uri/uri_env entirely (and no secrets file) must still be
	// rejected at ValidateConfig, exactly as before this feature.
	cfgPath2 := writeTempConfig(t, "version: \"1.0\"\n"+
		"defaults:\n  storage:\n    type: local\n    local_path: ./backups\n\n"+
		"databases:\n  mongo1:\n    type: mongodb\n    database: app\n")
	cfg2, err := LoadConfig(cfgPath2)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if err := ValidateConfig(cfg2); err == nil || !strings.Contains(err.Error(), "uri_env is required") {
		t.Fatalf("got err=%v, want uri_env-required error (unchanged behavior)", err)
	}
}

// T014: mongo_secrets_file on a non-mongodb job is rejected by ValidateConfig.
func TestValidateConfig_MongoSecretsFileRejectedForWrongType(t *testing.T) {
	t.Setenv("MYSQL_PW_MONGO_REJECT_TEST", "secret")
	cfgPath := writeTempConfig(t, "version: \"1.0\"\n"+
		"defaults:\n  storage:\n    type: local\n    local_path: ./backups\n\n"+
		"databases:\n  mysqljob:\n    type: mysql\n    database: app\n"+
		"    host: localhost\n    username: sentinel\n    password_env: MYSQL_PW_MONGO_REJECT_TEST\n"+
		"    mongo_secrets_file: /run/secrets/mongo.yaml\n")

	// LoadConfig must not error just from the presence of the field on a
	// mysql job (loader.go skips resolution entirely for non-mongodb types).
	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v (loader must skip mongo_secrets_file for non-mongodb types)", err)
	}
	err = ValidateConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "mongo_secrets_file is only valid for mongodb backup jobs") {
		t.Fatalf("got err=%v, want the mongo_secrets_file type-rejection error", err)
	}
}

// T017 (Polish): group/world-readable secrets file triggers a warning
// through the full LoadConfig path, without blocking load.
func TestLoadConfig_MongoSecretsFilePermissionWarning(t *testing.T) {
	sfPath := writeTestMongoSecretsFile(t, "password: s3cr3t\n", 0o644)

	cfgPath := writeTempConfig(t, "version: \"1.0\"\n"+
		"defaults:\n  storage:\n    type: local\n    local_path: ./backups\n\n"+
		"databases:\n  mongo1:\n    type: mongodb\n    database: app\n"+
		"    uri: \"mongodb://u@host:27017/\"\n"+
		"    mongo_secrets_file: \""+sfPath+"\"\n")

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
	if !strings.Contains(string(buf[:n]), "group- or world-readable") {
		t.Fatalf("expected a permission warning on stderr, got %q", string(buf[:n]))
	}
}
