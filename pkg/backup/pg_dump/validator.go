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
	if level != -1 && (level < 1 || level > 9) {
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
