package mongo

import "github.com/denisakp/sentinel/internal/ports"

var (
	_ ports.DumpBuilder     = (*Builder)(nil)
	_ ports.EngineOptions   = (*DumpMongoArgs)(nil)
	_ ports.DumpArgsFactory = ArgsFactory{}
)
