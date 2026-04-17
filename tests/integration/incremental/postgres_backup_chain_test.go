package incremental

import "testing"

func TestPostgresBackupChain_FullThenIncremental(t *testing.T) {
	t.Skip("integration test scaffold: requires PostgreSQL 17+ environment and sentinel CLI orchestration")
}
