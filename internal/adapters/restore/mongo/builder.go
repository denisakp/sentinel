package mongo

import (
	"context"
	"fmt"

	"github.com/denisakp/sentinel/internal/ports"
)

// Builder is the zero-field type satisfying ports.RestoreBuilder for
// MongoDB restores (spec 036). Dispatches to Restore or ReplayOplog based
// on the concrete type of bc.Options.
type Builder struct{}

// Build dispatches to Restore (when bc.Options is *RestoreArgs) or to
// ReplayOplog (when bc.Options is *OplogReplayArgs).
func (Builder) Build(bc ports.RestoreBuildContext) (ports.RestoreBuildResult, error) {
	ctx := bc.Context
	if ctx == nil {
		ctx = context.Background()
	}
	switch args := bc.Options.(type) {
	case *RestoreArgs:
		return ports.RestoreBuildResult{}, Restore(ctx, args)
	case *OplogReplayArgs:
		return ports.RestoreBuildResult{}, ReplayOplog(ctx, args)
	default:
		return ports.RestoreBuildResult{}, fmt.Errorf("mongo restore builder: wrong options type %T", bc.Options)
	}
}
