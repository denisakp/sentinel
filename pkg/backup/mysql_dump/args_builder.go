package mysql_dump

import (
	"github.com/denisakp/sentinel/internal/backup"
)

type MySqlDumpArgs struct {
	Host           string // MySQL host
	Port           string // MySQL port
	Username       string // MySQL username
	Password       string // MySQL password
	Database       string // MySQL database name
	AdditionalArgs string // Additional arguments for the mysql_dump command
	OutName        string // Output name for the backup file
}

// argsBuilder builds the arguments for the mysql_dump command
func argsBuilder(mda *MySqlDumpArgs) ([]string, error) {
	if err := validateRequiredArgs(mda); err != nil {
		return nil, err
	}

	mda.Host = backup.DefaultString(mda.Host, "127.0.0.1") // set the default host to 127.0.0.1 if not provided
	mda.Port = backup.DefaultString(mda.Port, "3306")      // set the default port to 3306 if not provided

	args := []string{
		"--host=" + mda.Host,
		"--port=" + mda.Port,
		"--user=" + mda.Username,
	}

	if mda.Password == "" {
		args = append(args, "--skip-password")
	} // skip password prompt if password is not provided

	if mda.AdditionalArgs != "" {
		additionalArgs := backup.ParseAdditionalArgs(mda.AdditionalArgs)
		args = append(args, additionalArgs...)
	} // handle additional arguments

	args = backup.RemoveArgsDuplicate(args) // remove duplicate arguments
	args = append(args, mda.Database)       // add database name

	return args, nil
}
