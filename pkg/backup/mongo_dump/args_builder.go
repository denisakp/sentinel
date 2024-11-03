package mongo_dump

import (
	"fmt"
	"github.com/denisakp/sentinel/internal/backup"
	"github.com/denisakp/sentinel/internal/storage"
	"github.com/denisakp/sentinel/internal/utils"
)

type DumpMongoArgs struct {
	Uri            string          // MongoDB URI
	Compress       bool            // Compress the backup file
	AdditionalArgs string          // Additional arguments for the mongo_dump command
	Storage        *storage.Params // Storage parameters
}

func argsBuilder(da *DumpMongoArgs, backupPath string) ([]string, error) {
	// set default values
	da.Uri = utils.DefaultValue(da.Uri, "mongodb://localhost:27017")

	// handle output name
	outName := utils.DefaultValue(da.Storage.OutName, utils.DefaultBackupOutName())
	da.Storage.OutName = utils.FullPath(backupPath, outName)

	args := []string{
		fmt.Sprintf("--uri=%s", da.Uri),
		fmt.Sprintf("--out=%s", da.Storage.OutName),
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
