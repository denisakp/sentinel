package pg

import "github.com/denisakp/sentinel/internal/ports"

// ArgsFactory builds *PgDumpArgs from a pure ports.DumpJobSpec. Body lifted
// from the former config.BuildPgDumpArgs. Storage and the PITR/WAL fields are
// intentionally left zero: the command layer sets Storage, and PITR metadata is
// populated later in the pipeline. TLS is carried on the spec, since nothing
// downstream ever populated it and a configured tls: block had no effect (#189).
type ArgsFactory struct{}

func (ArgsFactory) BuildDumpArgs(spec ports.DumpJobSpec) (ports.EngineOptions, error) {
	return &PgDumpArgs{
		Host:                 spec.Host,
		Port:                 spec.Port,
		Username:             spec.Username,
		Password:             spec.Password,
		Database:             spec.Database,
		AdditionalArgs:       spec.AdditionalArgs,
		TLS:                  spec.TLS,
		PgOutFormat:          spec.PgOutFormat,
		Compress:             spec.Compress,
		CompressionAlgorithm: spec.CompressionAlgorithm,
		CompressionLevel:     spec.CompressionLevel,
	}, nil
}
