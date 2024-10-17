package pg_dump

import (
	"fmt"
	"github.com/denisakp/sentinel/internal/backup"
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
}

// argsBuilder builds the arguments for the pg_dump command
func argsBuilder(pda *PgDumpArgs) ([]string, error) {
	if err := validateRequiredArgs(pda); err != nil {
		return nil, err
	}

	pda.Host = backup.DefaultString(pda.Host, "127.0.0.1") // set the default host to 127.0.0.1 if not provided
	pda.Port = backup.DefaultString(pda.Port, "5432")      // set the default port to 5432 if not provided

	args := []string{
		"--host=" + pda.Host,
		"--port=" + pda.Port,
		"--username=" + pda.Username,
		"--dbname=" + pda.Database,
		"--file=" + pda.OutName,
		"--format=" + pda.OutFormat,
	}

	if pda.Compress {
		pda.CompressionAlgorithm = backup.DefaultString(pda.CompressionAlgorithm, "gzip") // set the default compression algorithm to gzip if not provided

		// handle compression algorithm
		if err := validatePgCompressionAlgorithm(pda.CompressionAlgorithm); err != nil {
			return nil, err
		}

		// handle compression level
		if err := validatePgCompressionLevel(pda.CompressionLevel); err != nil {
			return nil, err
		}

		args = append(args, fmt.Sprintf("--compress=%s:%d", pda.CompressionAlgorithm, pda.CompressionLevel)) // add compression arguments
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
