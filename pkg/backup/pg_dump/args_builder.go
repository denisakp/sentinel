package pg_dump

import (
	"fmt"
	"github.com/denisakp/sentinel/internal/backup"
	"github.com/denisakp/sentinel/internal/utils"
)

type PgDumpArgs struct {
	Host                 string // PostgresSQL host
	Port                 string // PostgresSQL port
	Username             string // PostgresSQL username
	Password             string // PostgresSQL password
	Database             string // PostgresSQL database name
	OutName              string // Output name for the backup file
	OutFormat            string // Output format for the backup file
	Compress             bool   // Enable compression
	CompressionAlgorithm string // Compression algorithm
	CompressionLevel     int    // Compression level
	AdditionalArgs       string // Additional arguments for the pg_dump command
	StorageType          string // Storage type for the backup file
	StoragePath          string // Storage path for the backup file
}

// argsBuilder builds the arguments for the pg_dump command
func argsBuilder(pda *PgDumpArgs, backupPath string) ([]string, error) {
	if err := validateRequiredArgs(pda); err != nil {
		return nil, err
	}

	// set the default host and port if not provided
	pda.Host = utils.DefaultValue(pda.Host, "127.0.0.1")
	pda.Port = utils.DefaultValue(pda.Port, "5432")

	// enable compression if compression algorithm is set
	if pda.CompressionAlgorithm != "" {
		pda.Compress = true
	}

	// set default output format
	pda.OutFormat = utils.DefaultValue(pda.OutFormat, "p")
	if err := validatePgOutFormat(pda.OutFormat); err != nil {
		return nil, err
	}

	// handle backup outName
	if err := setOutName(pda); err != nil {
		return nil, err
	}

	pda.OutName = utils.FullPath(backupPath, pda.OutName)

	args := []string{
		"--host=" + pda.Host,
		"--port=" + pda.Port,
		"--username=" + pda.Username,
		"--dbname=" + pda.Database,
		"--format=" + pda.OutFormat,
	}

	if pda.Compress {
		if err := addCompression(&args, pda); err != nil {
			return nil, err
		}
	}

	// handle the
	if pda.OutFormat == "d" {
		args = append(args, "--file="+pda.OutName)
	}

	// handle additional arguments
	if pda.AdditionalArgs != "" {
		additionalArgs := backup.ParseAdditionalArgs(pda.AdditionalArgs)
		args = append(args, additionalArgs...)
	}

	// remove duplicated arguments
	args = backup.RemoveArgsDuplicate(args) // remove duplicated arguments

	return args, nil
}

func addCompression(args *[]string, pda *PgDumpArgs) error {
	// set the default compression algorithm to gzip if not provided
	pda.CompressionAlgorithm = utils.DefaultValue(pda.CompressionAlgorithm, "gzip")

	// validate the compression algorithm
	if err := validatePgCompressionAlgorithm(pda.CompressionAlgorithm); err != nil {
		return err
	}

	// validate the compression level
	if err := validatePgCompressionLevel(pda.CompressionLevel); err != nil {
		return err
	}

	// add compression arguments
	*args = append(*args, fmt.Sprintf("--compress=%s:%d", pda.CompressionAlgorithm, pda.CompressionLevel))

	return nil
}
