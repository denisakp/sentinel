package integration_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
)

var (
	buildOnce sync.Once
	binPath   string
	buildErr  error
)

func buildSentinel(t *testing.T) string {
	t.Helper()
	buildOnce.Do(func() {
		tmpDir, err := os.MkdirTemp("", "sentinel-bin-*")
		if err != nil {
			buildErr = err
			return
		}
		binPath = filepath.Join(tmpDir, "sentinel")

		cmd := exec.Command("go", "build", "-o", binPath, "./")
		cmd.Env = os.Environ()
		cmd.Dir = projectRoot(t)
		buildErr = cmd.Run()
	})

	if buildErr != nil {
		t.Fatalf("failed to build sentinel: %v", buildErr)
	}

	return binPath
}

func projectRoot(t *testing.T) string {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	// tests/integration -> project root is two levels up
	return filepath.Dir(filepath.Dir(cwd))
}
