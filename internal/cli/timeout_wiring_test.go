package cli

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/denisakp/sentinel/internal/ports"
	"github.com/denisakp/sentinel/internal/scheduler"
)

// TestRunWithTimeoutStopsTheWorkItWraps is half the guard for #194, and the
// cheap half.
func TestRunWithTimeoutStopsTheWorkItWraps(t *testing.T) {
	// timeoutMinutes is in minutes, so a real deadline is impractical to wait
	// for here. The zero case is what matters for the wiring: it must pass the
	// caller's context through untouched rather than inventing one.
	var got context.Context
	err := scheduler.RunWithTimeout(context.Background(), 0, func(ctx context.Context) error {
		got = ctx
		return nil
	})
	if err != nil {
		t.Fatalf("RunWithTimeout error = %v", err)
	}
	if _, hasDeadline := got.Deadline(); hasDeadline {
		t.Error("a zero timeout must not impose a deadline")
	}

	err = scheduler.RunWithTimeout(context.Background(), 1, func(ctx context.Context) error {
		got = ctx
		return nil
	})
	if err != nil {
		t.Fatalf("RunWithTimeout error = %v", err)
	}
	if _, hasDeadline := got.Deadline(); !hasDeadline {
		t.Error("a non-zero timeout must impose a deadline on the context it hands the job")
	}
}

// TestNoDumpStartsAnUncancellableSubprocess is the half that matters, and the
// reason wiring RunWithTimeout alone would have been a fake fix.
//
// Every dump started its subprocess with exec.Command and no context, and every
// Builder dropped the BuildContext it was handed. A deadline therefore reached
// nothing: RunWithTimeout would have returned only once the hung dump finished on
// its own, which is exactly the situation job_timeout_minutes exists to escape.
//
// This is a source check rather than a behavioural one, deliberately. Driving a
// real dump needs the engine client binaries, so a behavioural test would skip on
// most machines, and since nothing in CI runs `go test ./...` it would skip
// everywhere: a guard that never executes is worse than none, because it reports
// coverage it does not have. Checking that the dangerous call shape is absent
// always runs and catches the regression directly.
func TestNoDumpStartsAnUncancellableSubprocess(t *testing.T) {
	root, err := repoRootFromTest()
	if err != nil {
		t.Fatalf("locating repository root: %v", err)
	}

	var offenders []string
	dumpDir := filepath.Join(root, "internal", "adapters", "dump")
	err = filepath.WalkDir(dumpDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		body, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		for i, line := range strings.Split(string(body), "\n") {
			if strings.Contains(line, "exec.Command(") && !strings.Contains(line, "exec.CommandContext(") {
				rel, _ := filepath.Rel(root, path)
				offenders = append(offenders, fmt.Sprintf("%s:%d: %s", rel, i+1, strings.TrimSpace(line)))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", dumpDir, err)
	}

	if len(offenders) > 0 {
		t.Errorf("%d dump subprocess(es) started without a context, so no deadline can stop them "+
			"and scheduler.job_timeout_minutes cannot kill a hung dump:\n  %s",
			len(offenders), strings.Join(offenders, "\n  "))
	}
}

func repoRootFromTest() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found above %s", dir)
		}
		dir = parent
	}
}

// stubProber satisfies ports.DBProber and reports the database as reachable, so
// Build proceeds to the dump rather than stopping at the connectivity gate.
type stubProber struct{}

func (stubProber) Ping(context.Context, ports.DatabaseConfig) error { return nil }
func (stubProber) ListDatabases(context.Context, ports.DatabaseConfig) ([]string, error) {
	return nil, nil
}
func (stubProber) AssessPostgresCascadeSafety(context.Context, ports.DatabaseConfig, ports.CascadeTarget) (*ports.CascadeSafetyResult, error) {
	return nil, errors.New("not used")
}
