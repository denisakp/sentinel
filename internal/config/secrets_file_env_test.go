package config

import (
	"strings"
	"testing"
)

// T003 (US1): defaults_file_env only (no literal path) -> resolved and applied.
func TestLoadConfig_DefaultsFileEnvOnly(t *testing.T) {
	p := writeTestDefaultsFile(t, "[client]\nhost=127.0.0.1\nuser=sentinel\npassword=s3cr3t\n", 0o600)
	t.Setenv("MYCNF_PATH_ENV", p)

	cfgPath := writeTempConfig(t, "version: \"1.0\"\n"+
		"defaults:\n  storage:\n    type: local\n    local_path: ./backups\n\n"+
		"databases:\n  mydb:\n    type: mysql\n    database: app\n"+
		"    defaults_file_env: MYCNF_PATH_ENV\n")

	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	job := cfg.Databases["mydb"]
	if job.DefaultsFile != p {
		t.Fatalf("DefaultsFile = %q, want %q", job.DefaultsFile, p)
	}
	if job.Host != "127.0.0.1" || job.Username != "sentinel" {
		t.Fatalf("got Host=%q Username=%q, want file contents applied", job.Host, job.Username)
	}
	pw, err := ResolveJobPassword(job)
	if err != nil || pw != "s3cr3t" {
		t.Fatalf("password = %q err=%v", pw, err)
	}
}

// T004 (US1): mongo_secrets_file_env only (no literal path) -> resolved and applied.
func TestLoadConfig_MongoSecretsFileEnvOnly(t *testing.T) {
	p := writeTestMongoSecretsFile(t, "password: s3cr3t\n", 0o600)
	t.Setenv("MONGO_SECRETS_PATH_ENV", p)

	cfgPath := writeTempConfig(t, "version: \"1.0\"\n"+
		"defaults:\n  storage:\n    type: local\n    local_path: ./backups\n\n"+
		"databases:\n  mongo1:\n    type: mongodb\n    database: app\n"+
		"    uri: \"mongodb://appuser@host:27017/\"\n"+
		"    mongo_secrets_file_env: MONGO_SECRETS_PATH_ENV\n")

	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	job := cfg.Databases["mongo1"]
	if job.MongoSecretsFile != p {
		t.Fatalf("MongoSecretsFile = %q, want %q", job.MongoSecretsFile, p)
	}
	if job.URI != "mongodb://appuser:s3cr3t@host:27017/" {
		t.Fatalf("URI = %q, want composed password", job.URI)
	}
}

// T005 (US2): defaults_file_env names an unset variable -> fails fast.
func TestLoadConfig_DefaultsFileEnvUnsetVariable(t *testing.T) {
	cfgPath := writeTempConfig(t, "version: \"1.0\"\n"+
		"defaults:\n  storage:\n    type: local\n    local_path: ./backups\n\n"+
		"databases:\n  mydb:\n    type: mysql\n    database: app\n"+
		"    defaults_file_env: MYCNF_DEFINITELY_UNSET_VAR\n")

	_, err := LoadConfig(cfgPath)
	if err == nil || !strings.Contains(err.Error(), "MYCNF_DEFINITELY_UNSET_VAR") {
		t.Fatalf("got err=%v, want an error naming the unset variable", err)
	}
}

// T006 (US2): mongo_secrets_file_env names an unset variable -> fails fast.
func TestLoadConfig_MongoSecretsFileEnvUnsetVariable(t *testing.T) {
	cfgPath := writeTempConfig(t, "version: \"1.0\"\n"+
		"defaults:\n  storage:\n    type: local\n    local_path: ./backups\n\n"+
		"databases:\n  mongo1:\n    type: mongodb\n    database: app\n"+
		"    uri: \"mongodb://u@host:27017/\"\n"+
		"    mongo_secrets_file_env: MONGO_DEFINITELY_UNSET_VAR\n")

	_, err := LoadConfig(cfgPath)
	if err == nil || !strings.Contains(err.Error(), "MONGO_DEFINITELY_UNSET_VAR") {
		t.Fatalf("got err=%v, want an error naming the unset variable", err)
	}
}

// T007 (US3): literal defaults_file only (no _env) -> unchanged behavior.
func TestLoadConfig_DefaultsFileLiteralOnlyUnaffectedByEnvFeature(t *testing.T) {
	p := writeTestDefaultsFile(t, "[client]\nhost=127.0.0.1\nuser=sentinel\npassword=s3cr3t\n", 0o600)

	cfgPath := writeTempConfig(t, "version: \"1.0\"\n"+
		"defaults:\n  storage:\n    type: local\n    local_path: ./backups\n\n"+
		"databases:\n  mydb:\n    type: mysql\n    database: app\n"+
		"    defaults_file: \""+p+"\"\n")

	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	job := cfg.Databases["mydb"]
	if job.DefaultsFile != p || job.Host != "127.0.0.1" {
		t.Fatalf("got DefaultsFile=%q Host=%q", job.DefaultsFile, job.Host)
	}
}

// T008 (US3): literal mongo_secrets_file only (no _env) -> unchanged behavior.
func TestLoadConfig_MongoSecretsFileLiteralOnlyUnaffectedByEnvFeature(t *testing.T) {
	p := writeTestMongoSecretsFile(t, "password: s3cr3t\n", 0o600)

	cfgPath := writeTempConfig(t, "version: \"1.0\"\n"+
		"defaults:\n  storage:\n    type: local\n    local_path: ./backups\n\n"+
		"databases:\n  mongo1:\n    type: mongodb\n    database: app\n"+
		"    uri: \"mongodb://u@host:27017/\"\n"+
		"    mongo_secrets_file: \""+p+"\"\n")

	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	job := cfg.Databases["mongo1"]
	if job.MongoSecretsFile != p || job.URI != "mongodb://u:s3cr3t@host:27017/" {
		t.Fatalf("got MongoSecretsFile=%q URI=%q", job.MongoSecretsFile, job.URI)
	}
}

// T009 (US3): both literal and _env set -> _env wins (precedence proof), for both mechanisms.
func TestLoadConfig_SecretsFileEnvWinsOverLiteral(t *testing.T) {
	t.Run("defaults_file", func(t *testing.T) {
		literalPath := writeTestDefaultsFile(t, "[client]\nhost=wrong-host\nuser=wronguser\n", 0o600)
		envPath := writeTestDefaultsFile(t, "[client]\nhost=right-host\nuser=rightuser\npassword=s3cr3t\n", 0o600)
		t.Setenv("MYCNF_PRECEDENCE_VAR", envPath)

		cfgPath := writeTempConfig(t, "version: \"1.0\"\n"+
			"defaults:\n  storage:\n    type: local\n    local_path: ./backups\n\n"+
			"databases:\n  mydb:\n    type: mysql\n    database: app\n"+
			"    defaults_file: \""+literalPath+"\"\n"+
			"    defaults_file_env: MYCNF_PRECEDENCE_VAR\n")

		cfg, err := LoadConfig(cfgPath)
		if err != nil {
			t.Fatalf("LoadConfig() error = %v", err)
		}
		job := cfg.Databases["mydb"]
		if job.DefaultsFile != envPath {
			t.Fatalf("DefaultsFile = %q, want the env-resolved path %q (env must win)", job.DefaultsFile, envPath)
		}
		if job.Host != "right-host" {
			t.Fatalf("Host = %q, want right-host (from the env-resolved file)", job.Host)
		}
	})

	t.Run("mongo_secrets_file", func(t *testing.T) {
		literalPath := writeTestMongoSecretsFile(t, "password: wrongpass\n", 0o600)
		envPath := writeTestMongoSecretsFile(t, "password: s3cr3t\n", 0o600)
		t.Setenv("MONGO_SECRETS_PRECEDENCE_VAR", envPath)

		cfgPath := writeTempConfig(t, "version: \"1.0\"\n"+
			"defaults:\n  storage:\n    type: local\n    local_path: ./backups\n\n"+
			"databases:\n  mongo1:\n    type: mongodb\n    database: app\n"+
			"    uri: \"mongodb://u@host:27017/\"\n"+
			"    mongo_secrets_file: \""+literalPath+"\"\n"+
			"    mongo_secrets_file_env: MONGO_SECRETS_PRECEDENCE_VAR\n")

		cfg, err := LoadConfig(cfgPath)
		if err != nil {
			t.Fatalf("LoadConfig() error = %v", err)
		}
		job := cfg.Databases["mongo1"]
		if job.MongoSecretsFile != envPath {
			t.Fatalf("MongoSecretsFile = %q, want the env-resolved path %q (env must win)", job.MongoSecretsFile, envPath)
		}
		if job.URI != "mongodb://u:s3cr3t@host:27017/" {
			t.Fatalf("URI = %q, want composed from the env-resolved file's password", job.URI)
		}
	})
}

// T010 (US3): neither literal nor _env set for either mechanism -> completely unaffected.
func TestLoadConfig_NeitherSecretsFileFieldSetUnaffected(t *testing.T) {
	t.Setenv("MYSQL_PW_UNAFFECTED_TEST", "secret")
	cfgPath := writeTempConfig(t, "version: \"1.0\"\n"+
		"defaults:\n  storage:\n    type: local\n    local_path: ./backups\n\n"+
		"databases:\n  mydb:\n    type: mysql\n    database: app\n"+
		"    host: localhost\n    username: sentinel\n    password_env: MYSQL_PW_UNAFFECTED_TEST\n"+
		"  mongo1:\n    type: mongodb\n    database: app\n"+
		"    uri: \"mongodb://u:pw@host:27017/\"\n")

	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if cfg.Databases["mydb"].DefaultsFile != "" || cfg.Databases["mongo1"].MongoSecretsFile != "" {
		t.Fatalf("expected both secrets-file fields to remain empty")
	}
	if err := ValidateConfig(cfg); err != nil {
		t.Fatalf("ValidateConfig() error = %v", err)
	}
}
