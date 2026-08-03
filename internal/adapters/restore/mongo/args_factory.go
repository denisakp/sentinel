package mongo

import (
	"fmt"
	"strings"

	"github.com/denisakp/sentinel/internal/ports"
)

// ArgsFactory builds Mongo restore args from a pure ports.RestoreJobSpec.
// PrimaryRestore -> *RestoreArgs (body from the former
// config.BuildMongoRestoreArgs); OplogReplay -> *OplogReplayArgs (body from
// the former config.BuildMongoOplogReplayArgs, incl. its validation).
type ArgsFactory struct{}

func (ArgsFactory) BuildRestoreArgs(spec ports.RestoreJobSpec, phase ports.RestorePhase) (ports.RestoreOptions, error) {
	switch phase {
	case ports.PrimaryRestore:
		return &RestoreArgs{
			URI:            spec.URI,
			Database:       spec.Database,
			BackupPath:     spec.BackupPath,
			OnConflict:     spec.OnConflict,
			Gzip:           spec.Gzip,
			Archive:        spec.Archive,
			AdditionalArgs: spec.AdditionalArgs,
		}, nil
	case ports.OplogReplay:
		if spec.Engine != "mongodb" {
			return nil, fmt.Errorf("mongodb oplog replay is only valid for mongodb jobs")
		}
		if strings.TrimSpace(spec.ArchivePath) == "" {
			return nil, fmt.Errorf("oplog archive path is required")
		}
		if strings.TrimSpace(spec.URI) == "" {
			return nil, fmt.Errorf("uri is required for mongodb oplog replay")
		}
		return &OplogReplayArgs{
			URI:         spec.URI,
			ArchivePath: spec.ArchivePath,
		}, nil
	default:
		return nil, fmt.Errorf("mongodb restore does not support phase %d", phase)
	}
}
