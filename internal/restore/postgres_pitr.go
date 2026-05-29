package restore

import (
	"context"
	"fmt"

	"github.com/denisakp/sentinel/internal/config"
	pgrestore "github.com/denisakp/sentinel/internal/adapters/restore/pg"
)

// executePostgresPITR runs PostgreSQL restore orchestration for PITR requests.
// The initial implementation reuses pg_restore execution and relies on planner
// gating to ensure PITR requests are validated before execution.
func executePostgresPITR(ctx context.Context, job config.RestoreJob, password, stagedPath string) error {
	args, err := config.BuildPgRestoreArgs(job, password, stagedPath)
	if err != nil {
		return fmt.Errorf("failed to build postgres PITR restore args: %w", err)
	}
	if err := pgrestore.Restore(ctx, args); err != nil {
		return fmt.Errorf("failed to execute postgres PITR restore: %w", err)
	}
	return nil
}
