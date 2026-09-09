package config_census

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/denisakp/sentinel/internal/config"
)

// Behavioural proof for the subset of configuration keys whose effect the
// existing suite can observe without new fixtures or live infrastructure.
//
// The standard here is deliberately higher than "the configuration was
// accepted". That assertion is exactly the one whose weakness produced this
// defect batch: a command that validates a key, discards it, and exits zero
// passes it. These tests instead set a key to two different values and require
// the resulting behaviour to differ. A key that is ignored produces identical
// results and fails.
//
// The subset is small, and honestly so. Most keys only reveal their effect
// against a live database, which is the container-backed suite's job. See
// reachability/observable-subset.md for the determination and what the rest
// would need.

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "sentinel.yaml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}
	return p
}

const baseConfig = `version: "1"
defaults:
  output_dir: /tmp/sentinel-census
  compression:
    enabled: %s
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

// TestDefaultsCompressionReachesJobs proves that defaults.compression.enabled is
// honoured rather than merely accepted: the same job, unchanged, resolves
// differently depending only on the default.
//
// This key matters beyond the census. Issue #188 reports that this inheritance
// reaches engines it should not, and a defect of that shape is invisible unless
// something asserts the value actually arrives at the job.
func TestDefaultsCompressionReachesJobs(t *testing.T) {
	t.Setenv("PROBE_PW", "census-probe")
	onPath := writeConfig(t, sprintf(baseConfig, "true"))
	offPath := writeConfig(t, sprintf(baseConfig, "false"))

	on, err := config.LoadConfig(onPath)
	if err != nil {
		t.Fatalf("loading config with compression enabled: %v", err)
	}
	off, err := config.LoadConfig(offPath)
	if err != nil {
		t.Fatalf("loading config with compression disabled: %v", err)
	}

	onJob, ok := on.Databases["probe"]
	if !ok {
		t.Fatal("job \"probe\" missing from the loaded configuration")
	}
	offJob, ok := off.Databases["probe"]
	if !ok {
		t.Fatal("job \"probe\" missing from the loaded configuration")
	}

	if onJob.Compression.Enabled == offJob.Compression.Enabled {
		t.Errorf(
			"defaults.compression.enabled does not reach the job: it resolved to %v in both the "+
				"enabled and the disabled configuration.\nThe key is accepted and then ignored, "+
				"which is the defect shape this census exists to catch.",
			onJob.Compression.Enabled)
	}
	if !onJob.Compression.Enabled {
		t.Error("defaults.compression.enabled: true did not enable compression on an inheriting job")
	}
}

// TestHistoryDBPathIsHonoured proves history_db_path is read rather than
// ignored: two configurations differing only in that key resolve to different
// paths.
func TestHistoryDBPathIsHonoured(t *testing.T) {
	t.Setenv("PROBE_PW", "census-probe")
	const tmpl = `version: "1"
history_db_path: %s
defaults:
  output_dir: /tmp/sentinel-census
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
	a, err := config.LoadConfig(writeConfig(t, sprintf(tmpl, "/tmp/census-a.db")))
	if err != nil {
		t.Fatalf("loading first configuration: %v", err)
	}
	b, err := config.LoadConfig(writeConfig(t, sprintf(tmpl, "/tmp/census-b.db")))
	if err != nil {
		t.Fatalf("loading second configuration: %v", err)
	}
	if a.HistoryDBPath == b.HistoryDBPath {
		t.Errorf("history_db_path is ignored: both configurations resolved to %q", a.HistoryDBPath)
	}
	if a.HistoryDBPath != "/tmp/census-a.db" {
		t.Errorf("history_db_path resolved to %q, want %q", a.HistoryDBPath, "/tmp/census-a.db")
	}
}

func sprintf(format string, a ...any) string { return fmtSprintf(format, a...) }
