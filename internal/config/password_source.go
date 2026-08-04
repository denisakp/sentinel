package config

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"
)

// PasswordSource identifies the channel a backup invocation used to acquire
// the database password.
type PasswordSource int

const (
	// SourceNone indicates no password channel resolved a value. For engines
	// where an empty password is valid (e.g. mongodb with embedded URI credentials)
	// the caller may accept this; otherwise it is an error.
	SourceNone PasswordSource = iota
	// SourceFlag corresponds to the deprecated --password flag.
	SourceFlag
	// SourceEnvFlag corresponds to --password-env VAR.
	SourceEnvFlag
	// SourceFileFlag corresponds to --password-file PATH.
	SourceFileFlag
	// SourceConfigEnv corresponds to databases.<id>.password_env in YAML config.
	SourceConfigEnv
)

// Resolution is the value object returned by Resolve. Password is never logged
// or serialized. FilePath and FileMode are populated only when Source is
// SourceFileFlag so callers can emit the permission warning.
type Resolution struct {
	Password string
	Source   PasswordSource
	FilePath string
	FileMode os.FileMode
}

// ResolveFlags carries the CLI flag state in a Cobra-independent shape so that
// Resolve is unit-testable in isolation.
type ResolveFlags struct {
	Password        string
	PasswordEnv     string
	PasswordFile    string
	PasswordSet     bool
	PasswordEnvSet  bool
	PasswordFileSet bool
}

// Sentinel errors for the Resolve validation rules. Callers wrap these with
// context; tests use errors.Is to assert which rule fired.
var (
	ErrMultipleFlags  = errors.New("multiple password sources supplied on the command line")
	ErrEnvVarUnset    = errors.New("environment variable referenced by --password-env is unset or empty")
	ErrFileUnreadable = errors.New("cannot read password file")
	ErrFileEmpty      = errors.New("password file is empty after trimming the first line")
)

// PasswordFromFile reads the first line of path, applies right-trim of
// " \t\r\n", and returns the result together with the file's stat mode.
// Returns ErrFileEmpty when the trimmed first line is empty.
func PasswordFromFile(path string) (string, os.FileMode, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, fmt.Errorf("cannot read password file '%s': %w", path, err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return "", 0, fmt.Errorf("cannot read password file '%s': %w", path, err)
	}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	if !sc.Scan() {
		if scanErr := sc.Err(); scanErr != nil {
			return "", info.Mode(), fmt.Errorf("cannot read password file '%s': %w", path, scanErr)
		}
		return "", info.Mode(), fmt.Errorf("password file '%s' is empty after trimming the first line: %w", path, ErrFileEmpty)
	}
	line := strings.TrimRight(sc.Text(), " \t\r\n")
	if line == "" {
		return "", info.Mode(), fmt.Errorf("password file '%s' is empty after trimming the first line: %w", path, ErrFileEmpty)
	}
	return line, info.Mode(), nil
}

// Resolve enforces validation rules V1–V7 from
// specs/015-remove-password-flag/data-model.md and returns the chosen
// channel's resolved password.
func Resolve(flags ResolveFlags, job BackupJob) (Resolution, error) {
	supplied := suppliedFlagNames(flags)
	if len(supplied) >= 2 {
		return Resolution{}, fmt.Errorf("multiple password sources supplied on the command line (%s); use exactly one of --password, --password-env, --password-file: %w", strings.Join(supplied, ", "), ErrMultipleFlags)
	}

	switch {
	case flags.PasswordSet:
		return Resolution{Password: flags.Password, Source: SourceFlag}, nil
	case flags.PasswordEnvSet:
		if flags.PasswordEnv == "" {
			return Resolution{}, fmt.Errorf("environment variable '' referenced by --password-env is unset or empty: %w", ErrEnvVarUnset)
		}
		v := os.Getenv(flags.PasswordEnv)
		if v == "" {
			return Resolution{}, fmt.Errorf("environment variable '%s' referenced by --password-env is unset or empty: %w", flags.PasswordEnv, ErrEnvVarUnset)
		}
		return Resolution{Password: v, Source: SourceEnvFlag}, nil
	case flags.PasswordFileSet:
		pw, mode, err := PasswordFromFile(flags.PasswordFile)
		if err != nil {
			return Resolution{}, err
		}
		return Resolution{Password: pw, Source: SourceFileFlag, FilePath: flags.PasswordFile, FileMode: mode}, nil
	}

	if job.Type == "mongodb" && job.PasswordEnv == "" {
		return Resolution{Source: SourceNone}, nil
	}

	pw, err := ResolveJobPassword(job)
	if err != nil {
		return Resolution{}, err
	}
	return Resolution{Password: pw, Source: SourceConfigEnv}, nil
}

// ResolveJobPassword resolves a backup job's password from password_env,
// falling back to the password parsed from defaults_file (spec 056 / PRD 44)
// when password_env is unset. Both existing independent password-resolution
// call sites (Resolve above, and internal/cli/backup.go's listDatabases)
// converge on this single function so my.cnf-sourced credentials are visible
// consistently across the dump build, its in-dump connectivity check, and
// discovery — without either consumer needing its own edit.
func ResolveJobPassword(job BackupJob) (string, error) {
	if job.PasswordEnv != "" {
		return PasswordFromEnv(job.PasswordEnv)
	}
	if job.myCnfPassword != "" {
		return job.myCnfPassword, nil
	}
	// Same error shape PasswordFromEnv("") would have produced — preserves
	// today's exact "nothing supplied" error when defaults_file is unset.
	return "", fmt.Errorf("password_env is required")
}

func suppliedFlagNames(f ResolveFlags) []string {
	out := make([]string, 0, 3)
	if f.PasswordSet {
		out = append(out, "--password")
	}
	if f.PasswordEnvSet {
		out = append(out, "--password-env")
	}
	if f.PasswordFileSet {
		out = append(out, "--password-file")
	}
	return out
}
