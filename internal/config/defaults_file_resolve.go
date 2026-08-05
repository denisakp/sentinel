package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"

	"github.com/denisakp/sentinel/internal/adapters/mysqlargs"
)

// applyDefaultsFile parses job.DefaultsFile and fills any of Host/Username/
// Port left empty by explicit config, and caches a resolved password (read
// only via ResolveJobPassword) when password_env is unset (spec 056 / PRD
// 44). Called only for mysql/mariadb jobs with a non-empty DefaultsFile; any
// problem reading or parsing the file is a hard config-load error (FR-006) —
// except "no [client] section", which means the file simply has nothing to
// contribute (spec Edge Cases), not a failure.
func applyDefaultsFile(job *BackupJob, cfg *Configuration) error {
	data, mode, encrypted, err := readSecretsFileMaybeDecrypt(job.DefaultsFile, cfg)
	if err != nil {
		return fmt.Errorf("defaults_file: %w", err)
	}

	creds, err := mysqlargs.ParseDefaultsFileBytes(data)
	if err != nil {
		if errors.Is(err, mysqlargs.ErrDefaultsFileNoClientSection) {
			return nil
		}
		return fmt.Errorf("defaults_file '%s': %w", job.DefaultsFile, err)
	}

	// Permission warning applies only to plaintext files: an encrypted secrets
	// file being group/world-readable is ciphertext, not a credential exposure.
	if !encrypted && mode.Perm()&0o044 != 0 {
		fmt.Fprintf(os.Stderr,
			"warning: defaults_file '%s' has permissions 0o%03o (group- or world-readable); recommend chmod 0600\n",
			job.DefaultsFile, mode.Perm())
	}

	if job.Host == "" && creds.Host != "" {
		job.Host = creds.Host
	}
	if job.Username == "" && creds.User != "" {
		job.Username = creds.User
	}
	if job.PasswordEnv == "" && creds.Password != "" {
		job.myCnfPassword = creds.Password
	}
	if job.Port == 0 && creds.Port != "" {
		port, err := strconv.Atoi(creds.Port)
		if err != nil {
			return fmt.Errorf("defaults_file '%s': invalid port %q: %w", job.DefaultsFile, creds.Port, err)
		}
		job.Port = port
	}
	return nil
}
