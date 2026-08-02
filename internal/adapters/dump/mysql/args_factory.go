package mysql

import "github.com/denisakp/sentinel/internal/ports"

// ArgsFactory builds *MySqlDumpArgs from a pure ports.DumpJobSpec (spec 040 /
// PRD 27). Body lifted from the former config.BuildMySQLDumpArgs; Storage is
// set by the command layer.
type ArgsFactory struct{}

func (ArgsFactory) BuildDumpArgs(spec ports.DumpJobSpec) (ports.EngineOptions, error) {
	return &MySqlDumpArgs{
		Host:           spec.Host,
		Port:           spec.Port,
		Username:       spec.Username,
		Password:       spec.Password,
		Database:       spec.Database,
		AdditionalArgs: spec.AdditionalArgs,
	}, nil
}
