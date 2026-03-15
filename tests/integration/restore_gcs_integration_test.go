package integration_test

import (
	"os/exec"
	"strings"
	"testing"
)

func TestRestoreRunHelpIncludesGCSAndKeepFileFlags(t *testing.T) {
	bin := buildSentinel(t)
	cmd := exec.Command(bin, "restore", "run", "-h")
	cmd.Dir = projectRoot(t)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("restore run -h failed: %v\n%s", err, string(out))
	}

	help := string(out)
	if !strings.Contains(help, "--gcs-bucket") {
		t.Fatalf("restore run help missing --gcs-bucket flag:\n%s", help)
	}
	if !strings.Contains(help, "--gcs-credentials-file") {
		t.Fatalf("restore run help missing --gcs-credentials-file flag:\n%s", help)
	}
	if !strings.Contains(help, "--keep-file") {
		t.Fatalf("restore run help missing --keep-file flag:\n%s", help)
	}
}
