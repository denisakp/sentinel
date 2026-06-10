package mysql

import "github.com/denisakp/sentinel/internal/ports"

var (
	_ ports.DumpBuilder   = (*Builder)(nil)
	_ ports.EngineOptions = (*MySqlDumpArgs)(nil)
)
