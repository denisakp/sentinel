package chain_assembler

// Ported from internal/restore/incremental/assembler_test.go (spec 038
// Sub-PR L) — the AssemblePostgresChain halves; preconditions tests moved to
// internal/domain/restore/incremental/preconditions_test.go.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/denisakp/sentinel/internal/adapters/restore/incremental/pgcombine"
)

func TestAssemblePostgresChain_EmptyStagingDir(t *testing.T) {
	if _, err := NewAdapter().AssemblePostgresChain(context.Background(), "", []string{"a", "b"}, ""); err == nil {
		t.Fatal("expected error")
	}
}

func TestAssemblePostgresChain_NoSources(t *testing.T) {
	if _, err := NewAdapter().AssemblePostgresChain(context.Background(), t.TempDir(), nil, ""); err == nil {
		t.Fatal("expected error")
	}
}

func TestAssemblePostgresChain_SingleSourcePassthrough(t *testing.T) {
	out, err := NewAdapter().AssemblePostgresChain(context.Background(), t.TempDir(), []string{"only"}, "")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if out != "only" {
		t.Fatalf("want passthrough %q, got %q", "only", out)
	}
}

func TestAssemblePostgresChain_Success(t *testing.T) {
	originalCombine := combinePostgresChain
	t.Cleanup(func() { combinePostgresChain = originalCombine })
	combinePostgresChain = func(ctx context.Context, args *pgcombine.CombineArgs) error { return nil }

	stagingDir := t.TempDir()
	out, err := NewAdapter().AssemblePostgresChain(context.Background(), stagingDir, []string{"a", "b"}, "")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if out == "" {
		t.Fatal("expected output dir")
	}
}

func TestAssemblePostgresChain_CleansOutputDirOnCombineFailure(t *testing.T) {
	originalCombine := combinePostgresChain
	t.Cleanup(func() { combinePostgresChain = originalCombine })

	combinePostgresChain = func(ctx context.Context, args *pgcombine.CombineArgs) error {
		return errors.New("combine failed")
	}

	stagingDir := t.TempDir()
	if _, err := NewAdapter().AssemblePostgresChain(context.Background(), stagingDir, []string{"base", "incr"}, ""); err == nil {
		t.Fatal("expected combine failure")
	}

	matches, globErr := filepath.Glob(filepath.Join(stagingDir, "combined_*"))
	if globErr != nil {
		t.Fatalf("Glob() error = %v", globErr)
	}
	for _, match := range matches {
		if _, statErr := os.Stat(match); !os.IsNotExist(statErr) {
			t.Fatalf("expected combine output cleaned up, stat err = %v", statErr)
		}
	}
}
