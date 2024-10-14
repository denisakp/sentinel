package mysql_dump

import "fmt"

func validateRequiredArgs(mda *MySqlDumpArgs) error {
	if mda.Database == "" {
		return fmt.Errorf("database name is missing")
	}

	if mda.Username == "" {
		return fmt.Errorf("username is missing")
	}

	return nil
}
