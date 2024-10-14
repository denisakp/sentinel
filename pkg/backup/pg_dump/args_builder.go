package pg_dump

import (
	"fmt"
	"github.com/denisakp/sentinel/pkg/backup"
)

type PgDumpArgs struct {
	Host                 string
	Port                 string
	Username             string
	Password             string
	Database             string
	OutName              string
	OutFormat            string
	Compress             bool
	CompressionAlgorithm string
	CompressionLevel     int
	AdditionalArgs       string
}

// ArgsBuilder builds the arguments for the pg_dump command
func ArgsBuilder(pda *PgDumpArgs) ([]string, error) {
	if err := validateRequiredArgs(pda); err != nil {
		return nil, err
	}

	pda.Host = backup.DefaultString(pda.Host, "127.0.0.1")
	pda.Port = backup.DefaultString(pda.Port, "5432")

	args := []string{
		"--host=" + pda.Host,
		"--port=" + pda.Port,
		"--username=" + pda.Username,
		"--dbname=" + pda.Database,
		"--file=" + pda.OutName,
		"--format=" + pda.OutFormat,
	}

	if pda.Compress {
		pda.CompressionAlgorithm = backup.DefaultString(pda.CompressionAlgorithm, "gzip")

		// handle compression algorithm
		if err := ValidatePgCompressionAlgorithm(pda.CompressionAlgorithm); err != nil {
			return nil, err
		}

		// handle compression level
		if err := ValidatePgCompressionLevel(pda.CompressionLevel); err != nil {
			return nil, err
		}

		args = append(args, fmt.Sprintf("--compress=%s:%d", pda.CompressionAlgorithm, pda.CompressionLevel))
	}

	// handle additional arguments
	if pda.AdditionalArgs != "" {
		additionalArgs := backup.ParseAdditionalArgs(pda.AdditionalArgs)
		args = append(args, additionalArgs...)
	}

	// remove duplicated arguments
	args = backup.RemoveArgsDuplicate(args)

	return args, nil
}
