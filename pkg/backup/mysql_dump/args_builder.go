package mysql_dump

import (
	"fmt"

	"github.com/denisakp/sentinel/internal/backup"
	"github.com/denisakp/sentinel/internal/adapters/storage"
	internaltls "github.com/denisakp/sentinel/internal/tls"
	"github.com/denisakp/sentinel/internal/ports"
	"github.com/denisakp/sentinel/internal/utils"
)

type MySqlDumpArgs struct {
	Host           string          // MySQL host
	Port           string          // MySQL port
	Username       string          // MySQL username
	Password       string          // MySQL password
	Database       string          // MySQL database name
	AdditionalArgs string          // Additional arguments for the mysql_dump command
	Storage        *storage.Params // Storage parameters
	TLS            *ports.Config
}

// engineOptions satisfies ports.EngineOptions.
func (*MySqlDumpArgs) IsEngineOptions() {}

// argsBuilder builds the arguments for the mysql_dump command
func argsBuilder(mda *MySqlDumpArgs) ([]string, error) {
	if err := validateRequiredArgs(mda); err != nil {
		return nil, err
	}

	mda.Host = utils.DefaultValue(mda.Host, "127.0.0.1") // set the default host to 127.0.0.1 if not provided
	mda.Port = utils.DefaultValue(mda.Port, "3306")      // set the default port to 3306 if not provided

	args := []string{
		fmt.Sprintf("--host=%s", mda.Host),
		fmt.Sprintf("--port=%s", mda.Port),
		fmt.Sprintf("--user=%s", mda.Username),
	}

	if mda.Password == "" {
		args = append(args, "--skip-password")
	} // skip password prompt if password is not provided

	if mda.AdditionalArgs != "" {
		additionalArgs, err := backup.ParseAdditionalArgs(mda.AdditionalArgs)
		if err != nil {
			return nil, fmt.Errorf("failed to parse additional_args: %w", err)
		}
		args = append(args, additionalArgs...)
	} // handle additional arguments

	args = append(args, internaltls.BuildTLSArgs("mysql", mda.TLS)...)

	args = backup.RemoveArgsDuplicate(args) // remove duplicate arguments
	args = append(args, mda.Database)       // add database name

	return args, nil
}
