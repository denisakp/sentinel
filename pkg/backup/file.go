package backup

import (
	"fmt"
	"strings"
	"time"
)

// GenerateBackupOutName generates the backup output name
func GenerateBackupOutName(dbName string) string {
	timestamp := time.Now().Format("2006-01-02T15-04-05")

	if dbName == "" {
		return fmt.Sprintf("BACKUP_%s", timestamp)
	}

	return fmt.Sprintf("BACKUP_%s_%s", strings.ToUpper(dbName), timestamp)
}
