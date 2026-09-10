package backup

import (
	"os"
	"path/filepath"
	"testing"
)

// TestResolveArtifactRefFallsBackToTheProducedPath is the regression guard for
// the execution-history half of #151.
//
// Without an explicit `output:`, job.OutName is empty because the dump adapter
// chose the filename itself. ResolveArtifactRef recomputed the path from OutName,
// got nothing, and recorded the artifact as "unknown", so the execution history
// pointed at no file at all.
//
// The adapter reports its real path on every engine, so the information was
// available the whole time; it simply was not consulted.
func TestResolveArtifactRefFallsBackToTheProducedPath(t *testing.T) {
	dir := t.TempDir()
	produced := filepath.Join(dir, "SENTINEL_2026-01-02T15-04-05.sql")
	if err := os.WriteFile(produced, []byte("dump bytes"), 0o600); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}

	path, size := ResolveArtifactRef("local", dir, "", "", produced)
	if path == "unknown" {
		t.Fatal("the artifact was recorded as \"unknown\" although the adapter reported its path")
	}
	if path != produced {
		t.Errorf("artifact path = %q, want %q", path, produced)
	}
	if size != int64(len("dump bytes")) {
		t.Errorf("artifact size = %d, want %d", size, len("dump bytes"))
	}
}

// TestResolveArtifactRefPrefersAnExplicitOutName: the fallback must not override
// a configured name, or a job with `output:` would start recording a different
// path than the one it writes.
func TestResolveArtifactRefPrefersAnExplicitOutName(t *testing.T) {
	dir := t.TempDir()
	configured := filepath.Join(dir, "shop.sql")
	if err := os.WriteFile(configured, []byte("x"), 0o600); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}

	path, _ := ResolveArtifactRef("local", dir, "shop.sql", "", filepath.Join(dir, "other.sql"))
	if path != configured {
		t.Errorf("artifact path = %q, want the configured %q", path, configured)
	}
}

// TestResolveArtifactRefStillUnknownWithNothingToGoOn: when neither a name nor a
// produced path is available there is genuinely nothing to record, and saying so
// is better than inventing a path.
func TestResolveArtifactRefStillUnknownWithNothingToGoOn(t *testing.T) {
	if path, _ := ResolveArtifactRef("local", t.TempDir(), "", "", ""); path != "unknown" {
		t.Errorf("artifact path = %q, want \"unknown\"", path)
	}
}
