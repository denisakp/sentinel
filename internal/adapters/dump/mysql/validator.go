package mysql

import "github.com/denisakp/sentinel/internal/adapters/mysqlargs"

// validateRequiredArgs delegates to the shared MySQL-family validator.
func validateRequiredArgs(mda *MySqlDumpArgs) error {
	return mysqlargs.ValidateRequired(mda.Username, mda.Database)
}
