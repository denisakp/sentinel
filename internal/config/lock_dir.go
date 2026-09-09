package config

import (
	"os"
	"path/filepath"
	"strconv"
)

// systemLockDir is the right home for lock files when Sentinel runs as a
// system daemon: one well-known place, shared by every invocation on the host.
const systemLockDir = "/var/run/sentinel"

// DefaultLockDir returns the directory for job lock files when the
// configuration does not name one.
//
// This used to be systemLockDir unconditionally, which no unprivileged user can
// create: /var/run is root-owned, so the first lock acquisition failed with a
// permission error and the operation failed with it (#154). Locking is a safety
// mechanism, and one that only works for root is one that does not work.
//
// The rule is deliberate rather than a probe. Probing whether a directory is
// creatable has to create it, which is a side effect at configuration-load time,
// and it makes the resolved path depend on the order in which things ran. A
// caller can always be explicit with scheduler.lock_dir; this only decides what
// happens when nobody said.
//
//	root                 -> /var/run/sentinel, unchanged, the daemon case
//	XDG_RUNTIME_DIR set  -> $XDG_RUNTIME_DIR/sentinel, per-user and mode 0700 by
//	                        specification, which is what a lock file wants
//	HOME set             -> $HOME/.local/state/sentinel/locks, per the XDG base
//	                        directory specification for state that persists
//	otherwise            -> a uid-suffixed directory under the temporary dir
//
// The last case is the weakest and is a fallback, not a choice: a shared
// temporary directory is world-writable, so the uid suffix keeps two users on
// one host from colliding. It is reached only when neither XDG_RUNTIME_DIR nor
// HOME is set, which in practice means a stripped container environment.
//
// Locks in a per-user directory serialize that user's own invocations. Two
// different users backing up the same job to the same target will not see each
// other's locks, which is a real limitation: say so with scheduler.lock_dir if
// that is the deployment.
func DefaultLockDir() string {
	if os.Geteuid() == 0 {
		return systemLockDir
	}
	if runtimeDir := os.Getenv("XDG_RUNTIME_DIR"); runtimeDir != "" {
		return filepath.Join(runtimeDir, "sentinel")
	}
	if home := os.Getenv("HOME"); home != "" {
		return filepath.Join(home, ".local", "state", "sentinel", "locks")
	}
	return filepath.Join(os.TempDir(), "sentinel-"+strconv.Itoa(os.Geteuid()))
}
