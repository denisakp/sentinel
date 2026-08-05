package config

import (
	"fmt"
	"net/url"
	"os"

	"gopkg.in/yaml.v3"
)

// mongoSecrets is the Sentinel-native secrets-file shape. All fields are
// optional; any subset may be present (spec 057 / PRD 45).
type mongoSecrets struct {
	URI               string `yaml:"uri,omitempty"`
	Password          string `yaml:"password,omitempty"`
	SSLPEMKeyPassword string `yaml:"ssl_pem_key_password,omitempty"`
}

// readMongoSecretsFile reads and parses path. Any problem opening, stat'ing,
// or unmarshaling it is a hard error — never a silent skip (FR-008).
func readMongoSecretsFile(path string) (mongoSecrets, os.FileMode, error) {
	f, err := os.Open(path)
	if err != nil {
		return mongoSecrets{}, 0, fmt.Errorf("cannot read mongo secrets file '%s': %w", path, err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return mongoSecrets{}, 0, fmt.Errorf("cannot read mongo secrets file '%s': %w", path, err)
	}

	var secrets mongoSecrets
	dec := yaml.NewDecoder(f)
	if err := dec.Decode(&secrets); err != nil {
		return mongoSecrets{}, info.Mode(), fmt.Errorf("cannot parse mongo secrets file '%s': %w", path, err)
	}
	return secrets, info.Mode(), nil
}

// applyMongoSecrets reads job.MongoSecretsFile and composes its values into
// job.URI (fallback-only, per-field precedence — contracts/uri-composition.md)
// and job.TLS.mongoPEMPassphrase. Called only for mongodb jobs with a
// non-empty MongoSecretsFile.
func applyMongoSecrets(job *BackupJob) error {
	secrets, mode, err := readMongoSecretsFile(job.MongoSecretsFile)
	if err != nil {
		return err
	}

	if mode.Perm()&0o044 != 0 {
		fmt.Fprintf(os.Stderr,
			"warning: mongo_secrets_file '%s' has permissions 0o%03o (group- or world-readable); recommend chmod 0600\n",
			job.MongoSecretsFile, mode.Perm())
	}

	// `uri` is fallback-only: used only if job.URI is completely empty.
	// If job.URI is already set, the file's `uri` is silently unused
	// (FR-004, US2 AS-3) — not an error.
	if job.URI == "" && secrets.URI != "" {
		if _, err := url.Parse(secrets.URI); err != nil {
			return fmt.Errorf("mongo_secrets_file '%s': invalid uri: %w", job.MongoSecretsFile, err)
		}
		job.URI = secrets.URI
	}

	if secrets.Password != "" {
		if err := composeMongoPassword(job, secrets.Password); err != nil {
			return err
		}
	}

	if secrets.SSLPEMKeyPassword != "" && job.TLS != nil && job.TLS.ClientKeyPasswordEnv == "" {
		job.TLS.mongoPEMPassphrase = secrets.SSLPEMKeyPassword
	}

	return nil
}

// composeMongoPassword injects password into job.URI's userinfo, honoring
// the conflict/missing-username rules from contracts/uri-composition.md
// (FR-005, FR-006).
func composeMongoPassword(job *BackupJob, password string) error {
	if job.URI == "" {
		return fmt.Errorf("mongo_secrets_file '%s': password supplied but no username is known for job '%s' (no uri/uri_env configured)", job.MongoSecretsFile, job.Name)
	}
	u, err := url.Parse(job.URI)
	if err != nil {
		return fmt.Errorf("mongo_secrets_file '%s': job '%s' has an unparseable uri: %w", job.MongoSecretsFile, job.Name, err)
	}

	hasUsername := u.User != nil && u.User.Username() != ""
	_, hasPassword := u.User.Password()

	if hasPassword {
		return fmt.Errorf("mongo_secrets_file '%s': job '%s' already has a password in its uri; conflicting password sources", job.MongoSecretsFile, job.Name)
	}
	if !hasUsername {
		return fmt.Errorf("mongo_secrets_file '%s': password supplied but no username is known for job '%s'", job.MongoSecretsFile, job.Name)
	}

	u.User = url.UserPassword(u.User.Username(), password)
	job.URI = u.String()
	return nil
}

// ResolveMongoTLSPassphrase resolves a mongodb job's TLS private-key
// passphrase from ClientKeyPasswordEnv, falling back to the passphrase
// parsed from MongoSecretsFile (spec 057 / PRD 45) when the env var is
// unset. tls may be nil (no TLS configured) — returns ("", nil) in that
// case, matching the existing "nothing configured" behavior.
//
// NOTE: there is currently no live caller that wires a resolved passphrase
// into a real MongoDB TLS connection (job.TLS never reaches
// mongo_tls.PrepareMongoTLS on the config-driven backup path today — a
// pre-existing, separate gap, out of scope here per spec 057's
// Clarifications). This function makes the value resolvable through an
// equivalent path to ClientKeyPasswordEnv; it does not by itself make the
// passphrase reach a live connection.
func ResolveMongoTLSPassphrase(tls *TLSConfig) (string, error) {
	if tls == nil {
		return "", nil
	}
	if tls.ClientKeyPasswordEnv != "" {
		v := os.Getenv(tls.ClientKeyPasswordEnv)
		if v == "" {
			return "", fmt.Errorf("passphrase env var %q is unset or empty (referenced by tls.client_key_password_env)", tls.ClientKeyPasswordEnv)
		}
		return v, nil
	}
	return tls.mongoPEMPassphrase, nil
}
