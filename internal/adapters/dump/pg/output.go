package pg

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
		if !hasExtension(pda.Storage.OutName, ".backup") {
			pda.Storage.OutName += ".backup"
		}
	case "d":
		// Directory format - no extension
	case "t":
		if !hasExtension(pda.Storage.OutName, ".tar") {
			pda.Storage.OutName += ".tar"
		}
	case "p":
		if !hasExtension(pda.Storage.OutName, ".sql") {
			pda.Storage.OutName += ".sql"
		}
	}

	return nil
}

func hasExtension(filename, ext string) bool {
	return len(filename) > len(ext) && filename[len(filename)-len(ext):] == ext
}
