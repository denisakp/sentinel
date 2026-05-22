package mongo_dump

import (
	"fmt"
	"strings"

	"github.com/denisakp/sentinel/internal/backup"
	"github.com/denisakp/sentinel/internal/storage"
	internaltls "github.com/denisakp/sentinel/internal/tls"
	"github.com/denisakp/sentinel/internal/utils"
)

type DumpMongoArgs struct {
	Uri            string          // MongoDB URI
	Database       string          // MongoDB database name (optional)
	Compress       bool            // Compress the backup file
	AdditionalArgs string          // Additional arguments for the mongo_dump command
	Storage        *storage.Params // Storage parameters
	TLS            *internaltls.Config
}

func argsBuilder(da *DumpMongoArgs, backupPath string) ([]string, *internaltls.MongoTLSMaterial, error) {
	// set default values
	da.Uri = utils.DefaultValue(da.Uri, "mongodb://localhost:27017")

	// handle output name
	outName := utils.DefaultValue(da.Storage.OutName, utils.DefaultBackupOutName())
	da.Storage.OutName = utils.FullPath(backupPath, outName)

	parsedAdditionalArgs := []string{}
	if da.AdditionalArgs != "" {
		var err error
		parsedAdditionalArgs, err = backup.ParseAdditionalArgs(da.AdditionalArgs)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to parse additional_args: %w", err)
		}
	}

	hasArchive := false
	for _, arg := range parsedAdditionalArgs {
		if arg == "--archive" || strings.HasPrefix(arg, "--archive=") {
			hasArchive = true
		}
	}

	args := []string{
		fmt.Sprintf("--uri=%s", da.Uri),
	}
	if !hasArchive {
		args = append(args, fmt.Sprintf("--out=%s", da.Storage.OutName))
	}
	args = append(args, "--quiet")
	if da.Database != "" {
		args = append(args, fmt.Sprintf("--db=%s", da.Database))
	}

	// Handle compression
	if da.Compress {
		args = append(args, "--gzip")
	}

	if len(parsedAdditionalArgs) > 0 {
		args = append(args, parsedAdditionalArgs...)
	}

	material, tlsArgs, err := internaltls.PrepareMongoTLS(da.TLS, da.Storage.OutName)
	if err != nil {
		return nil, nil, fmt.Errorf("prepare mongo tls material: %w", err)
	}
	args = append(args, tlsArgs...)

	args = backup.RemoveArgsDuplicate(args) // remove duplicate arguments

	return args, material, nil
}
