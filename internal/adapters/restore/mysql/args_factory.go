package mysql

import (
	"fmt"

	"github.com/denisakp/sentinel/internal/ports"
)

// ArgsFactory builds *RestoreArgs from a pure ports.RestoreJobSpec (spec 041 /
// PRD 28). Body lifted from the former config.BuildMySQLRestoreArgs.
type ArgsFactory struct{}

func (ArgsFactory) BuildRestoreArgs(spec ports.RestoreJobSpec, phase ports.RestorePhase) (ports.RestoreOptions, error) {
	if phase != ports.PrimaryRestore {
		return nil, fmt.Errorf("mysql restore does not support phase %d", phase)
	}
	return &RestoreArgs{
		Host:           spec.Host,
		Port:           spec.Port,
		Username:       spec.Username,
		Password:       spec.Password,
		Database:       spec.Database,
		BackupPath:     spec.BackupPath,
		OnConflict:     spec.OnConflict,
		AdditionalArgs: spec.AdditionalArgs,
	}, nil
}
