package incremental

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/denisakp/sentinel/pkg/backup/pg_combine"
)

func TestValidateAssemblyPreconditions(t *testing.T) {
	cases := []struct {
		name    string
		req     AssemblyRequest
		wantSub string
	}{
		{"missing_staging_dir", AssemblyRequest{}, "insufficient_staging_space"},
		{"insufficient_space", AssemblyRequest{StagingDir: "/tmp", EstimatedBytes: 100, AvailableBytes: 10}, "insufficient_staging_space"},
		{"missing_combine_tool", AssemblyRequest{StagingDir: "/tmp", RequiresCombineTool: true}, "required_tool_missing"},
		{"ok", AssemblyRequest{StagingDir: "/tmp", EstimatedBytes: 10, AvailableBytes: 100}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateAssemblyPreconditions(tc.req)
			if tc.wantSub == "" {
				if err != nil {
					t.Fatalf("unexpected err: %v", err)
				}
				return
			}
			if err == nil || !contains(err.Error(), tc.wantSub) {
				t.Fatalf("want err containing %q, got %v", tc.wantSub, err)
			}
		})
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestAssemblePostgresChain_EmptyStagingDir(t *testing.T) {
	_, err := AssemblePostgresChain(context.Background(), "", []string{"a", "b"}, "")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestAssemblePostgresChain_NoSources(t *testing.T) {
	_, err := AssemblePostgresChain(context.Background(), t.TempDir(), nil, "")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestAssemblePostgresChain_SingleSourcePassthrough(t *testing.T) {
	out, err := AssemblePostgresChain(context.Background(), t.TempDir(), []string{"only"}, "")
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
	combinePostgresChain = func(ctx context.Context, args *pg_combine.CombineArgs) error { return nil }

	stagingDir := t.TempDir()
	out, err := AssemblePostgresChain(context.Background(), stagingDir, []string{"a", "b"}, "")
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

	combinePostgresChain = func(ctx context.Context, args *pg_combine.CombineArgs) error {
		return errors.New("combine failed")
	}

	stagingDir := t.TempDir()
	_, err := AssemblePostgresChain(context.Background(), stagingDir, []string{"base", "incr"}, "")
	if err == nil {
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
