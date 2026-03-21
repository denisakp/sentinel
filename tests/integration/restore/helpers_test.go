package integration_test

import (
	"os"
	"path/filepath"
	"testing"
)

func restoreProjectRoot(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	// tests/integration/restore -> project root is three levels up
	return filepath.Dir(filepath.Dir(filepath.Dir(cwd)))
}

func restoreFixturesDir(t *testing.T) string {
	t.Helper()
	return filepath.Join(restoreProjectRoot(t), "tests", "integration", "restore", "testdata")
}

func copyRestoreFixture(t *testing.T, name string) string {
	t.Helper()
	src := filepath.Join(restoreFixturesDir(t), name)
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("failed to read restore fixture %s: %v", name, err)
	}
	dst := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(dst, data, 0o600); err != nil {
		t.Fatalf("failed to copy restore fixture %s: %v", name, err)
	}
	return dst
}
