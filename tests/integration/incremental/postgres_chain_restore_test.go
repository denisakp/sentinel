package incremental

import "testing"

func TestPostgresChainRestore_FullPlusIncrementals(t *testing.T) {
	t.Skip("integration test scaffold: requires postgres 17+, pg_combinebackup, and prepared full+incremental artifacts")
}
