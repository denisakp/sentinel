package pg_dump

import (
	"fmt"
	"github.com/denisakp/sentinel/internal/utils"
)

// setOutName Helper function to set output name based on compression and format
func setOutName(pda *PgDumpArgs) error {
	pda.Storage.OutName = utils.DefaultValue(pda.Storage.OutName, utils.DefaultBackupOutName())

	if pda.Compress && pda.PgOutFormat == "p" {
		return fmt.Errorf("plain format does not support compression")
	}
	if pda.Compress && pda.PgOutFormat == "t" {
		return fmt.Errorf("tar format does not support compression")
	}

	switch pda.PgOutFormat {
	case "c":
		pda.Storage.OutName += ".backup"
	case "d":
	// Directory format if empty and compression is enabled
	case "t":
		pda.Storage.OutName += ".tar"
	case "p":
		pda.Storage.OutName += ".sql"
	default:
		return fmt.Errorf("unsupported output format: %s", pda.PgOutFormat)
	}

	return nil
}
