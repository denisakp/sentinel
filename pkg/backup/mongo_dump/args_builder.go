package mongo_dump

import (
	"github.com/denisakp/sentinel/internal/backup"
)

type DumpMongoArgs struct {
	Uri            string
	Compress       bool
	AdditionalArgs string
	OutName        string
}

func argsBuilder(da *DumpMongoArgs) ([]string, error) {
	da.Uri = backup.DefaultString(da.Uri, "mongodb://localhost:27017") // set default uri

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
