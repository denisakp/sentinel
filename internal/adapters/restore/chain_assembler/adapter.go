// Package chain_assembler implements ports.ChainAssembler over the
// pg_combinebackup wrapper. Body ported from
// internal/restore/incremental/assembler.go::AssemblePostgresChain.
package chain_assembler

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/denisakp/sentinel/internal/adapters/restore/incremental/pgcombine"
)

// combinePostgresChain is the per-adapter test seam over pgcombine.Combine.
var combinePostgresChain = pgcombine.Combine

// Adapter satisfies ports.ChainAssembler.
type Adapter struct{}

// NewAdapter constructs the chain assembler adapter (stateless).
func NewAdapter() *Adapter { return &Adapter{} }

// AssemblePostgresChain combines a staged chain into a single assembled
// directory path. A single staged source is returned unchanged.
func (*Adapter) AssemblePostgresChain(ctx context.Context, stagingDir string, stagedSources []string, toolsPath string) (string, error) {
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
