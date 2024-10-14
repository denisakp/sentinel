package mariadb_dump

import "github.com/denisakp/sentinel/pkg/backup"

type MariaDBDumpArgs struct {
	Host           string
	Port           string
	Username       string
	Password       string
	Database       string
	AdditionalArgs string
	OutName        string
}

// ArgsBuilder builds the arguments for the mariadb_dump command
func ArgsBuilder(mda *MariaDBDumpArgs) ([]string, error) {
	if err := validateRequiredArgs(mda); err != nil {
		return nil, err
	}

	mda.Host = backup.DefaultString(mda.Host, "127.0.0.1")
	mda.Port = backup.DefaultString(mda.Port, "3306")

	args := []string{
		"--host=" + mda.Host,
		"--port=" + mda.Port,
		"--user=" + mda.Username,
	}

	if mda.Password != "" {
		args = append(args, "--password="+mda.Password)
	}

	if mda.AdditionalArgs != "" {
		additionalArgs := backup.ParseAdditionalArgs(mda.AdditionalArgs)
		args = append(args, additionalArgs...)
	}

	args = backup.RemoveArgsDuplicate(args)
	args = append(args, mda.Database)

	return args, nil
}
