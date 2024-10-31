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
func argsBuilder(pda *PgDumpArgs) ([]string, error) {
	pda.Host = utils.DefaultValue(pda.Host, "127.0.0.1") // set the default host to 127.0.0.1 if not provided
	pda.Port = utils.DefaultValue(pda.Port, "5432")      // set the default port to 5432 if not provided

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

	args := []string{
		"--host=" + pda.Host,
		"--port=" + pda.Port,
		"--username=" + pda.Username,
		"--dbname=" + pda.Database,
		"--file=" + pda.OutName,
		"--format=" + pda.OutFormat,
	}

	if pda.Compress {
		if err := addCompression(&args, pda); err != nil {
			return nil, err
		}
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
