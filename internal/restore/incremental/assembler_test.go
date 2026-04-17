package incremental

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/denisakp/sentinel/pkg/backup/pg_combine"
)

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
