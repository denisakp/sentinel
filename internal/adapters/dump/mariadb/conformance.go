package mariadb

import "github.com/denisakp/sentinel/internal/ports"

var (
	_ ports.DumpBuilder   = (*Builder)(nil)
	_ ports.EngineOptions = (*MariaDBDumpArgs)(nil)
)
