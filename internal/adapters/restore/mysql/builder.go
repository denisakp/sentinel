package mysql

import (
	"context"
	"fmt"

	"github.com/denisakp/sentinel/internal/ports"
)

// Builder is the zero-field type satisfying ports.RestoreBuilder for
// MySQL restores (spec 036).
type Builder struct{}

// Build wraps the existing Restore entry point. bc.Options must be
// *RestoreArgs.
func (Builder) Build(bc ports.RestoreBuildContext) (ports.RestoreBuildResult, error) {
	args, ok := bc.Options.(*RestoreArgs)
	if !ok {
		return ports.RestoreBuildResult{}, fmt.Errorf("mysql restore builder: wrong options type %T", bc.Options)
	}
	ctx := bc.Context
	if ctx == nil {
		ctx = context.Background()
	}
	return ports.RestoreBuildResult{}, Restore(ctx, args)
}
