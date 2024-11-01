package utils

import (
	"fmt"
	"path/filepath"
	"time"
)

// DefaultValue returns the default value if the value is empty
func DefaultValue(value, defaultValue string) string {
	if value == "" {
		return defaultValue
	}
	return value
}

// DefaultBackupOutName returns the default backup output name
func DefaultBackupOutName() string {
	timestamp := time.Now().Format("2006-01-02T15-04-05") // format the current time
	return fmt.Sprintf("SENTINEL_%s", timestamp)
}

func FinalOutName(outName string) string {
	ext := filepath.Ext(outName)

	if ext == "" {
		return DefaultValue(outName, DefaultBackupOutName()) + ".sql"
	}

	return outName
}

func FullPath(path, fileName string) string {
	return filepath.Join(path, fileName)
}
