package lock

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
	"github.com/denisakp/sentinel/internal/ports"
)

func writeLockFile(t *testing.T, dir, jobName string, jl ports.JobLock) string {
	t.Helper()
	body, err := json.Marshal(jl)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	path := filepath.Join(dir, jobName+".lock")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}

func TestScanStale_MatchesAcquireSemantics(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir)
	threshold := time.Minute
	host, _ := os.Hostname()

	// (a) live-holder via real acquire — must be preserved
	if _, err := m.TryAcquire("live", threshold); err != nil {
		t.Fatalf("TryAcquire live: %v", err)
	}
	t.Cleanup(func() { _ = m.Release("live") })

	// (b) recent dead-PID — preserved
	writeLockFile(t, dir, "recent-dead", ports.JobLock{
		PID: 999999999, JobName: "recent-dead",
		StartTime: time.Now().Add(-10 * time.Second),
		Hostname:  host,
	})

	// (c) ancient dead-PID — removed
	ancient := writeLockFile(t, dir, "ancient-dead", ports.JobLock{
		PID: 999999999, JobName: "ancient-dead",
		StartTime: time.Now().Add(-2 * time.Hour),
		Hostname:  host,
	})

	// (d) recycled-self young — preserved (live PID)
	writeLockFile(t, dir, "recycled-self", ports.JobLock{
		PID: os.Getpid(), JobName: "recycled-self",
		StartTime: time.Now().Add(-10 * time.Second),
		Hostname:  host,
	})

	removed, err := m.ScanStale(threshold)
	if err != nil {
		t.Fatalf("ScanStale: %v", err)
	}
	if len(removed) != 1 || removed[0] != ancient {
		t.Fatalf("removed=%v, want [%s]", removed, ancient)
	}
	for _, name := range []string{"live", "recent-dead", "recycled-self"} {
		if _, err := os.Stat(filepath.Join(dir, name+".lock")); err != nil {
			t.Errorf("%s.lock missing after scan: %v", name, err)
		}
	}
}

func TestScanStale_RespectsForeignHost(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir)
	path := writeLockFile(t, dir, "foreign", ports.JobLock{
		PID: 999999999, JobName: "foreign",
		StartTime: time.Now().Add(-2 * time.Hour),
		Hostname:  "some-other-host",
	})

	removed, err := m.ScanStale(time.Minute)
	if err != nil {
		t.Fatalf("ScanStale: %v", err)
	}
	if len(removed) != 0 {
		t.Errorf("removed=%v, want none", removed)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("foreign-host lock removed: %v", err)
	}
}
