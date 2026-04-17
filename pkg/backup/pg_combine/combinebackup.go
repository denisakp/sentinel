package pg_combine

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
)

// CombineArgs defines inputs for pg_combinebackup execution.
type CombineArgs struct {
	ToolsPath string
	OutputDir string
	Sources   []string
}

// Combine runs pg_combinebackup to assemble a full+incremental chain.
func Combine(ctx context.Context, args *CombineArgs) error {
	if args == nil {
		return fmt.Errorf("combine args are required")
	}
	if args.OutputDir == "" {
		return fmt.Errorf("output dir is required")
	}
	if len(args.Sources) == 0 {
		return fmt.Errorf("at least one source backup is required")
	}

	tool := "pg_combinebackup"
	if args.ToolsPath != "" {
		tool = filepath.Join(args.ToolsPath, tool)
	}
	if _, err := exec.LookPath(tool); err != nil {
		return fmt.Errorf("required_tool_missing: pg_combinebackup")
	}

	cmdArgs := append([]string{"--output", args.OutputDir}, args.Sources...)
	cmd := exec.CommandContext(ctx, tool, cmdArgs...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("pg_combinebackup failed: %w: %s", err, string(out))
	}

	return nil
}
