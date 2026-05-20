package tls_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	internaltls "github.com/denisakp/sentinel/internal/tls"
)

func TestSweepOrphanMaterial(t *testing.T) {
	dir := t.TempDir()

	livePid := os.Getpid()
	deadPid := 999999

	livePath := filepath.Join(dir, fmt.Sprintf("sentinel-mongo-tls-%d-abc.pem", livePid))
	deadPath := filepath.Join(dir, fmt.Sprintf("sentinel-mongo-tls-%d-xyz.pem", deadPid))
	unrelated := filepath.Join(dir, "unrelated.pem")

	for _, p := range []string{livePath, deadPath, unrelated} {
		if err := os.WriteFile(p, []byte("dummy"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	removed, err := internaltls.SweepOrphanMaterial(dir)
	if err != nil {
		t.Fatalf("sweep err: %v", err)
	}
	if removed != 1 {
		t.Errorf("removed = %d, want 1", removed)
	}
	if _, err := os.Stat(livePath); err != nil {
		t.Errorf("live file removed: %v", err)
	}
	if _, err := os.Stat(deadPath); !os.IsNotExist(err) {
		t.Errorf("dead-pid file not removed: stat err = %v", err)
	}
	if _, err := os.Stat(unrelated); err != nil {
		t.Errorf("unrelated file touched: %v", err)
	}
}

func TestSweepOrphanMaterial_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	removed, err := internaltls.SweepOrphanMaterial(dir)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 0 {
		t.Errorf("removed = %d, want 0", removed)
	}
}
