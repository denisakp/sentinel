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

// PgDumpArgsBuilder builds the arguments for the pg_dump command
func PgDumpArgsBuilder(pda *PgDumpArgs) ([]string, error) {
	if pda.Database == "" {
		return nil, fmt.Errorf("database name is required")
	}

	if pda.Username == "" {
		return nil, fmt.Errorf("username is required")
	}

	// set default host
	host := pda.Host
	if host == "" {
		host = "localhost"
	}

	// set default port
	port := pda.Port
	if port == "" {
		port = "5432"
	}

	args := []string{
		"--host=" + host,
		"--port=" + port,
		"--username=" + pda.Username,
		"--dbname=" + pda.Database,
		"--file=" + pda.OutName,
		"--format=" + pda.OutFormat,
	}

	if pda.Compress {
		if pda.CompressionAlgorithm == "" {
			pda.CompressionAlgorithm = "gzip"
		}

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
