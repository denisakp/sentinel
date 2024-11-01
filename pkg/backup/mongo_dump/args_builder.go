package mongo_dump

import (
	"github.com/denisakp/sentinel/internal/backup"
	"github.com/denisakp/sentinel/internal/utils"
)

type DumpMongoArgs struct {
	Uri            string // MongoDB URI
	Compress       bool   // Compress the backup file
	AdditionalArgs string // Additional arguments for the mongo_dump command
	OutName        string // Output name for the backup file
	StorageType    string // Storage type for the backup file
	StoragePath    string // Storage path for the backup file
}

func argsBuilder(da *DumpMongoArgs) ([]string, error) {
	// set default values
	da.Uri = utils.DefaultValue(da.Uri, "mongodb://localhost:27017")

	args := []string{
		"--uri=" + da.Uri,
		"--out=" + da.OutName,
		"--quiet",
	}

	// Handle compression
	if da.Compress {
		args = append(args, "--gzip")
	}

	if da.AdditionalArgs != "" {
		additionalArgs := backup.ParseAdditionalArgs(da.AdditionalArgs)
		args = append(args, additionalArgs...)
	}

	args = backup.RemoveArgsDuplicate(args) // remove duplicate arguments

	return args, nil
}
