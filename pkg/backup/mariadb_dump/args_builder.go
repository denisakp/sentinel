package mariadb_dump

import (
	"github.com/denisakp/sentinel/internal/backup"
)

type MariaDBDumpArgs struct {
	Host           string // MariaDB host
	Port           string // MariaDB port
	Username       string // MariaDB username
	Password       string // MariaDB password
	Database       string // MariaDB database name
	AdditionalArgs string // Additional arguments for the mariadb_dump command
	OutName        string // Output name for the backup file
}

// ArgsBuilder builds the arguments for the mariadb_dump command
func ArgsBuilder(mda *MariaDBDumpArgs) ([]string, error) {
	if err := validateRequiredArgs(mda); err != nil {
		return nil, err
	}

	mda.Host = backup.DefaultString(mda.Host, "127.0.0.1") // set the default host to 127.0.0.1 if not provided
	mda.Port = backup.DefaultString(mda.Port, "3306")      // set the default port to 3306 if not provided

	args := []string{
		"--host=" + mda.Host,
		"--port=" + mda.Port,
		"--user=" + mda.Username,
	} // build the required arguments

	if mda.Password != "" {
		args = append(args, "--password="+mda.Password)
	} // add the password argument if provided

	if mda.AdditionalArgs != "" {
		additionalArgs := backup.ParseAdditionalArgs(mda.AdditionalArgs)
		args = append(args, additionalArgs...)
	} // add additional arguments if provided

	args = backup.RemoveArgsDuplicate(args) // remove duplicated arguments
	args = append(args, mda.Database)       // add the database name to the arguments

	return args, nil
}
