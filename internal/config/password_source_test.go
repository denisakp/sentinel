package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writePwFile(t *testing.T, contents string, mode os.FileMode) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "pw")
	if err := os.WriteFile(p, []byte(contents), mode); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := os.Chmod(p, mode); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	return p
}

func TestPasswordFromFile(t *testing.T) {
	t.Run("first line trimmed CRLF", func(t *testing.T) {
		p := writePwFile(t, "hunter2\r\nignored\n", 0o600)
		got, _, err := PasswordFromFile(p)
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if got != "hunter2" {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("trailing tabs and spaces stripped", func(t *testing.T) {
		p := writePwFile(t, "secret \t\n", 0o600)
		got, _, err := PasswordFromFile(p)
		if err != nil || got != "secret" {
			t.Fatalf("got=%q err=%v", got, err)
		}
	})

	t.Run("preserves leading whitespace", func(t *testing.T) {
		p := writePwFile(t, "  spaced\n", 0o600)
		got, _, err := PasswordFromFile(p)
		if err != nil || got != "  spaced" {
			t.Fatalf("got=%q err=%v", got, err)
		}
	})

	t.Run("empty file is error", func(t *testing.T) {
		p := writePwFile(t, "", 0o600)
		_, _, err := PasswordFromFile(p)
		if !errors.Is(err, ErrFileEmpty) {
			t.Fatalf("want ErrFileEmpty, got %v", err)
		}
	})

	t.Run("blank-first-line is error", func(t *testing.T) {
		p := writePwFile(t, "   \nactual\n", 0o600)
		_, _, err := PasswordFromFile(p)
		if !errors.Is(err, ErrFileEmpty) {
			t.Fatalf("want ErrFileEmpty, got %v", err)
		}
	})

	t.Run("missing file is unreadable", func(t *testing.T) {
		_, _, err := PasswordFromFile(filepath.Join(t.TempDir(), "nope"))
		if err == nil || !strings.Contains(err.Error(), "cannot read password file") {
			t.Fatalf("want cannot read password file, got %v", err)
		}
	})

	t.Run("returns observed mode", func(t *testing.T) {
		p := writePwFile(t, "x\n", 0o644)
		_, mode, err := PasswordFromFile(p)
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if mode.Perm()&0o044 == 0 {
			t.Fatalf("expected world/group read bit observed in %o", mode.Perm())
		}
	})
}

func TestResolve(t *testing.T) {
	t.Run("V1 two flags conflict", func(t *testing.T) {
		_, err := Resolve(ResolveFlags{
			Password: "x", PasswordSet: true,
			PasswordEnv: "FOO", PasswordEnvSet: true,
		}, BackupJob{Type: "postgres"})
		if !errors.Is(err, ErrMultipleFlags) {
			t.Fatalf("want ErrMultipleFlags, got %v", err)
		}
		if !strings.Contains(err.Error(), "--password, --password-env") {
			t.Fatalf("error must name flags: %v", err)
		}
	})

	t.Run("V1 triple flag conflict", func(t *testing.T) {
		_, err := Resolve(ResolveFlags{
			Password: "x", PasswordSet: true,
			PasswordEnv: "FOO", PasswordEnvSet: true,
			PasswordFile: "/tmp/x", PasswordFileSet: true,
		}, BackupJob{Type: "postgres"})
		if !errors.Is(err, ErrMultipleFlags) {
			t.Fatalf("want ErrMultipleFlags")
		}
	})

	t.Run("V1 env+file conflict", func(t *testing.T) {
		_, err := Resolve(ResolveFlags{
			PasswordEnv: "FOO", PasswordEnvSet: true,
			PasswordFile: "/tmp/x", PasswordFileSet: true,
		}, BackupJob{Type: "postgres"})
		if !errors.Is(err, ErrMultipleFlags) {
			t.Fatalf("want ErrMultipleFlags")
		}
	})

	t.Run("V5 flag wins", func(t *testing.T) {
		res, err := Resolve(ResolveFlags{Password: "fromflag", PasswordSet: true},
			BackupJob{Type: "postgres", PasswordEnv: "IGNORED"})
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if res.Source != SourceFlag || res.Password != "fromflag" {
			t.Fatalf("got %+v", res)
		}
	})

	t.Run("V2 env unset", func(t *testing.T) {
		t.Setenv("DEFINITELY_UNSET_PASSWORD_VAR", "")
		_, err := Resolve(ResolveFlags{PasswordEnv: "DEFINITELY_UNSET_PASSWORD_VAR", PasswordEnvSet: true},
			BackupJob{Type: "postgres"})
		if !errors.Is(err, ErrEnvVarUnset) {
			t.Fatalf("want ErrEnvVarUnset, got %v", err)
		}
		if !strings.Contains(err.Error(), "DEFINITELY_UNSET_PASSWORD_VAR") {
			t.Fatalf("error must name var: %v", err)
		}
	})

	t.Run("V2 env set resolves", func(t *testing.T) {
		t.Setenv("PWSRC_TEST_VAR", "value42")
		res, err := Resolve(ResolveFlags{PasswordEnv: "PWSRC_TEST_VAR", PasswordEnvSet: true},
			BackupJob{Type: "postgres"})
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if res.Source != SourceEnvFlag || res.Password != "value42" {
			t.Fatalf("got %+v", res)
		}
	})

	t.Run("V3 file resolves with mode", func(t *testing.T) {
		p := writePwFile(t, "hunter2\n", 0o600)
		res, err := Resolve(ResolveFlags{PasswordFile: p, PasswordFileSet: true},
			BackupJob{Type: "postgres"})
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if res.Source != SourceFileFlag || res.Password != "hunter2" || res.FilePath != p {
			t.Fatalf("got %+v", res)
		}
		if res.FileMode.Perm() != 0o600 {
			t.Fatalf("mode = %o", res.FileMode.Perm())
		}
	})

	t.Run("V3 file empty propagates", func(t *testing.T) {
		p := writePwFile(t, "\n", 0o600)
		_, err := Resolve(ResolveFlags{PasswordFile: p, PasswordFileSet: true},
			BackupJob{Type: "postgres"})
		if !errors.Is(err, ErrFileEmpty) {
			t.Fatalf("want ErrFileEmpty, got %v", err)
		}
	})

	t.Run("V6 config env unset", func(t *testing.T) {
		t.Setenv("PWSRC_CONFIG_UNSET", "")
		_, err := Resolve(ResolveFlags{}, BackupJob{Type: "postgres", PasswordEnv: "PWSRC_CONFIG_UNSET"})
		if err == nil || !strings.Contains(err.Error(), "PWSRC_CONFIG_UNSET") {
			t.Fatalf("want env unset error, got %v", err)
		}
	})

	t.Run("V6 config env resolves", func(t *testing.T) {
		t.Setenv("PWSRC_CONFIG_SET", "cfgvalue")
		res, err := Resolve(ResolveFlags{}, BackupJob{Type: "postgres", PasswordEnv: "PWSRC_CONFIG_SET"})
		if err != nil || res.Source != SourceConfigEnv || res.Password != "cfgvalue" {
			t.Fatalf("got %+v err=%v", res, err)
		}
	})

	t.Run("V7 mongodb empty acceptable", func(t *testing.T) {
		res, err := Resolve(ResolveFlags{}, BackupJob{Type: "mongodb"})
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if res.Source != SourceNone || res.Password != "" {
			t.Fatalf("got %+v", res)
		}
	})

	t.Run("V7 non-mongodb missing source errors", func(t *testing.T) {
		_, err := Resolve(ResolveFlags{}, BackupJob{Type: "postgres"})
		if err == nil {
			t.Fatalf("want error for missing source on postgres")
		}
	})

	t.Run("CLI flag overrides config silently", func(t *testing.T) {
		t.Setenv("PWSRC_OVERRIDE_VAR", "overridden")
		res, err := Resolve(ResolveFlags{PasswordEnv: "PWSRC_OVERRIDE_VAR", PasswordEnvSet: true},
			BackupJob{Type: "postgres", PasswordEnv: "PWSRC_CONFIG_DEFAULT"})
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if res.Source != SourceEnvFlag || res.Password != "overridden" {
			t.Fatalf("got %+v", res)
		}
	})

	t.Run("defaults_file password used when no CLI flags and password_env unset", func(t *testing.T) {
		job := BackupJob{Type: "mysql", myCnfPassword: "fromfile"}
		res, err := Resolve(ResolveFlags{}, job)
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if res.Source != SourceConfigEnv || res.Password != "fromfile" {
			t.Fatalf("got %+v", res)
		}
	})
}

func TestResolveJobPassword(t *testing.T) {
	t.Run("password_env set takes precedence, unchanged behavior", func(t *testing.T) {
		t.Setenv("RJP_ENV_VAR", "envvalue")
		job := BackupJob{PasswordEnv: "RJP_ENV_VAR", myCnfPassword: "fromfile"}
		got, err := ResolveJobPassword(job)
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if got != "envvalue" {
			t.Fatalf("got %q, want envvalue (password_env must win)", got)
		}
	})

	t.Run("myCnfPassword used when password_env unset", func(t *testing.T) {
		job := BackupJob{myCnfPassword: "fromfile"}
		got, err := ResolveJobPassword(job)
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if got != "fromfile" {
			t.Fatalf("got %q, want fromfile", got)
		}
	})

	t.Run("neither set produces the same error as before this feature", func(t *testing.T) {
		job := BackupJob{}
		_, err := ResolveJobPassword(job)
		if err == nil || !strings.Contains(err.Error(), "password_env is required") {
			t.Fatalf("got err=%v, want 'password_env is required'", err)
		}
	})
}
