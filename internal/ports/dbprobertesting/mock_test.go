package dbprobertesting_test

import (
	"github.com/denisakp/sentinel/internal/ports"
	"github.com/denisakp/sentinel/internal/ports/dbprobertesting"
)

// Compile-time port assertion (kept in _test package to avoid sub-package →
// parent-package cycle per CLAUDE.md import-cycle rule).
var _ ports.DBProber = (*dbprobertesting.MockProber)(nil)
