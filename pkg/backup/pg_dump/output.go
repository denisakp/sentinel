package pg_dump

import (
	"fmt"
	"github.com/denisakp/sentinel/internal/utils"
)

// setOutName Helper function to set output name based on compression and format
func setOutName(pda *PgDumpArgs) error {
	pda.OutName = utils.DefaultValue(pda.OutName, utils.DefaultBackupOutName())

	if pda.Compress && pda.OutFormat == "p" {
		return fmt.Errorf("plain format does not support compression")
	}
	if pda.Compress && pda.OutFormat == "t" {
		return fmt.Errorf("tar format does not support compression")
	}

	switch pda.OutFormat {
	case "c":
		pda.OutName += ".backup"
	case "d":
	// Directory format if empty and compression is enabled
	case "t":
		pda.OutName += ".tar"
	case "p":
		pda.OutName += ".sql"
	default:
		return fmt.Errorf("unsupported output format: %s", pda.OutFormat)
	}

	return nil
}
