package mysql_dump

import "fmt"

// validateRequiredArgs validates required arguments for MySQL dump
func validateRequiredArgs(mda *MySqlDumpArgs) error {
	if mda.Database == "" {
		return fmt.Errorf("database name is missing")
	}

	if mda.Username == "" {
		return fmt.Errorf("username is missing")
	}

	return nil
}
