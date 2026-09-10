package mongo

import (
	"fmt"
	"strings"

	mongotls "github.com/denisakp/sentinel/internal/adapters/mongo_tls"
	"github.com/denisakp/sentinel/internal/adapters/storage"
	backup "github.com/denisakp/sentinel/internal/domain/backup"
	"github.com/denisakp/sentinel/internal/ports"
	"github.com/denisakp/sentinel/internal/utils"
)

type DumpMongoArgs struct {
	Uri            string          // MongoDB URI
	Database       string          // MongoDB database name (optional)
	Compress       bool            // Compress the backup file
	AdditionalArgs string          // Additional arguments for the mongo_dump command
	Storage        *storage.Params // Storage parameters
	TLS            *ports.Config

	// RemoteStagingDir, when set for a remote backend, is a caller-provided
	// directory the mongodump archive is staged into. In this "executor-owned"
	// mode Backup does NOT upload and does NOT remove the directory — the
	// backup Executor hashes/encrypts/manifests the archive, uploads it (+ the
	// manifest sidecar), and cleans up. Empty → legacy self-owned
	// remote path (stage under <backup_path>/.staging/<job-id>/, upload, clean).
	RemoteStagingDir string
}

// engineOptions satisfies ports.EngineOptions.
func (*DumpMongoArgs) IsEngineOptions() {}

func argsBuilder(da *DumpMongoArgs, backupPath, stagingArchive string) ([]string, *mongotls.MongoTLSMaterial, error) {
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

	remote := da.Storage.StorageType != "" && da.Storage.StorageType != "local"

	args := []string{
		fmt.Sprintf("--uri=%s", da.Uri),
	}
	switch {
	case remote && !hasArchive:
		args = append(args, fmt.Sprintf("--archive=%s", stagingArchive))
	case !remote && !hasArchive:
		// --archive, not --out, so a local backup is a single file.
		//
		// --out makes mongodump write a DIRECTORY and produce nothing on stdout.
		// The pipeline then hashed an empty stdout, recorded sha256("") with a
		// size of 0, and pointed the manifest at a path the local backend cannot
		// enumerate because it lists files. The job reported success and the
		// history recorded a completed backup that `backup verify` could not
		// check and restore could not read. A manifest hash that is wrong in a
		// way that looks valid is worse than a missing one (#191).
		//
		// The remote path has always used --archive; this makes local match.
		args = append(args, fmt.Sprintf("--archive=%s", da.Storage.OutName))
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

	material, tlsArgs, err := mongotls.PrepareMongoTLS(da.TLS, da.Storage.OutName)
	if err != nil {
		return nil, nil, fmt.Errorf("prepare mongo tls material: %w", err)
	}
	args = append(args, tlsArgs...)

	args = backup.RemoveArgsDuplicate(args) // remove duplicate arguments

	return args, material, nil
}
