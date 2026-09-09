package config

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestDefaultLockDirIsWritableUnprivileged is the regression guard for #154.
//
// The default was /var/run/sentinel unconditionally. /var/run is root-owned, so
// the first lock acquisition by any unprivileged user failed on mkdir and took
// the operation down with it. A safety mechanism that only functions for root
// does not function.
//
// The assertion is the one that matters: the resolved directory can actually be
// created by the user running the test. Checking the string against an expected
// path would pass on a machine where that path is unusable, which is exactly the
// mistake being corrected.
func TestDefaultLockDirIsWritableUnprivileged(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root, where /var/run/sentinel is correct and creatable")
	}

	// Point HOME and XDG_RUNTIME_DIR at scratch space so the test neither reads
	// nor writes the real ones.
	t.Setenv("XDG_RUNTIME_DIR", "")
	t.Setenv("HOME", t.TempDir())

	dir := DefaultLockDir()
	if dir == systemLockDir {
		t.Fatalf("DefaultLockDir() = %q for an unprivileged user; that path cannot be created", dir)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("DefaultLockDir() = %q, which this user cannot create: %v", dir, err)
	}
}

func TestDefaultLockDirPrefersRuntimeDir(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root takes the system path before any environment is consulted")
	}
	runtimeDir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", runtimeDir)
	t.Setenv("HOME", t.TempDir())

	if got, want := DefaultLockDir(), filepath.Join(runtimeDir, "sentinel"); got != want {
		t.Errorf("DefaultLockDir() = %q, want %q", got, want)
	}
}

func TestDefaultLockDirFallsBackThroughHomeThenTemp(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root takes the system path before any environment is consulted")
	}
	home := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", "")
	t.Setenv("HOME", home)
	if got, want := DefaultLockDir(), filepath.Join(home, ".local", "state", "sentinel", "locks"); got != want {
		t.Errorf("with HOME set, DefaultLockDir() = %q, want %q", got, want)
	}

	// Neither set: a stripped container. The uid suffix keeps two users on one
	// host from colliding in a world-writable temporary directory.
	t.Setenv("HOME", "")
	got := DefaultLockDir()
	if !strings.HasSuffix(got, "sentinel-"+strconv.Itoa(os.Geteuid())) {
		t.Errorf("with neither XDG_RUNTIME_DIR nor HOME, DefaultLockDir() = %q, want a uid-suffixed temp path", got)
	}
}

// TestLoadConfigDoesNotDefaultToAnUnusableLockDir pins the wiring, so a future
// edit cannot reintroduce the unusable constant at the point it is applied.
func TestLoadConfigDoesNotDefaultToAnUnusableLockDir(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root")
	}
	t.Setenv("XDG_RUNTIME_DIR", "")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PROBE_PW", "x")

	dir := t.TempDir()
	path := filepath.Join(dir, "sentinel.yaml")
	body := `version: "1"
defaults:
  output_dir: ` + dir + `
databases:
  probe:
    db_type: postgres
    host: localhost
    port: 5432
    db_name: probe
    user: probe
    password_env: PROBE_PW
    enabled: true
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if cfg.Scheduler.LockDir == systemLockDir {
		t.Fatalf("LoadConfig defaulted lock_dir to %q, which an unprivileged user cannot create", cfg.Scheduler.LockDir)
	}
	if err := os.MkdirAll(cfg.Scheduler.LockDir, 0o755); err != nil {
		t.Fatalf("defaulted lock_dir %q is not creatable: %v", cfg.Scheduler.LockDir, err)
	}
}

// TestExplicitLockDirIsNeverOverridden: the default only decides what happens
// when nobody said. An operator who names a directory gets that directory.
func TestExplicitLockDirIsNeverOverridden(t *testing.T) {
	t.Setenv("PROBE_PW", "x")
	dir := t.TempDir()
	want := filepath.Join(dir, "explicit-locks")
	path := filepath.Join(dir, "sentinel.yaml")
	body := `version: "1"
scheduler:
  lock_dir: ` + want + `
defaults:
  output_dir: ` + dir + `
databases:
  probe:
    db_type: postgres
    host: localhost
    port: 5432
    db_name: probe
    user: probe
    password_env: PROBE_PW
    enabled: true
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if cfg.Scheduler.LockDir != want {
		t.Errorf("lock_dir = %q, want %q", cfg.Scheduler.LockDir, want)
	}
}
