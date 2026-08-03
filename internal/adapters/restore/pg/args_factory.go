package pg

import (
	"fmt"

	"github.com/denisakp/sentinel/internal/ports"
)

// ArgsFactory builds *RestoreArgs from a pure ports.RestoreJobSpec. Body
// lifted from the former config.BuildPgRestoreArgs.
type ArgsFactory struct{}

func (ArgsFactory) BuildRestoreArgs(spec ports.RestoreJobSpec, phase ports.RestorePhase) (ports.RestoreOptions, error) {
	if phase != ports.PrimaryRestore {
		return nil, fmt.Errorf("postgres restore does not support phase %d", phase)
	}
	return &RestoreArgs{
		Host:           spec.Host,
		Port:           spec.Port,
		Username:       spec.Username,
		Password:       spec.Password,
		Database:       spec.Database,
		BackupPath:     spec.BackupPath,
		OnConflict:     spec.OnConflict,
		AllowCascade:   spec.AllowCascade,
		AdditionalArgs: spec.AdditionalArgs,
	}, nil
}
