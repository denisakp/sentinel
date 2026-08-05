package mysqlargs

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func writeTempDefaultsFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, ".my.cnf")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write temp defaults file: %v", err)
	}
	return path
}

func TestParseDefaultsFile_AllFieldsUnquoted(t *testing.T) {
	path := writeTempDefaultsFile(t, "[client]\nhost=127.0.0.1\nuser=sentinel\npassword=s3cr3t\nport=3306\n")
	creds, _, err := ParseDefaultsFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := ClientCreds{Host: "127.0.0.1", User: "sentinel", Password: "s3cr3t", Port: "3306"}
	if creds != want {
		t.Fatalf("got %+v, want %+v", creds, want)
	}
}

func TestParseDefaultsFile_QuotedValues(t *testing.T) {
	path := writeTempDefaultsFile(t, "[client]\nhost = \"127.0.0.1\"\nuser = 'sentinel'\npassword = \"s3c#r3t\"\n")
	creds, _, err := ParseDefaultsFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if creds.Host != "127.0.0.1" || creds.User != "sentinel" || creds.Password != "s3c#r3t" {
		t.Fatalf("got %+v", creds)
	}
}

func TestParseDefaultsFile_CommentsInterspersed(t *testing.T) {
	path := writeTempDefaultsFile(t, "# leading comment\n; another style\n[client]\n# inline-ish comment line\nhost=127.0.0.1\n; separator\nuser=sentinel\n")
	creds, _, err := ParseDefaultsFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if creds.Host != "127.0.0.1" || creds.User != "sentinel" {
		t.Fatalf("got %+v", creds)
	}
}

func TestParseDefaultsFile_NoClientSection(t *testing.T) {
	path := writeTempDefaultsFile(t, "[mysqldump]\nquick\n")
	_, _, err := ParseDefaultsFile(path)
	if !errors.Is(err, ErrDefaultsFileNoClientSection) {
		t.Fatalf("got err=%v, want ErrDefaultsFileNoClientSection", err)
	}
}

func TestParseDefaultsFile_EmptyClientSection(t *testing.T) {
	path := writeTempDefaultsFile(t, "[client]\n[mysqldump]\nquick\n")
	_, _, err := ParseDefaultsFile(path)
	if !errors.Is(err, ErrDefaultsFileNoClientSection) {
		t.Fatalf("got err=%v, want ErrDefaultsFileNoClientSection", err)
	}
}

func TestParseDefaultsFile_MultipleSectionsOnlyClientExtracted(t *testing.T) {
	path := writeTempDefaultsFile(t, "[mysqldump]\nquick\nmax_allowed_packet=512M\n[client]\nhost=127.0.0.1\nuser=sentinel\n[mysql]\nprompt=x\n")
	creds, _, err := ParseDefaultsFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if creds.Host != "127.0.0.1" || creds.User != "sentinel" {
		t.Fatalf("got %+v (expected only [client] values)", creds)
	}
}

func TestParseDefaultsFile_MissingFile(t *testing.T) {
	_, _, err := ParseDefaultsFile(filepath.Join(t.TempDir(), "nonexistent.my.cnf"))
	if !errors.Is(err, ErrDefaultsFileUnreadable) {
		t.Fatalf("got err=%v, want ErrDefaultsFileUnreadable", err)
	}
}

func TestParseDefaultsFile_UnreadableFile(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root; permission checks are ineffective")
	}
	path := writeTempDefaultsFile(t, "[client]\nhost=127.0.0.1\n")
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	defer os.Chmod(path, 0o600)
	_, _, err := ParseDefaultsFile(path)
	if !errors.Is(err, ErrDefaultsFileUnreadable) {
		t.Fatalf("got err=%v, want ErrDefaultsFileUnreadable", err)
	}
}

func TestParseDefaultsFile_PermissionModeReturned(t *testing.T) {
	path := writeTempDefaultsFile(t, "[client]\nhost=127.0.0.1\nuser=sentinel\n")
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	_, mode, err := ParseDefaultsFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mode.Perm()&0o044 == 0 {
		t.Fatalf("expected group/world-readable bits set, got mode %v", mode.Perm())
	}
}

func TestParseDefaultsFile_MalformedSectionHeader(t *testing.T) {
	path := writeTempDefaultsFile(t, "[client\nhost=127.0.0.1\n")
	_, _, err := ParseDefaultsFile(path)
	if !errors.Is(err, ErrDefaultsFileUnreadable) {
		t.Fatalf("got err=%v, want ErrDefaultsFileUnreadable (malformed section)", err)
	}
}
