package incremental

// Pure assembly preconditions, carved from
// internal/restore/incremental/assembler.go.
// The I/O half (AssemblePostgresChain) lives in
// internal/adapters/restore/chain_assembler behind ports.ChainAssembler.

import "fmt"

// AssemblyRequest carries the minimal inputs needed before engine-specific
// assembly starts.
type AssemblyRequest struct {
	StagingDir           string
	EstimatedBytes       int64
	AvailableBytes       int64
	RequiresCombineTool  bool
	CombineToolAvailable bool
}

// ValidateAssemblyPreconditions checks disk/tool preconditions before chain
// assembly. Pure.
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
