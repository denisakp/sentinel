package mongo

import "github.com/denisakp/sentinel/internal/ports"

// ArgsFactory builds *DumpMongoArgs from a pure ports.DumpJobSpec. Body
// lifted from the former config.BuildMongoDumpArgs (gzip -> Compress, no
// password); Storage is set by the command layer.
type ArgsFactory struct{}

func (ArgsFactory) BuildDumpArgs(spec ports.DumpJobSpec) (ports.EngineOptions, error) {
	return &DumpMongoArgs{
		Uri:            spec.URI,
		Database:       spec.Database,
		Compress:       spec.Compress,
		AdditionalArgs: spec.AdditionalArgs,
	}, nil
}
