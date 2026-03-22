package pg_dump

import (
	"fmt"
)

func validatePgOutFormat(format string) error {
	validFormat := map[string]string{
		"c": "custom",
		"d": "directory",
		"t": "tar",
		"p": "plain",
	}

	if _, ok := validFormat[format]; !ok {
		return fmt.Errorf("unsupported format: %s", format)
	}

	return nil
}

func validatePgCompressionAlgorithm(algorithm string) error {
	validAlgorithm := map[string]string{
		"gzip": "gzip",
		"lz4":  "lz4",
		"none": "none",
		"zstd": "zstd",
	}

	if _, ok := validAlgorithm[algorithm]; !ok {
		return fmt.Errorf("unsupported compression algorithm: %s", algorithm)
	}

	return nil
}

func validatePgCompressionLevel(level int) error {
	// level should be 0-9 or -1 for default
	// 0 means no compression, 1-9 are valid levels
	if level < 0 || level > 9 {
		return fmt.Errorf("invalid compression level: %d", level)
	}

	return nil
}

func validateRequiredArgs(pda *PgDumpArgs) error {
	if pda.Database == "" {
		return fmt.Errorf("database name is required")
	}

	if pda.Username == "" {
		return fmt.Errorf("username is required")
	}

	return nil
}

func validatePITRMetadataArgs(pda *PgDumpArgs) error {
	if pda == nil {
		return fmt.Errorf("pg_dump args are required")
	}
	if !pda.PITREnabled {
		return nil
	}
	if pda.WALArchivePrefix == "" {
		return fmt.Errorf("wal archive prefix is required when pitr metadata capture is enabled")
	}
	if !pda.PITRWindowStartUTC.IsZero() && !pda.PITRWindowEndUTC.IsZero() && pda.PITRWindowEndUTC.Before(pda.PITRWindowStartUTC) {
		return fmt.Errorf("pitr window end must be greater than or equal to start")
	}
	return nil
}
