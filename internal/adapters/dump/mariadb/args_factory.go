package mariadb

import "github.com/denisakp/sentinel/internal/ports"

// ArgsFactory builds *MariaDBDumpArgs from a pure ports.DumpJobSpec. Body lifted
// from the former config.BuildMariaDBDumpArgs; Storage is set by the command
// layer.
type ArgsFactory struct{}

func (ArgsFactory) BuildDumpArgs(spec ports.DumpJobSpec) (ports.EngineOptions, error) {
	return &MariaDBDumpArgs{
		Host:           spec.Host,
		Port:           spec.Port,
		Username:       spec.Username,
		Password:       spec.Password,
		Database:       spec.Database,
		AdditionalArgs: spec.AdditionalArgs,
	}, nil
}
