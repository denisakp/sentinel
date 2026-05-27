package pg_dump

import (
	"fmt"
	"time"

	"github.com/denisakp/sentinel/internal/backup"
	"github.com/denisakp/sentinel/internal/adapters/storage"
	internaltls "github.com/denisakp/sentinel/internal/adapters/tls"
	"github.com/denisakp/sentinel/internal/ports"
	"github.com/denisakp/sentinel/internal/utils"
)

type PgDumpArgs struct {
	Host                 string          // PostgresSQL host
	Port                 string          // PostgresSQL port
	Username             string          // PostgresSQL username
	Password             string          // PostgresSQL password
	Database             string          // PostgresSQL database name
	PgOutFormat          string          // Output format for the backup file
	Compress             bool            // Enable compression
	CompressionAlgorithm string          // Compression algorithm
	CompressionLevel     int             // Compression level
	AdditionalArgs       string          // Additional arguments for the pg_dump command
	Storage              *storage.Params // Storage parameters
	TLS                  *ports.Config

	PITREnabled        bool      // Enables PITR metadata capture
	WALArchivePrefix   string    // WAL archive prefix associated with the backup lineage
	PITRWindowStartUTC time.Time // Earliest recoverable point for this backup lineage
	PITRWindowEndUTC   time.Time // Latest recoverable point for this backup lineage
	BackupTimelineID   string    // Source timeline id used for PITR metadata
	WALStartLSN        string    // First WAL LSN in backup lineage
	WALEndLSN          string    // Last WAL LSN in backup lineage
}

// engineOptions satisfies ports.EngineOptions.
func (*PgDumpArgs) IsEngineOptions() {}

// argsBuilder builds the arguments for the pg_dump command
func argsBuilder(pda *PgDumpArgs, backupPath string) ([]string, error) {
	if err := validateRequiredArgs(pda); err != nil {
		return nil, err
	}

	// initialize default arguments
	initializeDefaultArgs(pda)

	if err := validatePgOutFormat(pda.PgOutFormat); err != nil {
		return nil, err
	}
	if err := validatePITRMetadataArgs(pda); err != nil {
		return nil, err
	}

	// handle backup outName
	if err := setOutName(pda); err != nil {
		return nil, err
	}

	args := []string{
		fmt.Sprintf("--host=%s", pda.Host),
		fmt.Sprintf("--port=%s", pda.Port),
		fmt.Sprintf("--username=%s", pda.Username),
		fmt.Sprintf("--dbname=%s", pda.Database),
	}

	// Add output file for directory format only.
	// For file formats (c, p, t), pg_dump writes to stdout which is captured by
	// the Backup() function and passed to WriteBackup. Adding --file= for these
	// formats would cause pg_dump to write to the file directly, leaving stdout
	// empty and resulting in WriteBackup overwriting the real output with zero bytes.
	if pda.Storage.OutName != "" {
		if pda.PgOutFormat == "d" {
			// Directory format must use --file= to specify the output directory
			if pda.Storage.StorageType != "local" {
				pda.Storage.OutName = utils.FormatResourceValue(pda.Storage.OutName)
			} else {
				pda.Storage.OutName = utils.FullPath(backupPath, pda.Storage.OutName)
			}
			args = append(args, fmt.Sprintf("--file=%s", pda.Storage.OutName))
		} else {
			// File formats (c, p, t): pg_dump writes to stdout; set OutName for WriteBackup
			pda.Storage.OutName = utils.FullPath(backupPath, pda.Storage.OutName)
		}
	}

	args = append(args, fmt.Sprintf("--format=%s", pda.PgOutFormat))

	if pda.Compress {
		if err := addCompression(&args, pda); err != nil {
			return nil, err
		}
	}

	// handle additional arguments
	if pda.AdditionalArgs != "" {
		additionalArgs, err := backup.ParseAdditionalArgs(pda.AdditionalArgs)
		if err != nil {
			return nil, fmt.Errorf("failed to parse additional_args: %w", err)
		}
		args = append(args, additionalArgs...)
	}

	args = append(args, internaltls.BuildTLSArgs("postgres", pda.TLS)...)

	// remove duplicated arguments
	args = backup.RemoveArgsDuplicate(args) // remove duplicated arguments

	return args, nil
}

func addCompression(args *[]string, pda *PgDumpArgs) error {
	// set the default compression algorithm to gzip if not provided
	pda.CompressionAlgorithm = utils.DefaultValue(pda.CompressionAlgorithm, "gzip")
	if err := validatePgCompressionAlgorithm(pda.CompressionAlgorithm); err != nil {
		return err
	}

	// validate the compression level
	if err := validatePgCompressionLevel(pda.CompressionLevel); err != nil {
		return err
	}

	// add the compression arguments
	*args = append(*args, fmt.Sprintf("--compress=%s:%d", pda.CompressionAlgorithm, pda.CompressionLevel))

	return nil
}

func initializeDefaultArgs(pda *PgDumpArgs) {
	pda.Host = utils.DefaultValue(pda.Host, "127.0.0.1")
	pda.Port = utils.DefaultValue(pda.Port, "5432")
	pda.PgOutFormat = utils.DefaultValue(pda.PgOutFormat, "p")

	if pda.CompressionAlgorithm != "" {
		pda.Compress = true
	}
}
