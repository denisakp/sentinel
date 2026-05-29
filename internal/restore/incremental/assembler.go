package incremental

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/denisakp/sentinel/internal/adapters/restore/incremental/pgcombine"
)

var combinePostgresChain = pgcombine.Combine

// AssemblyRequest carries the minimal inputs needed before engine-specific assembly starts.
type AssemblyRequest struct {
	StagingDir           string
	EstimatedBytes       int64
	AvailableBytes       int64
	RequiresCombineTool  bool
	CombineToolAvailable bool
}

// ValidateAssemblyPreconditions checks disk/tool preconditions before chain assembly.
func ValidateAssemblyPreconditions(req AssemblyRequest) error {
	if req.StagingDir == "" {
		return fmt.Errorf("insufficient_staging_space: staging_dir is required")
	}
	if req.EstimatedBytes > 0 && req.AvailableBytes > 0 && req.AvailableBytes < req.EstimatedBytes {
		return fmt.Errorf("insufficient_staging_space: need=%d available=%d", req.EstimatedBytes, req.AvailableBytes)
	}
	if req.RequiresCombineTool && !req.CombineToolAvailable {
		return fmt.Errorf("required_tool_missing: pg_combinebackup")
	}
	return nil
}

// AssemblePostgresChain combines a staged chain into a single assembled directory path.
func AssemblePostgresChain(ctx context.Context, stagingDir string, stagedSources []string, toolsPath string) (string, error) {
	if stagingDir == "" {
		return "", fmt.Errorf("staging_dir is required")
	}
	if len(stagedSources) < 2 {
		if len(stagedSources) == 1 {
			return stagedSources[0], nil
		}
		return "", fmt.Errorf("at least one staged source is required")
	}

	outDir := filepath.Join(stagingDir, fmt.Sprintf("combined_%d", time.Now().UTC().UnixNano()))
	if err := os.MkdirAll(outDir, 0o700); err != nil {
		return "", fmt.Errorf("failed to create combine output dir: %w", err)
	}

	if err := combinePostgresChain(ctx, &pgcombine.CombineArgs{
		ToolsPath: toolsPath,
		OutputDir: outDir,
		Sources:   stagedSources,
	}); err != nil {
		if cleanupErr := os.RemoveAll(outDir); cleanupErr != nil {
			return "", fmt.Errorf("%w (cleanup failed: %v)", err, cleanupErr)
		}
		return "", err
	}

	return outDir, nil
}
