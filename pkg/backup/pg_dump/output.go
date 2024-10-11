package pg_dump

import (
	"fmt"
	"github.com/denisakp/sentinel/pkg/backup"
)

// setOutName Helper function to set output name based on compression and format
func setOutName(pda *PgDumpArgs) error {
	if pda.OutName == "" {
		pda.OutName = backup.GenerateBackupOutName(pda.Database)
	}

	if pda.Compress && pda.OutFormat == "" {
		pda.OutFormat = "c"
	}

	switch pda.OutFormat {
	case "c":
		pda.OutName += ".backup"
	case "d":
	// Directory format if empty and compression is enabled
	case "t":
		if pda.Compress {
			return fmt.Errorf("tar format does not support compression")
		}
		pda.OutName += ".tar"
	case "p":
		if pda.Compress {
			return fmt.Errorf("plain format does not support compression")
		}
		pda.OutName += ".sql"
	default:
		if pda.Compress {
			return fmt.Errorf("format %s is not supported for compressed dump", pda.OutFormat)
		}
		pda.OutName += ".backup"
	}

	return nil
}
