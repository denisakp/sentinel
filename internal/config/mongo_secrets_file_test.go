package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTestMongoSecretsFile(t *testing.T, content string, mode os.FileMode) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "mongo-secrets.yaml")
	if err := os.WriteFile(p, []byte(content), mode); err != nil {
		t.Fatalf("write secrets file: %v", err)
	}
	if err := os.Chmod(p, mode); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	return p
}

func TestReadMongoSecretsFile(t *testing.T) {
	t.Run("all three fields", func(t *testing.T) {
		p := writeTestMongoSecretsFile(t, "uri: mongodb://u@h/\npassword: pw\nssl_pem_key_password: pempass\n", 0o600)
		s, _, err := readMongoSecretsFile(p)
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if s.URI != "mongodb://u@h/" || s.Password != "pw" || s.SSLPEMKeyPassword != "pempass" {
			t.Fatalf("got %+v", s)
		}
	})

	t.Run("password only", func(t *testing.T) {
		p := writeTestMongoSecretsFile(t, "password: pw\n", 0o600)
		s, _, err := readMongoSecretsFile(p)
		if err != nil || s.Password != "pw" || s.URI != "" || s.SSLPEMKeyPassword != "" {
			t.Fatalf("got %+v err=%v", s, err)
		}
	})

	t.Run("missing file", func(t *testing.T) {
		_, _, err := readMongoSecretsFile(filepath.Join(t.TempDir(), "nope.yaml"))
		if err == nil || !strings.Contains(err.Error(), "cannot read mongo secrets file") {
			t.Fatalf("got err=%v", err)
		}
	})

	t.Run("malformed yaml", func(t *testing.T) {
		p := writeTestMongoSecretsFile(t, "uri: [unterminated\n", 0o600)
		_, _, err := readMongoSecretsFile(p)
		if err == nil || !strings.Contains(err.Error(), "cannot parse mongo secrets file") {
			t.Fatalf("got err=%v", err)
		}
	})

	t.Run("permission mode returned", func(t *testing.T) {
		p := writeTestMongoSecretsFile(t, "password: pw\n", 0o644)
		_, mode, err := readMongoSecretsFile(p)
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if mode.Perm()&0o044 == 0 {
			t.Fatalf("expected group/world-readable bits, got %o", mode.Perm())
		}
	})
}

func TestApplyMongoSecrets(t *testing.T) {
	t.Run("password fills a username-bearing URI with no password", func(t *testing.T) {
		p := writeTestMongoSecretsFile(t, "password: s3cr3t\n", 0o600)
		job := BackupJob{Name: "mongo1", Type: "mongodb", URI: "mongodb://appuser@host:27017/", MongoSecretsFile: p}
		if err := applyMongoSecrets(&job); err != nil {
			t.Fatalf("err: %v", err)
		}
		if job.URI != "mongodb://appuser:s3cr3t@host:27017/" {
			t.Fatalf("got URI=%q", job.URI)
		}
	})

	t.Run("full uri used when job.URI empty", func(t *testing.T) {
		p := writeTestMongoSecretsFile(t, "uri: mongodb://appuser:s3cr3t@host:27017/\n", 0o600)
		job := BackupJob{Name: "mongo1", Type: "mongodb", MongoSecretsFile: p}
		if err := applyMongoSecrets(&job); err != nil {
			t.Fatalf("err: %v", err)
		}
		if job.URI != "mongodb://appuser:s3cr3t@host:27017/" {
			t.Fatalf("got URI=%q", job.URI)
		}
	})

	t.Run("ssl_pem_key_password only", func(t *testing.T) {
		p := writeTestMongoSecretsFile(t, "ssl_pem_key_password: pempass\n", 0o600)
		tls := &TLSConfig{}
		job := BackupJob{Name: "mongo1", Type: "mongodb", URI: "mongodb://u@h/", MongoSecretsFile: p, TLS: tls}
		if err := applyMongoSecrets(&job); err != nil {
			t.Fatalf("err: %v", err)
		}
		got, err := ResolveMongoTLSPassphrase(job.TLS)
		if err != nil || got != "pempass" {
			t.Fatalf("got=%q err=%v", got, err)
		}
	})

	t.Run("conflict: uri already has a password", func(t *testing.T) {
		p := writeTestMongoSecretsFile(t, "password: fromfile\n", 0o600)
		job := BackupJob{Name: "mongo1", Type: "mongodb", URI: "mongodb://u:existing@h/", MongoSecretsFile: p}
		err := applyMongoSecrets(&job)
		if err == nil || !strings.Contains(err.Error(), "conflicting password sources") {
			t.Fatalf("got err=%v", err)
		}
	})

	t.Run("missing username: password with no uri at all", func(t *testing.T) {
		p := writeTestMongoSecretsFile(t, "password: fromfile\n", 0o600)
		job := BackupJob{Name: "mongo1", Type: "mongodb", MongoSecretsFile: p}
		err := applyMongoSecrets(&job)
		if err == nil || !strings.Contains(err.Error(), "no username is known") {
			t.Fatalf("got err=%v", err)
		}
	})

	t.Run("uri unused when job.URI already set (no error)", func(t *testing.T) {
		p := writeTestMongoSecretsFile(t, "uri: mongodb://other@h2/\n", 0o600)
		job := BackupJob{Name: "mongo1", Type: "mongodb", URI: "mongodb://u:existing@h/", MongoSecretsFile: p}
		if err := applyMongoSecrets(&job); err != nil {
			t.Fatalf("err: %v", err)
		}
		if job.URI != "mongodb://u:existing@h/" {
			t.Fatalf("got URI=%q, want unchanged", job.URI)
		}
	})

	t.Run("permission warning does not block", func(t *testing.T) {
		p := writeTestMongoSecretsFile(t, "password: s3cr3t\n", 0o644)
		job := BackupJob{Name: "mongo1", Type: "mongodb", URI: "mongodb://u@h/", MongoSecretsFile: p}

		oldStderr := os.Stderr
		r, w, _ := os.Pipe()
		os.Stderr = w
		err := applyMongoSecrets(&job)
		w.Close()
		os.Stderr = oldStderr
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		buf := make([]byte, 4096)
		n, _ := r.Read(buf)
		if !strings.Contains(string(buf[:n]), "group- or world-readable") {
			t.Fatalf("expected permission warning, got %q", string(buf[:n]))
		}
	})
}

func TestResolveMongoTLSPassphrase(t *testing.T) {
	t.Run("nil TLS returns empty, no error", func(t *testing.T) {
		got, err := ResolveMongoTLSPassphrase(nil)
		if err != nil || got != "" {
			t.Fatalf("got=%q err=%v", got, err)
		}
	})

	t.Run("ClientKeyPasswordEnv wins over file-cached value", func(t *testing.T) {
		t.Setenv("MONGO_PEM_TEST_VAR", "fromenv")
		tls := &TLSConfig{ClientKeyPasswordEnv: "MONGO_PEM_TEST_VAR"}
		p := writeTestMongoSecretsFile(t, "ssl_pem_key_password: fromfile\n", 0o600)
		job := BackupJob{Name: "j", Type: "mongodb", URI: "mongodb://u@h/", MongoSecretsFile: p, TLS: tls}
		if err := applyMongoSecrets(&job); err != nil {
			t.Fatalf("err: %v", err)
		}
		got, err := ResolveMongoTLSPassphrase(job.TLS)
		if err != nil || got != "fromenv" {
			t.Fatalf("got=%q err=%v, want fromenv (env must win)", got, err)
		}
	})

	t.Run("ClientKeyPasswordEnv set but env var empty errors", func(t *testing.T) {
		tls := &TLSConfig{ClientKeyPasswordEnv: "MONGO_PEM_DEFINITELY_UNSET"}
		_, err := ResolveMongoTLSPassphrase(tls)
		if err == nil || !strings.Contains(err.Error(), "MONGO_PEM_DEFINITELY_UNSET") {
			t.Fatalf("got err=%v", err)
		}
	})

	t.Run("neither set returns empty, no error", func(t *testing.T) {
		got, err := ResolveMongoTLSPassphrase(&TLSConfig{})
		if err != nil || got != "" {
			t.Fatalf("got=%q err=%v", got, err)
		}
	})
}
