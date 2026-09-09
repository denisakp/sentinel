package config_census

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// VerifyAnchor confirms that the anchor's file exists and contains its
// expression literally.
//
// This is the half of the record that cannot rot quietly. Deleting or renaming
// the code that reads a configuration key breaks the anchor and fails the check,
// which is the one direction that matters: a key silently becoming inert is
// exactly the defect this census exists to catch, and it would otherwise look
// identical to a key that still works.
func VerifyAnchor(repoRoot string, a *Anchor) error {
	if a == nil {
		return fmt.Errorf("no anchor")
	}
	full := filepath.Join(repoRoot, a.File)
	raw, err := os.ReadFile(full)
	if err != nil {
		return fmt.Errorf("evidence file %s cannot be read: %w", a.File, err)
	}
	if !strings.Contains(string(raw), a.Expression) {
		return fmt.Errorf(
			"evidence file %s no longer contains %q; the code that read this key was renamed or "+
				"removed, so either update the anchor or reclassify the key as inert",
			a.File, a.Expression)
	}
	return nil
}

// repoRoot walks up from the test's working directory to the module root.
func repoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found above %s", dir)
		}
		dir = parent
	}
}

// fmtSprintf is a thin indirection so test fixtures can format templates without
// each file importing fmt for one call.
func fmtSprintf(format string, a ...any) string { return fmt.Sprintf(format, a...) }
