package backup

import (
	"fmt"
	"strings"
	"time"
)

// GenerateBackupOutName generates the backup output name
func GenerateBackupOutName(dbName string) string {
	timestamp := time.Now().Format("2006-01-02T15-04-05") // format the current time

	dbName = DefaultString(dbName, fmt.Sprintf("BACKUP_%s", timestamp)) // set the default database name to an empty string if not provided

	return fmt.Sprintf("BACKUP_%s_%s", strings.ToUpper(dbName), timestamp)
}
