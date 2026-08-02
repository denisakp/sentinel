package pg

import "github.com/denisakp/sentinel/internal/ports"

// ArgsFactory builds *PgDumpArgs from a pure ports.DumpJobSpec (spec 040 /
// PRD 27). Body lifted from the former config.BuildPgDumpArgs; the Storage,
// TLS, and PITR/WAL fields are intentionally left zero — the command layer
// sets Storage, and PITR metadata is populated later in the pipeline.
type ArgsFactory struct{}

func (ArgsFactory) BuildDumpArgs(spec ports.DumpJobSpec) (ports.EngineOptions, error) {
	return &PgDumpArgs{
		Host:                 spec.Host,
		Port:                 spec.Port,
		Username:             spec.Username,
		Password:             spec.Password,
		Database:             spec.Database,
		AdditionalArgs:       spec.AdditionalArgs,
		PgOutFormat:          spec.PgOutFormat,
		Compress:             spec.Compress,
		CompressionAlgorithm: spec.CompressionAlgorithm,
		CompressionLevel:     spec.CompressionLevel,
	}, nil
}
